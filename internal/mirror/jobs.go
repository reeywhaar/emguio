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
		batch, err := m.store.NextJobs(ctx, configID, batchMax)
		if err != nil && ctx.Err() == nil {
			m.log.Error("mirror could not read the next job", "email_config", configID, "err", err)
		}
		var later <-chan time.Time
		switch {
		case err != nil:
			later = time.After(30 * time.Second)
		case len(batch) == 0:
		case time.Until(batch[0].NextAt) > 0:
			later = time.After(time.Until(batch[0].NextAt))
		default:
			m.do(ctx, batch)
			done = batch[0].UserID
			moved = moved || batch[0].Kind == store.JobMove || batch[0].Kind == store.JobDelete
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

// batchMax bounds the jobs done in one command to the mail server: a page's selection.
const batchMax = 500

// do tries a batch of jobs as one, and finishes, fails or puts off each by what became of its
// message.
func (m *Mirror) do(ctx context.Context, batch []*store.Job) {
	t, err := m.store.JobTarget(ctx, batch[0].EmailConfigID)
	outcome := make([]error, len(batch))
	if err != nil {
		for i := range outcome {
			outcome[i] = err
		}
	} else {
		try, cancel := context.WithTimeout(ctx, JobTimeout)
		outcome = m.attempt(try, t, batch)
		cancel()
		if ctx.Err() != nil {
			return
		}
	}
	for i, j := range batch {
		m.settle(ctx, j, outcome[i])
	}
}

// attempt does a batch of jobs on the server — the same done to messages of one mailbox, see
// NextJobs — in one command, and records what the server then holds. It says how each job went,
// nil for done: a refusal or a lost connection is every job's, a message gone is its own.
func (m *Mirror) attempt(ctx context.Context, t store.SyncTarget, batch []*store.Job) []error {
	out := make([]error, len(batch))
	every := func(err error) []error {
		for i := range out {
			if out[i] == nil {
				out[i] = err
			}
		}
		return out
	}
	head := batch[0]
	mb, err := m.store.MirrorMailbox(ctx, head.MailboxID)
	if err != nil {
		return every(err)
	}
	var (
		uidValidity uint32
		uids        = make([]uint32, len(batch))
		asked       []uint32
	)
	for i, j := range batch {
		v, uid, ok := ids.ParseMessage(j.Message)
		if !ok {
			out[i] = store.Invalid("%q is not a message id.", j.Message)
			continue
		}
		uidValidity, uids[i] = v, uid
		asked = append(asked, uid)
	}
	if len(asked) == 0 {
		return out
	}

	var (
		had  Had
		flag imap.Flag
		to   string
	)
	switch head.Kind {
	case store.JobSeen, store.JobFlagged:
		flag = Seen
		if head.Kind == store.JobFlagged {
			flag = Flagged
		}
		had, err = m.SetFlag(ctx, t, mb.Name, uidValidity, asked, flag, head.Value)
	case store.JobMove:
		var dest *store.Mailbox
		if dest, err = m.store.MirrorMailbox(ctx, head.Target); err == nil {
			to = dest.ID
			had, err = m.Move(ctx, t, mb.Name, uidValidity, asked, dest.Name)
		}
	case store.JobDelete:
		had, err = m.Delete(ctx, t, mb.Name, uidValidity, asked)
	default:
		err = store.Invalid("%q is not something a job does.", head.Kind)
	}
	if err != nil {
		return every(err)
	}

	// What the server now holds, message by message; one named twice is recorded once.
	record := func(uid uint32) error {
		before, ok := had[uid]
		switch {
		case !ok:
			return ErrGone
		case flag == "":
			return m.store.MessageMoved(ctx, mb.ID, to, uid, before.Seen)
		}
		now, step := before, 0
		if flag == Seen {
			now.Seen = head.Value
			if before.Seen != head.Value {
				step = 1
				if head.Value {
					step = -1
				}
			}
		} else {
			now.Flagged = head.Value
		}
		return m.store.SetMessageFlags(ctx, mb.ID, uid, now, step)
	}
	recorded := map[uint32]error{}
	for i := range batch {
		if out[i] != nil {
			continue
		}
		e, ok := recorded[uids[i]]
		if !ok {
			e = record(uids[i])
			recorded[uids[i]] = e
		}
		out[i] = e
	}
	return out
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
