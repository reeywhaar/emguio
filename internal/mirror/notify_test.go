package mirror

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

// running starts the mirror, and waits for its worker to be waiting in INBOX.
func (w *world) running() {
	w.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	w.t.Cleanup(cancel)
	go w.mirror.Run(ctx)
	eventually(w.t, func() bool { return w.said.commands("IDLE") > 0 })
}

const (
	notifyAll  = "SET (SELECTED (MessageNew MessageExpunge FlagChange)) (PERSONAL (MessageNew MessageExpunge FlagChange MailboxName))"
	notifyLess = "SET (SELECTED (MessageNew MessageExpunge FlagChange)) (PERSONAL (MessageNew MessageExpunge MailboxName))"
)

// Under NOTIFY, a folder other than INBOX is counted as soon as the server says it moved rather
// than at the next full pass, and the user told; the session goes on waiting after.
func TestAFolderTheServerSaysMovedIsCountedAtOnce(t *testing.T) {
	w, n := newFrontedWorld(t)
	w.create("Work")
	w.running()
	if got := n.Asked(); len(got) != 1 || got[0] != notifyAll {
		t.Fatalf("asked %q", got)
	}
	lists := w.said.commands("LIST")
	changes, stop := w.store.Watch(w.target.UserID)
	defer stop()

	w.deliver("Work", 1, "alice@example.com", "Plans")
	idles := w.said.commands("IDLE")
	n.Tell(`* STATUS "Work" (MESSAGES 1 UIDNEXT 2)`)
	eventually(t, func() bool {
		mb := w.mailbox("Work")
		return mb.Messages == 1 && mb.Unseen == 1
	})
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("nobody was told")
	}
	if w.said.commands("LIST") != lists {
		t.Error("a full pass was made")
	}
	eventually(t, func() bool { return w.said.commands("IDLE") > idles })
}

// A folder made elsewhere is listed as soon as the server says so.
func TestAFolderMadeElsewhereIsListedAtOnce(t *testing.T) {
	w, n := newFrontedWorld(t)
	w.running()
	w.create("Later")
	n.Tell(`* LIST () "/" "Later"`)
	eventually(t, func() bool { return w.mailbox("Later") != nil })
}

// A server that will not say a change of flags elsewhere is asked for the rest, and one that drops
// NOTIFY is asked again, after a full pass for what it did not say.
func TestNotifyIsAskedForLessOrAgain(t *testing.T) {
	w, n := newFrontedWorld(t)
	n.Refuse = func(asked string) bool { return strings.Count(asked, "FlagChange") > 1 }
	w.running()
	if got := n.Asked(); len(got) != 2 || got[1] != notifyLess {
		t.Fatalf("asked %q", got)
	}
	lists := w.said.commands("LIST")
	n.Tell("* OK [NOTIFICATIONOVERFLOW] Too many changes")
	eventually(t, func() bool { return len(n.Asked()) == 4 && w.said.commands("LIST") > lists })
}

// Refused NOTIFY altogether, the session waits in INBOX as it would on a server without it.
func TestARefusedNotifyLeavesTheWaitAsItWas(t *testing.T) {
	w, n := newFrontedWorld(t)
	n.Refuse = func(string) bool { return true }
	w.deliver("INBOX", 1, "alice@example.com", "First")
	w.running()
	w.deliver("INBOX", 2, "alice@example.com", "Second")
	eventually(t, func() bool { return subjects(w.window()) == "Second, First" })
	if got := len(n.Asked()); got != 2 {
		t.Errorf("asked %d times", got)
	}
}

// A mailbox whose counts are as they were, but which holds a new message in place of another — a
// draft saved again — has changed, and a pass says so.
func TestANewMessageInPlaceOfAnotherIsAChange(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Drafts")
	w.deliver("Drafts", 1, "misha@example.com", "Half")
	w.sync()
	if _, err := w.server.Select("Drafts", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := w.server.Store(imap.UIDSetNum(1), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.server.Expunge().Close(); err != nil {
		t.Fatal(err)
	}
	w.deliver("Drafts", 2, "misha@example.com", "Whole")

	ctx := context.Background()
	s, err := w.mirror.open(ctx, w.target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.client.Close()
	changed, err := s.pass(ctx, true, nil)
	if err != nil || !changed {
		t.Fatalf("pass = %v, %v", changed, err)
	}
	if mb := w.mailbox("Drafts"); mb.Messages != 1 || mb.UIDNext != 3 {
		t.Errorf("Drafts = %d messages, next %d", mb.Messages, mb.UIDNext)
	}
}

// Mail arriving after a pass read INBOX, but before the session waits there, is looked at at
// once rather than waiting for the next to arrive.
func TestMailBetweenAPassAndTheWaitIsNotMissed(t *testing.T) {
	w := newWorld(t, "hunter2")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := w.mirror.open(ctx, w.target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.client.Close()
	if _, err := s.pass(ctx, true, nil); err != nil {
		t.Fatal(err)
	}
	w.deliver("INBOX", 1, "alice@example.com", "Between")
	if why, err := s.wait(ctx, nil, nil); why != arrived || err != nil {
		t.Fatalf("wait = %d, %v", why, err)
	}
}
