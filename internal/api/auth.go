package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"emguio/internal/session"
	"emguio/internal/store"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// login checks a password and issues a cookie.
//
// Two buckets, with two different jobs. The global one bounds bcrypt, which at cost 12 on an
// unauthenticated endpoint is a CPU exhaustion vector before it is an authentication one. The
// per-username one stops somebody working through a password list against one user, which the
// global limit alone would only make them share with everybody else.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, &req) {
		return
	}
	if !s.loginAll.allow("") {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many sign-in attempts. Wait a minute.")
		return
	}
	if !s.loginUser.allow(strings.ToLower(req.Username)) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many sign-in attempts for that username. Wait a minute.")
		return
	}

	u, err := s.store.Authenticate(r.Context(), req.Username, req.Password)
	if errors.Is(err, store.ErrNotFound) {
		// One refusal for a wrong password and a missing user.
		s.log.Info("sign-in refused")
		refuse(w, http.StatusUnauthorized, CodeUnauthenticated, "That username and password do not match.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.sessions.Issue(r.Context(), w, r, u.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("signed in", "user", u.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Revoke(r.Context(), w, r); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         u.ID,
		"username":   u.Username,
		"created_at": u.CreatedAt.Unix(),
	})
}

// getInvite tells the acceptance page whether a link is live before somebody types a password
// into it. It reveals nothing but its own validity.
func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.InviteByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"expires_at": inv.ExpiresAt.Unix()})
}

// acceptInvite spends a link and signs the new user in.
//
// Signing in immediately, because being shown a login form straight afterwards is asking
// somebody to prove something they just proved.
func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req credentials
	if !decode(w, r, &req) {
		return
	}
	u, err := s.store.AcceptInvite(r.Context(), r.PathValue("token"), req.Username, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.sessions.Issue(r.Context(), w, r, u.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("user created", "user", u.ID)
	w.WriteHeader(http.StatusNoContent)
}

// requireSession admits a request that carries a live session, and nothing else.
//
// Applied at registration rather than inside a handler, so a handler cannot forget to check:
// one registered without this is visibly registered without it.
func (s *Server) requireSession(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.sessions.Resolve(r.Context(), w, r)
		if errors.Is(err, session.ErrNoSession) {
			// Never a redirect: a 302 to an HTML page is the least useful thing a fetch can
			// receive. The island reads this and navigates itself.
			refuse(w, http.StatusUnauthorized, CodeUnauthenticated, "Sign in first.")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		u, err := s.store.UserByID(r.Context(), sess.UserID)
		if errors.Is(err, store.ErrNotFound) {
			s.sessions.Clear(w)
			refuse(w, http.StatusUnauthorized, CodeUnauthenticated, "Sign in first.")
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxUser, u)))
	})
}

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxStart
)

func userOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxUser).(*store.User)
	return u
}
