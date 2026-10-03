package mirror

import (
	"context"
	"slices"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/store"
)

// capThreads is the THREAD algorithm conversations are read with: by References and In-Reply-To,
// then by subject for what has neither.
const capThreads = imap.Cap("THREAD=REFERENCES")

// Threads is how the server groups a mailbox's messages into conversations, by UID.
type Threads struct {
	UIDValidity uint32
	// at is each message's conversation, as an index into of; a message alone is in neither.
	at map[uint32]int
	of [][]uint32
}

// Of is the conversation a message is in, its UIDs ascending; nil for a message on its own.
func (t *Threads) Of(uid uint32) []uint32 {
	i, ok := t.at[uid]
	if !ok {
		return nil
	}
	return t.of[i]
}

// threadsKept is how many mailboxes' conversations are kept at once: each maps every message of
// its mailbox that is in one.
const threadsKept = 8

type keptThreads struct {
	config, mailbox                string
	uidValidity, uidNext, messages uint32
	threads                        *Threads
}

// Threads reads how the server groups a mailbox's messages into conversations, with THREAD. Kept
// for as long as the mailbox holds what it held then, by its UIDVALIDITY, UIDNEXT and count, so a
// list read page by page asks once. ErrUnsupported on a server without it. See docs/reading.md.
func (m *Mirror) Threads(ctx context.Context, t store.SyncTarget, mailbox string) (*Threads, error) {
	var out *Threads
	err := m.use(ctx, t, func(f *fetcher) error {
		if !f.session.client.Caps().Has(capThreads) {
			return ErrUnsupported
		}
		uidValidity, n, err := f.open(mailbox, false, true)
		if err != nil {
			return err
		}
		now := keptThreads{config: t.ID, mailbox: mailbox, uidValidity: uidValidity, uidNext: f.uidNext, messages: n}
		if out = m.keptThreads(now); out != nil {
			return nil
		}
		data, err := f.session.client.UIDThread(&imapclient.ThreadOptions{
			Algorithm:      imap.ThreadReferences,
			SearchCriteria: &imap.SearchCriteria{},
		}).Wait()
		if err != nil {
			return refused(err)
		}
		out = threadsOf(uidValidity, data)
		now.threads = out
		m.keepThreads(&now)
		return nil
	})
	return out, err
}

func threadsOf(uidValidity uint32, data []imapclient.ThreadData) *Threads {
	var walk func(d imapclient.ThreadData, into *[]uint32)
	walk = func(d imapclient.ThreadData, into *[]uint32) {
		*into = append(*into, d.Chain...)
		for _, sub := range d.SubThreads {
			walk(sub, into)
		}
	}
	groups := make([][]uint32, len(data))
	for i, d := range data {
		walk(d, &groups[i])
	}
	return ThreadsOf(uidValidity, groups...)
}

// ThreadsOf is conversations of a mailbox, each its messages' UIDs; one of a single message is
// none.
func ThreadsOf(uidValidity uint32, groups ...[]uint32) *Threads {
	out := &Threads{UIDValidity: uidValidity, at: map[uint32]int{}}
	for _, uids := range groups {
		if len(uids) < 2 {
			continue
		}
		uids = slices.Sorted(slices.Values(uids))
		for _, uid := range uids {
			out.at[uid] = len(out.of)
		}
		out.of = append(out.of, uids)
	}
	return out
}

// keptThreads is a mailbox's conversations as last read, if it still holds what it held then.
func (m *Mirror) keptThreads(now keptThreads) *Threads {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, k := range m.threads {
		if k.config != now.config || k.mailbox != now.mailbox {
			continue
		}
		if k.uidValidity != now.uidValidity || k.uidNext != now.uidNext || k.messages != now.messages {
			m.threads = slices.Delete(m.threads, i, i+1)
			return nil
		}
		m.threads = append(slices.Delete(m.threads, i, i+1), k)
		return k.threads
	}
	return nil
}

func (m *Mirror) keepThreads(k *keptThreads) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.threads) >= threadsKept {
		m.threads = m.threads[1:]
	}
	m.threads = append(m.threads, k)
}

// Headers is what a list shows of some of a mailbox's messages, by UID, newest first.
func (m *Mirror) Headers(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32) ([]store.Header, error) {
	var out []store.Header
	err := m.use(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, false); err != nil {
			return err
		}
		if len(uids) == 0 {
			return nil
		}
		var set imap.UIDSet
		for _, uid := range uids {
			set.AddNum(imap.UID(uid))
		}
		var err error
		out, err = f.session.fetch(set)
		return err
	})
	return out, err
}

// relatedMax bounds how many message ids one search for a conversation names.
const relatedMax = 60

// Related is what of a mailbox belongs to a conversation, told by Message-IDs: messages that
// answer one of answered, and those that are one of answers. It says the mailbox's UIDVALIDITY
// with their headers, newest first.
func (m *Mirror) Related(ctx context.Context, t store.SyncTarget, mailbox string, answered, answers []string) (*Listing, error) {
	var keys []imap.SearchCriteria
	header := func(name, id string) imap.SearchCriteria {
		return imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: name, Value: id}}}
	}
	for _, id := range answered {
		keys = append(keys, header("In-Reply-To", id), header("References", id))
	}
	for _, id := range answers {
		keys = append(keys, header("Message-ID", id))
	}
	if len(keys) > relatedMax {
		keys = keys[len(keys)-relatedMax:]
	}
	var out *Listing
	err := m.use(ctx, t, func(f *fetcher) error {
		uidValidity, _, err := f.open(mailbox, false, true)
		if err != nil {
			return err
		}
		out = &Listing{UIDValidity: uidValidity}
		if len(keys) == 0 {
			return nil
		}
		criteria := anyOf(keys)
		found, err := f.session.client.UIDSearch(&criteria, nil).Wait()
		if err != nil {
			return refused(err)
		}
		if uids := found.AllUIDs(); len(uids) > 0 {
			out.Headers, err = f.session.fetch(imap.UIDSetNum(uids...))
		}
		return err
	})
	return out, err
}

// anyOf is a search for messages any of keys finds, as a balanced tree of ORs: a chain as deep as
// the list is long is more than some servers will parse.
func anyOf(keys []imap.SearchCriteria) imap.SearchCriteria {
	if len(keys) == 1 {
		return keys[0]
	}
	half := len(keys) / 2
	return imap.SearchCriteria{Or: [][2]imap.SearchCriteria{{anyOf(keys[:half]), anyOf(keys[half:])}}}
}
