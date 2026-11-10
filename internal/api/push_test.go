package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"emguio/internal/store"
)

func TestADeviceIsRegisteredListedAndRemovedWithoutItsKeys(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)

	resp := c.do("POST", "/api/devices", `{"endpoint":"https://push.example.com/send/abc","p256dh":"BSecretPoint",
		"auth":"secretauth","label":"Safari on iPhone","client_id":"b1"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register = %s %v", resp.Status, c.json(resp))
	}
	var made deviceJSON
	json.NewDecoder(resp.Body).Decode(&made)

	listed := readAll(t, c.do("GET", "/api/devices", ""))
	for _, secret := range []string{"push.example.com", "BSecretPoint", "secretauth"} {
		if strings.Contains(listed, secret) {
			t.Errorf("the list carries %q: %s", secret, listed)
		}
	}
	if !strings.Contains(listed, `"client_id":"b1"`) || !strings.Contains(listed, made.ID) {
		t.Errorf("listed %s", listed)
	}
	if resp := c.do("DELETE", "/api/devices/"+made.ID, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("remove = %s", resp.Status)
	}
	if resp := c.do("DELETE", "/api/devices/"+made.ID, ""); resp.StatusCode != http.StatusNotFound {
		t.Errorf("remove again = %s", resp.Status)
	}
}

func TestAnEndpointThatIsNotHTTPSIsRefused(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	resp := c.do("POST", "/api/devices", `{"endpoint":"http://10.0.0.1/admin","p256dh":"k","auth":"a"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("register = %s", resp.Status)
	}
}

func TestNotificationsAreChosenForAnEmailConfigAndItsFolders(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", configBody(993, 0, "hunter2")).Body).Decode(&made)
	if made.Notify != "now" || made.DigestEvery != 60 {
		t.Fatalf("a new config notifies %q every %d", made.Notify, made.DigestEvery)
	}
	if _, err := st.PutMailboxes(context.Background(), made.ID, []store.Listed{
		{Name: "INBOX", SpecialUse: store.UseInbox, Selectable: true},
		{Name: "Junk", SpecialUse: store.UseJunk, Selectable: true},
	}); err != nil {
		t.Fatal(err)
	}
	var boxes struct{ Mailboxes []mailboxJSON }
	json.NewDecoder(c.do("GET", "/api/email-configs/"+made.ID+"/mailboxes", "").Body).Decode(&boxes)
	if len(boxes.Mailboxes) != 2 || !boxes.Mailboxes[0].Notify || boxes.Mailboxes[1].Notify {
		t.Fatalf("mailboxes %+v", boxes.Mailboxes)
	}

	resp := c.do("PUT", "/api/email-configs/"+made.ID+"/notifications",
		`{"notify":"digest","digest_every":120,"mailboxes":{"`+boxes.Mailboxes[0].ID+`":false}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put = %s %v", resp.Status, c.json(resp))
	}
	got := c.json(resp)
	if got["notify"] != "digest" || got["digest_every"] != float64(120) {
		t.Errorf("answered %v", got)
	}
	json.NewDecoder(c.do("GET", "/api/email-configs/"+made.ID+"/mailboxes", "").Body).Decode(&boxes)
	if boxes.Mailboxes[0].Notify {
		t.Error("INBOX still notifies")
	}

	if resp := c.do("PUT", "/api/email-configs/"+made.ID+"/notifications", `{"notify":"digest","digest_every":45}`); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("every 45 minutes = %s", resp.Status)
	}
}

func TestWithoutAPusherThereIsNoKey(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	if resp := c.do("GET", "/api/push/key", ""); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("key = %s", resp.Status)
	}
}
