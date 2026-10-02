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
	// Forward is the message this passes on, and which of its parts go with it.
	Forward *forwardRef `json:"forward"`
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

type forwardRef struct {
	messageRef
	// Parts are the sections of its attachments that go on with it.
	Parts []string `json:"parts"`
}

// sendMessage sends what somebody wrote from one of their email configs, through its outgoing
// server, and answers once the server has taken it. A copy is filed in Sent, and a reply's
// original marked answered, after that. See docs/sending.md.
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
		refuse(w, http.StatusConflict, CodeConflict, "This email config has no outgoing server. Add one in its settings to send from it.")
		return
	}

	m := compose.Mail{
		From:    mail.Address{Name: c.SenderName, Address: c.Email},
		Subject: strings.Join(strings.Fields(body.Subject), " "),
		Text:    body.Text,
		Date:    s.store.Now(),
	}
	for _, f := range []struct {
		name  string
		typed string
		into  *[]mail.Address
	}{{"To", body.To, &m.To}, {"Cc", body.Cc, &m.Cc}, {"Bcc", body.Bcc, &m.Bcc}} {
		if *f.into, err = compose.Addresses(f.name, f.typed); err != nil {
			refuse(w, http.StatusBadRequest, CodeInvalid, err.Error())
			return
		}
	}
	if len(m.To)+len(m.Cc)+len(m.Bcc) == 0 {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say who it goes to.")
		return
	}

	size := 0
	for _, a := range body.Attachments {
		m.Attachments = append(m.Attachments, attachment(a.Name, a.Type, a.Data))
		size += len(a.Data)
	}
	var answers *mirror.Answering
	if body.Reply != nil || body.Forward != nil {
		if s.mirror == nil {
			refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
			return
		}
	}
	if ref := body.Reply; ref != nil {
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, ref)
		if !ok {
			return
		}
		origin, err := s.mirror.Origin(r.Context(), t, mb.Name, uidValidity, uid)
		switch {
		case err == nil:
			m.InReplyTo = origin.MessageID
			m.References = origin.References
			if origin.MessageID != "" {
				m.References = append(m.References, origin.MessageID)
			}
			answers = &mirror.Answering{Mailbox: mb, UIDValidity: uidValidity, UID: uid}
		// Gone since it was opened: the reply still says what it says, unthreaded.
		case errors.Is(err, mirror.ErrGone):
		default:
			s.serverError(w, r, c.ID, err, "")
			return
		}
	}
	if ref := body.Forward; ref != nil {
		mb, uidValidity, uid, ok := s.messageOf(w, r, c, &ref.messageRef)
		if !ok {
			return
		}
		for _, at := range ref.Parts {
			section, ok := sectionOf(at)
			if !ok {
				refuse(w, http.StatusBadRequest, CodeInvalid, "A part is named by its section, like 2 or 1.3.")
				return
			}
			head, raw, err := s.mirror.Part(r.Context(), t, mb.Name, uidValidity, uid, section)
			if !s.serverError(w, r, c.ID, err, "The message it forwards is no longer on the server.") {
				return
			}
			p, data, err := message.DecodePart(head, raw, at)
			if err != nil {
				refuse(w, http.StatusUnprocessableEntity, CodeUnreadable, "One of the parts it forwards could not be read.")
				return
			}
			m.Attachments = append(m.Attachments, attachment(p.Name, p.Type, data))
			size += len(data)
		}
	}
	if size > attachmentsMax {
		refuse(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "Attachments come to more than 25 MB, which most mail servers will not take.")
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
		sent, err := s.sentMailbox(r, c)
		if err != nil {
			s.log.Warn("could not find Sent", "email_config", c.ID, "err", err)
		}
		s.mirror.Sent(t, mirror.Sending{Sent: sent, ID: built.ID, Raw: built.Raw, Answers: answers})
	}
	writeJSON(w, http.StatusOK, map[string]any{"message_id": built.ID})
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

// sentMailbox is where c's sent mail is filed, or nil when it has nowhere.
func (s *Server) sentMailbox(r *http.Request, c *store.EmailConfig) (*store.Mailbox, error) {
	boxes, err := s.store.Mailboxes(r.Context(), userOf(r).ID, c.ID)
	if err != nil {
		return nil, err
	}
	for _, mb := range boxes {
		if mb.SpecialUse == store.UseSent && mb.Selectable {
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
