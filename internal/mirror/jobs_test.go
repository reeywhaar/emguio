package mirror

import (
	"context"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"

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
	moved, err := w.mirror.List(context.Background(), w.target, "Archive", 0, 0, 10)
	if err != nil || headers(moved.Headers) != "Archive me" {
		t.Errorf("Archive = %v, %v", moved, err)
	}
	st, _ := w.server.Status("INBOX", &imap.StatusOptions{NumUnseen: true}).Wait()
	if *st.NumUnseen != 0 {
		t.Errorf("unseen on the server = %d", *st.NumUnseen)
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
