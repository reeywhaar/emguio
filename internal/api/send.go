package api

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"
	"net/mail"
	"strings"

	"emguio/internal/compose"
	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/message"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

// How much a message may carry: attachments as most servers take them, and the request that
// brings them as base64 in JSON, a third larger, with room for the text.
const (
	attachmentsMax = 25 << 20
	sendBodyMax    = 36 << 20
)

// sendBody is what somebody wrote, to send or to keep as a draft.
type sendBody struct {
	// To, Cc and Bcc are as typed: addresses apart by commas, each with a name or without.
	To      string `json:"to"`
	Cc      string `json:"cc"`
	Bcc     string `json:"bcc"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	// Attachments are files from the writer's own device.
	Attachments []attachmentBody `json:"attachments"`
	// Reply is the message this answers: it is threaded under it, and marked answered.
	Reply *messageRef `json:"reply"`
	// Carry is a message on the mail server some of whose parts go with this one: the one it
	// forwards, or the draft it was opened from.
	Carry *carryRef `json:"carry"`
	// Draft is a draft on the mail server this one was opened from, which it replaces.
	Draft *messageRef `json:"draft"`
	// DraftID is the draft kept here that this is, and Parts the attachments it holds that go
	// with it, by id and in order, before the new files.
	DraftID string   `json:"draft_id"`
	Parts   []string `json:"parts"`
	// Close is a draft's window closing: it is written to the mail server now.
	Close bool `json:"close"`
}

type attachmentBody struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Data is the file, base64 in the JSON.
	Data []byte `json:"data"`
}

type messageRef struct {
	Mailbox string `json:"mailbox"`
	Message string `json:"message"`
}

type carryRef struct {
	messageRef
	// Parts are the sections of its attachments that go on with it.
	Parts []string `json:"parts"`
}

// sendMessage sends what somebody wrote from one of their email configs, through its outgoing
// server, and answers once the server has taken it. A copy is filed in Sent, a reply's original
// marked answered and its draft deleted, after that. See docs/sending.md.
func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
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
	t := targetOf(u, c)
	logins, err := s.store.TargetLogins(r.Context(), t)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if logins.Outgoing == nil {
		refuse(w, http.StatusConflict, CodeConflict, "This mail account has no outgoing server. Add one in its settings to send from it.")
		return
	}

	// A draft kept here is not written to the mail server while it is being sent, and is again
	// should the sending not go.
	var (
		held  *store.Draft
		parts []store.DraftPart
		gone  bool
	)
	if body.DraftID != "" {
		if s.mirror == nil {
			refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
			return
		}
		if held, parts, err = s.mirror.HoldDraft(r.Context(), u.ID, body.DraftID); err != nil {
			s.fail(w, r, err)
			return
		}
		defer func() {
			if !gone {
				s.store.DueDraft(context.WithoutCancel(r.Context()), held.ID, s.store.Now().Add(mirror.DraftDelay))
				s.mirror.WakeDrafts()
			}
		}()
		if held.EmailConfigID != c.ID {
			s.fail(w, r, store.NotFound("This draft is no longer here."))
			return
		}
	}
	wr, ok := s.gather(w, r, c, t, &body, held, parts)
	if !ok {
		return
	}
	m := compose.Mail{
		From:        mail.Address{Name: c.SenderName, Address: c.Email},
		Subject:     body.Subject,
		Text:        body.Text,
		Attachments: wr.files,
		InReplyTo:   wr.inReplyTo,
		References:  wr.references,
		Date:        s.store.Now(),
	}
	if err := m.SetAddresses(body.To, body.Cc, body.Bcc); err != nil {
		refuse(w, http.StatusBadRequest, CodeInvalid, err.Error())
		return
	}
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say who it goes to.")
		return
	}
	built, err := compose.Build(m)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := logins.Outgoing
	// Not the request's context: a tab closed while the server is still taking the message
	// leaves the sending to finish, rather than cutting it off halfway.
	err = s.connector.Send(context.WithoutCancel(r.Context()), connect.Server{
		Host: out.Host, Port: out.Port, TLS: out.TLS, Username: out.Username, Password: out.Password,
	}, c.Email, built.Recipients, bytes.NewReader(built.Raw))
	whose := []any{"user", u.ID, "email_config", c.ID, "host", out.Host}
	var f *connect.Failure
	switch {
	case errors.As(err, &f):
		s.log.Warn("message not sent", append(whose, "why", f.Sentence)...)
		refuse(w, http.StatusBadGateway, CodeUnreachable, f.Sentence)
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	s.log.Info("message sent", append(whose, "recipients", len(built.Recipients),
		"attachments", len(wr.files), "bytes", len(built.Raw))...)

	gone = true
	if s.mirror != nil {
		if held != nil {
			if _, err := s.mirror.ForgetDraft(context.WithoutCancel(r.Context()), u.ID, held.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				s.log.Warn("a sent message's draft could not be forgotten here", "user", u.ID, "draft", held.ID, "error", err.Error())
			}
		}
		sent, err := s.specialMailbox(r, c, store.UseSent)
		if err != nil {
			s.log.Warn("Sent could not be found, so the message is not filed there", "user", u.ID, "email_config", c.ID, "error", err.Error())
		}
		s.mirror.Sent(t, mirror.Sending{Sent: sent, ID: built.ID, Raw: built.Raw, Answers: wr.answers, Draft: wr.replaces})
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": built.ID})
}

// written is what a message needs beyond its fields: what threads it, the message it answers,
// the copy on the mail server it replaces, and its files.
type written struct {
	inReplyTo  string
	references []string
	answers    *mirror.Located
	replaces   *mirror.Located
	files      []compose.Attachment
}

// gather reads what a body names beyond its fields, for a message from c: held is the draft kept
// here it is, with its parts, or nil. Files go in the order they are carried: those held here,
// then those from a message on the mail server, then new ones.
func (s *Server) gather(w http.ResponseWriter, r *http.Request, c *store.EmailConfig, t store.SyncTarget, body *sendBody, held *store.Draft, parts []store.DraftPart) (*written, bool) {
	out := &written{}
	if (body.Reply != nil || body.Carry != nil || body.Draft != nil) && s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return nil, false
	}

	switch {
	case held != nil && held.KeptMessage != "":
		out.replaces = s.located(r, c, held.KeptMailbox, held.KeptMessage)
	case body.Draft != nil:
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, body.Draft)
		if !ok {
			return nil, false
		}
		out.replaces = &mirror.Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
	}

	// A reply is threaded under what it answers; a draft, as it was when it began.
	switch {
	case body.Reply != nil:
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, body.Reply)
		if !ok {
			return nil, false
		}
		origin, err := s.mirror.Origin(r.Context(), t, mb.Name, uidValidity, uid)
		switch {
		case err == nil:
			out.inReplyTo, out.references = origin.MessageID, origin.References
			if origin.MessageID != "" {
				out.references = append(out.references, origin.MessageID)
			}
			out.answers = &mirror.Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
		// Gone since it was opened: what was written still says what it says, unthreaded.
		case errors.Is(err, mirror.ErrGone):
		default:
			s.serverError(w, r, c.ID, err, "")
			return nil, false
		}
	case held != nil:
		out.inReplyTo, out.references = held.InReplyTo, held.References
		if held.ReplyMessage != "" {
			out.answers = s.located(r, c, held.ReplyMailbox, held.ReplyMessage)
		}
	case body.Draft != nil:
		origin, err := s.mirror.Origin(r.Context(), t, out.replaces.Mailbox.Name, out.replaces.UIDValidity, out.replaces.UID)
		switch {
		case err == nil:
			out.inReplyTo, out.references = origin.InReplyTo, origin.References
		case errors.Is(err, mirror.ErrGone):
		default:
			s.serverError(w, r, c.ID, err, "")
			return nil, false
		}
	}

	size := 0
	byID := map[string]store.DraftPart{}
	for _, p := range parts {
		byID[p.ID] = p
	}
	for _, id := range body.Parts {
		p, ok := byID[id]
		if !ok {
			refuse(w, http.StatusBadRequest, CodeInvalid, "One of its attachments is no longer here.")
			return nil, false
		}
		out.files = append(out.files, compose.Attachment{Name: p.Name, Type: p.Type, Data: p.Data})
		size += len(p.Data)
	}
	if ref := body.Carry; ref != nil {
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, &ref.messageRef)
		if !ok {
			return nil, false
		}
		for _, at := range ref.Parts {
			section, ok := sectionOf(at)
			if !ok {
				refuse(w, http.StatusBadRequest, CodeInvalid, "A part is named by its section, like 2 or 1.3.")
				return nil, false
			}
			head, raw, err := s.mirror.Part(r.Context(), t, mb.Name, uidValidity, uid, section)
			if !s.serverError(w, r, c.ID, err, "The message its attachments come from is no longer on the server.") {
				return nil, false
			}
			p, data, err := message.DecodePart(head, raw, at)
			if err != nil {
				refuse(w, http.StatusUnprocessableEntity, CodeUnreadable, "One of its attachments could not be read.")
				return nil, false
			}
			out.files = append(out.files, attachment(p.Name, p.Type, data))
			size += len(data)
		}
	}
	for _, a := range body.Attachments {
		out.files = append(out.files, attachment(a.Name, a.Type, a.Data))
		size += len(a.Data)
	}
	if size > attachmentsMax {
		refuse(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Attachments come to more than 25 MB, which most mail servers will not take.")
		return nil, false
	}
	return out, true
}

// located is a message named by its mailbox's id and its own, or nil once that mailbox is gone.
func (s *Server) located(r *http.Request, c *store.EmailConfig, mailbox, message string) *mirror.Located {
	mb, err := s.store.Mailbox(r.Context(), userOf(r).ID, c.ID, mailbox)
	uidValidity, uid, ok := ids.ParseMessage(message)
	if err != nil || !ok {
		return nil
	}
	return &mirror.Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
}

// messageOf is the message a reply or a forward names, in one of c's mailboxes.
func (s *Server) messageOf(w http.ResponseWriter, r *http.Request, c *store.EmailConfig, ref *messageRef) (*store.Mailbox, uint32, uint32, bool) {
	mb, err := s.store.Mailbox(r.Context(), userOf(r).ID, c.ID, ref.Mailbox)
	if err != nil {
		s.fail(w, r, err)
		return nil, 0, 0, false
	}
	uidValidity, uid, ok := ids.ParseMessage(ref.Message)
	if !ok {
		refuse(w, http.StatusBadRequest, CodeInvalid, "That is not a message id.")
		return nil, 0, 0, false
	}
	return mb, uidValidity, uid, true
}

// specialMailbox is c's mailbox for a special use — where sent mail is filed, say — or nil when
// it has none.
func (s *Server) specialMailbox(r *http.Request, c *store.EmailConfig, use string) (*store.Mailbox, error) {
	boxes, err := s.store.Mailboxes(r.Context(), userOf(r).ID, c.ID)
	if err != nil {
		return nil, err
	}
	for _, mb := range boxes {
		if mb.SpecialUse == use && mb.Selectable {
			return mb, nil
		}
	}
	return nil, nil
}

// attachment is a file as it goes out: a name it can be saved under, and a type that is one.
func attachment(name, kind string, data []byte) compose.Attachment {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "attachment"
	}
	if _, _, err := mime.ParseMediaType(kind); err != nil {
		kind = "application/octet-stream"
	}
	return compose.Attachment{Name: name, Type: kind, Data: data}
}
