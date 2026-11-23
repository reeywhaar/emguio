package api

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/mbox"
)

// upload sends a file as itself, as the page does.
func upload(t *testing.T, c *client, url, kind string, body []byte) *http.Response {
	t.Helper()
	r := httptest.NewRequest("POST", url, bytes.NewReader(body))
	r.Header.Set("Content-Type", kind)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.server.ServeHTTP(w, r)
	return w.Result()
}

func mboxOf(t *testing.T, messages ...string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := mbox.NewWriter(&out)
	at := time.Date(2026, 11, 21, 9, 5, 0, 0, time.UTC)
	for i, text := range messages {
		if err := w.Write(mbox.Message{Date: at.Add(time.Duration(i) * time.Hour), Seen: i == 0, Flagged: i == 1}, strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	return out.Bytes()
}

func zipOf(t *testing.T, files map[string][]byte, order ...string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range order {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write(files[name])
	}
	zw.Close()
	return out.Bytes()
}

// An mbox goes into the folder with its flags and dates; a second time, nothing is doubled.
func TestAnMboxIsImportedIntoAFolder(t *testing.T) {
	c, cfg, boxes, fake := withExportable(t)
	file := mboxOf(t,
		"Message-ID: <a@example.com>\r\nSubject: one\r\n\r\nFrom me\r\n",
		"Message-ID: <b@example.com>\r\nSubject: two\r\n\r\nhi\r\n")
	url := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["Archive"] + "/import?name=Old.mbox"
	resp := upload(t, c, url, "application/mbox", file)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("import = %s %v", resp.Status, c.json(resp))
	}
	list := c.json(c.do("GET", "/api/imports", ""))["imports"].([]any)
	if len(list) != 1 {
		t.Fatalf("imports = %v", list)
	}
	if got := list[0].(map[string]any); got["state"] != "done" || got["total"] != 2.0 || got["imported"] != 2.0 || got["file"] != "Old.mbox" {
		t.Errorf("import = %v", got)
	}
	put := fake.appended["Archive"]
	if len(put) != 2 || put[0].text != "Message-ID: <a@example.com>\r\nSubject: one\r\n\r\nFrom me\r\n" {
		t.Fatalf("appended %+v", put)
	}
	if put[0].Flags[0] != imap.FlagSeen || len(put[1].Flags) != 1 || put[1].Flags[0] != imap.FlagFlagged ||
		!put[1].Date.Equal(time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC)) {
		t.Errorf("flags %v and %v, second dated %v", put[0].Flags, put[1].Flags, put[1].Date)
	}

	upload(t, c, url, "application/mbox", file)
	list = c.json(c.do("GET", "/api/imports", ""))["imports"].([]any)
	if got := list[1].(map[string]any); got["imported"] != 0.0 || got["skipped"] != 2.0 {
		t.Errorf("again = %v", got)
	}
	id := list[0].(map[string]any)["id"].(string)
	if resp := c.do("DELETE", "/api/imports/"+id, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("dismiss = %s", resp.Status)
	}
	if left := c.json(c.do("GET", "/api/imports", ""))["imports"].([]any); len(left) != 1 {
		t.Errorf("left = %v", left)
	}
}

// A zip with one root is that folder: its mbox goes into the folder imported into, and what is
// under the root becomes folders inside it.
func TestAZipIsImportedAsATree(t *testing.T) {
	c, cfg, boxes, fake := withExportable(t)
	file := zipOf(t, map[string][]byte{
		"Work.mbox":         mboxOf(t, "Subject: top\r\n\r\nx\r\n"),
		"Work/Clients.mbox": mboxOf(t, "Subject: inner\r\n\r\ny\r\n"),
		"__MACOSX/._Work":   []byte("junk"),
	}, "Work.mbox", "Work/Clients.mbox", "__MACOSX/._Work")
	url := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["Archive"] + "/import?name=Work.zip"
	if resp := upload(t, c, url, "application/zip", file); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("import = %s", resp.Status)
	}
	got := c.json(c.do("GET", "/api/imports", ""))["imports"].([]any)[0].(map[string]any)
	if got["state"] != "done" || got["imported"] != 2.0 || got["folders"] != 1.0 {
		t.Errorf("import = %v", got)
	}
	if len(fake.appended["Archive"]) != 1 || len(fake.appended["Archive/Clients"]) != 1 {
		t.Errorf("appended to %v", fake.appended)
	}
}

// emls as an export names them arrive dated by their names; with no status they are read.
func TestEmlsAreImportedDatedByTheirNames(t *testing.T) {
	c, cfg, boxes, fake := withExportable(t)
	file := zipOf(t, map[string][]byte{
		"Archive/2025-11-21-090500-2.eml":      []byte("Subject: old\n\nold\n"),
		"Archive/2025/2025-01-02-030405-9.eml": []byte("Subject: older\r\n\r\nolder\r\n"),
	}, "Archive/2025-11-21-090500-2.eml", "Archive/2025/2025-01-02-030405-9.eml")
	url := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["INBOX"] + "/import?name=Archive.zip"
	if resp := upload(t, c, url, "application/zip", file); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("import = %s", resp.Status)
	}
	in := fake.appended["INBOX"]
	if len(in) != 1 || in[0].text != "Subject: old\r\n\r\nold\r\n" || in[0].Flags[0] != imap.FlagSeen ||
		!in[0].Date.Equal(time.Date(2025, 11, 21, 9, 5, 0, 0, time.UTC)) {
		t.Errorf("INBOX got %+v", in)
	}
	if len(fake.appended["INBOX/2025"]) != 1 {
		t.Errorf("appended to %v", fake.appended)
	}
}

// Only what reads as mail is taken; another user's imports are not listed.
func TestAnImportIsMailAndItsUsers(t *testing.T) {
	c, cfg, boxes, _ := withExportable(t)
	url := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["Archive"] + "/import?name=notes.txt"
	if resp := upload(t, c, url, "text/plain", []byte("hello")); resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("a text file = %s", resp.Status)
	}
	upload(t, c, strings.Replace(url, "notes.txt", "one.eml", 1), "message/rfc822", []byte("Subject: x\r\n\r\ny\r\n"))
	other := newClient(t, c.server)
	user(t, c.server.store, "other", "a good password")
	other.do("POST", "/api/auth/login", `{"username":"other","password":"a good password"}`)
	other.do("POST", "/api/seal/unseal", `{"key":"`+sealKey+`"}`)
	if list := other.json(other.do("GET", "/api/imports", ""))["imports"].([]any); len(list) != 0 {
		t.Errorf("another user sees %v", list)
	}
}
