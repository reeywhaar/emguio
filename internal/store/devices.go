package store

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"emguio/internal/ids"
)

// VAPIDKeys is the instance's push keypair, made on the first call: the private key, and the
// public point uncompressed, as a browser is given it. Never rotated — see docs/push.md.
func (s *Store) VAPIDKeys(ctx context.Context) (*ecdsa.PrivateKey, []byte, error) {
	var der, pub []byte
	err := s.reader.QueryRowContext(ctx, `SELECT private_key, public_key FROM vapid WHERE singleton = 1`).Scan(&der, &pub)
	switch {
	case err == nil:
		key, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return nil, nil, fmt.Errorf("read vapid key: %w", err)
		}
		ec, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, nil, errors.New("read vapid key: it is not an ECDSA key")
		}
		return ec, pub, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, nil, fmt.Errorf("read vapid key: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("make vapid key: %w", err)
	}
	if der, err = x509.MarshalPKCS8PrivateKey(key); err != nil {
		return nil, nil, fmt.Errorf("make vapid key: %w", err)
	}
	point, err := key.PublicKey.ECDH()
	if err != nil {
		return nil, nil, fmt.Errorf("make vapid key: %w", err)
	}
	// Two processes starting at once end with one keypair: the second reads the first's.
	if _, err := s.writer.ExecContext(ctx,
		`INSERT INTO vapid (singleton, private_key, public_key, created_at) VALUES (1, ?, ?, ?)
		 ON CONFLICT (singleton) DO NOTHING`, der, point.Bytes(), unix(s.Now())); err != nil {
		return nil, nil, fmt.Errorf("store vapid key: %w", err)
	}
	return s.VAPIDKeys(ctx)
}

// Device is a browser one of a user's has let notify them.
type Device struct {
	ID     string
	UserID string
	// Endpoint is where its push service takes messages for it, and P256dh and Auth what they
	// are encrypted with. None of the three leaves this package but to send.
	Endpoint string
	P256dh   string
	Auth     string
	// Label names it for a person: "Safari on iPhone".
	Label string
	// ClientID is what the browser calls itself, empty when it said nothing.
	ClientID     string
	CreatedAt    time.Time
	LastOKAt     *time.Time
	FailureCount int
	LastError    string
}

// DeviceInput is a subscription as a browser hands it over.
type DeviceInput struct {
	Endpoint string
	P256dh   string
	Auth     string
	Label    string
	ClientID string
}

const deviceLabelMax = 100

// RegisterDevice keeps a browser's subscription for a user. The same endpoint again updates its
// row, taking it from whoever had it: a browser signed in as somebody else since notifies them
// now. Another endpoint from the same browser replaces the one it had, since a browser renews
// its subscription on its own.
func (s *Store) RegisterDevice(ctx context.Context, userID string, in DeviceInput) (*Device, error) {
	in.Label = strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(in.Label) > deviceLabelMax {
		in.Label = string([]rune(in.Label)[:deviceLabelMax])
	}
	u, err := url.Parse(in.Endpoint)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "" || len(in.Endpoint) > 2048:
		// Every push service is https, and this is a URL that is posted to.
		return nil, Invalid("A push endpoint is an https address.")
	case in.P256dh == "" || in.Auth == "" || len(in.P256dh) > 128 || len(in.Auth) > 64:
		return nil, Invalid("A subscription needs both of its keys.")
	case len(in.ClientID) > 64 || hasControl(in.ClientID) || hasControl(in.Label):
		return nil, Invalid("That browser id or label is not one this takes.")
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	defer tx.Rollback()
	// Empty matches nothing: a browser that did not say who it is cannot be told from another.
	if in.ClientID != "" {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM devices WHERE user_id = ? AND client_id = ? AND endpoint <> ?`,
			userID, in.ClientID, in.Endpoint); err != nil {
			return nil, fmt.Errorf("register device: %w", err)
		}
	}
	now := s.Now()
	d := &Device{UserID: userID, Endpoint: in.Endpoint, P256dh: in.P256dh, Auth: in.Auth,
		Label: in.Label, ClientID: in.ClientID}
	var created int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO devices (id, user_id, endpoint, p256dh, auth, label, client_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (endpoint) DO UPDATE SET
		   user_id = excluded.user_id, p256dh = excluded.p256dh, auth = excluded.auth,
		   label = excluded.label, client_id = excluded.client_id,
		   failure_count = 0, last_error = ''
		 RETURNING id, created_at`,
		ids.New(ids.Device, now.UnixMilli()), userID, in.Endpoint, in.P256dh, in.Auth, in.Label, in.ClientID, unix(now)).
		Scan(&d.ID, &created); err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("register device: %w", err)
	}
	d.CreatedAt = fromUnix(created)
	return d, nil
}

// Devices lists one user's, oldest first.
func (s *Store) Devices(ctx context.Context, userID string) ([]*Device, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, label, client_id, created_at, last_ok_at, failure_count, last_error
		   FROM devices WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	out := []*Device{}
	for rows.Next() {
		var (
			d       Device
			created int64
			ok      sql.NullInt64
		)
		if err := rows.Scan(&d.ID, &d.UserID, &d.Endpoint, &d.P256dh, &d.Auth, &d.Label, &d.ClientID,
			&created, &ok, &d.FailureCount, &d.LastError); err != nil {
			return nil, fmt.Errorf("list devices: %w", err)
		}
		d.CreatedAt = fromUnix(created)
		if ok.Valid {
			at := fromUnix(ok.Int64)
			d.LastOKAt = &at
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// DeleteDevice stops notifying one of a user's browsers.
func (s *Store) DeleteDevice(ctx context.Context, userID, id string) error {
	if !ids.Valid(ids.Device, id) {
		return Invalid("%q is not a device id.", id)
	}
	res, err := s.writer.ExecContext(ctx, `DELETE FROM devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such device.")
	}
	return nil
}

// DropDevice deletes a device whose push service said its subscription is over.
func (s *Store) DropDevice(ctx context.Context, id string) error {
	if _, err := s.writer.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, id); err != nil {
		return fmt.Errorf("drop device: %w", err)
	}
	return nil
}

// DeviceDelivered records that a device's push service took a message.
func (s *Store) DeviceDelivered(ctx context.Context, id string) error {
	if _, err := s.writer.ExecContext(ctx,
		`UPDATE devices SET last_ok_at = ?, failure_count = 0, last_error = '' WHERE id = ?`, unix(s.Now()), id); err != nil {
		return fmt.Errorf("device delivered: %w", err)
	}
	return nil
}

// DeviceFailed records why a device's push service did not take a message.
func (s *Store) DeviceFailed(ctx context.Context, id, reason string) error {
	if _, err := s.writer.ExecContext(ctx,
		`UPDATE devices SET failure_count = failure_count + 1, last_error = ? WHERE id = ?`, reason, id); err != nil {
		return fmt.Errorf("device failed: %w", err)
	}
	return nil
}
