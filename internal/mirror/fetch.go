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

	"emguio/internal/connect"
	"emguio/internal/message"
	"emguio/internal/store"
)

// ErrGone is a message or a mailbox the server no longer has as it was named: expunged, moved,
// or in a mailbox whose UIDVALIDITY has changed since.
var ErrGone = errors.New("mirror: gone")

// FetchIdle is how long a reading session stays open after its last use.
const FetchIdle = 5 * time.Minute

// fetcher is one of an email config's sessions besides the sync's: one for reading what is not
// kept — a mailbox's list, a message, one of its parts — and one for changing what is on the
// server: jobs, drafts, filing in Sent. Sessions of their own, so a read waits behind neither a
// pass nor a long run of jobs; one request at a time on each.
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
	uidNext     uint32
}

// fetcherFor is the config's session for reading, or for changing, a new one when the config
// changed since the last: the old one would go on signing in with what was saved before.
func (m *Mirror) fetcherFor(t store.SyncTarget, changing bool) *fetcher {
	key := t.ID
	if changing {
		key += " changing"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.fetchers[key]
	if f != nil && !f.target.UpdatedAt.Equal(t.UpdatedAt) {
		go f.shut()
		f = nil
	}
	if f == nil {
		f = &fetcher{target: t}
		m.fetchers[key] = f
	}
	return f
}

// use runs one request on the config's reading session.
func (m *Mirror) use(ctx context.Context, t store.SyncTarget, do func(f *fetcher) error) error {
	return m.on(ctx, m.fetcherFor(t, false), do)
}

// change runs one change on the config's session for changing.
func (m *Mirror) change(ctx context.Context, t store.SyncTarget, do func(f *fetcher) error) error {
	return m.on(ctx, m.fetcherFor(t, true), do)
}

// on runs one request on f. The request bounds it: when ctx ends first, the session is closed
// under it. A session that failed is not trusted with the next request; one whose server
// answered — gone, refused, cannot — is fine.
func (m *Mirror) on(ctx context.Context, f *fetcher, do func(f *fetcher) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.ready(m); err != nil {
		return err
	}
	client := f.session.client
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()
	err := do(f)
	var no *connect.Failure
	if err != nil && !errors.Is(err, ErrGone) && !errors.Is(err, ErrUnsupported) && !errors.As(err, &no) {
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
	f.mailbox, f.writable, f.uidValidity, f.uidNext = mailbox, writable, data.UIDValidity, uint32(data.UIDNext)
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
// before, the ones that arrived before that UID under uidValidity. With q, only those the server
// finds for it, see Criteria.
//
// The first run is the last messages by sequence number, which needs only the count the mailbox
// opens with. A later one, and every run of a search, asks the server which UIDs match below the
// cursor, so mail arriving or leaving between two runs does not shift the second.
func (m *Mirror) List(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, before uint32, limit int, q string) (*Listing, error) {
	criteria := Criteria(q)
	var out *Listing
	err := m.use(ctx, t, func(f *fetcher) error {
		have, n, err := f.open(mailbox, false, true)
		if err != nil {
			return err
		}
		out = &Listing{UIDValidity: have}
		var set imap.NumSet
		if before == 0 && criteria == nil {
			if n == 0 {
				return nil
			}
			from := uint32(1)
			if n > uint32(limit) {
				from = n - uint32(limit) + 1
			}
			set, out.More = seqRange(from, n), from > 1
		} else {
			var search imap.SearchCriteria
			if criteria != nil {
				search = *criteria
			}
			if before > 0 {
				if have != uidValidity {
					return ErrGone
				}
				if before == 1 {
					return nil
				}
				search.UID = []imap.UIDSet{{{Start: 1, Stop: imap.UID(before - 1)}}}
			}
			var opts *imap.SearchOptions
			if caps := f.session.client.Caps(); caps.Has(imap.CapESearch) || caps.Has(imap.CapIMAP4rev2) {
				opts = &imap.SearchOptions{ReturnAll: true}
			}
			found, err := f.session.client.UIDSearch(&search, opts).Wait()
			if err != nil {
				return refused(err)
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
	Header store.Header
	// ReplyTo is where its sender asks replies to go, when the server says.
	ReplyTo []store.Address
	// Bcc is in a message only as its sender keeps it: a draft, or sent mail some clients file.
	Bcc       []store.Address
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
		if env := msgs[0].Envelope; env != nil {
			out.ReplyTo = addresses(env.ReplyTo)
			out.Bcc = addresses(env.Bcc)
		}

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

// Flags emguio sets on the server: read, starred, and answered once a reply has gone.
const (
	Seen     = imap.FlagSeen
	Flagged  = imap.FlagFlagged
	Answered = imap.FlagAnswered
)

// ErrUnsupported is an action the server cannot do to some messages without touching others.
var ErrUnsupported = errors.New("mirror: unsupported")

// Had is the flags each message an action was asked about had before it, by UID. A UID the
// server no longer has is not in it: a STORE, MOVE or EXPUNGE naming one succeeds and does
// nothing, so what is gone is told apart first.
type Had map[uint32]store.Flags

// present reads the flags of those of uids the server still has, in one FETCH.
func (f *fetcher) present(uids []uint32) (Had, error) {
	var asked imap.UIDSet
	for _, uid := range uids {
		asked.AddNum(imap.UID(uid))
	}
	msgs, err := f.session.client.Fetch(asked, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return nil, err
	}
	had := make(Had, len(msgs))
	for _, msg := range msgs {
		had[uint32(msg.UID)] = flagsOf(msg.Flags)
	}
	return had, nil
}

// set is the UIDs that are there, for the one command that acts on all of them.
func (h Had) set() imap.UIDSet {
	var s imap.UIDSet
	for uid := range h {
		s.AddNum(imap.UID(uid))
	}
	return s
}

// has is whether flags carry flag: Seen, Flagged or Answered.
func has(flags store.Flags, flag imap.Flag) bool {
	switch flag {
	case Flagged:
		return flags.Flagged
	case Answered:
		return flags.Answered
	}
	return flags.Seen
}

// SetFlag sets or clears one flag of messages on the server, Seen or Flagged, in one STORE of
// those it changes, and says the flags each had.
func (m *Mirror) SetFlag(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32, flag imap.Flag, on bool) (Had, error) {
	var had Had
	err := m.change(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, true); err != nil {
			return err
		}
		var err error
		if had, err = f.present(uids); err != nil {
			return err
		}
		var change imap.UIDSet
		for uid, flags := range had {
			if has(flags, flag) != on {
				change.AddNum(imap.UID(uid))
			}
		}
		if len(change) == 0 {
			return nil
		}
		op := imap.StoreFlagsAdd
		if !on {
			op = imap.StoreFlagsDel
		}
		return refused(f.session.client.Store(change, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{flag}}, nil).Close())
	})
	return had, err
}

// Move moves messages to another mailbox in one command, and says the flags each had.
//
// With MOVE where the server has it. Without, COPY, \Deleted and an EXPUNGE of those UIDs,
// which needs UIDPLUS: a plain EXPUNGE would also remove whatever another client had marked
// deleted, and on a server with neither the move is refused.
func (m *Mirror) Move(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32, to string) (Had, error) {
	var had Had
	err := m.change(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, true); err != nil {
			return err
		}
		var err error
		if had, err = f.present(uids); err != nil || len(had) == 0 {
			return err
		}
		if caps := f.session.client.Caps(); !caps.Has(imap.CapMove) && !caps.Has(imap.CapUIDPlus) {
			return ErrUnsupported
		}
		_, err = f.session.client.Move(had.set(), to).Wait()
		return refused(err)
	})
	return had, err
}

// Delete removes messages from the server for good: \Deleted, and an EXPUNGE of those UIDs. It
// needs UIDPLUS, for the reason Move does, and says the flags each had.
func (m *Mirror) Delete(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32) (Had, error) {
	var had Had
	err := m.change(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, true); err != nil {
			return err
		}
		var err error
		if had, err = f.present(uids); err != nil || len(had) == 0 {
			return err
		}
		if !f.session.client.Caps().Has(imap.CapUIDPlus) {
			return ErrUnsupported
		}
		set := had.set()
		if err := f.session.client.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
			return refused(err)
		}
		return refused(f.session.client.UIDExpunge(set).Close())
	})
	return had, err
}

// refused is a server's NO to an action, as the sentence a person is shown; a request the server
// refused is not a session gone wrong.
func refused(err error) error {
	var no *imap.Error
	if errors.As(err, &no) {
		return &connect.Failure{Class: "refused", Sentence: "The server refused: " + connect.Said(no.Text)}
	}
	return err
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
