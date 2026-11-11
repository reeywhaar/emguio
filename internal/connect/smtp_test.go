package connect

import (
	"testing"
	"time"
)

// FUTURERELEASE gives the longest hold and the latest date; the earlier of the two bounds it.
func TestTheLatestHoldIsTheEarlierOfItsTwoLimits(t *testing.T) {
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	for limits, want := range map[string]time.Time{
		"86400 2026-12-01T00:00:00Z":    now.Add(24 * time.Hour),
		"31536000 2026-11-20T00:00:00Z": time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		"3600":                          now.Add(time.Hour),
		"":                              {},
		"soon":                          {},
	} {
		if got := latest(limits, now); !got.Equal(want) {
			t.Errorf("%q: %s, want %s", limits, got, want)
		}
	}
}
