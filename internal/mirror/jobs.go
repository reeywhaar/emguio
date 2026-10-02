package mirror

import (
	"context"
	"errors"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/store"
)

// JobTimeout bounds one try of one job.
const JobTimeout = 2 * time.Minute

// jobBackoff is how long a job waits before its next try, after a try that did not reach the
// server. A server that answered and refused is not asked again.
var jobBackoff = []time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// Kick says an email config has a job waiting, starting its runner if it has none.
//
// One runner per config, doing its jobs one at a time in the order asked, on the reading
// session: a job is done because somebody asked, whether or not the page that asked is still
// open. See docs/reading.md.
func (m *Mirror) Kick(configID string) {
	m.mu.Lock()
	wake := m.runners[configID]
	if wake == nil {
		wake = make(chan struct{}, 1)
		m.runners[configID] = wake
		m.running.Add(1)
		go func() {
			defer m.running.Done()
			m.runJobs(configID, wake)
		}()
	}
	m.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (m *Mirror) runJobs(configID string, wake chan struct{}) {
	ctx := m.life
	// Whose jobs this run has done, and whether one took a message out of its mailbox. Told
	// once the run stops: each telling has every page watching read its lists again, and a
	// selection of a hundred messages would otherwise be a hundred reads while it is worked on.
	// A page sees the jobs leave meanwhile by asking for them.
	var (
		done  string
		moved bool
	)
	for {
		j, err := m.store.NextJob(ctx, configID)
		if err != nil && ctx.Err() == nil {
			m.log.Error("mirror could not read the next job", "email_config", configID, "err", err)
		}
		var later <-chan time.Time
		switch {
		case err != nil:
			later = time.After(30 * time.Second)
		case j == nil:
		case time.Until(j.NextAt) > 0:
			later = time.After(time.Until(j.NextAt))
		default:
			m.do(ctx, j)
			done = j.UserID
			moved = moved || j.Kind == store.JobMove || j.Kind == store.JobDelete
			continue
		}
		if done != "" {
			if moved {
				// The window has places to fill, and the counts are the server's to confirm.
				m.Refresh(configID)
			}
			m.store.Notify(done)
			done, moved = "", false
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-later:
		}
	}
}

// do tries one job, and finishes it, fails it, or puts it off.
func (m *Mirror) do(ctx context.Context, j *store.Job) {
	t, err := m.store.JobTarget(ctx, j.EmailConfigID)
	if err != nil {
		m.settle(ctx, j, err)
		return
	}
	try, cancel := context.WithTimeout(ctx, JobTimeout)
	defer cancel()
	err = m.attempt(try, t, j)
	if ctx.Err() != nil {
		return
	}
	m.settle(ctx, j, err)
}

// attempt does a job on the server and records what the server then holds.
func (m *Mirror) attempt(ctx context.Context, t store.SyncTarget, j *store.Job) error {
	mb, err := m.store.MirrorMailbox(ctx, j.MailboxID)
	if err != nil {
		return err
	}
	uidValidity, uid, ok := ids.ParseMessage(j.Message)
	if !ok {
		return store.Invalid("%q is not a message id.", j.Message)
	}
	switch j.Kind {
	case store.JobSeen, store.JobFlagged:
		flag := imap.Flag(Seen)
		if j.Kind == store.JobFlagged {
			flag = Flagged
		}
		flags, moved, err := m.SetFlag(ctx, t, mb.Name, uidValidity, uid, flag, j.Value)
		if err != nil {
			return err
		}
		step := 0
		if moved && flag == Seen {
			step = 1
			if j.Value {
				step = -1
			}
		}
		return m.store.SetMessageFlags(ctx, mb.ID, uid, flags, step)
	case store.JobMove, store.JobDelete:
		to := ""
		var flags store.Flags
		if j.Kind == store.JobMove {
			dest, err := m.store.MirrorMailbox(ctx, j.Target)
			if err != nil {
				return err
			}
			to = dest.ID
			if flags, err = m.Move(ctx, t, mb.Name, uidValidity, uid, dest.Name); err != nil {
				return err
			}
		} else if flags, err = m.Delete(ctx, t, mb.Name, uidValidity, uid); err != nil {
			return err
		}
		return m.store.MessageMoved(ctx, mb.ID, to, uid, flags.Seen)
	}
	return store.Invalid("%q is not something a job does.", j.Kind)
}

// settle ends a try: done, failed with a sentence for its user, or put off until the server
// can be reached.
func (m *Mirror) settle(ctx context.Context, j *store.Job, err error) {
	var (
		f       *connect.Failure
		outcome error
	)
	switch {
	case err == nil, errors.Is(err, ErrGone) && j.Kind == store.JobDelete:
		outcome = m.store.FinishJob(ctx, j.ID)
	case errors.Is(err, ErrGone):
		outcome = m.store.FailJob(ctx, j.ID, "This message is no longer on the server.")
	case errors.Is(err, ErrUnsupported):
		outcome = m.store.FailJob(ctx, j.ID, unsupported(j.Kind))
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalid):
		outcome = m.store.FailJob(ctx, j.ID, err.Error())
	case errors.As(err, &f) && f.Class == "refused", j.Attempts >= len(jobBackoff):
		sentence, _ := explain(err)
		outcome = m.store.FailJob(ctx, j.ID, sentence)
	default:
		m.log.Warn("mirror will try a job again", "email_config", j.EmailConfigID, "job", j.ID, "attempt", j.Attempts+1)
		outcome = m.store.RetryJob(ctx, j.ID, time.Now().Add(jobBackoff[j.Attempts]))
	}
	if outcome != nil && ctx.Err() == nil {
		m.log.Error("mirror could not record a job", "job", j.ID, "err", outcome)
	}
}

func unsupported(kind string) string {
	if kind == store.JobDelete {
		return "This mail server cannot delete one message for good without touching others: it has no UIDPLUS."
	}
	return "This mail server cannot move one message without touching others: it has neither MOVE nor UIDPLUS."
}
