package connect

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// SendTimeout bounds the sending of one message, from dialing to the server's answer to its last
// byte: a large attachment over a slow link takes longer than signing in does.
const SendTimeout = 2 * time.Minute

// CheckSMTP signs in and leaves without sending anything.
func (c *Connector) CheckSMTP(ctx context.Context, s Server) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	client, release, err := c.openSMTP(ctx, s)
	if err != nil {
		return err
	}
	defer release()
	defer client.Close()
	client.Quit()
	return nil
}

// Send signs in and hands msg to the server for every address in to, from the address from.
// The server's refusal — of the sender, of a recipient, of the message — comes back in a
// sentence.
func (c *Connector) Send(ctx context.Context, s Server, from string, to []string, msg io.Reader) error {
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	client, release, err := c.openSMTP(ctx, s)
	if err != nil {
		return err
	}
	defer release()
	defer client.Close()
	if err := client.SendMail(from, to, msg); err != nil {
		return failed(ctx, s, smtpFailure(s, "send", err))
	}
	client.Quit()
	return nil
}

// openSMTP dials, says hello, secures the connection and signs in. The connection lives until
// ctx ends; release lets it go on past that.
func (c *Connector) openSMTP(ctx context.Context, s Server) (*smtp.Client, func() bool, error) {
	conn, release, err := c.dial(ctx, s)
	if err != nil {
		return nil, nil, err
	}
	var client *smtp.Client
	if s.TLS == StartTLS {
		client, err = smtp.NewClientStartTLS(conn, c.tlsConfig(s.Host))
		if err != nil {
			release()
			conn.Close()
			return nil, nil, failed(ctx, s, smtpFailure(s, "starttls", err))
		}
	} else {
		client = smtp.NewClient(conn)
		// Explicitly, so a server that is not SMTP at all says so here rather than as a missing
		// way to sign in.
		if err := client.Hello("localhost"); err != nil {
			release()
			client.Close()
			return nil, nil, failed(ctx, s, smtpFailure(s, "greeting", err))
		}
	}

	var mech sasl.Client
	switch {
	case client.SupportsAuth(sasl.Plain):
		mech = sasl.NewPlainClient("", s.Username, s.Password)
	case client.SupportsAuth(sasl.Login):
		mech = sasl.NewLoginClient(s.Username, s.Password)
	default:
		release()
		client.Close()
		return nil, nil, failed(ctx, s, fail("auth", "%s offers no way to sign in that emguio speaks.", s.addr()))
	}
	if err := client.Auth(mech); err != nil {
		release()
		client.Close()
		return nil, nil, failed(ctx, s, smtpFailure(s, "login", err))
	}
	return client, release, nil
}

func smtpFailure(s Server, stage string, err error) *Failure {
	var refusal *smtp.SMTPError
	if errors.As(err, &refusal) {
		text := Said(refusal.Message)
		switch stage {
		case "starttls":
			return fail("starttls", "%s refused STARTTLS: %s", s.addr(), text)
		case "login":
			return fail("auth", "%s refused the username or password: %s", s.addr(), text)
		case "send":
			return fail("refused", "%s refused the message: %s", s.addr(), text)
		}
		return fail("protocol", "%s refused: %s", s.addr(), text)
	}
	if f, ok := netFailure(s, err); ok {
		return f
	}
	if stage == "starttls" && strings.Contains(err.Error(), "STARTTLS") {
		return fail("starttls", "%s does not offer STARTTLS. If the port expects TLS from the start, choose that instead.", s.addr())
	}
	return fail("protocol", "%s did not answer as an SMTP server does.%s", s.addr(), tlsHint(s))
}
