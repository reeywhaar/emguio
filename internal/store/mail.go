package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"emguio/internal/ids"
)

// Special uses a mailbox can have, from the server's own flags or guessed from its name.
const (
	UseInbox   = "inbox"
	UseDrafts  = "drafts"
	UseSent    = "sent"
	UseArchive = "archive"
	UseJunk    = "junk"
	UseTrash   = "trash"
	UseAll     = "all"
	UseFlagged = "flagged"
)

// Mailbox is a folder on an email config's incoming server.
type Mailbox struct {
	ID            string
	EmailConfigID string
	// Name is the server's, decoded, and is what goes back to it.
	Name       string
	Delimiter  string
	SpecialUse string
	// Selectable is false for a mailbox that only holds others.
	Selectable bool
	// What the server said at the last look, zero before the first.
	UIDValidity uint32
	UIDNext     uint32
	Messages    uint32
	Unseen      uint32
	SyncedAt    *time.Time
}

// Path is the name split at the server's delimiter: where the mailbox sits in the tree.
func (m *Mailbox) Path() []string {
	if m.Delimiter == "" {
		return []string{m.Name}
	}
	return strings.Split(m.Name, m.Delimiter)
}

// Listed is a mailbox as the server's LIST names it.
type Listed struct {
	Name       string
	Delimiter  string
	SpecialUse string
	Selectable bool
}

// MailboxStatus is what the server said about a mailbox's contents at the last look.
type MailboxStatus struct {
	UIDValidity uint32
	UIDNext     uint32
	Messages    uint32
	Unseen      uint32
}

// Flags are the ones a list shows. Others the server holds are not copied.
type Flags struct {
	Seen     bool
	Flagged  bool
	Answered bool
	Draft    bool
}

// Address is somebody in a header.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Header is a message as a list knows it: flags, who, what, and when.
type Header struct {
	UID          uint32
	Flags        Flags
	InternalDate time.Time
	// Date is the Date header, nil when the message has none.
	Date           *time.Time
	Size           int64
	Subject        string
	From           Address
	To             []Address
	Cc             []Address
	MessageID      string
	InReplyTo      string
	HasAttachments bool
	// Preview is the start of the text, on one line, empty until something has read it.
	Preview string
}

// Message is a stored message, named by its own id.
type Message struct {
	ID string
	Header
}

// SyncTarget is an email config as the mirror sees it: whose it is, and when it last changed.
type SyncTarget struct {
	ID        string
	UserID    string
	UpdatedAt time.Time
}

// SyncTargets lists every email config on the instance. The mirror acts for each one's user
// without a request from them, which is the one reason to read across users.
func (s *Store) SyncTargets(ctx context.Context) ([]SyncTarget, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT id, user_id, updated_at FROM email_configs`)
	if err != nil {
		return nil, fmt.Errorf("sync targets: %w", err)
	}
	defer rows.Close()
	var out []SyncTarget
	for rows.Next() {
		var (
			t       SyncTarget
			updated int64
		)
		if err := rows.Scan(&t.ID, &t.UserID, &updated); err != nil {
			return nil, fmt.Errorf("sync targets: %w", err)
		}
		t.UpdatedAt = fromUnix(updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

// TargetLogins is what the mirror dials an email config with: the config as saved, passwords
// included.
func (s *Store) TargetLogins(ctx context.Context, t SyncTarget) (*Logins, error) {
	r, err := s.emailConfigRow(ctx, t.UserID, t.ID)
	if err != nil {
		return nil, err
	}
	in := EmailConfigInput{Name: r.Name, Email: r.Email, Incoming: Login{Server: r.Incoming}}
	if r.Outgoing != nil {
		in.Outgoing = &Login{Server: *r.Outgoing}
	}
	return s.Logins(ctx, t.UserID, t.ID, in)
}

// SetSyncState records how the latest attempt to bring an email config up to date went:
// syncErr empty is success, and moves synced_at.
func (s *Store) SetSyncState(ctx context.Context, id, syncErr string) error {
	var err error
	if syncErr == "" {
		_, err = s.writer.ExecContext(ctx,
			`UPDATE email_configs SET synced_at = ?, sync_error = '' WHERE id = ?`, unix(s.Now()), id)
	} else {
		_, err = s.writer.ExecContext(ctx,
			`UPDATE email_configs SET sync_error = ? WHERE id = ?`, syncErr, id)
	}
	if err != nil {
		return fmt.Errorf("sync state: %w", err)
	}
	return nil
}

const mailboxColumns = `id, email_config_id, name, delimiter, special_use, selectable,
  uidvalidity, uidnext, messages, unseen, synced_at`

func scanMailbox(sc interface{ Scan(...any) error }) (*Mailbox, error) {
	var (
		m      Mailbox
		synced sql.NullInt64
	)
	if err := sc.Scan(&m.ID, &m.EmailConfigID, &m.Name, &m.Delimiter, &m.SpecialUse, &m.Selectable,
		&m.UIDValidity, &m.UIDNext, &m.Messages, &m.Unseen, &synced); err != nil {
		return nil, err
	}
	if synced.Valid {
		at := fromUnix(synced.Int64)
		m.SyncedAt = &at
	}
	return &m, nil
}

func (s *Store) queryMailboxes(ctx context.Context, query string, args ...any) ([]*Mailbox, error) {
	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("mailboxes: %w", err)
	}
	defer rows.Close()
	var out []*Mailbox
	for rows.Next() {
		m, err := scanMailbox(rows)
		if err != nil {
			return nil, fmt.Errorf("mailboxes: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	SortMailboxes(out)
	return out, nil
}

// Mailboxes lists one of a user's email configs' mailboxes, in the order a sidebar shows them.
func (s *Store) Mailboxes(ctx context.Context, userID, configID string) ([]*Mailbox, error) {
	if _, err := s.emailConfigRow(ctx, userID, configID); err != nil {
		return nil, err
	}
	return s.queryMailboxes(ctx, `SELECT `+mailboxColumns+` FROM mailboxes WHERE email_config_id = ?`, configID)
}

// MirrorMailboxes is Mailboxes for the mirror, which acts for the config's user.
func (s *Store) MirrorMailboxes(ctx context.Context, configID string) ([]*Mailbox, error) {
	return s.queryMailboxes(ctx, `SELECT `+mailboxColumns+` FROM mailboxes WHERE email_config_id = ?`, configID)
}

// PutMailboxes makes the stored mailboxes the listed ones: new names added, the attributes of
// known ones updated, and the ones the server no longer has deleted with their messages. It
// reports whether anything a sidebar shows moved.
func (s *Store) PutMailboxes(ctx context.Context, configID string, listed []Listed) (bool, error) {
	known, err := s.MirrorMailboxes(ctx, configID)
	if err != nil {
		return false, err
	}
	byName := map[string]*Mailbox{}
	for _, m := range known {
		byName[m.Name] = m
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("put mailboxes: %w", err)
	}
	defer tx.Rollback()

	changed := false
	seen := map[string]bool{}
	now := s.Now()
	for _, l := range listed {
		if seen[l.Name] {
			continue
		}
		seen[l.Name] = true
		m := byName[l.Name]
		switch {
		case m == nil:
			_, err = tx.ExecContext(ctx,
				`INSERT INTO mailboxes (id, email_config_id, name, delimiter, special_use, selectable)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				ids.New(ids.Mailbox, now.UnixMilli()), configID, l.Name, l.Delimiter, l.SpecialUse, l.Selectable)
			changed = true
		case m.Delimiter != l.Delimiter || m.SpecialUse != l.SpecialUse || m.Selectable != l.Selectable:
			_, err = tx.ExecContext(ctx,
				`UPDATE mailboxes SET delimiter = ?, special_use = ?, selectable = ? WHERE id = ?`,
				l.Delimiter, l.SpecialUse, l.Selectable, m.ID)
			changed = true
		}
		if err != nil {
			return false, fmt.Errorf("put mailboxes: %w", err)
		}
	}
	for _, m := range known {
		if seen[m.Name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mailboxes WHERE id = ?`, m.ID); err != nil {
			return false, fmt.Errorf("put mailboxes: %w", err)
		}
		changed = true
	}
	return changed, tx.Commit()
}

// SetMailboxStatus records what the server said about a mailbox at a look that has been acted on.
func (s *Store) SetMailboxStatus(ctx context.Context, mailboxID string, st MailboxStatus) error {
	_, err := s.writer.ExecContext(ctx,
		`UPDATE mailboxes SET uidvalidity = ?, uidnext = ?, messages = ?, unseen = ?, synced_at = ? WHERE id = ?`,
		st.UIDValidity, st.UIDNext, st.Messages, st.Unseen, unix(s.Now()), mailboxID)
	if err != nil {
		return fmt.Errorf("mailbox status: %w", err)
	}
	return nil
}

// ResetMailbox drops every message of a mailbox whose UIDVALIDITY changed: the server has said
// its UIDs now name other messages, so none of the stored rows can be matched to anything.
func (s *Store) ResetMailbox(ctx context.Context, mailboxID string, uidValidity uint32) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("reset mailbox: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id = ?`, mailboxID); err != nil {
		return fmt.Errorf("reset mailbox: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE mailboxes SET uidvalidity = ?, uidnext = 0, messages = 0, unseen = 0, synced_at = NULL WHERE id = ?`,
		uidValidity, mailboxID); err != nil {
		return fmt.Errorf("reset mailbox: %w", err)
	}
	return tx.Commit()
}

// MessageFlags is every stored message of a mailbox, by UID, with the flags last copied.
func (s *Store) MessageFlags(ctx context.Context, mailboxID string) (map[uint32]Flags, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT uid, seen, flagged, answered, draft FROM messages WHERE mailbox_id = ?`, mailboxID)
	if err != nil {
		return nil, fmt.Errorf("message flags: %w", err)
	}
	defer rows.Close()
	out := map[uint32]Flags{}
	for rows.Next() {
		var (
			uid uint32
			f   Flags
		)
		if err := rows.Scan(&uid, &f.Seen, &f.Flagged, &f.Answered, &f.Draft); err != nil {
			return nil, fmt.Errorf("message flags: %w", err)
		}
		out[uid] = f
	}
	return out, rows.Err()
}

// PutMessages stores new messages, in one transaction. One already stored under a UID only has
// its flags updated: a UID names one message for as long as the UIDVALIDITY lasts.
func (s *Store) PutMessages(ctx context.Context, mailboxID string, headers []Header) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put messages: %w", err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (id, mailbox_id, uid, internal_date, date, size, seen, flagged, answered, draft,
		   subject, from_name, from_email, to_json, cc_json, message_id, in_reply_to, has_attachments, preview)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (mailbox_id, uid) DO UPDATE SET
		   seen = excluded.seen, flagged = excluded.flagged, answered = excluded.answered, draft = excluded.draft`)
	if err != nil {
		return fmt.Errorf("put messages: %w", err)
	}
	defer stmt.Close()
	now := s.Now()
	for _, h := range headers {
		var date any
		if h.Date != nil {
			date = unix(*h.Date)
		}
		to, _ := json.Marshal(nonNil(h.To))
		cc, _ := json.Marshal(nonNil(h.Cc))
		if _, err := stmt.ExecContext(ctx,
			ids.New(ids.Message, now.UnixMilli()), mailboxID, h.UID, unix(h.InternalDate), date, h.Size,
			h.Flags.Seen, h.Flags.Flagged, h.Flags.Answered, h.Flags.Draft,
			h.Subject, h.From.Name, h.From.Email, string(to), string(cc), h.MessageID, h.InReplyTo, h.HasAttachments, h.Preview); err != nil {
			return fmt.Errorf("put messages: %w", err)
		}
	}
	return tx.Commit()
}

func nonNil(a []Address) []Address {
	if a == nil {
		return []Address{}
	}
	return a
}

// SetFlags updates the flags of stored messages, in one transaction.
func (s *Store) SetFlags(ctx context.Context, mailboxID string, flags map[uint32]Flags) error {
	if len(flags) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set flags: %w", err)
	}
	defer tx.Rollback()
	for uid, f := range flags {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET seen = ?, flagged = ?, answered = ?, draft = ? WHERE mailbox_id = ? AND uid = ?`,
			f.Seen, f.Flagged, f.Answered, f.Draft, mailboxID, uid); err != nil {
			return fmt.Errorf("set flags: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteMessages drops the messages the server no longer has.
func (s *Store) DeleteMessages(ctx context.Context, mailboxID string, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("delete messages: %w", err)
	}
	defer tx.Rollback()
	for _, uid := range uids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, uid); err != nil {
			return fmt.Errorf("delete messages: %w", err)
		}
	}
	return tx.Commit()
}

// MessagePage is a run of a mailbox's messages, newest first, and where the next one starts.
type MessagePage struct {
	Messages []*Message
	// Next is empty when this run reached the oldest message.
	Next string
}

// PageMax bounds a run, whatever was asked for.
const PageMax = 200

// Messages lists a run of one of a user's mailboxes, newest to arrive first, starting after
// cursor — empty for the newest.
//
// A cursor is a position rather than an offset, so mail arriving between two pages does not
// shift the second by however many came in.
func (s *Store) Messages(ctx context.Context, userID, configID, mailboxID, cursor string, limit int) (*MessagePage, error) {
	if _, err := s.emailConfigRow(ctx, userID, configID); err != nil {
		return nil, err
	}
	if !ids.Valid(ids.Mailbox, mailboxID) {
		return nil, Invalid("%q is not a mailbox id.", mailboxID)
	}
	var n int
	if err := s.reader.QueryRowContext(ctx,
		`SELECT count(*) FROM mailboxes WHERE id = ? AND email_config_id = ?`, mailboxID, configID).Scan(&n); err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	if n == 0 {
		return nil, NotFound("There is no such mailbox.")
	}
	if limit <= 0 || limit > PageMax {
		limit = PageMax
	}

	query := `SELECT id, uid, internal_date, date, size, seen, flagged, answered, draft,
	            subject, from_name, from_email, to_json, cc_json, message_id, in_reply_to, has_attachments, preview
	          FROM messages WHERE mailbox_id = ?`
	args := []any{mailboxID}
	if cursor != "" {
		at, uid, err := parseCursor(cursor)
		if err != nil {
			return nil, err
		}
		query += ` AND (internal_date < ? OR (internal_date = ? AND uid < ?))`
		args = append(args, at, at, uid)
	}
	query += ` ORDER BY internal_date DESC, uid DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("messages: %w", err)
	}
	defer rows.Close()
	page := &MessagePage{Messages: []*Message{}}
	for rows.Next() {
		var (
			m        Message
			internal int64
			date     sql.NullInt64
			to, cc   string
		)
		if err := rows.Scan(&m.ID, &m.UID, &internal, &date, &m.Size,
			&m.Flags.Seen, &m.Flags.Flagged, &m.Flags.Answered, &m.Flags.Draft,
			&m.Subject, &m.From.Name, &m.From.Email, &to, &cc, &m.MessageID, &m.InReplyTo, &m.HasAttachments, &m.Preview); err != nil {
			return nil, fmt.Errorf("messages: %w", err)
		}
		m.InternalDate = fromUnix(internal)
		if date.Valid {
			d := fromUnix(date.Int64)
			m.Date = &d
		}
		json.Unmarshal([]byte(to), &m.To)
		json.Unmarshal([]byte(cc), &m.Cc)
		page.Messages = append(page.Messages, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Messages) > limit {
		page.Messages = page.Messages[:limit]
		last := page.Messages[limit-1]
		page.Next = makeCursor(unix(last.InternalDate), last.UID)
	}
	return page, nil
}

func makeCursor(at int64, uid uint32) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d.%d", at, uid)))
}

func parseCursor(cursor string) (int64, uint32, error) {
	bad := Invalid("That cursor is not one this gave out. Start the list again.")
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, 0, bad
	}
	at, uid, ok := strings.Cut(string(raw), ".")
	if !ok {
		return 0, 0, bad
	}
	a, err1 := strconv.ParseInt(at, 10, 64)
	u, err2 := strconv.ParseUint(uid, 10, 32)
	if err1 != nil || err2 != nil {
		return 0, 0, bad
	}
	return a, uint32(u), nil
}

// useOrder is where each special use sits in a sidebar; anything else comes after, by name.
var useOrder = map[string]int{
	UseInbox: 0, UseDrafts: 1, UseSent: 2, UseArchive: 3, UseAll: 4, UseFlagged: 5, UseJunk: 6, UseTrash: 7,
}

// SortMailboxes puts mailboxes in the order a sidebar shows them: the special ones first in a
// fixed order, each followed by the mailboxes inside it, then the rest by path.
//
// By path rather than by name, so a parent is followed by its children whatever characters
// their names happen to sort by.
func SortMailboxes(list []*Mailbox) {
	rootUse := map[string]string{}
	for _, m := range list {
		if len(m.Path()) == 1 {
			rootUse[m.Name] = m.SpecialUse
		}
	}
	rank := func(m *Mailbox) int {
		use := rootUse[m.Path()[0]]
		if len(m.Path()) == 1 {
			use = m.SpecialUse
		}
		if r, ok := useOrder[use]; ok {
			return r
		}
		return len(useOrder)
	}
	slices.SortStableFunc(list, func(a, b *Mailbox) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return slices.CompareFunc(a.Path(), b.Path(), func(x, y string) int {
			return strings.Compare(strings.ToLower(x), strings.ToLower(y))
		})
	})
}

// Opened is a message somebody asked to read: the message, and where it is on the server.
type Opened struct {
	Message
	MailboxID   string
	MailboxName string
	SpecialUse  string
	UIDValidity uint32
}

// OpenMessage finds one of a user's messages under one of their email configs.
func (s *Store) OpenMessage(ctx context.Context, userID, configID, messageID string) (*Opened, error) {
	if _, err := s.emailConfigRow(ctx, userID, configID); err != nil {
		return nil, err
	}
	if !ids.Valid(ids.Message, messageID) {
		return nil, Invalid("%q is not a message id.", messageID)
	}
	var (
		o        Opened
		internal int64
		date     sql.NullInt64
		to, cc   string
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT m.id, m.uid, m.internal_date, m.date, m.size, m.seen, m.flagged, m.answered, m.draft,
		        m.subject, m.from_name, m.from_email, m.to_json, m.cc_json, m.message_id, m.in_reply_to,
		        m.has_attachments, m.preview, b.id, b.name, b.special_use, b.uidvalidity
		   FROM messages m JOIN mailboxes b ON b.id = m.mailbox_id
		  WHERE m.id = ? AND b.email_config_id = ?`, messageID, configID).
		Scan(&o.ID, &o.UID, &internal, &date, &o.Size,
			&o.Flags.Seen, &o.Flags.Flagged, &o.Flags.Answered, &o.Flags.Draft,
			&o.Subject, &o.From.Name, &o.From.Email, &to, &cc, &o.MessageID, &o.InReplyTo,
			&o.HasAttachments, &o.Preview, &o.MailboxID, &o.MailboxName, &o.SpecialUse, &o.UIDValidity)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("There is no such message.")
	}
	if err != nil {
		return nil, fmt.Errorf("open message: %w", err)
	}
	o.InternalDate = fromUnix(internal)
	if date.Valid {
		d := fromUnix(date.Int64)
		o.Date = &d
	}
	json.Unmarshal([]byte(to), &o.To)
	json.Unmarshal([]byte(cc), &o.Cc)
	return &o, nil
}

// Body is a message as the server sent it, when it has been kept.
func (s *Store) Body(ctx context.Context, messageID string) ([]byte, bool, error) {
	var raw []byte
	err := s.reader.QueryRowContext(ctx, `SELECT raw FROM message_bodies WHERE message_id = ?`, messageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("body: %w", err)
	}
	return raw, true, nil
}

// PutBody keeps a message as the server sent it, with the preview it gives the list.
func (s *Store) PutBody(ctx context.Context, messageID string, raw []byte, preview string) error {
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("put body: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO message_bodies (message_id, raw, fetched_at) VALUES (?, ?, ?)
		 ON CONFLICT (message_id) DO UPDATE SET raw = excluded.raw, fetched_at = excluded.fetched_at`,
		messageID, raw, unix(s.Now())); err != nil {
		return fmt.Errorf("put body: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET preview = ? WHERE id = ?`, preview, messageID); err != nil {
		return fmt.Errorf("put body: %w", err)
	}
	return tx.Commit()
}

// SetPreview replaces a message's preview, when reading it gives a better one than it has.
func (s *Store) SetPreview(ctx context.Context, messageID, preview string) error {
	_, err := s.writer.ExecContext(ctx, `UPDATE messages SET preview = ? WHERE id = ?`, preview, messageID)
	if err != nil {
		return fmt.Errorf("set preview: %w", err)
	}
	return nil
}
