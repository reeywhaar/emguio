package mirror

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
	"emguio/internal/ids"
	"emguio/internal/store"
)

// elsewhere is another account of the world's user, on a server of its own, with its folders
// read and the server to change it on.
type elsewhere struct {
	target store.SyncTarget
	server *imapclient.Client
}

func (w *world) another(folders ...string) *elsewhere {
	w.t.Helper()
	port := connecttest.IMAP(w.t, w.cert, connect.Implicit, "ann", "pw")
	cfg, err := w.store.CreateEmailConfig(context.Background(), w.target.UserID, store.EmailConfigInput{
		Email: "ann@example.org",
		Incoming: store.Login{
			Server: store.Server{
				Protocol: store.ProtocolIMAP, Host: connecttest.Host, Port: port,
				TLS: store.TLSImplicit, Username: "ann",
			},
			Password: "pw",
		},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	e := &elsewhere{
		target: store.SyncTarget{ID: cfg.ID, UserID: w.target.UserID, UpdatedAt: cfg.UpdatedAt},
		server: connecttest.Admin(w.t, w.cert, port, "ann", "pw"),
	}
	for _, name := range folders {
		if err := e.server.Create(name, nil).Wait(); err != nil {
			w.t.Fatal(err)
		}
	}
	if err := w.mirror.Once(context.Background(), e.target); err != nil {
		w.t.Fatal(err)
	}
	return e
}

func (e *elsewhere) mailbox(w *world, name string) *store.Mailbox {
	w.t.Helper()
	boxes, err := w.store.Mailboxes(context.Background(), w.target.UserID, e.target.ID)
	if err != nil {
		w.t.Fatal(err)
	}
	for _, mb := range boxes {
		if mb.Name == name {
			return mb
		}
	}
	w.t.Fatalf("no %s elsewhere", name)
	return nil
}

// holds is what a server holds in mailbox: each message's subject, flags and arrival.
func holds(t *testing.T, c *imapclient.Client, mailbox string) []*imapclient.FetchMessageBuffer {
	t.Helper()
	data, err := c.Select(mailbox, nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if data.NumMessages == 0 {
		return nil
	}
	msgs, err := c.Fetch(imap.SeqSetNum(1, data.NumMessages), &imap.FetchOptions{
		Envelope: true, Flags: true, InternalDate: true,
	}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

// transfer queues INBOX's messages uids for to, as one request.
func (w *world) transfer(to *store.Mailbox, uids ...uint32) {
	w.t.Helper()
	inbox := w.mailbox("INBOX")
	var jobs []store.Job
	for _, uid := range uids {
		jobs = append(jobs, store.Job{
			UserID: w.target.UserID, EmailConfigID: w.target.ID, MailboxID: inbox.ID,
			Message: ids.Message(inbox.UIDValidity, uid), Kind: store.JobTransfer, Target: to.ID,
		})
	}
	if _, err := w.store.AddJobs(context.Background(), jobs); err != nil {
		w.t.Fatal(err)
	}
	w.mirror.Kick(w.target.ID)
}

// finished is the user's one transfer, once it is no longer moving.
func (w *world) finished() *store.Transfer {
	w.t.Helper()
	var last *store.Transfer
	eventually(w.t, func() bool {
		ts, err := w.store.Transfers(context.Background(), w.target.UserID)
		if err != nil || len(ts) != 1 {
			return false
		}
		last = ts[0]
		return last.State() != store.TransferMoving
	})
	return last
}

// A message moved to another account is all there, with its flags and the day it arrived, and
// gone from here, on the server and from what is kept.
func TestAMessageMovesToAnotherAccount(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 5, "alice@example.com", "Lunch", imap.FlagSeen, imap.FlagFlagged)
	w.deliver("INBOX", 6, "bob@example.com", "Stays")
	w.sync()
	there := w.another("Archive")

	w.transfer(there.mailbox(w, "Archive"), 1)
	tr := w.finished()
	if tr.State() != store.TransferDone || tr.Moved != 1 || tr.Total != 1 {
		t.Fatalf("transfer = %+v", tr)
	}

	got := holds(t, there.server, "Archive")
	if len(got) != 1 || got[0].Envelope.Subject != "Lunch" {
		t.Fatalf("Archive elsewhere holds %+v", got)
	}
	flags := map[imap.Flag]bool{}
	for _, f := range got[0].Flags {
		flags[f] = true
	}
	if !flags[imap.FlagSeen] || !flags[imap.FlagFlagged] {
		t.Errorf("its flags are %v", got[0].Flags)
	}
	if !got[0].InternalDate.Equal(start.Add(5 * time.Minute)) {
		t.Errorf("it arrived %v there", got[0].InternalDate)
	}
	if left := holds(t, w.server, "INBOX"); len(left) != 1 || left[0].Envelope.Subject != "Stays" {
		t.Errorf("INBOX here holds %+v", left)
	}
	if got := subjects(w.window()); got != "Stays" {
		t.Errorf("INBOX keeps %q", got)
	}
}

// A message there already, by its Message-ID — a try that stopped after copying it — is not
// copied again, only deleted here.
func TestAMessageThereAlreadyIsOnlyDeletedHere(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 5, "alice@example.com", "Lunch")
	w.sync()
	there := w.another("Archive")
	raw := "From: alice@example.com\nTo: misha@example.com\nSubject: Lunch\nMessage-ID: <5.INBOX@example.com>\n\nHello.\n"
	connecttest.Append(t, there.server, "Archive", raw, start)

	w.transfer(there.mailbox(w, "Archive"), 1)
	if tr := w.finished(); tr.State() != store.TransferDone || tr.Moved != 1 {
		t.Fatalf("transfer = %+v", tr)
	}
	if got := holds(t, there.server, "Archive"); len(got) != 1 {
		t.Errorf("Archive elsewhere holds %d", len(got))
	}
	if left := holds(t, w.server, "INBOX"); len(left) != 0 {
		t.Errorf("INBOX here holds %d", len(left))
	}
}

// A server that will not take the first message will not take the rest: the transfer stops, and
// every message stays where it was, its job ended with the reason.
func TestARefusalStopsATransfer(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 5, "alice@example.com", "One")
	w.deliver("INBOX", 6, "alice@example.com", "Two")
	w.sync()
	there := w.another("Archive")
	archive := there.mailbox(w, "Archive")
	if err := there.server.Delete("Archive").Wait(); err != nil {
		t.Fatal(err)
	}

	w.transfer(archive, 1, 2)
	tr := w.finished()
	if tr.State() != store.TransferStopped || tr.Moved != 0 || !strings.Contains(tr.Error, "no longer on the mail server") {
		t.Fatalf("transfer = %+v", tr)
	}
	if left := holds(t, w.server, "INBOX"); len(left) != 2 {
		t.Errorf("INBOX here holds %d", len(left))
	}
	for _, j := range w.jobs() {
		if j.Error != tr.Error {
			t.Errorf("job %s ended with %q", j.Message, j.Error)
		}
	}
}
