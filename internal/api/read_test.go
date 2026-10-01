package api

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/mirror"
)

var pixel = base64.StdEncoding.EncodeToString([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"))

var rich = strings.ReplaceAll(`From: Alice <alice@example.com>
To: misha@example.com
Subject: Message A
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=outer

--outer
Content-Type: multipart/related; boundary=inner

--inner
Content-Type: text/html; charset=utf-8

<p onclick="x()">Hi <img src="cid:logo@x"> <img src="https://track.example/p.gif"></p>
--inner
Content-Type: image/gif
Content-ID: <logo@x>
Content-Disposition: inline; filename=logo.gif
Content-Transfer-Encoding: base64

`+pixel+`
--inner--
--outer
Content-Type: text/html
Content-Disposition: attachment; filename="page.html"

<script>alert(1)</script>
--outer--
`, "\n", "\r\n")

type readView struct {
	Subject      string `json:"subject"`
	Text         string `json:"text"`
	HTML         string `json:"html"`
	RemoteImages int    `json:"remote_images"`
	Parts        []struct {
		Index  int    `json:"index"`
		Name   string `json:"name"`
		Type   string `json:"type"`
		Listed bool   `json:"listed"`
	} `json:"parts"`
}

func reading(t *testing.T) (*Server, *client, *fakeMirror, string, string) {
	t.Helper()
	s, _, c, cfg, inbox := withMail(t, 1)
	fake := &fakeMirror{raw: []byte(rich)}
	s.mirror = fake
	page := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))
	id := page["messages"].([]any)[0].(map[string]any)["id"].(string)
	return s, c, fake, cfg, id
}

func TestAMessageIsFetchedOnceAndThenReadFromTheStore(t *testing.T) {
	_, c, fake, cfg, id := reading(t)
	for range 2 {
		resp := c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("read = %s %v", resp.Status, c.json(resp))
		}
	}
	if fake.fetched != 1 {
		t.Errorf("fetched %d times, want once", fake.fetched)
	}
}

func TestAReadMessageIsSafeAndItsImagesPointHere(t *testing.T) {
	_, c, _, cfg, id := reading(t)
	var got readView
	json.NewDecoder(c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id, "").Body).Decode(&got)

	if strings.Contains(got.HTML, "onclick") || strings.Contains(got.HTML, "track.example") {
		t.Errorf("html = %s", got.HTML)
	}
	if got.RemoteImages != 1 {
		t.Errorf("remote images = %d", got.RemoteImages)
	}
	if !strings.Contains(got.HTML, `src="/api/email-configs/`+cfg+`/messages/`+id+`/parts/`) {
		t.Errorf("the carried image does not point at its part: %s", got.HTML)
	}
	var listed []string
	for _, p := range got.Parts {
		if p.Listed {
			listed = append(listed, p.Name)
		}
	}
	if strings.Join(listed, ",") != "page.html" {
		t.Errorf("listed parts = %v", listed)
	}

	var shown readView
	json.NewDecoder(c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id+"?images=1", "").Body).Decode(&shown)
	if !strings.Contains(shown.HTML, `src="/api/proxy?`) {
		t.Errorf("asked for images, html = %s", shown.HTML)
	}
}

// An attachment is the sender's file, and it must not become a page of this origin.
func TestAPartCannotActAsAPageHere(t *testing.T) {
	_, c, _, cfg, id := reading(t)
	var got readView
	json.NewDecoder(c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id, "").Body).Decode(&got)
	byName := map[string]int{}
	for _, p := range got.Parts {
		byName[p.Name] = p.Index
	}

	page := c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id+"/parts/"+strconv.Itoa(byName["page.html"]), "")
	if page.Header.Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(page.Header.Get("Content-Disposition"), "attachment") ||
		page.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(page.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("an HTML attachment was served as %v", page.Header)
	}

	logo := c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id+"/parts/"+strconv.Itoa(byName["logo.gif"]), "")
	if logo.Header.Get("Content-Type") != "image/gif" || !strings.HasPrefix(logo.Header.Get("Content-Disposition"), "inline") {
		t.Errorf("an image was served as %v", logo.Header)
	}

	if resp := c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id+"/parts/99", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a part that is not there = %s", resp.Status)
	}
}

func TestReadingStoresAPreviewForTheList(t *testing.T) {
	_, c, _, cfg, id := reading(t)
	c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id, "")
	boxes := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	inbox := boxes[0].(map[string]any)["id"].(string)
	msg := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))["messages"].([]any)[0].(map[string]any)
	if msg["preview"] != "Hi" {
		t.Errorf("preview = %q", msg["preview"])
	}
}

func TestAMessageGoneFromTheServerSaysSoAndAsksForALook(t *testing.T) {
	_, c, fake, cfg, id := reading(t)
	fake.err = mirror.ErrGone
	resp := c.do("GET", "/api/email-configs/"+cfg+"/messages/"+id, "")
	if resp.StatusCode != http.StatusNotFound || c.json(resp)["code"] != CodeGone {
		t.Fatalf("gone = %s", resp.Status)
	}
	if len(fake.refreshed) != 1 {
		t.Error("the list was not brought up to date")
	}
}

func TestAnotherUsersMessageIsNotFound(t *testing.T) {
	s, _, _, cfg, id := reading(t)
	robin := newClient(t, s)
	user(t, s.store, "robin", "a good password")
	robin.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`)
	for _, path := range []string{"/api/email-configs/" + cfg + "/messages/" + id, "/api/email-configs/" + cfg + "/messages/" + id + "/parts/0"} {
		if resp := robin.do("GET", path, ""); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s by another user = %s", path, resp.Status)
		}
	}
}

// imageServer serves a GIF and a page, on loopback over TLS.
func imageServer(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/p.gif" {
			raw, _ := base64.StdEncoding.DecodeString(pixel)
			w.Header().Set("Content-Type", "text/html") // what the sender claims is not believed
			w.Write(raw)
			return
		}
		w.Write([]byte("<html><script>x()</script></html>"))
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, pool
}

func TestTheProxyRelaysAnImageItSigned(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	srv, pool := imageServer(t)
	s.imageClient = imageClient(connect.New(connecttest.Loopback).WithRoots(pool))

	resp := c.do("GET", s.proxyURL(srv.URL+"/p.gif"), "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/gif" || !strings.HasPrefix(string(body), "GIF89a") {
		t.Fatalf("image = %s %v", resp.Status, resp.Header)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("the relayed image can be sniffed into something else")
	}

	if resp := c.do("GET", s.proxyURL(srv.URL+"/page"), ""); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a page through the proxy = %s, want refused", resp.Status)
	}
}

// Unsigned, the proxy would fetch anything for anybody signed in.
func TestTheProxyFetchesOnlyWhatItSigned(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	signed := s.proxyURL("https://example.com/a.gif")
	u, _ := url.Parse(signed)
	q := u.Query()
	q.Set("u", base64.RawURLEncoding.EncodeToString([]byte("https://example.com/b.gif")))
	if resp := c.do("GET", "/api/proxy?"+q.Encode(), ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another address under the same signature = %s", resp.Status)
	}
	if resp := c.do("GET", "/api/proxy?u=aHR0cHM6Ly9leGFtcGxlLmNvbQ", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("no signature = %s", resp.Status)
	}
}

// A message naming an address inside the network gets nothing from it.
func TestTheProxyNeverReachesInside(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	srv, pool := imageServer(t)
	s.imageClient = imageClient(connect.New(nil).WithRoots(pool))
	if resp := c.do("GET", s.proxyURL(srv.URL+"/p.gif"), ""); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("a loopback image = %s, want refused", resp.Status)
	}
}
