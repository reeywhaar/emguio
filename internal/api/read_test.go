package api

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/message"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

var pixel = base64.StdEncoding.EncodeToString([]byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"))

// rich is a message as its server describes it: HTML with a handler, a carried image and a
// remote one, and an HTML file attached.
var rich = message.Structure{
	HTML: &message.Text{Body: []byte(`<p onclick="x()">Hi <img src="cid:logo@x"> <img src="https://track.example/p.gif"></p>`), Charset: "utf-8"},
	Leaves: []message.Leaf{
		{Section: "1.2", Type: "image/gif", ContentID: "<logo@x>", Disposition: "inline",
			DispositionParams: map[string]string{"filename": "logo.gif"}, Encoding: "base64", Size: 78},
		{Section: "2", Type: "text/html", Disposition: "attachment",
			DispositionParams: map[string]string{"filename": "page.html"}, Size: 25},
	},
}

type readView struct {
	Subject    string `json:"subject"`
	Text       string `json:"text"`
	HTML       string `json:"html"`
	HeldImages int    `json:"held_images"`
	Parts      []struct {
		Section string `json:"section"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Listed  bool   `json:"listed"`
	} `json:"parts"`
}

// reading is a user with one message on the server, the rich one, kept in INBOX's window, and
// the path it is read at.
func reading(t *testing.T) (*Server, *client, *fakeMirror, string, string) {
	t.Helper()
	s, _, c, cfg, inbox := withMail(t, 1)
	fake := &fakeMirror{opened: rich, parts: map[string][2]string{
		"1.2": {"Content-Type: image/gif\r\nContent-ID: <logo@x>\r\nContent-Disposition: inline; filename=logo.gif\r\nContent-Transfer-Encoding: base64\r\n\r\n", pixel},
		"2":   {"Content-Type: text/html\r\nContent-Disposition: attachment; filename=\"page.html\"\r\n\r\n", "<script>alert(1)</script>"},
	}}
	s.mirror = fake
	page := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))
	id := page["messages"].([]any)[0].(map[string]any)["id"].(string)
	return s, c, fake, cfg, "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages/" + id
}

// Nothing of a message is kept: every opening asks the server.
func TestAMessageIsReadFromTheServerEveryTime(t *testing.T) {
	_, c, fake, _, path := reading(t)
	for range 2 {
		resp := c.do("GET", path, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("read = %s %v", resp.Status, c.json(resp))
		}
	}
	if fake.fetched != 2 {
		t.Errorf("fetched %d times, want every time", fake.fetched)
	}
}

func TestAReadMessageIsSafeAndItsImagesPointHere(t *testing.T) {
	_, c, _, _, path := reading(t)
	var got readView
	json.NewDecoder(c.do("GET", path, "").Body).Decode(&got)

	if strings.Contains(got.HTML, "onclick") || strings.Contains(got.HTML, "track.example") || strings.Contains(got.HTML, `src="https`) {
		t.Errorf("html = %s", got.HTML)
	}
	if got.HeldImages != 0 || !strings.Contains(got.HTML, `src="/api/proxy?`) || strings.Contains(got.HTML, "data-src") {
		t.Errorf("held images = %d, html = %s", got.HeldImages, got.HTML)
	}
	if !strings.Contains(got.HTML, `src="`+path+`/parts/1.2"`) {
		t.Errorf("the carried image does not point at its part: %s", got.HTML)
	}
	var listed []string
	for _, p := range got.Parts {
		if p.Listed {
			listed = append(listed, p.Name+"@"+p.Section)
		}
	}
	if strings.Join(listed, ",") != "page.html@2" {
		t.Errorf("listed parts = %v", listed)
	}
}

// In Junk an image would tell a spammer the address is read, so there they wait to be asked for.
func TestImagesInJunkAreHeld(t *testing.T) {
	s, st, c, cfg, _ := withMail(t, 0)
	s.mirror = &fakeMirror{opened: rich}
	ctx := context.Background()
	st.PutMailboxes(ctx, cfg, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Spam", SpecialUse: store.UseJunk, Selectable: true},
	})
	var junk string
	boxes, _ := st.MirrorMailboxes(ctx, cfg)
	for _, mb := range boxes {
		if mb.SpecialUse == store.UseJunk {
			junk = mb.ID
		}
	}
	var got readView
	json.NewDecoder(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+junk+"/messages/3-1", "").Body).Decode(&got)
	if got.HeldImages != 1 || !strings.Contains(got.HTML, `data-src="/api/proxy?`) || strings.Contains(got.HTML, ` src="/api/proxy?`) {
		t.Errorf("held images = %d, html = %s", got.HeldImages, got.HTML)
	}
}

// An attachment is the sender's file, and it must not become a page of this origin.
func TestAPartCannotActAsAPageHere(t *testing.T) {
	_, c, _, _, path := reading(t)

	page := c.do("GET", path+"/parts/2", "")
	if page.Header.Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(page.Header.Get("Content-Disposition"), "attachment") ||
		page.Header.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(page.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("an HTML attachment was served as %v", page.Header)
	}

	logo := c.do("GET", path+"/parts/1.2", "")
	body, _ := io.ReadAll(logo.Body)
	if logo.Header.Get("Content-Type") != "image/gif" || !strings.HasPrefix(logo.Header.Get("Content-Disposition"), "inline") ||
		!strings.HasPrefix(string(body), "GIF89a") {
		t.Errorf("an image was served as %v", logo.Header)
	}
	// A UID names one message for good, so its parts are the browser's to keep.
	if !strings.Contains(logo.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("cache = %q", logo.Header.Get("Cache-Control"))
	}

	if resp := c.do("GET", path+"/parts/9", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a part that is not there = %s", resp.Status)
	}
	for _, bad := range []string{"0", "1..2", "x", "1.0"} {
		if resp := c.do("GET", path+"/parts/"+bad, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("section %q = %s", bad, resp.Status)
		}
	}
}

func TestReadingStoresAPreviewForTheList(t *testing.T) {
	_, c, _, cfg, path := reading(t)
	c.do("GET", path, "")
	boxes := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	inbox := boxes[0].(map[string]any)["id"].(string)
	msg := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))["messages"].([]any)[0].(map[string]any)
	if msg["preview"] != "Hi" {
		t.Errorf("preview = %q", msg["preview"])
	}
}

func TestAMessageGoneFromTheServerSaysSoAndAsksForALook(t *testing.T) {
	_, c, fake, _, path := reading(t)
	fake.err = mirror.ErrGone
	resp := c.do("GET", path, "")
	if resp.StatusCode != http.StatusNotFound || c.json(resp)["code"] != CodeGone {
		t.Fatalf("gone = %s", resp.Status)
	}
	if len(fake.refreshed) != 1 {
		t.Error("the list was not brought up to date")
	}
}

func TestAnotherUsersMessageIsNotFound(t *testing.T) {
	s, _, fake, _, path := reading(t)
	robin := newClient(t, s)
	user(t, s.store, "robin", "a good password")
	robin.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`)
	for _, p := range []string{path, path + "/parts/2"} {
		if resp := robin.do("GET", p, ""); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s by another user = %s", p, resp.Status)
		}
	}
	if resp := robin.do("PATCH", path, `{"seen":true}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("marking another user's message = %s", resp.Status)
	}
	if fake.fetched != 0 || len(fake.seen) != 0 {
		t.Error("the server was asked on another user's behalf")
	}
}

func TestAMessageIdIsTheServersNumbers(t *testing.T) {
	_, c, _, _, path := reading(t)
	base := path[:strings.LastIndex(path, "/")]
	for _, bad := range []string{"m_1", "1", "7-0", "7.1", "x-y"} {
		if resp := c.do("GET", base+"/"+bad, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q = %s", bad, resp.Status)
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
	if !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("cache = %q", resp.Header.Get("Cache-Control"))
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

func TestMarkingReadTellsTheServerAndThenTheList(t *testing.T) {
	_, c, fake, cfg, path := reading(t)

	resp := c.do("PATCH", path, `{"seen":true}`)
	if got := c.json(resp); resp.StatusCode != http.StatusOK || got["seen"] != true || got["id"] != "7-1" {
		t.Fatalf("mark read = %s %v", resp.Status, got)
	}
	if len(fake.seen) != 1 || !fake.seen[0] {
		t.Fatalf("the server was told %v", fake.seen)
	}
	boxes := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	if unseen := boxes[0].(map[string]any)["unseen"]; unseen != float64(0) {
		t.Errorf("INBOX unseen after reading = %v", unseen)
	}
	inbox := boxes[0].(map[string]any)["id"].(string)
	msg := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))["messages"].([]any)[0].(map[string]any)
	if msg["seen"] != true {
		t.Error("the kept row still says unread")
	}

	// Already read on the server: the count does not move again.
	c.do("PATCH", path, `{"seen":true}`)
	boxes = c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	if unseen := boxes[0].(map[string]any)["unseen"]; unseen != float64(0) || len(fake.seen) != 1 {
		t.Errorf("unseen = %v, told %v", unseen, fake.seen)
	}
}

// The server first: a flag it did not take must not show as taken.
func TestAFlagTheServerRefusedIsNotRecorded(t *testing.T) {
	_, c, fake, cfg, path := reading(t)
	fake.err = &connect.Failure{Class: "network", Sentence: "imap.example.com:993 closed the connection."}
	resp := c.do("PATCH", path, `{"seen":true}`)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %s", resp.Status)
	}
	boxes := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	inbox := boxes[0].(map[string]any)["id"].(string)
	msg := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))["messages"].([]any)[0].(map[string]any)
	if msg["seen"] != false {
		t.Error("recorded as read though the server never took it")
	}

	fake.err = mirror.ErrGone
	if resp := c.do("PATCH", path, `{"seen":true}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("gone = %s", resp.Status)
	}
}

func TestAFlagChangeSaysWhatToChange(t *testing.T) {
	_, c, _, _, path := reading(t)
	if resp := c.do("PATCH", path, `{}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an empty change = %s", resp.Status)
	}
	if resp := c.do("PATCH", path, `{"flagged":true}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a flag this does not set = %s", resp.Status)
	}
}
