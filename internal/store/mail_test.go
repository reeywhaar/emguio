package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mailWorld is one user with one email config and one mailbox holding n messages, a minute apart.
func mailWorld(t *testing.T, n int) (*Store, *User, *EmailConfig, *Mailbox) {
	t.Helper()
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutMailboxes(ctx, cfg.ID, []Listed{{Name: "INBOX", SpecialUse: UseInbox, Selectable: true}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.MirrorMailboxes(ctx, cfg.ID)
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	var headers []Header
	for i := 1; i <= n; i++ {
		headers = append(headers, Header{UID: uint32(i), InternalDate: at.Add(time.Duration(i) * time.Minute), Subject: subject(i)})
	}
	if err := st.PutMessages(ctx, boxes[0].ID, headers); err != nil {
		t.Fatal(err)
	}
	return st, u, cfg, boxes[0]
}

func subject(i int) string { return "m" + string(rune('a'+i-1)) }

func TestTheWindowIsNewestFirstAndNamedByTheServersNumbers(t *testing.T) {
	st, _, _, mb := mailWorld(t, 3)
	mb.UIDValidity = 77
	got, err := st.Window(context.Background(), mb)
	if err != nil {
		t.Fatal(err)
	}
	var subjects, ids []string
	for _, m := range got {
		subjects = append(subjects, m.Subject)
		ids = append(ids, m.ID())
	}
	if strings.Join(subjects, ",") != "mc,mb,ma" || strings.Join(ids, ",") != "77-3,77-2,77-1" {
		t.Errorf("window = %v %v", subjects, ids)
	}
}

func TestAnotherUsersMailboxIsNotFound(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	if got, err := st.Mailbox(ctx, u.ID, cfg.ID, mb.ID); err != nil || got.Name != "INBOX" {
		t.Fatalf("own mailbox = %v, %v", got, err)
	}
	robin := newUser(t, st, "robin")
	if _, err := st.Mailboxes(ctx, robin.ID, cfg.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin's mailboxes of misha's config = %v", err)
	}
	if _, err := st.Mailbox(ctx, robin.ID, cfg.ID, mb.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin's look at misha's mailbox = %v", err)
	}

	// Nor through a config of their own: the mailbox has to belong to the config named.
	own, _ := st.CreateEmailConfig(ctx, robin.ID, draft())
	if _, err := st.Mailbox(ctx, robin.ID, own.ID, mb.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("misha's mailbox through robin's config = %v", err)
	}
	if _, err := st.Mailbox(ctx, u.ID, cfg.ID, "INBOX"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a name for an id = %v", err)
	}
}

// Another host or another username is another mailbox store; what was copied from the old one
// is not this config's mail any more.
func TestPointingAConfigElsewhereDropsWhatWasCopied(t *testing.T) {
	st, u, cfg, _ := mailWorld(t, 2)
	ctx := context.Background()

	edit := draft()
	edit.Name = "Renamed"
	edit.Incoming.Port = 143
	edit.Incoming.TLS = TLSStartTLS
	st.UpdateEmailConfig(ctx, u.ID, cfg.ID, edit)
	if boxes, _ := st.Mailboxes(ctx, u.ID, cfg.ID); len(boxes) != 1 {
		t.Fatalf("a rename and a new port dropped the copy: %d mailboxes", len(boxes))
	}

	edit.Incoming.Username = "someone-else@example.com"
	st.UpdateEmailConfig(ctx, u.ID, cfg.ID, edit)
	if boxes, _ := st.Mailboxes(ctx, u.ID, cfg.ID); len(boxes) != 0 {
		t.Errorf("another account's mail is still listed: %d mailboxes", len(boxes))
	}
}

func TestPuttingMailboxesAddsUpdatesAndDrops(t *testing.T) {
	st, _, cfg, _ := mailWorld(t, 1)
	ctx := context.Background()
	changed, err := st.PutMailboxes(ctx, cfg.ID, []Listed{
		{Name: "INBOX", SpecialUse: UseInbox, Selectable: true},
		{Name: "Sent", SpecialUse: UseSent, Selectable: true},
	})
	if err != nil || !changed {
		t.Fatalf("adding Sent = %v, %v", changed, err)
	}
	if changed, _ := st.PutMailboxes(ctx, cfg.ID, []Listed{
		{Name: "INBOX", SpecialUse: UseInbox, Selectable: true},
		{Name: "Sent", SpecialUse: UseSent, Selectable: true},
	}); changed {
		t.Error("the same list again reported a change")
	}
	st.PutMailboxes(ctx, cfg.ID, []Listed{{Name: "Sent", SpecialUse: UseSent, Selectable: true}})
	boxes, _ := st.MirrorMailboxes(ctx, cfg.ID)
	if len(boxes) != 1 || boxes[0].Name != "Sent" {
		t.Errorf("boxes = %v", boxes)
	}
	var n int
	st.reader.QueryRow(`SELECT count(*) FROM messages`).Scan(&n)
	if n != 0 {
		t.Errorf("%d messages outlived INBOX", n)
	}
}

func TestResettingAMailboxDropsItsMessages(t *testing.T) {
	st, _, cfg, mb := mailWorld(t, 3)
	ctx := context.Background()
	if err := st.ResetMailbox(ctx, mb.ID, 99); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.MirrorMailboxes(ctx, cfg.ID)
	window, _ := st.Window(ctx, boxes[0])
	if len(window) != 0 || boxes[0].UIDValidity != 99 || boxes[0].SyncedAt != nil {
		t.Errorf("after a reset: %d messages, %+v", len(window), boxes[0])
	}
}

func TestKeepingAMessageTwiceUpdatesOnlyItsFlags(t *testing.T) {
	st, _, _, mb := mailWorld(t, 1)
	ctx := context.Background()
	st.PutMessages(ctx, mb.ID, []Header{{UID: 1, InternalDate: time.Now(), Subject: "ignored", Flags: Flags{Seen: true}}})
	after, _ := st.Window(ctx, mb)
	if m := after[0]; m.Subject != "ma" || !m.Flags.Seen {
		t.Errorf("after keeping again: %+v", m)
	}
}

// The count moves only when the server's flag did, kept message or not.
func TestMarkingReadMovesTheRowAndTheCount(t *testing.T) {
	st, _, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	st.SetMailboxStatus(ctx, mb.ID, MailboxStatus{UIDValidity: 1, Messages: 40, Unseen: 5})

	st.SetSeen(ctx, mb.ID, 1, true, true)
	st.SetSeen(ctx, mb.ID, 39, true, true)
	st.SetSeen(ctx, mb.ID, 38, true, false)
	boxes, _ := st.MirrorMailboxes(ctx, cfg.ID)
	window, _ := st.Window(ctx, mb)
	if !window[0].Flags.Seen || boxes[0].Unseen != 3 {
		t.Errorf("seen = %v, unseen = %d", window[0].Flags.Seen, boxes[0].Unseen)
	}
}

func TestABetterPreviewReplacesAKeptOne(t *testing.T) {
	st, _, _, mb := mailWorld(t, 1)
	ctx := context.Background()
	if changed, _ := st.SetPreview(ctx, mb.ID, 1, "The whole text"); !changed {
		t.Error("a new preview changed nothing")
	}
	if changed, _ := st.SetPreview(ctx, mb.ID, 1, "The whole text"); changed {
		t.Error("the same preview again reported a change")
	}
	if changed, _ := st.SetPreview(ctx, mb.ID, 9, "Not kept"); changed {
		t.Error("a message outside the window reported a change")
	}
}

func TestASidebarIsSpecialMailboxesFirstEachWithItsChildren(t *testing.T) {
	mb := func(name, use string) *Mailbox { return &Mailbox{Name: name, Delimiter: "/", SpecialUse: use} }
	list := []*Mailbox{
		mb("Zebra", ""), mb("Trash", UseTrash), mb("Work-old", ""), mb("Work/Clients", ""),
		mb("Work", ""), mb("Sent", UseSent), mb("INBOX/Later", ""), mb("INBOX", UseInbox), mb("archive", ""),
	}
	SortMailboxes(list)
	var got []string
	for _, m := range list {
		got = append(got, m.Name)
	}
	want := "INBOX,INBOX/Later,Sent,Trash,archive,Work,Work/Clients,Work-old,Zebra"
	if strings.Join(got, ",") != want {
		t.Errorf("order = %s\nwant    %s", strings.Join(got, ","), want)
	}
}
