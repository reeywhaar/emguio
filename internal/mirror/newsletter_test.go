package mirror

import (
	"context"
	"testing"

	"emguio/internal/connect/connecttest"
)

// A message that says how to unsubscribe is a newsletter: kept as one, listed as one, and opened
// with its link.
func TestANewsletterIsToldByItsUnsubscribeHeader(t *testing.T) {
	w := newWorld(t, "hunter2")
	connecttest.Append(t, w.server, "INBOX", "From: news@example.com\nTo: misha@example.com\nSubject: Weekly\n"+
		"List-Unsubscribe: <mailto:leave@example.com>, <https://news.example.com/u/abc>\n"+
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click\n\nHello.\n", start)
	connecttest.Append(t, w.server, "INBOX", "From: ann@example.com\nTo: misha@example.com\nSubject: Lunch\n\nHi.\n", start.Add(60e9))
	w.sync()

	kept := w.window()
	if len(kept) != 2 || kept[0].Newsletter || !kept[1].Newsletter {
		t.Fatalf("kept %q, newsletters %v %v", subjects(kept), kept[0].Newsletter, kept[1].Newsletter)
	}

	ctx := context.Background()
	listed, err := w.mirror.List(ctx, w.target, "INBOX", After{}, 10, "")
	if err != nil || len(listed.Headers) != 2 || !listed.Headers[1].Newsletter {
		t.Fatalf("listed %+v, %v", listed, err)
	}

	opened, err := w.mirror.Read(ctx, w.target, "INBOX", listed.UIDValidity, listed.Headers[1].UID)
	if err != nil || opened.Unsubscribe.Link != "https://news.example.com/u/abc" || !opened.Unsubscribe.OneClick {
		t.Errorf("opened %+v, %v", opened.Unsubscribe, err)
	}
	how, err := w.mirror.Unsubscribe(ctx, w.target, "INBOX", listed.UIDValidity, listed.Headers[1].UID)
	if err != nil || how != opened.Unsubscribe {
		t.Errorf("unsubscribe = %+v, %v", how, err)
	}
	if none, err := w.mirror.Unsubscribe(ctx, w.target, "INBOX", listed.UIDValidity, listed.Headers[0].UID); err != nil || none.Any() {
		t.Errorf("a letter = %+v, %v", none, err)
	}
}
