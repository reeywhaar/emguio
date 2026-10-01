package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"emguio/internal/store"
)

// fakeMirror records what it was asked.
type fakeMirror struct {
	mu         sync.Mutex
	reconciled int
	refreshed  []string
	// raw and err are what a fetch gets, and fetched how many there were.
	raw     []byte
	err     error
	fetched int
}

func (f *fakeMirror) Reconcile() { f.mu.Lock(); f.reconciled++; f.mu.Unlock() }
func (f *fakeMirror) Raw(context.Context, store.SyncTarget, string, uint32, uint32) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched++
	return f.raw, f.err
}
func (f *fakeMirror) Refresh(id string) {
	f.mu.Lock()
	f.refreshed = append(f.refreshed, id)
	f.mu.Unlock()
}

// withMail is a signed-in user with one email config whose INBOX holds n messages, as the
// mirror would have left it.
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

func TestMessagesPageWithACursor(t *testing.T) {
	_, _, c, cfg, inbox := withMail(t, 3)
	base := "/api/email-configs/" + cfg + "/mailboxes/" + inbox + "/messages"
	first := c.json(c.do("GET", base+"?limit=2", ""))
	msgs := first["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["subject"] != "Message C" {
		t.Fatalf("first page = %v", first)
	}
	cursor, _ := first["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("no cursor with more to come")
	}
	second := c.json(c.do("GET", base+"?limit=2&cursor="+cursor, ""))
	if msgs := second["messages"].([]any); len(msgs) != 1 || msgs[0].(map[string]any)["subject"] != "Message A" {
		t.Errorf("second page = %v", second)
	}
	if _, more := second["next_cursor"]; more {
		t.Error("the last page offers another")
	}
	if resp := c.do("GET", base+"?limit=9999", ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a limit past the maximum = %s", resp.Status)
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
