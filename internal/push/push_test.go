package push

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"emguio/internal/seal"
	"emguio/internal/store"
	"emguio/internal/webpush"
	"emguio/internal/webpush/webpushtest"
)

func TestMain(m *testing.M) {
	store.SetBcryptCost(bcrypt.MinCost)
	os.Exit(m.Run())
}

// world is one user with one email config, its INBOX, and a phone at a push service that opens
// what it is sent.
type world struct {
	t       *testing.T
	store   *store.Store
	pusher  *Pusher
	service *webpushtest.Service
	user    string
	config  string
	inbox   *store.Mailbox
	uid     uint32
}

func newWorld(t *testing.T) *world {
	t.Helper()
	sealer, _ := seal.New(bytes.Repeat([]byte{7}, seal.KeySize))
	st, err := store.Open(t.TempDir(), sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateEmailConfig(ctx, u.ID, store.EmailConfigInput{
		Name: "Work", Email: "misha@example.com",
		Incoming: store.Login{Server: store.Server{Protocol: store.ProtocolIMAP, Host: "imap.example.com",
			Port: 993, TLS: store.TLSImplicit, Username: "misha"}, Password: "p"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutMailboxes(ctx, c.ID, []store.Listed{{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.MirrorMailboxes(ctx, c.ID)
	if err := st.SetMailboxStatus(ctx, boxes[0].ID, store.MailboxStatus{UIDValidity: 7, UIDNext: 1}); err != nil {
		t.Fatal(err)
	}
	boxes, _ = st.MirrorMailboxes(ctx, c.ID)

	service := webpushtest.NewService(t, webpushtest.NewBrowser(t))
	sub := service.Subscription()
	if _, err := st.RegisterDevice(ctx, u.ID, store.DeviceInput{
		Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth, Label: "Firefox on Mac", ClientID: "c"}); err != nil {
		t.Fatal(err)
	}
	key, pub, err := st.VAPIDKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sender := webpush.NewSender(key, pub, "https://mail.example.com")
	sender.SetClient(service.Client())
	return &world{t: t, store: st, pusher: New(st, sender, slog.New(slog.DiscardHandler)), service: service,
		user: u.ID, config: c.ID, inbox: boxes[0]}
}

// arrive puts messages in INBOX as the mirror does, and keeps them to be told.
func (w *world) arrive(subjects ...string) {
	w.t.Helper()
	ctx := context.Background()
	var headers []store.Header
	for _, s := range subjects {
		w.uid++
		headers = append(headers, store.Header{UID: w.uid, InternalDate: w.store.Now(), Subject: s,
			From: store.Address{Name: "Ann", Email: "ann@example.com"}, MessageID: "<" + s + "@example.com>"})
	}
	if err := w.store.PutMessages(ctx, w.inbox.ID, headers); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.store.Arrived(ctx, w.inbox, headers); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) told() []notification {
	w.t.Helper()
	var out []notification
	for _, p := range w.service.Got() {
		var n notification
		if err := json.Unmarshal(p.Payload, &n); err != nil {
			w.t.Fatalf("payload %q: %v", p.Payload, err)
		}
		out = append(out, n)
	}
	return out
}

func TestAMessageIsToldAsItArrives(t *testing.T) {
	w := newWorld(t)
	w.arrive("Lunch on Thursday?")
	w.pusher.Once(context.Background())

	got := w.told()
	if len(got) != 1 {
		t.Fatalf("told %+v", got)
	}
	want := notification{Title: "Ann", Body: "Lunch on Thursday?", Tag: w.inbox.ID + "/7-1",
		URL: "/c/" + w.config + "/" + w.inbox.ID + "/7-1"}
	if got[0] != want {
		t.Errorf("told %+v, want %+v", got[0], want)
	}
	if topic := w.service.Got()[0].Header.Get("Topic"); topic != w.config {
		t.Errorf("topic %q, want the config's id", topic)
	}
	w.pusher.Once(context.Background())
	if len(w.told()) != 1 {
		t.Error("told again on the next pass")
	}
}

func TestManyAtOnceAreToldTogether(t *testing.T) {
	w := newWorld(t)
	w.arrive("One", "Two", "Three", "Four", "Five", "Six", "Seven")
	w.pusher.Once(context.Background())

	got := w.told()
	if len(got) != 1 {
		t.Fatalf("told %d notifications", len(got))
	}
	if got[0].Title != "7 new messages" || got[0].URL != "/c/"+w.config+"/"+w.inbox.ID || got[0].Tag != w.config {
		t.Errorf("told %+v", got[0])
	}
	lines := strings.Split(got[0].Body, "\n")
	if len(lines) != 6 || lines[0] != "Ann: Seven" || lines[5] != "and 2 more" {
		t.Errorf("body %q", got[0].Body)
	}
}

func TestADigestWaitsItsInterval(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	w.store.SetClock(func() time.Time { return now })
	if err := w.store.SetNotifications(ctx, w.user, w.config, store.NotifyDigest, 30, nil); err != nil {
		t.Fatal(err)
	}
	w.arrive("Lunch")
	if next := w.pusher.Once(ctx); len(w.told()) != 0 || !next.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("told %d at once, next at %s", len(w.told()), next)
	}
	now = now.Add(31 * time.Minute)
	w.arrive("Report")
	w.pusher.Once(ctx)
	if got := w.told(); len(got) != 1 || got[0].Title != "2 new messages" {
		t.Errorf("told %+v", got)
	}
}

func TestAnotherAccountIsNamed(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if _, err := w.store.CreateEmailConfig(ctx, w.user, store.EmailConfigInput{
		Email: "home@example.com",
		Incoming: store.Login{Server: store.Server{Protocol: store.ProtocolIMAP, Host: "imap.example.com",
			Port: 993, TLS: store.TLSImplicit, Username: "home"}, Password: "p"},
	}); err != nil {
		t.Fatal(err)
	}
	w.arrive("Lunch")
	w.pusher.Once(ctx)
	if got := w.told(); len(got) != 1 || got[0].Title != "Ann · Work" {
		t.Errorf("told %+v", got)
	}
}

func TestADeviceWhoseSubscriptionIsOverIsDropped(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.service.Answer(http.StatusGone)
	if n, err := w.pusher.Test(ctx, w.user); err != nil || n != 0 {
		t.Fatalf("delivered %d, %v", n, err)
	}
	if list, _ := w.store.Devices(ctx, w.user); len(list) != 0 {
		t.Errorf("devices %+v", list)
	}
}

func TestABusyPushServiceKeepsTheDevice(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.service.Answer(http.StatusTooManyRequests)
	w.pusher.Test(ctx, w.user)
	list, _ := w.store.Devices(ctx, w.user)
	if len(list) != 1 || list[0].FailureCount != 1 || list[0].LastError == "" {
		t.Errorf("devices %+v", list)
	}
}

func TestATestReachesEveryDevice(t *testing.T) {
	w := newWorld(t)
	n, err := w.pusher.Test(context.Background(), w.user)
	if err != nil || n != 1 {
		t.Fatalf("delivered %d, %v", n, err)
	}
	if got := w.told(); len(got) != 1 || got[0].URL != "/settings" {
		t.Errorf("told %+v", got)
	}
}
