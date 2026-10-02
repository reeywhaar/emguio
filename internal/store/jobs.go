package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"emguio/internal/ids"
)

// What a job does to its message.
const (
	JobSeen    = "seen"
	JobFlagged = "flagged"
	JobMove    = "move"
	JobDelete  = "delete"
)

// Job is something a user asked to be done to a message, waiting to be done on the server or
// failed and waiting to be seen.
type Job struct {
	ID            string
	UserID        string
	EmailConfigID string
	MailboxID     string
	// Message is the API's name for it, {uidvalidity}-{uid}.
	Message string
	Kind    string
	// Value is set or cleared, for seen and flagged.
	Value bool
	// Target is the mailbox it is moved to.
	Target string
	// Seen is whether it was read when asked, as the asker drew it.
	Seen     bool
	Attempts int
	NextAt   time.Time
	// Error is the sentence a failed job ends with; empty while it waits.
	Error     string
	CreatedAt time.Time
}

const jobColumns = `id, user_id, email_config_id, mailbox_id, message, kind, value, target, seen,
  attempts, next_at, error, created_at`

func scanJob(sc interface{ Scan(...any) error }) (*Job, error) {
	var (
		j             Job
		next, created int64
	)
	if err := sc.Scan(&j.ID, &j.UserID, &j.EmailConfigID, &j.MailboxID, &j.Message, &j.Kind, &j.Value,
		&j.Target, &j.Seen, &j.Attempts, &next, &j.Error, &created); err != nil {
		return nil, err
	}
	j.NextAt, j.CreatedAt = fromUnix(next), fromUnix(created)
	return &j, nil
}

// AddJob queues a job, named and stamped here.
func (s *Store) AddJob(ctx context.Context, j Job) (*Job, error) {
	now := s.Now()
	j.ID, j.CreatedAt, j.NextAt = ids.New(ids.Job, now.UnixMilli()), now, now
	_, err := s.writer.ExecContext(ctx,
		`INSERT INTO jobs (`+jobColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, '', ?)`,
		j.ID, j.UserID, j.EmailConfigID, j.MailboxID, j.Message, j.Kind, j.Value, j.Target, j.Seen,
		unix(j.NextAt), unix(j.CreatedAt))
	if err != nil {
		return nil, fmt.Errorf("add job: %w", err)
	}
	return &j, nil
}

// Jobs is a user's jobs, waiting and failed, oldest first.
func (s *Store) Jobs(ctx context.Context, userID string) ([]*Job, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE user_id = ? ORDER BY rowid`, userID)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	defer rows.Close()
	out := []*Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("jobs: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// NextJob is the job an email config does next: its oldest waiting one, whether or not it may
// be tried yet. Nil when there is none. One at a time and in order, so a job asked after
// another is never done before it.
func (s *Store) NextJob(ctx context.Context, configID string) (*Job, error) {
	j, err := scanJob(s.reader.QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE email_config_id = ? AND error = '' ORDER BY rowid LIMIT 1`, configID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next job: %w", err)
	}
	return j, nil
}

// JobConfigs is every email config with a job waiting, for picking the queues up again after a
// restart.
func (s *Store) JobConfigs(ctx context.Context) ([]string, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT DISTINCT email_config_id FROM jobs WHERE error = ''`)
	if err != nil {
		return nil, fmt.Errorf("job configs: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("job configs: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RetryJob counts a try that did not get through, and puts the next off until at.
func (s *Store) RetryJob(ctx context.Context, id string, at time.Time) error {
	_, err := s.writer.ExecContext(ctx,
		`UPDATE jobs SET attempts = attempts + 1, next_at = ? WHERE id = ?`, unix(at), id)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	return nil
}

// FinishJob drops a job that is done.
func (s *Store) FinishJob(ctx context.Context, id string) error {
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("finish job: %w", err)
	}
	return nil
}

// FailJob ends a job with the sentence that says why, kept until the user has seen it.
func (s *Store) FailJob(ctx context.Context, id, sentence string) error {
	if _, err := s.writer.ExecContext(ctx, `UPDATE jobs SET error = ? WHERE id = ?`, sentence, id); err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	return nil
}

// DismissJob drops one of a user's failed jobs, once it has been shown. A job still waiting is
// not the user's to drop: it may be on the server already.
func (s *Store) DismissJob(ctx context.Context, userID, id string) error {
	if !ids.Valid(ids.Job, id) {
		return Invalid("%q is not a job id.", id)
	}
	res, err := s.writer.ExecContext(ctx,
		`DELETE FROM jobs WHERE id = ? AND user_id = ? AND error <> ''`, id, userID)
	if err != nil {
		return fmt.Errorf("dismiss job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such failed job.")
	}
	return nil
}

// SweepFailedJobs drops failed jobs nobody came back to see within a day.
func (s *Store) SweepFailedJobs(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx,
		`DELETE FROM jobs WHERE error <> '' AND created_at < ?`, unix(s.Now().Add(-24*time.Hour)))
	if err != nil {
		return 0, fmt.Errorf("sweep failed jobs: %w", err)
	}
	return res.RowsAffected()
}

// JobTarget is a job's email config as the mirror is asked about it.
func (s *Store) JobTarget(ctx context.Context, configID string) (SyncTarget, error) {
	var (
		t       = SyncTarget{ID: configID}
		updated int64
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT user_id, updated_at FROM email_configs WHERE id = ?`, configID).Scan(&t.UserID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return t, NotFound("There is no such email config.")
	}
	if err != nil {
		return t, fmt.Errorf("job target: %w", err)
	}
	t.UpdatedAt = fromUnix(updated)
	return t, nil
}

// MirrorMailbox is one mailbox by id, for the mirror, which acts for the mailbox's user.
func (s *Store) MirrorMailbox(ctx context.Context, id string) (*Mailbox, error) {
	m, err := scanMailbox(s.reader.QueryRowContext(ctx,
		`SELECT `+mailboxColumns+` FROM mailboxes WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("There is no such mailbox.")
	}
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	return m, nil
}
