// Package api is emguio's HTTP surface: handlers, the guard, and static serving.
//
// It takes an fs.FS, so tests drive an fstest.MapFS and `go test ./...` passes with no
// frontend build present.
package api

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"emguio/internal/app"
	"emguio/internal/autoconfig"
	"emguio/internal/config"
	"emguio/internal/connect"
	"emguio/internal/session"
	"emguio/internal/store"
)

// bodyMax bounds a JSON request.
const bodyMax = 1 << 20

// Server holds what every handler needs.
//
// Rate limiters belong on it, not at package level: two instances in one process would
// otherwise share one budget.
type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	store    *store.Store
	sessions *session.Manager
	spa      *SPA
	mux      *http.ServeMux
	// routes is every API pattern registered, which docs/api.md has to cover.
	routes []string

	// connector is the only way out to a mail server. A field so a test can trust its own
	// certificates and reach loopback.
	connector *connect.Connector
	// mirror is nil in tests that do not need mail to arrive.
	mirror Mirror
	// push is nil in tests that send no notifications.
	push Pusher
	// holds is what outgoing servers said about holding mail to send later.
	holds holds
	// finder looks up a mail domain's settings; a field so a test can stand in for the web.
	finder *autoconfig.Finder
	// web fetches the addresses messages name: images the proxy relays, a one-click unsubscribe.
	// proxyKey signs what the proxy may fetch.
	web      *http.Client
	proxyKey []byte

	loginAll  *limiter
	loginUser *limiter
	tests     *limiter
	lookups   *limiter
	images    *limiter
	pushTests *limiter
	// unsubscribes bounds how many one-click unsubscribes a user sends: each is a request
	// emguio makes to an address a message names.
	unsubscribes *limiter
	// unseals bounds tries at a user's seal key, each an Argon2 derivation.
	unseals *limiter

	// exports are the downloads of mail asked for, held for an hour; imports the files of mail
	// being put into folders, and how each went.
	exports exports
	imports imports

	// writeDeadline is how long a request that changes something may run. A field so a test
	// can make it short.
	writeDeadline time.Duration
}

// WriteDeadline is how long a request that changes something may run before it is stopped.
//
// There is one writer connection, and a request holding it holds every other write with it. Far
// longer than any write here takes, so it only ever stops one that is not going to finish.
const WriteDeadline = 2 * time.Minute

// handle registers an API pattern: recorded here rather than matched by prefix in a test, so a
// route is one docs/api.md has to cover the moment it is registered.
func (s *Server) handle(pattern string, h http.Handler) {
	s.routes = append(s.routes, pattern)
	s.mux.Handle(pattern, h)
}

// Routes is every API pattern registered.
func (s *Server) Routes() []string { return append([]string(nil), s.routes...) }

// New wires the routes. Authorization is decided at registration, so a handler cannot forget
// to check: one registered without its guard is visibly registered without it.
func New(cfg *config.Config, log *slog.Logger, st *store.Store, spa *SPA, docs *Docs, mirror Mirror, push Pusher) *Server {
	if docs == nil {
		docs = NewDocs(nil, nil, "")
	}
	s := &Server{
		cfg:      cfg,
		log:      log,
		store:    st,
		sessions: session.New(st, cfg.Secure),
		spa:      spa,
		mux:      http.NewServeMux(),

		connector: connect.New(cfg.AllowNetworks),
		mirror:    mirror,
		push:      push,
		holds:     holds{said: map[string]heldUntil{}},
		proxyKey:  proxyKey(),

		loginAll:     newLimiter(30, 2*time.Second),
		loginUser:    newLimiter(5, 20*time.Second),
		tests:        newLimiter(10, 6*time.Second),
		lookups:      newLimiter(10, 6*time.Second),
		images:       newLimiter(200, 100*time.Millisecond),
		pushTests:    newLimiter(3, 20*time.Second),
		unsubscribes: newLimiter(10, 6*time.Second),
		unseals:      newLimiter(5, 12*time.Second),

		writeDeadline: WriteDeadline,
	}

	s.web = webClient(s.connector)
	s.finder = finderOf(s.connector)

	s.mux.HandleFunc("GET /healthz", s.healthz)

	// Unauthenticated, because a reader has to read it before there is any question of
	// signing in.
	s.mux.Handle("GET /docs", docs)
	s.mux.Handle("GET /docs.md", docs)
	s.mux.HandleFunc("GET /llms.txt", s.llms)

	// Everything about proving who you are is under one root. An invitation belongs here
	// rather than under a resource of its own: accepting one is how a user starts.
	s.handle("POST /api/auth/login", http.HandlerFunc(s.login))
	s.handle("GET /api/auth/invites/{token}", http.HandlerFunc(s.getInvite))
	s.handle("POST /api/auth/invites/{token}/accept", http.HandlerFunc(s.acceptInvite))
	s.handle("POST /api/auth/logout", s.requireSession(s.logout))
	s.handle("GET /api/auth/me", s.requireSession(s.me))
	s.handle("PUT /api/auth/password", s.requireSession(s.changePassword))
	s.handle("GET /api/sessions", s.requireSession(s.listSessions))
	s.handle("DELETE /api/sessions/{id}", s.requireSession(s.revokeSession))
	s.handle("DELETE /api/sessions", s.requireSession(s.revokeOtherSessions))
	s.handle("POST /api/seal", s.requireSession(s.createSealKey))
	s.handle("POST /api/seal/carry", s.requireSession(s.carrySealKey))
	s.handle("POST /api/seal/unseal", s.requireSession(s.unseal))
	s.handle("PUT /api/seal", s.requireSession(s.changeSealKey))
	s.handle("POST /api/seal/reset", s.requireSession(s.resetSealKey))

	s.handle("GET /api/email-configs", s.requireUnsealed(s.listEmailConfigs))
	s.handle("POST /api/email-configs", s.requireUnsealed(s.createEmailConfig))
	s.handle("POST /api/email-configs/test", s.requireUnsealed(s.testEmailConfig))
	s.handle("POST /api/email-configs/autoconfig", s.requireUnsealed(s.lookUpSettings))
	s.handle("PUT /api/email-configs/order", s.requireUnsealed(s.orderEmailConfigs))
	s.handle("PUT /api/email-configs/{id}", s.requireUnsealed(s.putEmailConfig))
	s.handle("DELETE /api/email-configs/{id}", s.requireUnsealed(s.deleteEmailConfig))
	s.handle("PUT /api/email-configs/{id}/archive", s.requireUnsealed(s.putArchive))
	s.handle("PUT /api/email-configs/{id}/notifications", s.requireUnsealed(s.putNotifications))
	s.handle("PUT /api/email-configs/{id}/off", s.requireUnsealed(s.putOff))
	s.handle("PUT /api/email-configs/{id}/reading", s.requireUnsealed(s.putConfigReading))
	s.handle("POST /api/email-configs/{id}/test", s.requireUnsealed(s.testEmailConfig))
	s.handle("POST /api/email-configs/{id}/sync", s.requireUnsealed(s.awake(s.syncEmailConfig)))
	s.handle("POST /api/email-configs/{id}/send", s.requireUnsealed(s.awake(s.sendMessage)))
	s.handle("GET /api/email-configs/{id}/hold", s.requireUnsealed(s.awake(s.holdLimit)))
	s.handle("POST /api/email-configs/{id}/drafts", s.requireUnsealed(s.awake(s.createDraft)))
	s.handle("PUT /api/email-configs/{id}/drafts/{draft}", s.requireUnsealed(s.awake(s.saveDraft)))
	s.handle("DELETE /api/email-configs/{id}/drafts/{draft}", s.requireUnsealed(s.awake(s.deleteDraft)))
	s.handle("GET /api/email-configs/{id}/mailboxes", s.requireUnsealed(s.listMailboxes))
	s.handle("POST /api/email-configs/{id}/mailboxes", s.requireUnsealed(s.awake(s.createMailbox)))
	s.handle("PUT /api/email-configs/{id}/mailboxes/order", s.requireUnsealed(s.orderMailboxes))
	s.handle("PUT /api/email-configs/{id}/mailboxes/{mailbox}", s.requireUnsealed(s.awake(s.renameMailbox)))
	s.handle("DELETE /api/email-configs/{id}/mailboxes/{mailbox}", s.requireUnsealed(s.awake(s.deleteMailbox)))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/empty", s.requireUnsealed(s.awake(s.emptyMailbox)))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/read", s.requireUnsealed(s.awake(s.readMailbox)))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/delete-old", s.requireUnsealed(s.awake(s.deleteOldMail)))
	s.handle("GET /api/email-configs/{id}/mailboxes/{mailbox}/messages", s.requireUnsealed(s.awake(s.listMessages)))
	s.handle("GET /api/email-configs/{id}/mailboxes/{mailbox}/threads", s.requireUnsealed(s.awake(s.threadCounts)))
	s.handle("GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}", s.requireUnsealed(s.awake(s.readMessage)))
	s.handle("GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/conversation", s.requireUnsealed(s.awake(s.conversation)))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/unsubscribe", s.requireUnsealed(s.awake(s.unsubscribe)))
	s.handle("GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/parts/{section}", s.requireUnsealed(s.awake(s.readPart)))
	s.handle("GET /api/proxy", s.requireUnsealed(s.proxyImage))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/export", s.requireUnsealed(s.awake(s.createExport)))
	s.handle("GET /api/exports", s.requireUnsealed(s.listExports))
	s.handle("GET /api/exports/{export}", s.requireUnsealed(s.getExport))
	s.handle("DELETE /api/exports/{export}", s.requireUnsealed(s.dismissExport))
	s.handle("GET /api/exports/{export}/file", s.requireUnsealed(s.exportFile))
	s.handle("POST /api/exports/{export}/delete", s.requireUnsealed(s.deleteExported))
	s.handle("POST /api/email-configs/{id}/mailboxes/{mailbox}/import", s.requireUnsealed(s.awake(s.importMail)))
	s.handle("GET /api/imports", s.requireUnsealed(s.listImports))
	s.handle("POST /api/imports/{import}/cancel", s.requireUnsealed(s.cancelImport))
	s.handle("DELETE /api/imports/{import}", s.requireUnsealed(s.dismissImport))

	s.handle("GET /api/jobs", s.requireUnsealed(s.listJobs))
	s.handle("POST /api/jobs", s.requireUnsealed(s.postJobs))
	s.handle("DELETE /api/jobs/{job}", s.requireUnsealed(s.dismissJob))
	s.handle("GET /api/transfers", s.requireUnsealed(s.listTransfers))
	s.handle("POST /api/transfers/{transfer}/cancel", s.requireUnsealed(s.cancelTransfer))
	s.handle("POST /api/transfers/{transfer}/retry", s.requireUnsealed(s.retryTransfer))
	s.handle("DELETE /api/transfers/{transfer}", s.requireUnsealed(s.dismissTransfer))

	s.handle("GET /api/reading", s.requireUnsealed(s.reading))
	s.handle("PUT /api/reading", s.requireUnsealed(s.putReading))

	s.handle("GET /api/push/key", s.requireUnsealed(s.pushKey))
	s.handle("GET /api/push/settings", s.requireUnsealed(s.pushSettings))
	s.handle("PUT /api/push/settings", s.requireUnsealed(s.putPushSettings))
	s.handle("GET /api/devices", s.requireUnsealed(s.listDevices))
	s.handle("POST /api/devices", s.requireUnsealed(s.createDevice))
	s.handle("POST /api/devices/test", s.requireUnsealed(s.testDevices))
	s.handle("DELETE /api/devices/{id}", s.requireUnsealed(s.deleteDevice))

	s.handle("GET /api/events", s.requireSession(s.events))

	// Catch-all, so a mistyped API path never falls through to the SPA and reaches a fetch as
	// an HTML document it cannot parse.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such endpoint. /docs lists them all.")
	})

	s.mux.Handle("/", s.gate(spa))
	return s
}

// gate decides who gets which document, before the bundle loads.
//
// A signed-out visitor handed the application shell sees the whole interface draw and then
// every panel in it fail, which reads as a broken emguio rather than as a sign-in page. The
// answer has to be here: the island cannot ask before it has loaded, and by then it has drawn.
//
// Only shells are gated. A file is served to anybody, which is what lets the sign-in page load
// its own stylesheet.
func (s *Server) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sh := s.spa.shellFor(r.URL.Path)
		if sh == nil {
			next.ServeHTTP(w, r)
			return
		}
		_, err := s.sessions.Resolve(r.Context(), w, r)
		switch {
		case err != nil && !sh.public:
			redirect(w, r, signInPath)
		case err == nil && r.URL.Path == signInPath:
			// Somebody already signed in asking for the sign-in page means a stale tab or a
			// bookmark, not a second user.
			redirect(w, r, "/")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// signInPath is the one shell a signed-in visitor is sent away from. /invite is not: see the
// shell table.
const signInPath = "/login"

// redirect sends a navigation elsewhere, uncached.
//
// Without no-store a browser is entitled to remember that / redirects, and would keep doing it
// after the sign-in that fixed it.
func redirect(w http.ResponseWriter, r *http.Request, to string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, to, http.StatusFound)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// When it began, so a failure can say how long it had been waiting.
	r = r.WithContext(context.WithValue(r.Context(), ctxStart, time.Now()))
	sv := &served{ResponseWriter: w}
	r = r.WithContext(context.WithValue(r.Context(), ctxServed, sv))
	defer s.logRequest(r, sv)
	w = sv
	// Nothing here is shown in another page's frame, where a click could be steered.
	w.Header().Set("X-Frame-Options", "DENY")
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		// Reads take no writer, and the event stream rightly runs for as long as the tab is open.
	default:
		// An upload to import takes as long as the file takes to arrive.
		if uploading(r) {
			break
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.writeDeadline)
		defer cancel()
		r = r.WithContext(ctx)
	}
	s.guard(s.mux).ServeHTTP(w, r)
}

// served is what a request's line in the log says, filled in as it is answered: the status, and
// who asked, once a session says.
type served struct {
	http.ResponseWriter
	status int
	user   string
}

func (sv *served) WriteHeader(code int) {
	if sv.status == 0 {
		sv.status = code
	}
	sv.ResponseWriter.WriteHeader(code)
}

func (sv *served) Write(b []byte) (int, error) {
	if sv.status == 0 {
		sv.status = http.StatusOK
	}
	return sv.ResponseWriter.Write(b)
}

// Flush, for the event stream, which writes as things happen.
func (sv *served) Flush() {
	if f, ok := sv.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (sv *served) Unwrap() http.ResponseWriter { return sv.ResponseWriter }

// logRequest says what was asked and how it was answered: who, what, the outcome and how long.
// Not the query, which holds what somebody searched for, nor the bundle's files and the health
// check, which say nothing anybody looks for.
func (s *Server) logRequest(r *http.Request, sv *served) {
	path := r.URL.Path
	page := s.spa != nil && s.spa.shellFor(path) != nil
	if path == "/healthz" || (!strings.HasPrefix(path, "/api/") && !page &&
		path != "/docs" && path != "/docs.md" && path != "/llms.txt") {
		return
	}
	status := cmp.Or(sv.status, http.StatusOK)
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelWarn
	}
	attrs := []any{"method", r.Method, "path", loggedPath(path), "status", status, "took", elapsed(r)}
	if sv.user != "" {
		attrs = append(attrs, "user", sv.user)
	}
	s.log.Log(r.Context(), level, "request answered", attrs...)
}

// loggedPath is path with an invitation's token blanked: a live one is a way in.
func loggedPath(path string) string {
	for _, prefix := range []string{"/invite/", "/api/auth/invites/"} {
		if rest, ok := strings.CutPrefix(path, prefix); ok {
			if _, tail, more := strings.Cut(rest, "/"); more {
				return prefix + "…/" + tail
			}
			return prefix + "…"
		}
	}
	return path
}

// elapsed is how long the request has been running.
func elapsed(r *http.Request) time.Duration {
	if at, ok := r.Context().Value(ctxStart).(time.Time); ok {
		return time.Since(at).Round(100 * time.Microsecond)
	}
	return 0
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": app.Version})
}

// guard is the CSRF defense: SameSite on the cookie, Sec-Fetch-Site, and a declared content
// type. It leans on this origin being the only one the browser talks to, which a test pins by
// asserting no Access-Control-Allow-Origin is ever emitted.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		// A browser sets this; a script cannot forge it. Same-site is refused too: a sibling
		// subdomain gets the cookie. Absent, it is not a browser.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
			refuse(w, http.StatusForbidden, CodeForbidden, "That request came from somewhere else.")
			return
		}
		// Only when a body is present: a DELETE legitimately carries none. A file to import is
		// sent as itself, in a type no form can send.
		if r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) && !uploading(r) {
			refuse(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Send application/json.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// uploading is a file sent to import, as itself rather than as JSON: in a type a page of another
// origin cannot send without asking first, which this answers no to.
func uploading(r *http.Request) bool {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/import") {
		return false
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return importKinds[ct] != ""
}

// decode reads a JSON body into v, refusing anything it does not recognize.
//
// Never into a map: a map accepts anything and moves every validation into the handler, one
// forgotten check at a time. DisallowUnknownFields also means a caller's typo'd field is a
// refusal saying so rather than a silently ignored intention.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeUpTo(w, r, v, bodyMax)
}

// decodeUpTo is decode for a body that may be larger: a message with its attachments.
func decodeUpTo(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	// MaxBytesReader rather than LimitReader: a limit that cuts the body short silently reads as
	// JSON that ends early. Hitting this limit says so.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, fmt.Sprintf("That request body is over %d MB.", limit>>20))
			return false
		}
		refuse(w, http.StatusBadRequest, CodeInvalid, "That request body is not the JSON this expects: "+err.Error())
		return false
	}
	return true
}

// fail maps a store error onto a status and a code, in the one place that does.
//
// The message is the store's own, because it was written for whoever reads it.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		refuse(w, http.StatusNotFound, CodeNotFound, sentence(err, "There is no such thing."))
	case errors.Is(err, store.ErrConflict):
		refuse(w, http.StatusConflict, CodeConflict, sentence(err, "That already exists."))
	case errors.Is(err, store.ErrInvalid):
		refuse(w, http.StatusBadRequest, CodeInvalid, sentence(err, "That request is not valid."))
	case errors.Is(err, store.ErrSealed):
		refuse(w, http.StatusLocked, CodeSealed, sentence(err, "Unseal emguio with your seal key first."))
	case errors.Is(err, context.DeadlineExceeded):
		// The deadline on a write: it was waiting for something that was not coming, and the
		// time it waited is what a hang looks like in the log.
		s.log.Warn("request stopped at its deadline: something it waited for did not come", "method", r.Method, "path", r.URL.Path, "after", elapsed(r))
		refuse(w, http.StatusServiceUnavailable, CodeBusy, "That took too long and was stopped. Try again.")
	case errors.Is(err, context.Canceled):
		// The caller left, or the process is stopping: nobody reads an answer.
		s.log.Warn("request cancelled: the caller left, or the server is stopping", "method", r.Method, "path", r.URL.Path, "after", elapsed(r))
	default:
		// An unclassified error is a bug, and the message that reaches the caller deliberately
		// does not say what it was.
		s.log.Error("request failed with an error nothing here explains", "method", r.Method, "path", r.URL.Path, "after", elapsed(r), "error", err.Error())
		refuse(w, http.StatusInternalServerError, CodeInternal, "Something went wrong here.")
	}
}

// sentence is the error's own text when it carries one written for a reader.
func sentence(err error, fallback string) string {
	switch msg := err.Error(); msg {
	case "", store.ErrNotFound.Error(), store.ErrConflict.Error(), store.ErrInvalid.Error(), store.ErrSealed.Error():
		return fallback
	default:
		return msg
	}
}

func isJSON(ct string) bool {
	for i := 0; i < len(ct); i++ {
		if ct[i] == ';' {
			ct = ct[:i]
			break
		}
	}
	for len(ct) > 0 && ct[len(ct)-1] == ' ' {
		ct = ct[:len(ct)-1]
	}
	return ct == "application/json"
}
