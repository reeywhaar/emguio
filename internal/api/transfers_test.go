package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"emguio/internal/store"
)

// A message goes to another of the user's accounts as a transfer job, one transfer for the
// request, which is listed with how far it has got, cancelled, and let go once finished.
func TestATransferIsAskedListedAndCancelled(t *testing.T) {
	s, c, fake, cfg, boxes, ask := jobs(t)
	ctx := context.Background()
	u, _ := s.store.Authenticate(ctx, "misha", "a good password")
	other, err := s.store.CreateEmailConfig(ctx, u.ID, store.EmailConfigInput{
		Email:    "ann@example.org",
		Incoming: store.Login{Server: store.Server{Protocol: store.ProtocolIMAP, Host: "imap.example.org", Port: 993, TLS: store.TLSImplicit, Username: "ann"}, Password: "pw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.store.PutMailboxes(ctx, other.ID, []store.Listed{{Name: "Archive", SpecialUse: store.UseArchive, Selectable: true}})
	theirs, _ := s.store.MirrorMailboxes(ctx, other.ID)

	if resp := ask("transfer", fmt.Sprintf(`,"target":%q`, boxes[1])); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a transfer within the account = %s", resp.Status)
	}
	resp := ask("transfer", fmt.Sprintf(`,"target":%q`, theirs[0].ID))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("transfer = %s %v", resp.Status, c.json(resp))
	}
	var got struct{ Jobs []jobJSON }
	json.NewDecoder(resp.Body).Decode(&got)
	tr := got.Jobs[0].Transfer
	if len(fake.kicked) != 1 || fake.kicked[0] != cfg || tr == "" {
		t.Errorf("kicked %v, transfer %q", fake.kicked, tr)
	}

	listed := c.json(c.do("GET", "/api/transfers", ""))["transfers"].([]any)
	if len(listed) != 1 {
		t.Fatalf("transfers = %v", listed)
	}
	if one := listed[0].(map[string]any); one["id"] != tr || one["state"] != "moving" || one["total"] != 1.0 ||
		one["target_config"] != other.ID || one["target"] != theirs[0].ID {
		t.Errorf("transfer = %v", one)
	}
	if resp := c.do("DELETE", "/api/transfers/"+tr, ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("dismissing one moving = %s", resp.Status)
	}

	cancelled := c.json(c.do("POST", "/api/transfers/"+tr+"/cancel", ""))
	if cancelled["state"] != "cancelled" {
		t.Errorf("cancelled = %v", cancelled)
	}
	if j := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any)[0].(map[string]any); j["error"] != "Cancelled." {
		t.Errorf("its job = %v", j)
	}

	fake.kicked = nil
	again := c.json(c.do("POST", "/api/transfers/"+tr+"/retry", ""))
	if again["state"] != "moving" || len(fake.kicked) != 1 || fake.kicked[0] != cfg {
		t.Errorf("tried again = %v, kicked %v", again, fake.kicked)
	}
	if j := c.json(c.do("GET", "/api/jobs", ""))["jobs"].([]any)[0].(map[string]any); j["error"] != "" {
		t.Errorf("its job = %v", j)
	}
	if err := s.store.SetEmailConfigOff(ctx, u.ID, other.ID, true); err != nil {
		t.Fatal(err)
	}
	c.do("POST", "/api/transfers/"+tr+"/cancel", "")
	if resp := c.do("POST", "/api/transfers/"+tr+"/retry", ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("tried again into an account turned off = %s", resp.Status)
	}

	if resp := c.do("DELETE", "/api/transfers/"+tr, ""); resp.StatusCode != http.StatusNoContent {
		t.Errorf("dismissing = %s", resp.Status)
	}
	if listed := c.json(c.do("GET", "/api/transfers", ""))["transfers"].([]any); len(listed) != 0 {
		t.Errorf("left = %v", listed)
	}
}
