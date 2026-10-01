package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"emguio/internal/connect"
	"emguio/internal/connect/connecttest"
)

func configBody(imapPort, smtpPort int, password string) string {
	outgoing := "null"
	if smtpPort != 0 {
		outgoing = fmt.Sprintf(`{"host":%q,"port":%d,"tls":"implicit","username":"","password":""}`, connecttest.Host, smtpPort)
	}
	return fmt.Sprintf(`{"name":"Work","email":"misha@example.com",
		"incoming":{"protocol":"imap","host":%q,"port":%d,"tls":"implicit","username":"misha","password":%q},
		"outgoing":%s}`, connecttest.Host, imapPort, password, outgoing)
}

func TestAnEmailConfigIsSavedListedEditedAndDeleted(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)

	resp := c.do("POST", "/api/email-configs", configBody(993, 465, "hunter2"))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create = %s %v", resp.Status, c.json(resp))
	}
	var made emailConfigJSON
	json.NewDecoder(resp.Body).Decode(&made)
	if made.ID == "" || made.Incoming.Host != connecttest.Host || made.Outgoing == nil {
		t.Fatalf("made = %+v", made)
	}

	list := c.json(c.do("GET", "/api/email-configs", ""))
	if n := len(list["email_configs"].([]any)); n != 1 {
		t.Fatalf("listed %d", n)
	}

	edit := strings.Replace(configBody(993, 0, ""), `"Work"`, `"Personal"`, 1)
	resp = c.do("PUT", "/api/email-configs/"+made.ID, edit)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit = %s %v", resp.Status, c.json(resp))
	}
	got := c.json(resp)
	if got["name"] != "Personal" || got["outgoing"] != nil {
		t.Errorf("edited = %v", got)
	}

	if resp := c.do("DELETE", "/api/email-configs/"+made.ID, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %s", resp.Status)
	}
	if list := c.json(c.do("GET", "/api/email-configs", "")); len(list["email_configs"].([]any)) != 0 {
		t.Errorf("still listed after delete: %v", list)
	}
}

// A password goes in and never comes back out, not even to the user who typed it.
func TestNoPasswordIsEverInAResponse(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	resp := c.do("POST", "/api/email-configs", configBody(993, 465, "hunter2"))
	created := readAll(t, resp)
	listed := readAll(t, c.do("GET", "/api/email-configs", ""))
	for name, body := range map[string]string{"create": created, "list": listed} {
		if strings.Contains(body, "hunter2") || strings.Contains(body, "password") {
			t.Errorf("%s carries the password: %s", name, body)
		}
	}
}

func TestAnotherUsersEmailConfigIsNotFound(t *testing.T) {
	s, st := newServerStore(t, nil)
	misha := signIn(t, s, st)
	var made emailConfigJSON
	json.NewDecoder(misha.do("POST", "/api/email-configs", configBody(993, 0, "hunter2")).Body).Decode(&made)

	user(t, st, "robin", "a good password")
	robin := newClient(t, s)
	robin.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`)
	for _, req := range [][2]string{
		{"PUT", "/api/email-configs/" + made.ID},
		{"DELETE", "/api/email-configs/" + made.ID},
		{"POST", "/api/email-configs/" + made.ID + "/test"},
	} {
		body := ""
		if req[0] != "DELETE" {
			body = configBody(993, 0, "")
		}
		if resp := robin.do(req[0], req[1], body); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s by another user = %s, want 404", req[0], req[1], resp.Status)
		}
	}
}

func TestAnInvalidDraftSaysWhatIsWrong(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	body := strings.Replace(configBody(993, 0, "hunter2"), `"protocol":"imap"`, `"protocol":"pop3"`, 1)
	resp := c.do("POST", "/api/email-configs", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %s", resp.Status)
	}
	if got := c.json(resp); got["code"] != CodeInvalid || !strings.Contains(got["message"].(string), "POP3") {
		t.Errorf("body = %v", got)
	}
}

func TestTestingSignsInToBothServersWithoutSaving(t *testing.T) {
	s, st := newServerStore(t, nil)
	cert := connecttest.NewCert(t)
	s.connector = connect.New(connecttest.Loopback).WithRoots(cert.Pool)
	imapPort := connecttest.IMAP(t, cert, "implicit", "misha", "hunter2")
	smtpPort := connecttest.SMTP(t, cert, "implicit", "misha", "hunter2")
	c := signIn(t, s, st)

	got := c.json(c.do("POST", "/api/email-configs/test", configBody(imapPort, smtpPort, "hunter2")))
	in, out := got["incoming"].(map[string]any), got["outgoing"].(map[string]any)
	if in["ok"] != true || out["ok"] != true {
		t.Fatalf("result = %v", got)
	}
	if list := c.json(c.do("GET", "/api/email-configs", "")); len(list["email_configs"].([]any)) != 0 {
		t.Error("testing saved the draft")
	}

	got = c.json(c.do("POST", "/api/email-configs/test", configBody(imapPort, smtpPort, "wrong")))
	in = got["incoming"].(map[string]any)
	if in["ok"] != false || !strings.Contains(in["message"].(string), "refused the username or password") {
		t.Errorf("wrong password = %v", got)
	}
}

// The form shows a saved password as empty, and testing an edit must use the saved one.
func TestTestingASavedConfigUsesItsSavedPassword(t *testing.T) {
	s, st := newServerStore(t, nil)
	cert := connecttest.NewCert(t)
	s.connector = connect.New(connecttest.Loopback).WithRoots(cert.Pool)
	imapPort := connecttest.IMAP(t, cert, "implicit", "misha", "hunter2")
	c := signIn(t, s, st)

	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", configBody(imapPort, 0, "hunter2")).Body).Decode(&made)

	got := c.json(c.do("POST", "/api/email-configs/"+made.ID+"/test", configBody(imapPort, 0, "")))
	if in := got["incoming"].(map[string]any); in["ok"] != true {
		t.Errorf("result = %v", got)
	}
	if got["outgoing"] != nil {
		t.Errorf("tested an outgoing server that is not there: %v", got)
	}

	resp := c.do("POST", "/api/email-configs/test", configBody(imapPort, 0, ""))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("an unsaved draft with no password = %s, want 400", resp.Status)
	}
}

// It dials whatever it is given, so a loop of these must not be free.
func TestTestingIsRateLimited(t *testing.T) {
	s, st := newServerStore(t, nil)
	s.connector = connect.New(nil)
	c := signIn(t, s, st)
	var limited bool
	for i := 0; i < 15 && !limited; i++ {
		limited = c.do("POST", "/api/email-configs/test", configBody(993, 0, "hunter2")).StatusCode == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("fifteen tests in a row were never slowed down")
	}
}

func TestEmailConfigsNeedASession(t *testing.T) {
	s := newServer(t, nil)
	for _, req := range [][2]string{
		{"GET", "/api/email-configs"},
		{"POST", "/api/email-configs"},
		{"POST", "/api/email-configs/test"},
	} {
		if resp := newClient(t, s).do(req[0], req[1], ""); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s signed out = %s", req[0], req[1], resp.Status)
		}
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
