package autoconfig

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

// sites is the web as a test has it: a body for each address, 404 for the rest.
type sites map[string]string

func (s sites) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := s[r.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

// dns is the DNS as a test has it.
type dns struct {
	srv map[string][]*net.SRV
	mx  map[string][]*net.MX
}

func (d dns) LookupSRV(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if r, ok := d.srv["_"+service+"._"+proto+"."+name]; ok {
		return "", r, nil
	}
	return "", nil, errors.New("no such host")
}

func (d dns) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if r, ok := d.mx[name]; ok {
		return r, nil
	}
	return nil, errors.New("no such host")
}

func finder(web sites, names dns) *Finder {
	return &Finder{Client: &http.Client{Transport: web}, Resolver: names, ISPDB: ISPDB}
}

// A settings file as Mozilla's list writes one: a POP3 server first, an IMAP server with no TLS,
// and one that signs in only with OAuth, none of which emguio can use.
const listed = `<?xml version="1.0"?>
<clientConfig version="1.1">
  <emailProvider id="example.com">
    <domain>example.com</domain>
    <incomingServer type="pop3">
      <hostname>pop.example.com</hostname><port>995</port><socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>plain.example.com</hostname><port>143</port><socketType>plain</socketType>
      <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>oauth.example.com</hostname><port>993</port><socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username><authentication>OAuth2</authentication>
    </incomingServer>
    <incomingServer type="imap">
      <hostname>imap.%EMAILDOMAIN%</hostname><port>993</port><socketType>SSL</socketType>
      <username>%EMAILLOCALPART%</username>
      <authentication>OAuth2</authentication><authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.example.com</hostname><port>587</port><socketType>STARTTLS</socketType>
      <username>%EMAILADDRESS%</username><authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>`

func TestASettingsFileGivesWhatEmguioCanUse(t *testing.T) {
	got, err := Parse([]byte(listed), "ann@example.com")
	if got == nil || err != nil {
		t.Fatalf("nothing found: %v", err)
	}
	if got.Incoming != (Server{Host: "imap.example.com", Port: 993, TLS: "implicit", Username: "ann"}) {
		t.Errorf("incoming = %+v", got.Incoming)
	}
	if got.Outgoing == nil || *got.Outgoing != (Server{Host: "smtp.example.com", Port: 587, TLS: "starttls", Username: "ann@example.com"}) {
		t.Errorf("outgoing = %+v", got.Outgoing)
	}
	if got, err := Parse([]byte(`<clientConfig><emailProvider><incomingServer type="pop3"/></emailProvider></clientConfig>`), "ann@example.com"); got != nil || err != nil {
		t.Errorf("found a server emguio cannot use: %+v, %v", got, err)
	}
	if got, err := Parse([]byte("<html>"), "ann@example.com"); got != nil || err != nil {
		t.Errorf("found settings in a page that holds none: %+v, %v", got, err)
	}
}

// A provider that signs in only with OAuth says so, rather than having nothing to say.
func TestAProviderOfOAuthAloneSaysSo(t *testing.T) {
	oauth := `<clientConfig><emailProvider>
		<incomingServer type="imap"><hostname>outlook.office365.com</hostname><port>993</port><socketType>SSL</socketType><authentication>OAuth2</authentication></incomingServer>
	</emailProvider></clientConfig>`
	if got, err := Parse([]byte(oauth), "ann@outlook.com"); got != nil || !errors.Is(err, ErrOAuthOnly) {
		t.Errorf("parse = %+v, %v", got, err)
	}
	got, err := finder(sites{ISPDB + "outlook.com": oauth}, dns{}).Find(context.Background(), "ann@outlook.com")
	if got != nil || !errors.Is(err, ErrOAuthOnly) {
		t.Errorf("find = %+v, %v", got, err)
	}
}

// The provider's own settings are trusted first, then Mozilla's list, then the domain's DNS, then
// Mozilla's list for whoever its MX names.
func TestSettingsAreFoundWhereTheyAreMostTrusted(t *testing.T) {
	provider := strings.ReplaceAll(listed, "imap.%EMAILDOMAIN%", "own.%EMAILDOMAIN%")
	srv := dns{srv: map[string][]*net.SRV{
		"_imaps._tcp.example.com":      {{Target: "imap.dns.example.com.", Port: 993, Priority: 10}, {Target: "first.example.com.", Port: 993, Priority: 1}},
		"_submission._tcp.example.com": {{Target: "smtp.dns.example.com.", Port: 587}},
	}}
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		web    sites
		names  dns
		host   string
		source string
		domain string
	}{
		{"provider", sites{"https://autoconfig.example.com/mail/config-v1.1.xml": provider, ISPDB + "example.com": listed}, srv, "own.example.com", FromProvider, "example.com"},
		{"well-known", sites{"https://example.com/.well-known/autoconfig/mail/config-v1.1.xml": provider}, dns{}, "own.example.com", FromProvider, "example.com"},
		{"mozilla", sites{ISPDB + "example.com": listed}, srv, "imap.example.com", FromMozilla, "example.com"},
		{"dns", sites{}, srv, "first.example.com", FromDNS, "example.com"},
		{"mx", sites{ISPDB + "mailhost.net": strings.ReplaceAll(listed, "%EMAILDOMAIN%", "mailhost.net")},
			dns{mx: map[string][]*net.MX{"example.com": {{Host: "mx2.eu.mailhost.net.", Pref: 20}, {Host: "mx1.eu.mailhost.net.", Pref: 10}}}},
			"imap.mailhost.net", FromMozilla, "mailhost.net"},
	} {
		got, err := finder(c.web, c.names).Find(ctx, "Ann <ann@Example.com>")
		if err != nil || got == nil {
			t.Errorf("%s: %+v, %v", c.name, got, err)
			continue
		}
		if got.Incoming.Host != c.host || got.Source != c.source || got.Domain != c.domain {
			t.Errorf("%s: found %+v", c.name, got)
		}
	}

	got, _ := finder(sites{}, srv).Find(ctx, "ann@example.com")
	if got.Outgoing == nil || *got.Outgoing != (Server{Host: "smtp.dns.example.com", Port: 587, TLS: "starttls", Username: "ann@example.com"}) {
		t.Errorf("outgoing from DNS = %+v", got.Outgoing)
	}
	if got, err := finder(sites{}, dns{}).Find(ctx, "ann@example.com"); got != nil || err != nil {
		t.Errorf("nothing anywhere = %+v, %v", got, err)
	}
	for _, bad := range []string{"ann", "ann@localhost", ""} {
		if _, err := finder(sites{}, dns{}).Find(ctx, bad); !errors.Is(err, ErrNotAnAddress) {
			t.Errorf("%q = %v", bad, err)
		}
	}
}
