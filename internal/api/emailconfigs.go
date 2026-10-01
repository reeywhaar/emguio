package api

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"emguio/internal/connect"
	"emguio/internal/store"
)

type serverJSON struct {
	Protocol string `json:"protocol,omitempty"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
}

type emailConfigJSON struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Email    string     `json:"email"`
	Incoming serverJSON `json:"incoming"`
	// Outgoing is null for none, and its username is empty when it signs in as the incoming
	// server does.
	Outgoing  *serverJSON `json:"outgoing"`
	CreatedAt int64       `json:"created_at"`
	UpdatedAt int64       `json:"updated_at"`
}

func toJSON(c *store.EmailConfig) emailConfigJSON {
	out := emailConfigJSON{
		ID:        c.ID,
		Name:      c.Name,
		Email:     c.Email,
		Incoming:  serverJSON(c.Incoming),
		CreatedAt: c.CreatedAt.Unix(),
		UpdatedAt: c.UpdatedAt.Unix(),
	}
	if c.Outgoing != nil {
		o := serverJSON(*c.Outgoing)
		out.Outgoing = &o
	}
	return out
}

type loginBody struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type incomingBody struct {
	Protocol string `json:"protocol"`
	loginBody
}

// emailConfigBody is a whole email config, as the form sends it to save or to test. A password
// left empty is the one already saved.
type emailConfigBody struct {
	Name     string       `json:"name"`
	Email    string       `json:"email"`
	Incoming incomingBody `json:"incoming"`
	Outgoing *loginBody   `json:"outgoing"`
}

func (b emailConfigBody) input() store.EmailConfigInput {
	in := store.EmailConfigInput{
		Name:  b.Name,
		Email: b.Email,
		Incoming: store.Login{
			Server: store.Server{
				Protocol: b.Incoming.Protocol,
				Host:     b.Incoming.Host,
				Port:     b.Incoming.Port,
				TLS:      b.Incoming.TLS,
				Username: b.Incoming.Username,
			},
			Password: b.Incoming.Password,
		},
	}
	if o := b.Outgoing; o != nil {
		in.Outgoing = &store.Login{
			Server:   store.Server{Host: o.Host, Port: o.Port, TLS: o.TLS, Username: o.Username},
			Password: o.Password,
		}
	}
	return in
}

func (s *Server) listEmailConfigs(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.EmailConfigs(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]emailConfigJSON, len(list))
	for i, c := range list {
		out[i] = toJSON(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"email_configs": out})
}

func (s *Server) createEmailConfig(w http.ResponseWriter, r *http.Request) {
	var body emailConfigBody
	if !decode(w, r, &body) {
		return
	}
	c, err := s.store.CreateEmailConfig(r.Context(), userOf(r).ID, body.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("email config created", "user", userOf(r).ID, "email_config", c.ID, "host", c.Incoming.Host)
	writeJSON(w, http.StatusCreated, toJSON(c))
}

func (s *Server) putEmailConfig(w http.ResponseWriter, r *http.Request) {
	var body emailConfigBody
	if !decode(w, r, &body) {
		return
	}
	c, err := s.store.UpdateEmailConfig(r.Context(), userOf(r).ID, r.PathValue("id"), body.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toJSON(c))
}

func (s *Server) deleteEmailConfig(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteEmailConfig(r.Context(), userOf(r).ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("email config deleted", "user", userOf(r).ID, "email_config", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// checkJSON is one server's answer. Message is empty when it signed in.
type checkJSON struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// testEmailConfig signs in to a draft's servers and reports each one, without saving anything.
//
// A refusal from a server is a 200 with ok false: the test ran, and that is its answer. What is
// wrong with the draft itself is a 400 like any other.
//
// Limited per user, because it dials whatever it is given and a loop of these would make this
// instance somebody's port scanner.
func (s *Server) testEmailConfig(w http.ResponseWriter, r *http.Request) {
	var body emailConfigBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	if !s.tests.allow(u.ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many connection tests. Wait a minute.")
		return
	}
	id := r.PathValue("id")
	logins, err := s.store.Logins(r.Context(), u.ID, id, body.input())
	if err != nil {
		s.fail(w, r, err)
		return
	}

	var (
		wg       sync.WaitGroup
		incoming checkJSON
		outgoing *checkJSON
	)
	wg.Go(func() {
		incoming = s.check(r.Context(), id, "incoming", logins.Incoming, s.connector.CheckIMAP)
	})
	if o := logins.Outgoing; o != nil {
		wg.Go(func() {
			c := s.check(r.Context(), id, "outgoing", *o, s.connector.CheckSMTP)
			outgoing = &c
		})
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"incoming": incoming, "outgoing": outgoing})
}

func (s *Server) check(ctx context.Context, id, role string, l store.Login,
	run func(context.Context, connect.Server) error) checkJSON {
	err := run(ctx, connect.Server{
		Host:     l.Host,
		Port:     l.Port,
		TLS:      l.TLS,
		Username: l.Username,
		Password: l.Password,
	})
	if err == nil {
		return checkJSON{OK: true}
	}
	var f *connect.Failure
	if !errors.As(err, &f) {
		s.log.Error("connection test failed unclassified", "email_config", id, "server", role, "host", l.Host, "err", err)
		return checkJSON{Message: "Something went wrong here."}
	}
	s.log.Info("connection test failed", "email_config", id, "server", role, "host", l.Host, "class", f.Class)
	return checkJSON{Message: f.Sentence}
}
