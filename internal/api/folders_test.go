package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"emguio/internal/mirror"
)

// A folder is made on the server, at the top or inside another, kept like the rest, and answered
// with, for a message to be moved into.
func TestAFolderIsMadeToMoveInto(t *testing.T) {
	c, cfg, boxes, _, _ := withOutbox(t)
	path := "/api/email-configs/" + cfg + "/mailboxes"
	resp := c.do("POST", path, `{"name":" Projects "}`)
	got := c.json(resp)
	if resp.StatusCode != http.StatusCreated || got["name"] != "Projects" || got["selectable"] != true || !strings.HasPrefix(got["id"].(string), "mb_") {
		t.Fatalf("made = %s %v", resp.Status, got)
	}
	projects := got["id"].(string)
	resp = c.do("POST", path, fmt.Sprintf(`{"name":"Clients","parent":%q}`, projects))
	if got := c.json(resp); resp.StatusCode != http.StatusCreated || got["name"] != "Projects/Clients" || fmt.Sprint(got["path"]) != "[Projects Clients]" {
		t.Fatalf("inside = %s %v", resp.Status, got)
	}
	listed := c.json(c.do("GET", path, ""))["mailboxes"].([]any)
	if len(listed) != len(boxes)+2 {
		t.Errorf("listed %d folders", len(listed))
	}

	for body, want := range map[string]int{
		`{"name":""}`:            http.StatusBadRequest,
		`{"name":"a/b"}`:         http.StatusBadRequest,
		`{"name":"line\nbreak"}`: http.StatusBadRequest,
		`{"name":"projects"}`:    http.StatusConflict,
		`{"name":"inbox"}`:       http.StatusConflict,
	} {
		if resp := c.do("POST", path, body); resp.StatusCode != want {
			t.Errorf("%s = %s, want %d", body, resp.Status, want)
		}
	}
}

// The folder an email config archives to is its user's to choose, from its own folders — not one
// the server has for something else, Trash least of all — and to hand back to the server's.
func TestTheArchiveFolderIsChosen(t *testing.T) {
	c, cfg, boxes, _, _ := withOutbox(t)
	old := c.json(c.do("POST", "/api/email-configs/"+cfg+"/mailboxes", `{"name":"Old"}`))["id"].(string)
	path := "/api/email-configs/" + cfg + "/archive"
	resp := c.do("PUT", path, fmt.Sprintf(`{"mailbox":%q}`, old))
	if got := c.json(resp); resp.StatusCode != http.StatusOK || got["archive_mailbox"] != old {
		t.Fatalf("chose = %s %v", resp.Status, got)
	}
	listed := c.json(c.do("GET", "/api/email-configs", ""))["email_configs"].([]any)[0].(map[string]any)
	if listed["archive_mailbox"] != old {
		t.Errorf("listed = %v", listed["archive_mailbox"])
	}
	for _, name := range []string{"Sent", "Drafts", "INBOX"} {
		if resp := c.do("PUT", path, fmt.Sprintf(`{"mailbox":%q}`, boxes[name])); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %s", name, resp.Status)
		}
	}
	if got := c.json(c.do("PUT", path, `{"mailbox":""}`)); got["archive_mailbox"] != nil {
		t.Errorf("automatic again = %v", got["archive_mailbox"])
	}
	if resp := c.do("PUT", path, `{"mailbox":"mb_nope"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("not a folder id = %s", resp.Status)
	}
}

// A folder is renamed and moved, keeping its id and taking what is inside it along; the server's
// own folders keep theirs, and nothing goes inside itself.
func TestAFolderIsRenamedAndMoved(t *testing.T) {
	c, cfg, boxes, _, _ := withOutbox(t)
	path := "/api/email-configs/" + cfg + "/mailboxes"
	made := func(body string) string {
		t.Helper()
		resp := c.do("POST", path, body)
		got := c.json(resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("make %s = %s %v", body, resp.Status, got)
		}
		return got["id"].(string)
	}
	note := made(`{"name":"Notification"}`)
	letters := made(fmt.Sprintf(`{"name":"Newsletter","parent":%q}`, note))
	old := made(fmt.Sprintf(`{"name":"Old","parent":%q}`, letters))

	resp := c.do("PUT", path+"/"+letters, `{"name":"Letters","parent":""}`)
	got := c.json(resp)
	if resp.StatusCode != http.StatusOK || got["id"] != letters || got["name"] != "Letters" {
		t.Fatalf("to the top = %s %v", resp.Status, got)
	}
	var names []string
	for _, mb := range c.json(c.do("GET", path, ""))["mailboxes"].([]any) {
		mb := mb.(map[string]any)
		if mb["id"] == old {
			names = append(names, mb["name"].(string))
		}
	}
	if fmt.Sprint(names) != "[Letters/Old]" {
		t.Errorf("what was inside = %v", names)
	}

	for body, want := range map[string]int{
		fmt.Sprintf(`{"name":"Letters","parent":%q}`, old): http.StatusBadRequest,
		`{"name":"Notification","parent":""}`:              http.StatusConflict,
		`{"name":"","parent":""}`:                          http.StatusBadRequest,
	} {
		if resp := c.do("PUT", path+"/"+letters, body); resp.StatusCode != want {
			t.Errorf("%s = %s, want %d", body, resp.Status, want)
		}
	}
	if resp := c.do("PUT", path+"/"+boxes["Sent"], `{"name":"Mine","parent":""}`); resp.StatusCode != http.StatusConflict {
		t.Errorf("rename Sent = %s", resp.Status)
	}
}

// An empty folder is deleted; one with folders or mail in it, and the server's own, are not.
func TestAnEmptyFolderIsDeleted(t *testing.T) {
	c, cfg, boxes, _, fake := withOutbox(t)
	path := "/api/email-configs/" + cfg + "/mailboxes"
	parent := c.json(c.do("POST", path, `{"name":"Parent"}`))["id"].(string)
	child := c.json(c.do("POST", path, fmt.Sprintf(`{"name":"Child","parent":%q}`, parent)))["id"].(string)

	if resp := c.do("DELETE", path+"/"+parent, ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("with a folder inside = %s", resp.Status)
	}
	if resp := c.do("DELETE", path+"/"+boxes["Drafts"], ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("Drafts = %s", resp.Status)
	}
	fake.err = mirror.ErrNotEmpty
	if resp := c.do("DELETE", path+"/"+child, ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("with mail = %s", resp.Status)
	}
	fake.err = nil
	if resp := c.do("DELETE", path+"/"+child, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("empty = %s", resp.Status)
	}
	for _, mb := range c.json(c.do("GET", path, ""))["mailboxes"].([]any) {
		if mb.(map[string]any)["id"] == child {
			t.Error("the folder is still listed")
		}
	}
}
