package api

import (
	"context"
	"net/http"

	"emguio/internal/mirror"
	"emguio/internal/store"
)

type draftJSON struct {
	ID string `json:"id"`
	// Parts are the attachments it holds, in order.
	Parts []draftPartJSON `json:"parts"`
	// Problem is why it could not be written to the mail server the last time it was tried.
	Problem string `json:"problem"`
}

type draftPartJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func draftOf(d *store.Draft, parts []store.DraftPart) draftJSON {
	out := draftJSON{ID: d.ID, Parts: make([]draftPartJSON, len(parts)), Problem: d.Problem}
	for i, p := range parts {
		out.Parts[i] = draftPartJSON{ID: p.ID, Name: p.Name, Type: p.Type, Size: p.Size}
	}
	return out
}

// createDraft keeps what somebody began writing here, to be written to the config's Drafts on the
// mail server in a while: what a reply answers and the attachments it carries are read now, once.
// See docs/sending.md.
func (s *Server) createDraft(w http.ResponseWriter, r *http.Request) {
	var body sendBody
	if !decodeUpTo(w, r, &body, sendBodyMax) {
		return
	}
	u := userOf(r)
	c, err := s.store.EmailConfig(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	drafts, err := s.specialMailbox(r, c, store.UseDrafts)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if drafts == nil {
		refuse(w, http.StatusConflict, CodeConflict, "This mail account has no Drafts folder to keep drafts in.")
		return
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return
	}
	wr, ok := s.gather(w, r, c, targetOf(u, c), &body, nil, nil)
	if !ok {
		return
	}
	d := store.Draft{
		UserID:        u.ID,
		EmailConfigID: c.ID,
		DraftFields:   fieldsOf(&body),
		InReplyTo:     wr.inReplyTo,
		References:    wr.references,
	}
	if a := wr.answers; a != nil {
		d.ReplyMailbox, d.ReplyMessage = a.Mailbox.ID, body.Reply.Message
	}
	if body.Draft != nil {
		d.KeptMailbox, d.KeptMessage = body.Draft.Mailbox, body.Draft.Message
	}
	parts := make([]store.DraftPart, len(wr.files))
	for i, f := range wr.files {
		parts[i] = store.DraftPart{Name: f.Name, Type: f.Type, Data: f.Data}
	}
	made, held, err := s.store.CreateDraft(r.Context(), d, parts, s.store.Now().Add(mirror.DraftDelay))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.mirror.WakeDrafts()
	if body.Close {
		s.closeDraft(w, r, made.ID)
		return
	}
	writeJSON(w, http.StatusOK, draftOf(made, held))
}

// saveDraft keeps what is written in a draft here now: its fields, the attachments it still
// holds, and new files after them. The mail server has it in a while.
func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	var body sendBody
	if !decodeUpTo(w, r, &body, sendBodyMax) {
		return
	}
	u := userOf(r)
	d, ok := s.draftIn(w, r)
	if !ok {
		return
	}
	size := 0
	parts, err := s.store.DraftParts(r.Context(), d.ID, false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sizes := map[string]int{}
	for _, p := range parts {
		sizes[p.ID] = p.Size
	}
	for _, id := range body.Parts {
		size += sizes[id]
	}
	add := make([]store.DraftPart, len(body.Attachments))
	for i, a := range body.Attachments {
		f := attachment(a.Name, a.Type, a.Data)
		add[i] = store.DraftPart{Name: f.Name, Type: f.Type, Data: f.Data}
		size += len(a.Data)
	}
	if size > attachmentsMax {
		refuse(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Attachments come to more than 25 MB, which most mail servers will not take.")
		return
	}
	saved, held, err := s.store.SaveDraft(r.Context(), u.ID, d.ID, fieldsOf(&body), body.Parts, add,
		s.store.Now().Add(mirror.DraftDelay), body.Close)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.mirror.WakeDrafts()
	if body.Close {
		s.closeDraft(w, r, saved.ID)
		return
	}
	writeJSON(w, http.StatusOK, draftOf(saved, held))
}

// closeDraft writes a draft whose window closed to the mail server now, and it goes from here.
// One the server cannot take now stays, tried again later; one that has nowhere to go is refused,
// for the writer to choose whether to discard it.
func (s *Server) closeDraft(w http.ResponseWriter, r *http.Request, id string) {
	err := s.mirror.WriteDraft(context.WithoutCancel(r.Context()), id)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"closed": true})
		return
	}
	sentence, final := mirror.DraftProblem(err)
	if final {
		refuse(w, http.StatusConflict, CodeConflict, sentence)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"closed": false, "problem": sentence})
}

// deleteDraft discards a draft kept here, and says where the mail server holds its copy, for the
// writer to have it deleted there too.
func (s *Server) deleteDraft(w http.ResponseWriter, r *http.Request) {
	d, ok := s.draftIn(w, r)
	if !ok {
		return
	}
	d, err := s.mirror.ForgetDraft(r.Context(), userOf(r).ID, d.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var kept *messageRef
	if d.KeptMessage != "" {
		kept = &messageRef{Mailbox: d.KeptMailbox, Message: d.KeptMessage}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kept": kept})
}

// draftIn is the draft the path names, in the email config it names.
func (s *Server) draftIn(w http.ResponseWriter, r *http.Request) (*store.Draft, bool) {
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return nil, false
	}
	d, err := s.store.Draft(r.Context(), userOf(r).ID, r.PathValue("draft"))
	if err == nil && d.EmailConfigID != r.PathValue("id") {
		err = store.NotFound("This draft is no longer here.")
	}
	if err != nil {
		s.fail(w, r, err)
		return nil, false
	}
	return d, true
}

func fieldsOf(b *sendBody) store.DraftFields {
	return store.DraftFields{To: b.To, Cc: b.Cc, Bcc: b.Bcc, Subject: b.Subject, Text: b.Text}
}
