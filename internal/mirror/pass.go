package mirror

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/message"
	"emguio/internal/store"
)

// How many of a mailbox's newest messages are kept, so its list opens on them without asking the
// server: INBOX's, which is opened most, and every other's. Anything older is read from the server
// when it is asked for.
const (
	Window       = 30
	FolderWindow = 10
)

func windowOf(mb *store.Mailbox) uint32 {
	if mb.SpecialUse == store.UseInbox {
		return Window
	}
	return FolderWindow
}

// session is one signed-in IMAP connection for one email config.
type session struct {
	m      *Mirror
	target store.SyncTarget
	host   string
	client *imapclient.Client
	listed bool
	// moved is signalled when the server says, unasked, that the selected mailbox changed.
	moved chan struct{}
	// news is what it says, under NOTIFY, of the others.
	news *news
	// saw is INBOX as the last pass found it.
	saw *store.MailboxStatus
}

// pass brings what is kept up to date: on a full pass the list of mailboxes and every one's counts
// and window; otherwise INBOX's, or only the mailboxes named.
func (s *session) pass(ctx context.Context, full bool, named []string) (bool, error) {
	changed := false
	if full || !s.listed {
		listed, err := s.list()
		if err != nil {
			return false, err
		}
		moved, err := s.m.store.PutMailboxes(ctx, s.target.ID, listed)
		if err != nil {
			return false, err
		}
		changed = moved
		s.listed = true
	}
	boxes, err := s.m.store.MirrorMailboxes(ctx, s.target.ID)
	if err != nil {
		return changed, err
	}
	for _, mb := range boxes {
		switch {
		case !mb.Selectable:
			continue
		case full:
		case named != nil:
			if !slices.Contains(named, mb.Name) {
				continue
			}
		case mb.SpecialUse != store.UseInbox:
			continue
		}
		moved, err := s.window(ctx, mb)
		var refusal *imap.Error
		if errors.As(err, &refusal) {
			// One mailbox the server will not open is that mailbox's problem, not the session's.
			s.m.log.Warn("mirror skipped a mailbox", "email_config", s.target.ID, "mailbox", mb.ID, "reason", refusal.Text)
			continue
		}
		if err != nil {
			return changed, err
		}
		changed = changed || moved
	}
	return changed, nil
}

// uses maps the server's own flags for a mailbox's purpose onto ours.
var uses = map[imap.MailboxAttr]string{
	imap.MailboxAttrDrafts:  store.UseDrafts,
	imap.MailboxAttrSent:    store.UseSent,
	imap.MailboxAttrArchive: store.UseArchive,
	imap.MailboxAttrJunk:    store.UseJunk,
	imap.MailboxAttrTrash:   store.UseTrash,
	imap.MailboxAttrAll:     store.UseAll,
	imap.MailboxAttrFlagged: store.UseFlagged,
}

// guesses are the names a mailbox goes by on servers that do not flag what it is for.
var guesses = map[string]string{
	"drafts": store.UseDrafts, "черновики": store.UseDrafts,
	"sent": store.UseSent, "sent items": store.UseSent, "sent messages": store.UseSent,
	"sent mail": store.UseSent, "отправленные": store.UseSent,
	"archive": store.UseArchive, "archives": store.UseArchive, "архив": store.UseArchive,
	"junk": store.UseJunk, "spam": store.UseJunk, "junk e-mail": store.UseJunk, "спам": store.UseJunk,
	"trash": store.UseTrash, "deleted": store.UseTrash, "deleted items": store.UseTrash,
	"deleted messages": store.UseTrash, "bin": store.UseTrash, "корзина": store.UseTrash,
	"удаленные": store.UseTrash, "удалённые": store.UseTrash,
}

func (s *session) list() ([]store.Listed, error) {
	var opts *imap.ListOptions
	if s.client.Caps().Has(imap.CapSpecialUse) {
		opts = &imap.ListOptions{ReturnSpecialUse: true}
	}
	data, err := s.client.List("", "*", opts).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]store.Listed, 0, len(data))
	flagged := map[string]bool{}
	for _, d := range data {
		l := store.Listed{Name: d.Mailbox, Selectable: true}
		if d.Delim != 0 {
			l.Delimiter = string(d.Delim)
		}
		for _, a := range d.Attrs {
			switch a {
			case imap.MailboxAttrNoSelect, imap.MailboxAttrNonExistent:
				l.Selectable = false
			}
			if use, ok := uses[a]; ok && l.SpecialUse == "" {
				l.SpecialUse = use
			}
		}
		if strings.EqualFold(d.Mailbox, "INBOX") {
			l.SpecialUse = store.UseInbox
		}
		flagged[l.SpecialUse] = true
		out = append(out, l)
	}
	// A guess only for a use no mailbox claims, and only once each: two folders called Sent is
	// a server's own business.
	for i := range out {
		l := &out[i]
		if l.SpecialUse != "" || !l.Selectable {
			continue
		}
		parts := []string{l.Name}
		if l.Delimiter != "" {
			parts = strings.Split(l.Name, l.Delimiter)
		}
		use := guesses[strings.ToLower(parts[len(parts)-1])]
		if use != "" && !flagged[use] {
			l.SpecialUse = use
			flagged[use] = true
		}
	}
	return out, nil
}

// status is what the server says a mailbox holds now.
func (s *session) status(mb *store.Mailbox) (store.MailboxStatus, error) {
	st, err := s.client.Status(mb.Name, &imap.StatusOptions{
		NumMessages: true, UIDNext: true, UIDValidity: true, NumUnseen: true,
	}).Wait()
	if err != nil {
		return store.MailboxStatus{}, err
	}
	return store.MailboxStatus{
		UIDValidity: st.UIDValidity,
		UIDNext:     uint32(st.UIDNext),
		Messages:    deref(st.NumMessages),
		Unseen:      deref(st.NumUnseen),
	}, nil
}

// window brings a mailbox's counts and its kept newest messages up to date, and reports whether
// anything a list shows moved.
//
// The window is opened every time rather than only when STATUS has moved: a flag changed
// elsewhere — a star — moves no number STATUS reports, and the window is small enough that its
// flags cost one short FETCH.
func (s *session) window(ctx context.Context, mb *store.Mailbox) (bool, error) {
	now, err := s.status(mb)
	if err != nil {
		return false, err
	}
	was := store.MailboxStatus{UIDValidity: mb.UIDValidity, UIDNext: mb.UIDNext, Messages: mb.Messages, Unseen: mb.Unseen}
	changed := mb.SyncedAt == nil || now != was
	if now.UIDValidity != mb.UIDValidity {
		if err := s.m.store.ResetMailbox(ctx, mb.ID, now.UIDValidity); err != nil {
			return false, err
		}
	}
	// EXAMINE, not SELECT: a read-only session cannot change \Seen by accident.
	data, err := s.client.Select(mb.Name, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return false, err
	}
	defer s.unselect()

	current := map[uint32]store.Flags{}
	if n := data.NumMessages; n > 0 {
		from := uint32(1)
		if size := windowOf(mb); n > size {
			from = n - size + 1
		}
		msgs, err := s.client.Fetch(seqRange(from, n), &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return false, err
		}
		for _, msg := range msgs {
			current[uint32(msg.UID)] = flagsOf(msg.Flags)
		}
	}
	kept, err := s.m.store.MessageFlags(ctx, mb.ID)
	if err != nil {
		return false, err
	}

	var gone, missing []uint32
	moved := map[uint32]store.Flags{}
	for uid, f := range kept {
		switch now, ok := current[uid]; {
		case !ok:
			gone = append(gone, uid)
		case now != f:
			moved[uid] = now
		}
	}
	for uid := range current {
		if _, ok := kept[uid]; !ok {
			missing = append(missing, uid)
		}
	}
	if err := s.m.store.DeleteMessages(ctx, mb.ID, gone); err != nil {
		return false, err
	}
	if err := s.m.store.SetFlags(ctx, mb.ID, moved); err != nil {
		return false, err
	}
	if len(missing) > 0 {
		var set imap.UIDSet
		for _, uid := range missing {
			set.AddNum(imap.UID(uid))
		}
		headers, err := s.fetch(set)
		if err != nil {
			return false, err
		}
		if err := s.m.store.PutMessages(ctx, mb.ID, headers); err != nil {
			return false, err
		}
	}
	changed = changed || len(gone) > 0 || len(moved) > 0 || len(missing) > 0
	if mb.SpecialUse == store.UseInbox {
		s.saw = &now
	}
	return changed, s.m.store.SetMailboxStatus(ctx, mb.ID, now)
}

// fetch is what a list shows of the messages in set, newest to arrive first.
func (s *session) fetch(set imap.NumSet) ([]store.Header, error) {
	msgs, err := s.client.Fetch(set, &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		InternalDate:  true,
		RFC822Size:    true,
		Envelope:      true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true},
	}).Collect()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(msgs, func(a, b *imapclient.FetchMessageBuffer) int { return cmp.Compare(b.UID, a.UID) })
	out := make([]store.Header, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, header(msg))
	}
	s.previews(msgs, out)
	return out, nil
}

// How much of a text part a preview is read from. A line of text and room for the markup an HTML
// part wraps it in, first; then, for a message that gave no line, enough to get past an HTML
// part's head and styles, which can fill the first few kilobytes on their own.
const (
	previewBytes = 2 << 10
	previewMore  = 32 << 10
)

// textPart is where a message's preview is read from.
type textPart struct {
	section  []int
	encoding string
	charset  string
	html     bool
	size     uint32
}

// previewParts are where a preview can be read from, best first: the first plain text part
// outside an attachment, then the first HTML one.
func previewParts(bs imap.BodyStructure) []textPart {
	var plain, rich *textPart
	if bs == nil {
		return nil
	}
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if d := single.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
			return false
		}
		found := &textPart{section: append([]int(nil), path...), encoding: single.Encoding,
			charset: param(single.Params, "charset"), size: single.Size}
		switch single.MediaType() {
		case "text/plain":
			if plain == nil {
				plain = found
			}
		case "text/html":
			if rich == nil {
				found.html = true
				rich = found
			}
		case "message/rfc822":
			// A forwarded message's text is the forwarded message's, not this one's.
			return false
		}
		return true
	})
	var out []textPart
	for _, p := range []*textPart{plain, rich} {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

// previews fills in what each header's list row shows under its subject.
//
// The best part's first bytes, for every message; then, for a message that gave no line, more of
// that part when there was more, or else the next part. A FETCH per distinct part rather than
// per message: in a run of messages most keep their text in the same place.
//
// Best effort. A preview that cannot be read is an empty line in a list, not a failed sync.
func (s *session) previews(msgs []*imapclient.FetchMessageBuffer, headers []store.Header) {
	want := map[uint32]textPart{}
	next := map[uint32]textPart{}
	for _, msg := range msgs {
		parts := previewParts(msg.BodyStructure)
		if len(parts) == 0 {
			continue
		}
		want[uint32(msg.UID)] = parts[0]
		if parts[0].size > previewBytes {
			next[uint32(msg.UID)] = parts[0]
		} else if len(parts) > 1 {
			next[uint32(msg.UID)] = parts[1]
		}
	}
	found := s.readPreviews(want, previewBytes)
	again := map[uint32]textPart{}
	for uid, part := range next {
		if found[uid] == "" {
			again[uid] = part
		}
	}
	for uid, preview := range s.readPreviews(again, previewMore) {
		found[uid] = preview
	}
	for i := range headers {
		headers[i].Preview = found[headers[i].UID]
	}
}

// readPreviews reads the first size bytes of each message's part, and makes a line of each.
func (s *session) readPreviews(parts map[uint32]textPart, size uint32) map[uint32]string {
	type group struct {
		part textPart
		uids imap.UIDSet
	}
	groups := map[string]*group{}
	for uid, part := range parts {
		key := fmt.Sprint(part.section)
		if groups[key] == nil {
			groups[key] = &group{part: part}
		}
		groups[key].uids.AddNum(imap.UID(uid))
	}
	found := map[uint32]string{}
	for _, g := range groups {
		section := &imap.FetchItemBodySection{
			Part:    g.part.section,
			Partial: &imap.SectionPartial{Offset: 0, Size: int64(size)},
			Peek:    true,
		}
		got, err := s.client.Fetch(g.uids, &imap.FetchOptions{
			UID:         true,
			BodySection: []*imap.FetchItemBodySection{section},
		}).Collect()
		if err != nil {
			s.m.log.Warn("mirror could not read previews", "email_config", s.target.ID, "err", err)
			continue
		}
		for _, msg := range got {
			if len(msg.BodySection) == 0 {
				continue
			}
			part := parts[uint32(msg.UID)]
			found[uint32(msg.UID)] = message.SectionPreview(msg.BodySection[0].Bytes, part.encoding, part.charset, part.html)
		}
	}
	return found
}

func param(params map[string]string, key string) string {
	for k, v := range params {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// unselect closes the mailbox, so the next STATUS is never about the one open: servers are
// allowed to answer that one from stale numbers.
func (s *session) unselect() {
	if caps := s.client.Caps(); caps.Has(imap.CapUnselect) || caps.Has(imap.CapIMAP4rev2) {
		s.client.Unselect().Wait()
	}
}

func header(msg *imapclient.FetchMessageBuffer) store.Header {
	h := store.Header{
		UID:            uint32(msg.UID),
		Flags:          flagsOf(msg.Flags),
		InternalDate:   msg.InternalDate.UTC(),
		Size:           msg.RFC822Size,
		HasAttachments: hasAttachments(msg.BodyStructure),
	}
	if env := msg.Envelope; env != nil {
		h.Subject = clean(env.Subject)
		if len(env.From) > 0 {
			h.From = address(env.From[0])
		}
		h.To = addresses(env.To)
		h.Cc = addresses(env.Cc)
		h.MessageID = clean(env.MessageID)
		if len(env.InReplyTo) > 0 {
			h.InReplyTo = clean(env.InReplyTo[0])
		}
		if !env.Date.IsZero() {
			d := env.Date.UTC()
			h.Date = &d
		}
	}
	// A server that leaves out the internal date leaves the list nothing to order by, so the
	// sender's date stands in, and failing that the moment it was copied.
	if h.InternalDate.IsZero() {
		if h.Date != nil {
			h.InternalDate = *h.Date
		} else {
			h.InternalDate = time.Now().UTC()
		}
	}
	return h
}

func flagsOf(flags []imap.Flag) store.Flags {
	var f store.Flags
	for _, flag := range flags {
		switch flag {
		case imap.FlagSeen:
			f.Seen = true
		case imap.FlagFlagged:
			f.Flagged = true
		case imap.FlagAnswered:
			f.Answered = true
		case imap.FlagDraft:
			f.Draft = true
		}
	}
	return f
}

func address(a imap.Address) store.Address {
	email := a.Mailbox
	if a.Host != "" {
		email += "@" + a.Host
	}
	return store.Address{Name: clean(a.Name), Email: clean(email)}
}

func addresses(list []imap.Address) []store.Address {
	out := make([]store.Address, 0, len(list))
	for _, a := range list {
		// A group's start and end markers carry no address of their own.
		if a.Mailbox == "" {
			continue
		}
		out = append(out, address(a))
	}
	return out
}

// hasAttachments is any part a person would call an attachment: one marked as such, or a named
// part that is not shown inline.
func hasAttachments(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(_ []int, part imap.BodyStructure) bool {
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return !found
		}
		disposition := ""
		if d := single.Disposition(); d != nil {
			disposition = strings.ToLower(d.Value)
		}
		if disposition == "attachment" || (single.Filename() != "" && disposition != "inline") {
			found = true
		}
		return !found
	})
	return found
}

// clean is header text made fit to store: valid UTF-8, on one line, without the spaces a folded
// header leaves behind.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func seqRange(from, to uint32) imap.SeqSet {
	var set imap.SeqSet
	set.AddRange(from, to)
	return set
}

func deref(n *uint32) uint32 {
	if n == nil {
		return 0
	}
	return *n
}
