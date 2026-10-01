package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"emguio/internal/store"
)

// Mirror is what the API asks of the sync: to look again now, and to fetch one message. An
// interface so tests run without one.
type Mirror interface {
	// Reconcile re-reads the set of email configs, after one is added, changed or removed.
	Reconcile()
	// Refresh looks at every mailbox of one email config now.
	Refresh(id string)
	// Raw is one message as the server holds it, or mirror.ErrGone.
	Raw(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32) ([]byte, error)
	// SetSeen marks one message read or unread on the server, or says mirror.ErrGone.
	SetSeen(ctx context.Context, t store.SyncTarget, mailbox string, uidValidity, uid uint32, seen bool) error
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

// listMessages is one run of a mailbox, newest first. limit is at most store.PageMax.
func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > store.PageMax {
			refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("limit is a number from 1 to %d.", store.PageMax))
			return
		}
		limit = n
	}
	page, err := s.store.Messages(r.Context(), userOf(r).ID, r.PathValue("id"), r.PathValue("mailbox"),
		r.URL.Query().Get("cursor"), limit)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]messageJSON, len(page.Messages))
	for i, m := range page.Messages {
		out[i] = messageOut(m)
	}
	body := map[string]any{"messages": out}
	if page.Next != "" {
		body["next_cursor"] = page.Next
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
		ID:             m.ID,
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
