package api

import (
	"net/http"

	"emguio/internal/store"
)

type sealBody struct {
	// Key is the seal key to make, to unseal with, or to change to.
	Key string `json:"key"`
	// Current is the seal key being changed from.
	Current string `json:"current"`
}

// createSealKey makes the user's first seal key, for one who has none: a user from before seal
// keys. See docs/email-configs.md.
func (s *Server) createSealKey(w http.ResponseWriter, r *http.Request) {
	s.sealed(w, r, "seal key made", func(u *store.User, body sealBody) error {
		return s.store.CreateSealKey(r.Context(), u.ID, body.Key)
	})
}

// carrySealKey makes the first seal key of a user from before seal keys out of the key emguio ran
// with then, keeping their saved mail passwords.
func (s *Server) carrySealKey(w http.ResponseWriter, r *http.Request) {
	if !s.unseals.allow(userOf(r).ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many tries. Wait a minute.")
		return
	}
	s.sealed(w, r, "seal key carried over", func(u *store.User, body sealBody) error {
		return s.store.CarrySealKey(r.Context(), u.ID, body.Key)
	})
}

// unseal opens the user's seal key until the server stops, and starts reading their mail.
func (s *Server) unseal(w http.ResponseWriter, r *http.Request) {
	if !s.unseals.allow(userOf(r).ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many tries. Wait a minute.")
		return
	}
	s.sealed(w, r, "unsealed", func(u *store.User, body sealBody) error {
		return s.store.Unseal(r.Context(), u.ID, body.Key)
	})
}

// changeSealKey seals the user's key again with a new seal key, given the current one.
func (s *Server) changeSealKey(w http.ResponseWriter, r *http.Request) {
	if !s.unseals.allow(userOf(r).ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many tries. Wait a minute.")
		return
	}
	s.sealed(w, r, "seal key changed", func(u *store.User, body sealBody) error {
		return s.store.ChangeSealKey(r.Context(), u.ID, body.Current, body.Key)
	})
}

// resetSealKey replaces a forgotten seal key: each of the user's mail accounts asks for its
// passwords again.
func (s *Server) resetSealKey(w http.ResponseWriter, r *http.Request) {
	s.sealed(w, r, "seal key replaced", func(u *store.User, body sealBody) error {
		return s.store.ResetSealKey(r.Context(), u.ID, body.Key)
	})
}

// sealed runs one of the above, logs it, and has the mirror read what is now open.
func (s *Server) sealed(w http.ResponseWriter, r *http.Request, done string, do func(*store.User, sealBody) error) {
	var body sealBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	if err := do(u, body); err != nil {
		s.log.Info(done+": refused", "user", u.ID, "reason", err.Error())
		s.fail(w, r, err)
		return
	}
	s.log.Info(done, "user", u.ID)
	s.reconcile()
	w.WriteHeader(http.StatusNoContent)
}

// requireUnsealed admits a request with a live session whose user has unsealed: everything else
// a user does needs their mail passwords, or is not worth doing before they can be read.
func (s *Server) requireUnsealed(next http.HandlerFunc) http.Handler {
	return s.requireSession(func(w http.ResponseWriter, r *http.Request) {
		if !s.store.Unsealed(userOf(r).ID) {
			refuse(w, http.StatusLocked, CodeSealed, "Unseal emguio with your seal key first.")
			return
		}
		next(w, r)
	})
}
