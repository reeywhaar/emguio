package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"emguio/internal/ids"
)

// bcryptCost is 12. The login rate limit exists partly to bound what that costs on an
// unauthenticated endpoint.
//
// A var so tests can lower it: a suite that signs in a few dozen times otherwise spends its
// whole run inside the hash it is not testing.
var bcryptCost = 12

// SetBcryptCost lowers the work factor. Tests only.
func SetBcryptCost(cost int) {
	bcryptCost = cost
	h, _ := bcrypt.GenerateFromPassword([]byte("decoy"), cost)
	decoyHash = h
}

// passwordMaxBytes is bcrypt's own limit, checked rather than silently cut.
//
// Bytes and not runes, because the limit is on the encoded form and an emoji costs four. A
// longer password would otherwise authenticate against any prefix of itself.
const passwordMaxBytes = 72

// User is a person who signs in to emguio.
type User struct {
	ID        string
	Username  string
	CreatedAt time.Time
}

// execer is a pool or a transaction, so one insert serves both a bare create and an invitation
// accepted inside its own transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// CreateUser makes a user.
func (s *Store) CreateUser(ctx context.Context, username, password string) (*User, error) {
	u, hash, err := s.newUser(username, password)
	if err != nil {
		return nil, err
	}
	if err := insertUser(ctx, s.writer, u, hash); err != nil {
		return nil, err
	}
	return u, nil
}

// newUser validates and hashes, outside any transaction: bcrypt at cost 12 is the slowest
// thing a sign-up does, and it should not hold the one writer while it runs.
func (s *Store) newUser(username, password string) (*User, []byte, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return nil, nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, nil, err
	}
	now := s.Now()
	return &User{ID: ids.New(ids.User, now.UnixMilli()), Username: username, CreatedAt: now}, hash, nil
}

func insertUser(ctx context.Context, db execer, u *User, hash []byte) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, username, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		u.ID, u.Username, string(hash), unix(u.CreatedAt))
	if isUnique(err) {
		return Conflict("That username is taken.")
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// Authenticate checks a password.
//
// The same refusal for a wrong password and a missing user, and a real hash is compared against
// when no user matched, so the two take the same time. Without that, response latency alone is
// a list of which usernames exist.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*User, error) {
	var (
		u       User
		hash    string
		created int64
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE lower(username) = lower(?)`,
		username).Scan(&u.ID, &u.Username, &hash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		bcrypt.CompareHashAndPassword(decoyHash, []byte(password))
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrNotFound
	}
	u.CreatedAt = fromUnix(created)
	return &u, nil
}

// decoyHash is a real bcrypt hash at the same cost, so a missing user costs what a present one
// does.
var decoyHash = func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("decoy"), bcryptCost)
	return h
}()

// UserByID returns one user.
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	var (
		u       User
		created int64
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT id, username, created_at FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user: %w", err)
	}
	u.CreatedAt = fromUnix(created)
	return &u, nil
}

// HasUsers reports whether anybody can sign in yet. Nothing else decides whether serve prints
// an invitation at startup.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var n int
	err := s.reader.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n > 0, err
}

// ValidateUsername refuses what cannot be a username.
func ValidateUsername(username string) error {
	switch {
	case username == "":
		return Invalid("A username is required.")
	case len(username) > 64:
		return Invalid("That username is longer than 64 characters.")
	}
	for _, r := range username {
		if r < 0x20 || r == 0x7f {
			return Invalid("A username cannot contain control characters.")
		}
	}
	return nil
}

// ValidatePassword refuses what bcrypt would silently truncate.
func ValidatePassword(password string) error {
	switch {
	case len(password) < 8:
		return Invalid("A password needs at least 8 characters.")
	case len(password) > passwordMaxBytes:
		return Invalid("That password is longer than %d bytes, which is as much as the hash reads.", passwordMaxBytes)
	}
	return nil
}
