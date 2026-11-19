package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReadingIsChosenForTheUserAndForOneEmailConfig(t *testing.T) {
	s, st := newServerStore(t, nil)
	c := signIn(t, s, st)
	if got := readAll(t, c.do("GET", "/api/reading", "")); got != `{"read_on_archive":false,"read_on_trash":false}`+"\n" {
		t.Errorf("a new user's reading %s", got)
	}
	body := `{"read_on_archive":true,"read_on_trash":false}`
	if resp := c.do("PUT", "/api/reading", body); resp.StatusCode != http.StatusOK {
		t.Fatalf("put = %s %v", resp.Status, c.json(resp))
	}
	if got := readAll(t, c.do("GET", "/api/reading", "")); got != body+"\n" {
		t.Errorf("kept %s", got)
	}

	var made emailConfigJSON
	json.NewDecoder(c.do("POST", "/api/email-configs", configBody(993, 0, "hunter2")).Body).Decode(&made)
	if made.ReadOnArchive != nil || made.ReadOnTrash != nil {
		t.Fatalf("a new config chooses %v, %v", made.ReadOnArchive, made.ReadOnTrash)
	}
	resp := c.do("PUT", "/api/email-configs/"+made.ID+"/reading", `{"read_on_archive":null,"read_on_trash":true}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put = %s %v", resp.Status, c.json(resp))
	}
	var chosen emailConfigJSON
	json.NewDecoder(resp.Body).Decode(&chosen)
	if chosen.ReadOnArchive != nil || chosen.ReadOnTrash == nil || !*chosen.ReadOnTrash {
		t.Errorf("chose %v, %v", chosen.ReadOnArchive, chosen.ReadOnTrash)
	}
	if resp := c.do("PUT", "/api/email-configs/ec_01k6r2t5v7x9z1b3d5f7h9k1m3/reading", `{}`); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another's config = %s", resp.Status)
	}
}
