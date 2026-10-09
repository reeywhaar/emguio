package api

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strings"

	"emguio/internal/ids"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// threadsAsked bounds how many messages one ask for conversation counts names: a page of a list.
const threadsAsked = 200

// threadCounts says, of some of a mailbox's messages, how many messages each is in a conversation
// with there, for those not alone: a list asks for its rows a page at a time, after showing them.
// Nothing on a server without THREAD. See docs/reading.md.
func (s *Server) threadCounts(w http.ResponseWriter, r *http.Request) {
	c, mb, ok := s.mailboxOf(w, r)
	if !ok {
		return
	}
	asked := strings.Split(r.URL.Query().Get("messages"), ",")
	if len(asked) > threadsAsked {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Ask about at most 200 messages at once.")
		return
	}
	counts := map[string]int{}
	if s.mirror == nil {
		writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
		return
	}
	th, err := s.mirror.Threads(r.Context(), targetOf(userOf(r), c), mb.Name)
	if errors.Is(err, mirror.ErrUnsupported) {
		writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
		return
	}
	if !s.serverError(w, r, c.ID, err, "This folder is no longer on the server.") {
		return
	}
	for _, id := range asked {
		uidValidity, uid, ok := ids.ParseMessage(id)
		if !ok || uidValidity != th.UIDValidity {
			continue
		}
		if of := th.Of(uid); of != nil {
			counts[id] = len(of)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": counts})
}

// conversationMax bounds how many of a mailbox's messages one conversation shows: the newest.
const conversationMax = 100

type conversationJSON struct {
	messageJSON
	Mailbox string `json:"mailbox"`
}

// conversation is the conversation a message is in, oldest first: the messages of its mailbox the
// server threads it with, and those of Sent — of INBOX, for one in Sent — that answer one of them
// or that one of them answers. Empty on a server without THREAD.
func (s *Server) conversation(w http.ResponseWriter, r *http.Request) {
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	t := targetOf(userOf(r), c)
	th, err := s.mirror.Threads(r.Context(), t, mb.Name)
	if errors.Is(err, mirror.ErrUnsupported) {
		writeJSON(w, http.StatusOK, map[string]any{"messages": []conversationJSON{}, "earlier": 0})
		return
	}
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}
	if th.UIDValidity != uidValidity {
		s.serverError(w, r, c.ID, mirror.ErrGone, gone)
		return
	}
	members := th.Of(uid)
	if members == nil {
		members = []uint32{uid}
	}
	earlier := max(0, len(members)-conversationMax)
	members = members[earlier:]
	heads, err := s.mirror.Headers(r.Context(), t, mb.Name, uidValidity, members)
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}

	out := make([]conversationJSON, 0, len(heads))
	seen := map[string]bool{}
	add := func(h store.Header, in *store.Mailbox, uidValidity uint32) {
		id := bare(h.MessageID)
		if id != "" && seen[id] {
			return
		}
		seen[id] = true
		out = append(out, conversationJSON{messageOut(&store.Message{Header: h, UIDValidity: uidValidity}), in.ID})
	}
	var answered, answers []string
	for _, h := range heads {
		add(h, mb, uidValidity)
		if id := bare(h.MessageID); id != "" {
			answered = append(answered, id)
		}
		if id := bare(h.InReplyTo); id != "" {
			answers = append(answers, id)
		}
	}
	// The other side: what was sent in answer, or for a message in Sent, what came.
	use := store.UseSent
	if mb.SpecialUse == store.UseSent {
		use = store.UseInbox
	}
	if other, err := s.specialMailbox(r, c, use); err == nil && other != nil && other.ID != mb.ID {
		found, err := s.mirror.Related(r.Context(), t, other.Name, answered, answers)
		if err != nil {
			s.log.Warn("the other side of a conversation could not be looked for; it shows without it", "user", userOf(r).ID,
				"email_config", c.ID, "mailbox", other.ID, "error", err.Error())
		} else {
			for _, h := range found.Headers {
				add(h, other, found.UIDValidity)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b conversationJSON) int { return cmp.Compare(when(a), when(b)) })
	writeJSON(w, http.StatusOK, map[string]any{"messages": out, "earlier": earlier})
}

// bare is a message id without its angle brackets, as a header search finds it whichever way the
// header writes it.
func bare(id string) string { return strings.Trim(strings.TrimSpace(id), "<>") }

// when is when a message was written, as its sender says, or else when it arrived.
func when(m conversationJSON) int64 {
	if m.Sent != nil {
		return *m.Sent
	}
	return m.Date
}
