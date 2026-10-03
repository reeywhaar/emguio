// Package mirror keeps what emguio keeps of each email config's mail — its mailboxes and INBOX's
// newest messages — in step with its server, one worker per config holding one IMAP session.
// Everything else is read from the server when somebody asks for it, on a second session.
//
// Only what somebody asks for is ever written: a message read or starred, moved, or deleted.
// Everything else opens mailboxes with EXAMINE and fetches with BODY.PEEK. See docs/reading.md.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/connect"
	"emguio/internal/store"
)

// How often each thing is looked at.
const (
	// Reconcile is how often the set of email configs is re-read, for one added or changed
	// without anybody saying so.
	Reconcile = 30 * time.Second
	// Quick is how often INBOX is looked at.
	Quick = time.Minute
	// Full is how often every mailbox is.
	Full = 5 * time.Minute
)

// backoff is how long a worker waits after a failure, growing with each one in a row.
var backoff = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute}

// Mirror runs the workers.
type Mirror struct {
	store *store.Store
	conn  *connect.Connector
	log   *slog.Logger

	mu       sync.Mutex
	workers  map[string]*worker
	fetchers map[string]*fetcher
	runners  map[string]chan struct{}
	kick     chan struct{}
	// drafts wakes the draft writer, and drafting keeps it to one draft at a time.
	drafts   chan struct{}
	drafting sync.Mutex

	// life bounds the job runners, which outlive any one request: Run's end ends it.
	life    context.Context
	end     context.CancelFunc
	running sync.WaitGroup
}

func New(st *store.Store, conn *connect.Connector, log *slog.Logger) *Mirror {
	life, end := context.WithCancel(context.Background())
	return &Mirror{
		store:    st,
		conn:     conn,
		log:      log,
		workers:  map[string]*worker{},
		fetchers: map[string]*fetcher{},
		runners:  map[string]chan struct{}{},
		kick:     make(chan struct{}, 1),
		drafts:   make(chan struct{}, 1),
		life:     life,
		end:      end,
	}
}

// Run keeps one worker per email config until ctx ends, and returns once every worker, job
// runner and the draft writer has. Jobs left waiting by the last run are taken up again, and
// drafts left due.
func (m *Mirror) Run(ctx context.Context) {
	tick := time.NewTicker(Reconcile)
	defer tick.Stop()
	m.running.Add(1)
	go func() {
		defer m.running.Done()
		m.runDrafts()
	}()
	if configs, err := m.store.JobConfigs(ctx); err != nil {
		m.log.Error("mirror could not list waiting jobs", "err", err)
	} else {
		for _, id := range configs {
			m.Kick(id)
		}
	}
	for {
		m.reconcile(ctx)
		select {
		case <-ctx.Done():
			m.mu.Lock()
			for id, w := range m.workers {
				w.stop()
				delete(m.workers, id)
			}
			fetchers := m.fetchers
			m.fetchers = map[string]*fetcher{}
			m.mu.Unlock()
			m.end()
			m.running.Wait()
			for _, f := range fetchers {
				f.shut()
			}
			return
		case <-tick.C:
		case <-m.kick:
		}
	}
}

// Reconcile asks Run to re-read the email configs now, because one was added, changed or
// removed.
func (m *Mirror) Reconcile() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Refresh asks an email config's worker to look at every mailbox now.
func (m *Mirror) Refresh(id string) {
	m.mu.Lock()
	w := m.workers[id]
	m.mu.Unlock()
	if w == nil {
		m.Reconcile()
		return
	}
	w.poke()
}

// reconcile starts a worker for every config without one, and restarts any whose config changed
// since it started: it would otherwise go on signing in with what was saved before.
func (m *Mirror) reconcile(ctx context.Context) {
	targets, err := m.store.SyncTargets(ctx)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Error("mirror could not list email configs", "err", err)
		}
		return
	}
	want := map[string]store.SyncTarget{}
	for _, t := range targets {
		want[t.ID] = t
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for id, w := range m.workers {
		if t, ok := want[id]; !ok || !t.UpdatedAt.Equal(w.target.UpdatedAt) {
			w.stop()
			delete(m.workers, id)
		}
	}
	for id, t := range want {
		if m.workers[id] == nil {
			m.workers[id] = m.start(ctx, t)
		}
	}
}

type worker struct {
	target store.SyncTarget
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

func (w *worker) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *worker) stop() {
	w.cancel()
	<-w.done
}

func (m *Mirror) start(parent context.Context, t store.SyncTarget) *worker {
	ctx, cancel := context.WithCancel(parent)
	w := &worker{target: t, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	go func() {
		defer close(w.done)
		m.work(ctx, w)
	}()
	return w
}

// work holds a session open for as long as it lasts, and opens another after a pause when it
// does not: a server restarting, a network dropping, a password changed elsewhere.
func (m *Mirror) work(ctx context.Context, w *worker) {
	failures := 0
	for {
		synced, err := m.session(ctx, w)
		if ctx.Err() != nil {
			return
		}
		if synced {
			failures = 0
		}
		m.failed(ctx, w.target, err)
		wait := backoff[min(failures, len(backoff)-1)]
		failures++
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		case <-w.wake:
		}
	}
}

// session signs in and passes over the mailboxes until something fails. It reports whether it
// got through a pass, which is what tells a flaky connection from a wrong password.
func (m *Mirror) session(ctx context.Context, w *worker) (bool, error) {
	s, err := m.open(ctx, w.target)
	if err != nil {
		return false, err
	}
	defer s.client.Close()

	if err := s.notify(); err != nil {
		return false, err
	}
	// With IDLE the server says when INBOX moves, and the timer is only for every other
	// mailbox's counts; without it, INBOX is looked at every minute.
	idles := s.idles()
	every := Quick
	if idles {
		every = Full
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	full, lastFull, synced := true, time.Time{}, false
	var named []string
	for {
		began := time.Now()
		changed, err := s.pass(ctx, full, named)
		if err != nil {
			return synced, err
		}
		if full {
			lastFull = began
		}
		if err := m.store.SetSyncState(ctx, w.target.ID, ""); err != nil {
			return synced, err
		}
		// The first pass of a session always says so, which is what clears a failure shown
		// from before.
		if changed || !synced {
			m.store.Notify(w.target.UserID)
		}
		synced = true
		why, err := s.wait(ctx, tick.C, w.wake)
		named = nil
		switch {
		case why == ended:
			return true, nil
		case err != nil:
			return true, err
		case why == poked:
			full = true
		case why == told:
			var lost bool
			named, full, lost = s.news.take()
			if lost {
				if err := s.notify(); err != nil {
					return true, err
				}
			}
		default:
			// A tick comes a moment before Full has passed since the last full pass began.
			full = time.Since(lastFull) >= Full-every/2
		}
	}
}

// Why a session stopped waiting.
const (
	ticked = iota
	poked
	// arrived is INBOX moving, said by the server under IDLE.
	arrived
	// told is another mailbox moving, said by the server under NOTIFY.
	told
	ended
)

// idles is whether the server can say when a mailbox moves, rather than be asked.
func (s *session) idles() bool {
	caps := s.client.Caps()
	return caps.Has(imap.CapIdle) || caps.Has(imap.CapIMAP4rev2)
}

// wait waits between passes for the timer or a poke, and on a server with IDLE, in INBOX, for
// the server to say INBOX moved: new mail is looked at as it arrives rather than up to a minute
// later. INBOX is examined, never selected, so waiting there changes nothing.
func (s *session) wait(ctx context.Context, tick <-chan time.Time, wake <-chan struct{}) (int, error) {
	var (
		idle  *imapclient.IdleCommand
		moved chan struct{}
	)
	if s.idles() {
		moved = s.moved
		data, err := s.client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return ticked, err
		}
		// What was said before this wait, the pass just made has seen, unless it came after the
		// pass read INBOX: then INBOX is not as the pass found it, and is looked at again.
		select {
		case <-s.moved:
		default:
		}
		if saw := s.saw; saw != nil && (data.NumMessages != saw.Messages || uint32(data.UIDNext) != saw.UIDNext) {
			s.unselect()
			return arrived, nil
		}
		if idle, err = s.client.Idle(); err != nil {
			return ticked, err
		}
	}
	why := ticked
	select {
	case <-ctx.Done():
		return ended, nil
	case <-tick:
	case <-wake:
		why = poked
	case <-moved:
		why = arrived
	case <-s.news.ready:
		why = told
	}
	if idle == nil {
		return why, nil
	}
	if err := idle.Close(); err != nil {
		return why, err
	}
	if err := idle.Wait(); err != nil {
		return why, err
	}
	s.unselect()
	return why, nil
}

// Once signs in, makes one full pass, and signs out. What a worker does on a timer,
// done now and only once — for tests, and for a pass somebody is waiting on.
func (m *Mirror) Once(ctx context.Context, t store.SyncTarget) error {
	s, err := m.open(ctx, t)
	if err != nil {
		m.failed(ctx, t, err)
		return err
	}
	defer s.client.Close()
	if _, err := s.pass(ctx, true, nil); err != nil {
		m.failed(ctx, t, err)
		return err
	}
	s.client.Logout().Wait()
	if err := m.store.SetSyncState(ctx, t.ID, ""); err != nil {
		return err
	}
	m.store.Notify(t.UserID)
	return nil
}

func (m *Mirror) open(ctx context.Context, t store.SyncTarget) (*session, error) {
	logins, err := m.store.TargetLogins(ctx, t)
	if err != nil {
		return nil, err
	}
	in := logins.Incoming
	server := connect.Server{Host: in.Host, Port: in.Port, TLS: in.TLS, Username: in.Username, Password: in.Password}
	heard := newNews()
	moved := make(chan struct{}, 1)
	say := func() {
		select {
		case moved <- struct{}{}:
		default:
		}
	}
	client, err := m.conn.OpenIMAPTelling(ctx, server, &imapclient.UnilateralDataHandler{
		Mailbox: func(*imapclient.UnilateralDataMailbox) { say() },
		Expunge: func(uint32) { say() },
		Fetch: func(msg *imapclient.FetchMessageData) {
			msg.Collect()
			say()
		},
		Status:               func(data *imap.StatusData) { heard.moved(data.Mailbox) },
		List:                 func(*imap.ListData) { heard.listed() },
		NotificationOverflow: heard.overflowed,
	})
	if err != nil {
		return nil, err
	}
	return &session{m: m, target: t, host: in.Host, client: client, moved: moved, news: heard}, nil
}

// failed records why the latest attempt did not work, in a sentence for the settings page.
func (m *Mirror) failed(ctx context.Context, t store.SyncTarget, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	sentence, class := explain(err)
	m.log.Warn("mirror failed", "email_config", t.ID, "class", class)
	if class == "unknown" {
		m.log.Error("mirror failed unclassified", "email_config", t.ID, "err", err)
	}
	if err := m.store.SetSyncState(ctx, t.ID, sentence); err != nil {
		m.log.Error("mirror could not record a failure", "email_config", t.ID, "err", err)
		return
	}
	m.store.Notify(t.UserID)
}

func explain(err error) (sentence, class string) {
	var f *connect.Failure
	var refusal *imap.Error
	switch {
	case errors.As(err, &f):
		return f.Sentence, f.Class
	case errors.As(err, &refusal):
		return fmt.Sprintf("The server refused: %s", connect.Said(refusal.Text)), "refused"
	case errors.Is(err, store.ErrInvalid), errors.Is(err, store.ErrNotFound):
		return err.Error(), "config"
	default:
		return "The connection to the server was lost. It will be tried again.", "unknown"
	}
}
