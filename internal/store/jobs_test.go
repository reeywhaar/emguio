package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// next is the first of what a config does next, or nil.
func next(t *testing.T, st *Store, configID string) *Job {
	t.Helper()
	jobs, err := st.NextJobs(context.Background(), configID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) == 0 {
		return nil
	}
	return jobs[0]
}

func TestJobsAreDoneInTheOrderAsked(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 2)
	ctx := context.Background()
	first, err := st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-1", Kind: JobSeen, Value: true})
	if err != nil {
		t.Fatal(err)
	}
	st.AddJob(ctx, Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: "1-2", Kind: JobMove, Target: mb.ID})

	if n := next(t, st, cfg.ID); n.ID != first.ID || n.Kind != JobSeen || !n.Value {
		t.Fatalf("next = %+v", n)
	}
	// Put off is still next: nothing after it goes before it.
	st.RetryJob(ctx, first.ID, time.Now().Add(time.Hour))
	if again := next(t, st, cfg.ID); again.ID != first.ID || again.Attempts != 1 {
		t.Errorf("after a retry = %+v", again)
	}
	st.FinishJob(ctx, first.ID)
	if n := next(t, st, cfg.ID); n == nil || n.Message != "1-2" {
		t.Errorf("after the first = %+v", n)
	}
	if configs, _ := st.JobConfigs(ctx); len(configs) != 1 || configs[0] != cfg.ID {
		t.Errorf("configs with jobs = %v", configs)
	}
}

// What goes to the server at once is a run of the same done to one mailbox's messages, as a
// selection acted on asks for; anything else asked in between ends the run, so the order holds.
func TestARunOfTheSameJobIsDoneAtOnce(t *testing.T) {
	st, u, cfg, mb := mailWorld(t, 2)
	ctx := context.Background()
	job := func(message, kind string, value bool) Job {
		return Job{UserID: u.ID, EmailConfigID: cfg.ID, MailboxID: mb.ID, Message: message, Kind: kind, Value: value}
	}
	st.AddJobs(ctx, []Job{
		job("1-1", JobSeen, true), job("1-2", JobSeen, true), job("1-3", JobSeen, true),
		job("1-4", JobSeen, false),
		job("1-5", JobSeen, false),
		job("2-6", JobSeen, false),
	})
	runs := [][]string{}
	for {
		jobs, err := st.NextJobs(ctx, cfg.ID, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) == 0 {
			break
		}
		run := []string{}
		for _, j := range jobs {
			run = append(run, j.Message)
			st.FinishJob(ctx, j.ID)
		}
		runs = append(runs, run)
	}
	// At most two at once here; marking unread is not marking read; another UIDVALIDITY is
	// another mailbox as far as the server is concerned.
	want := "[[1-1 1-2] [1-3] [1-4 1-5] [2-6]]"
	if got := fmt.Sprint(runs); got != want {
		t.Errorf("runs = %s, want %s", got, want)
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
	if n := next(t, st, cfg.ID); n != nil {
		t.Errorf("a failed job is still next: %+v", n)
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
