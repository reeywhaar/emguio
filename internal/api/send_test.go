package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// withOutbox is a signed-in user with one email config whose outgoing server is a real SMTP
// server here, keeping what it is given, and whose mailboxes are INBOX, Drafts and Sent.
func withOutbox(t *testing.T) (*client, string, map[string]string, *connecttest.Outbox, *fakeMirror) {
	t.Helper()
	s, st := newServerStore(t, nil)
	cert := connecttest.NewCert(t)
	s.connector = connect.New(connecttest.Loopback).WithRoots(cert.Pool)
	box := &connecttest.Outbox{Holds: 24 * time.Hour}
	port := connecttest.SMTPInto(t, cert, connect.Implicit, "misha", "hunter2", box)
	fake := &fakeMirror{store: st}
	s.mirror = fake
	c := signIn(t, s, st)
	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", fmt.Sprintf(`{"name":"Work","email":"misha@example.com","sender_name":"Misha V",
		"incoming":{"protocol":"imap","host":%q,"port":993,"tls":"implicit","username":"misha","password":"hunter2"},
		"outgoing":{"host":%q,"port":%d,"tls":"implicit","username":"","password":""}}`, connecttest.Host, connecttest.Host, port)).Body).Decode(&made)
	ctx := context.Background()
	st.PutMailboxes(ctx, made.ID, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Drafts", SpecialUse: store.UseDrafts, Selectable: true},
		{Name: "Sent", SpecialUse: store.UseSent, Selectable: true},
	})
	boxes, _ := st.MirrorMailboxes(ctx, made.ID)
	ids := map[string]string{}
	for _, mb := range boxes {
		ids[mb.Name] = mb.ID
	}
	return c, made.ID, ids, box, fake
}

// A message goes out through the config's outgoing server, from its address under its sender's
// name, to everybody it names, Bcc's included and in no header; a copy is then filed in Sent.
func TestAMessageIsSentAndFiled(t *testing.T) {
	c, cfg, boxes, box, fake := withOutbox(t)
	body := fmt.Sprintf(`{"to":"Robin <robin@example.com>","cc":"","bcc":"secret@example.com","subject":"Hello\nthere",
		"text":"Hi Robin.","attachments":[{"name":"notes.txt","type":"text/plain","data":%q}]}`,
		base64.StdEncoding.EncodeToString([]byte("the notes")))
	resp := c.do("POST", "/api/email-configs/"+cfg+"/send", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send = %s %v", resp.Status, c.json(resp))
	}
	id := c.json(resp)["message_id"].(string)

	sent := box.Sent()
	if len(sent) != 1 || sent[0].From != "misha@example.com" || strings.Join(sent[0].To, ",") != "robin@example.com,secret@example.com" {
		t.Fatalf("sent = %+v", sent)
	}
	raw := string(sent[0].Data)
	for _, want := range []string{`From: "Misha V" <misha@example.com>`, "Subject: Hello there", "notes.txt", "Message-Id: " + id} {
		if !strings.Contains(raw, want) {
			t.Errorf("the message has no %q:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, "secret@example.com") {
		t.Error("the Bcc is in the message")
	}
	if len(fake.sendings) != 1 || fake.sendings[0].Sent.ID != boxes["Sent"] || fake.sendings[0].ID != id || fake.sendings[0].Answers != nil {
		t.Errorf("filed = %+v", fake.sendings)
	}
}

// A reply is threaded under what it answers, which is marked answered; a forward carries the
// parts it was asked to.
func TestAReplyIsThreadedAndAForwardCarriesItsParts(t *testing.T) {
	c, cfg, boxes, box, fake := withOutbox(t)
	fake.origin = &mirror.Origin{MessageID: "<q@example.com>", References: []string{"<root@example.com>"}}
	fake.parts = map[string][2]string{"2": {"Content-Type: application/pdf; name=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n", "JVBERi0xLjc=\r\n"}}
	inbox := boxes["INBOX"]

	resp := c.do("POST", "/api/email-configs/"+cfg+"/send", fmt.Sprintf(`{"to":"alice@example.com","subject":"Re: Question","text":"Yes.",
		"reply":{"mailbox":%q,"message":"7-1"}}`, inbox))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reply = %s %v", resp.Status, c.json(resp))
	}
	raw := string(box.Sent()[0].Data)
	if !strings.Contains(raw, "In-Reply-To: <q@example.com>") || !strings.Contains(raw, "References: <root@example.com> <q@example.com>") {
		t.Errorf("not threaded:\n%s", raw)
	}
	if a := fake.sendings[0].Answers; a == nil || a.Mailbox.ID != inbox || a.UIDValidity != 7 || a.UID != 1 {
		t.Errorf("answers = %+v", a)
	}

	resp = c.do("POST", "/api/email-configs/"+cfg+"/send", fmt.Sprintf(`{"to":"kim@example.com","subject":"Fwd: Report","text":"See below.",
		"carry":{"mailbox":%q,"message":"7-1","parts":["2"]}}`, inbox))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forward = %s %v", resp.Status, c.json(resp))
	}
	if raw := string(box.Sent()[1].Data); !strings.Contains(raw, "report.pdf") || !strings.Contains(raw, "JVBERi0xLjc=") {
		t.Errorf("the forward carries nothing:\n%s", raw)
	}
}

// What cannot be sent says why, and nothing goes.
func TestWhatCannotBeSentSaysWhy(t *testing.T) {
	c, cfg, _, box, _ := withOutbox(t)
	for body, want := range map[string]string{
		`{"to":"","subject":"x","text":"x"}`:                                       "Say who it goes to.",
		`{"to":"robin@example.com, bob@","subject":"x","text":"x"}`:                "“bob@” in To is not an address.",
		`{"to":"nobody@` + connecttest.Refused + `","subject":"x","text":"x"}`:     "refused the message: No such user here",
		`{"to":"robin@example.com","attachments":[{"name":"x","data":"!!"}]}`:      "not the JSON this expects",
		`{"to":"robin@example.com","reply":{"mailbox":"mb_nope","message":"7-1"}}`: "",
	} {
		resp := c.do("POST", "/api/email-configs/"+cfg+"/send", body)
		got := c.json(resp)
		if resp.StatusCode < 400 || (want != "" && !strings.Contains(got["message"].(string), want)) {
			t.Errorf("%s = %s %v, want %q", body, resp.Status, got, want)
		}
	}
	if n := len(box.Sent()); n != 0 {
		t.Errorf("%d sent", n)
	}

	_, _, nowhere, _, _ := withMail(t, 0)
	resp := nowhere.do("POST", "/api/email-configs/"+nowhereConfig(t, nowhere)+"/send", `{"to":"robin@example.com"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("without an outgoing server = %s", resp.Status)
	}
}

// nowhereConfig is the id of the one config a user has.
func nowhereConfig(t *testing.T, c *client) string {
	t.Helper()
	return c.json(c.do("GET", "/api/email-configs", ""))["email_configs"].([]any)[0].(map[string]any)["id"].(string)
}

// A message is handed over to be sent later, to a server that holds mail: dated then, filed in
// Sent dated then, and asked to go then. Not sooner than a minute from now.
func TestAMessageIsHandedOverToSendLater(t *testing.T) {
	c, cfg, _, box, fake := withOutbox(t)
	got := c.json(c.do("GET", "/api/email-configs/"+cfg+"/hold", ""))
	latest, _ := got["latest"].(float64)
	if d := time.Until(time.Unix(int64(latest), 0)); d < 23*time.Hour || d > 24*time.Hour {
		t.Fatalf("hold = %v", got)
	}

	at := time.Now().Add(2 * time.Hour).Truncate(time.Second).UTC()
	resp := c.do("POST", "/api/email-configs/"+cfg+"/send",
		fmt.Sprintf(`{"to":"robin@example.com","subject":"Later","text":"Hi.","send_at":%d}`, at.Unix()))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("send later = %s %v", resp.Status, c.json(resp))
	}
	sent := box.Sent()
	if len(sent) != 1 || !sent[0].HoldUntil.Equal(at) {
		t.Fatalf("sent = %+v", sent)
	}
	if date := "Date: " + at.Format(time.RFC1123Z); !strings.Contains(string(sent[0].Data), date) {
		t.Errorf("the message is not dated %s:\n%s", at, sent[0].Data)
	}
	if len(fake.sendings) != 1 || !fake.sendings[0].Date.Equal(at) {
		t.Errorf("filed %+v", fake.sendings)
	}

	resp = c.do("POST", "/api/email-configs/"+cfg+"/send",
		fmt.Sprintf(`{"to":"robin@example.com","subject":"Now","text":"Hi.","send_at":%d}`, time.Now().Unix()))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("send at now = %s", resp.Status)
	}
	if len(box.Sent()) != 1 {
		t.Error("a time that has come was handed over")
	}
}

// withScheduled is withOutbox's account on a server that shows held mail in a Scheduled folder,
// with one message held there, UID 4.
func withScheduled(t *testing.T) (*client, string, map[string]string, *connecttest.Outbox, *fakeMirror) {
	t.Helper()
	c, cfg, boxes, box, fake := withOutbox(t)
	ctx := context.Background()
	fake.store.PutMailboxes(ctx, cfg, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Drafts", SpecialUse: store.UseDrafts, Selectable: true},
		{Name: "Sent", SpecialUse: store.UseSent, Selectable: true},
		{Name: "Scheduled", SpecialUse: store.UseScheduled, Selectable: true},
	})
	all, _ := fake.store.MirrorMailboxes(ctx, cfg)
	for _, mb := range all {
		boxes[mb.Name] = mb.ID
	}
	fake.store.SetMailboxStatus(ctx, boxes["Scheduled"], store.MailboxStatus{UIDValidity: 7, UIDNext: 5, Messages: 1})
	fake.store.PutMessages(ctx, boxes["Scheduled"], []store.Header{
		{UID: 4, InternalDate: time.Now().Add(time.Hour), Subject: "Held", Flags: store.Flags{Seen: true}},
	})
	return c, cfg, boxes, box, fake
}

// A server that shows held mail in Scheduled files it in Sent itself when it goes, so emguio
// files no copy of its own.
func TestHeldMailIsNotFiledWhereTheServerFilesIt(t *testing.T) {
	c, cfg, _, box, fake := withScheduled(t)
	at := time.Now().Add(2 * time.Hour).Unix()
	resp := c.do("POST", "/api/email-configs/"+cfg+"/send",
		fmt.Sprintf(`{"to":"robin@example.com","subject":"Later","text":"Hi.","send_at":%d}`, at))
	if resp.StatusCode != http.StatusOK || len(box.Sent()) != 1 {
		t.Fatalf("send later = %s, %d handed over", resp.Status, len(box.Sent()))
	}
	if len(fake.sendings) != 1 || fake.sendings[0].Sent != nil {
		t.Errorf("filed in %+v", fake.sendings)
	}
}

// An edit of a held message cancels it first, then goes; one already gone stops the edit before
// anything is sent.
func TestAHeldMessageIsCancelledForItsEditToGo(t *testing.T) {
	c, cfg, boxes, box, fake := withScheduled(t)
	at := time.Now().Add(3 * time.Hour).Unix()
	body := fmt.Sprintf(`{"to":"robin@example.com","subject":"Held, edited","text":"Hi.","send_at":%d,
		"cancels":{"mailbox":%q,"message":"7-4"}}`, at, boxes["Scheduled"])
	if resp := c.do("POST", "/api/email-configs/"+cfg+"/send", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("edit = %s %v", resp.Status, c.json(resp))
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "Scheduled 4" || len(box.Sent()) != 1 {
		t.Fatalf("deleted %v, handed over %d", fake.deleted, len(box.Sent()))
	}

	fake.gone = map[uint32]bool{4: true}
	resp := c.do("POST", "/api/email-configs/"+cfg+"/send", body)
	if got := c.json(resp); resp.StatusCode != http.StatusNotFound || got["code"] != "gone" {
		t.Errorf("edit of one gone = %s %v", resp.Status, got)
	}
	if len(box.Sent()) != 1 {
		t.Error("an edit of a message already gone was sent")
	}

	resp = c.do("POST", "/api/email-configs/"+cfg+"/send", fmt.Sprintf(`{"to":"robin@example.com","subject":"x","text":"x",
		"cancels":{"mailbox":%q,"message":"7-4"}}`, boxes["INBOX"]))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("cancelling a message in INBOX = %s", resp.Status)
	}
}
