package api

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/message"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

type partJSON struct {
	// Section is where the server holds it, and what it is fetched by.
	Section string `json:"section"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int    `json:"size"`
	// Listed is false for an image the HTML shows in place.
	Listed bool `json:"listed"`
}

type readJSON struct {
	messageJSON
	Cc      []store.Address `json:"cc"`
	Mailbox string          `json:"mailbox"`
	// ReplyTo is where its sender asks replies to go.
	ReplyTo []store.Address `json:"reply_to"`
	Text    string          `json:"text"`
	// HTMLText is the HTML as text, for a message with no text of its own: what a reply quotes.
	HTMLText string `json:"html_text"`
	// HTML is sanitized, and empty for a message with none. It is still a stranger's, and the
	// page shows it only in a sandboxed frame.
	HTML string `json:"html"`
	// HeldImages is how many images from elsewhere are held back, in Junk: each a blank image
	// until somebody asks, with the proxy's address for it in data-src. Elsewhere they load
	// through the proxy, and this is 0.
	HeldImages int        `json:"held_images"`
	Parts      []partJSON `json:"parts"`
}

// messageAt is the message a request names, and its mailbox and email config. It writes the
// refusal itself and reports false when there is nothing to go on with.
func (s *Server) messageAt(w http.ResponseWriter, r *http.Request) (*store.EmailConfig, *store.Mailbox, uint32, uint32, bool) {
	uidValidity, uid, ok := ids.ParseMessage(r.PathValue("message"))
	if !ok {
		refuse(w, http.StatusBadRequest, CodeInvalid, fmt.Sprintf("%q is not a message id.", r.PathValue("message")))
		return nil, nil, 0, 0, false
	}
	c, mb, ok := s.mailboxOf(w, r)
	if !ok {
		return nil, nil, 0, 0, false
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
		return nil, nil, 0, 0, false
	}
	return c, mb, uidValidity, uid, true
}

// gone is what a message the server no longer has says.
const gone = "This message is no longer on the server. It leaves the list at the next look."

// readMessage is one message for the reading pane, without its attachments: fetched from the
// server every time it is opened, and kept nowhere.
func (s *Server) readMessage(w http.ResponseWriter, r *http.Request) {
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	opened, err := s.mirror.Read(r.Context(), targetOf(userOf(r), c), mb.Name, uidValidity, uid)
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}
	id := ids.Message(uidValidity, uid)
	read := message.Show(opened.Structure, message.Options{
		PartURL: func(at string) string {
			return fmt.Sprintf("/api/email-configs/%s/mailboxes/%s/messages/%s/parts/%s", c.ID, mb.ID, id, at)
		},
		Proxy: s.proxyURL,
		// Loading an image tells its sender the message was opened, and when; in Junk it also
		// tells a spammer the address is read. See docs/reading.md.
		Hold: mb.SpecialUse == store.UseJunk,
	})

	// Whatever the list showed, read from the first bytes at sync, the whole message says better.
	if changed, err := s.store.SetPreview(r.Context(), mb.ID, uid, read.Preview); err != nil {
		s.log.Warn("could not store a preview", "mailbox", mb.ID, "message", id, "err", err)
	} else if changed {
		s.store.Notify(userOf(r).ID)
	}

	m := &store.Message{Header: opened.Header, UIDValidity: uidValidity}
	m.Preview = read.Preview
	out := readJSON{
		messageJSON: messageOut(m),
		Cc:          nonNil(m.Cc),
		Mailbox:     mb.ID,
		ReplyTo:     nonNil(opened.ReplyTo),
		Text:        read.Text,
		HTMLText:    read.HTMLText,
		HTML:        read.HTML,
		HeldImages:  read.Held,
		Parts:       []partJSON{},
	}
	for _, p := range read.Parts {
		out.Parts = append(out.Parts, partJSON{Section: p.Section, Name: p.Name, Type: p.Type, Size: p.Size, Listed: p.Listed})
	}
	writeJSON(w, http.StatusOK, out)
}

// serverError writes the refusal for what the mail server said, and reports whether there was
// nothing to refuse. goneSentence is what a thing the server no longer has says.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, configID string, err error, goneSentence string) bool {
	var f *connect.Failure
	switch {
	case err == nil:
		return true
	case errors.Is(err, mirror.ErrGone):
		// What is kept is behind the server; a look now brings it up to date.
		s.mirror.Refresh(configID)
		refuse(w, http.StatusNotFound, CodeGone, goneSentence)
	case errors.As(err, &f):
		refuse(w, http.StatusBadGateway, CodeUnreachable, f.Sentence)
	default:
		s.fail(w, r, err)
	}
	return false
}

// inline are the types a part may be shown as rather than downloaded: images a browser draws and
// cannot run.
var inline = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
}

// sectionPattern is an IMAP section number: "2", "1.3", nested no deeper than any real message.
var sectionPattern = regexp.MustCompile(`^[1-9][0-9]{0,3}(\.[1-9][0-9]{0,3}){0,15}$`)

// sectionOf reads a section number as IMAP takes it, "1.3" as [1 3].
func sectionOf(at string) ([]int, bool) {
	if !sectionPattern.MatchString(at) {
		return nil, false
	}
	var section []int
	for _, n := range strings.Split(at, ".") {
		i, _ := strconv.Atoi(n)
		section = append(section, i)
	}
	return section, true
}

// readPart is one part of a message, fetched alone: an attachment to download, or an image the
// HTML shows.
//
// Whatever it is, it is served so that it cannot act as a page of this origin: nosniff, a
// sandbox CSP, and anything that is not a plain image as a download. And as immutable: a UID
// under one UIDVALIDITY names one message for good, and a message never changes.
func (s *Server) readPart(w http.ResponseWriter, r *http.Request) {
	at := r.PathValue("section")
	section, ok := sectionOf(at)
	if !ok {
		refuse(w, http.StatusBadRequest, CodeInvalid, "A part is named by its section, like 2 or 1.3.")
		return
	}
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	head, body, err := s.mirror.Part(r.Context(), targetOf(userOf(r), c), mb.Name, uidValidity, uid, section)
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}
	// A server answers a section a message does not have with nothing at all.
	if len(head) == 0 && len(body) == 0 {
		refuse(w, http.StatusNotFound, CodeNotFound, "This message has no such part.")
		return
	}
	part, content, err := message.DecodePart(head, body, at)
	if err != nil {
		refuse(w, http.StatusUnprocessableEntity, CodeUnreadable, "This part is not in a form that can be read.")
		return
	}

	disposition, kind := "attachment", "application/octet-stream"
	if inline[part.Type] {
		disposition, kind = "inline", part.Type
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": part.Name}))
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(content)
}

// imageMax bounds what the proxy relays: an image in a message, not a download.
const imageMax = 10 << 20

// proxyKey signs the addresses the proxy will fetch, derived from the server key so it changes
// when that does and is never stored.
func proxyKey(secret []byte) []byte {
	key, err := hkdf.Key(sha256.New, secret, nil, "emguio image proxy", 32)
	if err != nil {
		panic(err)
	}
	return key
}

func (s *Server) sign(remote string) string {
	mac := hmac.New(sha256.New, s.proxyKey)
	mac.Write([]byte(remote))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// proxyURL is the proxy's address for a remote image. Only what this wrote is fetched, so the
// proxy relays the images in messages and is not a way to fetch anything else from this host.
func (s *Server) proxyURL(remote string) string {
	return "/api/proxy?" + url.Values{
		"u": {base64.RawURLEncoding.EncodeToString([]byte(remote))},
		"s": {s.sign(remote)},
	}.Encode()
}

// imageClient fetches what the proxy relays, through the same screen as every mail server: a
// message naming an address inside the network gets nothing from it.
func imageClient(c *connect.Connector) *http.Client {
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:           c.DialContext,
			TLSClientConfig:       &tls.Config{RootCAs: c.Roots(), MinVersion: tls.VersionTLS12},
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("not a web address")
			}
			return nil
		},
	}
}

// proxyImage fetches a remote image for a message the user chose to show images in, so the
// sender learns that the message was opened but not from where.
func (s *Server) proxyImage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	raw, err := base64.RawURLEncoding.DecodeString(q.Get("u"))
	remote := string(raw)
	if err != nil || !hmac.Equal([]byte(s.sign(remote)), []byte(q.Get("s"))) {
		refuse(w, http.StatusNotFound, CodeNotFound, "That image address was not given out here.")
		return
	}
	if !s.images.allow(userOf(r).ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many images at once. Wait a moment.")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, remote, nil)
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		refuse(w, http.StatusBadRequest, CodeInvalid, "That is not an image address.")
		return
	}
	req.Header.Set("User-Agent", "emguio image proxy")
	resp, err := s.imageClient.Do(req)
	if err != nil {
		refuse(w, http.StatusBadGateway, CodeUnreachable, "The image could not be fetched.")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		refuse(w, http.StatusBadGateway, CodeUnreachable, "The image could not be fetched.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, imageMax+1))
	if err != nil {
		refuse(w, http.StatusBadGateway, CodeUnreachable, "The image could not be fetched.")
		return
	}
	if len(body) > imageMax {
		refuse(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "That image is larger than 10 MB.")
		return
	}
	// What the bytes are, not what the sender says they are.
	kind := http.DetectContentType(body)
	if i := strings.IndexByte(kind, ';'); i >= 0 {
		kind = kind[:i]
	}
	if !inline[kind] && kind != "image/bmp" && kind != "image/x-icon" && kind != "image/vnd.microsoft.icon" {
		refuse(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "That is not an image this shows.")
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// The address is signed and names one image; the browser keeps it rather than asking again.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Write(body)
}
