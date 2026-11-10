package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// notifyWorld is mailWorld's INBOX and a Junk folder beside it, none of it new yet, a device to
// tell, and the clock at nine.
func notifyWorld(t *testing.T) (*Store, *User, *EmailConfig, map[string]*Mailbox, *time.Time) {
	t.Helper()
	st, u, cfg, _ := mailWorld(t, 3)
	ctx := context.Background()
	if _, err := st.PutMailboxes(ctx, cfg.ID, []Listed{
		{Name: "INBOX", SpecialUse: UseInbox, Selectable: true},
		{Name: "Junk", SpecialUse: UseJunk, Selectable: true},
		{Name: "Lists", Selectable: true},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return now })
	if _, err := st.RegisterDevice(ctx, u.ID, phone("a")); err != nil {
		t.Fatal(err)
	}
	return st, u, cfg, boxesOf(t, st, cfg.ID), &now
}

func boxesOf(t *testing.T, st *Store, configID string) map[string]*Mailbox {
	t.Helper()
	boxes, err := st.MirrorMailboxes(context.Background(), configID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]*Mailbox{}
	for _, mb := range boxes {
		out[mb.Name] = mb
	}
	return out
}

func phone(name string) DeviceInput {
	return DeviceInput{Endpoint: "https://push.example.com/" + name, P256dh: "BKey" + name, Auth: "auth" + name,
		Label: "Safari on iPhone", ClientID: "client-" + name}
}

func arrival(uid uint32, from, subject, messageID string) Header {
	return Header{UID: uid, InternalDate: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		From: Address{Name: from, Email: "x@example.com"}, Subject: subject, MessageID: messageID}
}

// arrive keeps headers in mb as the mirror does: the window first, then what is new.
func arrive(t *testing.T, st *Store, mb *Mailbox, headers ...Header) int {
	t.Helper()
	ctx := context.Background()
	if err := st.PutMessages(ctx, mb.ID, headers); err != nil {
		t.Fatal(err)
	}
	n, err := st.Arrived(ctx, mb, headers)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func due(t *testing.T, st *Store) ([]Batch, time.Time) {
	t.Helper()
	batches, next, err := st.Due(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return batches, next
}

func subjects(b Batch) []string {
	var out []string
	for _, a := range b.Arrivals {
		out = append(out, a.Subject)
	}
	return out
}

func TestMailIsDueAtOnceNewestFirst(t *testing.T) {
	st, _, _, boxes, _ := notifyWorld(t)
	arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>"), arrival(11, "Bob", "Report", "<2@x>"))

	batches, _ := due(t, st)
	if len(batches) != 1 {
		t.Fatalf("%d batches due", len(batches))
	}
	b := batches[0]
	if got := subjects(b); len(got) != 2 || got[0] != "Report" || got[1] != "Lunch" {
		t.Errorf("due %v", got)
	}
	if !b.Alone || b.Digest || b.Label != "Work" {
		t.Errorf("batch %+v", b)
	}
	if err := st.Settle(context.Background(), b.Settles); err != nil {
		t.Fatal(err)
	}
	if batches, _ := due(t, st); len(batches) != 0 {
		t.Errorf("settled mail is due again: %+v", batches)
	}
}

// Gmail files a message under a label and in INBOX at once, and a rule can move one later.
func TestAMessageIsToldOnceWhicheverFoldersItIsIn(t *testing.T) {
	st, _, _, boxes, _ := notifyWorld(t)
	if n := arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>")); n != 1 {
		t.Fatalf("kept %d", n)
	}
	if n := arrive(t, st, boxes["Lists"], arrival(4, "Ann", "Lunch", "<1@x>")); n != 0 {
		t.Errorf("the same message was kept again from another folder")
	}
}

func TestTheServersOwnFoldersAreQuietUntilChosen(t *testing.T) {
	st, u, cfg, boxes, _ := notifyWorld(t)
	if n := arrive(t, st, boxes["Junk"], arrival(1, "Spam", "Win", "<s@x>")); n != 0 {
		t.Errorf("Junk kept %d", n)
	}
	ctx := context.Background()
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyNow, 0,
		map[string]bool{boxes["Junk"].ID: true, boxes["INBOX"].ID: false}); err != nil {
		t.Fatal(err)
	}
	boxes = boxesOf(t, st, cfg.ID)
	if n := arrive(t, st, boxes["Junk"], arrival(2, "Spam", "Win again", "<s2@x>")); n != 1 {
		t.Errorf("Junk chosen kept %d", n)
	}
	if n := arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>")); n != 0 {
		t.Errorf("INBOX turned off kept %d", n)
	}
}

func TestNothingIsKeptWithoutADeviceToTell(t *testing.T) {
	st, u, _, boxes, _ := notifyWorld(t)
	ctx := context.Background()
	devices, _ := st.Devices(ctx, u.ID)
	if err := st.DeleteDevice(ctx, u.ID, devices[0].ID); err != nil {
		t.Fatal(err)
	}
	if n := arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>")); n != 0 {
		t.Errorf("kept %d with nowhere to tell", n)
	}
}

func TestADigestIsDueItsIntervalAfterWhatArrivedFirst(t *testing.T) {
	st, u, cfg, boxes, now := notifyWorld(t)
	ctx := context.Background()
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyDigest, 30, nil); err != nil {
		t.Fatal(err)
	}
	arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>"))
	*now = now.Add(20 * time.Minute)
	arrive(t, st, boxes["INBOX"], arrival(11, "Bob", "Report", "<2@x>"))

	batches, next := due(t, st)
	if len(batches) != 0 {
		t.Fatalf("due after 20 minutes of 30: %+v", batches)
	}
	if want := time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC); !next.Equal(want) {
		t.Errorf("next at %s, want %s", next, want)
	}
	*now = now.Add(10 * time.Minute)
	batches, _ = due(t, st)
	if len(batches) != 1 || !batches[0].Digest || len(batches[0].Arrivals) != 2 {
		t.Fatalf("due %+v", batches)
	}
}

func TestWhatWasReadOrMovedBeforeItIsToldIsLeftOut(t *testing.T) {
	st, u, cfg, boxes, _ := notifyWorld(t)
	ctx := context.Background()
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyDigest, 60, nil); err != nil {
		t.Fatal(err)
	}
	inbox := boxes["INBOX"]
	arrive(t, st, inbox, arrival(10, "Ann", "Read", "<1@x>"), arrival(11, "Bob", "Moved", "<2@x>"),
		arrival(12, "Cat", "Waiting", "<3@x>"))
	if err := st.SetFlags(ctx, inbox.ID, map[uint32]Flags{10: {Seen: true}}); err != nil {
		t.Fatal(err)
	}
	if err := st.MessageMoved(ctx, inbox.ID, "", 11, false); err != nil {
		t.Fatal(err)
	}
	st.SetClock(func() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC) })

	batches, _ := due(t, st)
	if len(batches) != 1 {
		t.Fatalf("%d batches", len(batches))
	}
	if got := subjects(batches[0]); len(got) != 1 || got[0] != "Waiting" {
		t.Errorf("tells %v", got)
	}
	if len(batches[0].Settles) != 3 {
		t.Errorf("settles %d of 3", len(batches[0].Settles))
	}
}

func TestTurningNotificationsOffSettlesWhatWasWaiting(t *testing.T) {
	st, u, cfg, boxes, _ := notifyWorld(t)
	ctx := context.Background()
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyDigest, 240, nil); err != nil {
		t.Fatal(err)
	}
	arrive(t, st, boxes["INBOX"], arrival(10, "Ann", "Lunch", "<1@x>"))
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyOff, 0, nil); err != nil {
		t.Fatal(err)
	}
	if batches, next := due(t, st); len(batches) != 0 || !next.IsZero() {
		t.Errorf("off still has %+v due, next %s", batches, next)
	}
	if err := st.SetNotifications(ctx, u.ID, cfg.ID, NotifyNow, 0, nil); err != nil {
		t.Fatal(err)
	}
	if batches, _ := due(t, st); len(batches) != 0 {
		t.Errorf("what arrived while off is told after: %+v", batches)
	}
	c, _ := st.EmailConfig(ctx, u.ID, cfg.ID)
	if c.Notify != NotifyNow || c.DigestEvery != 240 {
		t.Errorf("notify %q every %d, want now and the 240 kept", c.Notify, c.DigestEvery)
	}
}

func TestNotificationsAreChosenOnlyFromWhatThereIs(t *testing.T) {
	st, u, cfg, boxes, _ := notifyWorld(t)
	ctx := context.Background()
	for _, c := range []struct {
		notify  string
		every   int
		folders map[string]bool
		want    error
	}{
		{"sometimes", 0, nil, ErrInvalid},
		{NotifyDigest, 45, nil, ErrInvalid},
		{NotifyNow, 0, map[string]bool{"inbox": true}, ErrInvalid},
		{NotifyNow, 0, map[string]bool{"mb_01k6r2p3q5s7t9v1w3x5y7z9a1": true}, ErrNotFound},
	} {
		if err := st.SetNotifications(ctx, u.ID, cfg.ID, c.notify, c.every, c.folders); !errors.Is(err, c.want) {
			t.Errorf("%q %d %v: %v, want %v", c.notify, c.every, c.folders, err, c.want)
		}
	}
	stranger := newUser(t, st, "stranger")
	if err := st.SetNotifications(ctx, stranger.ID, cfg.ID, NotifyOff, 0, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's config: %v", err)
	}
	if !boxes["INBOX"].Notifies() || boxes["Junk"].Notifies() || !boxes["Lists"].Notifies() {
		t.Error("INBOX and the user's own notify by default, Junk does not")
	}
}
