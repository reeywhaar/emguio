package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"emguio/internal/ids"
)

// InviteLifetime is how long a link is good for. Single use either way.
const InviteLifetime = 7 * 24 * time.Hour

// Invite is a link that makes one user.
type Invite struct {
	ID        string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateInvite mints a link and returns it with the token, which is readable exactly once.
//
// The table holds token_hash and never the token, so a lost link is reissued rather than
// recovered — the same stance as a session, and for the same reason.
func (s *Store) CreateInvite(ctx context.Context) (*Invite, string, error) {
	token, err := Secret()
	if err != nil {
		return nil, "", err
	}
	now := s.Now()
	inv := &Invite{
		ID:        ids.New(ids.Invite, now.UnixMilli()),
		CreatedAt: now,
		ExpiresAt: now.Add(InviteLifetime),
	}
	_, err = s.writer.ExecContext(ctx,
		`INSERT INTO invites (id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		inv.ID, hashToken(token), unix(now), unix(inv.ExpiresAt))
	if err != nil {
		return nil, "", fmt.Errorf("create invite: %w", err)
	}
	return inv, token, nil
}

// errInviteDead is the one refusal for a link that was used, expired, or never existed: telling
// them apart tells a stranger which tokens were real.
var errInviteDead = NotFound("That invitation has been used or has expired.")

// InviteByToken returns a live, unaccepted invitation.
//
// It reveals nothing but its own validity, which is what lets the acceptance page tell somebody
// a link is dead before they type a password into it.
func (s *Store) InviteByToken(ctx context.Context, token string) (*Invite, error) {
	var (
		inv              Invite
		created, expires int64
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT id, created_at, expires_at FROM invites
		  WHERE token_hash = ? AND accepted_at IS NULL AND expires_at > ?`,
		hashToken(token), unix(s.Now())).
		Scan(&inv.ID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errInviteDead
	}
	if err != nil {
		return nil, fmt.Errorf("invite: %w", err)
	}
	inv.CreatedAt = fromUnix(created)
	inv.ExpiresAt = fromUnix(expires)
	return &inv, nil
}

// AcceptInvite spends an invitation and makes the user it was for.
//
// One transaction for both: a username already taken leaves the link unspent for another try,
// and the guarded update means two requests racing on one link make one user.
func (s *Store) AcceptInvite(ctx context.Context, token, username, password string) (*User, error) {
	if _, err := s.InviteByToken(ctx, token); err != nil {
		return nil, err
	}
	u, hash, err := s.newUser(username, password)
	if err != nil {
		return nil, err
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("accept invite: %w", err)
	}
	defer tx.Rollback()

	if err := insertUser(ctx, tx, u, hash); err != nil {
		return nil, err
	}
	now := unix(s.Now())
	res, err := tx.ExecContext(ctx,
		`UPDATE invites SET accepted_at = ?, user_id = ?
		  WHERE token_hash = ? AND accepted_at IS NULL AND expires_at > ?`,
		now, u.ID, hashToken(token), now)
	if err != nil {
		return nil, fmt.Errorf("accept invite: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errInviteDead
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("accept invite: %w", err)
	}
	return u, nil
}

// SweepInvites collects links nobody used.
func (s *Store) SweepInvites(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx,
		`DELETE FROM invites WHERE accepted_at IS NULL AND expires_at <= ?`, unix(s.Now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Secret is 32 random bytes as base64url: a session cookie, an invitation token.
func Secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
