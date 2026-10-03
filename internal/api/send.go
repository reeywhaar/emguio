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
	// Carry is a message some of whose parts go with this one: the one it forwards, or the draft
	// it was saved as.
	Carry *carryRef `json:"carry"`
	// Draft is the draft it was saved as, which what is sent or saved now replaces.
	Draft *messageRef `json:"draft"`
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
	wr, ok := s.writtenOf(w, r, c, t, &body, false)
	if !ok {
		return
	}
	built, err := compose.Build(wr.mail)
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
	var f *connect.Failure
	switch {
	case errors.As(err, &f):
		refuse(w, http.StatusBadGateway, CodeUnreachable, f.Sentence)
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}

	if s.mirror != nil {
		sent, err := s.specialMailbox(r, c, store.UseSent)
		if err != nil {
			s.log.Warn("could not find Sent", "email_config", c.ID, "err", err)
		}
		s.mirror.Sent(t, mirror.Sending{Sent: sent, ID: built.ID, Raw: built.Raw, Answers: wr.answers, Draft: wr.draft})
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": built.ID})
}

// saveDraft keeps what somebody is writing in the config's Drafts, in place of the draft it was
// saved as before, and says where the new one is and its attachments' sections, in the order
// they were carried and attached.
func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
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
	t := targetOf(u, c)
	wr, ok := s.writtenOf(w, r, c, t, &body, true)
	if !ok {
		return
	}
	built, err := compose.Build(wr.mail)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// Not the request's context: cut off between keeping the new draft and deleting the old,
	// there would be two.
	uidValidity, uid, err := s.mirror.SaveDraft(context.WithoutCancel(r.Context()), t, drafts.Name, built.Raw, wr.draft)
	if errors.Is(err, mirror.ErrUnsupported) {
		refuse(w, http.StatusConflict, CodeConflict, "This mail server cannot keep drafts: it has no UIDPLUS.")
		return
	}
	if !s.serverError(w, r, c.ID, err, "") {
		return
	}
	s.mirror.Refresh(c.ID)
	parts := built.Parts
	if parts == nil {
		parts = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mailbox": drafts.ID,
		"message": ids.Message(uidValidity, uid),
		"parts":   parts,
	})
}

// written is what somebody wrote, as a message to build, with the messages it touches.
type written struct {
	mail    compose.Mail
	answers *mirror.Located
	draft   *mirror.Located
}

// writtenOf reads a send body into a message from c. A draft keeps whatever its fields hold so
// far; a message to send is refused for a field that is not addresses, or for going to nobody.
func (s *Server) writtenOf(w http.ResponseWriter, r *http.Request, c *store.EmailConfig, t store.SyncTarget, body *sendBody, draft bool) (*written, bool) {
	out := &written{mail: compose.Mail{
		From:    mail.Address{Name: c.SenderName, Address: c.Email},
		Subject: strings.Join(strings.Fields(body.Subject), " "),
		Text:    body.Text,
		Date:    s.store.Now(),
		Draft:   draft,
	}}
	m := &out.mail
	for _, f := range []struct {
		name  string
		typed string
		into  *[]mail.Address
	}{{"To", body.To, &m.To}, {"Cc", body.Cc, &m.Cc}, {"Bcc", body.Bcc, &m.Bcc}} {
		var err error
		*f.into, err = compose.Addresses(f.name, f.typed)
		switch {
		case err != nil && draft:
			if m.Typed == nil {
				m.Typed = map[string]string{}
			}
			m.Typed[f.name] = strings.Join(strings.Fields(f.typed), " ")
		case err != nil:
			refuse(w, http.StatusBadRequest, CodeInvalid, err.Error())
			return nil, false
		}
	}
	if !draft && len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say who it goes to.")
		return nil, false
	}
	if (body.Reply != nil || body.Carry != nil || body.Draft != nil) && s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return nil, false
	}

	if ref := body.Draft; ref != nil {
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, ref)
		if !ok {
			return nil, false
		}
		out.draft = &mirror.Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
	}
	// A reply is threaded under what it answers; a draft opened again, as it was.
	threading, answering := body.Reply, true
	if threading == nil {
		threading, answering = body.Draft, false
	}
	if threading != nil {
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, threading)
		if !ok {
			return nil, false
		}
		origin, err := s.mirror.Origin(r.Context(), t, mb.Name, uidValidity, uid)
		switch {
		case err == nil && answering:
			m.InReplyTo = origin.MessageID
			m.References = origin.References
			if origin.MessageID != "" {
				m.References = append(m.References, origin.MessageID)
			}
			out.answers = &mirror.Located{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
		case err == nil:
			m.InReplyTo, m.References = origin.InReplyTo, origin.References
		// Gone since it was opened: what was written still says what it says, unthreaded.
		case errors.Is(err, mirror.ErrGone):
		default:
			s.serverError(w, r, c.ID, err, "")
			return nil, false
		}
	}

	size := 0
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
			m.Attachments = append(m.Attachments, attachment(p.Name, p.Type, data))
			size += len(data)
		}
	}
	return out, attachFiles(w, m, body, size)
}

// attachFiles adds the writer's own files after what was carried, as long as all of it is
// within what mail servers take.
func attachFiles(w http.ResponseWriter, m *compose.Mail, body *sendBody, size int) bool {
	for _, a := range body.Attachments {
		m.Attachments = append(m.Attachments, attachment(a.Name, a.Type, a.Data))
		size += len(a.Data)
	}
	if size > attachmentsMax {
		refuse(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Attachments come to more than 25 MB, which most mail servers will not take.")
		return false
	}
	return true
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
