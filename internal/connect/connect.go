// Package connect is how emguio reaches a mail server: a dialer that refuses addresses on the
// inside, TLS, and signing in. Nothing else opens a socket to a host somebody typed.
//
// Every failure it returns is a *Failure, written for whoever typed the address.
package connect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// How a server is secured. Never neither: a password does not cross the network in the clear.
const (
	Implicit = "implicit"
	StartTLS = "starttls"
)

// Timeout bounds one check from dialing to signing out.
const Timeout = 15 * time.Second

// Server is somewhere to sign in.
type Server struct {
	Host     string
	Port     int
	TLS      string
	Username string
	Password string
}

func (s Server) addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Connector dials mail servers.
type Connector struct {
	allow []netip.Prefix
	// roots is nil for the system's own.
	roots   *x509.CertPool
	timeout time.Duration
}

// New is a connector that dials public addresses, and the private ones in allow.
func New(allow []netip.Prefix) *Connector {
	return &Connector{allow: allow, timeout: Timeout}
}

// WithRoots trusts these certificates rather than the system's. Tests only.
func (c *Connector) WithRoots(pool *x509.CertPool) *Connector {
	out := *c
	out.roots = pool
	return &out
}

// dial opens a connection that has passed the screen, with TLS already up when it is implicit.
//
// The connection is closed when ctx ends, which is what bounds a check: the protocol libraries
// set read and write deadlines of their own, and a deadline set here would be overwritten by the
// first command. Call release once the connection is closed some other way.
func (c *Connector) dial(ctx context.Context, s Server) (conn net.Conn, release func() bool, err error) {
	d := net.Dialer{Timeout: c.timeout, ControlContext: c.screen}
	raw, err := d.DialContext(ctx, "tcp", s.addr())
	if err != nil {
		return nil, nil, dialFailure(s, err)
	}
	release = context.AfterFunc(ctx, func() { raw.Close() })
	if s.TLS != Implicit {
		return raw, release, nil
	}
	tc := tls.Client(raw, c.tlsConfig(s.Host))
	if err := tc.HandshakeContext(ctx); err != nil {
		release()
		raw.Close()
		return nil, nil, tlsFailure(s, err)
	}
	return tc, release, nil
}

// failed is f, unless the check ran out of time: once ctx has closed the connection, whatever
// the library says next is about the close rather than about the server.
func failed(ctx context.Context, s Server, f *Failure) *Failure {
	if ctx.Err() != nil {
		return timeoutFailure(s)
	}
	return f
}

func (c *Connector) tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: c.roots, MinVersion: tls.VersionTLS12}
}

// screen runs after the name has resolved and before the socket connects, on the address
// actually being dialed. Checking here rather than resolving first means a name that answers
// differently the second time it is asked still cannot reach inside.
func (c *Connector) screen(_ context.Context, _, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return &screened{address: address}
	}
	if !c.allowed(ap.Addr().Unmap()) {
		return &screened{address: ap.Addr().Unmap().String()}
	}
	return nil
}

func (c *Connector) allowed(ip netip.Addr) bool {
	if Public(ip) {
		return true
	}
	for _, p := range c.allow {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

type screened struct{ address string }

func (e *screened) Error() string { return "screened: " + e.address }

// Public reports whether ip is somewhere on the internet rather than inside a network: not
// loopback, private, link-local, shared, documentation or reserved.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// reserved is what IsGlobalUnicast lets through and is not the internet anyway. The translation
// prefixes are here because each can carry a private IPv4 address inside it.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

// Failure is why a server could not be reached or signed in to: a sentence for whoever typed
// its address, and a class for the log.
type Failure struct {
	Class    string
	Sentence string
}

func (f *Failure) Error() string { return f.Sentence }

func fail(class, format string, a ...any) *Failure {
	return &Failure{Class: class, Sentence: fmt.Sprintf(format, a...)}
}

func dialFailure(s Server, err error) *Failure {
	var sc *screened
	var dns *net.DNSError
	switch {
	case errors.As(err, &sc):
		return fail("private", "%s is at %s, a private address, and emguio connects only to public ones.", s.Host, sc.address)
	case errors.As(err, &dns) && dns.IsNotFound:
		return fail("dns", "There is no host called %s.", s.Host)
	case errors.As(err, &dns):
		return fail("dns", "%s could not be looked up just now.", s.Host)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fail("refused", "%s refused the connection. Check the port.", s.addr())
	case timedOut(err):
		return timeoutFailure(s)
	default:
		return fail("network", "%s could not be reached.", s.addr())
	}
}

func tlsFailure(s Server, err error) *Failure {
	var host x509.HostnameError
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case errors.As(err, &host):
		return fail("tls", "The certificate %s presented is not for %s.", s.addr(), s.Host)
	case errors.As(err, &unknown):
		return fail("tls", "The certificate %s presented is not signed by anybody this server trusts.", s.addr())
	case errors.As(err, &invalid):
		return fail("tls", "The certificate %s presented is not valid: %s.", s.addr(), invalid.Error())
	case errors.As(err, &record):
		return fail("tls", "%s did not answer with TLS. If the port expects STARTTLS, choose that instead.", s.addr())
	case timedOut(err):
		return timeoutFailure(s)
	default:
		return fail("tls", "The TLS handshake with %s failed.", s.addr())
	}
}

func timeoutFailure(s Server) *Failure {
	return fail("timeout", "%s did not answer in time.%s", s.addr(), tlsHint(s))
}

// tlsHint guesses at the commonest reason a server says nothing sensible: STARTTLS against a port
// that wants TLS from the first byte, where the server waits for a handshake and the client for
// a greeting.
func tlsHint(s Server) string {
	if s.TLS == StartTLS && (s.Port == 993 || s.Port == 465) {
		return fmt.Sprintf(" Port %d usually expects TLS from the start rather than STARTTLS.", s.Port)
	}
	return ""
}

// netFailure is the network failing partway through a protocol, rather than the protocol
// refusing.
func netFailure(s Server, err error) (*Failure, bool) {
	var f *Failure
	var tlsErr *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var netErr net.Error
	switch {
	case errors.As(err, &f):
		return f, true
	case errors.As(err, &tlsErr), errors.As(err, &record):
		return tlsFailure(s, err), true
	case timedOut(err):
		return timeoutFailure(s), true
	case errors.As(err, &netErr), errors.Is(err, syscall.ECONNRESET), errors.Is(err, net.ErrClosed):
		return fail("network", "%s closed the connection.", s.addr()), true
	}
	return nil, false
}

func timedOut(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}

// Said is what a server wrote, made safe to put in a sentence: valid UTF-8, one line, and short.
func Said(text string) string {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if r == '�' || r < 0x20 || r == 0x7f {
			r = ' '
		}
		out = append(out, r)
	}
	if len(out) > 200 {
		out = append(out[:200], '…')
	}
	return string(out)
}
