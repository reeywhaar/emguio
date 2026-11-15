package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// transferWorld is mailWorld with another account of the same user and its Archive.
func transferWorld(t *testing.T, n int) (*Store, *User, *EmailConfig, *Mailbox, *Mailbox) {
	t.Helper()
	st, u, cfg, inbox := mailWorld(t, n)
	ctx := context.Background()
	other, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutMailboxes(ctx, other.ID, []Listed{{Name: "Archive", SpecialUse: UseArchive, Selectable: true}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.MirrorMailboxes(ctx, other.ID)
	return st, u, cfg, inbox, boxes[0]
}

func transferJobs(u *User, cfg *EmailConfig, from, to *Mailbox, messages ...string) []Job {
	var jobs []Job
	for _, msg := range messages {
		jobs = append(jobs, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: from.ID, Message: msg, Kind: JobTransfer, Target: to.ID})
	}
	return jobs
}

// The transfer jobs of one request from one mailbox to one other are one transfer, which knows
// how many it has and counts them as they go; each goes alone.
func TestATransferIsTheMessagesAskedTogether(t *testing.T) {
	st, u, cfg, inbox, archive := transferWorld(t, 3)
	ctx := context.Background()
	jobs, err := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-1", "1-2", "1-3"))
	if err != nil {
		t.Fatal(err)
	}
	tr := jobs[0].Transfer
	for _, j := range jobs {
		if j.Transfer != tr || tr == "" {
			t.Fatalf("jobs name transfers %q and %q", tr, j.Transfer)
		}
	}
	if batch, _ := st.NextJobs(ctx, cfg.ID, 100); len(batch) != 1 {
		t.Errorf("a transfer goes %d at a time", len(batch))
	}

	st.FinishJob(ctx, jobs[0].ID)
	st.TransferMoved(ctx, tr)
	got, err := st.Transfer(ctx, u.ID, tr)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || got.Moved != 1 || got.Waiting != 2 || got.State() != TransferMoving ||
		got.TargetConfigID != archive.EmailConfigID || got.TargetMailboxID != archive.ID {
		t.Errorf("transfer = %+v", got)
	}
}

// Cancelled, what has moved stays moved and what waits is left, its jobs ended so a page draws
// those messages where they are. A transfer that finished stays as it ended.
func TestCancellingLeavesWhatWaits(t *testing.T) {
	st, u, cfg, inbox, archive := transferWorld(t, 3)
	ctx := context.Background()
	jobs, _ := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-1", "1-2", "1-3"))
	tr := jobs[0].Transfer
	st.FinishJob(ctx, jobs[0].ID)
	st.TransferMoved(ctx, tr)

	if err := st.DismissTransfer(ctx, u.ID, tr); !errors.Is(err, ErrInvalid) {
		t.Errorf("dismissing one still moving = %v", err)
	}
	got, err := st.CancelTransfer(ctx, u.ID, tr)
	if err != nil || got.State() != TransferCancelled || got.Moved != 1 || got.Waiting != 0 {
		t.Fatalf("cancelled = %+v, %v", got, err)
	}
	left, _ := st.Jobs(ctx, u.ID)
	if len(left) != 2 || left[0].Error != cancelledSentence || left[1].Error != cancelledSentence {
		t.Errorf("jobs left = %+v", left)
	}
	if n := next(t, st, cfg.ID); n != nil {
		t.Errorf("still to do: %+v", n)
	}

	if err := st.DismissTransfer(ctx, u.ID, tr); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.Jobs(ctx, u.ID); len(left) != 0 {
		t.Errorf("dismissed, jobs left = %d", len(left))
	}
	if _, err := st.Transfer(ctx, u.ID, tr); !errors.Is(err, ErrNotFound) {
		t.Errorf("dismissed transfer = %v", err)
	}

	done, _ := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-1"))
	st.FinishJob(ctx, done[0].ID)
	st.TransferMoved(ctx, done[0].Transfer)
	if got, _ := st.CancelTransfer(ctx, u.ID, done[0].Transfer); got.State() != TransferDone {
		t.Errorf("a finished transfer cancelled = %+v", got)
	}
}

// A server's refusal stops a transfer with its sentence, on the transfer and every job left.
func TestAStoppedTransferSaysWhy(t *testing.T) {
	st, u, cfg, inbox, archive := transferWorld(t, 2)
	ctx := context.Background()
	jobs, _ := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-1", "1-2"))
	if err := st.StopTransfer(ctx, jobs[0].Transfer, "Over quota."); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Transfer(ctx, u.ID, jobs[0].Transfer)
	if got.State() != TransferStopped || got.Error != "Over quota." {
		t.Errorf("stopped = %+v", got)
	}
	for _, j := range must(st.Jobs(ctx, u.ID)) {
		if j.Error != "Over quota." {
			t.Errorf("job ended with %q", j.Error)
		}
	}
}

// A finished transfer nobody came back to see goes after a day; one still moving does not.
func TestFinishedTransfersAreSweptAfterADay(t *testing.T) {
	st, u, cfg, inbox, archive := transferWorld(t, 2)
	ctx := context.Background()
	moving, _ := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-1"))
	finished, _ := st.AddJobs(ctx, transferJobs(u, cfg, inbox, archive, "1-2"))
	st.FinishJob(ctx, finished[0].ID)

	st.SetClock(func() time.Time { return time.Now().Add(25 * time.Hour) })
	if n, err := st.SweepTransfers(ctx); err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	left := must(st.Transfers(ctx, u.ID))
	if len(left) != 1 || left[0].ID != moving[0].Transfer {
		t.Errorf("left = %+v", left)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
