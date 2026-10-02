package api

import (
	"fmt"
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

// jobsMax bounds one request: a page of messages selected and acted on at once.
const jobsMax = 500

type jobsBody struct {
	Jobs []jobBody `json:"jobs"`
}

// postJobs takes what is to be done to messages — read or unread, starred or not, moved,
// deleted for good — and answers as soon as it is queued. The mirror does each on the server in
// the order asked, whether or not the page that asked is still open; see docs/reading.md.
//
// A list, so a selection acted on is one request. All of it is queued or none: one job that
// cannot be refuses the request.
func (s *Server) postJobs(w http.ResponseWriter, r *http.Request) {
	var body jobsBody
	if !decode(w, r, &body) {
		return
	}
	if len(body.Jobs) == 0 || len(body.Jobs) > jobsMax {
		refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("Ask for between 1 and %d jobs at once.", jobsMax))
		return
	}
	u := userOf(r)
	jobs := make([]store.Job, 0, len(body.Jobs))
	configs := map[string]*store.EmailConfig{}
	mailboxes := map[string]*store.Mailbox{}
	mailbox := func(c *store.EmailConfig, id string) (*store.Mailbox, error) {
		if mb := mailboxes[id]; mb != nil {
			return mb, nil
		}
		mb, err := s.store.Mailbox(r.Context(), u.ID, c.ID, id)
		if err == nil {
			mailboxes[id] = mb
		}
		return mb, err
	}
	for _, b := range body.Jobs {
		switch b.Kind {
		case store.JobSeen, store.JobFlagged, store.JobMove, store.JobDelete:
		default:
			refuse(w, http.StatusBadRequest, CodeInvalid, "Say what to do: seen, flagged, move or delete.")
			return
		}
		if _, _, ok := ids.ParseMessage(b.Message); !ok {
			refuse(w, http.StatusBadRequest, CodeInvalid, "That is not a message id.")
			return
		}
		c := configs[b.EmailConfig]
		if c == nil {
			var err error
			if c, err = s.store.EmailConfig(r.Context(), u.ID, b.EmailConfig); err != nil {
				s.fail(w, r, err)
				return
			}
			configs[c.ID] = c
		}
		mb, err := mailbox(c, b.Mailbox)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		j := store.Job{
			UserID: u.ID, EmailConfigID: c.ID, MailboxID: mb.ID, Message: b.Message,
			Kind: b.Kind, Value: b.Value, Seen: b.Seen,
		}
		if b.Kind == store.JobMove {
			to, err := mailbox(c, b.Target)
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
		jobs = append(jobs, j)
	}
	added, err := s.store.AddJobs(r.Context(), jobs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if s.mirror != nil {
		for id := range configs {
			s.mirror.Kick(id)
		}
	}
	out := make([]jobJSON, len(added))
	for i, j := range added {
		out[i] = jobOut(j)
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobs": out})
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
