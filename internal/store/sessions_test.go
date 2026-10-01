package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestALapsedSessionIsNotASession(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return at })
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, "cookie", u.ID, "Firefox"); err != nil {
		t.Fatal(err)
	}

	st.SetClock(func() time.Time { return at.Add(SessionLifetime) })
	if _, err := st.SessionByToken(ctx, "cookie"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("lapsed session = %v, want not found", err)
	}
	if n, err := st.SweepSessions(ctx); err != nil || n != 1 {
		t.Errorf("swept %d, %v; want the one lapsed session", n, err)
	}
}

// Every request would otherwise rewrite the row for a window measured in days.
func TestASessionSlidesAtMostOnceARefresh(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return at })
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, "cookie", u.ID, "Firefox"); err != nil {
		t.Fatal(err)
	}

	st.SetClock(func() time.Time { return at.Add(SessionRefresh / 2) })
	if _, moved, err := st.TouchSession(ctx, "cookie", "Firefox"); err != nil || moved {
		t.Fatalf("touch inside the refresh = %v, %v; want nothing moved", moved, err)
	}

	later := at.Add(SessionRefresh)
	st.SetClock(func() time.Time { return later })
	expires, moved, err := st.TouchSession(ctx, "cookie", "Firefox")
	if err != nil || !moved {
		t.Fatalf("touch after the refresh = %v, %v; want it moved", moved, err)
	}
	if !expires.Equal(later.Add(SessionLifetime)) {
		t.Errorf("expires = %v, want a full lifetime from now", expires)
	}
}

// Deleting a user takes their sessions, or a cookie outlives the person it signed in.
func TestDeletingAUserTakesTheirSessions(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, "cookie", u.ID, "Firefox"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.writer.Exec(`DELETE FROM users WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionByToken(ctx, "cookie"); !errors.Is(err, ErrNotFound) {
		t.Errorf("session after its user = %v, want gone", err)
	}
}
