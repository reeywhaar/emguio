package mirror

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"golang.org/x/crypto/bcrypt"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/seal"
	"emguio/internal/store"
)

func TestMain(m *testing.M) {
	store.SetBcryptCost(bcrypt.MinCost)
	os.Exit(m.Run())
}

// world is one IMAP server with one user, an email config pointing at it, and a mirror.
type world struct {
	t      *testing.T
	store  *store.Store
	mirror *Mirror
	target store.SyncTarget
	// server changes what is on the server the way another mail client would.
	server *imapclient.Client
}

func newWorld(t *testing.T, password string) *world {
	t.Helper()
	cert := connecttest.NewCert(t)
	port := connecttest.IMAP(t, cert, connect.Implicit, "misha", "hunter2")

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
	cfg, err := st.CreateEmailConfig(ctx, u.ID, store.EmailConfigInput{
		Email: "misha@example.com",
		Incoming: store.Login{
			Server: store.Server{
				Protocol: store.ProtocolIMAP, Host: connecttest.Host, Port: port,
				TLS: store.TLSImplicit, Username: "misha",
			},
			Password: password,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := connect.New(connecttest.Loopback).WithRoots(cert.Pool)
	return &world{
		t:      t,
		store:  st,
		mirror: New(st, conn, slog.New(slog.DiscardHandler)),
		target: store.SyncTarget{ID: cfg.ID, UserID: u.ID, UpdatedAt: cfg.UpdatedAt},
		server: connecttest.Admin(t, cert, port, "misha", "hunter2"),
	}
}

var start = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

// deliver puts a message on the server, arriving n minutes after start.
func (w *world) deliver(mailbox string, n int, from, subject string, flags ...imap.Flag) {
	w.t.Helper()
	raw := fmt.Sprintf("From: %s\nTo: misha@example.com\nSubject: %s\nDate: %s\nMessage-ID: <%d.%s@example.com>\n\nHello.\n",
		from, subject, start.Add(time.Duration(n)*time.Minute).Format(time.RFC1123Z), n, mailbox)
	connecttest.Append(w.t, w.server, mailbox, raw, start.Add(time.Duration(n)*time.Minute), flags...)
}

func (w *world) create(names ...string) {
	w.t.Helper()
	for _, name := range names {
		if err := w.server.Create(name, nil).Wait(); err != nil {
			w.t.Fatal(err)
		}
	}
}

func (w *world) sync() {
	w.t.Helper()
	if err := w.mirror.Once(context.Background(), w.target); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) mailbox(name string) *store.Mailbox {
	w.t.Helper()
	boxes, err := w.store.Mailboxes(context.Background(), w.target.UserID, w.target.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, mb := range boxes {
		if mb.Name == name {
			return mb
		}
	}
	return nil
}

func (w *world) messages(name string) []*store.Message {
	w.t.Helper()
	mb := w.mailbox(name)
	if mb == nil {
		w.t.Fatalf("no mailbox %q", name)
	}
	page, err := w.store.Messages(context.Background(), w.target.UserID, w.target.ID, mb.ID, "", 100)
	if err != nil {
		w.t.Fatal(err)
	}
	return page.Messages
}

func subjects(msgs []*store.Message) string {
	var out []string
	for _, m := range msgs {
		out = append(out, m.Subject)
	}
	return strings.Join(out, ", ")
}

// onServer changes the server's flags on one INBOX message, as another client reading it would.
func (w *world) onServer(uid uint32, op imap.StoreFlagsOp, flags ...imap.Flag) {
	w.t.Helper()
	if _, err := w.server.Select("INBOX", nil).Wait(); err != nil {
		w.t.Fatal(err)
	}
	if err := w.server.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{Op: op, Flags: flags, Silent: true}, nil).Close(); err != nil {
		w.t.Fatal(err)
	}
}

func TestAFirstPassCopiesEveryMailboxAndItsMessages(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Sent", "Projects", "Projects/emguio")
	w.deliver("INBOX", 1, "Alice <alice@example.com>", "First")
	w.deliver("INBOX", 2, "bob@example.com", "Second", imap.FlagSeen)
	w.deliver("INBOX", 3, "Carol <carol@example.com>", "Third", imap.FlagFlagged)
	w.deliver("Sent", 4, "misha@example.com", "Sent one")
	w.sync()

	inbox := w.mailbox("INBOX")
	if inbox == nil || inbox.SpecialUse != store.UseInbox || inbox.Messages != 3 || inbox.Unseen != 2 {
		t.Fatalf("INBOX = %+v", inbox)
	}
	if sent := w.mailbox("Sent"); sent == nil || sent.SpecialUse != store.UseSent {
		t.Errorf("Sent = %+v, want it recognized by name", sent)
	}
	if nested := w.mailbox("Projects/emguio"); nested == nil || strings.Join(nested.Path(), ">") != "Projects>emguio" {
		t.Errorf("nested mailbox = %+v", nested)
	}

	msgs := w.messages("INBOX")
	if got := subjects(msgs); got != "Third, Second, First" {
		t.Fatalf("INBOX lists %q, want the newest first", got)
	}
	third, second, first := msgs[0], msgs[1], msgs[2]
	if first.From != (store.Address{Name: "Alice", Email: "alice@example.com"}) {
		t.Errorf("from = %+v", first.From)
	}
	if first.Flags.Seen || !second.Flags.Seen || !third.Flags.Flagged {
		t.Errorf("flags = %+v, %+v, %+v", first.Flags, second.Flags, third.Flags)
	}
	if first.Date == nil || !first.Date.Equal(start.Add(time.Minute)) {
		t.Errorf("date = %v", first.Date)
	}
	if got := subjects(w.messages("Sent")); got != "Sent one" {
		t.Errorf("Sent lists %q", got)
	}
}

// Reading in emguio must never mark anything read anywhere else.
func TestSyncingMarksNothingSeenOnTheServer(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Unread")
	w.sync()

	st, err := w.server.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if *st.NumUnseen != 1 {
		t.Errorf("unseen on the server after a sync = %d, want 1", *st.NumUnseen)
	}
}

func TestNewMailArrivesOnTheNextPass(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "First")
	w.sync()
	w.deliver("INBOX", 2, "alice@example.com", "Second")
	w.sync()
	if got := subjects(w.messages("INBOX")); got != "Second, First" {
		t.Errorf("INBOX lists %q", got)
	}
}

func TestAMessageExpungedOnTheServerIsDropped(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Keep")
	w.deliver("INBOX", 2, "alice@example.com", "Drop")
	w.sync()

	w.onServer(2, imap.StoreFlagsAdd, imap.FlagDeleted)
	if err := w.server.Expunge().Close(); err != nil {
		t.Fatal(err)
	}
	w.sync()
	if got := subjects(w.messages("INBOX")); got != "Keep" {
		t.Errorf("INBOX lists %q", got)
	}
}

func TestAFlagChangedElsewhereIsCopied(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Read me")
	w.sync()

	w.onServer(1, imap.StoreFlagsAdd, imap.FlagSeen)
	w.sync()
	if msgs := w.messages("INBOX"); !msgs[0].Flags.Seen {
		t.Error("a message read in another client is still unread here")
	}
	if inbox := w.mailbox("INBOX"); inbox.Unseen != 0 {
		t.Errorf("INBOX unseen = %d", inbox.Unseen)
	}
}

// A star changes no count STATUS reports, so only a full look at INBOX's flags can catch it.
func TestAStarAddedElsewhereIsCaughtByAFullPass(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Star me")
	w.sync()

	w.onServer(1, imap.StoreFlagsAdd, imap.FlagFlagged)
	w.sync()
	if msgs := w.messages("INBOX"); !msgs[0].Flags.Flagged {
		t.Error("a star added elsewhere was not copied")
	}
}

// A new UIDVALIDITY means every stored UID now names something else.
func TestAMailboxRecreatedOnTheServerStartsAgain(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Lists")
	w.deliver("Lists", 1, "alice@example.com", "Old")
	w.sync()
	before := w.mailbox("Lists")

	if err := w.server.Delete("Lists").Wait(); err != nil {
		t.Fatal(err)
	}
	w.create("Lists")
	w.deliver("Lists", 2, "alice@example.com", "New")
	w.sync()

	after := w.mailbox("Lists")
	if after.UIDValidity == before.UIDValidity {
		t.Fatal("the server did not change UIDVALIDITY; the test proves nothing")
	}
	if got := subjects(w.messages("Lists")); got != "New" {
		t.Errorf("Lists = %q, want only what is there now", got)
	}
}

func TestAMailboxDeletedOnTheServerIsDroppedWithItsMessages(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Old")
	w.deliver("Old", 1, "alice@example.com", "Gone soon")
	w.sync()

	if err := w.server.Delete("Old").Wait(); err != nil {
		t.Fatal(err)
	}
	w.sync()
	if mb := w.mailbox("Old"); mb != nil {
		t.Errorf("Old is still listed: %+v", mb)
	}
}

// koi8-r and a Cyrillic mailbox name are an ordinary Russian account, not an edge case.
func TestEncodedSubjectsAndMailboxNamesAreDecoded(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Входящие/Отчёты")
	w.deliver("INBOX", 1, "=?koi8-r?B?8NLJ18XU?= <ivan@example.ru>", "=?koi8-r?B?8NLJ18XU?=")
	w.sync()

	msg := w.messages("INBOX")[0]
	if msg.Subject != "Привет" || msg.From.Name != "Привет" {
		t.Errorf("subject = %q, from = %q", msg.Subject, msg.From.Name)
	}
	if w.mailbox("Входящие/Отчёты") == nil {
		t.Error("the Cyrillic mailbox did not arrive under its own name")
	}
}

func TestAWrongPasswordIsTheSyncError(t *testing.T) {
	w := newWorld(t, "wrong")
	if err := w.mirror.Once(context.Background(), w.target); err == nil {
		t.Fatal("synced with a wrong password")
	}
	cfg, err := w.store.EmailConfig(context.Background(), w.target.UserID, w.target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.SyncError, "refused the username or password") {
		t.Errorf("sync error = %q", cfg.SyncError)
	}
}

// A browser is told, so the list fills while it is open.
func TestAPassTellsTheUsersWatchers(t *testing.T) {
	w := newWorld(t, "hunter2")
	changes, stop := w.store.Watch(w.target.UserID)
	defer stop()
	w.deliver("INBOX", 1, "alice@example.com", "Hello")
	w.sync()
	select {
	case <-changes:
	default:
		t.Error("nobody was told")
	}
}

func TestAWorkerStartsForANewConfigAndStopsWhenItIsDeleted(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Hello")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.mirror.Run(ctx)
		close(done)
	}()

	eventually(t, func() bool {
		mb := w.mailbox("INBOX")
		return mb != nil && mb.SyncedAt != nil
	})

	if err := w.store.DeleteEmailConfig(context.Background(), w.target.UserID, w.target.ID); err != nil {
		t.Fatal(err)
	}
	w.mirror.Reconcile()
	eventually(t, func() bool {
		w.mirror.mu.Lock()
		defer w.mirror.mu.Unlock()
		return len(w.mirror.workers) == 0
	})

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

func eventually(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("never happened")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
