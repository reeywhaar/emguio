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
	"strconv"
	"strings"
	"time"

	"emguio/internal/connect"
	"emguio/internal/message"
	"emguio/internal/mirror"
	"emguio/internal/store"
)

type partJSON struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int    `json:"size"`
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
	// RemoteImages is how many images the HTML asks for from elsewhere: blocked, unless the
	// request said images=1, when they come through the proxy.
	RemoteImages int        `json:"remote_images"`
	Parts        []partJSON `json:"parts"`
}

// readMessage is one message, whole, for the reading pane. The first opening fetches it from the
// server; after that it comes from the store.
func (s *Server) readMessage(w http.ResponseWriter, r *http.Request) {
	configID, messageID := r.PathValue("id"), r.PathValue("message")
	opened, raw, ok := s.openBody(w, r, configID, messageID)
	if !ok {
		return
	}
	opts := message.Options{
		PartURL: func(i int) string {
			return fmt.Sprintf("/api/email-configs/%s/messages/%s/parts/%d", configID, messageID, i)
		},
	}
	if r.URL.Query().Get("images") == "1" {
		opts.Proxy = s.proxyURL
	}
	read, err := message.Parse(raw, opts)
	if errors.Is(err, message.ErrUnreadable) {
		refuse(w, http.StatusUnprocessableEntity, CodeUnreadable, "This message is not in a form that can be read.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}

	// Whatever the list showed before — read from the first bytes at sync, or by an older build —
	// the whole message says it better.
	if read.Preview != opened.Preview {
		if err := s.store.SetPreview(r.Context(), opened.ID, read.Preview); err != nil {
			s.log.Warn("could not store a preview", "message", opened.ID, "err", err)
		} else {
			s.store.Notify(userOf(r).ID)
		}
	}

	out := readJSON{
		messageJSON:  messageOut(&opened.Message),
		Cc:           nonNil(opened.Cc),
		Mailbox:      opened.MailboxID,
		Text:         read.Text,
		HTML:         read.HTML,
		RemoteImages: read.Remote,
		Parts:        []partJSON{},
	}
	for _, p := range read.Parts {
		out.Parts = append(out.Parts, partJSON{Index: p.Index, Name: p.Name, Type: p.Type, Size: p.Size, Listed: p.Listed})
	}
	writeJSON(w, http.StatusOK, out)
}

type flagsBody struct {
	Seen *bool `json:"seen"`
}

// patchMessage changes a message's flags: for now, whether it is read. The server is told first,
// and the store only once the server has taken it, so the two never disagree about which is
// right.
//
// Its own request rather than something reading does on the side: a GET that changes things is
// one a prefetch, or a link from anywhere, can make on somebody's behalf.
func (s *Server) patchMessage(w http.ResponseWriter, r *http.Request) {
	var body flagsBody
	if !decode(w, r, &body) {
		return
	}
	if body.Seen == nil {
		refuse(w, http.StatusBadRequest, CodeInvalid, "Say what to change: seen is the one flag this sets.")
		return
	}
	u := userOf(r)
	config, err := s.store.EmailConfig(r.Context(), u.ID, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	opened, err := s.store.OpenMessage(r.Context(), u.ID, config.ID, r.PathValue("message"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if opened.Flags.Seen != *body.Seen {
		if s.mirror == nil {
			refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "The server cannot be reached right now.")
			return
		}
		target := store.SyncTarget{ID: config.ID, UserID: u.ID, UpdatedAt: config.UpdatedAt}
		err := s.mirror.SetSeen(r.Context(), target, opened.MailboxName, opened.UIDValidity, opened.UID, *body.Seen)
		if !s.serverError(w, r, config.ID, err) {
			return
		}
		if err := s.store.SetSeen(r.Context(), opened.ID, *body.Seen); err != nil {
			s.fail(w, r, err)
			return
		}
		opened.Flags.Seen = *body.Seen
		s.store.Notify(u.ID)
	}
	writeJSON(w, http.StatusOK, messageOut(&opened.Message))
}

// serverError writes the refusal for what the mail server said, and reports whether there was
// nothing to refuse.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, configID string, err error) bool {
	var f *connect.Failure
	switch {
	case err == nil:
		return true
	case errors.Is(err, mirror.ErrGone):
		// The list is behind the server; a look now brings it up to date.
		s.mirror.Refresh(configID)
		refuse(w, http.StatusNotFound, CodeGone, "This message is no longer on the server. It leaves the list at the next look.")
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

// readPart is one part of a message: an attachment to download, or an image the HTML shows.
//
// Whatever it is, it is served so that it cannot act as a page of this origin: nosniff, a
// sandbox CSP, and anything that is not a plain image as a download.
func (s *Server) readPart(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		refuse(w, http.StatusBadRequest, CodeInvalid, "A part is named by its number.")
		return
	}
	_, raw, ok := s.openBody(w, r, r.PathValue("id"), r.PathValue("message"))
	if !ok {
		return
	}
	part, content, err := message.PartOf(raw, index)
	if err != nil {
		refuse(w, http.StatusNotFound, CodeNotFound, "This message has no such part.")
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
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(content)
}

// openBody finds a user's message and its raw bytes, fetching them from the server the first
// time. It writes the refusal itself and reports false when there is nothing to go on with.
func (s *Server) openBody(w http.ResponseWriter, r *http.Request, configID, messageID string) (*store.Opened, []byte, bool) {
	u := userOf(r)
	config, err := s.store.EmailConfig(r.Context(), u.ID, configID)
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	opened, err := s.store.OpenMessage(r.Context(), u.ID, configID, messageID)
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	raw, kept, err := s.store.Body(r.Context(), opened.ID)
	if err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	if kept {
		return opened, raw, true
	}
	if s.mirror == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "Messages cannot be fetched right now.")
		return nil, nil, false
	}

	target := store.SyncTarget{ID: config.ID, UserID: u.ID, UpdatedAt: config.UpdatedAt}
	raw, err = s.mirror.Raw(r.Context(), target, opened.MailboxName, opened.UIDValidity, opened.UID)
	if !s.serverError(w, r, config.ID, err) {
		return nil, nil, false
	}

	preview := ""
	if read, err := message.Parse(raw, message.Options{}); err == nil {
		preview = read.Preview
	}
	if err := s.store.PutBody(r.Context(), opened.ID, raw, preview); err != nil {
		s.fail(w, r, err)
		return nil, nil, false
	}
	return opened, raw, true
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
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(body)
}
