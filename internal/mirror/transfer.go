package mirror

import (
	"context"
	"errors"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/ids"
	"emguio/internal/store"
)

// kept are the flags a transferred message keeps: a keyword one server takes, another may not.
var kept = map[imap.Flag]bool{imap.FlagSeen: true, imap.FlagFlagged: true, imap.FlagAnswered: true, imap.FlagDraft: true}

// whole is a message as it is copied to another server: all of it, its flags, the date it
// arrived, and its Message-ID, to find it there by.
type whole struct {
	raw   []byte
	flags []imap.Flag
	date  time.Time
	id    string
}

// errTargetGone is a transfer's mailbox gone from its server since it was asked.
var errTargetGone = store.NotFound("The folder it was moving to is no longer on the mail server.")

// transfer moves one message to a mailbox of another email config: read whole here, put there
// with its flags and the date it arrived unless a message there has its Message-ID already,
// then deleted here for good. A stop between the two leaves it in both, and the next try finds
// it there and only deletes it here. See docs/reading.md.
func (m *Mirror) transfer(ctx context.Context, t store.SyncTarget, j *store.Job) error {
	from, err := m.store.MirrorMailbox(ctx, j.MailboxID)
	if err != nil {
		return err
	}
	to, err := m.store.MirrorMailbox(ctx, j.Target)
	if err != nil {
		return err
	}
	there, err := m.store.JobTarget(ctx, to.EmailConfigID)
	if err != nil {
		return err
	}
	uidValidity, uid, ok := ids.ParseMessage(j.Message)
	if !ok {
		return store.Invalid("%q is not a message id.", j.Message)
	}
	w, err := m.whole(ctx, t, from.Name, uidValidity, uid)
	if err != nil {
		return err
	}
	if err := m.place(ctx, there, to.Name, w); err != nil {
		return err
	}
	had, err := m.Delete(ctx, t, from.Name, uidValidity, []uint32{uid})
	// Gone from here meanwhile, it is there all the same.
	if errors.Is(err, ErrGone) {
		return nil
	}
	if err != nil {
		return err
	}
	before, ok := had[uid]
	if !ok {
		return nil
	}
	return m.store.MessageMoved(ctx, from.ID, "", uid, before.Seen)
}

// whole reads a message for copying elsewhere. Deleting it here after needs UIDPLUS, so on a
// server without it nothing is read, and nothing copied.
func (m *Mirror) whole(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) (*whole, error) {
	var w whole
	err := m.change(ctx, t, func(f *fetcher) error {
		if err := f.openAt(mailbox, uidValidity, true, uid); err != nil {
			return err
		}
		if !f.session.client.Caps().Has(imap.CapUIDPlus) {
			return ErrUnsupported
		}
		body := &imap.FetchItemBodySection{Peek: true}
		msgs, err := f.session.client.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{
			UID: true, Flags: true, InternalDate: true, Envelope: true,
			BodySection: []*imap.FetchItemBodySection{body},
		}).Collect()
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			return ErrGone
		}
		msg := msgs[0]
		w.raw, w.date = msg.FindBodySection(body), msg.InternalDate
		if msg.Envelope != nil {
			w.id = msg.Envelope.MessageID
		}
		for _, flag := range msg.Flags {
			if kept[flag] {
				w.flags = append(w.flags, flag)
			}
		}
		return nil
	})
	return &w, err
}

// place puts a message read elsewhere into mailbox, unless one there has its Message-ID.
func (m *Mirror) place(ctx context.Context, t store.SyncTarget, mailbox string, w *whole) error {
	return m.change(ctx, t, func(f *fetcher) error {
		err := appendOnce(f, mailbox, w.id, w.raw, w.flags, w.date)
		if errors.Is(err, ErrGone) {
			return errTargetGone
		}
		return err
	})
}

// appendOnce appends raw to mailbox with flags, dated at, unless a message there has the
// Message-ID id already. Without an id there is nothing to look for, and it is appended.
func appendOnce(f *fetcher, mailbox, id string, raw []byte, flags []imap.Flag, at time.Time) error {
	if id != "" {
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
	}
	if at.IsZero() {
		at = time.Now()
	}
	cmd := f.session.client.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: flags, Time: at})
	if _, err := cmd.Write(raw); err != nil {
		cmd.Close()
		return err
	}
	if err := cmd.Close(); err != nil {
		return refused(err)
	}
	_, err := cmd.Wait()
	return refused(err)
}
