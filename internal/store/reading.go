package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Reading is what a user's mail does as it is put away, in every email config that does not
// choose for itself. See docs/reading.md.
type Reading struct {
	// ReadOnArchive and ReadOnTrash are whether mail is marked read before it is archived, and
	// before it is deleted.
	ReadOnArchive bool
	ReadOnTrash   bool
}

// Reading is a user's choice for every email config.
func (s *Store) Reading(ctx context.Context, userID string) (Reading, error) {
	var out Reading
	err := s.reader.QueryRowContext(ctx,
		`SELECT read_on_archive, read_on_trash FROM users WHERE id = ?`, userID).
		Scan(&out.ReadOnArchive, &out.ReadOnTrash)
	if errors.Is(err, sql.ErrNoRows) {
		return out, NotFound("There is no such user.")
	}
	if err != nil {
		return out, fmt.Errorf("reading: %w", err)
	}
	return out, nil
}

// SetReading chooses it.
func (s *Store) SetReading(ctx context.Context, userID string, r Reading) error {
	res, err := s.writer.ExecContext(ctx,
		`UPDATE users SET read_on_archive = ?, read_on_trash = ? WHERE id = ?`,
		r.ReadOnArchive, r.ReadOnTrash, userID)
	if err != nil {
		return fmt.Errorf("set reading: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such user.")
	}
	s.changed()
	return nil
}
