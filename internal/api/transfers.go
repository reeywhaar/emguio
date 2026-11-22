package api

import (
	"net/http"

	"emguio/internal/store"
)

type transferJSON struct {
	ID          string `json:"id"`
	EmailConfig string `json:"email_config"`
	Mailbox     string `json:"mailbox"`
	// TargetConfig and Target are the account and mailbox the messages go to.
	TargetConfig string `json:"target_config"`
	Target       string `json:"target"`
	Total        int    `json:"total"`
	Moved        int    `json:"moved"`
	// State is moving, done, cancelled or stopped.
	State string `json:"state"`
	// Error is the sentence a server stopped it with.
	Error     string `json:"error"`
	CreatedAt int64  `json:"created_at"`
}

func transferOut(t *store.Transfer) transferJSON {
	return transferJSON{
		ID: t.ID, EmailConfig: t.EmailConfigID, Mailbox: t.MailboxID, TargetConfig: t.TargetConfigID,
		Target: t.TargetMailboxID, Total: t.Total, Moved: t.Moved, State: t.State(), Error: t.Error,
		CreatedAt: t.CreatedAt.Unix(),
	}
}

// listTransfers is the user's moves of messages to another of their accounts, moving and
// finished, oldest first: how far each has got. See docs/reading.md.
func (s *Server) listTransfers(w http.ResponseWriter, r *http.Request) {
	ts, err := s.store.Transfers(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]transferJSON, len(ts))
	for i, t := range ts {
		out[i] = transferOut(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": out})
}

// cancelTransfer leaves where they are the messages of a transfer still waiting. What has moved
// stays moved, and the message under way finishes.
func (s *Server) cancelTransfer(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	t, err := s.store.CancelTransfer(r.Context(), u.ID, r.PathValue("transfer"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("transfer cancelled", "user", u.ID, "transfer", t.ID, "moved", t.Moved, "total", t.Total)
	s.store.Notify(u.ID)
	writeJSON(w, http.StatusOK, transferOut(t))
}

// retryTransfer sends again the messages a stopped or cancelled transfer left where they were.
func (s *Server) retryTransfer(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	t, err := s.store.Transfer(r.Context(), u.ID, r.PathValue("transfer"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for id, off := range map[string]string{
		t.EmailConfigID:  offSentence,
		t.TargetConfigID: "The account it would go to is turned off. Turn it on in Settings first.",
	} {
		c, err := s.store.EmailConfig(r.Context(), u.ID, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if c.Off {
			refuse(w, http.StatusConflict, CodeConflict, off)
			return
		}
	}
	if t, err = s.store.RetryTransfer(r.Context(), u.ID, t.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.mirror != nil {
		s.mirror.Kick(t.EmailConfigID)
	}
	s.log.Info("transfer tried again", "user", u.ID, "transfer", t.ID, "moved", t.Moved, "total", t.Total)
	s.store.Notify(u.ID)
	writeJSON(w, http.StatusOK, transferOut(t))
}

// dismissTransfer lets a finished transfer go, once it has been shown.
func (s *Server) dismissTransfer(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DismissTransfer(r.Context(), userOf(r).ID, r.PathValue("transfer")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
