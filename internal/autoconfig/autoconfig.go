// Package autoconfig finds a mail domain's servers from an address, to fill in the form a mail
// account is added with: the provider's own settings, Mozilla's list of providers, the domain's
// DNS. A pre-fill and nothing more — what it finds is tested before it is saved. See
// docs/email-configs.md.
package autoconfig

import (
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"net/http"
	"net/mail"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// Where settings come from.
const (
	FromProvider = "provider"
	FromMozilla  = "mozilla"
	FromDNS      = "dns"
)

// Server is one server as found: TLS "implicit" or "starttls", never neither.
type Server struct {
	Host     string
	Port     int
	TLS      string
	Username string
}

// Found is a domain's servers. Source says where they were found, and Domain for which domain:
// the address's, or the one its MX names, for a domain whose mail another provider handles.
type Found struct {
	Incoming Server
	Outgoing *Server
	Source   string
	Domain   string
}

// Resolver is the DNS a Finder asks.
type Resolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

// ISPDB is Mozilla's list of providers, by domain.
const ISPDB = "https://autoconfig.thunderbird.net/v1.1/"

// Finder looks up a domain's servers. Client is the only way it reaches the web, so it is the
// screened one: a provider's own settings live at an address a user typed.
type Finder struct {
	Client   *http.Client
	Resolver Resolver
	// ISPDB is Mozilla's list, by domain; a field so a test can stand in for it.
	ISPDB string
}

// Timeout bounds a whole lookup.
const Timeout = 8 * time.Second

// readMax bounds a settings file. Real ones are a few kilobytes.
const readMax = 256 << 10

// ErrNotAnAddress is an address with no domain to look up.
var ErrNotAnAddress = errors.New("autoconfig: not an address")

// ErrOAuthOnly is a provider whose servers take only OAuth, which emguio does not sign in with.
var ErrOAuthOnly = errors.New("autoconfig: oauth only")

// Find looks for the servers of address's domain everywhere at once, and answers with the most
// trusted that knew: the provider's own settings, then Mozilla's list, then the domain's DNS, then
// Mozilla's list for the provider its MX names. Nil when none did, and ErrOAuthOnly when what was
// found takes only OAuth.
func (f *Finder) Find(ctx context.Context, address string) (*Found, error) {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return nil, ErrNotAnAddress
	}
	address = parsed.Address
	at := strings.LastIndex(address, "@")
	domain, err := idna.Lookup.ToASCII(strings.ToLower(address[at+1:]))
	if err != nil || !strings.Contains(domain, ".") {
		return nil, ErrNotAnAddress
	}
	address = address[:at+1] + domain
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	ways := []func() (*Found, error){
		func() (*Found, error) {
			return f.first(ctx, address, domain, FromProvider,
				"https://autoconfig."+domain+"/mail/config-v1.1.xml",
				"https://"+domain+"/.well-known/autoconfig/mail/config-v1.1.xml")
		},
		func() (*Found, error) { return f.first(ctx, address, domain, FromMozilla, f.ISPDB+domain) },
		func() (*Found, error) { return f.srv(ctx, address, domain), nil },
		func() (*Found, error) { return f.mx(ctx, address, domain) },
	}
	type answer struct {
		found *Found
		err   error
	}
	answers := make([]chan answer, len(ways))
	for i, way := range ways {
		answers[i] = make(chan answer, 1)
		go func() {
			found, err := way()
			answers[i] <- answer{found, err}
		}()
	}
	var oauth error
	for _, c := range answers {
		a := <-c
		if a.found != nil {
			return a.found, nil
		}
		if a.err != nil {
			oauth = a.err
		}
	}
	return nil, oauth
}

// first is the first of urls that holds usable settings, read as for domain; ErrOAuthOnly when
// one held settings that take only OAuth and none held any emguio can use.
func (f *Finder) first(ctx context.Context, address, domain, source string, urls ...string) (*Found, error) {
	var oauth error
	for _, url := range urls {
		out, err := f.read(ctx, address, url)
		if out != nil {
			out.Source, out.Domain = source, domain
			return out, nil
		}
		if err != nil {
			oauth = err
		}
	}
	return nil, oauth
}

// read fetches one settings file, Thunderbird's format, and takes what emguio can use from it.
func (f *Finder) read(ctx context.Context, address, url string) (*Found, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, readMax))
	if err != nil {
		return nil, nil
	}
	return Parse(body, address)
}

type clientConfig struct {
	Providers []struct {
		Incoming []serverXML `xml:"incomingServer"`
		Outgoing []serverXML `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

type serverXML struct {
	Type           string   `xml:"type,attr"`
	Hostname       string   `xml:"hostname"`
	Port           int      `xml:"port"`
	SocketType     string   `xml:"socketType"`
	Username       string   `xml:"username"`
	Authentication []string `xml:"authentication"`
}

// Parse reads a settings file for address: the first IMAP server and the first SMTP server it
// lists that are reached over TLS and signed in to with a password. Nil without such an IMAP
// server, and ErrOAuthOnly when the IMAP servers it lists take only OAuth.
func Parse(body []byte, address string) (*Found, error) {
	var cfg clientConfig
	if err := xml.Unmarshal(body, &cfg); err != nil {
		return nil, nil
	}
	var oauth error
	for _, p := range cfg.Providers {
		in, onlyOAuth := usable(p.Incoming, "imap", address)
		if in == nil {
			if onlyOAuth {
				oauth = ErrOAuthOnly
			}
			continue
		}
		out, _ := usable(p.Outgoing, "smtp", address)
		return &Found{Incoming: *in, Outgoing: out}, nil
	}
	return nil, oauth
}

// usable is the first of servers of kind emguio can sign in to, and whether there were any over
// TLS that take only OAuth.
func usable(servers []serverXML, kind, address string) (*Server, bool) {
	oauth := false
	for _, s := range servers {
		if !strings.EqualFold(s.Type, kind) || s.Port < 1 || s.Port > 65535 {
			continue
		}
		var tls string
		switch strings.ToUpper(s.SocketType) {
		case "SSL":
			tls = "implicit"
		case "STARTTLS":
			tls = "starttls"
		default:
			continue
		}
		if !password(s.Authentication) {
			oauth = oauth || slices.Contains(s.Authentication, "OAuth2")
			continue
		}
		host := strings.TrimSuffix(strings.TrimSpace(fill(s.Hostname, address)), ".")
		if host == "" {
			continue
		}
		return &Server{Host: host, Port: s.Port, TLS: tls, Username: fill(s.Username, address)}, false
	}
	return nil, oauth
}

// password is whether a server takes a password: said so, or nothing said at all.
func password(methods []string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, m := range methods {
		switch strings.TrimSpace(m) {
		case "password-cleartext", "password-encrypted":
			return true
		}
	}
	return false
}

// fill puts address into a settings file's placeholders.
func fill(s, address string) string {
	at := strings.LastIndex(address, "@")
	return strings.NewReplacer(
		"%EMAILADDRESS%", address,
		"%EMAILLOCALPART%", address[:at],
		"%EMAILDOMAIN%", address[at+1:],
	).Replace(strings.TrimSpace(s))
}

// srv is the servers the domain names in DNS, RFC 6186 and 8314: implicit TLS first.
func (f *Finder) srv(ctx context.Context, address, domain string) *Found {
	in := f.service(ctx, domain, address, [2]string{"imaps", "implicit"}, [2]string{"imap", "starttls"})
	if in == nil {
		return nil
	}
	return &Found{
		Incoming: *in,
		Outgoing: f.service(ctx, domain, address, [2]string{"submissions", "implicit"}, [2]string{"submission", "starttls"}),
		Source:   FromDNS,
		Domain:   domain,
	}
}

// service is the first of services, each a name and the TLS it means, the domain offers.
func (f *Finder) service(ctx context.Context, domain, address string, services ...[2]string) *Server {
	for _, s := range services {
		_, records, err := f.Resolver.LookupSRV(ctx, s[0], "tcp", domain)
		if err != nil || len(records) == 0 {
			continue
		}
		r := slices.MinFunc(records, func(a, b *net.SRV) int { return cmp.Compare(a.Priority, b.Priority) })
		host := strings.TrimSuffix(r.Target, ".")
		// "." is a domain saying it does not offer the service.
		if host == "" || r.Port == 0 {
			continue
		}
		return &Server{Host: host, Port: int(r.Port), TLS: s[1], Username: address}
	}
	return nil
}

// mx is Mozilla's settings for the provider that handles the domain's mail, by its MX: a domain
// of one's own at Google or Microsoft, say.
func (f *Finder) mx(ctx context.Context, address, domain string) (*Found, error) {
	records, err := f.Resolver.LookupMX(ctx, domain)
	if err != nil || len(records) == 0 {
		return nil, nil
	}
	first := slices.MinFunc(records, func(a, b *net.MX) int { return cmp.Compare(a.Pref, b.Pref) })
	provider, err := publicsuffix.EffectiveTLDPlusOne(strings.TrimSuffix(first.Host, "."))
	if err != nil || provider == domain {
		return nil, nil
	}
	return f.first(ctx, address, provider, FromMozilla, f.ISPDB+provider)
}
