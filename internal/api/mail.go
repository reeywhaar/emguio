package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emguio/internal/ids"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// Mirror is what the API asks of the sync: to look again now, and whatever is not kept. An
// interface so tests run without a mail server.
type Mirror interface {
	// Reconcile re-reads the set of email configs, after one is added, changed or removed.
	Reconcile()
	// Refresh looks at every mailbox of one email config now.
	Refresh(id string)
	// List is a run of a mailbox's messages from the server, newest first; with q, of those
	// the server finds for it.
	List(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, before uint32, limit int, q string) (*mirror.Listing, error)
	// Read is one message as the server holds it, or mirror.ErrGone.
	Read(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) (*mirror.Opened, error)
	// Part is one part of a message, its MIME header and its body, or mirror.ErrGone.
	Part(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32, section []int) ([]byte, []byte, error)
	// Kick says an email config has a job waiting.
	Kick(configID string)
	// Origin is what a reply needs of the message it answers, or mirror.ErrGone.
	Origin(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) (*mirror.Origin, error)
	// Sent files a sent message in Sent and marks what it answers, in the background.
	Sent(t store.SyncTarget, s mirror.Sending)
	// WakeDrafts says a draft was saved; WriteDraft writes one to the mail server now.
	WakeDrafts()
	WriteDraft(ctx context.Context, id string) error
	// HoldDraft keeps a draft from being written while it is sent; ForgetDraft removes it.
	HoldDraft(ctx context.Context, userID, id string) (*store.Draft, []store.DraftPart, error)
	ForgetDraft(ctx context.Context, userID, id string) (*store.Draft, error)
}

// targetOf is an email config as the mirror is asked about it.
func targetOf(u *store.User, c *store.EmailConfig) store.SyncTarget {
	return store.SyncTarget{ID: c.ID, UserID: u.ID, UpdatedAt: c.UpdatedAt}
}

// mailboxOf is the email config and the mailbox a request names, both the user's. It writes the
// refusal itself and reports false when there is nothing to go on with.
func (s *Server) mailboxOf(w http.ResponseWriter, r *http.Request) (*store.EmailConfig, *store.Mailbox, bool) {
	u := userOf(r)
	c, err := s.store.EmailConfig(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	mb, err := s.store.Mailbox(r.Context(), u.ID, c.ID, r.PathValue("mailbox"))
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	return c, mb, true
}

type mailboxJSON struct {
	ID string `json:"id"`
	// Name is the server's, and Path is it split into the tree it describes.
	Name       string   `json:"name"`
	Path       []string `json:"path"`
	SpecialUse string   `json:"special_use"`
	Selectable bool     `json:"selectable"`
	Messages   uint32   `json:"messages"`
	Unseen     uint32   `json:"unseen"`
	// UIDNext moves on with each message put in it, so a list can tell it changed when its
	// counts did not.
	UIDNext uint32 `json:"uid_next"`
}

func (s *Server) listMailboxes(w http.ResponseWriter, r *http.Request) {
	boxes, err := s.store.Mailboxes(r.Context(), userOf(r).ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]mailboxJSON, len(boxes))
	for i, mb := range boxes {
		out[i] = mailboxJSON{
			ID:         mb.ID,
			Name:       mb.Name,
			Path:       mb.Path(),
			SpecialUse: mb.SpecialUse,
			Selectable: mb.Selectable,
			Messages:   mb.Messages,
			Unseen:     mb.Unseen,
			UIDNext:    mb.UIDNext,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mailboxes": out})
}

type messageJSON struct {
	ID   string          `json:"id"`
	From store.Address   `json:"from"`
	To   []store.Address `json:"to"`
	// Subject is empty when the message has none.
	Subject string `json:"subject"`
	// Date is when the message arrived, which is what the list is ordered by. The sender's own
	// claim is Sent, null when the message makes none.
	Date           int64  `json:"date"`
	Sent           *int64 `json:"sent"`
	Seen           bool   `json:"seen"`
	Flagged        bool   `json:"flagged"`
	Answered       bool   `json:"answered"`
	Draft          bool   `json:"draft"`
	HasAttachments bool   `json:"has_attachments"`
	Preview        string `json:"preview"`
}

// How long a run of a list is, unless the request says, and at most.
const (
	pageSize = 50
	pageMax  = 200
)

// queryMax bounds what a mailbox is searched for, in bytes: a line typed into a field.
const queryMax = 500

// listMessages is one run of a mailbox, newest first. INBOX opens on the window kept of it, and
// everything past it, like every other mailbox, comes from the server.
//
// With q, the run is what the mail server finds in the mailbox for it, INBOX's included: nothing
// is kept to search here. See docs/reading.md.
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > queryMax {
		refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("Search for at most %d characters.", queryMax))
		return
	}
	limit := pageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > pageMax {
			refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("limit is a number from 1 to %d.", pageMax))
			return
		}
		limit = n
	}
	var uidValidity, before uint32
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		var ok bool
		if uidValidity, before, ok = ids.ParseMessage(cursor); !ok {
			refuse(w, http.StatusBadRequest, CodeInvalid, "That cursor is not one this gave out. Start the list again.")
			return
		}
	}
	c, mb, ok := s.mailboxOf(w, r)
	if !ok {
		return
	}

	var (
		msgs []*store.Message
		more bool
	)
	if q == "" && before == 0 && mb.SpecialUse == store.UseInbox && mb.SyncedAt != nil {
		window, err := s.store.Window(r.Context(), mb)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		msgs, more = window, len(window) > 0 && int(mb.Messages) > len(window)
	} else {
		if s.mirror == nil {
			refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
			return
		}
		listing, err := s.mirror.List(r.Context(), targetOf(userOf(r), c), mb.Name, uidValidity, before, limit, q)
		if !s.serverError(w, r, c.ID, err, "This folder has changed on the server since the list was opened. Open it again.") {
			return
		}
		for _, h := range listing.Headers {
			msgs = append(msgs, &store.Message{Header: h, UIDValidity: listing.UIDValidity})
		}
		more = listing.More
	}

	out := make([]messageJSON, len(msgs))
	for i, m := range msgs {
		out[i] = messageOut(m)
	}
	body := map[string]any{"messages": out}
	if more && len(msgs) > 0 {
		body["next_cursor"] = msgs[len(msgs)-1].ID()
	}
	writeJSON(w, http.StatusOK, body)
}

// syncEmailConfig asks for a look at every mailbox now. It answers at once: what the look finds
// arrives on the event stream.
func (s *Server) syncEmailConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.EmailConfig(r.Context(), userOf(r).ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.mirror != nil {
		s.mirror.Refresh(c.ID)
	}
	w.WriteHeader(http.StatusAccepted)
}

func messageOut(m *store.Message) messageJSON {
	return messageJSON{
		ID:             m.ID(),
		From:           m.From,
		To:             nonNil(m.To),
		Subject:        m.Subject,
		Date:           m.InternalDate.Unix(),
		Sent:           unixOrNil(m.Date),
		Seen:           m.Flags.Seen,
		Flagged:        m.Flags.Flagged,
		Answered:       m.Flags.Answered,
		Draft:          m.Flags.Draft,
		HasAttachments: m.HasAttachments,
		Preview:        m.Preview,
	}
}

func unixOrNil(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	u := t.Unix()
	return &u
}

func nonNil(a []store.Address) []store.Address {
	if a == nil {
		return []store.Address{}
	}
	return a
}
