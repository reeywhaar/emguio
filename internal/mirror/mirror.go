// Package mirror keeps the store's copy of every email config's mail in step with its server:
// one worker per config, each holding one IMAP session.
//
// It only reads. Mailboxes are opened with EXAMINE and messages fetched without their bodies,
// so nothing it does changes a flag on the server. See docs/reading.md.
package mirror

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

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
	kick     chan struct{}
}

func New(st *store.Store, conn *connect.Connector, log *slog.Logger) *Mirror {
	return &Mirror{
		store:    st,
		conn:     conn,
		log:      log,
		workers:  map[string]*worker{},
		fetchers: map[string]*fetcher{},
		kick:     make(chan struct{}, 1),
	}
}

// Run keeps one worker per email config until ctx ends, and returns once every worker has.
func (m *Mirror) Run(ctx context.Context) {
	tick := time.NewTicker(Reconcile)
	defer tick.Stop()
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

	tick := time.NewTicker(Quick)
	defer tick.Stop()
	full, lastFull, synced := true, time.Time{}, false
	for {
		changed, err := s.pass(ctx, full)
		if err != nil {
			return synced, err
		}
		if full {
			lastFull = time.Now()
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
		select {
		case <-ctx.Done():
			return true, nil
		case <-tick.C:
			full = time.Since(lastFull) >= Full
		case <-w.wake:
			full = true
		}
	}
}

// Once signs in, passes over every mailbox once, and signs out. What a worker does on a timer,
// done now and only once — for tests, and for a pass somebody is waiting on.
func (m *Mirror) Once(ctx context.Context, t store.SyncTarget) error {
	s, err := m.open(ctx, t)
	if err != nil {
		m.failed(ctx, t, err)
		return err
	}
	defer s.client.Close()
	if _, err := s.pass(ctx, true); err != nil {
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
	client, err := m.conn.OpenIMAP(ctx, server)
	if err != nil {
		return nil, err
	}
	return &session{m: m, target: t, host: in.Host, client: client}, nil
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
