package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emguio/internal/session"
	"emguio/internal/store"
)

// client keeps a cookie jar of one, which is all a session is.
type client struct {
	t      *testing.T
	server *Server
	cookie *http.Cookie
}

func newClient(t *testing.T, s *Server) *client { return &client{t: t, server: s} }

func (c *client) do(method, path, body string) *http.Response {
	c.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
		r.ContentLength = 0
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Firefox/140.0")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.server.ServeHTTP(w, r)
	resp := w.Result()
	for _, ck := range resp.Cookies() {
		if ck.Name == session.CookieName {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	return resp
}

func (c *client) json(resp *http.Response) map[string]any {
	c.t.Helper()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

// sealKey is every test user's seal key.
const sealKey = "a seal key for the tests"

// user is a user with a seal key, open.
func user(t *testing.T, st *store.Store, username, password string) *store.User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), username, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSealKey(context.Background(), u.ID, sealKey); err != nil {
		t.Fatal(err)
	}
	return u
}

func signIn(t *testing.T, s *Server, st *store.Store) *client {
	t.Helper()
	user(t, st, "misha", "a good password")
	c := newClient(t, s)
	if resp := c.do("POST", "/api/auth/login", `{"username":"misha","password":"a good password"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login = %s", resp.Status)
	}
	if c.cookie == nil {
		t.Fatal("signing in set no cookie")
	}
	return c
}

func TestSignInAndOut(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)

	me := c.json(c.do("GET", "/api/auth/me", ""))
	if me["username"] != "misha" {
		t.Fatalf("me = %v", me)
	}

	if resp := c.do("POST", "/api/auth/logout", ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %s", resp.Status)
	}
	if c.cookie != nil {
		t.Error("signing out left the cookie in place")
	}
	if resp := c.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("me after logout = %s, want 401", resp.Status)
	}
}

// A copied cookie must stop working at sign-out, not only the one in this browser.
func TestSigningOutEndsTheSessionNotJustTheCookie(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	copied := *c.cookie

	c.do("POST", "/api/auth/logout", "")

	thief := newClient(t, s)
	thief.cookie = &copied
	if resp := thief.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a copy of the cookie after sign-out = %s, want 401", resp.Status)
	}
}

// One refusal for a wrong password and a missing user: response latency and wording are
// otherwise a list of which usernames exist.
func TestAWrongPasswordAndAMissingUserRefuseIdentically(t *testing.T) {
	s, st := newServerStore(t, nil)
	user(t, st, "misha", "a good password")
	c := newClient(t, s)

	wrong := c.do("POST", "/api/auth/login", `{"username":"misha","password":"not it"}`)
	missing := c.do("POST", "/api/auth/login", `{"username":"nobody","password":"not it"}`)

	if wrong.StatusCode != http.StatusUnauthorized || missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("statuses = %s and %s", wrong.Status, missing.Status)
	}
	a, b := c.json(wrong), c.json(missing)
	if a["message"] != b["message"] || a["code"] != b["code"] {
		t.Errorf("refusals differ: %v vs %v", a, b)
	}
}

// Never a redirect: a 302 to an HTML page is the least useful thing a fetch can receive.
func TestAnUnauthenticatedAPICallIsJSONAndNotARedirect(t *testing.T) {
	s := newServer(t, nil)
	resp := newClient(t, s).do("GET", "/api/auth/me", "")

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %s", resp.Status)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("redirected to %q", loc)
	}
	var body errorBody
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Ok || body.Code != CodeUnauthenticated {
		t.Errorf("body = %+v", body)
	}
}

func TestTheSessionCookieIsHttpOnlyAndSecure(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)

	if !c.cookie.HttpOnly {
		t.Error("the cookie is readable from script")
	}
	if !c.cookie.Secure {
		t.Error("an https instance shipped the cookie without Secure")
	}
	if c.cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.cookie.SameSite)
	}
}

// Per username, so somebody working through a password list against one user does not get to
// share the global budget with everybody else.
func TestLoginIsRateLimitedPerUsername(t *testing.T) {
	s, st := newServerStore(t, nil)
	user(t, st, "misha", "a good password")
	c := newClient(t, s)

	var limited bool
	for i := 0; i < 12; i++ {
		resp := c.do("POST", "/api/auth/login", `{"username":"misha","password":"wrong"}`)
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("a password list against one user was never slowed down")
	}

	// Another user is unaffected, which is the point of the second bucket.
	user(t, st, "robin", "a good password")
	if resp := c.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`); resp.StatusCode != http.StatusNoContent {
		t.Errorf("a second user was locked out by the first: %s", resp.Status)
	}
}

func TestAnInvitationMakesOneUserAndSignsThemIn(t *testing.T) {
	s, st := newServerStore(t, nil)
	_, token, err := st.CreateInvite(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	c := newClient(t, s)
	if resp := c.do("GET", "/api/auth/invites/"+token, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("a live invitation = %s", resp.Status)
	}

	// A seal key that will not do leaves the invitation unspent.
	short := `{"username":"misha","password":"a good password","seal_key":"short"}`
	if resp := c.do("POST", "/api/auth/invites/"+token+"/accept", short); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a short seal key = %s", resp.Status)
	}
	body := `{"username":"misha","password":"a good password","seal_key":"` + sealKey + `"}`
	if resp := c.do("POST", "/api/auth/invites/"+token+"/accept", body); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("accept = %s", resp.Status)
	}
	// Being shown a login form straight afterwards is asking somebody to prove something they
	// just proved.
	if c.cookie == nil {
		t.Fatal("accepting an invitation did not sign the user in")
	}
	// Their seal key is open: they made it a moment ago.
	if me := c.json(c.do("GET", "/api/auth/me", "")); me["username"] != "misha" || me["seal"] != "open" {
		t.Errorf("me = %v", me)
	}

	second := newClient(t, s)
	if resp := second.do("GET", "/api/auth/invites/"+token, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a spent invitation = %s, want 404", resp.Status)
	}
	if resp := second.do("POST", "/api/auth/invites/"+token+"/accept", `{"username":"robin","password":"a good password","seal_key":"`+sealKey+`"}`); resp.StatusCode == http.StatusNoContent {
		t.Error("one invitation made two users")
	}
}

// The store's own sentence reaches the page, because it was written for whoever reads it.
func TestATakenUsernameSaysSo(t *testing.T) {
	s, st := newServerStore(t, nil)
	user(t, st, "misha", "a good password")
	_, token, _ := st.CreateInvite(context.Background())

	c := newClient(t, s)
	resp := c.do("POST", "/api/auth/invites/"+token+"/accept", `{"username":"Misha","password":"a good password","seal_key":"`+sealKey+`"}`)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %s, want 409", resp.Status)
	}
	if got := c.json(resp); got["code"] != CodeConflict || got["message"] != "That username is taken." {
		t.Errorf("body = %v", got)
	}
}

// A session that slides re-issues the cookie, or the browser drops it at the old expiry while
// the row is still live.
func TestASlidingSessionReissuesItsCookie(t *testing.T) {
	s, st := newServerStore(t, nil)
	at := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return at })
	c := signIn(t, s, st)
	first := c.cookie.Expires

	st.SetClock(func() time.Time { return at.Add(store.SessionRefresh) })
	if resp := c.do("GET", "/api/auth/me", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("me = %s", resp.Status)
	}
	if !c.cookie.Expires.After(first) {
		t.Errorf("cookie expires %v after sliding, want later than %v", c.cookie.Expires, first)
	}
}

// Sealed, a user reaches nothing but signing out, themselves and their seal key; unsealed with
// the right key, everything.
func TestASealedUserUnsealsWithTheirKey(t *testing.T) {
	s, st := newServerStore(t, nil)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)
	c.do("POST", "/api/auth/login", `{"username":"misha","password":"a good password"}`)

	// A user from before seal keys makes one.
	if me := c.json(c.do("GET", "/api/auth/me", "")); me["seal"] != "none" {
		t.Fatalf("me = %v", me)
	}
	if resp := c.do("GET", "/api/email-configs", ""); resp.StatusCode != http.StatusLocked || c.json(resp)["code"] != CodeSealed {
		t.Fatalf("sealed, the list = %s", resp.Status)
	}
	if resp := c.do("POST", "/api/seal", `{"key":"`+sealKey+`"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("make = %s %v", resp.Status, c.json(resp))
	}
	if resp := c.do("GET", "/api/email-configs", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("open, the list = %s", resp.Status)
	}

	// As after a restart.
	st.Lock(u.ID)
	if me := c.json(c.do("GET", "/api/auth/me", "")); me["seal"] != "sealed" {
		t.Fatalf("me = %v", me)
	}
	if resp := c.do("POST", "/api/seal/unseal", `{"key":"not the seal key at all"}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a wrong key = %s", resp.Status)
	}
	if resp := c.do("POST", "/api/seal/unseal", `{"key":"`+sealKey+`"}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unseal = %s %v", resp.Status, c.json(resp))
	}
	if resp := c.do("PUT", "/api/seal", `{"current":"`+sealKey+`","key":"another seal key, longer still"}`); resp.StatusCode != http.StatusNoContent {
		t.Errorf("change = %s %v", resp.Status, c.json(resp))
	}
	if resp := c.do("POST", "/api/seal/reset", `{"key":"a third seal key, the second forgotten"}`); resp.StatusCode != http.StatusNoContent {
		t.Errorf("reset = %s %v", resp.Status, c.json(resp))
	}
}
