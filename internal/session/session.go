// Package session is the cookie: issuing one, resolving one, and sliding its window.
//
// It owns the cookie and nothing else. What a session *is* lives in internal/store, which
// never learns that HTTP exists.
package session

import (
	"context"
	"errors"
	"net/http"
	"time"

	"emguio/internal/store"
)

// CookieName is the one cookie emguio sets.
const CookieName = "emguio_auth"

// Manager issues and resolves sessions.
type Manager struct {
	store  *store.Store
	secure bool
}

func New(st *store.Store, secure bool) *Manager {
	return &Manager{store: st, secure: secure}
}

// Issue signs somebody in and sets the cookie.
func (m *Manager) Issue(ctx context.Context, w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := store.Secret()
	if err != nil {
		return err
	}
	sess, err := m.store.CreateSession(ctx, token, userID, r.UserAgent())
	if err != nil {
		return err
	}
	m.set(w, token, sess.ExpiresAt)
	return nil
}

// Resolve returns the session a request arrived on, or ErrNoSession.
//
// It also slides the window, at most once a refresh, and re-issues the cookie when it moves.
func (m *Manager) Resolve(ctx context.Context, w http.ResponseWriter, r *http.Request) (*store.Session, error) {
	token := Token(r)
	if token == "" {
		return nil, ErrNoSession
	}
	sess, err := m.store.SessionByToken(ctx, token)
	if errors.Is(err, store.ErrNotFound) {
		// The cookie names a row that is gone or lapsed. Clearing it here means the next
		// request is not another lookup that fails the same way.
		m.clear(w)
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}

	// Asked only when the window is due to move, so an ordinary read never takes the one
	// writer connection and never queues behind a write.
	if m.store.Now().Sub(sess.LastSeenAt) < store.SessionRefresh {
		return sess, nil
	}
	expires, moved, err := m.store.TouchSession(ctx, token, r.UserAgent())
	if err != nil {
		// Best effort: a window that did not slide now slides on the next request, and a read is
		// not refused for want of a bookkeeping write.
		return sess, nil
	}
	if moved {
		m.set(w, token, expires)
		sess.ExpiresAt = expires
	}
	return sess, nil
}

// Revoke signs the current session out.
func (m *Manager) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if token := Token(r); token != "" {
		if err := m.store.DeleteSession(ctx, token); err != nil {
			return err
		}
	}
	m.clear(w)
	return nil
}

// Clear removes the cookie without touching the row, for when the row is already gone.
func (m *Manager) Clear(w http.ResponseWriter) { m.clear(w) }

// ErrNoSession means the request carried none, or one that is no longer live.
var ErrNoSession = errors.New("no session")

// Token is the cookie value on a request, or "".
func Token(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (m *Manager) set(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
