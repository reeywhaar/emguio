package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// jobs is a user with one email config and its INBOX, Work and Work/Clients, and a way to ask
// for a job on INBOX's message 7-1.
func jobs(t *testing.T) (*Server, *client, *fakeMirror, string, []string, func(kind, extra string) *http.Response) {
	t.Helper()
	s, _, c, cfg, _ := withMail(t, 1)
	fake := &fakeMirror{}
	s.mirror = fake
	var boxes []string
	for _, b := range c.json(c.do("GET", "/api/email-configs/"+cfg+"/mailboxes", ""))["mailboxes"].([]any) {
		boxes = append(boxes, b.(map[string]any)["id"].(string))
	}
	ask := func(kind, extra string) *http.Response {
		return c.do("POST", "/api/jobs", fmt.Sprintf(`{"jobs":[{"email_config":%q,"mailbox":%q,"message":"7-1","kind":%q%s}]}`, cfg, boxes[0], kind, extra))
	}
	return s, c, fake, cfg, boxes, ask
}

// A job is taken at once and done by the mirror: the answer does not wait for the mail server.
func TestAJobIsTakenAtOnceAndTheMirrorIsTold(t *testing.T) {
	_, c, fake, cfg, boxes, ask := jobs(t)
	resp := ask("move", fmt.Sprintf(`,"target":%q,"seen":true`, boxes[1]))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("move = %s %v", resp.Status, c.json(resp))
	}
	var got struct{ Jobs []jobJSON }
	json.NewDecoder(resp.Body).Decode(&got)
	if j := got.Jobs[0]; len(got.Jobs) != 1 || j.Kind != "move" || j.Target != boxes[1] || !j.Seen || j.Message != "7-1" || j.Error != "" {
		t.Errorf("jobs = %+v", got.Jobs)
	}
	if len(fake.kicked) != 1 || fake.kicked[0] != cfg {
		t.Errorf("kicked %v", fake.kicked)
	}
	ask("seen", `,"value":true`)

	// Waiting jobs are listed, in the order asked, for a page opened after they were.
	listed := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any)
	if len(listed) != 2 || listed[0].(map[string]any)["kind"] != "move" || listed[1].(map[string]any)["kind"] != "seen" {
		t.Errorf("jobs = %v", listed)
	}
}

func TestAJobSaysWhatIsWrongWithIt(t *testing.T) {
	_, c, _, _, boxes, ask := jobs(t)
	for name, resp := range map[string]*http.Response{
		"unknown kind":        ask("answer", ""),
		"move to here":        ask("move", fmt.Sprintf(`,"target":%q`, boxes[0])),
		"move to nowhere":     ask("move", `,"target":"INBOX"`),
		"not a message id":    c.do("POST", "/api/jobs", fmt.Sprintf(`{"jobs":[{"email_config":"ec_x","mailbox":%q,"message":"m_1","kind":"seen"}]}`, boxes[0])),
		"none":                c.do("POST", "/api/jobs", `{"jobs":[]}`),
		"a field it does not": ask("seen", `,"draft":true`),
	} {
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %s", name, resp.Status)
		}
	}
}

func TestAnotherUsersMessageTakesNoJob(t *testing.T) {
	s, _, fake, cfg, boxes, _ := jobs(t)
	user(t, s.store, "robin", "a good password")
	robin := newClient(t, s)
	robin.do("POST", "/api/auth/login", `{"username":"robin","password":"a good password"}`)
	resp := robin.do("POST", "/api/jobs", fmt.Sprintf(`{"jobs":[{"email_config":%q,"mailbox":%q,"message":"7-1","kind":"delete"}]}`, cfg, boxes[0]))
	if resp.StatusCode != http.StatusNotFound || len(fake.kicked) != 0 {
		t.Errorf("another user's job = %s, kicked %v", resp.Status, fake.kicked)
	}
	if listed := robin.json(robin.do("GET", "/api/jobs", ""))["jobs"].([]any); len(listed) != 0 {
		t.Errorf("robin sees %v", listed)
	}
}

// A failed job is the user's to let go once shown; a waiting one is not, it may be on the
// server already.
func TestOnlyAFailedJobIsDismissed(t *testing.T) {
	s, c, _, _, _, ask := jobs(t)
	var got struct{ Jobs []jobJSON }
	json.NewDecoder(ask("delete", "").Body).Decode(&got)
	j := got.Jobs[0]
	if resp := c.do("DELETE", "/api/jobs/"+j.ID, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("dismissing a waiting job = %s", resp.Status)
	}
	s.store.FailJob(t.Context(), j.ID, "The server refused: no.")
	listed := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any)
	if listed[0].(map[string]any)["error"] != "The server refused: no." {
		t.Errorf("jobs = %v", listed)
	}
	if resp := c.do("DELETE", "/api/jobs/"+j.ID, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("dismissing a failed job = %s", resp.Status)
	}
	if listed := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any); len(listed) != 0 {
		t.Errorf("after dismissal: %v", listed)
	}
}

// A selection acted on is one request, queued whole or not at all.
func TestASelectionIsQueuedWholeOrNotAtAll(t *testing.T) {
	_, c, fake, cfg, boxes, _ := jobs(t)
	one := func(message, kind, extra string) string {
		return fmt.Sprintf(`{"email_config":%q,"mailbox":%q,"message":%q,"kind":%q%s}`, cfg, boxes[0], message, kind, extra)
	}
	resp := c.do("POST", "/api/jobs", `{"jobs":[`+one("7-1", "seen", `,"value":true`)+`,`+one("7-2", "move", fmt.Sprintf(`,"target":%q`, boxes[2]))+`]}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("two jobs = %s", resp.Status)
	}
	if len(fake.kicked) != 1 {
		t.Errorf("kicked %v, want the config once", fake.kicked)
	}
	bad := c.do("POST", "/api/jobs", `{"jobs":[`+one("7-3", "seen", "")+`,`+one("7-4", "answer", "")+`]}`)
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("one bad job = %s", bad.Status)
	}
	if listed := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any); len(listed) != 2 {
		t.Errorf("jobs = %v, want the first two and nothing of the refused request", listed)
	}
}
