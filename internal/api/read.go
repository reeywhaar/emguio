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

	"github.com/emersion/go-imap/v2"

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
	Text    string          `json:"text"`
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
		Text:        read.Text,
		HTML:        read.HTML,
		HeldImages:  read.Held,
		Parts:       []partJSON{},
	}
	for _, p := range read.Parts {
		out.Parts = append(out.Parts, partJSON{Section: p.Section, Name: p.Name, Type: p.Type, Size: p.Size, Listed: p.Listed})
	}
	writeJSON(w, http.StatusOK, out)
}

type flagsBody struct {
	Seen    *bool `json:"seen"`
	Flagged *bool `json:"flagged"`
}

type flagsJSON struct {
	ID       string `json:"id"`
	Seen     bool   `json:"seen"`
	Flagged  bool   `json:"flagged"`
	Answered bool   `json:"answered"`
	Draft    bool   `json:"draft"`
}

// patchMessage changes a message's flags: whether it is read, and whether it is starred. The
// server is told first, and what is kept only once the server has taken it, so the two never
// disagree about which is right.
//
// Its own request rather than something reading does on the side: a GET that changes things is
// one a prefetch, or a link from anywhere, can make on somebody's behalf.
func (s *Server) patchMessage(w http.ResponseWriter, r *http.Request) {
	var body flagsBody
	if !decode(w, r, &body) {
		return
	}
	if body.Seen == nil && body.Flagged == nil {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say what to change: seen, flagged, or both.")
		return
	}
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	u := userOf(r)
	var flags store.Flags
	for _, change := range []struct {
		flag imap.Flag
		on   *bool
	}{{mirror.Seen, body.Seen}, {mirror.Flagged, body.Flagged}} {
		if change.on == nil {
			continue
		}
		now, moved, err := s.mirror.SetFlag(r.Context(), targetOf(u, c), mb.Name, uidValidity, uid, change.flag, *change.on)
		if !s.serverError(w, r, c.ID, err, gone) {
			return
		}
		step := 0
		if moved && change.flag == mirror.Seen {
			step = 1
			if *change.on {
				step = -1
			}
		}
		if err := s.store.SetMessageFlags(r.Context(), mb.ID, uid, now, step); err != nil {
			s.fail(w, r, err)
			return
		}
		if moved {
			s.store.Notify(u.ID)
		}
		flags = now
	}
	writeJSON(w, http.StatusOK, flagsJSON{
		ID: ids.Message(uidValidity, uid), Seen: flags.Seen, Flagged: flags.Flagged, Answered: flags.Answered, Draft: flags.Draft,
	})
}

type moveBody struct {
	// To is the mailbox's id.
	To string `json:"to"`
}

// moveMessage moves a message to another of the config's mailboxes: archiving, deleting to
// Trash, marking as spam and taking it out of spam are each a move, to the mailbox the server
// says is for it.
func (s *Server) moveMessage(w http.ResponseWriter, r *http.Request) {
	var body moveBody
	if !decode(w, r, &body) {
		return
	}
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	u := userOf(r)
	to, err := s.store.Mailbox(r.Context(), u.ID, c.ID, body.To)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if to.ID == mb.ID || !to.Selectable {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Choose another folder to move it to.")
		return
	}
	flags, err := s.mirror.Move(r.Context(), targetOf(u, c), mb.Name, uidValidity, uid, to.Name)
	if errors.Is(err, mirror.ErrUnsupported) {
		refuse(w, http.StatusConflict, CodeConflict, "This mail server cannot move one message without touching others: it has neither MOVE nor UIDPLUS.")
		return
	}
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}
	s.moved(w, r, c.ID, mb.ID, to.ID, uid, flags)
}

// deleteMessage removes a message from the server for good. Deleting to Trash is a move; this is
// what is done in Trash, or where there is none.
func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request) {
	c, mb, uidValidity, uid, ok := s.messageAt(w, r)
	if !ok {
		return
	}
	flags, err := s.mirror.Delete(r.Context(), targetOf(userOf(r), c), mb.Name, uidValidity, uid)
	if errors.Is(err, mirror.ErrUnsupported) {
		refuse(w, http.StatusConflict, CodeConflict, "This mail server cannot delete one message for good without touching others: it has no UIDPLUS.")
		return
	}
	if !s.serverError(w, r, c.ID, err, gone) {
		return
	}
	s.moved(w, r, c.ID, mb.ID, "", uid, flags)
}

// moved records a message gone from a mailbox, says so to the user's tabs, and asks the mirror
// for a look: the window has a place to fill, and the counts are the server's to confirm.
func (s *Server) moved(w http.ResponseWriter, r *http.Request, configID, from, to string, uid uint32, flags store.Flags) {
	if err := s.store.MessageMoved(r.Context(), from, to, uid, flags.Seen); err != nil {
		s.fail(w, r, err)
		return
	}
	s.store.Notify(userOf(r).ID)
	s.mirror.Refresh(configID)
	w.WriteHeader(http.StatusNoContent)
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

// readPart is one part of a message, fetched alone: an attachment to download, or an image the
// HTML shows.
//
// Whatever it is, it is served so that it cannot act as a page of this origin: nosniff, a
// sandbox CSP, and anything that is not a plain image as a download. And as immutable: a UID
// under one UIDVALIDITY names one message for good, and a message never changes.
func (s *Server) readPart(w http.ResponseWriter, r *http.Request) {
	at := r.PathValue("section")
	if !sectionPattern.MatchString(at) {
		refuse(w, http.StatusBadRequest, CodeInvalid, "A part is named by its section, like 2 or 1.3.")
		return
	}
	var section []int
	for _, n := range strings.Split(at, ".") {
		i, _ := strconv.Atoi(n)
		section = append(section, i)
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
