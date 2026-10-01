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

func TestAMailboxPagesNewestFirstWithoutRepeatsOrGaps(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 5)
	ctx := context.Background()

	var got []string
	cursor := ""
	for pages := 0; ; pages++ {
		page, err := st.Messages(ctx, u.ID, cfg.ID, mb.ID, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Messages {
			got = append(got, m.Subject)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
		if pages > 5 {
			t.Fatal("the cursor never ran out")
		}
	}
	if strings.Join(got, ",") != "me,md,mc,mb,ma" {
		t.Errorf("paged %v", got)
	}
}

// A position rather than an offset: mail arriving between pages does not shift the next one.
func TestNewMailDoesNotShiftTheNextPage(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 4)
	ctx := context.Background()
	first, _ := st.Messages(ctx, u.ID, cfg.ID, mb.ID, "", 2)

	st.PutMessages(ctx, mb.ID, []Header{{UID: 9, InternalDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), Subject: "new"}})
	second, _ := st.Messages(ctx, u.ID, cfg.ID, mb.ID, first.Next, 2)
	if len(second.Messages) != 2 || second.Messages[0].Subject != "mb" {
		t.Errorf("second page = %v", second.Messages)
	}
}

func TestACursorThisDidNotGiveOutIsRefused(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	if _, err := st.Messages(context.Background(), u.ID, cfg.ID, mb.ID, "nonsense", 2); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want invalid", err)
	}
}

func TestAnotherUsersMailIsNotFound(t *testing.T) {
	st, _, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	robin := newUser(t, st, "robin")
	if _, err := st.Mailboxes(ctx, robin.ID, cfg.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin's mailboxes of misha's config = %v", err)
	}
	if _, err := st.Messages(ctx, robin.ID, cfg.ID, mb.ID, "", 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin's messages of misha's mailbox = %v", err)
	}

	// Nor through a config of their own: the mailbox has to belong to the config named.
	own, _ := st.CreateEmailConfig(ctx, robin.ID, draft())
	if _, err := st.Messages(ctx, robin.ID, own.ID, mb.ID, "", 10); !errors.Is(err, ErrNotFound) {
		t.Errorf("misha's mailbox through robin's config = %v", err)
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
	st, u, cfg, mb := mailWorld(t, 3)
	ctx := context.Background()
	if err := st.ResetMailbox(ctx, mb.ID, 99); err != nil {
		t.Fatal(err)
	}
	page, _ := st.Messages(ctx, u.ID, cfg.ID, mb.ID, "", 10)
	boxes, _ := st.MirrorMailboxes(ctx, cfg.ID)
	if len(page.Messages) != 0 || boxes[0].UIDValidity != 99 || boxes[0].SyncedAt != nil {
		t.Errorf("after a reset: %d messages, %+v", len(page.Messages), boxes[0])
	}
}

func TestStoringAMessageTwiceUpdatesItsFlagsAndKeepsItsID(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	before, _ := st.Messages(ctx, u.ID, cfg.ID, mb.ID, "", 10)
	st.PutMessages(ctx, mb.ID, []Header{{UID: 1, InternalDate: time.Now(), Subject: "ignored", Flags: Flags{Seen: true}}})
	after, _ := st.Messages(ctx, u.ID, cfg.ID, mb.ID, "", 10)
	m := after.Messages[0]
	if m.ID != before.Messages[0].ID || m.Subject != "ma" || !m.Flags.Seen {
		t.Errorf("after storing again: %+v", m)
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
