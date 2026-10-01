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
	// listing is what a list from the server gets, listed the cursor of each one asked for, and
	// searched the query of each that had one.
	listing  *mirror.Listing
	listed   []uint32
	searched []string
	// opened is the message on the server, and fetched how many times it was asked for.
	opened  message.Structure
	fetched int
	// parts are its parts by section, a MIME header and a body each.
	parts map[string][2]string
	// kicked is each config told it has a job waiting.
	kicked []string
	// origin is what a reply reads of its original, and sendings what was sent.
	origin   *mirror.Origin
	sendings []mirror.Sending
	// threads are the server's conversations, nil for a server without THREAD; heads what it holds
	// of each UID; related what a search of another mailbox finds, and what each search asked.
	threads *mirror.Threads
	heads   map[uint32]store.Header
	related *mirror.Listing
	asked   [][]string
	// store is where the drafts it holds and forgets are kept; written each draft written, and
	// woken how many times it was told one was saved.
	store   *store.Store
	written []string
	woken   int
	err     error
}

func (f *fakeMirror) Threads(context.Context, store.SyncTarget, string) (*mirror.Threads, error) {
	if f.threads == nil {
		return nil, mirror.ErrUnsupported
	}
	return f.threads, nil
}
func (f *fakeMirror) Headers(_ context.Context, _ store.SyncTarget, _ string, _ uint32, uids []uint32) ([]store.Header, error) {
	var out []store.Header
	for _, uid := range uids {
		out = append(out, f.heads[uid])
	}
	return out, nil
}
func (f *fakeMirror) Related(_ context.Context, _ store.SyncTarget, _ string, answered, answers []string) (*mirror.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, append(append([]string{}, answered...), answers...))
	if f.related == nil {
		return &mirror.Listing{}, nil
	}
	return f.related, nil
}
func (f *fakeMirror) CreateMailbox(ctx context.Context, t store.SyncTarget, parent *store.Mailbox, name string) (string, []store.Listed, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	full := name
	if parent != nil {
		full = parent.Name + parent.Delimiter + name
	}
	known, err := f.store.MirrorMailboxes(ctx, t.ID)
	if err != nil {
		return "", nil, err
	}
	listed := []store.Listed{{Name: full, Delimiter: "/", Selectable: true}}
	for _, mb := range known {
		listed = append(listed, store.Listed{Name: mb.Name, Delimiter: mb.Delimiter, SpecialUse: mb.SpecialUse, Selectable: mb.Selectable})
	}
	return full, listed, nil
}

// listedAs is the config's folders as the server would list them, each name passed through rename.
func (f *fakeMirror) listedAs(ctx context.Context, t store.SyncTarget, rename func(string) (string, bool)) ([]store.Listed, error) {
	known, err := f.store.MirrorMailboxes(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	var listed []store.Listed
	for _, mb := range known {
		name, keep := rename(mb.Name)
		if keep {
			listed = append(listed, store.Listed{Name: name, Delimiter: mb.Delimiter, SpecialUse: mb.SpecialUse, Selectable: mb.Selectable})
		}
	}
	return listed, nil
}

func (f *fakeMirror) RenameMailbox(ctx context.Context, t store.SyncTarget, from string, parent *store.Mailbox, name string) (string, []store.Listed, error) {
	if f.err != nil {
		return "", nil, f.err
	}
	full := name
	if parent != nil {
		full = parent.Name + parent.Delimiter + name
	}
	listed, err := f.listedAs(ctx, t, func(n string) (string, bool) {
		if n == from {
			return full, true
		}
		if rest, ok := strings.CutPrefix(n, from+"/"); ok {
			return full + "/" + rest, true
		}
		return n, true
	})
	return full, listed, err
}

func (f *fakeMirror) DeleteMailbox(ctx context.Context, t store.SyncTarget, name string) ([]store.Listed, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.listedAs(ctx, t, func(n string) (string, bool) { return n, n != name })
}

func (f *fakeMirror) WakeDrafts() { f.mu.Lock(); f.woken++; f.mu.Unlock() }
func (f *fakeMirror) WriteDraft(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, id)
	return f.err
}
func (f *fakeMirror) HoldDraft(ctx context.Context, userID, id string) (*store.Draft, []store.DraftPart, error) {
	d, err := f.store.HoldDraft(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	parts, err := f.store.DraftParts(ctx, id, true)
	return d, parts, err
}
func (f *fakeMirror) ForgetDraft(ctx context.Context, userID, id string) (*store.Draft, error) {
	return f.store.DeleteDraft(ctx, userID, id)
}

func (f *fakeMirror) Origin(context.Context, store.SyncTarget, string, uint32, uint32) (*mirror.Origin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.origin == nil {
		return nil, mirror.ErrGone
	}
	return f.origin, nil
}
func (f *fakeMirror) Sent(_ store.SyncTarget, s mirror.Sending) {
	f.mu.Lock()
	f.sendings = append(f.sendings, s)
	f.mu.Unlock()
}

func (f *fakeMirror) Reconcile() { f.mu.Lock(); f.reconciled++; f.mu.Unlock() }
func (f *fakeMirror) Refresh(id string) {
	f.mu.Lock()
	f.refreshed = append(f.refreshed, id)
	f.mu.Unlock()
}
func (f *fakeMirror) List(_ context.Context, _ store.SyncTarget, _ string, _, before uint32, _ int, q string) (*mirror.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listed = append(f.listed, before)
	if q != "" {
		f.searched = append(f.searched, q)
	}
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
	h := store.Header{UID: uid, Subject: "Message A", From: store.Address{Name: "Alice", Email: "alice@example.com"}}
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
func (f *fakeMirror) Kick(id string) {
	f.mu.Lock()
	f.kicked = append(f.kicked, id)
	f.mu.Unlock()
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

// A config says how much of its INBOX is unread, for the switcher to mark the ones with mail.
func TestAConfigSaysWhatIsUnreadInItsInbox(t *testing.T) {
	_, _, c, _, _ := withMail(t, 3)
	got := c.json(c.do("GET", "/api/email-configs", ""))["email_configs"].([]any)[0].(map[string]any)
	if got["inbox_unseen"] != float64(3) {
		t.Errorf("inbox_unseen = %v", got["inbox_unseen"])
	}
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

// A search is the mail server's, in INBOX too: nothing is kept to search here.
func TestASearchIsAskedOfTheServer(t *testing.T) {
	s, _, c, cfg, inbox := withMail(t, 3)
	fake := &fakeMirror{listing: &mirror.Listing{UIDValidity: 7, Headers: []store.Header{{UID: 2, Subject: "Message B"}}}}
	s.mirror = fake
	base := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages"

	found := c.json(c.do("GET", base+"?q=+from%3Aalice+lunch+", ""))
	if got := subjectsOf(found); got != "Message B" || len(fake.searched) != 1 || fake.searched[0] != "from:alice lunch" {
		t.Errorf("found %q, the server asked %q", got, fake.searched)
	}
	if resp := c.do("GET", base+"?q="+strings.Repeat("a", queryMax+1), ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a query too long = %s", resp.Status)
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

// Changes close together are one event, sent once the gap after the last is over: each has the
// browser read its lists again.
func TestTheEventStreamSpacesChangesOut(t *testing.T) {
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
	events := make(chan time.Time)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if sc.Text() == "event: changed" {
				events <- time.Now()
			}
		}
		close(events)
	}()

	me := c.json(c.do("GET", "/api/auth/me", ""))["id"].(string)
	st.Notify(me)
	first := <-events
	for range 3 {
		time.Sleep(10 * time.Millisecond)
		st.Notify(me)
	}
	var after []time.Time
	deadline := time.After(eventGap + 500*time.Millisecond)
	for collecting := true; collecting; {
		select {
		case at := <-events:
			after = append(after, at)
		case <-deadline:
			collecting = false
		}
	}
	if len(after) != 1 {
		t.Fatalf("%d events after the first, want 1", len(after))
	}
	if gap := after[0].Sub(first); gap < eventGap-50*time.Millisecond {
		t.Errorf("the second came %v after the first", gap)
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
