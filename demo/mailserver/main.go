// Command mailserver is the demo's mail server: IMAP and SMTP over TLS, and the settings a
// provider publishes, for any address on one shared password, held in memory and gone when it
// stops. Mail sent to an address it has a mailbox for lands in that mailbox's INBOX. See
// demo/README.md.
//
//	mailserver          serve
//	mailserver ready    whether the one running answers IMAP with the certificate it handed out
package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// The names the servers answer as, which the demo's network gives this container.
var names = []string{"imap.example.com", "smtp.example.com", "autoconfig.example.com"}

// Every mailbox a new address starts with, which emguio knows by name.
var folders = []string{"Drafts", "Sent", "Archive", "Junk", "Trash"}

func main() {
	password := env("MAIL_PASSWORD", "demodemo")
	ca := filepath.Join(env("CERT_DIR", "/certs"), "ca.pem")
	if len(os.Args) > 1 && os.Args[1] == "ready" {
		if err := ready(ca); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cert, err := certify(ca)
	if err != nil {
		log.Fatalf("making the certificates: %v", err)
	}
	keys := &tls.Config{Certificates: []tls.Certificate{cert}}
	skip, err := rand.Int(rand.Reader, big.NewInt(5000))
	if err != nil {
		log.Fatalf("choosing where UIDVALIDITY starts: %v", err)
	}
	m := &mail{password: password, users: map[string]*imapmemserver.User{}, skip: int(skip.Int64()) + 1}

	go serveIMAP(m, keys)
	go serveSMTP(m, keys)
	go serveSettings(keys)
	log.Printf("serving IMAP on 993, SMTP on 465 and settings on 443, for any address with the password %q", password)
	select {}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// mail is every mailbox, made the first time its address signs in or is written to by the demo.
type mail struct {
	password string
	// skip is how many UIDVALIDITYs a new mailbox passes over first, chosen for this start: emguio
	// keeps what it read of the last start, and a mailbox numbered as the last one's would be
	// taken for it, its new mail under the old mail's numbers.
	skip  int
	mu    sync.Mutex
	users map[string]*imapmemserver.User
}

// user is the mailbox of address, made on its first sign-in with the password.
func (m *mail) user(address, password string) (*imapmemserver.User, error) {
	if password != m.password {
		return nil, imapserver.ErrAuthFailed
	}
	key := strings.ToLower(address)
	m.mu.Lock()
	defer m.mu.Unlock()
	if u := m.users[key]; u != nil {
		return u, nil
	}
	u := imapmemserver.NewUser(address, password)
	// Each mailbox made takes the next UIDVALIDITY, and there is no other way to say where they
	// start.
	for range m.skip {
		if err := u.Create(".skip", nil); err != nil {
			return nil, err
		}
		if err := u.Delete(".skip"); err != nil {
			return nil, err
		}
	}
	for _, name := range append([]string{"INBOX"}, folders...) {
		if err := u.Create(name, nil); err != nil {
			return nil, err
		}
	}
	m.users[key] = u
	log.Printf("a mailbox for %s", address)
	return u, nil
}

// known is the mailbox of address if it has one.
func (m *mail) known(address string) *imapmemserver.User {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[strings.ToLower(address)]
}

// --- IMAP --------------------------------------------------------------------------------------

func serveIMAP(m *mail, keys *tls.Config) {
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return &session{mail: m}, nil, nil
		},
		// What a current server offers and emguio uses: IDLE, MOVE, and UIDPLUS.
		Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}, imap.CapMove: {}, imap.CapUIDPlus: {}},
	})
	ln, err := tls.Listen("tcp", ":993", keys)
	if err != nil {
		log.Fatalf("IMAP: %v", err)
	}
	log.Fatalf("IMAP: %v", srv.Serve(ln))
}

// session is one IMAP connection: nobody's until it signs in, then that address's.
type session struct {
	mail *mail
	*imapmemserver.UserSession
}

func (s *session) Login(username, password string) error {
	u, err := s.mail.user(username, password)
	if err != nil {
		return err
	}
	s.UserSession = imapmemserver.NewUserSession(u)
	return nil
}

func (s *session) Close() error {
	if s.UserSession == nil {
		return nil
	}
	return s.UserSession.Close()
}

// --- SMTP --------------------------------------------------------------------------------------

func serveSMTP(m *mail, keys *tls.Config) {
	srv := smtp.NewServer(backend{m})
	srv.Addr = ":465"
	srv.Domain = "smtp.example.com"
	srv.TLSConfig = keys
	srv.MaxMessageBytes = 50 << 20
	log.Fatalf("SMTP: %v", srv.ListenAndServeTLS())
}

type backend struct{ mail *mail }

func (b backend) NewSession(*smtp.Conn) (smtp.Session, error) {
	return &delivery{mail: b.mail}, nil
}

// delivery is one SMTP connection: signed in with any address and the password, and each
// message put in the INBOX of every recipient that has a mailbox here. The rest go nowhere.
type delivery struct {
	mail   *mail
	signed bool
	to     []string
}

func (d *delivery) AuthMechanisms() []string { return []string{sasl.Plain} }

func (d *delivery) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, username, password string) error {
		if _, err := d.mail.user(username, password); err != nil {
			return err
		}
		d.signed = true
		return nil
	}), nil
}

func (d *delivery) Mail(string, *smtp.MailOptions) error {
	if !d.signed {
		return smtp.ErrAuthRequired
	}
	return nil
}

func (d *delivery) Rcpt(to string, _ *smtp.RcptOptions) error {
	d.to = append(d.to, to)
	return nil
}

func (d *delivery) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	for _, to := range d.to {
		u := d.mail.known(to)
		if u == nil {
			log.Printf("mail for %s goes nowhere: no mailbox here", to)
			continue
		}
		if _, err := u.Append("INBOX", literal{bytes.NewReader(raw)}, &imap.AppendOptions{Time: time.Now()}); err != nil {
			return err
		}
		log.Printf("mail delivered to %s", to)
	}
	return nil
}

func (d *delivery) Reset()        { d.to = nil }
func (d *delivery) Logout() error { return nil }

// literal is a message as APPEND takes it: its bytes and how many.
type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return l.Reader.Size() }

// --- settings ----------------------------------------------------------------------------------

// The settings a provider publishes for Thunderbird, which emguio reads first: an address here
// fills in the servers on its own.
const settings = `<?xml version="1.0"?>
<clientConfig version="1.1">
  <emailProvider id="example.com">
    <domain>example.com</domain>
    <incomingServer type="imap">
      <hostname>imap.example.com</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.example.com</hostname>
      <port>465</port>
      <socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>
`

func serveSettings(keys *tls.Config) {
	srv := &http.Server{
		Addr:              ":443",
		TLSConfig:         keys,
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "/config-v1.1.xml") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/xml")
			io.WriteString(w, settings)
		}),
	}
	log.Fatalf("settings: %v", srv.ListenAndServeTLS("", ""))
}

// --- certificates ------------------------------------------------------------------------------

// certify makes a certificate authority for this run, writes it where emguio and the seeder read
// it, and makes the servers' certificate from it.
func certify(caPath string) (tls.Certificate, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	now := time.Now()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "emguio demo"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(30 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return tls.Certificate{}, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := os.MkdirAll(filepath.Dir(caPath), 0o755); err != nil {
		return tls.Certificate{}, err
	}
	// Whole or not at all: emguio and the seeder start once it answers, and read this then.
	tmp := caPath + ".new"
	if err := os.WriteFile(tmp, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.Rename(tmp, caPath); err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// ready is whether the server running here answers IMAP with a certificate the authority at
// caPath made: so one left over from an earlier run does not pass.
func ready(caPath string) error {
	pemBytes, err := os.ReadFile(caPath)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return fmt.Errorf("%s holds no certificate", caPath)
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", "127.0.0.1:993",
		&tls.Config{RootCAs: pool, ServerName: names[0]})
	if err != nil {
		return err
	}
	return conn.Close()
}
