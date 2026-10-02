package mirror

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/connect/connecttest"
	"emguio/internal/ids"
	"emguio/internal/store"
)

// job queues one job on INBOX's message uid.
func (w *world) job(kind string, uid uint32, value bool, target string) {
	w.t.Helper()
	inbox := w.mailbox("INBOX")
	if _, err := w.store.AddJob(context.Background(), store.Job{
		UserID: w.target.UserID, EmailConfigID: w.target.ID, MailboxID: inbox.ID,
		Message: ids.Message(inbox.UIDValidity, uid), Kind: kind, Value: value, Target: target,
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) jobs() []*store.Job {
	w.t.Helper()
	jobs, err := w.store.Jobs(context.Background(), w.target.UserID)
	if err != nil {
		w.t.Fatal(err)
	}
	return jobs
}

// A job is done on the server because somebody asked, with no request waiting on it, and the
// jobs of one config are done in the order they were asked.
func TestJobsAreDoneOnTheServerInTheOrderAsked(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	w.deliver("INBOX", 1, "alice@example.com", "Read me")
	w.deliver("INBOX", 2, "alice@example.com", "Archive me")
	w.sync()
	archive := w.mailbox("Archive")

	w.job(store.JobSeen, 1, true, "")
	w.job(store.JobFlagged, 1, true, "")
	w.job(store.JobMove, 2, false, archive.ID)
	w.mirror.Kick(w.target.ID)
	eventually(t, func() bool { return len(w.jobs()) == 0 })

	kept := w.window()
	if got := subjects(kept); got != "Read me" || !kept[0].Flags.Seen || !kept[0].Flags.Flagged {
		t.Errorf("INBOX keeps %q %+v", got, kept)
	}
	moved, err := w.mirror.List(context.Background(), w.target, "Archive", 0, 0, 10, "")
	if err != nil || headers(moved.Headers) != "Archive me" {
		t.Errorf("Archive = %v, %v", moved, err)
	}
	st, _ := w.server.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
	if *st.NumUnseen != 0 {
		t.Errorf("unseen on the server = %d", *st.NumUnseen)
	}
}

// A selection acted on is one command to the mail server rather than one a message, and each
// job is settled by what became of its own message: one another client took away fails alone.
func TestARunOfTheSameJobIsOneCommand(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.create("Archive")
	w.deliver("INBOX", 1, "alice@example.com", "One")
	w.deliver("INBOX", 2, "alice@example.com", "Two", imap.FlagSeen)
	w.deliver("INBOX", 3, "alice@example.com", "Three")
	w.deliver("INBOX", 4, "alice@example.com", "Four")
	w.sync()
	archive := w.mailbox("Archive")
	if _, err := w.server.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := w.server.Store(imap.UIDSetNum(4), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.server.Expunge().Close(); err != nil {
		t.Fatal(err)
	}

	before := w.said.commands("UID MOVE")
	for uid := uint32(1); uid <= 4; uid++ {
		w.job(store.JobMove, uid, false, archive.ID)
	}
	w.mirror.Kick(w.target.ID)
	eventually(t, func() bool { return len(w.jobs()) == 1 })

	if n := w.said.commands("UID MOVE") - before; n != 1 {
		t.Errorf("%d moves sent, want 1", n)
	}
	left := w.jobs()[0]
	if !strings.HasSuffix(left.Message, "-4") || left.Error != "This message is no longer on the server." {
		t.Errorf("left = %+v", left)
	}
	moved, err := w.mirror.List(context.Background(), w.target, "Archive", 0, 0, 10, "")
	if err != nil || headers(moved.Headers) != "Three, Two, One" {
		t.Errorf("Archive = %v, %v", moved, err)
	}
	// Counted by what each had on the server: two of the three unread.
	if got := w.mailbox("Archive"); got.Messages != 3 || got.Unseen != 2 {
		t.Errorf("Archive counts %d, %d unread", got.Messages, got.Unseen)
	}
}

// A run of jobs is told to its user once it is over: every page watching reads its lists again
// on each telling, and a selection acted on would otherwise be a read for each message in it.
func TestARunOfJobsIsToldOnceItIsOver(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "One")
	w.deliver("INBOX", 2, "alice@example.com", "Two")
	w.sync()
	changes, stop := w.store.Watch(w.target.UserID)
	defer stop()

	w.job(store.JobSeen, 1, true, "")
	w.job(store.JobFlagged, 1, true, "")
	w.job(store.JobSeen, 2, true, "")
	w.mirror.Kick(w.target.ID)
	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("the run was never told")
	}
	if left := len(w.jobs()); left != 0 {
		t.Errorf("told with %d jobs still waiting", left)
	}
}

// The server answered no: the job ends with its sentence for its user, and the queue goes on.
func TestARefusedJobFailsAndTheNextStillRuns(t *testing.T) {
	w := newWorld(t, "hunter2")
	w.deliver("INBOX", 1, "alice@example.com", "Stay")
	w.sync()
	// A folder emguio knows of that the server no longer has.
	w.store.PutMailboxes(context.Background(), w.target.ID, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Gone", Selectable: true},
	})
	gone := w.mailbox("Gone")

	w.job(store.JobMove, 1, false, gone.ID)
	w.job(store.JobSeen, 1, true, "")
	w.mirror.Kick(w.target.ID)
	eventually(t, func() bool {
		jobs := w.jobs()
		return len(jobs) == 1 && jobs[0].Error != ""
	})
	if got := w.jobs()[0]; got.Kind != store.JobMove || !strings.HasPrefix(got.Error, "The server refused") {
		t.Errorf("failed job = %+v", got)
	}
	if !w.window()[0].Flags.Seen {
		t.Error("the job after the refused one did not run")
	}
}

// A server that cannot be reached is not a refusal: the job waits and is tried again.
func TestAJobThatCannotReachTheServerIsTriedAgain(t *testing.T) {
	w := newWorld(t, "wrong")
	ctx := context.Background()
	w.store.PutMailboxes(ctx, w.target.ID, []store.Listed{{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true}})
	w.store.SetMailboxStatus(ctx, w.mailbox("INBOX").ID, store.MailboxStatus{UIDValidity: 1})
	w.job(store.JobSeen, 1, true, "")
	w.mirror.Kick(w.target.ID)
	eventually(t, func() bool {
		jobs := w.jobs()
		return len(jobs) == 1 && jobs[0].Attempts == 1
	})
	if j := w.jobs()[0]; j.Error != "" {
		t.Errorf("failed after one try: %+v", j)
	}
}

// A sent message is filed in Sent once, read, and not again where the server filed it itself;
// the message it answers is marked answered. A reply reads its original's Message-ID and
// References to thread under it, and where its sender asks replies to go.
func TestASentMessageIsFiledOnceAndItsOriginalAnswered(t *testing.T) {
	was := fileAfter
	fileAfter = 0
	defer func() { fileAfter = was }()
	w := newWorld(t, "hunter2")
	w.create("Sent")
	connecttest.Append(t, w.server, "INBOX", "From: alice@example.com\nReply-To: lists@example.com\nTo: misha@example.com\nSubject: Question\nMessage-ID: <q@example.com>\nReferences: <root@example.com>\n <before@example.com>\n\nWell?\n", start)
	w.sync()
	inbox, sent := w.mailbox("INBOX"), w.mailbox("Sent")
	ctx := context.Background()

	origin, err := w.mirror.Origin(ctx, w.target, "INBOX", inbox.UIDValidity, 1)
	if err != nil || origin.MessageID != "<q@example.com>" || strings.Join(origin.References, " ") != "<root@example.com> <before@example.com>" {
		t.Fatalf("origin = %+v, %v", origin, err)
	}
	opened, err := w.mirror.Read(ctx, w.target, "INBOX", inbox.UIDValidity, 1)
	if err != nil || len(opened.ReplyTo) != 1 || opened.ReplyTo[0].Email != "lists@example.com" {
		t.Fatalf("reply-to = %+v, %v", opened, err)
	}

	raw := []byte("From: misha@example.com\r\nTo: alice@example.com\r\nSubject: Re: Question\r\nMessage-ID: <reply@example.com>\r\n\r\nYes.\r\n")
	w.mirror.Sent(w.target, Sending{Sent: sent, ID: "<reply@example.com>", Raw: raw,
		Answers: &Answering{Mailbox: inbox, UIDValidity: inbox.UIDValidity, UID: 1}})
	filed := func() []store.Header {
		listing, err := w.mirror.List(ctx, w.target, "Sent", 0, 0, 10, "")
		if err != nil {
			t.Fatal(err)
		}
		return listing.Headers
	}
	eventually(t, func() bool { return len(filed()) == 1 && w.window()[0].Flags.Answered })
	if h := filed()[0]; h.Subject != "Re: Question" || !h.Flags.Seen {
		t.Errorf("filed = %+v", h)
	}
	if err := w.mirror.file(ctx, w.target, "Sent", "<reply@example.com>", raw); err != nil || len(filed()) != 1 {
		t.Errorf("filed again: %v, %d in Sent", err, len(filed()))
	}
}
