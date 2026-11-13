package api

import (
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/message"
)

// sender is a newsletter's server, and what it was sent.
type sender struct {
	mu   sync.Mutex
	got  []string
	pool *x509.CertPool
	url  string
}

func newSender(t *testing.T) *sender {
	t.Helper()
	snd := &sender{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		snd.mu.Lock()
		snd.got = append(snd.got, r.Method+" "+r.URL.Path+" "+r.Header.Get("Content-Type")+" "+string(body))
		snd.mu.Unlock()
		if r.URL.Path == "/gone" {
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	snd.pool = x509.NewCertPool()
	snd.pool.AddCert(srv.Certificate())
	snd.url = srv.URL
	return snd
}

func (snd *sender) said() []string {
	snd.mu.Lock()
	defer snd.mu.Unlock()
	return append([]string(nil), snd.got...)
}

// One click is a POST that says so, to the link the message names, and nothing else.
func TestOneClickUnsubscribesWithAPost(t *testing.T) {
	s, _, c, cfg, inbox := withMail(t, 1)
	snd := newSender(t)
	s.web = webClient(connect.New(connecttest.Loopback).WithRoots(snd.pool))
	fake := &fakeMirror{unsubscribing: message.Unsubscribe{Link: snd.url + "/u/abc", OneClick: true}}
	s.mirror = fake
	path := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages/7-1"

	read := c.json(c.do("GET", path, ""))
	if u, _ := read["unsubscribe"].(map[string]any); u["link"] != snd.url+"/u/abc" || u["one_click"] != true {
		t.Fatalf("unsubscribe = %v", read["unsubscribe"])
	}

	if resp := c.do("POST", path+"/unsubscribe", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unsubscribe = %s %v", resp.Status, c.json(resp))
	}
	if got := snd.said(); len(got) != 1 || got[0] != "POST /u/abc application/x-www-form-urlencoded List-Unsubscribe=One-Click" {
		t.Errorf("the sender was sent %q", got)
	}

	fake.unsubscribing.Link = snd.url + "/gone"
	resp := c.do("POST", path+"/unsubscribe", "")
	if body := c.json(resp); resp.StatusCode != http.StatusBadGateway || !strings.Contains(body["message"].(string), "404") {
		t.Errorf("refused = %s %v", resp.Status, body)
	}
}

// Without one click there is nothing to send: the link is the sender's page, for the browser.
func TestWithoutOneClickNothingIsSent(t *testing.T) {
	s, _, c, cfg, inbox := withMail(t, 1)
	snd := newSender(t)
	s.web = webClient(connect.New(connecttest.Loopback).WithRoots(snd.pool))
	s.mirror = &fakeMirror{unsubscribing: message.Unsubscribe{Link: snd.url + "/u/abc"}}
	path := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages/7-1"

	if u, _ := c.json(c.do("GET", path, ""))["unsubscribe"].(map[string]any); u["one_click"] != false {
		t.Errorf("unsubscribe = %v", u)
	}
	if resp := c.do("POST", path+"/unsubscribe", ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("unsubscribe = %s", resp.Status)
	}
	if got := snd.said(); len(got) != 0 {
		t.Errorf("the sender was sent %q", got)
	}
}

// A message naming an address inside the network gets nothing from it, as with images.
func TestOneClickNeverReachesInside(t *testing.T) {
	s, _, c, cfg, inbox := withMail(t, 1)
	snd := newSender(t)
	s.web = webClient(connect.New(nil).WithRoots(snd.pool))
	s.mirror = &fakeMirror{unsubscribing: message.Unsubscribe{Link: snd.url + "/u/abc", OneClick: true}}
	path := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages/7-1/unsubscribe"
	if resp := c.do("POST", path, ""); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a loopback link = %s, want refused", resp.Status)
	}
	if got := snd.said(); len(got) != 0 {
		t.Errorf("the sender was sent %q", got)
	}
}

// A message that names no way has no unsubscribe at all.
func TestAMessageWithoutALinkHasNoUnsubscribe(t *testing.T) {
	s, _, c, cfg, inbox := withMail(t, 1)
	s.mirror = &fakeMirror{}
	read := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages/7-1", ""))
	if read["unsubscribe"] != nil || read["newsletter"] != false {
		t.Errorf("unsubscribe = %v, newsletter = %v", read["unsubscribe"], read["newsletter"])
	}
}
