package mirror

import (
	"errors"
	"maps"
	"slices"
	"sync"

	"github.com/emersion/go-imap/v2"
)

// news is what the server said unasked under NOTIFY, between passes: which mailboxes other than
// the one waited in moved, and whether the list of them did.
type news struct {
	mu     sync.Mutex
	boxes  map[string]bool
	relist bool
	// lost is the server dropping NOTIFY, which is then asked for again.
	lost  bool
	ready chan struct{}
}

func newNews() *news {
	return &news{boxes: map[string]bool{}, ready: make(chan struct{}, 1)}
}

// moved is a mailbox's STATUS, said because something in it changed.
func (n *news) moved(mailbox string) { n.tell(func() { n.boxes[mailbox] = true }) }

// listed is a mailbox made, renamed or deleted.
func (n *news) listed() { n.tell(func() { n.relist = true }) }

// overflowed is the server giving up on saying, and saying so.
func (n *news) overflowed() { n.tell(func() { n.relist, n.lost = true, true }) }

func (n *news) tell(note func()) {
	n.mu.Lock()
	note()
	n.mu.Unlock()
	select {
	case n.ready <- struct{}{}:
	default:
	}
}

// take is what was said since the last take.
func (n *news) take() (boxes []string, relist, lost bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	boxes = slices.Sorted(maps.Keys(n.boxes))
	relist, lost = n.relist, n.lost
	n.boxes, n.relist, n.lost = map[string]bool{}, false, false
	return boxes, relist, lost
}

// notify asks a server with NOTIFY to say whenever any mailbox moves, not only the one waited in:
// every folder's counts as they change, rather than at the next full pass. A change of flags
// elsewhere first, which some servers will not say without CONDSTORE; refused, without it, and
// refused again, the session waits as it would without NOTIFY.
func (s *session) notify() error {
	if !s.client.Caps().Has(imap.CapNotify) {
		return nil
	}
	moves := []imap.NotifyEvent{imap.NotifyEventMessageNew, imap.NotifyEventMessageExpunge}
	selected := imap.NotifyItem{
		MailboxSpec: imap.NotifyMailboxSpecSelected,
		Events:      append(slices.Clone(moves), imap.NotifyEventFlagChange),
	}
	for _, events := range [][]imap.NotifyEvent{
		append(slices.Clone(moves), imap.NotifyEventFlagChange, imap.NotifyEventMailboxName),
		append(slices.Clone(moves), imap.NotifyEventMailboxName),
	} {
		cmd, err := s.client.Notify(&imap.NotifyOptions{Items: []imap.NotifyItem{
			selected,
			{MailboxSpec: imap.NotifyMailboxSpecPersonal, Events: events},
		}})
		if err != nil {
			return err
		}
		err = cmd.Wait()
		var refusal *imap.Error
		if errors.As(err, &refusal) {
			continue
		}
		return err
	}
	s.m.log.Info("mirror was refused NOTIFY", "email_config", s.target.ID)
	return nil
}
