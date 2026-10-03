// Package compose makes what somebody wrote into a message to send: headers as mail expects
// them, the text, what is attached, and what it answers. See docs/sending.md.
package compose

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jhillyerd/enmime/v2"
)

// Mail is a message as written.
type Mail struct {
	From        mail.Address
	To, Cc, Bcc []mail.Address
	Subject     string
	Text        string
	Attachments []Attachment
	// InReplyTo and References thread a reply under what it answers, in every client that
	// threads.
	InReplyTo  string
	References []string
	Date       time.Time
	// Draft is a message kept to be written on: its Bcc in a header, and it may go to nobody
	// yet.
	Draft bool
	// Typed are a draft's address fields that are not addresses yet, by header, as written.
	Typed map[string]string
}

// Attachment is a file carried with a message.
type Attachment struct {
	Name string
	Type string
	Data []byte
}

// Built is a message ready for the server: its bytes, the Message-ID it was given, and every
// address it goes to, Bcc's among them though no header names them.
type Built struct {
	Raw        []byte
	ID         string
	Recipients []string
	// Parts are the attachments' sections, in order, as IMAP numbers them.
	Parts []string
}

// Build makes m into a message.
func Build(m Mail) (*Built, error) {
	id, err := messageID(m.From.Address)
	if err != nil {
		return nil, err
	}
	b := enmime.Builder().
		From(m.From.Name, m.From.Address).
		ToAddrs(m.To).
		CCAddrs(m.Cc).
		BCCAddrs(m.Bcc).
		Subject(m.Subject).
		Date(m.Date).
		// Text lines end in CRLF on the wire, whatever the browser sent.
		Text([]byte(strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n"))).
		Header("Message-ID", id)
	if m.InReplyTo != "" {
		b = b.Header("In-Reply-To", m.InReplyTo)
	}
	if len(m.References) > 0 {
		b = b.Header("References", strings.Join(m.References, " "))
	}
	for _, a := range m.Attachments {
		b = b.AddAttachment(a.Data, a.Type, a.Name)
	}
	if m.Draft {
		// enmime wants somebody to send to, which a draft may not have yet: a Bcc, which it
		// never writes.
		b = b.BCCAddrs(append(slices.Clone(m.Bcc), mail.Address{Address: "draft@emguio.invalid"}))
	}
	part, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	if m.Draft {
		if len(m.Bcc) > 0 {
			part.Header.Set("Bcc", joined(m.Bcc))
		}
		for name, typed := range m.Typed {
			part.Header.Set(name, typed)
		}
	}
	var raw bytes.Buffer
	if err := part.Encode(&raw); err != nil {
		return nil, fmt.Errorf("compose: %w", err)
	}
	out := &Built{Raw: raw.Bytes(), ID: id}
	// After the text, in a multipart/mixed.
	for i := range m.Attachments {
		out.Parts = append(out.Parts, strconv.Itoa(i+2))
	}
	for _, list := range [][]mail.Address{m.To, m.Cc, m.Bcc} {
		for _, a := range list {
			out.Recipients = append(out.Recipients, a.Address)
		}
	}
	return out, nil
}

// joined is addresses as a header holds them, names encoded where they need it.
func joined(list []mail.Address) string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.String()
	}
	return strings.Join(out, ", ")
}

// messageID is a new Message-ID, at the sender's own domain as RFC 5322 asks.
func messageID(from string) (string, error) {
	_, domain, _ := strings.Cut(from, "@")
	if domain == "" {
		domain = "emguio.invalid"
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("compose: %w", err)
	}
	return "<" + hex.EncodeToString(b[:]) + "@" + domain + ">", nil
}

// Addresses reads a field as typed — addresses apart by commas, each with a name or without —
// and names the first that is not an address, for the sentence the writer is shown.
func Addresses(field, typed string) ([]mail.Address, error) {
	if strings.TrimSpace(typed) == "" {
		return nil, nil
	}
	list, err := mail.ParseAddressList(typed)
	if err == nil {
		out := make([]mail.Address, len(list))
		for i, a := range list {
			out[i] = *a
		}
		return out, nil
	}
	for _, piece := range strings.Split(typed, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		if _, err := mail.ParseAddress(piece); err != nil {
			return nil, &Unaddressable{Field: field, Typed: piece}
		}
	}
	return nil, &Unaddressable{Field: field}
}

// Unaddressable is a field holding something that is not an address.
type Unaddressable struct {
	Field string
	// Typed is what is not an address, when it can be told apart from the rest.
	Typed string
}

func (e *Unaddressable) Error() string {
	if e.Typed == "" {
		return fmt.Sprintf("%s holds something that is not an address.", e.Field)
	}
	return fmt.Sprintf("“%s” in %s is not an address.", e.Typed, e.Field)
}
