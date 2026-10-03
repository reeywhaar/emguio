package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"emguio/internal/autoconfig"
)

type web map[string]string

func (s web) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := s[r.URL.String()]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

type noDNS struct{}

func (noDNS) LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error) {
	return "", nil, errors.New("no such host")
}
func (noDNS) LookupMX(context.Context, string) ([]*net.MX, error) {
	return nil, errors.New("no such host")
}

// The form is told a domain's servers as they were found, and where; or that none were.
func TestSettingsAreLookedUpForAnAddress(t *testing.T) {
	s, st := newServerStore(t, nil)
	s.finder = &autoconfig.Finder{Client: &http.Client{Transport: web{
		autoconfig.ISPDB + "example.com": `<clientConfig><emailProvider>
			<incomingServer type="imap"><hostname>imap.example.com</hostname><port>993</port><socketType>SSL</socketType><username>%EMAILADDRESS%</username></incomingServer>
			<outgoingServer type="smtp"><hostname>smtp.example.com</hostname><port>465</port><socketType>SSL</socketType><username>%EMAILADDRESS%</username></outgoingServer>
		</emailProvider></clientConfig>`,
	}}, Resolver: noDNS{}, ISPDB: autoconfig.ISPDB}
	c := signIn(t, s, st)

	resp := c.do("POST", "/api/email-configs/autoconfig", `{"email":"ann@example.com"}`)
	got := c.json(resp)
	in, _ := got["incoming"].(map[string]any)
	out, _ := got["outgoing"].(map[string]any)
	if resp.StatusCode != http.StatusOK || got["found"] != true || got["source"] != "mozilla" || got["domain"] != "example.com" ||
		in["host"] != "imap.example.com" || in["tls"] != "implicit" || in["username"] != "ann@example.com" || in["protocol"] != "imap" ||
		out["host"] != "smtp.example.com" || out["port"] != float64(465) {
		t.Errorf("found = %s %v", resp.Status, got)
	}
	if got := c.json(c.do("POST", "/api/email-configs/autoconfig", `{"email":"ann@nowhere.example"}`)); got["found"] != false {
		t.Errorf("nothing = %v", got)
	}
	s.finder.Client.Transport.(web)[autoconfig.ISPDB+"outlook.com"] = `<clientConfig><emailProvider>
		<incomingServer type="imap"><hostname>outlook.office365.com</hostname><port>993</port><socketType>SSL</socketType><authentication>OAuth2</authentication></incomingServer>
	</emailProvider></clientConfig>`
	if got := c.json(c.do("POST", "/api/email-configs/autoconfig", `{"email":"ann@outlook.com"}`)); got["found"] != false || got["oauth_only"] != true {
		t.Errorf("OAuth only = %v", got)
	}
	if resp := c.do("POST", "/api/email-configs/autoconfig", `{"email":"ann"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("not an address = %s", resp.Status)
	}
}
