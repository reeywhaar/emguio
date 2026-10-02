package connect_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
)

func connector(cert *connecttest.Cert) *connect.Connector {
	return connect.New(connecttest.Loopback).WithRoots(cert.Pool)
}

func server(port int, mode, username, password string) connect.Server {
	return connect.Server{Host: connecttest.Host, Port: port, TLS: mode, Username: username, Password: password}
}

// failure insists the error is a Failure of the given class, which is what the API shows.
func failure(t *testing.T, err error, class string) *connect.Failure {
	t.Helper()
	var f *connect.Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v (%T), want a Failure", err, err)
	}
	if f.Class != class {
		t.Fatalf("class = %q (%s), want %q", f.Class, f.Sentence, class)
	}
	return f
}

func TestIMAPSignsInOverEitherKindOfTLS(t *testing.T) {
	cert := connecttest.NewCert(t)
	c := connector(cert)
	for _, mode := range []string{connect.Implicit, connect.StartTLS} {
		port := connecttest.IMAP(t, cert, mode, "misha", "hunter2")
		if err := c.CheckIMAP(context.Background(), server(port, mode, "misha", "hunter2")); err != nil {
			t.Errorf("%s: %v", mode, err)
		}
	}
}

func TestIMAPSaysTheServerRefusedThePassword(t *testing.T) {
	cert := connecttest.NewCert(t)
	port := connecttest.IMAP(t, cert, connect.Implicit, "misha", "hunter2")
	err := connector(cert).CheckIMAP(context.Background(), server(port, connect.Implicit, "misha", "wrong"))
	f := failure(t, err, "auth")
	if !strings.Contains(f.Sentence, "refused the username or password") {
		t.Errorf("sentence = %q", f.Sentence)
	}
}

func TestSMTPSignsInOverEitherKindOfTLS(t *testing.T) {
	cert := connecttest.NewCert(t)
	c := connector(cert)
	for _, mode := range []string{connect.Implicit, connect.StartTLS} {
		port := connecttest.SMTP(t, cert, mode, "misha", "hunter2")
		if err := c.CheckSMTP(context.Background(), server(port, mode, "misha", "hunter2")); err != nil {
			t.Errorf("%s: %v", mode, err)
		}
	}
}

func TestSMTPSaysTheServerRefusedThePassword(t *testing.T) {
	cert := connecttest.NewCert(t)
	port := connecttest.SMTP(t, cert, connect.StartTLS, "misha", "hunter2")
	err := connector(cert).CheckSMTP(context.Background(), server(port, connect.StartTLS, "misha", "wrong"))
	failure(t, err, "auth")
}

// A message goes to the server as given, for each recipient; one the server will not deliver to
// is its refusal, in a sentence, and nothing is sent.
func TestSMTPSendsAMessage(t *testing.T) {
	cert := connecttest.NewCert(t)
	box := &connecttest.Outbox{}
	port := connecttest.SMTPInto(t, cert, connect.Implicit, "misha", "hunter2", box)
	s := server(port, connect.Implicit, "misha", "hunter2")
	msg := "From: misha@example.com\r\nTo: robin@example.com\r\nSubject: Hi\r\n\r\nHello.\r\n"

	err := connector(cert).Send(context.Background(), s, "misha@example.com",
		[]string{"robin@example.com", "kim@example.com"}, strings.NewReader(msg))
	if err != nil {
		t.Fatal(err)
	}
	sent := box.Sent()
	if len(sent) != 1 || sent[0].From != "misha@example.com" || strings.Join(sent[0].To, ",") != "robin@example.com,kim@example.com" ||
		!strings.Contains(string(sent[0].Data), "Hello.") {
		t.Fatalf("sent = %+v", sent)
	}

	err = connector(cert).Send(context.Background(), s, "misha@example.com",
		[]string{"robin@example.com", "nobody@" + connecttest.Refused}, strings.NewReader(msg))
	f := failure(t, err, "refused")
	if !strings.Contains(f.Sentence, "No such user here") {
		t.Errorf("sentence = %q", f.Sentence)
	}
	if n := len(box.Sent()); n != 1 {
		t.Errorf("%d sent after a refusal", n)
	}
}

// The commonest mistake in the form: TLS chosen for a port that speaks plain text first.
func TestImplicitTLSAgainstAPlainPortSaysToTryStartTLS(t *testing.T) {
	cert := connecttest.NewCert(t)
	port := connecttest.IMAP(t, cert, "starttls", "misha", "hunter2")
	err := connector(cert).CheckIMAP(context.Background(), server(port, connect.Implicit, "misha", "hunter2"))
	f := failure(t, err, "tls")
	if !strings.Contains(f.Sentence, "STARTTLS") {
		t.Errorf("sentence = %q, want it to name the other setting", f.Sentence)
	}
}

// A password must never cross the network because a server forgot to offer encryption.
func TestStartTLSAgainstAServerWithoutItStopsBeforeThePassword(t *testing.T) {
	cert := connecttest.NewCert(t)
	imapPort := connecttest.IMAP(t, cert, "", "misha", "hunter2")
	err := connector(cert).CheckIMAP(context.Background(), server(imapPort, connect.StartTLS, "misha", "hunter2"))
	failure(t, err, "starttls")

	smtpPort := connecttest.SMTP(t, cert, "", "misha", "hunter2")
	err = connector(cert).CheckSMTP(context.Background(), server(smtpPort, connect.StartTLS, "misha", "hunter2"))
	failure(t, err, "starttls")
}

func TestACertificateNobodyVouchesForIsRefused(t *testing.T) {
	cert := connecttest.NewCert(t)
	port := connecttest.IMAP(t, cert, connect.Implicit, "misha", "hunter2")
	untrusting := connect.New(connecttest.Loopback)
	err := untrusting.CheckIMAP(context.Background(), server(port, connect.Implicit, "misha", "hunter2"))
	failure(t, err, "tls")
}

// What a user types can be anything, and the inside of the network is not theirs to reach.
func TestAPrivateAddressIsNeverDialed(t *testing.T) {
	cert := connecttest.NewCert(t)
	port := connecttest.IMAP(t, cert, connect.Implicit, "misha", "hunter2")
	public := connect.New(nil).WithRoots(cert.Pool)

	err := public.CheckIMAP(context.Background(), server(port, connect.Implicit, "misha", "hunter2"))
	f := failure(t, err, "private")
	if !strings.Contains(f.Sentence, "127.0.0.1") {
		t.Errorf("sentence = %q, want it to name the address", f.Sentence)
	}
}

func TestAClosedPortSaysSo(t *testing.T) {
	cert := connecttest.NewCert(t)
	err := connector(cert).CheckIMAP(context.Background(),
		server(connecttest.ClosedPort(t), connect.Implicit, "misha", "hunter2"))
	failure(t, err, "refused")
}

// STARTTLS on a port that wants TLS from the first byte waits for a greeting that never comes.
func TestASilentServerTimesOut(t *testing.T) {
	cert := connecttest.NewCert(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := connector(cert).CheckIMAP(ctx, server(connecttest.Silent(t), connect.StartTLS, "misha", "hunter2"))
	failure(t, err, "timeout")
}

func TestPublicIsTheInternetOnly(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8":         true,
		"2606:4700::1111": true,
		"127.0.0.1":       false,
		"10.1.2.3":        false,
		"172.16.0.1":      false,
		"192.168.1.1":     false,
		"169.254.169.254": false,
		"100.64.0.1":      false,
		"0.0.0.0":         false,
		"::1":             false,
		"fe80::1":         false,
		"fd00::1":         false,
		"::ffff:10.0.0.1": false,
		"64:ff9b::a00:1":  false,
		"255.255.255.255": false,
		"198.51.100.7":    false,
	} {
		if got := connect.Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
}
