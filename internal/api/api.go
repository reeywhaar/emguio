// Package api is emguio's HTTP surface: handlers, the guard, and static serving.
//
// It takes an fs.FS, so tests drive an fstest.MapFS and `go test ./...` passes with no
// frontend build present.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"emguio/internal/app"
	"emguio/internal/config"
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

	loginAll  *limiter
	loginUser *limiter

	// writeDeadline is how long a request that changes something may run. A field so a test
	// can make it short.
	writeDeadline time.Duration
}

// WriteDeadline is how long a request that changes something may run before it is stopped.
//
// There is one writer connection, and a request holding it holds every other write with it. Far
// longer than any write here takes, so it only ever stops one that is not going to finish.
const WriteDeadline = 2 * time.Minute

// New wires the routes. Authorization is decided at registration, so a handler cannot forget
// to check: one registered without its guard is visibly registered without it.
func New(cfg *config.Config, log *slog.Logger, st *store.Store, spa *SPA) *Server {
	s := &Server{
		cfg:      cfg,
		log:      log,
		store:    st,
		sessions: session.New(st, cfg.Secure),
		spa:      spa,
		mux:      http.NewServeMux(),

		loginAll:  newLimiter(30, 2*time.Second),
		loginUser: newLimiter(5, 20*time.Second),

		writeDeadline: WriteDeadline,
	}

	s.mux.HandleFunc("GET /healthz", s.healthz)

	// Everything about proving who you are is under one root. An invitation belongs here
	// rather than under a resource of its own: accepting one is how a user starts.
	s.mux.HandleFunc("POST /api/auth/login", s.login)
	s.mux.HandleFunc("GET /api/auth/invites/{token}", s.getInvite)
	s.mux.HandleFunc("POST /api/auth/invites/{token}/accept", s.acceptInvite)
	s.mux.Handle("POST /api/auth/logout", s.requireSession(s.logout))
	s.mux.Handle("GET /api/auth/me", s.requireSession(s.me))

	// Catch-all, so a mistyped API path never falls through to the SPA and reaches a fetch as
	// an HTML document it cannot parse.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, http.StatusNotFound, CodeNotFound, "There is no such endpoint.")
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
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		// Reads take no writer.
	default:
		ctx, cancel := context.WithTimeout(r.Context(), s.writeDeadline)
		defer cancel()
		r = r.WithContext(ctx)
	}
	s.guard(s.mux).ServeHTTP(w, r)
}

// elapsed is how long the request has been running.
func elapsed(r *http.Request) time.Duration {
	if at, ok := r.Context().Value(ctxStart).(time.Time); ok {
		return time.Since(at).Round(time.Millisecond)
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
		// A browser sets this; a script cannot forge it.
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			refuse(w, http.StatusForbidden, CodeForbidden, "That request came from somewhere else.")
			return
		}
		// Only when a body is present: a DELETE legitimately carries none.
		if r.ContentLength != 0 && !isJSON(r.Header.Get("Content-Type")) {
			refuse(w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Send application/json.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// decode reads a JSON body into v, refusing anything it does not recognize.
//
// Never into a map: a map accepts anything and moves every validation into the handler, one
// forgotten check at a time. DisallowUnknownFields also means a caller's typo'd field is a
// refusal saying so rather than a silently ignored intention.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	// MaxBytesReader rather than LimitReader: a limit that cuts the body short silently reads as
	// JSON that ends early. Hitting this limit says so.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, bodyMax))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var over *http.MaxBytesError
		if errors.As(err, &over) {
			refuse(w, http.StatusRequestEntityTooLarge, CodeBodyTooLarge, "That request body is over 1 MB.")
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
	case errors.Is(err, context.DeadlineExceeded):
		// The deadline on a write: it was waiting for something that was not coming, and the
		// time it waited is what a hang looks like in the log.
		s.log.Warn("request stopped at its deadline", "method", r.Method, "path", r.URL.Path, "after", elapsed(r))
		refuse(w, http.StatusServiceUnavailable, CodeBusy, "That took too long and was stopped. Try again.")
	case errors.Is(err, context.Canceled):
		// The caller left, or the process is stopping: nobody reads an answer.
		s.log.Warn("request cancelled", "method", r.Method, "path", r.URL.Path, "after", elapsed(r))
	default:
		// An unclassified error is a bug, and the message that reaches the caller deliberately
		// does not say what it was.
		s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "after", elapsed(r), "err", err)
		refuse(w, http.StatusInternalServerError, CodeInternal, "Something went wrong here.")
	}
}

// sentence is the error's own text when it carries one written for a reader.
func sentence(err error, fallback string) string {
	switch msg := err.Error(); msg {
	case "", store.ErrNotFound.Error(), store.ErrConflict.Error(), store.ErrInvalid.Error():
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
