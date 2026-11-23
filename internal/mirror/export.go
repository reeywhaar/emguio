package mirror

import (
	"context"
	"io"
	"slices"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"emguio/internal/store"
)

// exportBatch is how many messages one FETCH asks for while exporting. Each is still handed on
// alone, its text as it arrives: the batch bounds round trips, not memory.
const exportBatch = 100

// Exporting is one message as Export hands it on: what is said around its text.
type Exporting struct {
	UID  uint32
	Date time.Time
	// From is the envelope's first sender, for an mbox's From_ line.
	From                           string
	Seen, Answered, Flagged, Draft bool
}

// errShifted is a folder that changed under an export, so a message's text could not be told
// apart from another's.
var errShifted = store.Conflict("The folder changed while it was exported. Export it again.")

// Export reads messages of a mailbox, uids or with none every one, oldest first, and hands each
// to each with its text to read as it comes from the server: none is held whole. A session of
// its own, so the config's reading and changing wait on none of it. uidValidity, when not 0,
// is what uids were named under. It says the UIDVALIDITY it read under.
func (m *Mirror) Export(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32, each func(Exporting, io.Reader) error) (uint32, error) {
	var opened uint32
	err := m.on(ctx, m.fetcherKeyed(t, "export"), func(f *fetcher) error {
		have, _, err := f.open(mailbox, false, true)
		if err != nil {
			return err
		}
		if uidValidity != 0 && have != uidValidity {
			return ErrGone
		}
		opened = have
		c := f.session.client
		list := slices.Clone(uids)
		if uids == nil {
			found, err := c.UIDSearch(&imap.SearchCriteria{}, f.session.all()).Wait()
			if err != nil {
				return refused(err)
			}
			for _, uid := range found.AllUIDs() {
				list = append(list, uint32(uid))
			}
		}
		slices.Sort(list)
		list = slices.Compact(list)
		for len(list) > 0 {
			n := min(exportBatch, len(list))
			if err := exportBatchOf(c, list[:n], each); err != nil {
				return err
			}
			list = list[n:]
		}
		return nil
	})
	return opened, err
}

// exportBatchOf hands on a batch: what is said around each message first, which is small, then
// the texts, each streamed. A message gone meanwhile is passed over.
func exportBatchOf(c *imapclient.Client, batch []uint32, each func(Exporting, io.Reader) error) error {
	set := imap.UIDSet{}
	for _, uid := range batch {
		set.AddNum(imap.UID(uid))
	}
	metas, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, InternalDate: true, Envelope: true}).Collect()
	if err != nil {
		return err
	}
	bySeq := map[uint32]Exporting{}
	byUID := map[uint32]Exporting{}
	for _, msg := range metas {
		e := Exporting{UID: uint32(msg.UID), Date: msg.InternalDate}
		if env := msg.Envelope; env != nil && len(env.From) > 0 {
			e.From = env.From[0].Addr()
		}
		for _, flag := range msg.Flags {
			switch flag {
			case imap.FlagSeen:
				e.Seen = true
			case imap.FlagAnswered:
				e.Answered = true
			case imap.FlagFlagged:
				e.Flagged = true
			case imap.FlagDraft:
				e.Draft = true
			}
		}
		bySeq[msg.SeqNum], byUID[e.UID] = e, e
	}

	cmd := c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}})
	defer cmd.Close()
	for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
		// The UID names the message; a server that sends it after the text is matched by its
		// sequence number, and the UID checked once it comes.
		e, known := bySeq[msg.SeqNum]
		var said, wrote uint32
		for item := msg.Next(); item != nil; item = msg.Next() {
			switch it := item.(type) {
			case imapclient.FetchItemDataUID:
				said = uint32(it.UID)
				if wrote == 0 {
					e, known = byUID[said]
				}
			case imapclient.FetchItemDataBodySection:
				if !known || it.Literal == nil {
					continue
				}
				if err := each(e, it.Literal); err != nil {
					return err
				}
				wrote = e.UID
			}
		}
		if wrote != 0 && said != 0 && said != wrote {
			return errShifted
		}
	}
	return cmd.Close()
}

// Discard takes messages out of a mailbox named under uidValidity: moved to another, Trash, when
// to names one, and deleted for good when it does not; marked read first when read is set. It
// says the UIDs it took, those still there. A move needs MOVE or UIDPLUS, and deleting for good
// UIDPLUS, as DeleteOld does.
func (m *Mirror) Discard(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity uint32, uids []uint32, to string, read bool) ([]uint32, error) {
	var took []uint32
	err := m.change(ctx, t, func(f *fetcher) error {
		if _, _, err := f.open(mailbox, true, true); err != nil {
			return err
		}
		if f.uidValidity != uidValidity {
			return ErrGone
		}
		set := imap.UIDSet{}
		for _, uid := range uids {
			set.AddNum(imap.UID(uid))
		}
		found, err := f.session.client.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{set}}, f.session.all()).Wait()
		if err != nil {
			return refused(err)
		}
		there := found.AllUIDs()
		if len(there) == 0 {
			return nil
		}
		if err := takeOut(f, there, to, read); err != nil {
			return err
		}
		for _, uid := range there {
			took = append(took, uint32(uid))
		}
		return nil
	})
	return took, err
}
