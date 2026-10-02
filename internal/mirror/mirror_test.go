package mirror

import (
	"bytes"
	"context"
	"errors"
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
	"emguio/internal/message"
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
	return newWorldWith(t, password, connecttest.Modern)
}

func newWorldWith(t *testing.T, password string, caps imap.CapSet) *world {
	t.Helper()
	cert := connecttest.NewCert(t)
	port := connecttest.IMAPWith(t, cert, connect.Implicit, "misha", "hunter2", caps)

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
	m := New(st, conn, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		m.end()
		m.running.Wait()
	})
	return &world{
		t:      t,
		store:  st,
		mirror: m,
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

// window is what is kept of INBOX, newest first.
func (w *world) window() []*store.Message {
	w.t.Helper()
	mb := w.mailbox("INBOX")
	if mb == nil {
		w.t.Fatal("no INBOX")
	}
	msgs, err := w.store.Window(context.Background(), mb)
	if err != nil {
		w.t.Fatal(err)
	}
	return msgs
}

func headers(list []store.Header) string {
	var out []string
	for _, h := range list {
		out = append(out, h.Subject)
	}
	return strings.Join(out, ", ")
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

func TestAFirstPassListsTheMailboxesAndKeepsINBOXsNewest(t *testing.T) {
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
	if sent := w.mailbox("Sent"); sent == nil || sent.SpecialUse != store.UseSent || sent.Messages != 1 {
		t.Errorf("Sent = %+v, want it recognized by name and counted", sent)
	}
	if nested := w.mailbox("Projects/emguio"); nested == nil || strings.Join(nested.Path(), ">") != "Projects>emguio" {
		t.Errorf("nested mailbox = %+v", nested)
	}

	msgs := w.window()
	if got := subjects(msgs); got != "Third, Second, First" {
		t.Fatalf("INBOX keeps %q, want the newest first", got)
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
	if first.ID() != fmt.Sprintf("%d-1", inbox.UIDValidity) {
		t.Errorf("id = %q", first.ID())
	}

	var kept int
	for _, mb := range []string{"Sent", "Projects", "Projects/emguio"} {
		got, _ := w.store.Window(context.Background(), w.mailbox(mb))
		kept += len(got)
	}
	if kept != 0 {
		t.Errorf("%d messages kept outside INBOX", kept)
	}
}

// The window is INBOX's newest and no more: what arrives pushes the oldest out.
func TestOnlyTheNewestAreKept(t *testing.T) {
	w := newWorld(t, "hunter2")
	for i := 1; i <= Window+2; i++ {
		w.deliver("INBOX", i, "alice@example.com", fmt.Sprintf("m%02d", i))
	}
	w.sync()
	msgs := w.window()
	if len(msgs) != Window || msgs[0].Subject != fmt.Sprintf("m%02d", Window+2) || msgs[Window-1].Subject != "m03" {
		t.Fatalf("kept %d, from %q to %q", len(msgs), msgs[0].Subject, msgs[len(msgs)-1].Subject)
	}

	w.deliver("INBOX", Window+3, "alice@example.com", "newest")
	w.sync()
	msgs = w.window()
	if len(msgs) != Window || msgs[0].Subject != "newest" || msgs[Window-1].Subject != "m04" {
		t.Errorf("after one more: kept %d, from %q to %q", len(msgs), msgs[0].Subject, msgs[len(msgs)-1].Subject)
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
	if got := subjects(w.window()); got != "Second, First" {
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
	if got := subjects(w.window()); got != "Keep" {
		t.Errorf("INBOX lists %q", got)
	}
}

func TestAFlagChangedElsewhereIsCopied(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Read me")
	w.sync()

	w.onServer(1, imap.StoreFlagsAdd, imap.FlagSeen)
	w.sync()
	if msgs := w.window(); !msgs[0].Flags.Seen {
		t.Error("a message read in another client is still unread here")
	}
	if inbox := w.mailbox("INBOX"); inbox.Unseen != 0 {
		t.Errorf("INBOX unseen = %d", inbox.Unseen)
	}
}

// A star changes no count STATUS reports, so the window's flags are looked at every pass.
func TestAStarAddedElsewhereIsCopied(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Star me")
	w.sync()

	w.onServer(1, imap.StoreFlagsAdd, imap.FlagFlagged)
	w.sync()
	if msgs := w.window(); !msgs[0].Flags.Flagged {
		t.Error("a star added elsewhere was not copied")
	}
}

// A new UIDVALIDITY means every UID named before now names something else, so a cursor from
// before is refused rather than read as a position in the new mailbox.
func TestAMailboxRecreatedOnTheServerIsGoneToAnOldCursor(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Lists")
	for i := 1; i <= 3; i++ {
		w.deliver("Lists", i, "alice@example.com", fmt.Sprintf("Old %d", i))
	}
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Lists", 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.server.Delete("Lists").Wait(); err != nil {
		t.Fatal(err)
	}
	w.create("Lists")
	w.deliver("Lists", 4, "alice@example.com", "New")
	again, err := w.mirror.List(ctx, w.target, "Lists", 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if again.UIDValidity == first.UIDValidity {
		t.Fatal("the server did not change UIDVALIDITY; the test proves nothing")
	}
	last := first.Headers[len(first.Headers)-1]
	if _, err := w.mirror.List(ctx, w.target, "Lists", first.UIDValidity, last.UID, 2); !errors.Is(err, ErrGone) {
		t.Errorf("an old cursor = %v, want gone", err)
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

// Encoded words and a mailbox named in another script are how mail that is not English arrives. The
// server here decodes headers itself, so this proves the names round-trip and the subject lands;
// reading a header a real server sends as written is tested in connect.
func TestEncodedSubjectsAndMailboxNamesArrive(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Входящие/Отчёты")
	w.deliver("INBOX", 1, "=?UTF-8?B?0J/RgNC40LLQtdGC?= <ivan@example.ru>", "=?UTF-8?B?0J/RgNC40LLQtdGC?=")
	w.sync()

	msg := w.window()[0]
	if msg.Subject != "Привет" || msg.From.Name != "Привет" {
		t.Errorf("subject = %q, from = %q", msg.Subject, msg.From.Name)
	}
	if w.mailbox("Входящие/Отчёты") == nil {
		t.Error("the mailbox did not arrive under its own name")
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

func TestASyncReadsAPreviewFromTheFirstTextPart(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Plain")
	connecttest.Append(t, w.server, "INBOX", `From: bob@example.com
Subject: Rich
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=b

--b
Content-Type: text/html; charset=utf-8
Content-Transfer-Encoding: quoted-printable

<p>Caf=C3=A9 tomorrow?</p>
--b
Content-Type: application/pdf
Content-Disposition: attachment; filename=menu.pdf

%PDF-
--b--
`, start.Add(2*time.Minute))
	w.sync()

	msgs := w.window()
	if msgs[0].Preview != "Café tomorrow?" || !msgs[0].HasAttachments {
		t.Errorf("rich preview = %q, attachments %v", msgs[0].Preview, msgs[0].HasAttachments)
	}
	if msgs[1].Preview != "Hello." {
		t.Errorf("plain preview = %q", msgs[1].Preview)
	}
}

// A notification's HTML can spend its first kilobytes on a head and styles, which give no line;
// a plain part can be empty beside an HTML one that is not. Both are read further.
func TestAPreviewIsLookedForPastWhatGivesNoLine(t *testing.T) {
	w := newWorld(t, "hunter2")
	styles := strings.Repeat(".a{color:red}\n", 400)
	connecttest.Append(t, w.server, "INBOX", "From: monitor@example.com\nSubject: Styled\nMIME-Version: 1.0\n"+
		"Content-Type: text/html; charset=utf-8\n\n<html><head><style>"+styles+"</style></head><body><p>Front RECOVERED</p></body></html>\n",
		start.Add(time.Minute))
	connecttest.Append(t, w.server, "INBOX", `From: monitor@example.com
Subject: Empty plain
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary=b

--b
Content-Type: text/plain; charset=utf-8


--b
Content-Type: text/html; charset=utf-8

<p>Backend FAILED</p>
--b--
`, start.Add(2*time.Minute))
	w.sync()

	msgs := w.window()
	if msgs[0].Preview != "Backend FAILED" {
		t.Errorf("beside an empty plain part = %q", msgs[0].Preview)
	}
	if msgs[1].Preview != "Front RECOVERED" {
		t.Errorf("past the styles = %q", msgs[1].Preview)
	}
}

// Every mailbox but the window is read from the server, a run at a time, by UID: mail that
// arrives between two runs does not shift the second.
func TestAListPagesTheServerNewestFirst(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	for i := 1; i <= 5; i++ {
		w.deliver("Archive", i, "alice@example.com", fmt.Sprintf("a%d", i))
	}
	ctx := context.Background()
	first, err := w.mirror.List(ctx, w.target, "Archive", 0, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := headers(first.Headers); got != "a5, a4" || !first.More || first.Headers[0].Preview != "Hello." {
		t.Fatalf("first run = %q more %v preview %q", got, first.More, first.Headers[0].Preview)
	}

	w.deliver("Archive", 6, "alice@example.com", "a6")
	cursor := first.Headers[1].UID
	second, err := w.mirror.List(ctx, w.target, "Archive", first.UIDValidity, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := headers(second.Headers); got != "a3, a2" || !second.More {
		t.Fatalf("second run = %q more %v", got, second.More)
	}
	last, err := w.mirror.List(ctx, w.target, "Archive", first.UIDValidity, second.Headers[1].UID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := headers(last.Headers); got != "a1" || last.More {
		t.Errorf("last run = %q more %v", got, last.More)
	}

	w.create("Empty")
	if empty, err := w.mirror.List(ctx, w.target, "Empty", 0, 0, 2); err != nil || len(empty.Headers) != 0 || empty.More {
		t.Errorf("an empty mailbox = %+v, %v", empty, err)
	}
	if _, err := w.mirror.List(ctx, w.target, "Nowhere", 0, 0, 2); !errors.Is(err, ErrGone) {
		t.Errorf("a mailbox the server does not have = %v, want gone", err)
	}
}

func TestAMessageIsReadAndStaysUnread(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "Alice <alice@example.com>", "Open me")
	w.sync()
	inbox := w.mailbox("INBOX")

	opened, err := w.mirror.Read(context.Background(), w.target, "INBOX", inbox.UIDValidity, 1)
	if err != nil {
		t.Fatal(err)
	}
	if p := opened.Structure.Plain; p == nil || strings.TrimSpace(string(p.Body)) != "Hello." || len(opened.Structure.Leaves) != 0 {
		t.Errorf("structure = %+v", opened.Structure)
	}
	if opened.Header.Subject != "Open me" || opened.Header.From.Name != "Alice" || opened.Header.Flags.Seen {
		t.Errorf("header = %+v", opened.Header)
	}
	st, _ := w.server.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
	if *st.NumUnseen != 1 {
		t.Error("fetching the message marked it read on the server")
	}

	// The session is kept for the next one.
	if _, err := w.mirror.Read(context.Background(), w.target, "INBOX", inbox.UIDValidity, 1); err != nil {
		t.Errorf("a second fetch: %v", err)
	}
}

func TestAMessageNoLongerOnTheServerIsGone(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Soon gone")
	w.sync()
	inbox := w.mailbox("INBOX")

	w.onServer(1, imap.StoreFlagsAdd, imap.FlagDeleted)
	w.server.Expunge().Close()
	ctx := context.Background()
	if _, err := w.mirror.Read(ctx, w.target, "INBOX", inbox.UIDValidity, 1); !errors.Is(err, ErrGone) {
		t.Errorf("expunged = %v, want gone", err)
	}
	if _, err := w.mirror.Read(ctx, w.target, "INBOX", inbox.UIDValidity+1, 1); !errors.Is(err, ErrGone) {
		t.Errorf("another UIDVALIDITY = %v, want gone", err)
	}
	if _, err := w.mirror.Read(ctx, w.target, "Nowhere", inbox.UIDValidity, 1); !errors.Is(err, ErrGone) {
		t.Errorf("a mailbox the server does not have = %v, want gone", err)
	}
	if _, _, err := w.mirror.Part(ctx, w.target, "INBOX", inbox.UIDValidity, 1, []int{1}); !errors.Is(err, ErrGone) {
		t.Errorf("a part of an expunged message = %v, want gone", err)
	}
}

// Opening a message asks for its text and the server's word on the rest: an attachment is
// fetched when somebody downloads it, however large.
func TestAMessageOpensWithoutItsAttachments(t *testing.T) {
	w := newWorld(t, "hunter2")
	connecttest.Append(t, w.server, "INBOX", `From: bob@example.com
Subject: Report
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary=b

--b
Content-Type: multipart/alternative; boundary=a

--a
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

=D0=9F=D1=80=D0=B8=D0=B2=D0=B5=D1=82
--a
Content-Type: text/html; charset=utf-8

<p>Hi</p>
--a--
--b
Content-Type: application/pdf
Content-Disposition: attachment; filename*=utf-8''%D0%9E%D1%82%D1%87%D1%91%D1%82.pdf
Content-Transfer-Encoding: base64

`+strings.Repeat("JVBERi0xLjQK\n", 1000)+`--b--
`, start)
	w.sync()
	inbox := w.mailbox("INBOX")

	opened, err := w.mirror.Read(context.Background(), w.target, "INBOX", inbox.UIDValidity, 1)
	if err != nil {
		t.Fatal(err)
	}
	st := opened.Structure
	if st.Plain == nil || st.Plain.Charset != "utf-8" || st.Plain.Encoding != "quoted-printable" ||
		strings.TrimSpace(string(st.Plain.Body)) != "=D0=9F=D1=80=D0=B8=D0=B2=D0=B5=D1=82" {
		t.Errorf("plain = %+v", st.Plain)
	}
	if st.HTML == nil || strings.TrimSpace(string(st.HTML.Body)) != "<p>Hi</p>" {
		t.Errorf("html = %+v", st.HTML)
	}
	if len(st.Leaves) != 1 {
		t.Fatalf("leaves = %+v", st.Leaves)
	}
	pdf := st.Leaves[0]
	if pdf.Section != "2" || pdf.Type != "application/pdf" || pdf.Disposition != "attachment" || pdf.Size < 12000 ||
		message.Param(pdf.DispositionParams, "filename") != "Отчёт.pdf" {
		t.Errorf("pdf = %+v", pdf)
	}
}

// An image costs its own bytes, not its message's.
func TestAPartIsFetchedAloneByItsSection(t *testing.T) {
	w := newWorld(t, "hunter2")
	connecttest.Append(t, w.server, "INBOX", `From: bob@example.com
Subject: Logo
MIME-Version: 1.0
Content-Type: multipart/related; boundary=b

--b
Content-Type: text/html; charset=utf-8

<img src="cid:logo">
--b
Content-Type: image/png
Content-ID: <logo>
Content-Transfer-Encoding: base64

iVBORw0KGgo=
--b--
`, start)
	w.sync()
	inbox := w.mailbox("INBOX")

	head, body, err := w.mirror.Part(context.Background(), w.target, "INBOX", inbox.UIDValidity, 1, []int{2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(head), "image/png") || strings.Contains(string(head), "Subject") {
		t.Errorf("head = %q", head)
	}
	if strings.TrimSpace(string(body)) != "iVBORw0KGgo=" {
		t.Errorf("body = %q", body)
	}
}

// Reading in emguio is reading: the server holds the flag, and other clients see it.
func TestMarkingAMessageReadSetsTheServersFlag(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Read me")
	w.sync()
	inbox := w.mailbox("INBOX")
	ctx := context.Background()
	unseen := func() uint32 {
		st, err := w.server.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
		if err != nil {
			t.Fatal(err)
		}
		return *st.NumUnseen
	}

	flags, moved, err := w.mirror.SetFlag(ctx, w.target, "INBOX", inbox.UIDValidity, 1, Seen, true)
	if err != nil || !moved || !flags.Seen {
		t.Fatalf("marking read = %+v, %v, %v", flags, moved, err)
	}
	if n := unseen(); n != 0 {
		t.Errorf("unseen on the server after marking read = %d", n)
	}
	if _, moved, _ := w.mirror.SetFlag(ctx, w.target, "INBOX", inbox.UIDValidity, 1, Seen, true); moved {
		t.Error("marking a read message read again said it moved")
	}

	// A message just opened for reading is marked on the same session.
	if _, err := w.mirror.Read(ctx, w.target, "INBOX", inbox.UIDValidity, 1); err != nil {
		t.Fatal(err)
	}
	if _, moved, err := w.mirror.SetFlag(ctx, w.target, "INBOX", inbox.UIDValidity, 1, Seen, false); err != nil || !moved {
		t.Fatalf("marking unread = %v, %v", moved, err)
	}
	if n := unseen(); n != 1 {
		t.Errorf("unseen on the server after marking unread = %d", n)
	}
	if _, _, err := w.mirror.SetFlag(ctx, w.target, "INBOX", inbox.UIDValidity, 9, Seen, true); !errors.Is(err, ErrGone) {
		t.Errorf("a UID the server does not have = %v, want gone", err)
	}
}

func TestAStarIsSetOnTheServer(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Star me")
	w.sync()
	inbox := w.mailbox("INBOX")
	flags, moved, err := w.mirror.SetFlag(context.Background(), w.target, "INBOX", inbox.UIDValidity, 1, Flagged, true)
	if err != nil || !moved || !flags.Flagged || flags.Seen {
		t.Fatalf("star = %+v, %v, %v", flags, moved, err)
	}
	w.sync()
	if !w.window()[0].Flags.Flagged {
		t.Error("the star is not on the server")
	}
}

func TestAMessageIsMovedAndDeletedOnTheServer(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	w.deliver("INBOX", 1, "alice@example.com", "Keep")
	w.deliver("INBOX", 2, "alice@example.com", "Archive me")
	w.deliver("INBOX", 3, "alice@example.com", "Delete me", imap.FlagSeen)
	w.sync()
	inbox := w.mailbox("INBOX")
	ctx := context.Background()

	flags, err := w.mirror.Move(ctx, w.target, "INBOX", inbox.UIDValidity, 2, "Archive")
	if err != nil || flags.Seen {
		t.Fatalf("move = %+v, %v", flags, err)
	}
	if flags, err := w.mirror.Delete(ctx, w.target, "INBOX", inbox.UIDValidity, 3); err != nil || !flags.Seen {
		t.Fatalf("delete = %+v, %v", flags, err)
	}
	w.sync()
	if got := subjects(w.window()); got != "Keep" {
		t.Errorf("INBOX keeps %q", got)
	}
	archived, err := w.mirror.List(ctx, w.target, "Archive", 0, 0, 10)
	if err != nil || headers(archived.Headers) != "Archive me" {
		t.Errorf("Archive = %v, %v", archived, err)
	}
	if _, err := w.mirror.Move(ctx, w.target, "INBOX", inbox.UIDValidity, 2, "Archive"); !errors.Is(err, ErrGone) {
		t.Errorf("moving it again = %v, want gone", err)
	}
	// A folder the server does not have is its refusal, in a sentence.
	var f *connect.Failure
	if _, err := w.mirror.Move(ctx, w.target, "INBOX", inbox.UIDValidity, 1, "Nowhere"); !errors.As(err, &f) {
		t.Errorf("a folder that is not there = %v", err)
	}
	// And the session goes on.
	if _, err := w.mirror.Read(ctx, w.target, "INBOX", inbox.UIDValidity, 1); err != nil {
		t.Errorf("after a refusal: %v", err)
	}
}

// Without MOVE or UIDPLUS, removing one message means an EXPUNGE that removes whatever else
// another client marked deleted too, so neither is tried.
func TestWithoutMoveOrUIDPlusNothingIsRemoved(t *testing.T) {
	w := newWorldWith(t, "hunter2", connecttest.Bare)
	w.create("Archive")
	w.deliver("INBOX", 1, "alice@example.com", "Stay")
	w.sync()
	inbox := w.mailbox("INBOX")
	ctx := context.Background()
	if _, err := w.mirror.Move(ctx, w.target, "INBOX", inbox.UIDValidity, 1, "Archive"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("move = %v, want unsupported", err)
	}
	if _, err := w.mirror.Delete(ctx, w.target, "INBOX", inbox.UIDValidity, 1); !errors.Is(err, ErrUnsupported) {
		t.Errorf("delete = %v, want unsupported", err)
	}
	w.sync()
	if got := subjects(w.window()); got != "Stay" {
		t.Errorf("INBOX keeps %q", got)
	}
}
