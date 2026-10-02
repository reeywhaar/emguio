package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestJobsAreDoneOneAtATimeInTheOrderAsked(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 2)
	ctx := context.Background()
	first, err := st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobSeen, Value: true})
	if err != nil {
		t.Fatal(err)
	}
	st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-2", Kind: JobMove, Target: mb.ID})

	next, _ := st.NextJob(ctx, cfg.ID)
	if next.ID != first.ID || next.Kind != JobSeen || !next.Value {
		t.Fatalf("next = %+v", next)
	}
	// Put off is still next: nothing after it goes before it.
	st.RetryJob(ctx, first.ID, time.Now().Add(time.Hour))
	if again, _ := st.NextJob(ctx, cfg.ID); again.ID != first.ID || again.Attempts != 1 {
		t.Errorf("after a retry = %+v", again)
	}
	st.FinishJob(ctx, first.ID)
	if next, _ := st.NextJob(ctx, cfg.ID); next == nil || next.Message != "1-2" {
		t.Errorf("after the first = %+v", next)
	}
	if configs, _ := st.JobConfigs(ctx); len(configs) != 1 || configs[0] != cfg.ID {
		t.Errorf("configs with jobs = %v", configs)
	}
}

// A failed job waits to be seen, out of the queue, and only its user can let it go.
func TestAFailedJobWaitsForItsUser(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	j, _ := st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobDelete})

	if err := st.DismissJob(ctx, u.ID, j.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("dismissing a waiting job = %v", err)
	}
	st.FailJob(ctx, j.ID, "The server refused: no.")
	if next, _ := st.NextJob(ctx, cfg.ID); next != nil {
		t.Errorf("a failed job is still next: %+v", next)
	}
	jobs, _ := st.Jobs(ctx, u.ID)
	if len(jobs) != 1 || jobs[0].Error != "The server refused: no." {
		t.Fatalf("jobs = %+v", jobs)
	}

	robin := newUser(t, st, "robin")
	if err := st.DismissJob(ctx, robin.ID, j.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's dismissal = %v", err)
	}
	if err := st.DismissJob(ctx, u.ID, j.ID); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := st.Jobs(ctx, u.ID); len(jobs) != 0 {
		t.Errorf("after dismissal: %+v", jobs)
	}
}

func TestAFailedJobNobodyCameBackForIsSwept(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	now := time.Now()
	st.SetClock(func() time.Time { return now.Add(-48 * time.Hour) })
	old, _ := st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobDelete})
	st.SetClock(func() time.Time { return now })
	st.FailJob(ctx, old.ID, "no")
	waiting, _ := st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobSeen})

	if n, err := st.SweepFailedJobs(ctx); err != nil || n != 1 {
		t.Fatalf("swept %d, %v", n, err)
	}
	if jobs, _ := st.Jobs(ctx, u.ID); len(jobs) != 1 || jobs[0].ID != waiting.ID {
		t.Errorf("left %+v", jobs)
	}
}

// A mailbox the server no longer has takes its jobs with it: there is nothing to do them to.
func TestAMailboxGoneTakesItsJobs(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 1)
	ctx := context.Background()
	st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobSeen})
	st.PutMailboxes(ctx, cfg.ID, []Listed{{Name: "Other", Selectable: true}})
	if jobs, _ := st.Jobs(ctx, u.ID); len(jobs) != 0 {
		t.Errorf("jobs outlived their mailbox: %+v", jobs)
	}
}
