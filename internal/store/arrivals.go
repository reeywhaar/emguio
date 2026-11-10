package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"emguio/internal/ids"
)

// SetNotifications chooses how one of a user's email configs tells their devices about new mail,
// and which of its folders do: the folders named are set, the others left as they were. An
// every of 0 keeps the interval set.
func (s *Store) SetNotifications(ctx context.Context, userID, configID, notify string, every int, folders map[string]bool) error {
	if _, err := s.emailConfigRow(ctx, userID, configID); err != nil {
		return err
	}
	switch notify {
	case NotifyNow, NotifyDigest, NotifyOff:
	default:
		return Invalid("%q is not a way to notify. Choose now, digest or off.", notify)
	}
	if every != 0 && !slices.Contains(DigestEvery, every) {
		return Invalid("A digest goes every 30, 60, 120 or 240 minutes.")
	}
	boxes, err := s.MirrorMailboxes(ctx, configID)
	if err != nil {
		return err
	}
	for id := range folders {
		switch {
		case !ids.Valid(ids.Mailbox, id):
			return Invalid("%q is not a mailbox id.", id)
		case !slices.ContainsFunc(boxes, func(mb *Mailbox) bool { return mb.ID == id }):
			return NotFound("There is no such mailbox.")
		}
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set notifications: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE email_configs SET notify = ?, digest_every = CASE WHEN ? = 0 THEN digest_every ELSE ? END
		 WHERE id = ? AND user_id = ?`, notify, every, every, configID, userID); err != nil {
		return fmt.Errorf("set notifications: %w", err)
	}
	for id, on := range folders {
		if _, err := tx.ExecContext(ctx,
			`UPDATE mailboxes SET notify = ? WHERE id = ? AND email_config_id = ?`, on, id, configID); err != nil {
			return fmt.Errorf("set notifications: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set notifications: %w", err)
	}
	// What is waiting may be due now, or not at all.
	s.wake()
	return nil
}

// Arrivals is signalled when mail is kept to be told, or how it is told changes.
func (s *Store) Arrivals() <-chan struct{} { return s.arrived }

func (s *Store) wake() {
	select {
	case s.arrived <- struct{}{}:
	default:
	}
}

// Arrived keeps mail that has just come into a mailbox, to be told to its user's devices: while
// its email config notifies, the folder does, and the user has a device to tell. A message kept
// for the config already, by its Message-ID, is not kept again: one message can be filed in two
// folders at once. It reports how many it kept.
func (s *Store) Arrived(ctx context.Context, mb *Mailbox, headers []Header) (int, error) {
	if len(headers) == 0 || !mb.Notifies() {
		return 0, nil
	}
	var (
		notify  string
		devices bool
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT notify, EXISTS (SELECT 1 FROM devices WHERE devices.user_id = email_configs.user_id)
		   FROM email_configs WHERE id = ?`, mb.EmailConfigID).Scan(&notify, &devices)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("arrived: %w", err)
	case notify == NotifyOff || !devices:
		return 0, nil
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("arrived: %w", err)
	}
	defer tx.Rollback()
	now := unix(s.Now())
	kept := 0
	for _, h := range headers {
		if h.MessageID != "" {
			var known bool
			if err := tx.QueryRowContext(ctx,
				`SELECT EXISTS (SELECT 1 FROM arrivals WHERE email_config_id = ? AND message_id = ?)`,
				mb.EmailConfigID, h.MessageID).Scan(&known); err != nil {
				return 0, fmt.Errorf("arrived: %w", err)
			}
			if known {
				continue
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO arrivals (email_config_id, mailbox_id, uidvalidity, uid, message_id,
			   from_name, from_email, subject, preview, arrived_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
			mb.EmailConfigID, mb.ID, mb.UIDValidity, h.UID, h.MessageID,
			h.From.Name, h.From.Email, h.Subject, h.Preview, now)
		if err != nil {
			return 0, fmt.Errorf("arrived: %w", err)
		}
		n, _ := res.RowsAffected()
		kept += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("arrived: %w", err)
	}
	if kept > 0 {
		s.wake()
	}
	return kept, nil
}

// Batch is what one email config has to tell its user's devices.
type Batch struct {
	EmailConfigID string
	UserID        string
	// Label is what the user calls the config: its name, or else its address.
	Label string
	// Alone is its being the user's only email config, which a notification need not name then.
	Alone bool
	// Digest is its gathering what arrived, rather than telling each as it came.
	Digest bool
	// Arrivals are what is still worth telling, newest first.
	Arrivals []Arrival
	// Settles is every arrival the batch is for, the ones already read among them.
	Settles []int64
}

// Arrival is a message as a notification tells it.
type Arrival struct {
	MailboxID   string
	UIDValidity uint32
	UID         uint32
	From        Address
	Subject     string
	Preview     string
}

// Due is every batch whose time has come, and when the next one's will, zero for none waiting.
//
// A config that notifies now is due at once, and one that sends a digest once its oldest waiting
// arrival has waited the interval. What is waiting for one that no longer notifies is settled
// untold.
func (s *Store) Due(ctx context.Context) ([]Batch, time.Time, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT ec.id, ec.user_id, ec.name, ec.email, ec.notify, ec.digest_every, MIN(a.arrived_at),
		        (SELECT COUNT(*) FROM email_configs o WHERE o.user_id = ec.user_id)
		   FROM arrivals a JOIN email_configs ec ON ec.id = a.email_config_id
		  WHERE a.told_at IS NULL
		  GROUP BY ec.id`)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("due: %w", err)
	}
	type waiting struct {
		batch Batch
		mode  string
		ready time.Time
	}
	var all []waiting
	for rows.Next() {
		var (
			w            waiting
			name, email  string
			every        int
			first, users int64
		)
		if err := rows.Scan(&w.batch.EmailConfigID, &w.batch.UserID, &name, &email, &w.mode, &every, &first, &users); err != nil {
			rows.Close()
			return nil, time.Time{}, fmt.Errorf("due: %w", err)
		}
		w.batch.Label = name
		if w.batch.Label == "" {
			w.batch.Label = email
		}
		w.batch.Alone = users == 1
		w.batch.Digest = w.mode == NotifyDigest
		w.ready = fromUnix(first)
		if w.mode == NotifyDigest {
			w.ready = w.ready.Add(time.Duration(every) * time.Minute)
		}
		all = append(all, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("due: %w", err)
	}

	now := s.Now()
	var (
		out  []Batch
		next time.Time
	)
	for _, w := range all {
		if w.mode != NotifyOff && w.ready.After(now) {
			if next.IsZero() || w.ready.Before(next) {
				next = w.ready
			}
			continue
		}
		b := w.batch
		if err := s.waitingFor(ctx, &b); err != nil {
			return nil, time.Time{}, err
		}
		if w.mode == NotifyOff {
			if err := s.Settle(ctx, b.Settles); err != nil {
				return nil, time.Time{}, err
			}
			continue
		}
		out = append(out, b)
	}
	return out, next, nil
}

// waitingFor fills in a batch's untold arrivals, leaving out from what it tells a message read
// since, one no longer in its folder, and one in a folder that no longer notifies.
func (s *Store) waitingFor(ctx context.Context, b *Batch) error {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT a.id, a.mailbox_id, a.uidvalidity, a.uid, a.from_name, a.from_email, a.subject, a.preview,
		        mb.uidvalidity, mb.notify, mb.special_use, m.seen,
		        (SELECT MIN(k.uid) FROM messages k WHERE k.mailbox_id = a.mailbox_id)
		   FROM arrivals a
		   JOIN mailboxes mb ON mb.id = a.mailbox_id
		   LEFT JOIN messages m ON m.mailbox_id = a.mailbox_id AND m.uid = a.uid
		  WHERE a.email_config_id = ? AND a.told_at IS NULL
		  ORDER BY a.arrived_at DESC, a.id DESC`, b.EmailConfigID)
	if err != nil {
		return fmt.Errorf("due: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id         int64
			a          Arrival
			validity   uint32
			chosen     sql.NullBool
			use        string
			seen       sql.NullBool
			oldestKept sql.NullInt64
		)
		if err := rows.Scan(&id, &a.MailboxID, &a.UIDValidity, &a.UID, &a.From.Name, &a.From.Email, &a.Subject, &a.Preview,
			&validity, &chosen, &use, &seen, &oldestKept); err != nil {
			return fmt.Errorf("due: %w", err)
		}
		b.Settles = append(b.Settles, id)
		var choice *bool
		if chosen.Valid {
			choice = &chosen.Bool
		}
		// Not kept and newer than the oldest kept is gone from the folder; older has only left
		// the window.
		there := seen.Valid || (oldestKept.Valid && int64(a.UID) < oldestKept.Int64)
		if validity != a.UIDValidity || !notifies(choice, use) || !there || (seen.Valid && seen.Bool) {
			continue
		}
		b.Arrivals = append(b.Arrivals, a)
	}
	return rows.Err()
}

// Settle marks arrivals told.
func (s *Store) Settle(ctx context.Context, arrivals []int64) error {
	if len(arrivals) == 0 {
		return nil
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("settle arrivals: %w", err)
	}
	defer tx.Rollback()
	now := unix(s.Now())
	for _, id := range arrivals {
		if _, err := tx.ExecContext(ctx, `UPDATE arrivals SET told_at = ? WHERE id = ?`, now, id); err != nil {
			return fmt.Errorf("settle arrivals: %w", err)
		}
	}
	return tx.Commit()
}

// arrivalsKept is how long an arrival is remembered: long enough that the same message filed
// in another folder later, by a rule or by hand, is known.
const arrivalsKept = 7 * 24 * time.Hour

// SweepArrivals forgets arrivals older than arrivalsKept.
func (s *Store) SweepArrivals(ctx context.Context) (int64, error) {
	res, err := s.writer.ExecContext(ctx, `DELETE FROM arrivals WHERE arrived_at < ?`, unix(s.Now().Add(-arrivalsKept)))
	if err != nil {
		return 0, fmt.Errorf("sweep arrivals: %w", err)
	}
	return res.RowsAffected()
}
