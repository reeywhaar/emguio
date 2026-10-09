package mirror

import (
	"context"
	"errors"
	"net/mail"
	"time"

	"emguio/internal/compose"
	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/store"
)

// DraftDelay is how long after a change a draft kept here is written to the mail server: often
// enough that little is only ever here, seldom enough that the server is not handed a new copy
// at every pause in the typing.
const DraftDelay = 30 * time.Second

// draftBackoff is how long a draft the server could not take waits for its next try.
var draftBackoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, time.Hour}

// ErrNoDrafts is an email config with no Drafts folder to write a draft into.
var ErrNoDrafts = errors.New("mirror: no drafts folder")

// WakeDrafts says a draft was saved, and may be due sooner than the writer is waiting for.
func (m *Mirror) WakeDrafts() {
	select {
	case m.drafts <- struct{}{}:
	default:
	}
}

// runDrafts writes each draft kept here when it is due, until Run ends. See docs/sending.md.
func (m *Mirror) runDrafts() {
	ctx := m.life
	for {
		d, err := m.store.NextDraft(ctx)
		var later <-chan time.Time
		switch {
		case err != nil:
			if ctx.Err() == nil {
				m.log.Error("drafts waiting to be written could not be read", "error", err.Error())
			}
			later = time.After(30 * time.Second)
		case d == nil:
		case time.Until(d.DueAt) > 0:
			later = time.After(time.Until(d.DueAt))
		default:
			if err := m.WriteDraft(ctx, d.ID); err != nil {
				// Recorded with its next try; a pause all the same, should the record have failed.
				later = time.After(time.Second)
				break
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-m.drafts:
		case <-later:
		}
	}
}

// WriteDraft writes a draft kept here to its config's Drafts, in place of the copy written
// before, and says why not when the server would not take it. One at a time: a draft written
// twice at once would leave two copies there.
func (m *Mirror) WriteDraft(ctx context.Context, id string) error {
	m.drafting.Lock()
	defer m.drafting.Unlock()
	ctx, cancel := context.WithTimeout(ctx, JobTimeout)
	defer cancel()
	d, err := m.store.MirrorDraft(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		// Sent or discarded meanwhile.
		return nil
	}
	if err != nil {
		return err
	}
	if d.Written == d.Version {
		return m.store.DraftWritten(ctx, d.ID, d.Version, d.KeptMailbox, d.KeptMessage, time.Time{})
	}
	err = m.writeDraft(ctx, d)
	whose := []any{"user", d.UserID, "email_config", d.EmailConfigID, "draft", d.ID}
	if err == nil {
		m.log.Info("draft written to Drafts", whose...)
	}
	if err == nil || ctx.Err() != nil {
		return err
	}
	sentence, final := DraftProblem(err)
	again := time.Time{}
	attrs := append(whose, "why", sentence, "error", err.Error())
	if final {
		attrs = append(attrs, "retry", "no; it stays here until it is changed or sent")
	} else {
		wait := draftBackoff[min(d.Attempts, len(draftBackoff)-1)]
		again = time.Now().Add(wait)
		attrs = append(attrs, "retry_in", wait.String())
	}
	m.log.Warn("draft not written to Drafts", attrs...)
	if rerr := m.store.DraftFailed(ctx, d.ID, sentence, again); rerr != nil {
		m.log.Error("a draft's failure to be written could not be recorded", append(whose, "error", rerr.Error())...)
	}
	m.store.Notify(d.UserID)
	return err
}

func (m *Mirror) writeDraft(ctx context.Context, d *store.Draft) error {
	t, err := m.store.JobTarget(ctx, d.EmailConfigID)
	if err != nil {
		return err
	}
	c, err := m.store.EmailConfig(ctx, d.UserID, d.EmailConfigID)
	if err != nil {
		return err
	}
	boxes, err := m.store.MirrorMailboxes(ctx, d.EmailConfigID)
	if err != nil {
		return err
	}
	var drafts *store.Mailbox
	for _, mb := range boxes {
		if mb.SpecialUse == store.UseDrafts && mb.Selectable {
			drafts = mb
		}
	}
	if drafts == nil {
		return ErrNoDrafts
	}
	parts, err := m.store.DraftParts(ctx, d.ID, true)
	if err != nil {
		return err
	}
	msg := compose.Mail{
		From:       mail.Address{Name: c.SenderName, Address: c.Email},
		Subject:    d.Subject,
		Text:       d.Text,
		InReplyTo:  d.InReplyTo,
		References: d.References,
		Date:       m.store.Now(),
		Draft:      true,
	}
	msg.SetAddresses(d.To, d.Cc, d.Bcc)
	for _, p := range parts {
		msg.Attachments = append(msg.Attachments, compose.Attachment{Name: p.Name, Type: p.Type, Data: p.Data})
	}
	built, err := compose.Build(msg)
	if err != nil {
		return err
	}
	var replaces *Located
	if d.KeptMessage != "" {
		mb, err := m.store.MirrorMailbox(ctx, d.KeptMailbox)
		uidValidity, uid, ok := ids.ParseMessage(d.KeptMessage)
		if err == nil && ok {
			replaces = &Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
		}
	}
	uidValidity, uid, err := m.SaveDraft(ctx, t, drafts.Name, built.Raw, replaces)
	if err != nil {
		return err
	}
	if err := m.store.DraftWritten(ctx, d.ID, d.Version, drafts.ID, ids.Message(uidValidity, uid), time.Now().Add(DraftDelay)); err != nil {
		return err
	}
	m.store.Notify(d.UserID)
	m.Refresh(d.EmailConfigID)
	return nil
}

// DraftProblem is why a draft could not be written, in a sentence, and whether trying again
// would change nothing.
func DraftProblem(err error) (string, bool) {
	var f *connect.Failure
	switch {
	case errors.Is(err, ErrNoDrafts):
		return "This mail account has no Drafts folder to keep drafts in.", true
	case errors.Is(err, ErrUnsupported):
		return "This mail server cannot keep drafts: it has no UIDPLUS.", true
	case errors.As(err, &f):
		return f.Sentence, false
	default:
		sentence, _ := explain(err)
		return sentence, false
	}
}

// HoldDraft stops a draft being written while it is sent, once a write under way is done, and
// says it as it is then, with its parts.
func (m *Mirror) HoldDraft(ctx context.Context, userID, id string) (*store.Draft, []store.DraftPart, error) {
	m.drafting.Lock()
	defer m.drafting.Unlock()
	d, err := m.store.HoldDraft(ctx, userID, id)
	if err != nil {
		return nil, nil, err
	}
	parts, err := m.store.DraftParts(ctx, id, true)
	return d, parts, err
}

// ForgetDraft removes a draft from here once a write under way is done, and says where the mail
// server holds it, for that copy to go too.
func (m *Mirror) ForgetDraft(ctx context.Context, userID, id string) (*store.Draft, error) {
	m.drafting.Lock()
	defer m.drafting.Unlock()
	return m.store.DeleteDraft(ctx, userID, id)
}
