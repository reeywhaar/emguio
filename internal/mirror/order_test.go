package mirror

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// expunge removes one message from a mailbox on the server, as another client deleting it would.
func (w *world) expunge(mailbox string, uid uint32) {
	w.t.Helper()
	if _, err := w.server.Select(mailbox, nil).Wait(); err != nil {
		w.t.Fatal(err)
	}
	set := imap.UIDSetNum(imap.UID(uid))
	if err := w.server.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		w.t.Fatal(err)
	}
	if err := w.server.UIDExpunge(set).Close(); err != nil {
		w.t.Fatal(err)
	}
}

// dated is each time the server was asked for the dates of a mailbox's messages, by the UIDs
// asked about.
func (w *world) dated() []string {
	w.said.mu.Lock()
	defer w.said.mu.Unlock()
	var out []string
	for _, line := range strings.Split(w.said.buf.String(), "\n") {
		_, rest, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if uids, ok := strings.CutSuffix(rest, " (UID INTERNALDATE)"); ok {
			out = append(out, strings.TrimPrefix(uids, "UID FETCH "))
		}
	}
	return out
}

// Mail moved in from another folder keeps the date it first arrived on, and is listed by it
// rather than as the newest: what is kept of the folder, and every run read from the server.
func TestMailMovedInIsListedByWhenItArrived(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Orders")
	for i := 10; i < 10+FolderWindow+1; i++ {
		w.deliver("Orders", i, "shop@example.com", fmt.Sprintf("o%d", i))
	}
	w.deliver("Orders", 1, "shop@example.com", "receipt")
	w.sync()

	kept, err := w.store.Window(context.Background(), w.mailbox("Orders"))
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(kept); got != "o20, o19, o18, o17, o16, o15, o14, o13, o12, o11" {
		t.Errorf("Orders keeps %q, want its newest by date", got)
	}

	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Orders", After{}, 11, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := headers(first.Headers); !strings.HasPrefix(got, "o20, o19") || !strings.HasSuffix(got, "o11, o10") || !first.More {
		t.Fatalf("first run = %q more %v", got, first.More)
	}
	next, err := w.mirror.List(ctx, w.target, "Orders", after(first.UIDValidity, first.Headers[10]), 11, "")
	if err != nil || headers(next.Headers) != "receipt" || next.More {
		t.Errorf("next run = %+v, %v", next, err)
	}

	found, err := w.mirror.List(ctx, w.target, "Orders", After{}, 3, "from:shop")
	if err != nil || headers(found.Headers) != "o20, o19, o18" {
		t.Errorf("a search = %+v, %v", found, err)
	}
}

// The dates are asked for once, and after that only those of what arrived since: a folder read
// page by page, or looked at again, does not ask for all of them each time.
func TestOnlyWhatArrivedSinceIsDated(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	for i := 1; i <= 5; i++ {
		w.deliver("Archive", i, "alice@example.com", fmt.Sprintf("a%d", i))
	}
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Archive", After{}, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.mirror.List(ctx, w.target, "Archive", after(first.UIDValidity, first.Headers[1]), 2, ""); err != nil {
		t.Fatal(err)
	}
	w.deliver("Archive", 6, "alice@example.com", "a6")
	again, err := w.mirror.List(ctx, w.target, "Archive", After{}, 2, "")
	if err != nil || headers(again.Headers) != "a6, a5" {
		t.Fatalf("after one more = %+v, %v", again, err)
	}
	if got := strings.Join(w.dated(), " "); got != "1:5 6" {
		t.Errorf("dates asked for %q, want all once and then the new one", got)
	}

	// One leaving is told by a search for what is there, without dating the rest again.
	w.expunge("Archive", 3)
	gone, err := w.mirror.List(ctx, w.target, "Archive", after(again.UIDValidity, again.Headers[1]), 5, "")
	if err != nil || headers(gone.Headers) != "a4, a2, a1" {
		t.Errorf("after one left = %+v, %v", gone, err)
	}
	if got := strings.Join(w.dated(), " "); got != "1:5 6" {
		t.Errorf("dates asked for %q", got)
	}
}

// A run goes on from where the last one ended even when the message it ended on has gone since:
// its date says where it was.
func TestARunGoesOnPastAMessageGoneSince(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	for i := 1; i <= 5; i++ {
		w.deliver("Archive", i, "alice@example.com", fmt.Sprintf("a%d", i))
	}
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Archive", After{}, 2, "")
	if err != nil || headers(first.Headers) != "a5, a4" {
		t.Fatalf("first run = %+v, %v", first, err)
	}
	w.expunge("Archive", first.Headers[1].UID)
	next, err := w.mirror.List(ctx, w.target, "Archive", after(first.UIDValidity, first.Headers[1]), 2, "")
	if err != nil || headers(next.Headers) != "a3, a2" || !next.More {
		t.Errorf("next run = %+v, %v", next, err)
	}
}

// A server with SORT says the order itself, in UIDs alone, and nothing is dated.
func TestAServerWithSortSaysTheOrder(t *testing.T) {
	w, front := newFrontedWorld(t)
	front.Sorts = "2 3 1"
	w.create("Archive")
	w.deliver("Archive", 1, "alice@example.com", "a")
	w.deliver("Archive", 2, "alice@example.com", "b")
	w.deliver("Archive", 3, "alice@example.com", "c")
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Archive", After{}, 2, "")
	if err != nil || headers(first.Headers) != "a, c" || !first.More {
		t.Fatalf("first run = %+v, %v", first, err)
	}
	next, err := w.mirror.List(ctx, w.target, "Archive", after(first.UIDValidity, first.Headers[1]), 2, "")
	if err != nil || headers(next.Headers) != "b" || next.More {
		t.Errorf("next run = %+v, %v", next, err)
	}
	if front.Sorted() != 1 || len(w.dated()) != 0 {
		t.Errorf("sorted %d times, dated %v; want sorted once", front.Sorted(), w.dated())
	}
}

// SORT says no dates, so a run after a message gone since is placed by the dates after all.
func TestPastAMessageGoneSinceSortIsDatedAfterAll(t *testing.T) {
	w, front := newFrontedWorld(t)
	front.Sorts = "1 2 3"
	w.create("Archive")
	w.deliver("Archive", 1, "alice@example.com", "a")
	w.deliver("Archive", 2, "alice@example.com", "b")
	w.deliver("Archive", 3, "alice@example.com", "c")
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Archive", After{}, 1, "")
	if err != nil || headers(first.Headers) != "c" {
		t.Fatalf("first run = %+v, %v", first, err)
	}
	w.expunge("Archive", 3)
	front.Sorts = "1 2"
	next, err := w.mirror.List(ctx, w.target, "Archive", after(first.UIDValidity, first.Headers[0]), 5, "")
	if err != nil || headers(next.Headers) != "b, a" {
		t.Errorf("next run = %+v, %v", next, err)
	}
	if got := w.dated(); len(got) != 1 {
		t.Errorf("dated %v, want once", got)
	}
}

// A pass dates a folder only when what it holds changed: one where only a flag moved keeps the
// newest it kept.
func TestAPassDatesOnlyAFolderThatChanged(t *testing.T) {
	w := newWorld(t, "hunter2")
	for i := 1; i <= 3; i++ {
		w.deliver("INBOX", i, "alice@example.com", fmt.Sprintf("m%d", i))
	}
	w.sync()
	w.mirror.orders = nil
	w.onServer(2, imap.StoreFlagsAdd, imap.FlagSeen)
	w.sync()
	if got := w.dated(); len(got) != 1 {
		t.Errorf("dated %v, want only on the first pass", got)
	}
	if msgs := w.window(); subjects(msgs) != "m3, m2, m1" || !msgs[1].Flags.Seen {
		t.Errorf("INBOX keeps %q", subjects(msgs))
	}
}

// What is kept of a folder from before, chosen by highest UID, is chosen again by date on the
// first pass after emguio starts, though nothing in the folder changed.
func TestAWindowKeptByAnOlderRuleIsChosenAgain(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Orders")
	for i := 10; i < 10+FolderWindow+1; i++ {
		w.deliver("Orders", i, "shop@example.com", fmt.Sprintf("o%d", i))
	}
	w.deliver("Orders", 1, "shop@example.com", "receipt")
	w.sync()

	// As the older rule kept it: the receipt, moved in last, in place of o11.
	ctx := context.Background()
	mb := w.mailbox("Orders")
	receipt, err := w.mirror.Headers(ctx, w.target, "Orders", mb.UIDValidity, []uint32{12})
	if err != nil || len(receipt) != 1 {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if err := w.store.DeleteMessages(ctx, mb.ID, []uint32{2}); err != nil {
		t.Fatal(err)
	}
	if err := w.store.PutMessages(ctx, mb.ID, receipt); err != nil {
		t.Fatal(err)
	}

	w.mirror.windowed = map[string]bool{}
	w.sync()
	kept, err := w.store.Window(ctx, w.mailbox("Orders"))
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(kept); got != "o20, o19, o18, o17, o16, o15, o14, o13, o12, o11" {
		t.Errorf("Orders keeps %q, want its newest by date", got)
	}
}

// A slow list says how long each step took: waiting for the session, signing in, opening the
// folder, ordering it, reading the headers and the previews.
func TestASlowListSaysWhatTookHowLong(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	w.deliver("Archive", 1, "alice@example.com", "a1")
	var logged bytes.Buffer
	w.mirror.log = slog.New(slog.NewTextHandler(&logged, nil))
	was := slowList
	slowList = 0
	t.Cleanup(func() { slowList = was })

	if _, err := w.mirror.List(context.Background(), w.target, "Archive", After{}, 10, "a1"); err != nil {
		t.Fatal(err)
	}
	line := logged.String()
	for _, key := range []string{"took=", "waiting=", "signing_in=", "opening=", "messages=1", "ordering=", "searching=", "headers=", "previews="} {
		if !strings.Contains(line, key) {
			t.Errorf("the log says %q, without %s", line, key)
		}
	}
	if strings.Contains(line, "signing_in=0s") {
		t.Errorf("the first list signed in, yet the log says %q", line)
	}
}
