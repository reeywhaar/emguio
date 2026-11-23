package mirror

import (
	"context"
	"io"
	"time"

	"github.com/emersion/go-imap/v2"

	"emguio/internal/store"
)

// Appending is one message to put in a mailbox: Size bytes that Write writes, as a server takes
// them, written only once it is known the message is not there already.
type Appending struct {
	Size      int64
	MessageID string
	Flags     []imap.Flag
	// Date is when it arrived; zero for now.
	Date  time.Time
	Write func(io.Writer) error
}

// Append puts a message in a mailbox, on a session of its own for imports, unless one there has
// its Message-ID already. It says whether it did. See docs/reading.md.
func (m *Mirror) Append(ctx context.Context, t store.SyncTarget, mailbox string, a Appending) (bool, error) {
	put := false
	err := m.on(ctx, m.fetcherKeyed(t, "import"), func(f *fetcher) error {
		c := f.session.client
		if a.MessageID != "" {
			// Opened once for a run of messages into it: what each one appends is seen by the next.
			if _, _, err := f.open(mailbox, false, false); err != nil {
				return err
			}
			found, err := c.UIDSearch(&imap.SearchCriteria{
				Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: a.MessageID}},
			}, nil).Wait()
			if err != nil {
				return refused(err)
			}
			if len(found.AllUIDs()) > 0 {
				return nil
			}
		}
		cmd := c.Append(mailbox, a.Size, &imap.AppendOptions{Flags: a.Flags, Time: a.Date})
		if err := a.Write(cmd); err != nil {
			cmd.Close()
			return err
		}
		if err := cmd.Close(); err != nil {
			return refused(err)
		}
		if _, err := cmd.Wait(); err != nil {
			return refused(err)
		}
		put = true
		return nil
	})
	return put, err
}

// Go runs fn in the background for as long as the mirror runs: work a request started that
// outlives it, stopped with the mirror and waited for.
func (m *Mirror) Go(fn func(ctx context.Context)) {
	m.running.Go(func() { fn(m.life) })
}
