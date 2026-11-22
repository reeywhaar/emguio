package api

import (
	"context"
	"net/http"
	"testing"
)

func TestTheSessionListShowsThisBrowserAndTheOthers(t *testing.T) {
	s, st := newServerStore(t, nil)
	here := signIn(t, s, st)
	elsewhere := newClient(t, s)
	elsewhere.do("POST", "/api/auth/login", `{"username":"misha","password":"a good password"}`)

	body := here.json(here.do("GET", "/api/sessions", ""))
	list, _ := body["sessions"].([]any)
	if len(list) != 2 {
		t.Fatalf("sessions = %v", body)
	}
	var current int
	for _, row := range list {
		m := row.(map[string]any)
		if m["current"] == true {
			current++
		}
		if m["device"] != "Firefox on a Mac" {
			t.Errorf("device = %v", m["device"])
		}
		if m["user_agent"] == "" {
			t.Error("the user agent is not shown beside the guess")
		}
	}
	if current != 1 {
		t.Errorf("%d rows are the current session", current)
	}
}

// Signing out everywhere else keeps the tab the button was pressed in.
func TestSigningOutOtherSessionsKeepsThisOne(t *testing.T) {
	s, st := newServerStore(t, nil)
	here := signIn(t, s, st)
	elsewhere := newClient(t, s)
	elsewhere.do("POST", "/api/auth/login", `{"username":"misha","password":"a good password"}`)

	if resp := here.do("DELETE", "/api/sessions", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out others = %s", resp.Status)
	}
	if resp := here.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("this session was signed out: %s", resp.Status)
	}
	if resp := elsewhere.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the other session is still signed in: %s", resp.Status)
	}
}

func TestTheCurrentSessionCanBeSignedOut(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	id := c.json(c.do("GET", "/api/sessions", ""))["sessions"].([]any)[0].(map[string]any)["id"].(string)

	if resp := c.do("DELETE", "/api/sessions/"+id, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("sign out = %s", resp.Status)
	}
	if c.cookie != nil {
		t.Error("the cookie outlived its session")
	}
}

// Somebody else's session matches nothing rather than being signed out.
func TestOneUserCannotSignOutAnothersSession(t *testing.T) {
	s, st := newServerStore(t, nil)
	mine := signIn(t, s, st)
	user(t, st, "other", "a good password")
	theirs := newClient(t, s)
	theirs.do("POST", "/api/auth/login", `{"username":"other","password":"a good password"}`)
	theirID := theirs.json(theirs.do("GET", "/api/sessions", ""))["sessions"].([]any)[0].(map[string]any)["id"].(string)

	if list := mine.json(mine.do("GET", "/api/sessions", ""))["sessions"].([]any); len(list) != 1 {
		t.Errorf("my sessions = %v", list)
	}
	if resp := mine.do("DELETE", "/api/sessions/"+theirID, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %s, want 404", resp.Status)
	}
	mine.do("DELETE", "/api/sessions", "")
	if resp := theirs.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Error("somebody else's session was signed out")
	}
}

// A new password signs out every other session and keeps this one; the old one no longer signs in.
func TestChangingThePasswordSignsOutTheOtherSessions(t *testing.T) {
	s, st := newServerStore(t, nil)
	here := signIn(t, s, st)
	elsewhere := newClient(t, s)
	elsewhere.do("POST", "/api/auth/login", `{"username":"misha","password":"a good password"}`)

	if resp := here.do("PUT", "/api/auth/password", `{"current":"not it","password":"a new password"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a wrong current password = %s", resp.Status)
	}
	if resp := elsewhere.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("a refused change signed out the other session: %s", resp.Status)
	}
	if resp := here.do("PUT", "/api/auth/password", `{"current":"a good password","password":"short"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a short new password = %s", resp.Status)
	}

	if resp := here.do("PUT", "/api/auth/password", `{"current":"a good password","password":"a new password"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change = %s %v", resp.Status, here.json(resp))
	}
	if resp := here.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Errorf("this session was signed out: %s", resp.Status)
	}
	if resp := elsewhere.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("the other session is still signed in: %s", resp.Status)
	}
	// The store, since these tries have spent the username's sign-in budget.
	if _, err := st.Authenticate(context.Background(), "misha", "a good password"); err == nil {
		t.Error("the old password still signs in")
	}
	if _, err := st.Authenticate(context.Background(), "misha", "a new password"); err != nil {
		t.Errorf("the new password = %v", err)
	}
}

// Guessing the current password from a session is no faster than guessing at sign-in.
func TestGuessingTheCurrentPasswordIsLimited(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	var last int
	for range 10 {
		last = c.do("PUT", "/api/auth/password", `{"current":"not it","password":"a new password"}`).StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("the tenth guess = %d, want 429", last)
	}
}
