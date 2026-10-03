package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"emguio/internal/ids"
)

// Draft is what somebody is writing, kept here between saves until it is written to the mail
// server's Drafts. See docs/sending.md.
type Draft struct {
	ID            string
	UserID        string
	EmailConfigID string
	DraftFields
	// ReplyMailbox and ReplyMessage are the message it answers, empty for none.
	ReplyMailbox string
	ReplyMessage string
	// InReplyTo and References thread it under what it answers.
	InReplyTo  string
	References []string
	// KeptMailbox and KeptMessage are where the mail server holds it as last written.
	KeptMailbox string
	KeptMessage string
	// Version counts its saves, and Written is the one last written to the mail server.
	Version  int
	Written  int
	DueAt    time.Time
	Attempts int
	// Closed is its window closed: it goes from here once written.
	Closed bool
	// Problem is why it could not be written, the last time it was tried.
	Problem   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DraftFields is what is typed into a draft.
type DraftFields struct {
	To, Cc, Bcc string
	Subject     string
	Text        string
}

// DraftPart is a file a draft carries. Data is read only where it is asked for.
type DraftPart struct {
	ID   string
	Name string
	Type string
	Size int
	Data []byte
}

const draftColumns = `id, user_id, email_config_id, to_field, cc_field, bcc_field, subject, body,
  reply_mailbox, reply_message, in_reply_to, refs, kept_mailbox, kept_message, version, written,
  due_at, attempts, closed, problem, created_at, updated_at`

func scanDraft(sc interface{ Scan(...any) error }) (*Draft, error) {
	var (
		d                     Draft
		refs                  string
		due, created, updated int64
	)
	if err := sc.Scan(&d.ID, &d.UserID, &d.EmailConfigID, &d.To, &d.Cc, &d.Bcc, &d.Subject, &d.Text,
		&d.ReplyMailbox, &d.ReplyMessage, &d.InReplyTo, &refs, &d.KeptMailbox, &d.KeptMessage,
		&d.Version, &d.Written, &due, &d.Attempts, &d.Closed, &d.Problem, &created, &updated); err != nil {
		return nil, err
	}
	d.References = strings.Fields(refs)
	if due > 0 {
		d.DueAt = fromUnix(due)
	}
	d.CreatedAt, d.UpdatedAt = fromUnix(created), fromUnix(updated)
	return &d, nil
}

var errNoDraft = NotFound("This draft is no longer here.")

// CreateDraft keeps a new draft, with parts, due to be written at due.
func (s *Store) CreateDraft(ctx context.Context, d Draft, parts []DraftPart, due time.Time) (*Draft, []DraftPart, error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create draft: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	d.ID, d.Version, d.DueAt, d.CreatedAt, d.UpdatedAt = ids.New(ids.Draft, now.UnixMilli()), 1, due, now, now
	if _, err := tx.ExecContext(ctx, `INSERT INTO drafts (id, user_id, email_config_id, to_field,
  cc_field, bcc_field, subject, body, reply_mailbox, reply_message, in_reply_to, refs,
  kept_mailbox, kept_message, due_at, created_at, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.UserID, d.EmailConfigID, d.To, d.Cc, d.Bcc, d.Subject, d.Text, d.ReplyMailbox,
		d.ReplyMessage, d.InReplyTo, strings.Join(d.References, " "), d.KeptMailbox, d.KeptMessage,
		unix(due), unix(now), unix(now)); err != nil {
		return nil, nil, fmt.Errorf("create draft: %w", err)
	}
	out, err := addParts(ctx, tx, d.ID, 0, parts, now)
	if err != nil {
		return nil, nil, err
	}
	return &d, out, tx.Commit()
}

// SaveDraft keeps what is typed into one of a user's drafts now, the parts named by keep in
// that order followed by add, and makes it due to be written by due unless it already is
// sooner. A closed draft is due now.
func (s *Store) SaveDraft(ctx context.Context, userID, id string, f DraftFields, keep []string, add []DraftPart, due time.Time, closed bool) (*Draft, []DraftPart, error) {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("save draft: %w", err)
	}
	defer tx.Rollback()
	now := s.Now()
	if closed {
		due = now
	}
	res, err := tx.ExecContext(ctx, `UPDATE drafts SET to_field = ?, cc_field = ?, bcc_field = ?,
  subject = ?, body = ?, version = version + 1, closed = ?, updated_at = ?,
  due_at = CASE WHEN due_at > 0 AND due_at < ? THEN due_at ELSE ? END
  WHERE id = ? AND user_id = ?`,
		f.To, f.Cc, f.Bcc, f.Subject, f.Text, closed, unix(now), unix(due), unix(due), id, userID)
	if err != nil {
		return nil, nil, fmt.Errorf("save draft: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil, errNoDraft
	}

	held, err := partsOf(ctx, tx, id)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]DraftPart{}
	for _, p := range held {
		byID[p.ID] = p
	}
	var out []DraftPart
	for i, pid := range keep {
		p, ok := byID[pid]
		if !ok {
			return nil, nil, Invalid("One of its attachments is no longer here.")
		}
		delete(byID, pid)
		if _, err := tx.ExecContext(ctx, `UPDATE draft_parts SET position = ? WHERE id = ?`, i, pid); err != nil {
			return nil, nil, fmt.Errorf("save draft: %w", err)
		}
		out = append(out, p)
	}
	for pid := range byID {
		if _, err := tx.ExecContext(ctx, `DELETE FROM draft_parts WHERE id = ?`, pid); err != nil {
			return nil, nil, fmt.Errorf("save draft: %w", err)
		}
	}
	added, err := addParts(ctx, tx, id, len(keep), add, now)
	if err != nil {
		return nil, nil, err
	}
	d, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM drafts WHERE id = ?`, id))
	if err != nil {
		return nil, nil, fmt.Errorf("save draft: %w", err)
	}
	return d, append(out, added...), tx.Commit()
}

func addParts(ctx context.Context, tx *sql.Tx, draftID string, from int, parts []DraftPart, now time.Time) ([]DraftPart, error) {
	out := make([]DraftPart, 0, len(parts))
	for i, p := range parts {
		p.ID, p.Size = ids.New(ids.DraftPart, now.UnixMilli()), len(p.Data)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO draft_parts (id, draft_id, position, name, type, data) VALUES (?, ?, ?, ?, ?, ?)`,
			p.ID, draftID, from+i, p.Name, p.Type, p.Data); err != nil {
			return nil, fmt.Errorf("draft parts: %w", err)
		}
		p.Data = nil
		out = append(out, p)
	}
	return out, nil
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func partsOf(ctx context.Context, q querier, draftID string) ([]DraftPart, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, name, type, length(data) FROM draft_parts WHERE draft_id = ? ORDER BY position`, draftID)
	if err != nil {
		return nil, fmt.Errorf("draft parts: %w", err)
	}
	defer rows.Close()
	var out []DraftPart
	for rows.Next() {
		var p DraftPart
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Size); err != nil {
			return nil, fmt.Errorf("draft parts: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Draft is one of a user's drafts.
func (s *Store) Draft(ctx context.Context, userID, id string) (*Draft, error) {
	d, err := s.MirrorDraft(ctx, id)
	if err == nil && d.UserID != userID {
		return nil, errNoDraft
	}
	return d, err
}

// MirrorDraft is a draft by id, for the mirror, which acts for its user.
func (s *Store) MirrorDraft(ctx context.Context, id string) (*Draft, error) {
	d, err := scanDraft(s.reader.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM drafts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNoDraft
	}
	if err != nil {
		return nil, fmt.Errorf("draft: %w", err)
	}
	return d, nil
}

// DraftParts is a draft's parts in order, with their bytes where withData asks.
func (s *Store) DraftParts(ctx context.Context, draftID string, withData bool) ([]DraftPart, error) {
	if !withData {
		return partsOf(ctx, s.reader, draftID)
	}
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, name, type, data FROM draft_parts WHERE draft_id = ? ORDER BY position`, draftID)
	if err != nil {
		return nil, fmt.Errorf("draft parts: %w", err)
	}
	defer rows.Close()
	var out []DraftPart
	for rows.Next() {
		var p DraftPart
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Data); err != nil {
			return nil, fmt.Errorf("draft parts: %w", err)
		}
		p.Size = len(p.Data)
		out = append(out, p)
	}
	return out, rows.Err()
}

// NextDraft is the draft due soonest, due or not yet; nil for none.
func (s *Store) NextDraft(ctx context.Context) (*Draft, error) {
	d, err := scanDraft(s.reader.QueryRowContext(ctx,
		`SELECT `+draftColumns+` FROM drafts WHERE due_at > 0 ORDER BY due_at LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("next draft: %w", err)
	}
	return d, nil
}

// DraftWritten records version of a draft written to the mail server, held there now as
// message in mailbox. A closed draft written as it last was goes from here; one saved since is
// due again, by next.
func (s *Store) DraftWritten(ctx context.Context, id string, version int, mailbox, message string, next time.Time) error {
	if _, err := s.writer.ExecContext(ctx,
		`DELETE FROM drafts WHERE id = ? AND closed = 1 AND version = ?`, id, version); err != nil {
		return fmt.Errorf("draft written: %w", err)
	}
	_, err := s.writer.ExecContext(ctx, `UPDATE drafts SET written = ?, kept_mailbox = ?,
  kept_message = ?, attempts = 0, problem = '',
  due_at = CASE WHEN version = ? THEN 0 ELSE max(due_at, ?) END WHERE id = ?`,
		version, mailbox, message, version, unixOrZero(next), id)
	if err != nil {
		return fmt.Errorf("draft written: %w", err)
	}
	return nil
}

// DraftFailed records why a draft could not be written, and when to try again; zero for not
// until it is saved again.
func (s *Store) DraftFailed(ctx context.Context, id, problem string, again time.Time) error {
	_, err := s.writer.ExecContext(ctx,
		`UPDATE drafts SET problem = ?, attempts = attempts + 1, due_at = ? WHERE id = ?`,
		problem, unixOrZero(again), id)
	if err != nil {
		return fmt.Errorf("draft failed: %w", err)
	}
	return nil
}

// HoldDraft stops a draft being written while it is sent, and says it as it is.
func (s *Store) HoldDraft(ctx context.Context, userID, id string) (*Draft, error) {
	res, err := s.writer.ExecContext(ctx, `UPDATE drafts SET due_at = 0 WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return nil, fmt.Errorf("hold draft: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errNoDraft
	}
	return s.Draft(ctx, userID, id)
}

// DueDraft makes a draft due again at due, after a sending that did not go.
func (s *Store) DueDraft(ctx context.Context, id string, due time.Time) error {
	_, err := s.writer.ExecContext(ctx,
		`UPDATE drafts SET due_at = ? WHERE id = ? AND written < version`, unix(due), id)
	if err != nil {
		return fmt.Errorf("due draft: %w", err)
	}
	return nil
}

// DeleteDraft removes one of a user's drafts from here, and says what it was.
func (s *Store) DeleteDraft(ctx context.Context, userID, id string) (*Draft, error) {
	d, err := s.Draft(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM drafts WHERE id = ?`, id); err != nil {
		return nil, fmt.Errorf("delete draft: %w", err)
	}
	return d, nil
}

// SweepDrafts removes drafts nobody has saved for a week: a window left open, or one whose
// server would not take it. Written or not, by then nobody is coming back for it.
func (s *Store) SweepDrafts(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx,
		`DELETE FROM drafts WHERE updated_at < ?`, unix(s.Now().Add(-7*24*time.Hour)))
	if err != nil {
		return 0, fmt.Errorf("sweep drafts: %w", err)
	}
	return res.RowsAffected()
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return unix(t)
}
