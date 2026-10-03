package api

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/mirror"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// partIDs is the ids of a draft's parts as an answer gives them.
func partIDs(got map[string]any) []string {
	var out []string
	for _, p := range got["parts"].([]any) {
		out = append(out, p.(map[string]any)["id"].(string))
	}
	return out
}

// A draft is kept here as typed — a field not an address yet, nobody to send to — with what it
// answers read once; each save keeps the attachments named, in order, and adds new ones after.
// The mail server has it later, or now when its window closes.
func TestADraftIsKeptHereAndSavedInPlace(t *testing.T) {
	c, cfg, boxes, _, fake := withOutbox(t)
	fake.origin = &mirror.Origin{MessageID: "<q@example.com>", References: []string{"<root@example.com>"}}
	resp := c.do("POST", "/api/email-configs/"+cfg+"/drafts", fmt.Sprintf(`{"cc":"bob@","subject":"Re: Plans","text":"Half",
		"reply":{"mailbox":%q,"message":"7-1"},"attachments":[{"name":"notes.txt","type":"text/plain","data":%q}]}`, boxes["INBOX"], b64("the notes")))
	got := c.json(resp)
	if resp.StatusCode != http.StatusOK || len(got["parts"].([]any)) != 1 || got["problem"] != "" {
		t.Fatalf("create = %s %v", resp.Status, got)
	}
	id, notes := got["id"].(string), partIDs(got)[0]
	d, err := fake.store.MirrorDraft(context.Background(), id)
	if err != nil || d.Cc != "bob@" || d.InReplyTo != "<q@example.com>" || d.ReplyMailbox != boxes["INBOX"] || d.DueAt.IsZero() {
		t.Fatalf("kept = %+v, %v", d, err)
	}

	path := "/api/email-configs/" + cfg + "/drafts/" + id
	resp = c.do("PUT", path, fmt.Sprintf(`{"to":"robin@example.com","text":"Whole","parts":[%q],
		"attachments":[{"name":"plan.txt","type":"text/plain","data":%q}]}`, notes, b64("the plan")))
	got = c.json(resp)
	if parts := partIDs(got); resp.StatusCode != http.StatusOK || len(parts) != 2 || parts[0] != notes {
		t.Fatalf("save = %s %v", resp.Status, got)
	}
	plan := partIDs(got)[1]
	resp = c.do("PUT", path, fmt.Sprintf(`{"text":"Whole","parts":[%q]}`, plan))
	if parts := partIDs(c.json(resp)); len(parts) != 1 || parts[0] != plan {
		t.Fatalf("dropped notes = %v", parts)
	}
	if fake.woken < 3 || len(fake.written) != 0 {
		t.Errorf("woken %d, written %v", fake.woken, fake.written)
	}

	resp = c.do("PUT", path, fmt.Sprintf(`{"text":"Done","parts":[%q],"close":true}`, plan))
	if got := c.json(resp); resp.StatusCode != http.StatusOK || got["closed"] != true || len(fake.written) != 1 {
		t.Fatalf("close = %s %v, written %v", resp.Status, got, fake.written)
	}
	fake.err = &connect.Failure{Class: "unreachable", Sentence: "imap.example.com cannot be reached."}
	resp = c.do("PUT", path, `{"text":"Again","close":true}`)
	if got := c.json(resp); resp.StatusCode != http.StatusAccepted || got["problem"] != "imap.example.com cannot be reached." {
		t.Errorf("close unreached = %s %v", resp.Status, got)
	}
	fake.err = mirror.ErrUnsupported
	resp = c.do("PUT", path, `{"text":"Again","close":true}`)
	if got := c.json(resp); resp.StatusCode != http.StatusConflict || !strings.Contains(got["message"].(string), "UIDPLUS") {
		t.Errorf("close unsupported = %s %v", resp.Status, got)
	}
}

// A draft sent goes from here, and its copy from the mail server once the message has gone; it
// is threaded and answers as it was begun. One whose sending does not go is kept, due again.
func TestASentDraftGoesFromHereAndFromTheServer(t *testing.T) {
	c, cfg, boxes, box, fake := withOutbox(t)
	fake.origin = &mirror.Origin{MessageID: "<q@example.com>", References: []string{"<root@example.com>"}}
	made := c.json(c.do("POST", "/api/email-configs/"+cfg+"/drafts", fmt.Sprintf(`{"to":"alice@example.com","text":"Yes.",
		"reply":{"mailbox":%q,"message":"7-1"}}`, boxes["INBOX"])))
	id := made["id"].(string)
	ctx := context.Background()
	if err := fake.store.DraftWritten(ctx, id, 1, boxes["Drafts"], "9-100", time.Time{}); err != nil {
		t.Fatal(err)
	}
	fake.origin = nil

	resp := c.do("POST", "/api/email-configs/"+cfg+"/send", fmt.Sprintf(`{"to":"nobody@%s","text":"Yes.","draft_id":%q}`, connecttest.Refused, id))
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("refused send = %s", resp.Status)
	}
	if d, err := fake.store.MirrorDraft(ctx, id); err != nil || d.DueAt.IsZero() && d.Written < d.Version {
		t.Errorf("after a refusal = %+v, %v", d, err)
	}

	resp = c.do("POST", "/api/email-configs/"+cfg+"/send", fmt.Sprintf(`{"to":"alice@example.com","subject":"Re: Question","text":"Yes.","draft_id":%q}`, id))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send = %s %v", resp.Status, c.json(resp))
	}
	raw := string(box.Sent()[0].Data)
	if !strings.Contains(raw, "In-Reply-To: <q@example.com>") || !strings.Contains(raw, "References: <root@example.com> <q@example.com>") {
		t.Errorf("not threaded:\n%s", raw)
	}
	s := fake.sendings[0]
	if s.Draft == nil || s.Draft.Mailbox.ID != boxes["Drafts"] || s.Draft.UID != 100 || s.Answers == nil || s.Answers.UID != 1 {
		t.Errorf("sending = %+v", s)
	}
	if _, err := fake.store.MirrorDraft(ctx, id); err == nil {
		t.Error("the draft is still here")
	}
}

// A draft discarded goes from here, and its answer says where the mail server holds its copy.
func TestADiscardedDraftSaysWhereItsCopyIs(t *testing.T) {
	c, cfg, boxes, _, fake := withOutbox(t)
	id := c.json(c.do("POST", "/api/email-configs/"+cfg+"/drafts", `{"text":"Never mind"}`))["id"].(string)
	fake.store.DraftWritten(context.Background(), id, 1, boxes["Drafts"], "9-100", time.Time{})
	path := "/api/email-configs/" + cfg + "/drafts/" + id
	got := c.json(c.do("DELETE", path, ""))
	if kept, _ := got["kept"].(map[string]any); kept["mailbox"] != boxes["Drafts"] || kept["message"] != "9-100" {
		t.Errorf("discard = %v", got)
	}
	if resp := c.do("DELETE", path, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("again = %s", resp.Status)
	}
}

// Without a Drafts folder there is nowhere for a draft to go, and none is begun.
func TestADraftNeedsADraftsFolder(t *testing.T) {
	_, _, nowhere, _, _ := withMail(t, 0)
	resp := nowhere.do("POST", "/api/email-configs/"+nowhereConfig(t, nowhere)+"/drafts", `{"text":"x"}`)
	if got := nowhere.json(resp); resp.StatusCode != http.StatusConflict || !strings.Contains(got["message"].(string), "no Drafts folder") {
		t.Errorf("without Drafts = %s %v", resp.Status, got)
	}
}
