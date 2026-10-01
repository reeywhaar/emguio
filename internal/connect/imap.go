package connect

import (
	"context"
	"errors"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// CheckIMAP signs in and opens INBOX read-only, which is everything reading mail will need and
// changes nothing on the server.
func (c *Connector) CheckIMAP(ctx context.Context, s Server) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, release, err := c.dial(ctx, s)
	if err != nil {
		return err
	}
	defer release()
	opts := &imapclient.Options{TLSConfig: c.tlsConfig(s.Host)}
	var client *imapclient.Client
	if s.TLS == StartTLS {
		client, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			return failed(ctx, s, imapFailure(s, "starttls", err))
		}
	} else {
		client = imapclient.New(conn, opts)
	}
	defer client.Close()

	if err := client.WaitGreeting(); err != nil {
		return failed(ctx, s, imapFailure(s, "greeting", err))
	}
	if err := client.Login(s.Username, s.Password).Wait(); err != nil {
		return failed(ctx, s, imapFailure(s, "login", err))
	}
	// EXAMINE, not SELECT: a read-only session cannot change \Seen by accident.
	if _, err := client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return failed(ctx, s, imapFailure(s, "inbox", err))
	}
	client.Logout().Wait()
	return nil
}

func imapFailure(s Server, stage string, err error) *Failure {
	var refusal *imap.Error
	if errors.As(err, &refusal) {
		text := said(refusal.Text)
		switch stage {
		case "starttls":
			return fail("starttls", "%s does not offer STARTTLS: %s", s.addr(), text)
		case "login":
			return fail("auth", "%s refused the username or password: %s", s.addr(), text)
		case "inbox":
			return fail("mailbox", "Signed in, but INBOX could not be opened: %s", text)
		}
		return fail("protocol", "%s refused: %s", s.addr(), text)
	}
	if f, ok := netFailure(s, err); ok {
		return f
	}
	if stage == "starttls" && strings.Contains(err.Error(), "PREAUTH") {
		return fail("starttls", "%s signed in before STARTTLS, which would have sent the password in the clear.", s.addr())
	}
	return fail("protocol", "%s did not answer as an IMAP server does.%s", s.addr(), tlsHint(s))
}
