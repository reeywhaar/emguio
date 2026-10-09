package mirror

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/store"
)

// Origin is what a reply needs of the message it answers, to be threaded under it, and what a
// draft says it answers itself.
type Origin struct {
	MessageID  string
	InReplyTo  string
	References []string
}

// msgID is one message id in a References header.
var msgID = regexp.MustCompile(`<[^<>\s]+>`)

// Origin reads a message's Message-ID, In-Reply-To and the References it carries, for a reply's
// In-Reply-To and References.
func (m *Mirror) Origin(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) (*Origin, error) {
	var out *Origin
	err := m.use(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, false); err != nil {
			return err
		}
		refs := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: []string{"References"}, Peek: true}
		msgs, err := f.session.client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID:         true,
			Envelope:    true,
			BodySection: []*imap.FetchItemBodySection{refs},
		}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return ErrGone
		}
		out = &Origin{References: msgID.FindAllString(string(msgs[0].FindBodySection(refs)), -1)}
		// The envelope gives them without their angle brackets, which the headers want back.
		if env := msgs[0].Envelope; env != nil {
			out.MessageID = bracketed(env.MessageID)
			if len(env.InReplyTo) > 0 {
				out.InReplyTo = bracketed(env.InReplyTo[0])
			}
		}
		return nil
	})
	return out, err
}

func bracketed(id string) string {
	if id = strings.Trim(clean(id), "<>"); id == "" {
		return ""
	}
	return "<" + id + ">"
}

// Sending is what follows a message's sending, once the server has taken it.
type Sending struct {
	// Sent is where a copy is filed; nil for a config with no Sent mailbox.
	Sent *store.Mailbox
	// ID is the message's Message-ID, and Raw the message.
	ID  string
	Raw []byte
	// Answers is the message it replies to, marked answered; nil for none.
	Answers *Located
	// Draft is the draft it was written in, deleted; nil for none.
	Draft *Located
}

// Located is a message where the server holds it.
type Located struct {
	Mailbox     *store.Mailbox
	UIDValidity uint32
	UID         uint32
}

// fileAfter is how long a sent message is given to reach Sent by the mail server's own hand
// before a copy is filed: Gmail and others file what their SMTP is given, and another copy would
// be a second one there.
var fileAfter = 3 * time.Second

// Sent files a copy of a sent message in Sent, unless the server already has one there by its
// Message-ID, and marks the message it answers as answered. In the background, after the sender
// has been told it went: by then it has, and what fails here is logged rather than shown.
func (m *Mirror) Sent(t store.SyncTarget, s Sending) {
	m.running.Add(1)
	go func() {
		defer m.running.Done()
		ctx := m.life
		select {
		case <-ctx.Done():
			return
		case <-time.After(fileAfter):
		}
		if s.Sent != nil {
			if err := m.file(ctx, t, s.Sent.Name, s.ID, s.Raw); err != nil && ctx.Err() == nil {
				m.log.Warn("a sent message could not be filed in Sent", append(who(t), "error", err.Error())...)
			}
		}
		if a := s.Answers; a != nil {
			had, err := m.SetFlag(ctx, t, a.Mailbox.Name, a.UIDValidity, []uint32{a.UID}, Answered, true)
			if before, ok := had[a.UID]; err == nil && ok {
				before.Answered = true
				err = m.store.SetMessageFlags(ctx, a.Mailbox.ID, a.UID, before, 0)
			}
			if err != nil && ctx.Err() == nil {
				m.log.Warn("the message replied to could not be marked answered", append(who(t), "error", err.Error())...)
			}
		}
		if d := s.Draft; d != nil {
			if _, err := m.Delete(ctx, t, d.Mailbox.Name, d.UIDValidity, []uint32{d.UID}); err != nil && !errors.Is(err, ErrGone) && ctx.Err() == nil {
				m.log.Warn("a sent message's draft could not be deleted from Drafts", append(who(t), "error", err.Error())...)
			}
		}
		m.store.Notify(t.UserID)
		m.Refresh(t.ID)
	}()
}

// file appends raw to mailbox, read, unless a message there has its Message-ID already.
func (m *Mirror) file(ctx context.Context, t store.SyncTarget, mailbox, id string, raw []byte) error {
	return m.change(ctx, t, func(f *fetcher) error {
		if _, _, err := f.open(mailbox, false, true); err != nil {
			return err
		}
		found, err := f.session.client.UIDSearch(&imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: id}},
		}, nil).Wait()
		if err != nil {
			return refused(err)
		}
		if len(found.AllUIDs()) > 0 {
			return nil
		}
		cmd := f.session.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{
			Flags: []imap.Flag{imap.FlagSeen},
			Time:  time.Now(),
		})
		if _, err := cmd.Write(raw); err != nil {
			cmd.Close()
			return err
		}
		if err := cmd.Close(); err != nil {
			return refused(err)
		}
		_, err = cmd.Wait()
		return refused(err)
	})
}

// SaveDraft keeps a draft in mailbox, read and marked \Draft, in place of the one it replaces,
// and says where the server put it. The new one first, so a failure leaves the old. Refused on
// a server without UIDPLUS: it neither says where a message went nor removes one alone.
func (m *Mirror) SaveDraft(ctx context.Context, t store.SyncTarget, mailbox string, raw []byte, replaces *Located) (uint32, uint32, error) {
	var at *imap.AppendData
	err := m.change(ctx, t, func(f *fetcher) error {
		if !f.session.client.Caps().Has(imap.CapUIDPlus) {
			return ErrUnsupported
		}
		cmd := f.session.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{
			Flags: []imap.Flag{imap.FlagSeen, imap.FlagDraft},
			Time:  time.Now(),
		})
		if _, err := cmd.Write(raw); err != nil {
			cmd.Close()
			return err
		}
		if err := cmd.Close(); err != nil {
			return refused(err)
		}
		var err error
		if at, err = cmd.Wait(); err != nil {
			return refused(err)
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if replaces != nil {
		if _, err := m.Delete(ctx, t, replaces.Mailbox.Name, replaces.UIDValidity, []uint32{replaces.UID}); err != nil && !errors.Is(err, ErrGone) {
			m.log.Warn("an older copy of a draft could not be deleted from Drafts", append(who(t), "error", err.Error())...)
		}
	}
	return at.UIDValidity, uint32(at.UID), nil
}
