package mirror

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/store"
)

const (
	// batch is how many new messages are fetched and stored at once. Newest first, and stored
	// as each batch lands, so a large mailbox fills from the top while the rest arrives.
	batch = 500
	// recent is how many of a mailbox's newest messages have their flags looked at on a quick
	// pass. A flag on an older one changes rarely, and is caught on the next full pass.
	recent = 2000
)

// session is one signed-in IMAP connection for one email config.
type session struct {
	m      *Mirror
	target store.SyncTarget
	host   string
	client *imapclient.Client
	listed bool
}

// pass brings the store up to date. A full pass lists the mailboxes and looks at all of them; a
// quick one looks at INBOX only.
func (s *session) pass(ctx context.Context, full bool) (bool, error) {
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
		if !mb.Selectable || (!full && mb.SpecialUse != store.UseInbox) {
			continue
		}
		moved, err := s.mailbox(ctx, mb, full)
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

// mailbox brings one mailbox up to date, and reports whether anything a list shows moved.
//
// STATUS first, which is cheap: when its numbers match the last look, nothing that changes them
// has happened, and the mailbox is not opened at all. A flag changed elsewhere without changing
// the unseen count is caught by a full pass, which looks at INBOX's flags whatever STATUS says.
func (s *session) mailbox(ctx context.Context, mb *store.Mailbox, full bool) (bool, error) {
	st, err := s.client.Status(mb.Name, &imap.StatusOptions{
		NumMessages: true, UIDNext: true, UIDValidity: true, NumUnseen: true,
	}).Wait()
	if err != nil {
		return false, err
	}
	now := store.MailboxStatus{
		UIDValidity: st.UIDValidity,
		UIDNext:     uint32(st.UIDNext),
		Messages:    deref(st.NumMessages),
		Unseen:      deref(st.NumUnseen),
	}
	was := store.MailboxStatus{UIDValidity: mb.UIDValidity, UIDNext: mb.UIDNext, Messages: mb.Messages, Unseen: mb.Unseen}
	same := mb.SyncedAt != nil && now == was
	if same && !(full && mb.SpecialUse == store.UseInbox) {
		return false, nil
	}

	changed := !same
	if now.UIDValidity != mb.UIDValidity {
		if err := s.m.store.ResetMailbox(ctx, mb.ID, now.UIDValidity); err != nil {
			return false, err
		}
	}
	// EXAMINE, not SELECT: a read-only session cannot change \Seen by accident.
	if _, err := s.client.Select(mb.Name, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return false, err
	}
	defer s.unselect()

	var searchOpts *imap.SearchOptions
	if caps := s.client.Caps(); caps.Has(imap.CapESearch) || caps.Has(imap.CapIMAP4rev2) {
		searchOpts = &imap.SearchOptions{ReturnAll: true}
	}
	found, err := s.client.UIDSearch(&imap.SearchCriteria{}, searchOpts).Wait()
	if err != nil {
		return false, err
	}
	local, err := s.m.store.MessageFlags(ctx, mb.ID)
	if err != nil {
		return false, err
	}

	onServer := map[uint32]bool{}
	var missing []uint32
	for _, u := range found.AllUIDs() {
		uid := uint32(u)
		onServer[uid] = true
		if _, ok := local[uid]; !ok {
			missing = append(missing, uid)
		}
	}
	var gone []uint32
	for uid := range local {
		if !onServer[uid] {
			gone = append(gone, uid)
			delete(local, uid)
		}
	}
	if err := s.m.store.DeleteMessages(ctx, mb.ID, gone); err != nil {
		return false, err
	}
	changed = changed || len(gone) > 0

	if moved, err := s.flags(ctx, mb, local, full); err != nil {
		return false, err
	} else if moved {
		changed = true
	}

	slices.Sort(missing)
	slices.Reverse(missing)
	for start := 0; start < len(missing); start += batch {
		chunk := missing[start:min(start+batch, len(missing))]
		headers, err := s.fetch(chunk)
		if err != nil {
			return false, err
		}
		if err := s.m.store.PutMessages(ctx, mb.ID, headers); err != nil {
			return false, err
		}
		s.m.store.Notify(s.target.UserID)
		changed = true
	}

	return changed, s.m.store.SetMailboxStatus(ctx, mb.ID, now)
}

// flags copies the flags of messages already stored: the newest on a quick pass, all of them on
// a full one.
func (s *session) flags(ctx context.Context, mb *store.Mailbox, local map[uint32]store.Flags, full bool) (bool, error) {
	if len(local) == 0 {
		return false, nil
	}
	uids := make([]uint32, 0, len(local))
	for uid := range local {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	from := uids[0]
	if !full && len(uids) > recent {
		from = uids[len(uids)-recent]
	}
	msgs, err := s.client.Fetch(imap.UIDSet{{Start: imap.UID(from), Stop: 0}},
		&imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return false, err
	}
	moved := map[uint32]store.Flags{}
	for _, msg := range msgs {
		uid := uint32(msg.UID)
		was, ok := local[uid]
		if f := flagsOf(msg.Flags); ok && f != was {
			moved[uid] = f
		}
	}
	return len(moved) > 0, s.m.store.SetFlags(ctx, mb.ID, moved)
}

func (s *session) fetch(uids []uint32) ([]store.Header, error) {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
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
	out := make([]store.Header, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, header(msg))
	}
	return out, nil
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

func deref(n *uint32) uint32 {
	if n == nil {
		return 0
	}
	return *n
}
