// Package connecttest runs real mail servers on loopback for tests: IMAP with one user and an
// INBOX, SMTP that accepts one sign-in, each secured the way a test asks.
//
// Real servers rather than fakes of the client, because what is being tested is whether the
// conversation works.
package connecttest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log"
	"math/big"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// Host is where every server here listens.
const Host = "127.0.0.1"

// Loopback is what a connector has to be told it may dial to reach them.
var Loopback = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}

// Cert is a self-signed certificate for Host, and a pool that trusts it.
type Cert struct {
	Config *tls.Config
	Pool   *x509.CertPool
}

func NewCert(t testing.TB) *Cert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: Host},
		IPAddresses:  []net.IP{net.ParseIP(Host)},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,

		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return &Cert{
		Config: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		Pool:   pool,
	}
}

// What an IMAP server here offers: Modern what any current server does, IDLE, MOVE and UIDPLUS
// among it, and Bare only IMAP4rev1, for what has to be refused or polled for without them.
var (
	Modern = imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}, imap.CapMove: {}, imap.CapUIDPlus: {}}
	Bare   = imap.CapSet{imap.CapIMAP4rev1: {}}
)

// IMAP serves username and password with an empty INBOX, secured as mode says: "implicit",
// "starttls", or "" for none at all, and offering what Modern does. It returns the port.
func IMAP(t testing.TB, cert *Cert, mode, username, password string) int {
	t.Helper()
	return IMAPWith(t, cert, mode, username, password, Modern)
}

// IMAPWith is IMAP offering caps.
func IMAPWith(t testing.TB, cert *Cert, mode, username, password string, caps imap.CapSet) int {
	t.Helper()
	return IMAPSaying(t, cert, mode, username, password, caps, nil)
}

// IMAPSaying is IMAPWith writing what passes between it and its clients to said, for a test
// that cares how many commands something took.
func IMAPSaying(t testing.TB, cert *Cert, mode, username, password string, caps imap.CapSet, said io.Writer) int {
	t.Helper()
	mem := imapmemserver.New()
	user := imapmemserver.NewUser(username, password)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)

	opts := &imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         caps,
		Logger:       log.New(io.Discard, "", 0),
		InsecureAuth: mode == "",
		DebugWriter:  said,
	}
	if mode == "starttls" {
		opts.TLSConfig = cert.Config
	}
	srv := imapserver.New(opts)
	ln := listen(t, cert, mode)
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return port(ln)
}

// SMTP accepts username and password over PLAIN and nothing else, secured as mode says.
func SMTP(t testing.TB, cert *Cert, mode, username, password string) int {
	t.Helper()
	return SMTPInto(t, cert, mode, username, password, nil)
}

// Outbox is what an SMTP server here was given to send.
type Outbox struct {
	mu   sync.Mutex
	sent []Delivery
}

// Delivery is one message handed to the server: who from, who to, and its bytes.
type Delivery struct {
	From string
	To   []string
	Data []byte
}

// Sent is every message given so far, in order.
func (o *Outbox) Sent() []Delivery {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.sent)
}

// Refused is the domain an SMTP server here will not deliver to, as a real one refuses an
// address it has no way to.
const Refused = "refused.example.com"

// SMTPInto is SMTP keeping what it is given in box.
func SMTPInto(t testing.TB, cert *Cert, mode, username, password string, box *Outbox) int {
	t.Helper()
	srv := smtp.NewServer(smtp.BackendFunc(func(*smtp.Conn) (smtp.Session, error) {
		return &smtpSession{username: username, password: password, box: box}, nil
	}))
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = mode == ""
	srv.ErrorLog = log.New(io.Discard, "", 0)
	if mode == "starttls" {
		srv.TLSConfig = cert.Config
	}
	ln := listen(t, cert, mode)
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return port(ln)
}

// Silent accepts connections and never says anything, which is what a TLS port looks like to a
// client waiting for a greeting.
func Silent(t testing.TB) int {
	t.Helper()
	ln := listen(t, nil, "")
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	return port(ln)
}

// Admin signs in to a server IMAP started with "implicit", as its user, for a test to change
// what is on it the way another mail client would.
func Admin(t testing.TB, cert *Cert, port int, username, password string) *imapclient.Client {
	t.Helper()
	conn, err := tls.Dial("tcp", net.JoinHostPort(Host, strconv.Itoa(port)),
		&tls.Config{RootCAs: cert.Pool, ServerName: Host})
	if err != nil {
		t.Fatal(err)
	}
	c := imapclient.New(conn, nil)
	t.Cleanup(func() { c.Close() })
	if err := c.Login(username, password).Wait(); err != nil {
		t.Fatal(err)
	}
	return c
}

// Append puts a message into a mailbox, arriving at the given time with the given flags.
func Append(t testing.TB, c *imapclient.Client, mailbox, raw string, at time.Time, flags ...imap.Flag) {
	t.Helper()
	raw = strings.ReplaceAll(raw, "\n", "\r\n")
	cmd := c.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: flags, Time: at})
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

// ClosedPort is a port nothing listens on.
func ClosedPort(t testing.TB) int {
	t.Helper()
	ln := listen(t, nil, "")
	p := port(ln)
	ln.Close()
	return p
}

func listen(t testing.TB, cert *Cert, mode string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(Host, "0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	if mode == "implicit" {
		return tls.NewListener(ln, cert.Config)
	}
	return ln
}

func port(ln net.Listener) int { return ln.Addr().(*net.TCPAddr).Port }

type smtpSession struct {
	username, password string
	box                *Outbox
	from               string
	to                 []string
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *smtpSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if username != s.username || password != s.password {
			return smtp.ErrAuthFailed
		}
		return nil
	}), nil
}

func (s *smtpSession) Mail(from string, _ *smtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *smtpSession) Rcpt(to string, _ *smtp.RcptOptions) error {
	if strings.HasSuffix(to, "@"+Refused) {
		return &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "No such user here"}
	}
	s.to = append(s.to, to)
	return nil
}

func (s *smtpSession) Data(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil || s.box == nil {
		return err
	}
	s.box.mu.Lock()
	defer s.box.mu.Unlock()
	s.box.sent = append(s.box.sent, Delivery{From: s.from, To: s.to, Data: data})
	return nil
}

func (s *smtpSession) Reset() {
	s.from, s.to = "", nil
}
func (s *smtpSession) Logout() error { return nil }
