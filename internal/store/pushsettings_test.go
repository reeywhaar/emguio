package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQuietHoursEndWhereTheyAreSet(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	at := func(d, h, m int) time.Time { return time.Date(2026, 9, d, h, m, 0, 0, berlin) }
	night := Quiet{From: 23 * 60, Until: 7*60 + 30, Zone: "Europe/Berlin"}
	lunch := Quiet{From: 12 * 60, Until: 13 * 60, Zone: "Europe/Berlin"}
	for _, c := range []struct {
		name string
		q    Quiet
		t    time.Time
		end  time.Time
	}{
		{"before the night", night, at(1, 22, 59), time.Time{}},
		{"as it starts", night, at(1, 23, 0), at(2, 7, 30)},
		{"past midnight", night, at(2, 3, 0), at(2, 7, 30)},
		{"as it ends", night, at(2, 7, 30), time.Time{}},
		{"in the day", lunch, at(1, 12, 15), at(1, 13, 0)},
		{"after the day's", lunch, at(1, 13, 0), time.Time{}},
	} {
		end, held := c.q.End(c.t)
		if held != !c.end.IsZero() || !end.Equal(c.end) {
			t.Errorf("%s: end %s, held %v; want %s", c.name, end, held, c.end)
		}
	}
}

// Berlin's clocks go back an hour on 2026-10-25: the night is an hour longer, and ends at seven
// all the same.
func TestQuietHoursEndOnTheWallClockAcrossAChangeOfTime(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	q := Quiet{From: 23 * 60, Until: 7 * 60, Zone: "Europe/Berlin"}
	end, held := q.End(time.Date(2026, 10, 24, 23, 30, 0, 0, berlin))
	if want := time.Date(2026, 10, 25, 7, 0, 0, 0, berlin); !held || !end.Equal(want) {
		t.Errorf("end %s, want %s", end, want)
	}
}

func TestPushSettingsAreKeptAndChecked(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.PushSettings(ctx, u.ID); err != nil || got.Silent || got.Quiet != nil {
		t.Fatalf("a new user's settings %+v, %v", got, err)
	}
	want := PushSettings{Silent: true, Quiet: &Quiet{From: 22 * 60, Until: 8 * 60, Zone: "Asia/Tokyo"}}
	if err := st.SetPushSettings(ctx, u.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, err := st.PushSettings(ctx, u.ID); err != nil || !got.Silent || got.Quiet == nil || *got.Quiet != *want.Quiet {
		t.Errorf("kept %+v, %v", got, err)
	}
	for _, q := range []Quiet{
		{From: 8 * 60, Until: 8 * 60, Zone: "Asia/Tokyo"},
		{From: 24 * 60, Until: 8 * 60, Zone: "Asia/Tokyo"},
		{From: 22 * 60, Until: 8 * 60, Zone: ""},
		{From: 22 * 60, Until: 8 * 60, Zone: "Local"},
		{From: 22 * 60, Until: 8 * 60, Zone: "Mars/Olympus"},
	} {
		if err := st.SetPushSettings(ctx, u.ID, PushSettings{Quiet: &q}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want invalid", q, err)
		}
	}
	if err := st.SetPushSettings(ctx, u.ID, PushSettings{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.PushSettings(ctx, u.ID); got.Silent || got.Quiet != nil {
		t.Errorf("cleared %+v", got)
	}
}
