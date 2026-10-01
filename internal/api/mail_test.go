package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"emguio/internal/message"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// fakeMirror is a mail server's answers, and a record of what it was asked.
type fakeMirror struct {
	mu         sync.Mutex
	reconciled int
	refreshed  []string
	// listing is what a list from the server gets, and listed the cursor of each one asked for.
	listing *mirror.Listing
	listed  []uint32
	// opened is the message on the server, and fetched how many times it was asked for.
	opened  message.Structure
	fetched int
	// parts are its parts by section, a MIME header and a body each.
	parts map[string][2]string
	// flags are the message's on the server, and seen each change of them it was told to make.
	flags store.Flags
	seen  []bool
	err   error
}

func (f *fakeMirror) Reconcile() { f.mu.Lock(); f.reconciled++; f.mu.Unlock() }
func (f *fakeMirror) Refresh(id string) {
	f.mu.Lock()
	f.refreshed = append(f.refreshed, id)
	f.mu.Unlock()
}
func (f *fakeMirror) List(_ context.Context, _ store.SyncTarget, _ string, _, before uint32, _ int) (*mirror.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, before)
	if f.err != nil {
		return nil, f.err
	}
	return f.listing, nil
}
func (f *fakeMirror) Read(_ context.Context, _ store.SyncTarget, _ string, _, uid uint32) (*mirror.Opened, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched++
	if f.err != nil {
		return nil, f.err
	}
	h := store.Header{UID: uid, Flags: f.flags, Subject: "Message A", From: store.Address{Name: "Alice", Email: "alice@example.com"}}
	return &mirror.Opened{Header: h, Structure: f.opened}, nil
}
func (f *fakeMirror) Part(_ context.Context, _ store.SyncTarget, _ string, _, _ uint32, section []int) ([]byte, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, nil, f.err
	}
	var at []string
	for _, n := range section {
		at = append(at, strconv.Itoa(n))
	}
	p := f.parts[strings.Join(at, ".")]
	return []byte(p[0]), []byte(p[1]), nil
}
func (f *fakeMirror) SetSeen(_ context.Context, _ store.SyncTarget, _ string, _, _ uint32, seen bool) (store.Flags, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Flags{}, false, f.err
	}
	if f.flags.Seen == seen {
		return f.flags, false, nil
	}
	f.flags.Seen = seen
	f.seen = append(f.seen, seen)
	return f.flags, true, nil
}

// withMail is a signed-in user with one email config whose INBOX keeps n messages under
// UIDVALIDITY 7, as the mirror would have left it.
func withMail(t *testing.T, n int) (*Server, *store.Store, *client, string, string) {
	t.Helper()
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", configBody(993, 0, "hunter2")).Body).Decode(&made)

	ctx := context.Background()
	st.PutMailboxes(ctx, made.ID, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Work", Delimiter: "/", Selectable: true},
		{Name: "Work/Clients", Delimiter: "/", Selectable: true},
	})
	boxes, _ := st.MirrorMailboxes(ctx, made.ID)
	at := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	var headers []store.Header
	for i := 1; i <= n; i++ {
		headers = append(headers, store.Header{
			UID: uint32(i), InternalDate: at.Add(time.Duration(i) * time.Minute),
			Subject: "Message " + string(rune('A'+i-1)), From: store.Address{Name: "Alice", Email: "alice@example.com"},
		})
	}
	st.PutMessages(ctx, boxes[0].ID, headers)
	st.SetMailboxStatus(ctx, boxes[0].ID, store.MailboxStatus{UIDValidity: 7, UIDNext: uint32(n + 1), Messages: uint32(n), Unseen: uint32(n)})
	return s, st, c, made.ID, boxes[0].ID
}

func TestMailboxesAreListedInSidebarOrder(t *testing.T) {
	_, _, c, cfg, _ := withMail(t, 0)
	got := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))
	boxes := got["mailboxes"].([]any)
	var names []string
	for _, b := range boxes {
		names = append(names, b.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "INBOX,Work,Work/Clients" {
		t.Errorf("names = %v", names)
	}
	clients := boxes[2].(map[string]any)
	if path := clients["path"].([]any); len(path) != 2 || path[1] != "Clients" {
		t.Errorf("path = %v", clients["path"])
	}
}

func subjectsOf(page map[string]any) string {
	var out []string
	for _, m := range page["messages"].([]any) {
		out = append(out, m.(map[string]any)["subject"].(string))
	}
	return strings.Join(out, ",")
}

// INBOX opens on what is kept of it, without asking the server; the rest is the server's.
func TestINBOXOpensOnTheWindowAndGoesOnFromTheServer(t *testing.T) {
	s, st, c, cfg, inbox := withMail(t, 3)
	fake := &fakeMirror{listing: &mirror.Listing{UIDValidity: 7, Headers: []store.Header{{UID: 0x1, Subject: "Older"}}}}
	s.mirror = fake
	base := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages"

	first := c.json(c.do("GET", base, ""))
	if got := subjectsOf(first); got != "Message C,Message B,Message A" {
		t.Fatalf("first page = %q", got)
	}
	if id := first["messages"].([]any)[0].(map[string]any)["id"]; id != "7-3" {
		t.Errorf("id = %v", id)
	}
	if _, more := first["next_cursor"]; more || len(fake.listed) != 0 {
		t.Errorf("all of INBOX is kept, yet next = %v and the server was asked %v", first["next_cursor"], fake.listed)
	}

	// More on the server than is kept: the next run starts below the oldest kept.
	st.SetMailboxStatus(context.Background(), inbox, store.MailboxStatus{UIDValidity: 7, Messages: 40})
	first = c.json(c.do("GET", base, ""))
	if first["next_cursor"] != "7-1" {
		t.Fatalf("next = %v", first["next_cursor"])
	}
	second := c.json(c.do("GET", base+"?cursor=7-1", ""))
	if got := subjectsOf(second); got != "Older" || len(fake.listed) != 1 || fake.listed[0] != 1 {
		t.Errorf("second page = %q, asked from %v", got, fake.listed)
	}
	if _, more := second["next_cursor"]; more {
		t.Error("the last run offers another")
	}

	for _, bad := range []string{"?limit=9999", "?cursor=nonsense", "?cursor=7-0"} {
		if resp := c.do("GET", base+bad, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %s", bad, resp.Status)
		}
	}
}

func TestAnotherMailboxIsListedFromTheServer(t *testing.T) {
	s, _, c, cfg, _ := withMail(t, 1)
	fake := &fakeMirror{listing: &mirror.Listing{UIDValidity: 9, Headers: []store.Header{{UID: 5, Subject: "Contract"}, {UID: 2, Subject: "Brief"}}, More: true}}
	s.mirror = fake
	boxes := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any)
	work := boxes[1].(map[string]any)["id"].(string)

	page := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+work+"/messages", ""))
	if got := subjectsOf(page); got != "Contract,Brief" || page["next_cursor"] != "9-2" || fake.listed[0] != 0 {
		t.Errorf("page = %q next %v asked %v", got, page["next_cursor"], fake.listed)
	}

	// Renumbered since the list was opened: start again, and the sidebar is looked at.
	fake.err = mirror.ErrGone
	resp := c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+work+"/messages?cursor=9-2", "")
	if resp.StatusCode != http.StatusNotFound || c.json(resp)["code"] != CodeGone || len(fake.refreshed) != 1 {
		t.Errorf("renumbered = %s, refreshed %v", resp.Status, fake.refreshed)
	}
}

// Before the first look has filled the window, INBOX too is the server's.
func TestINBOXBeforeTheFirstLookIsTheServers(t *testing.T) {
	s, st, c, cfg, inbox := withMail(t, 0)
	st.ResetMailbox(context.Background(), inbox, 7)
	fake := &fakeMirror{listing: &mirror.Listing{UIDValidity: 7, Headers: []store.Header{{UID: 1, Subject: "Hello"}}}}
	s.mirror = fake
	page := c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes/"+inbox+"/messages", ""))
	if got := subjectsOf(page); got != "Hello" {
		t.Errorf("page = %q", got)
	}
}

func TestAnotherUsersMailboxesAndMessagesAreNotFound(t *testing.T) {
	s, st, _, cfg, inbox := withMail(t, 1)
	user(t, st, "robin", "a good password")
	robin := newClient(t, s)
	robin.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`)
	for _, path := range []string{
		"/api/email-configs/" + cfg + "/mailboxes",
		"/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages",
	} {
		if resp := robin.do("GET", path, ""); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s by another user = %s", path, resp.Status)
		}
	}
	if resp := robin.do("POST", "/api/email-configs/"+cfg+"/sync", ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("sync of another user's config = %s", resp.Status)
	}
}

func TestSavingAConfigTellsTheMirrorAndSyncAsksForALook(t *testing.T) {
	s, st := newServerStore(t, nil)
	fake := &fakeMirror{}
	s.mirror = fake
	c := signIn(t, s, st)
	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", configBody(993, 0, "hunter2")).Body).Decode(&made)
	if fake.reconciled != 1 {
		t.Errorf("reconciled %d times after a create", fake.reconciled)
	}
	if resp := c.do("POST", "/api/email-configs/"+made.ID+"/sync", ""); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("sync = %s", resp.Status)
	}
	if len(fake.refreshed) != 1 || fake.refreshed[0] != made.ID {
		t.Errorf("refreshed %v", fake.refreshed)
	}
}

func TestAConfigCarriesItsSyncState(t *testing.T) {
	_, st, c, cfg, _ := withMail(t, 0)
	st.SetSyncState(context.Background(), cfg, "imap.example.com:993 refused the username or password: no")
	got := c.json(c.do("GET", "/api/email-configs", ""))["email_configs"].([]any)[0].(map[string]any)
	if got["sync_error"] != "imap.example.com:993 refused the username or password: no" || got["synced_at"] != nil {
		t.Errorf("config = %v", got)
	}
}

// The stream carries no payload, only that the user's mail moved.
func TestTheEventStreamSaysWhenMailMoved(t *testing.T) {
	s, st, c, _, _ := withMail(t, 0)
	srv := httptest.NewServer(s)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}

	me := c.json(c.do("GET", "/api/auth/me", ""))
	st.Notify(me["id"].(string))

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-lines:
			if line == "event: changed" {
				return
			}
		case <-deadline:
			t.Fatal("no event arrived")
		}
	}
}
