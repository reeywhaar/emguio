package api

import (
	"fmt"
	"net/http"
	"testing"
)

// A turned-off account is kept, and its settings still change, but nothing reaches its mail
// server until it is turned on again: its mail, its jobs, a transfer to it.
func TestATurnedOffAccountIsKeptAndLeftAlone(t *testing.T) {
	_, c, fake, cfg, boxes, ask := jobs(t)
	messages := "/api/email-configs/" + cfg + "/mailboxes/" + boxes[0] + "/messages"

	off := c.json(c.do("PUT", "/api/email-configs/"+cfg+"/off", `{"off":true}`))
	if off["off"] != true || fake.reconciled != 1 {
		t.Fatalf("turned off = %v, reconciled %d", off, fake.reconciled)
	}
	for name, resp := range map[string]*http.Response{
		"its messages": c.do("GET", messages, ""),
		"a job":        ask("seen", `,"value":true`),
		"a fetch":      c.do("POST", "/api/email-configs/"+cfg+"/sync", ""),
	} {
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("%s = %s", name, resp.Status)
		}
	}
	if resp := c.do("PUT", "/api/email-configs/"+cfg+"/archive", fmt.Sprintf(`{"mailbox":%q}`, boxes[1])); resp.StatusCode != http.StatusOK {
		t.Errorf("choosing its archive folder = %s", resp.Status)
	}
	if listed := c.json(c.do("GET", "/api/email-configs", ""))["email_configs"].([]any); len(listed) != 1 || listed[0].(map[string]any)["off"] != true {
		t.Errorf("listed = %v", listed)
	}

	c.do("PUT", "/api/email-configs/"+cfg+"/off", `{"off":false}`)
	if resp := c.do("GET", messages, ""); resp.StatusCode != http.StatusOK {
		t.Errorf("turned on, its messages = %s", resp.Status)
	}

}
