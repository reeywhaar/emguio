package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"emguio/internal/ids"
)

// How long a session lasts and how often its window is pushed forward.
//
// The throttle is not an optimization: without it every request rewrites the row and emits a
// Set-Cookie, for a window measured in days.
const (
	SessionLifetime = 7 * 24 * time.Hour
	SessionRefresh  = time.Hour
)

// userAgentMax is how much of the browser's description of itself is kept.
const userAgentMax = 400

// Session is one live sign-in.
//
// There is no field holding the cookie value and no way to get one from this package. The
// table is keyed by sha256 of it, so nothing readable ever contains something replayable, and
// the lookup is timing-safe without trying to be: telling two rows apart by timing would mean
// finding a 256-bit preimage.
type Session struct {
	// ID is derived from the stored hash, so it is stable for the life of the session and
	// reveals nothing: a hash of a hash of a value only the browser holds.
	ID         string
	UserID     string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

// SessionID names in public the session presenting this token, without a lookup.
func SessionID(token string) string { return sessionID(hashToken(token)) }

func sessionID(idHash []byte) string { return ids.Derive(ids.Session, idHash) }

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateSession records a sign-in. token is the cookie value; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, token, userID, userAgent string) (*Session, error) {
	now := s.Now()
	expires := now.Add(SessionLifetime)
	userAgent = truncate(userAgent, userAgentMax)

	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, user_id, user_agent, created_at, last_seen_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, userAgent, unix(now), unix(now), unix(expires))
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &Session{
		ID:         SessionID(token),
		UserID:     userID,
		UserAgent:  userAgent,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  expires,
	}, nil
}

// SessionByToken returns a live session, or ErrNotFound.
//
// Expiry is applied in the query. A lapsed row is not a session that exists and is refused; it
// is not a session.
func (s *Store) SessionByToken(ctx context.Context, token string) (*Session, error) {
	var (
		sess                       Session
		created, lastSeen, expires int64
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT user_id, user_agent, created_at, last_seen_at, expires_at
		   FROM sessions WHERE id_hash = ? AND expires_at > ?`,
		hashToken(token), unix(s.Now())).
		Scan(&sess.UserID, &sess.UserAgent, &created, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	sess.ID = SessionID(token)
	sess.CreatedAt = fromUnix(created)
	sess.LastSeenAt = fromUnix(lastSeen)
	sess.ExpiresAt = fromUnix(expires)
	return &sess, nil
}

// TouchSession slides the window forward and reports whether it moved.
func (s *Store) TouchSession(ctx context.Context, token, userAgent string) (time.Time, bool, error) {
	now := s.Now()
	expires := now.Add(SessionLifetime)
	res, err := s.writer.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ?, expires_at = ?, user_agent = ?
		   WHERE id_hash = ? AND last_seen_at <= ?`,
		unix(now), unix(expires), truncate(userAgent, userAgentMax),
		hashToken(token), unix(now.Add(-SessionRefresh)))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("touch session: %w", err)
	}
	n, _ := res.RowsAffected()
	return expires, n > 0, nil
}

// Sessions lists a user's live sign-ins, most recently used first. Lapsed rows are left out
// rather than left to the sweep: one that expired minutes ago is not offered for signing out.
func (s *Store) Sessions(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id_hash, user_agent, created_at, last_seen_at, expires_at
		   FROM sessions WHERE user_id = ? AND expires_at > ?
		  ORDER BY last_seen_at DESC`,
		userID, unix(s.Now()))
	if err != nil {
		return nil, fmt.Errorf("sessions: %w", err)
	}
	defer rows.Close()
	out := []*Session{}
	for rows.Next() {
		var (
			idHash                     []byte
			created, lastSeen, expires int64
			sess                       = Session{UserID: userID}
		)
		if err := rows.Scan(&idHash, &sess.UserAgent, &created, &lastSeen, &expires); err != nil {
			return nil, fmt.Errorf("sessions: %w", err)
		}
		sess.ID = sessionID(idHash)
		sess.CreatedAt = fromUnix(created)
		sess.LastSeenAt = fromUnix(lastSeen)
		sess.ExpiresAt = fromUnix(expires)
		out = append(out, &sess)
	}
	return out, rows.Err()
}

// RevokeSession signs out one of a user's sessions by its public id.
//
// The id is a digest, so no query turns it back into a key: the user's own rows are read and
// matched. That is the scoping too: somebody else's session matches nothing here.
func (s *Store) RevokeSession(ctx context.Context, userID, id string) error {
	if !ids.Valid(ids.Session, id) {
		return Invalid("%q is not a session id.", id)
	}
	rows, err := s.reader.QueryContext(ctx, `SELECT id_hash FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	var target []byte
	for rows.Next() {
		var idHash []byte
		if err := rows.Scan(&idHash); err != nil {
			rows.Close()
			return fmt.Errorf("revoke session: %w", err)
		}
		if sessionID(idHash) == id {
			target = idHash
			break
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if target == nil {
		return NotFound("There is no such session.")
	}
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, target); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// RevokeOtherSessions signs out every session of a user but the one presenting token.
func (s *Store) RevokeOtherSessions(ctx context.Context, userID, token string) (int64, error) {
	res, err := s.writer.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = ? AND id_hash <> ?`, userID, hashToken(token))
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return res.RowsAffected()
}

// DeleteSession signs one session out.
//
// Deleting one that is already gone is not an error: a sign-out is a statement about the
// future, not a claim about the present.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.writer.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, hashToken(token))
	return err
}

// SweepSessions collects lapsed rows, which are already unusable.
func (s *Store) SweepSessions(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, unix(s.Now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
