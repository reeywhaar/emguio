package api

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"emguio/internal/mirror"
	"emguio/internal/store"
)

// withExportable is the outbox world with Archive, Archive/2025 inside it and Trash, and mail in
// the first two.
func withExportable(t *testing.T) (*client, string, map[string]string, *fakeMirror) {
	t.Helper()
	c, cfg, boxes, _, fake := withOutbox(t)
	ctx := context.Background()
	fake.store.PutMailboxes(ctx, cfg, []store.Listed{
		{Name: "INBOX", Delimiter: "/", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Archive", Delimiter: "/", Selectable: true},
		{Name: "Archive/2025", Delimiter: "/", Selectable: true},
		{Name: "Trash", Delimiter: "/", SpecialUse: store.UseTrash, Selectable: true},
	})
	all, _ := fake.store.MirrorMailboxes(ctx, cfg)
	for _, mb := range all {
		boxes[mb.Name] = mb.ID
	}
	at := time.Date(2026, 11, 21, 9, 5, 0, 0, time.UTC)
	fake.exportable = map[string][]exportMessage{
		"Archive": {
			{mirror.Exporting{UID: 4, Date: at, From: "ann@example.com", Seen: true}, "Subject: one\r\n\r\nFrom me\r\n"},
			{mirror.Exporting{UID: 9, Date: at.Add(time.Hour), Flagged: true}, "Subject: two\r\n\r\nhi\r\n"},
		},
		"Archive/2025": {
			{mirror.Exporting{UID: 2, Date: at.AddDate(-1, 0, 0)}, "Subject: old\r\n\r\nold\r\n"},
		},
	}
	return c, cfg, boxes, fake
}

func exported(t *testing.T, c *client, cfg, mailbox, body string) map[string]any {
	t.Helper()
	resp := c.do("POST", "/api/email-configs/"+cfg+"/mailboxes/"+mailbox+"/export", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("export = %s %v", resp.Status, c.json(resp))
	}
	return c.json(resp)
}

func download(t *testing.T, c *client, url string) (*http.Response, []byte) {
	t.Helper()
	resp := c.do("GET", url, "")
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

// unzipped is each file of a zip by name, read whole, and the names in order.
func unzipped(t *testing.T, body []byte) ([]string, map[string]string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		names = append(names, f.Name)
		files[f.Name] = string(b)
	}
	return names, files
}

// A folder downloads as a zip of its mbox, once; then what went into it, and only that, goes to
// Trash.
func TestAFolderIsExportedAsMboxAndThenDeleted(t *testing.T) {
	c, cfg, boxes, fake := withExportable(t)
	x := exported(t, c, cfg, boxes["Archive"], `{"format":"mbox"}`)
	if x["state"] != "ready" || x["file"] != "Archive.zip" {
		t.Errorf("export = %v", x)
	}
	id, url := x["id"].(string), x["url"].(string)
	if resp := c.do("POST", "/api/exports/"+id+"/delete", `{}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("deleting before the download = %s", resp.Status)
	}

	resp, body := download(t, c, url)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `filename=Archive.zip`) {
		t.Fatalf("file = %s %v", resp.Status, resp.Header)
	}
	names, files := unzipped(t, body)
	want := "From ann@example.com Sat Nov 21 09:05:00 2026\nStatus: RO\nSubject: one\n\n>From me\n\n" +
		"From MAILER-DAEMON Sat Nov 21 10:05:00 2026\nStatus: O\nX-Status: F\nSubject: two\n\nhi\n\n"
	if len(names) != 1 || files["Archive.mbox"] != want {
		t.Errorf("files %v, Archive.mbox =\n%q\nwant\n%q", names, files["Archive.mbox"], want)
	}
	if again, _ := download(t, c, url); again.StatusCode != http.StatusConflict {
		t.Errorf("a second download = %s", again.Status)
	}
	if got := c.json(c.do("GET", "/api/exports/"+id, "")); got["state"] != "done" || got["exported"] != 2.0 {
		t.Errorf("after = %v", got)
	}

	got := c.json(c.do("POST", "/api/exports/"+id+"/delete", `{"read":true}`))
	if got["trash"] != 2.0 || got["deleted"] != 0.0 {
		t.Errorf("deleted = %v", got)
	}
	if len(fake.discarded) != 1 {
		t.Fatalf("discarded %+v", fake.discarded)
	}
	if d := fake.discarded[0]; d.mailbox != "Archive" || d.to != "Trash" || !d.read || len(d.uids) != 2 || d.uids[0] != 4 || d.uids[1] != 9 {
		t.Errorf("discarded %+v", d)
	}
	if resp := c.do("POST", "/api/exports/"+id+"/delete", `{}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("deleting twice = %s", resp.Status)
	}

	list := c.json(c.do("GET", "/api/exports", ""))["exports"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["mailbox"] != boxes["Archive"] {
		t.Errorf("exports = %v", list)
	}
	if resp := c.do("DELETE", "/api/exports/"+id, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("dismiss = %s", resp.Status)
	}
	if list := c.json(c.do("GET", "/api/exports", ""))["exports"].([]any); len(list) != 0 {
		t.Errorf("after dismissing = %v", list)
	}
}

// A folder exported with its subfolders goes with them once emptied, the deepest first; one that
// mail came into since stays, and so does each folder holding it.
func TestDeletingAnExportedFolderDeletesTheFolders(t *testing.T) {
	c, cfg, boxes, fake := withExportable(t)
	x := exported(t, c, cfg, boxes["Archive"], `{"format":"mbox","subfolders":true}`)
	download(t, c, x["url"].(string))
	got := c.json(c.do("POST", "/api/exports/"+x["id"].(string)+"/delete", `{}`))
	if got["trash"] != 3.0 || got["folders"] != 2.0 || got["kept"] != 0.0 {
		t.Errorf("deleted = %v", got)
	}
	left, _ := fake.store.MirrorMailboxes(context.Background(), cfg)
	for _, mb := range left {
		if strings.HasPrefix(mb.Name, "Archive") {
			t.Errorf("%s is still there", mb.Name)
		}
	}

	c, cfg, boxes, fake = withExportable(t)
	fake.holding = map[string]bool{"Archive/2025": true}
	x = exported(t, c, cfg, boxes["Archive"], `{"format":"mbox","subfolders":true}`)
	download(t, c, x["url"].(string))
	got = c.json(c.do("POST", "/api/exports/"+x["id"].(string)+"/delete", `{}`))
	if got["folders"] != 0.0 || got["kept"] != 2.0 {
		t.Errorf("with mail since = %v", got)
	}

	// A selection's folder stays.
	c, cfg, boxes, _ = withExportable(t)
	x = exported(t, c, cfg, boxes["Archive"], `{"format":"mbox","messages":["7-4","7-9"]}`)
	download(t, c, x["url"].(string))
	if got := c.json(c.do("POST", "/api/exports/"+x["id"].(string)+"/delete", `{}`)); got["folders"] != 0.0 {
		t.Errorf("a selection = %v", got)
	}
}

// With its subfolders, as a zip of eml files in a tree, each named by when it arrived.
func TestAFolderTreeIsExportedAsEml(t *testing.T) {
	c, cfg, boxes, _ := withExportable(t)
	x := exported(t, c, cfg, boxes["Archive"], `{"format":"eml","subfolders":true}`)
	if x["file"] != "Archive.zip" {
		t.Errorf("export = %v", x)
	}
	resp, body := download(t, c, x["url"].(string))
	if resp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("type = %s", resp.Header.Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	want := "Archive/2026-11-21-090500-4.eml Archive/2026-11-21-100500-9.eml Archive/2025/2025-11-21-090500-2.eml"
	if strings.Join(names, " ") != want {
		t.Errorf("names = %v", names)
	}
	f, _ := zr.File[0].Open()
	text, _ := io.ReadAll(f)
	if string(text) != "Subject: one\r\n\r\nFrom me\r\n" {
		t.Errorf("an eml is the message as the server holds it: %q", text)
	}
}

// One message selected, as eml, is a zip of that message's file; mbox with subfolders a zip of
// mboxes.
func TestASelectionAndAnMboxTree(t *testing.T) {
	c, cfg, boxes, _ := withExportable(t)
	x := exported(t, c, cfg, boxes["Archive"], `{"format":"eml","messages":["7-9"]}`)
	_, body := download(t, c, x["url"].(string))
	names, files := unzipped(t, body)
	if x["file"] != "Archive.zip" || len(names) != 1 || files["Archive/2026-11-21-100500-9.eml"] != "Subject: two\r\n\r\nhi\r\n" {
		t.Errorf("one message = %v %v", x, names)
	}

	x = exported(t, c, cfg, boxes["Archive"], `{"format":"mbox","subfolders":true}`)
	_, body = download(t, c, x["url"].(string))
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 || zr.File[0].Name != "Archive.mbox" || zr.File[1].Name != "Archive/2025.mbox" {
		t.Errorf("files = %v, %v", zr.File[0].Name, len(zr.File))
	}

	for _, bad := range []string{`{"format":"pst"}`, `{"format":"eml","messages":["7-9"],"subfolders":true}`, `{"format":"eml","messages":["7-9","8-1"]}`} {
		if resp := c.do("POST", "/api/email-configs/"+cfg+"/mailboxes/"+boxes["Archive"]+"/export", bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %s", bad, resp.Status)
		}
	}
}

// Somebody else's export is no export of theirs.
func TestAnExportIsItsUsers(t *testing.T) {
	c, cfg, boxes, _ := withExportable(t)
	x := exported(t, c, cfg, boxes["Archive"], `{"format":"mbox"}`)
	other := newClient(t, c.server)
	user(t, c.server.store, "other", "a good password")
	other.do("POST", "/api/auth/login", `{"username":"other","password":"a good password"}`)
	other.do("POST", "/api/seal/unseal", `{"key":"`+sealKey+`"}`)
	if resp, _ := download(t, other, x["url"].(string)); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another user's download = %s", resp.Status)
	}
}
