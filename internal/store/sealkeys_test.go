package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"emguio/internal/seal"
)

// After a restart nothing of a user's is read until they unseal, and then what was saved opens.
func TestAUserIsSealedUntilTheyUnsealAfterARestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if state, _ := st.SealState(ctx, u.ID); state != SealKeySealed {
		t.Fatalf("after a restart %s", state)
	}
	if targets, _ := st.SyncTargets(ctx); len(targets) != 0 {
		t.Errorf("a sealed user's accounts are read: %v", targets)
	}
	if _, err := st.CreateEmailConfig(ctx, u.ID, draft()); !errors.Is(err, ErrSealed) {
		t.Errorf("saved while sealed: %v", err)
	}
	if err := st.Unseal(ctx, u.ID, "not the seal key at all"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a wrong key: %v", err)
	}
	if err := st.Unseal(ctx, u.ID, testSealKey); err != nil {
		t.Fatal(err)
	}
	if state, _ := st.SealState(ctx, u.ID); state != SealKeyOpen {
		t.Errorf("unsealed, %s", state)
	}
	edit := draft()
	edit.Incoming.Password = ""
	if l, err := st.Logins(ctx, u.ID, cfg.ID, edit); err != nil || l.Incoming.Password != draft().Incoming.Password {
		t.Errorf("the saved password: %v", err)
	}
	if targets, _ := st.SyncTargets(ctx); len(targets) != 1 {
		t.Errorf("unsealed, %d accounts read", len(targets))
	}
}

// A new seal key opens what the old one did; the old one opens nothing.
func TestTheSealKeyIsChangedWithTheCurrentOne(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	st, _ := Open(dir)
	u := newUser(t, st, "misha")
	cfg, _ := st.CreateEmailConfig(ctx, u.ID, draft())
	const next = "another seal key, longer still"
	if err := st.ChangeSealKey(ctx, u.ID, "not the seal key at all", next); !errors.Is(err, ErrInvalid) {
		t.Errorf("changed with a wrong key: %v", err)
	}
	if err := st.ChangeSealKey(ctx, u.ID, testSealKey, "short"); !errors.Is(err, ErrInvalid) {
		t.Errorf("changed to a short key: %v", err)
	}
	if err := st.ChangeSealKey(ctx, u.ID, testSealKey, next); err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, _ = Open(dir)
	defer st.Close()
	if err := st.Unseal(ctx, u.ID, testSealKey); !errors.Is(err, ErrInvalid) {
		t.Errorf("the old key still opens: %v", err)
	}
	if err := st.Unseal(ctx, u.ID, next); err != nil {
		t.Fatal(err)
	}
	edit := draft()
	edit.Incoming.Password = ""
	if _, err := st.Logins(ctx, u.ID, cfg.ID, edit); err != nil {
		t.Errorf("the saved password under the new key: %v", err)
	}
}

func TestASealKeyIsMadeOnceAndLongEnough(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "misha", "a good password")
	if state, _ := st.SealState(ctx, u.ID); state != SealKeyNone {
		t.Errorf("a new user's %s", state)
	}
	if err := st.CreateSealKey(ctx, u.ID, "too short"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a short key: %v", err)
	}
	if err := st.CreateSealKey(ctx, u.ID, testSealKey); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSealKey(ctx, u.ID, testSealKey); !errors.Is(err, ErrConflict) {
		t.Errorf("a second one: %v", err)
	}
}

// A user from before seal keys keeps their saved passwords with the key emguio ran with then,
// which is their seal key from then on; another key opens nothing of theirs.
func TestTheKeyEmguioRanWithCarriesThePasswordsOver(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "misha", "a good password")
	server := bytes.Repeat([]byte{7}, seal.KeySize)
	old, _ := seal.New(server)
	// Saved as it was before seal keys: sealed with the server's key, and no key of the user's.
	st.keys[u.ID] = old
	cfg, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	st.Lock(u.ID)
	if state, _ := st.SealState(ctx, u.ID); state != SealKeyNone {
		t.Fatalf("before, %s", state)
	}

	wrong := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, seal.KeySize))
	if err := st.CarrySealKey(ctx, u.ID, wrong); !errors.Is(err, ErrInvalid) {
		t.Errorf("another key: %v", err)
	}
	if err := st.CarrySealKey(ctx, u.ID, "not base64 at all, nor 32 bytes"); !errors.Is(err, ErrInvalid) {
		t.Errorf("not a key: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(server)
	if err := st.CarrySealKey(ctx, u.ID, " "+key+"\n"); err != nil {
		t.Fatal(err)
	}
	edit := draft()
	edit.Incoming.Password = ""
	if l, err := st.Logins(ctx, u.ID, cfg.ID, edit); err != nil || l.Incoming.Password != draft().Incoming.Password {
		t.Errorf("the saved password: %v", err)
	}
	st.Lock(u.ID)
	if err := st.Unseal(ctx, u.ID, key); err != nil {
		t.Errorf("the old key is not the seal key: %v", err)
	}
}
