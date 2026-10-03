package mirror

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"emguio/internal/connect/connecttest"
	"emguio/internal/store"
)

// Conversations are the server's THREAD, read once while the mailbox holds what it held, and
// again once it holds more.
func TestConversationsAreTheServersAndReadOnce(t *testing.T) {
	w, n := newFrontedWorld(t)
	for i := 1; i <= 4; i++ {
		w.deliver("INBOX", i, "alice@example.com", "Hello")
	}
	w.sync()
	n.Threads = "(1 (2)(3))(4)"
	ctx := context.Background()
	th, err := w.mirror.Threads(ctx, w.target, "INBOX")
	if err != nil {
		t.Fatal(err)
	}
	if got := th.Of(2); !slices.Equal(got, []uint32{1, 2, 3}) || th.Of(4) != nil || th.UIDValidity != w.mailbox("INBOX").UIDValidity {
		t.Fatalf("of 2 = %v, of 4 = %v", got, th.Of(4))
	}
	if _, err := w.mirror.Threads(ctx, w.target, "INBOX"); err != nil || n.Threaded() != 1 {
		t.Errorf("read again: %v, %d times", err, n.Threaded())
	}
	w.deliver("INBOX", 5, "alice@example.com", "Hello")
	if _, err := w.mirror.Threads(ctx, w.target, "INBOX"); err != nil || n.Threaded() != 2 {
		t.Errorf("after new mail: %v, %d times", err, n.Threaded())
	}
}

// A server without THREAD has no conversations to tell.
func TestWithoutThreadThereAreNoConversations(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.sync()
	if _, err := w.mirror.Threads(context.Background(), w.target, "INBOX"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("threads = %v", err)
	}
}

// The other side of a conversation is found by its Message-IDs: what answers one of its messages,
// and what one of them answers.
func TestTheOtherSideOfAConversationIsFoundByMessageID(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Sent")
	for _, raw := range []string{
		"From: misha@example.com\nTo: alice@example.com\nSubject: Lunch?\nMessage-ID: <mine@example.com>\n\nShall we?\n",
		"From: misha@example.com\nTo: alice@example.com\nSubject: Re: Pizza\nMessage-ID: <reply@example.com>\nIn-Reply-To: <q@example.com>\nReferences: <mine@example.com> <q@example.com>\n\nSushi.\n",
		"From: misha@example.com\nTo: bob@example.com\nSubject: Report\nMessage-ID: <other@example.com>\n\nAttached.\n",
	} {
		connecttest.Append(t, w.server, "Sent", raw, start)
	}
	w.sync()
	ctx := context.Background()
	found, err := w.mirror.Related(ctx, w.target, "Sent", []string{"<q@example.com>"}, []string{"<mine@example.com>"})
	if err != nil {
		t.Fatal(err)
	}
	if got := headers(found.Headers); got != "Re: Pizza, Lunch?" || found.UIDValidity != w.mailbox("Sent").UIDValidity {
		t.Errorf("found %q", got)
	}
	got, err := w.mirror.Headers(ctx, w.target, "Sent", found.UIDValidity, []uint32{3})
	if err != nil || headers(got) != "Report" {
		t.Errorf("headers = %q, %v", headers(got), err)
	}
}

// A folder is made on the server at the top or inside another, and the folders listed after say
// it is there.
func TestAFolderIsMadeAndListed(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Work")
	w.sync()
	ctx := context.Background()
	full, listed, err := w.mirror.CreateMailbox(ctx, w.target, nil, "Projects")
	if err != nil || full != "Projects" {
		t.Fatalf("top = %q, %v", full, err)
	}
	full, listed, err = w.mirror.CreateMailbox(ctx, w.target, w.mailbox("Work"), "Clients")
	if err != nil || full != "Work/Clients" {
		t.Fatalf("inside = %q, %v", full, err)
	}
	var names []string
	for _, l := range listed {
		names = append(names, l.Name)
	}
	slices.Sort(names)
	if strings.Join(names, ", ") != "INBOX, Projects, Work, Work/Clients" {
		t.Errorf("listed %v", names)
	}
	if _, _, err := w.mirror.CreateMailbox(ctx, w.target, nil, "Projects"); err == nil {
		t.Error("made the same folder twice")
	}
}

// A folder is renamed and moved on the server, and deleted once empty; one that holds mail is not
// deleted. What is inside a folder moves with it on a real server; the one here leaves it.
func TestAFolderIsRenamedMovedAndDeleted(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Notification", "Notification/Newsletter", "Work")
	w.deliver("Work", 1, "alice@example.com", "Plans")
	w.sync()
	ctx := context.Background()
	names := func(listed []store.Listed) string {
		var out []string
		for _, l := range listed {
			out = append(out, l.Name)
		}
		slices.Sort(out)
		return strings.Join(out, ", ")
	}

	full, listed, err := w.mirror.RenameMailbox(ctx, w.target, "Notification/Newsletter", nil, "Letters")
	if err != nil || full != "Letters" {
		t.Fatalf("to the top = %q, %v", full, err)
	}
	if got := names(listed); got != "INBOX, Letters, Notification, Work" {
		t.Errorf("listed %s", got)
	}
	full, _, err = w.mirror.RenameMailbox(ctx, w.target, "Letters", w.mailbox("Work"), "Letters")
	if err != nil || full != "Work/Letters" {
		t.Fatalf("inside = %q, %v", full, err)
	}

	listed, err = w.mirror.DeleteMailbox(ctx, w.target, "Notification")
	if err != nil || strings.Contains(names(listed), "Notification") {
		t.Errorf("delete = %s, %v", names(listed), err)
	}
	if _, err := w.mirror.DeleteMailbox(ctx, w.target, "Work"); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("a folder with mail = %v", err)
	}
}
