package store

import (
	"slices"
	"testing"
)

// Started twice is busy once, and stops with the last; only its user hears of it.
func TestWorkIsCountedAndToldToItsUserOnly(t *testing.T) {
	st := openStore(t)
	told, stop := st.WatchBusy("u_1")
	defer stop()
	other, stopOther := st.WatchBusy("u_2")
	defer stopOther()
	heard := func(ch <-chan struct{}) bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}

	pass := st.Working("u_1", "ec_1")
	if !heard(told) || heard(other) {
		t.Fatal("the start was not told to its user alone")
	}
	job := st.Working("u_1", "ec_1")
	if heard(told) {
		t.Error("more work on a busy config was told again")
	}
	if got := st.Busy("u_1"); !slices.Equal(got, []string{"ec_1"}) {
		t.Errorf("busy %v", got)
	}
	if got := st.Busy("u_2"); len(got) != 0 {
		t.Errorf("another user's busy %v", got)
	}

	pass()
	pass()
	if heard(told) || len(st.Busy("u_1")) != 1 {
		t.Error("a config stopped being busy with work still underway")
	}
	job()
	if !heard(told) || len(st.Busy("u_1")) != 0 {
		t.Errorf("the last stop: busy %v", st.Busy("u_1"))
	}
}
