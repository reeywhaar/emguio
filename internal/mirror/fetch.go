package mirror

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/message"
	"emguio/internal/store"
)

// ErrGone is a message or a mailbox the server no longer has as it was named: expunged, moved,
// or in a mailbox whose UIDVALIDITY has changed since.
var ErrGone = errors.New("mirror: gone")

// FetchIdle is how long a reading session stays open after its last use.
const FetchIdle = 5 * time.Minute

// fetcher is an email config's second session, for whatever somebody asks for that is not
// kept: a mailbox's list, a message, one of its parts, and marking it read. A session of its
// own, so none of it waits behind a pass; one request at a time, because one person is asking.
type fetcher struct {
	mu      sync.Mutex
	target  store.SyncTarget
	session *session
	cancel  context.CancelFunc
	idle    *time.Timer

	// The mailbox the session has open, and how: a run of requests about one message — its
	// text, then each of its images — opens it once.
	mailbox     string
	writable    bool
	uidValidity uint32
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

// use runs one request on the config's reading session. The request bounds it: when ctx ends
// first, the session is closed under it. A session that failed is not trusted with the next
// request; one that said a message is gone is fine.
func (m *Mirror) use(ctx context.Context, t store.SyncTarget, do func(f *fetcher) error) error {
	f := m.fetcherFor(t)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ready(m); err != nil {
		return err
	}
	client := f.session.client
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	err := do(f)
	if err != nil && !errors.Is(err, ErrGone) {
		f.close()
	}
	return err
}

// open makes mailbox the open one and says its UIDVALIDITY and how many messages it holds.
//
// EXAMINE unless writable: BODY.PEEK leaves \Seen alone either way, and a read-only mailbox
// cannot have it changed by accident. A mailbox already open is reused unless fresh asks for its
// count as it is now, or a write needs it writable.
func (f *fetcher) open(mailbox string, writable, fresh bool) (uint32, uint32, error) {
	if !fresh && f.mailbox == mailbox && (f.writable || !writable) {
		return f.uidValidity, 0, nil
	}
	f.mailbox = ""
	data, err := f.session.client.Select(mailbox, &imap.SelectOptions{ReadOnly: !writable}).Wait()
	var refusal *imap.Error
	if errors.As(err, &refusal) {
		return 0, 0, ErrGone
	}
	if err != nil {
		return 0, 0, err
	}
	f.mailbox, f.writable, f.uidValidity = mailbox, writable, data.UIDValidity
	return data.UIDValidity, data.NumMessages, nil
}

// openAt is open for a message named by uidValidity: ErrGone when the mailbox has been
// renumbered since.
func (f *fetcher) openAt(mailbox string, uidValidity uint32, writable bool) error {
	have, _, err := f.open(mailbox, writable, false)
	if err != nil {
		return err
	}
	if have != uidValidity {
		return ErrGone
	}
	return nil
}

// Listing is a run of a mailbox's messages, newest to arrive first.
type Listing struct {
	UIDValidity uint32
	Headers     []store.Header
	// More is whether older messages remain.
	More bool
}

// List is a run of up to limit of a mailbox's messages, newest first: the newest, or with
// before, the ones that arrived before that UID under uidValidity.
//
// The first run is the last messages by sequence number, which needs only the count the mailbox
// opens with. A later one asks which UIDs are below the cursor, so mail arriving or leaving
// between two runs does not shift the second.
func (m *Mirror) List(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, before uint32, limit int) (*Listing, error) {
	var out *Listing
	err := m.use(ctx, t, func(f *fetcher) error {
		have, n, err := f.open(mailbox, false, true)
		if err != nil {
			return err
		}
		out = &Listing{UIDValidity: have}
		var set imap.NumSet
		if before == 0 {
			if n == 0 {
				return nil
			}
			from := uint32(1)
			if n > uint32(limit) {
				from = n - uint32(limit) + 1
			}
			set, out.More = seqRange(from, n), from > 1
		} else {
			if have != uidValidity {
				return ErrGone
			}
			if before == 1 {
				return nil
			}
			var opts *imap.SearchOptions
			if caps := f.session.client.Caps(); caps.Has(imap.CapESearch) || caps.Has(imap.CapIMAP4rev2) {
				opts = &imap.SearchOptions{ReturnAll: true}
			}
			below := imap.UIDSet{{Start: 1, Stop: imap.UID(before - 1)}}
			found, err := f.session.client.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{below}}, opts).Wait()
			if err != nil {
				return err
			}
			uids := found.AllUIDs()
			slices.Sort(uids)
			if len(uids) == 0 {
				return nil
			}
			if len(uids) > limit {
				uids, out.More = uids[len(uids)-limit:], true
			}
			set = imap.UIDSetNum(uids...)
		}
		out.Headers, err = f.session.fetch(set)
		return err
	})
	return out, err
}

// Opened is a message as a reading pane needs it: what a list shows of it, and its structure
// with the bytes of its text.
type Opened struct {
	Header    store.Header
	Structure message.Structure
}

// Read is one message, without its attachments: its headers and the server's description of
// its parts first, then the bytes of its text alone. An attachment is fetched when somebody
// downloads it, so a large one never slows the opening. Fetched with BODY.PEEK, so reading it
// is not marking it read: that is SetSeen's.
func (m *Mirror) Read(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) (*Opened, error) {
	var out *Opened
	err := m.use(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, false); err != nil {
			return err
		}
		set := imap.UIDSetNum(imap.UID(uid))
		msgs, err := f.session.client.Fetch(set, &imap.FetchOptions{
			UID:           true,
			Flags:         true,
			InternalDate:  true,
			RFC822Size:    true,
			Envelope:      true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
		}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return ErrGone
		}
		out = &Opened{Header: header(msgs[0])}

		texts := previewParts(msgs[0].BodyStructure)
		var sections []*imap.FetchItemBodySection
		for _, p := range texts {
			sections = append(sections, &imap.FetchItemBodySection{Part: p.section, Peek: true})
		}
		var bodies *imapclient.FetchMessageBuffer
		if len(sections) > 0 {
			got, err := f.session.client.Fetch(set, &imap.FetchOptions{UID: true, BodySection: sections}).Collect()
			if err != nil {
				return err
			}
			if len(got) == 0 {
				return ErrGone
			}
			bodies = got[0]
		}
		out.Structure = structureOf(msgs[0].BodyStructure, texts, sections, bodies)
		return nil
	})
	return out, err
}

// structureOf is a message as the server described it, with the bytes of its text: every part
// that is not a container and not that text, as a leaf.
func structureOf(bs imap.BodyStructure, texts []textPart, sections []*imap.FetchItemBodySection, bodies *imapclient.FetchMessageBuffer) message.Structure {
	var st message.Structure
	isText := map[string]bool{}
	for i, p := range texts {
		at := sectionString(p.section)
		isText[at] = true
		text := &message.Text{Encoding: p.encoding, Charset: p.charset}
		if bodies != nil {
			text.Body = bodies.FindBodySection(sections[i])
		}
		if p.html {
			st.HTML = text
		} else {
			st.Plain = text
		}
	}
	if bs == nil {
		return st
	}
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		at := sectionString(path)
		if isText[at] {
			return true
		}
		leaf := message.Leaf{
			Section:   at,
			Type:      single.MediaType(),
			Params:    single.Params,
			ContentID: single.ID,
			Encoding:  single.Encoding,
			Size:      int(single.Size),
		}
		if d := single.Disposition(); d != nil {
			leaf.Disposition, leaf.DispositionParams = strings.ToLower(d.Value), d.Params
		}
		st.Leaves = append(st.Leaves, leaf)
		return true
	})
	return st
}

func sectionString(path []int) string {
	out := make([]string, len(path))
	for i, n := range path {
		out[i] = strconv.Itoa(n)
	}
	return strings.Join(out, ".")
}

// Part is one part of a message, by its IMAP section: its MIME header and its body, still in
// its transfer encoding. Fetched alone, so an image in a message costs its own bytes rather
// than the whole message's.
func (m *Mirror) Part(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32, section []int) ([]byte, []byte, error) {
	var head, body []byte
	err := m.use(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, false); err != nil {
			return err
		}
		mime := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierMIME, Part: section, Peek: true}
		content := &imap.FetchItemBodySection{Part: section, Peek: true}
		msgs, err := f.session.client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID:         true,
			BodySection: []*imap.FetchItemBodySection{mime, content},
		}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return ErrGone
		}
		head, body = msgs[0].FindBodySection(mime), msgs[0].FindBodySection(content)
		return nil
	})
	return head, body, err
}

// SetSeen marks one message read or unread on the server: the one flag emguio writes, because
// opening a message here is reading it. It says the flags the message has now, and whether
// this changed them.
func (m *Mirror) SetSeen(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32, seen bool) (store.Flags, bool, error) {
	var (
		flags store.Flags
		moved bool
	)
	err := m.use(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, true); err != nil {
			return err
		}
		// Asked first, because a STORE on a UID the server no longer has succeeds and changes
		// nothing.
		set := imap.UIDSetNum(imap.UID(uid))
		msgs, err := f.session.client.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return ErrGone
		}
		flags = flagsOf(msgs[0].Flags)
		if flags.Seen == seen {
			return nil
		}
		op := imap.StoreFlagsAdd
		if !seen {
			op = imap.StoreFlagsDel
		}
		if err := f.session.client.Store(set, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
			return err
		}
		flags.Seen, moved = seen, true
		return nil
	})
	return flags, moved, err
}

// ready opens the session if there is none, or the one there was has closed.
func (f *fetcher) ready(m *Mirror) error {
	if f.session != nil && !f.closed() {
		f.rest()
		return nil
	}
	f.close()
	life, cancel := context.WithCancel(context.Background())
	s, err := m.open(life, f.target)
	if err != nil {
		cancel()
		return err
	}
	f.session, f.cancel = s, cancel
	f.rest()
	return nil
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
	f.mailbox = ""
}
