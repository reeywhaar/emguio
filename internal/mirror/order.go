package mirror

import (
	"cmp"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// After is where a list goes on from: the last message the page before showed, by its UID and
// the date it arrived, which places it when it has gone since.
type After struct {
	UIDValidity, UID uint32
	Date             time.Time
}

// counts are a mailbox as a session opened it: what an order is good for.
type counts struct {
	uidValidity, uidNext, messages uint32
}

// order is a mailbox's messages by the date each arrived, newest first, and by UID between two
// of the same second: the order every list is in. See docs/reading.md.
type order struct {
	config, mailbox string
	at              counts
	// dated is whether each place has its date: not when the server sorted.
	dated  bool
	places []place
}

type place struct {
	uid  uint32
	date int64
}

// newer is whether a comes before b in a list.
func newer(a, b place) int {
	if c := cmp.Compare(b.date, a.date); c != 0 {
		return c
	}
	return cmp.Compare(b.uid, a.uid)
}

// after is where in o a list goes on from: past the message named, or, that one gone, past where
// its date puts it. False when o has no dates to tell that by.
func (o *order) after(a After) (int, bool) {
	if i := slices.IndexFunc(o.places, func(p place) bool { return p.uid == a.UID }); i >= 0 {
		return i + 1, true
	}
	if !o.dated {
		return 0, false
	}
	i, _ := slices.BinarySearchFunc(o.places, place{uid: a.UID, date: a.Date.Unix()}, newer)
	return i, true
}

// newest is the UIDs of up to n of o's newest.
func (o *order) newest(n int) []uint32 {
	out := make([]uint32, 0, min(n, len(o.places)))
	for _, p := range o.places[:min(n, len(o.places))] {
		out = append(out, p.uid)
	}
	return out
}

// ordersKept is how many mailboxes' orders are kept at once, each a place per message.
const ordersKept = 16

// orderOf is the order of the mailbox s has open, as at says it stands: kept from the last time
// while the mailbox holds what it held then; else from SORT (RFC 5256) where the server has it;
// else from every message's INTERNALDATE, of only what arrived since when an order is kept. With
// dates, never SORT's, which gives none.
func (s *session) orderOf(mailbox string, at counts, dates bool) (*order, error) {
	kept := s.m.keptOrder(s.target.ID, mailbox)
	if kept != nil && kept.at == at && (kept.dated || !dates) {
		return kept, nil
	}
	o := &order{config: s.target.ID, mailbox: mailbox, at: at}
	var err error
	switch {
	case at.messages == 0:
		o.dated = true
	case kept != nil && kept.dated && kept.at.uidValidity == at.uidValidity && at.uidNext != 0:
		o.places, err = s.dated(kept.places, kept.at.uidNext, at)
		o.dated = true
	case !dates && s.client.Caps().Has(imap.CapSort):
		o.places, err = s.sorted(at)
	default:
		o.places, err = s.dated(nil, 1, at)
		o.dated = true
	}
	if err != nil {
		return nil, err
	}
	// Short of the count, something moved between asking and answering: asked again next time.
	if at.uidNext != 0 && len(o.places) == int(at.messages) {
		s.m.keepOrder(o)
	}
	return o, nil
}

// sorted is the server's order, by SORT ARRIVAL: oldest first, which ties by sequence and so by
// UID, turned around.
func (s *session) sorted(at counts) ([]place, error) {
	uids, err := s.client.UIDSort(&imapclient.SortOptions{
		SearchCriteria: &imap.SearchCriteria{},
		SortCriteria:   []imapclient.SortCriterion{{Key: imapclient.SortKeyArrival}},
	}).Wait()
	if err != nil {
		return nil, refused(err)
	}
	out := make([]place, 0, len(uids))
	for _, uid := range slices.Backward(uids) {
		if at.uidNext == 0 || uid < at.uidNext {
			out = append(out, place{uid: uid})
		}
	}
	return out, nil
}

// dated is was, of a mailbox up to UIDNEXT from, brought up to at: the date of each message
// since, and without those gone, which one UID SEARCH tells when the count says some are.
func (s *session) dated(was []place, from uint32, at counts) ([]place, error) {
	out := slices.Clone(was)
	if at.uidNext == 0 || from < at.uidNext {
		var set imap.UIDSet
		if at.uidNext == 0 {
			set.AddRange(imap.UID(from), 0)
		} else {
			set.AddRange(imap.UID(from), imap.UID(at.uidNext-1))
		}
		msgs, err := s.client.Fetch(set, &imap.FetchOptions{UID: true, InternalDate: true}).Collect()
		if err != nil {
			return nil, err
		}
		for _, msg := range msgs {
			// A range past the last UID is answered with the last message.
			if uint32(msg.UID) < from {
				continue
			}
			out = append(out, place{uid: uint32(msg.UID), date: msg.InternalDate.Unix()})
		}
	}
	if len(out) > int(at.messages) {
		found, err := s.client.UIDSearch(&imap.SearchCriteria{}, s.all()).Wait()
		if err != nil {
			return nil, refused(err)
		}
		there := map[uint32]bool{}
		for _, uid := range found.AllUIDs() {
			there[uint32(uid)] = true
		}
		out = slices.DeleteFunc(out, func(p place) bool { return !there[p.uid] })
	}
	slices.SortFunc(out, newer)
	return out, nil
}

// all asks a SEARCH for its UIDs as ranges where the server can say them so.
func (s *session) all() *imap.SearchOptions {
	if caps := s.client.Caps(); caps.Has(imap.CapESearch) || caps.Has(imap.CapIMAP4rev2) {
		return &imap.SearchOptions{ReturnAll: true}
	}
	return nil
}

// keptOrder is a mailbox's order as last read, most recently used last.
func (m *Mirror) keptOrder(config, mailbox string) *order {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, o := range m.orders {
		if o.config == config && o.mailbox == mailbox {
			m.orders = append(slices.Delete(m.orders, i, i+1), o)
			return o
		}
	}
	return nil
}

func (m *Mirror) keepOrder(o *order) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders = slices.DeleteFunc(m.orders, func(k *order) bool { return k.config == o.config && k.mailbox == o.mailbox })
	if len(m.orders) >= ordersKept {
		m.orders = m.orders[1:]
	}
	m.orders = append(m.orders, o)
}

func uidSet(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
	return set
}
