package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// changed records that something a person chose was written: a user, an invitation, a mail
// account or what it is set to, a device, the instance's push keys. What the mirror keeps of the
// mail servers, jobs, sessions and drafts on their way to the server are read or made again, and
// do not count. See docs/deploy.md.
func (s *Store) changed() { s.changes.Add(1) }

// Changes is the counter the backup loop compares against what the agent last accepted.
func (s *Store) Changes() uint64 { return s.changes.Load() }

// SnapshotTo writes a consistent copy of the database to path.
//
// VACUUM INTO rather than a file copy: a WAL database is three files with the committed state
// spread across them, so a file-level copy of a running instance is a copy of a moment that
// never existed.
func (s *Store) SnapshotTo(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// VACUUM INTO refuses to overwrite.
	os.Remove(path)
	if _, err := s.writer.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	return nil
}
