package api

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"

	"emguio/internal/autoconfig"
	"emguio/internal/connect"
)

// finderOf looks settings up through the connector, which refuses this instance's own networks:
// a provider's settings live at an address a user typed.
func finderOf(c *connect.Connector) *autoconfig.Finder {
	return &autoconfig.Finder{
		Client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext:           c.DialContext,
				TLSClientConfig:       &tls.Config{RootCAs: c.Roots(), MinVersion: tls.VersionTLS12},
				ResponseHeaderTimeout: 5 * time.Second,
			},
			// Settings are read over https only, all the way.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 || req.URL.Scheme != "https" {
					return errors.New("not followed")
				}
				return nil
			},
		},
		Resolver: net.DefaultResolver,
		ISPDB:    autoconfig.ISPDB,
	}
}

type autoconfigBody struct {
	Email string `json:"email"`
}

// lookUpSettings finds the servers of an address's domain, for the form a mail account is added
// with to fill in. Found or not is a 200: the lookup ran, and that is its answer.
//
// Limited per user, because it fetches from wherever the address's domain says. See
// docs/email-configs.md.
func (s *Server) lookUpSettings(w http.ResponseWriter, r *http.Request) {
	var body autoconfigBody
	if !decode(w, r, &body) {
		return
	}
	if !s.lookups.allow(userOf(r).ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many lookups. Wait a minute.")
		return
	}
	found, err := s.finder.Find(r.Context(), body.Email)
	switch {
	case errors.Is(err, autoconfig.ErrNotAnAddress):
		refuse(w, http.StatusBadRequest, CodeInvalid, "That is not an email address.")
		return
	case errors.Is(err, autoconfig.ErrOAuthOnly):
		writeJSON(w, http.StatusOK, map[string]any{"found": false, "oauth_only": true})
		return
	case err != nil:
		s.fail(w, r, err)
		return
	case found == nil:
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}
	out := map[string]any{
		"found":    true,
		"source":   found.Source,
		"domain":   found.Domain,
		"incoming": serverJSON{Protocol: "imap", Host: found.Incoming.Host, Port: found.Incoming.Port, TLS: found.Incoming.TLS, Username: found.Incoming.Username},
		"outgoing": nil,
	}
	if o := found.Outgoing; o != nil {
		out["outgoing"] = serverJSON{Host: o.Host, Port: o.Port, TLS: o.TLS, Username: o.Username}
	}
	writeJSON(w, http.StatusOK, out)
}
