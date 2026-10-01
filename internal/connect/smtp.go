package connect

import (
	"context"
	"errors"
	"strings"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// CheckSMTP signs in and leaves without sending anything.
func (c *Connector) CheckSMTP(ctx context.Context, s Server) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	conn, release, err := c.dial(ctx, s)
	if err != nil {
		return err
	}
	defer release()
	var client *smtp.Client
	if s.TLS == StartTLS {
		client, err = smtp.NewClientStartTLS(conn, c.tlsConfig(s.Host))
		if err != nil {
			return failed(ctx, s, smtpFailure(s, "starttls", err))
		}
	} else {
		client = smtp.NewClient(conn)
		// Explicitly, so a server that is not SMTP at all says so here rather than as a missing
		// way to sign in.
		if err := client.Hello("localhost"); err != nil {
			client.Close()
			return failed(ctx, s, smtpFailure(s, "greeting", err))
		}
	}
	defer client.Close()

	var mech sasl.Client
	switch {
	case client.SupportsAuth(sasl.Plain):
		mech = sasl.NewPlainClient("", s.Username, s.Password)
	case client.SupportsAuth(sasl.Login):
		mech = sasl.NewLoginClient(s.Username, s.Password)
	default:
		return failed(ctx, s, fail("auth", "%s offers no way to sign in that emguio speaks.", s.addr()))
	}
	if err := client.Auth(mech); err != nil {
		return failed(ctx, s, smtpFailure(s, "login", err))
	}
	client.Quit()
	return nil
}

func smtpFailure(s Server, stage string, err error) *Failure {
	var refusal *smtp.SMTPError
	if errors.As(err, &refusal) {
		text := said(refusal.Message)
		switch stage {
		case "starttls":
			return fail("starttls", "%s refused STARTTLS: %s", s.addr(), text)
		case "login":
			return fail("auth", "%s refused the username or password: %s", s.addr(), text)
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
