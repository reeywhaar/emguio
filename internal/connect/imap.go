package connect

import (
	"context"
	"errors"
	"mime"
	"net"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

// wordDecoder reads encoded header words in whatever charset they declare — koi8-r and
// windows-1251 included — rather than only the few the standard library knows.
var wordDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

// OpenIMAP signs in and returns the session. Signing in is bounded by the connector's timeout;
// the session is not, and lives until ctx ends or it is closed.
func (c *Connector) OpenIMAP(ctx context.Context, s Server) (*imapclient.Client, error) {
	signIn, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, release, err := c.dial(signIn, s)
	if err != nil {
		return nil, err
	}
	client, err := c.signInIMAP(signIn, s, conn)
	if !release() {
		// The sign-in ran out of time and the connection was closed under it.
		if client != nil {
			client.Close()
		}
		return nil, timeoutFailure(s)
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	context.AfterFunc(ctx, func() { client.Close() })
	return client, nil
}

func (c *Connector) signInIMAP(ctx context.Context, s Server, conn net.Conn) (*imapclient.Client, error) {
	opts := &imapclient.Options{TLSConfig: c.tlsConfig(s.Host), WordDecoder: wordDecoder}
	var client *imapclient.Client
	if s.TLS == StartTLS {
		var err error
		client, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			return nil, failed(ctx, s, imapFailure(s, "starttls", err))
		}
	} else {
		client = imapclient.New(conn, opts)
	}
	if err := client.WaitGreeting(); err != nil {
		client.Close()
		return nil, failed(ctx, s, imapFailure(s, "greeting", err))
	}
	if err := client.Login(s.Username, s.Password).Wait(); err != nil {
		client.Close()
		return nil, failed(ctx, s, imapFailure(s, "login", err))
	}
	return client, nil
}

// CheckIMAP signs in and opens INBOX read-only, which is everything reading mail will need and
// changes nothing on the server.
func (c *Connector) CheckIMAP(ctx context.Context, s Server) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	client, err := c.OpenIMAP(ctx, s)
	if err != nil {
		return err
	}
	defer client.Close()
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
		text := Said(refusal.Text)
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
