package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"emguio/internal/ids"
)

// What a transfer has come to.
const (
	TransferMoving    = "moving"
	TransferDone      = "done"
	TransferCancelled = "cancelled"
	TransferStopped   = "stopped"
)

// Transfer is a move of messages to a mailbox of another of the user's email configs, one
// transfer job per message, and how far it has got. See docs/reading.md.
type Transfer struct {
	ID              string
	UserID          string
	EmailConfigID   string
	MailboxID       string
	TargetConfigID  string
	TargetMailboxID string
	Total           int
	Moved           int
	Cancelled       bool
	// Error is the sentence a server stopped it with.
	Error string
	// Waiting is how many of its messages are still to go.
	Waiting   int
	CreatedAt time.Time
}

// State is what it has come to: moving while messages wait, else stopped, cancelled or done.
func (t *Transfer) State() string {
	switch {
	case t.Error != "":
		return TransferStopped
	case t.Cancelled:
		return TransferCancelled
	case t.Waiting > 0:
		return TransferMoving
	}
	return TransferDone
}

// What a cancelled transfer's messages that did not go end with.
const cancelledSentence = "Cancelled."

const transferColumns = `t.id, t.user_id, t.email_config_id, t.mailbox_id, t.target_config_id,
  t.target_mailbox_id, t.total, t.moved, t.cancelled, t.error, t.created_at,
  (SELECT COUNT(*) FROM jobs j WHERE j.transfer = t.id AND j.error = '')`

func scanTransfer(sc interface{ Scan(...any) error }) (*Transfer, error) {
	var (
		t       Transfer
		created int64
	)
	if err := sc.Scan(&t.ID, &t.UserID, &t.EmailConfigID, &t.MailboxID, &t.TargetConfigID, &t.TargetMailboxID,
		&t.Total, &t.Moved, &t.Cancelled, &t.Error, &created, &t.Waiting); err != nil {
		return nil, err
	}
	t.CreatedAt = fromUnix(created)
	return &t, nil
}

// addTransfers makes one Transfer for each mailbox and target the transfer jobs among jobs go
// from and to, and names it in each of them.
func (s *Store) addTransfers(ctx context.Context, tx *sql.Tx, jobs []Job, now time.Time) error {
	made := map[string]*Transfer{}
	var order []*Transfer
	for i := range jobs {
		j := &jobs[i]
		if j.Kind != JobTransfer || j.Transfer != "" {
			continue
		}
		key := j.MailboxID + " " + j.Target
		t := made[key]
		if t == nil {
			var to string
			err := tx.QueryRowContext(ctx, `SELECT email_config_id FROM mailboxes WHERE id = ?`, j.Target).Scan(&to)
			if errors.Is(err, sql.ErrNoRows) {
				return NotFound("There is no such mailbox.")
			}
			if err != nil {
				return fmt.Errorf("add transfer: %w", err)
			}
			t = &Transfer{
				ID: ids.New(ids.Transfer, now.UnixMilli()), UserID: j.UserID, EmailConfigID: j.EmailConfigID,
				MailboxID: j.MailboxID, TargetConfigID: to, TargetMailboxID: j.Target, CreatedAt: now,
			}
			made[key] = t
			order = append(order, t)
		}
		t.Total++
		j.Transfer = t.ID
	}
	for _, t := range order {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO transfers (id, user_id, email_config_id, mailbox_id, target_config_id, target_mailbox_id, total, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			t.ID, t.UserID, t.EmailConfigID, t.MailboxID, t.TargetConfigID, t.TargetMailboxID, t.Total, unix(t.CreatedAt)); err != nil {
			return fmt.Errorf("add transfer: %w", err)
		}
	}
	return nil
}

// Transfers is a user's transfers, moving and finished, oldest first.
func (s *Store) Transfers(ctx context.Context, userID string) ([]*Transfer, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT `+transferColumns+` FROM transfers t WHERE t.user_id = ? ORDER BY t.rowid`, userID)
	if err != nil {
		return nil, fmt.Errorf("transfers: %w", err)
	}
	defer rows.Close()
	out := []*Transfer{}
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("transfers: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Transfer is one of a user's transfers.
func (s *Store) Transfer(ctx context.Context, userID, id string) (*Transfer, error) {
	if !ids.Valid(ids.Transfer, id) {
		return nil, Invalid("%q is not a transfer id.", id)
	}
	t, err := scanTransfer(s.reader.QueryRowContext(ctx,
		`SELECT `+transferColumns+` FROM transfers t WHERE t.id = ? AND t.user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("There is no such transfer.")
	}
	if err != nil {
		return nil, fmt.Errorf("transfer: %w", err)
	}
	return t, nil
}

// TransferMoved counts one more of a transfer's messages moved.
func (s *Store) TransferMoved(ctx context.Context, id string) error {
	if _, err := s.writer.ExecContext(ctx, `UPDATE transfers SET moved = moved + 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("transfer moved: %w", err)
	}
	return nil
}

// CancelTransfer leaves where they are the messages of a transfer still waiting; the one under
// way finishes. A transfer that has finished stays as it ended.
func (s *Store) CancelTransfer(ctx context.Context, userID, id string) (*Transfer, error) {
	t, err := s.Transfer(ctx, userID, id)
	if err != nil || t.State() != TransferMoving {
		return t, err
	}
	if err := s.endTransfer(ctx, id, "cancelled = 1", cancelledSentence); err != nil {
		return nil, err
	}
	return s.Transfer(ctx, userID, id)
}

// StopTransfer ends a transfer with the sentence a server refused it in, leaving where they are
// the messages still waiting.
func (s *Store) StopTransfer(ctx context.Context, id, sentence string) error {
	return s.endTransfer(ctx, id, "error = ?", sentence, sentence)
}

// RetryTransfer sends again the messages a stopped or cancelled transfer left where they were,
// as if asked now: after whatever was asked since. One still moving stays as it is.
func (s *Store) RetryTransfer(ctx context.Context, userID, id string) (*Transfer, error) {
	t, err := s.Transfer(ctx, userID, id)
	if err != nil || t.State() == TransferMoving {
		return t, err
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE transfer = ? AND error <> '' ORDER BY rowid`, id)
	if err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	var left []*Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("retry transfer: %w", err)
		}
		left = append(left, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	if len(left) == 0 {
		return nil, Invalid("Nothing is left to move.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE transfer = ? AND error <> ''`, id); err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	now := s.Now()
	for _, j := range left {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO jobs (`+jobColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, '', ?)`,
			ids.New(ids.Job, now.UnixMilli()), j.UserID, j.EmailConfigID, j.MailboxID, j.Message, j.Kind, j.Value,
			j.Target, j.Transfer, j.Seen, unix(now), unix(now)); err != nil {
			return nil, fmt.Errorf("retry transfer: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE transfers SET error = '', cancelled = 0 WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("retry transfer: %w", err)
	}
	return s.Transfer(ctx, userID, id)
}

// endTransfer marks a transfer ended, and its waiting jobs with sentence: ended, they are no
// longer done, and a page draws their messages where they are again.
func (s *Store) endTransfer(ctx context.Context, id, set, sentence string, args ...any) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("end transfer: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE transfers SET `+set+` WHERE id = ?`, append(args, id)...); err != nil {
		return fmt.Errorf("end transfer: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET error = ? WHERE transfer = ? AND error = ''`, sentence, id); err != nil {
		return fmt.Errorf("end transfer: %w", err)
	}
	return tx.Commit()
}

// DismissTransfer drops a finished transfer, and the jobs it left, once it has been shown.
func (s *Store) DismissTransfer(ctx context.Context, userID, id string) error {
	t, err := s.Transfer(ctx, userID, id)
	if err != nil {
		return err
	}
	if t.State() == TransferMoving {
		return Invalid("It is still moving: cancel it first.")
	}
	return s.dropTransfer(ctx, id)
}

func (s *Store) dropTransfer(ctx context.Context, id string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("drop transfer: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM jobs WHERE transfer = ?`, id); err != nil {
		return fmt.Errorf("drop transfer: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transfers WHERE id = ?`, id); err != nil {
		return fmt.Errorf("drop transfer: %w", err)
	}
	return tx.Commit()
}

// SweepTransfers drops finished transfers nobody came back to see within a day.
func (s *Store) SweepTransfers(ctx context.Context) (int64, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT t.id FROM transfers t WHERE t.created_at < ?
		   AND NOT EXISTS (SELECT 1 FROM jobs j WHERE j.transfer = t.id AND j.error = '')`,
		unix(s.Now().Add(-24*time.Hour)))
	if err != nil {
		return 0, fmt.Errorf("sweep transfers: %w", err)
	}
	var old []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("sweep transfers: %w", err)
		}
		old = append(old, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("sweep transfers: %w", err)
	}
	for _, id := range old {
		if err := s.dropTransfer(ctx, id); err != nil {
			return 0, err
		}
	}
	return int64(len(old)), nil
}

// MailboxOf is a mailbox of any of a user's email configs, by id.
func (s *Store) MailboxOf(ctx context.Context, userID, mailboxID string) (*Mailbox, error) {
	if !ids.Valid(ids.Mailbox, mailboxID) {
		return nil, Invalid("%q is not a mailbox id.", mailboxID)
	}
	m, err := scanMailbox(s.reader.QueryRowContext(ctx,
		`SELECT `+mailboxColumns+` FROM mailboxes
		  WHERE id = ? AND email_config_id IN (SELECT id FROM email_configs WHERE user_id = ?)`, mailboxID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("There is no such mailbox.")
	}
	if err != nil {
		return nil, fmt.Errorf("mailbox: %w", err)
	}
	return m, nil
}
