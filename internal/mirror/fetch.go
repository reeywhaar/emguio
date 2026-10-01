package mirror

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/store"
)

// ErrGone is a message the server no longer has where the store says it is: expunged, moved, or
// in a mailbox whose UIDVALIDITY has changed since the last look.
var ErrGone = errors.New("mirror: message gone")

// FetchIdle is how long a reading session stays open after its last use.
const FetchIdle = 5 * time.Minute

// fetcher is an email config's second session, for reading whole messages when somebody opens
// one. A session of its own, so opening a message never waits behind a pass copying thousands of
// headers; one at a time, because a person reads one message at a time.
type fetcher struct {
	mu      sync.Mutex
	target  store.SyncTarget
	session *session
	cancel  context.CancelFunc
	idle    *time.Timer
}

// Raw is one message as the server holds it, fetched with BODY.PEEK so reading it here leaves it
// unread everywhere else.
func (m *Mirror) Raw(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) ([]byte, error) {
	f := m.fetcherFor(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := f.raw(ctx, m, mailbox, uidValidity, uid)
	if err != nil && !errors.Is(err, ErrGone) {
		// A session that failed is not trusted with the next request.
		f.close()
	}
	return raw, err
}

// fetcherFor is the config's fetcher, a new one when the config changed since the last: the old
// one would go on signing in with what was saved before.
func (m *Mirror) fetcherFor(t store.SyncTarget) *fetcher {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fetchers[t.ID]
	if f != nil && !f.target.UpdatedAt.Equal(t.UpdatedAt) {
		go f.shut()
		f = nil
	}
	if f == nil {
		f = &fetcher{target: t}
		m.fetchers[t.ID] = f
	}
	return f
}

func (f *fetcher) raw(ctx context.Context, m *Mirror, mailbox string, uidValidity, uid uint32) ([]byte, error) {
	if f.session == nil || f.closed() {
		f.close()
		life, cancel := context.WithCancel(context.Background())
		s, err := m.open(life, f.target)
		if err != nil {
			cancel()
			return nil, err
		}
		f.session, f.cancel = s, cancel
	}
	f.rest()

	// The request bounds the fetch: when it ends first, the session is closed under the fetch.
	client := f.session.client
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()

	data, err := client.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait()
	var refusal *imap.Error
	if errors.As(err, &refusal) {
		return nil, ErrGone
	}
	if err != nil {
		return nil, err
	}
	if data.UIDValidity != uidValidity {
		return nil, ErrGone
	}
	whole := &imap.FetchItemBodySection{Peek: true}
	msgs, err := client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{whole},
	}).Collect()
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 || len(msgs[0].BodySection) == 0 {
		return nil, ErrGone
	}
	return msgs[0].BodySection[0].Bytes, nil
}

func (f *fetcher) closed() bool {
	select {
	case <-f.session.client.Closed():
		return true
	default:
		return false
	}
}

// rest restarts the idle clock.
func (f *fetcher) rest() {
	if f.idle != nil {
		f.idle.Stop()
	}
	f.idle = time.AfterFunc(FetchIdle, f.shut)
}

func (f *fetcher) shut() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.close()
}

// close signs out, if signed in. The caller holds mu.
func (f *fetcher) close() {
	if f.idle != nil {
		f.idle.Stop()
		f.idle = nil
	}
	if f.session != nil {
		f.session.client.Close()
		f.cancel()
		f.session = nil
	}
}
