package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	// A zone the browser named is found whether or not the system has a zone database.
	_ "time/tzdata"
)

// PushSettings is how a user's notifications come, whichever email config they are for. See
// docs/push.md.
type PushSettings struct {
	// Silent asks each device to show them without a sound.
	Silent bool
	// Quiet is the hours nothing is sent, every day; nil for none.
	Quiet *Quiet
}

// Quiet is a stretch of every day in a time zone, past midnight when From is after Until.
type Quiet struct {
	// From and Until are minutes after midnight.
	From, Until int
	// Zone is an IANA name, as a browser gives it.
	Zone string
}

const minutesInDay = 24 * 60

func (q Quiet) location() (*time.Location, error) {
	if q.From < 0 || q.From >= minutesInDay || q.Until < 0 || q.Until >= minutesInDay {
		return nil, Invalid("Quiet hours start and end within a day, from 00:00 to 23:59.")
	}
	if q.From == q.Until {
		return nil, Invalid("Quiet hours end at another time than they start.")
	}
	// LoadLocation takes "" and "Local" as the server's own zone, which is UTC here.
	if q.Zone == "" || q.Zone == "Local" {
		return nil, Invalid("Quiet hours need a time zone.")
	}
	loc, err := time.LoadLocation(q.Zone)
	if err != nil {
		return nil, Invalid("%q is not a time zone emguio knows.", q.Zone)
	}
	return loc, nil
}

// End is when the quiet hours that hold t end, and false when t is outside them.
func (q Quiet) End(t time.Time) (time.Time, bool) {
	loc, err := q.location()
	if err != nil {
		return time.Time{}, false
	}
	local := t.In(loc)
	m := local.Hour()*60 + local.Minute()
	in := m >= q.From && m < q.Until
	if q.From > q.Until {
		in = m >= q.From || m < q.Until
	}
	if !in {
		return time.Time{}, false
	}
	y, mo, d := local.Date()
	end := time.Date(y, mo, d, q.Until/60, q.Until%60, 0, 0, loc)
	if !end.After(t) {
		end = time.Date(y, mo, d+1, q.Until/60, q.Until%60, 0, 0, loc)
	}
	return end, true
}

// PushSettings is how a user's notifications come.
func (s *Store) PushSettings(ctx context.Context, userID string) (PushSettings, error) {
	var (
		out         PushSettings
		from, until sql.NullInt64
		zone        string
	)
	err := s.reader.QueryRowContext(ctx,
		`SELECT push_silent, quiet_from, quiet_until, quiet_zone FROM users WHERE id = ?`, userID).
		Scan(&out.Silent, &from, &until, &zone)
	if errors.Is(err, sql.ErrNoRows) {
		return out, NotFound("There is no such user.")
	}
	if err != nil {
		return out, fmt.Errorf("push settings: %w", err)
	}
	if from.Valid && until.Valid {
		out.Quiet = &Quiet{From: int(from.Int64), Until: int(until.Int64), Zone: zone}
	}
	return out, nil
}

// SetPushSettings chooses how a user's notifications come. What quiet hours held may be due at
// once.
func (s *Store) SetPushSettings(ctx context.Context, userID string, p PushSettings) error {
	var from, until any
	zone := ""
	if q := p.Quiet; q != nil {
		if _, err := q.location(); err != nil {
			return err
		}
		from, until, zone = q.From, q.Until, q.Zone
	}
	res, err := s.writer.ExecContext(ctx,
		`UPDATE users SET push_silent = ?, quiet_from = ?, quiet_until = ?, quiet_zone = ? WHERE id = ?`,
		p.Silent, from, until, zone, userID)
	if err != nil {
		return fmt.Errorf("set push settings: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such user.")
	}
	s.wake()
	return nil
}
