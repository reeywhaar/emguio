package api

import (
	"net/http"

	"emguio/internal/ids"
	"emguio/internal/store"
)

type jobJSON struct {
	ID          string `json:"id"`
	EmailConfig string `json:"email_config"`
	Mailbox     string `json:"mailbox"`
	Message     string `json:"message"`
	// Kind is seen, flagged, move or delete.
	Kind string `json:"kind"`
	// Value is set or cleared, for seen and flagged.
	Value bool `json:"value"`
	// Target is the mailbox a move goes to.
	Target string `json:"target"`
	// Seen is whether the message was read when asked, as the asker drew it.
	Seen bool `json:"seen"`
	// Error is the sentence a failed job ends with; empty while it waits.
	Error     string `json:"error"`
	CreatedAt int64  `json:"created_at"`
}

func jobOut(j *store.Job) jobJSON {
	return jobJSON{
		ID: j.ID, EmailConfig: j.EmailConfigID, Mailbox: j.MailboxID, Message: j.Message, Kind: j.Kind,
		Value: j.Value, Target: j.Target, Seen: j.Seen, Error: j.Error, CreatedAt: j.CreatedAt.Unix(),
	}
}

type jobBody struct {
	EmailConfig string `json:"email_config"`
	Mailbox     string `json:"mailbox"`
	Message     string `json:"message"`
	Kind        string `json:"kind"`
	Value       bool   `json:"value"`
	Target      string `json:"target"`
	Seen        bool   `json:"seen"`
}

// postJob takes something to be done to a message — read or unread, starred or not, moved,
// deleted for good — and answers as soon as it is queued. The mirror does it on the server in
// the order asked, whether or not the page that asked is still open; see docs/reading.md.
func (s *Server) postJob(w http.ResponseWriter, r *http.Request) {
	var body jobBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	switch body.Kind {
	case store.JobSeen, store.JobFlagged, store.JobMove, store.JobDelete:
	default:
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say what to do: seen, flagged, move or delete.")
		return
	}
	if _, _, ok := ids.ParseMessage(body.Message); !ok {
		refuse(w, http.StatusBadRequest, CodeInvalid, "That is not a message id.")
		return
	}
	c, err := s.store.EmailConfig(r.Context(), u.ID, body.EmailConfig)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mb, err := s.store.Mailbox(r.Context(), u.ID, c.ID, body.Mailbox)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	j := store.Job{
		UserID: u.ID, EmailConfigID: c.ID, MailboxID: mb.ID, Message: body.Message,
		Kind: body.Kind, Value: body.Value, Seen: body.Seen,
	}
	if body.Kind == store.JobMove {
		to, err := s.store.Mailbox(r.Context(), u.ID, c.ID, body.Target)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if to.ID == mb.ID || !to.Selectable {
			refuse(w, http.StatusBadRequest, CodeInvalid, "Choose another folder to move it to.")
			return
		}
		j.Target = to.ID
	}
	added, err := s.store.AddJob(r.Context(), j)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.mirror != nil {
		s.mirror.Kick(c.ID)
	}
	writeJSON(w, http.StatusAccepted, jobOut(added))
}

// listJobs is the user's jobs, waiting and failed, oldest first: what a page opened after they
// were asked for draws as still to be done, and what failed while nobody was looking.
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.store.Jobs(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]jobJSON, len(jobs))
	for i, j := range jobs {
		out[i] = jobOut(j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
}

// dismissJob lets a failed job go, once its sentence has been shown.
func (s *Server) dismissJob(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DismissJob(r.Context(), userOf(r).ID, r.PathValue("job")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
