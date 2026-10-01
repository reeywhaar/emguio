package mirror

import (
	"context"
	"errors"
	"testing"
	"time"

	"emguio/internal/store"
)

// keep makes a draft here, as the API does, due at once.
func (w *world) keep(f store.DraftFields, parts ...store.DraftPart) *store.Draft {
	w.t.Helper()
	d, _, err := w.store.CreateDraft(context.Background(), store.Draft{
		UserID: w.target.UserID, EmailConfigID: w.target.ID, DraftFields: f,
	}, parts, w.store.Now())
	if err != nil {
		w.t.Fatal(err)
	}
	return d
}

// drafts is what the mail server holds in Drafts.
func (w *world) drafts() []store.Header {
	w.t.Helper()
	listing, err := w.mirror.List(context.Background(), w.target, "Drafts", 0, 0, 10, "")
	if err != nil {
		w.t.Fatal(err)
	}
	return listing.Headers
}

// A draft kept here is written to Drafts, each time in place of the copy before, and once its
// window is closed and it is written, it goes from here.
func TestADraftIsWrittenInPlaceOfItsLastCopy(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Drafts")
	w.sync()
	ctx := context.Background()
	d := w.keep(store.DraftFields{To: "bob@", Subject: "Half"}, store.DraftPart{Name: "notes.txt", Type: "text/plain", Data: []byte("the notes")})
	if err := w.mirror.WriteDraft(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	first, err := w.store.MirrorDraft(ctx, d.ID)
	if err != nil || first.Written != 1 || first.KeptMessage == "" || !first.DueAt.IsZero() {
		t.Fatalf("after writing = %+v, %v", first, err)
	}
	if h := w.drafts(); len(h) != 1 || h[0].Subject != "Half" || !h[0].HasAttachments {
		t.Fatalf("Drafts holds %+v", h)
	}

	if _, _, err := w.store.SaveDraft(ctx, w.target.UserID, d.ID, store.DraftFields{Subject: "Whole"}, nil, nil, w.store.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := w.mirror.WriteDraft(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if h := w.drafts(); len(h) != 1 || h[0].Subject != "Whole" {
		t.Fatalf("Drafts holds %+v", h)
	}
	if _, err := w.store.MirrorDraft(ctx, d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a closed draft written is still here: %v", err)
	}
}

// The writer takes a draft up when it comes due, without being asked.
func TestADueDraftIsWrittenByItself(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Drafts")
	w.sync()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.mirror.Run(ctx)
	w.keep(store.DraftFields{Subject: "On its own"})
	w.mirror.WakeDrafts()
	eventually(t, func() bool { h := w.drafts(); return len(h) == 1 && h[0].Subject == "On its own" })
}

// With no Drafts folder a draft has nowhere to go, which trying again will not change.
func TestADraftWithNowhereToGoSaysSo(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.sync()
	ctx := context.Background()
	d := w.keep(store.DraftFields{Subject: "Lost"})
	if err := w.mirror.WriteDraft(ctx, d.ID); !errors.Is(err, ErrNoDrafts) {
		t.Fatalf("write = %v", err)
	}
	got, err := w.store.MirrorDraft(ctx, d.ID)
	if err != nil || got.Problem != "This mail account has no Drafts folder to keep drafts in." || !got.DueAt.IsZero() {
		t.Errorf("after = %+v, %v", got, err)
	}
}

// A read does not wait for a change under way: each has a session of its own.
func TestAReadDoesNotWaitForAChange(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Hello")
	w.sync()
	f := w.mirror.fetcherFor(w.target, true)
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := w.mirror.Read(ctx, w.target, "INBOX", w.mailbox("INBOX").UIDValidity, 1); err != nil {
		t.Fatal(err)
	}
}
