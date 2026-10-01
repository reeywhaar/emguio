package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"emguio/internal/ids"
)

// What an incoming server can speak, and how a server is secured — see docs/email-configs.md.
const (
	ProtocolIMAP = "imap"

	TLSImplicit = "implicit"
	TLSStartTLS = "starttls"
)

// Server is where an email config reads or sends from, without its password.
type Server struct {
	// Protocol is the incoming server's, and empty on an outgoing one.
	Protocol string
	Host     string
	Port     int
	TLS      string
	// Username is empty on an outgoing server that signs in as the incoming one does.
	Username string
}

// EmailConfig is one of a user's mail accounts.
type EmailConfig struct {
	ID       string
	UserID   string
	Name     string
	Email    string
	Incoming Server
	// Outgoing is nil when there is none.
	Outgoing  *Server
	CreatedAt time.Time
	UpdatedAt time.Time
	// SyncedAt is when its mail was last brought up to date, and SyncError why the latest try
	// did not, or empty when it did.
	SyncedAt  *time.Time
	SyncError string
}

// Login is a server and the password to sign in to it.
type Login struct {
	Server
	Password string
}

// EmailConfigInput is a whole email config as a form sends it. On an edit, a password left
// empty keeps the one stored.
type EmailConfigInput struct {
	Name     string
	Email    string
	Incoming Login
	Outgoing *Login
}

// Logins are an email config's servers ready to dial: passwords opened, and an outgoing server
// that shares the incoming sign-in given it.
type Logins struct {
	Incoming Login
	Outgoing *Login
}

// emailConfigRow is an email config as stored: its sealed secrets beside what can be shown.
type emailConfigRow struct {
	EmailConfig
	incomingSecret []byte
	// outgoingSecret is nil when there is no outgoing server, or it signs in as the incoming one.
	outgoingSecret []byte
}

const emailConfigColumns = `id, user_id, name, email,
  incoming_protocol, incoming_host, incoming_port, incoming_tls, incoming_username, incoming_secret,
  outgoing_host, outgoing_port, outgoing_tls, outgoing_username, outgoing_secret,
  created_at, updated_at`

// emailConfigSelect is what a read takes: every written column, and the sync state the mirror
// keeps beside them.
const emailConfigSelect = emailConfigColumns + `, synced_at, sync_error`

func scanEmailConfig(sc interface{ Scan(...any) error }) (*emailConfigRow, error) {
	var (
		r                  emailConfigRow
		oHost, oTLS, oUser sql.NullString
		oPort              sql.NullInt64
		created, updated   int64
		synced             sql.NullInt64
	)
	err := sc.Scan(&r.ID, &r.UserID, &r.Name, &r.Email,
		&r.Incoming.Protocol, &r.Incoming.Host, &r.Incoming.Port, &r.Incoming.TLS, &r.Incoming.Username, &r.incomingSecret,
		&oHost, &oPort, &oTLS, &oUser, &r.outgoingSecret,
		&created, &updated, &synced, &r.SyncError)
	if err != nil {
		return nil, err
	}
	if oHost.Valid {
		r.Outgoing = &Server{Host: oHost.String, Port: int(oPort.Int64), TLS: oTLS.String, Username: oUser.String}
	}
	r.CreatedAt = fromUnix(created)
	r.UpdatedAt = fromUnix(updated)
	if synced.Valid {
		at := fromUnix(synced.Int64)
		r.SyncedAt = &at
	}
	return &r, nil
}

// EmailConfigs lists one user's email configs, oldest first.
func (s *Store) EmailConfigs(ctx context.Context, userID string) ([]*EmailConfig, error) {
	rows, err := s.reader.QueryContext(ctx,
		`SELECT `+emailConfigSelect+` FROM email_configs WHERE user_id = ? ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list email configs: %w", err)
	}
	defer rows.Close()
	out := []*EmailConfig{}
	for rows.Next() {
		r, err := scanEmailConfig(rows)
		if err != nil {
			return nil, fmt.Errorf("list email configs: %w", err)
		}
		out = append(out, &r.EmailConfig)
	}
	return out, rows.Err()
}

// EmailConfig returns one of a user's email configs. Somebody else's is not found, not
// forbidden: whether an id exists is not theirs to learn.
func (s *Store) EmailConfig(ctx context.Context, userID, id string) (*EmailConfig, error) {
	r, err := s.emailConfigRow(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return &r.EmailConfig, nil
}

func (s *Store) emailConfigRow(ctx context.Context, userID, id string) (*emailConfigRow, error) {
	if !ids.Valid(ids.EmailConfig, id) {
		return nil, Invalid("%q is not an email config id.", id)
	}
	r, err := scanEmailConfig(s.reader.QueryRowContext(ctx,
		`SELECT `+emailConfigSelect+` FROM email_configs WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, NotFound("There is no such email config.")
	}
	if err != nil {
		return nil, fmt.Errorf("email config: %w", err)
	}
	return r, nil
}

// CreateEmailConfig saves a new email config. Both passwords it signs in with are required.
func (s *Store) CreateEmailConfig(ctx context.Context, userID string, in EmailConfigInput) (*EmailConfig, error) {
	in, err := s.complete(nil, in)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	id := ids.New(ids.EmailConfig, now.UnixMilli())
	args := []any{id, userID, in.Name, in.Email,
		in.Incoming.Protocol, in.Incoming.Host, in.Incoming.Port, in.Incoming.TLS, in.Incoming.Username,
		s.sealer.Seal([]byte(in.Incoming.Password), secretAAD(id, "incoming"))}
	args = append(args, s.outgoingColumns(id, in.Outgoing)...)
	args = append(args, unix(now), unix(now))
	_, err = s.writer.ExecContext(ctx,
		`INSERT INTO email_configs (`+emailConfigColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("create email config: %w", err)
	}
	return shown(id, userID, in, now, now), nil
}

// UpdateEmailConfig replaces an email config with what a form sent, keeping any password the
// form left empty.
func (s *Store) UpdateEmailConfig(ctx context.Context, userID, id string, in EmailConfigInput) (*EmailConfig, error) {
	r, err := s.emailConfigRow(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if in, err = s.complete(r, in); err != nil {
		return nil, err
	}
	now := s.Now()
	args := []any{in.Name, in.Email,
		in.Incoming.Protocol, in.Incoming.Host, in.Incoming.Port, in.Incoming.TLS, in.Incoming.Username,
		s.sealer.Seal([]byte(in.Incoming.Password), secretAAD(id, "incoming"))}
	args = append(args, s.outgoingColumns(id, in.Outgoing)...)
	args = append(args, unix(now), id, userID)

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("update email config: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE email_configs SET name = ?, email = ?,
		   incoming_protocol = ?, incoming_host = ?, incoming_port = ?, incoming_tls = ?, incoming_username = ?, incoming_secret = ?,
		   outgoing_host = ?, outgoing_port = ?, outgoing_tls = ?, outgoing_username = ?, outgoing_secret = ?,
		   updated_at = ?
		 WHERE id = ? AND user_id = ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("update email config: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, NotFound("There is no such email config.")
	}
	// Another host or another username is another mailbox store, and what was copied from the
	// old one is not this config's mail any more. A port or a security setting is the same one.
	moved := in.Incoming.Host != r.Incoming.Host || in.Incoming.Username != r.Incoming.Username
	if moved {
		if _, err := tx.ExecContext(ctx, `DELETE FROM mailboxes WHERE email_config_id = ?`, id); err != nil {
			return nil, fmt.Errorf("update email config: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE email_configs SET synced_at = NULL, sync_error = '' WHERE id = ?`, id); err != nil {
			return nil, fmt.Errorf("update email config: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("update email config: %w", err)
	}
	if moved {
		s.Notify(userID)
	}
	out := shown(id, userID, in, r.CreatedAt, now)
	if !moved {
		out.SyncedAt, out.SyncError = r.SyncedAt, r.SyncError
	}
	return out, nil
}

// DeleteEmailConfig removes one of a user's email configs.
func (s *Store) DeleteEmailConfig(ctx context.Context, userID, id string) error {
	if !ids.Valid(ids.EmailConfig, id) {
		return Invalid("%q is not an email config id.", id)
	}
	res, err := s.writer.ExecContext(ctx, `DELETE FROM email_configs WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("delete email config: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return NotFound("There is no such email config.")
	}
	return nil
}

// Logins is what dialing needs for a form's draft: completed with what is stored under id, or
// with nothing when id is empty, and the outgoing server's sign-in filled in when it shares the
// incoming one.
func (s *Store) Logins(ctx context.Context, userID, id string, in EmailConfigInput) (*Logins, error) {
	var r *emailConfigRow
	if id != "" {
		var err error
		if r, err = s.emailConfigRow(ctx, userID, id); err != nil {
			return nil, err
		}
	}
	in, err := s.complete(r, in)
	if err != nil {
		return nil, err
	}
	out := &Logins{Incoming: in.Incoming}
	if o := in.Outgoing; o != nil {
		login := *o
		if login.Username == "" {
			login.Username = in.Incoming.Username
			login.Password = in.Incoming.Password
		}
		out.Outgoing = &login
	}
	return out, nil
}

// complete validates a draft and fills each password it left empty from r, which is nil for an
// email config not saved yet.
func (s *Store) complete(r *emailConfigRow, in EmailConfigInput) (EmailConfigInput, error) {
	if in.Outgoing != nil {
		o := *in.Outgoing
		in.Outgoing = &o
	}
	if err := in.normalize(); err != nil {
		return in, err
	}
	if in.Incoming.Password == "" {
		if r == nil {
			return in, Invalid("The incoming server needs a password.")
		}
		// Wherever the incoming password will be sent: the incoming server, and an outgoing
		// one that signs in as it does.
		sentTo := []string{in.Incoming.Host}
		if o := in.Outgoing; o != nil && o.Username == "" {
			sentTo = append(sentTo, o.Host)
		}
		if err := sameHosts(sentTo, r.incomingHosts()); err != nil {
			return in, err
		}
		p, err := s.open(r.incomingSecret, r.ID, "incoming")
		if err != nil {
			return in, err
		}
		in.Incoming.Password = p
	}
	if o := in.Outgoing; o != nil && o.Username != "" && o.Password == "" {
		if r == nil || r.outgoingSecret == nil {
			return in, Invalid("The outgoing server needs a password, or leave its username empty to sign in as the incoming server does.")
		}
		if err := sameHosts([]string{o.Host}, []string{r.Outgoing.Host}); err != nil {
			return in, err
		}
		p, err := s.open(r.outgoingSecret, r.ID, "outgoing")
		if err != nil {
			return in, err
		}
		o.Password = p
	}
	return in, nil
}

// incomingHosts are where the stored incoming password has been sent with this config's say-so.
func (r *emailConfigRow) incomingHosts() []string {
	hosts := []string{r.Incoming.Host}
	if r.Outgoing != nil && r.Outgoing.Username == "" {
		hosts = append(hosts, r.Outgoing.Host)
	}
	return hosts
}

// sameHosts refuses to send a stored password anywhere it was not saved for. Without this,
// whoever holds a session could change a host to their own and press Test to collect it.
func sameHosts(sentTo, savedFor []string) error {
	for _, h := range sentTo {
		known := false
		for _, saved := range savedFor {
			known = known || h == saved
		}
		if !known {
			return Invalid("A saved password is only sent to the host it was saved for. Enter it again to use it with %s.", h)
		}
	}
	return nil
}

func (s *Store) open(sealed []byte, id, role string) (string, error) {
	plain, err := s.sealer.Open(sealed, secretAAD(id, role))
	if err != nil {
		return "", Invalid("The password saved for the %s server can no longer be read: the server key has changed since it was saved. Enter it again.", role)
	}
	return string(plain), nil
}

// secretAAD binds a sealed password to its row and its server, so one copied anywhere else does
// not open.
func secretAAD(id, role string) []byte { return []byte(id + "/" + role) }

// outgoingColumns are the five outgoing values in column order: all NULL for no server, and a
// NULL username and secret for one that signs in as the incoming server does.
func (s *Store) outgoingColumns(id string, o *Login) []any {
	if o == nil {
		return []any{nil, nil, nil, nil, nil}
	}
	if o.Username == "" {
		return []any{o.Host, o.Port, o.TLS, nil, nil}
	}
	return []any{o.Host, o.Port, o.TLS, o.Username, s.sealer.Seal([]byte(o.Password), secretAAD(id, "outgoing"))}
}

func shown(id, userID string, in EmailConfigInput, created, updated time.Time) *EmailConfig {
	c := &EmailConfig{
		ID:        id,
		UserID:    userID,
		Name:      in.Name,
		Email:     in.Email,
		Incoming:  in.Incoming.Server,
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if in.Outgoing != nil {
		o := in.Outgoing.Server
		c.Outgoing = &o
	}
	return c
}

func (in *EmailConfigInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.TrimSpace(in.Email)
	switch {
	case utf8.RuneCountInString(in.Name) > 64:
		return Invalid("That name is longer than 64 characters.")
	case hasControl(in.Name):
		return Invalid("A name cannot contain control characters.")
	}
	if err := validateEmail(in.Email); err != nil {
		return err
	}

	if err := in.Incoming.normalize("incoming"); err != nil {
		return err
	}
	switch in.Incoming.Protocol {
	case ProtocolIMAP:
	case "":
		return Invalid("Choose the protocol the incoming server speaks.")
	default:
		return Invalid("%q is not a protocol emguio reads mail with. Choose imap.", in.Incoming.Protocol)
	}
	if in.Incoming.Username == "" {
		return Invalid("The incoming server needs a username.")
	}

	if o := in.Outgoing; o != nil {
		o.Protocol = ""
		if err := o.normalize("outgoing"); err != nil {
			return err
		}
		if o.Username == "" {
			o.Password = ""
		}
	}
	return nil
}

func (l *Login) normalize(role string) error {
	l.Protocol = strings.ToLower(strings.TrimSpace(l.Protocol))
	l.Host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(l.Host)), ".")
	l.TLS = strings.ToLower(strings.TrimSpace(l.TLS))
	l.Username = strings.TrimSpace(l.Username)

	if err := validateHost(l, role); err != nil {
		return err
	}
	if l.Port < 1 || l.Port > 65535 {
		return Invalid("The %s server needs a port between 1 and 65535.", role)
	}
	switch l.TLS {
	case TLSImplicit, TLSStartTLS:
	case "":
		return Invalid("Choose how the %s server is secured.", role)
	default:
		return Invalid("%q is not a way to secure a server. Choose implicit or starttls.", l.TLS)
	}
	switch {
	case len(l.Username) > 320:
		return Invalid("The %s server's username is longer than 320 characters.", role)
	case hasControl(l.Username):
		return Invalid("The %s server's username cannot contain control characters.", role)
	case len(l.Password) > 1024:
		return Invalid("The %s server's password is longer than 1024 bytes.", role)
	}
	return nil
}

// validateHost takes a name or an address, and refuses what is plainly a URL or has a port in
// it: those go in fields of their own, and a host that carries them is never dialed as meant.
func validateHost(l *Login, role string) error {
	if l.Host == "" {
		return Invalid("The %s server needs a host, like mail.example.com.", role)
	}
	if len(l.Host) > 253 {
		return Invalid("The %s server's host is longer than 253 characters.", role)
	}
	if addr, err := netip.ParseAddr(strings.Trim(l.Host, "[]")); err == nil {
		l.Host = addr.String()
		return nil
	}
	if strings.ContainsAny(l.Host, " /:@?#\\[]") || hasControl(l.Host) {
		return Invalid("%q is not a host name. Give the %s server on its own, like mail.example.com, with the port in its own field.", l.Host, role)
	}
	return nil
}

func validateEmail(email string) error {
	switch {
	case email == "":
		return Invalid("An email address is required.")
	case len(email) > 254:
		return Invalid("That email address is longer than 254 characters.")
	}
	at := strings.LastIndex(email, "@")
	if at < 1 || at == len(email)-1 || strings.ContainsAny(email, " <>,;\"") || hasControl(email) {
		return Invalid("%q is not an email address.", email)
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
