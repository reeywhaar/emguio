package connect

import (
	"context"
	"errors"
	"io"
	"slices"
	"strconv"
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

// Send signs in and hands msg to the server for every address in to, from the address from:
// with until, for the server to hold and send then. The server's refusal — of the sender, of a
// recipient, of the message, of the time — comes back in a sentence.
func (c *Connector) Send(ctx context.Context, s Server, from string, to []string, msg io.Reader, until time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	client, release, err := c.openSMTP(ctx, s)
	if err != nil {
		return err
	}
	defer release()
	defer client.Close()
	if ok, _ := client.Extension("FUTURERELEASE"); !until.IsZero() && !ok {
		return fail("refused", "%s does not hold mail to send later.", s.addr())
	}
	if err := send(client, from, to, msg, until); err != nil {
		return failed(ctx, s, smtpFailure(s, "send", err))
	}
	client.Quit()
	return nil
}

// send is the client's SendMail, with a time to hold the message until.
func send(client *smtp.Client, from string, to []string, msg io.Reader, until time.Time) error {
	opts := &smtp.MailOptions{HoldUntil: until}
	opts.UTF8 = !ascii(from) || slices.ContainsFunc(to, func(a string) bool { return !ascii(a) })
	if err := client.Mail(from, opts); err != nil {
		return err
	}
	for _, addr := range to {
		if err := client.Rcpt(addr, nil); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, msg); err != nil {
		return err
	}
	return w.Close()
}

func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Holds is the latest the server will hold a message until, to send it then, zero for a server
// that holds none: FUTURERELEASE (RFC 4865) in its greeting, with the longest hold in seconds and
// the latest date, of which the earlier.
func (c *Connector) Holds(ctx context.Context, s Server) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	client, release, err := c.openSMTP(ctx, s)
	if err != nil {
		return time.Time{}, err
	}
	defer release()
	defer client.Close()
	ok, limits := client.Extension("FUTURERELEASE")
	client.Quit()
	if !ok {
		return time.Time{}, nil
	}
	return latest(limits, time.Now()), nil
}

func latest(limits string, now time.Time) time.Time {
	var out time.Time
	fields := strings.Fields(limits)
	if len(fields) > 0 {
		if secs, err := strconv.ParseInt(fields[0], 10, 64); err == nil && secs > 0 {
			out = now.Add(time.Duration(secs) * time.Second)
		}
	}
	if len(fields) > 1 {
		if at, err := time.Parse(time.RFC3339, fields[1]); err == nil && (out.IsZero() || at.Before(out)) {
			out = at
		}
	}
	return out.UTC()
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
