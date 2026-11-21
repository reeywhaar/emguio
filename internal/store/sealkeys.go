package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"emguio/internal/seal"
)

/*
A user's mail passwords are sealed with a key of their own, held in memory only from when they
unseal until the process stops. See docs/email-configs.md.

That key is random and kept sealed, in turn, with one made from the user's seal key: a passphrase
they choose, apart from their password. Changing the seal key seals the same key again, so the
mail passwords stay as they are; a wrong one does not open it.
*/

// sealKeyMin is the shortest seal key, in characters.
const sealKeyMin = 12

// Argon2id, as RFC 9106 has it for a server short of memory: two passes over 19 MiB.
const (
	kdfTime    = 2
	kdfMemory  = 19 * 1024
	kdfThreads = 1
	saltSize   = 16
)

// ValidateSealKey says why a seal key will not do, or nothing.
func ValidateSealKey(key string) error {
	if utf8.RuneCountInString(key) < sealKeyMin {
		return Invalid("A seal key is at least %d characters.", sealKeyMin)
	}
	return nil
}

// SealKeyState is whether a user has a seal key, and whether it is open now.
type SealKeyState string

const (
	SealKeyNone   SealKeyState = "none"
	SealKeySealed SealKeyState = "sealed"
	SealKeyOpen   SealKeyState = "open"
)

// SealState is a user's: none before they make a key, sealed until they unseal, open after.
func (s *Store) SealState(ctx context.Context, userID string) (SealKeyState, error) {
	if s.sealerOf(userID) != nil {
		return SealKeyOpen, nil
	}
	_, wrapped, err := s.sealKey(ctx, userID)
	if err != nil {
		return "", err
	}
	if wrapped == nil {
		return SealKeyNone, nil
	}
	return SealKeySealed, nil
}

// CreateSealKey makes a user's first seal key, and opens it.
func (s *Store) CreateSealKey(ctx context.Context, userID, key string) error {
	if err := ValidateSealKey(key); err != nil {
		return err
	}
	if _, wrapped, err := s.sealKey(ctx, userID); err != nil {
		return err
	} else if wrapped != nil {
		return Conflict("You have a seal key already.")
	}
	return s.newSealKey(ctx, userID, key)
}

// ResetSealKey replaces a forgotten seal key with a new one, and opens it. What was sealed with
// the old one no longer opens: each mail account asks for its passwords again.
func (s *Store) ResetSealKey(ctx context.Context, userID, key string) error {
	if err := ValidateSealKey(key); err != nil {
		return err
	}
	return s.newSealKey(ctx, userID, key)
}

func (s *Store) newSealKey(ctx context.Context, userID, key string) error {
	inner := make([]byte, seal.KeySize)
	if _, err := rand.Read(inner); err != nil {
		return err
	}
	sealer, err := seal.New(inner)
	if err != nil {
		return err
	}
	if err := s.wrap(ctx, userID, key, inner); err != nil {
		return err
	}
	s.keysMu.Lock()
	s.keys[userID] = sealer
	s.keysMu.Unlock()
	s.changed()
	return nil
}

// CarrySealKey makes the first seal key of a user from before seal keys out of the key emguio ran
// with then, which their saved mail passwords are sealed with: each it opens is sealed again with
// a key of the user's own, and that old key is their seal key from then on. It has to open at
// least one of them, where there are any.
func (s *Store) CarrySealKey(ctx context.Context, userID, key string) error {
	key = strings.TrimSpace(key)
	if _, wrapped, err := s.sealKey(ctx, userID); err != nil {
		return err
	} else if wrapped != nil {
		return Conflict("You have a seal key already.")
	}
	old, err := serverKey(key)
	if err != nil {
		return err
	}
	type secret struct {
		id, role string
		plain    []byte
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, incoming_secret, outgoing_secret FROM email_configs WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("carry seal key: %w", err)
	}
	var (
		opened []secret
		saved  int
	)
	for rows.Next() {
		var (
			id      string
			in, out []byte
		)
		if err := rows.Scan(&id, &in, &out); err != nil {
			rows.Close()
			return fmt.Errorf("carry seal key: %w", err)
		}
		for role, sealed := range map[string][]byte{"incoming": in, "outgoing": out} {
			if sealed == nil {
				continue
			}
			saved++
			if plain, err := old.Open(sealed, secretAAD(id, role)); err == nil {
				opened = append(opened, secret{id, role, plain})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("carry seal key: %w", err)
	}
	if saved > 0 && len(opened) == 0 {
		return Invalid("That key does not open your saved passwords.")
	}

	inner := make([]byte, seal.KeySize)
	if _, err := rand.Read(inner); err != nil {
		return err
	}
	sealer, err := seal.New(inner)
	if err != nil {
		return err
	}
	salt, sealed, err := wrapped(userID, key, inner)
	if err != nil {
		return err
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("carry seal key: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE users SET seal_salt = ?, seal_key = ? WHERE id = ?`, salt, sealed, userID); err != nil {
		return fmt.Errorf("carry seal key: %w", err)
	}
	for _, sc := range opened {
		if _, err := tx.ExecContext(ctx, `UPDATE email_configs SET `+sc.role+`_secret = ? WHERE id = ?`,
			sealer.Seal(sc.plain, secretAAD(sc.id, sc.role)), sc.id); err != nil {
			return fmt.Errorf("carry seal key: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("carry seal key: %w", err)
	}
	s.keysMu.Lock()
	s.keys[userID] = sealer
	s.keysMu.Unlock()
	s.changed()
	return nil
}

// serverKey reads the key emguio ran with before seal keys: 32 bytes in base64, either alphabet,
// padded or not, as `openssl rand -base64 32` and a hand-copied value disagree about both.
func serverKey(raw string) (*seal.Sealer, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == seal.KeySize {
			return seal.New(key)
		}
	}
	return nil, Invalid("That is not the key emguio ran with: it is 32 bytes in base64, as EMGUIO_SECRET_KEY was.")
}

// Unseal opens a user's seal key until the process stops.
func (s *Store) Unseal(ctx context.Context, userID, key string) error {
	inner, err := s.unwrap(ctx, userID, key)
	if err != nil {
		return err
	}
	sealer, err := seal.New(inner)
	if err != nil {
		return err
	}
	s.keysMu.Lock()
	s.keys[userID] = sealer
	s.keysMu.Unlock()
	return nil
}

// ChangeSealKey seals a user's key again with a new seal key, which the current one has to open.
func (s *Store) ChangeSealKey(ctx context.Context, userID, current, next string) error {
	if err := ValidateSealKey(next); err != nil {
		return err
	}
	inner, err := s.unwrap(ctx, userID, current)
	if err != nil {
		return err
	}
	if err := s.wrap(ctx, userID, next, inner); err != nil {
		return err
	}
	s.changed()
	return nil
}

// Lock forgets a user's open key, as a restart does.
func (s *Store) Lock(userID string) {
	s.keysMu.Lock()
	delete(s.keys, userID)
	s.keysMu.Unlock()
}

// Unsealed is whether a user's seal key is open now.
func (s *Store) Unsealed(userID string) bool { return s.sealerOf(userID) != nil }

func (s *Store) sealerOf(userID string) *seal.Sealer {
	s.keysMu.RLock()
	defer s.keysMu.RUnlock()
	return s.keys[userID]
}

// openSealer is a user's open key, or why there is none.
func (s *Store) openSealer(userID string) (*seal.Sealer, error) {
	if sl := s.sealerOf(userID); sl != nil {
		return sl, nil
	}
	return nil, Sealed("Unseal emguio with your seal key first.")
}

func (s *Store) sealKey(ctx context.Context, userID string) (salt, wrapped []byte, err error) {
	err = s.reader.QueryRowContext(ctx, `SELECT seal_salt, seal_key FROM users WHERE id = ?`, userID).Scan(&salt, &wrapped)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, NotFound("There is no such user.")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("seal key: %w", err)
	}
	return salt, wrapped, nil
}

// made is the key a seal key makes with salt, as a sealer.
func made(key string, salt []byte) (*seal.Sealer, error) {
	return seal.New(argon2.IDKey([]byte(key), salt, kdfTime, kdfMemory, kdfThreads, seal.KeySize))
}

func wrapAAD(userID string) []byte { return []byte(userID + "/seal key") }

// wrapped is inner sealed with what key makes, and the salt it was made with.
func wrapped(userID, key string, inner []byte) (salt, sealed []byte, err error) {
	salt = make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}
	outer, err := made(key, salt)
	if err != nil {
		return nil, nil, err
	}
	return salt, outer.Seal(inner, wrapAAD(userID)), nil
}

func (s *Store) wrap(ctx context.Context, userID, key string, inner []byte) error {
	salt, sealed, err := wrapped(userID, key, inner)
	if err != nil {
		return err
	}
	res, err := s.writer.ExecContext(ctx, `UPDATE users SET seal_salt = ?, seal_key = ? WHERE id = ?`,
		salt, sealed, userID)
	if err != nil {
		return fmt.Errorf("seal key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such user.")
	}
	return nil
}

func (s *Store) unwrap(ctx context.Context, userID, key string) ([]byte, error) {
	salt, wrapped, err := s.sealKey(ctx, userID)
	if err != nil {
		return nil, err
	}
	if wrapped == nil {
		return nil, Invalid("You have no seal key yet.")
	}
	outer, err := made(key, salt)
	if err != nil {
		return nil, err
	}
	inner, err := outer.Open(wrapped, wrapAAD(userID))
	if err != nil {
		return nil, Invalid("That is not your seal key.")
	}
	return inner, nil
}
