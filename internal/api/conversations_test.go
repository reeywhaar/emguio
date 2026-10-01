package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"emguio/internal/mirror"
	"emguio/internal/store"
)

// A list's rows are told how many messages each is in a conversation with in their folder, those
// not alone; nothing on a server without THREAD.
func TestAListIsToldItsConversations(t *testing.T) {
	c, cfg, boxes, _, fake := withOutbox(t)
	path := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["INBOX"] + "/threads?messages=7-1,7-2,7-4,8-3,nope"
	if got := c.json(c.do("GET", path, "")); fmt.Sprint(got["counts"]) != "map[]" {
		t.Errorf("without THREAD = %v", got)
	}
	fake.threads = mirror.ThreadsOf(7, []uint32{1, 2, 3}, []uint32{4})
	if got := c.json(c.do("GET", path, "")); fmt.Sprint(got["counts"]) != "map[7-1:3 7-2:3]" {
		t.Errorf("counts = %v", got)
	}
}

// A conversation is what the server threads in the folder and its other side from Sent — what
// answers it, and what it answers — oldest first, each with its folder.
func TestAConversationIsItsThreadAndTheOtherSide(t *testing.T) {
	c, cfg, boxes, _, fake := withOutbox(t)
	at := func(h int) time.Time { return time.Date(2026, 9, 30, h, 0, 0, 0, time.UTC) }
	fake.threads = mirror.ThreadsOf(7, []uint32{1, 3})
	fake.heads = map[uint32]store.Header{
		1: {UID: 1, Subject: "Re: Lunch", MessageID: "q@example.com", InReplyTo: "<mine@example.com>", InternalDate: at(10)},
		3: {UID: 3, Subject: "Re: Lunch", MessageID: "<last@example.com>", InternalDate: at(13)},
	}
	fake.related = &mirror.Listing{UIDValidity: 9, Headers: []store.Header{
		{UID: 6, Subject: "Re: Lunch", MessageID: "<answer@example.com>", InternalDate: at(11)},
		{UID: 5, Subject: "Lunch", MessageID: "<mine@example.com>", InternalDate: at(9)},
		// The same message filed in both, which shows once.
		{UID: 7, Subject: "Re: Lunch", MessageID: "<last@example.com>", InternalDate: at(13)},
	}}
	path := "/api/email-configs/" + cfg + "/mailboxes/" + boxes["INBOX"] + "/messages/"
	got := c.json(c.do("GET", path+"7-3/conversation", ""))
	var order []string
	for _, m := range got["messages"].([]any) {
		m := m.(map[string]any)
		order = append(order, fmt.Sprintf("%s in %s", m["id"], name(boxes, m["mailbox"].(string))))
	}
	if fmt.Sprint(order) != "[9-5 in Sent 7-1 in INBOX 9-6 in Sent 7-3 in INBOX]" {
		t.Errorf("conversation = %v", order)
	}
	if fmt.Sprint(fake.asked) != "[[q@example.com last@example.com mine@example.com]]" {
		t.Errorf("searched for %v", fake.asked)
	}

	if resp := c.do("GET", path+"8-3/conversation", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another UIDVALIDITY = %s", resp.Status)
	}
	fake.threads = nil
	if got := c.json(c.do("GET", path+"7-3/conversation", "")); len(got["messages"].([]any)) != 0 {
		t.Errorf("without THREAD = %v", got)
	}
}

func name(boxes map[string]string, id string) string {
	for n, b := range boxes {
		if b == id {
			return n
		}
	}
	return id
}
