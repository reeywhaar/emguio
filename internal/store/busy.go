package store

import (
	"slices"
	"sync"
)

// Working records that the mirror is at work on an email config's mail server until done is
// called: a pass over its folders, or a change asked of it. Its user's busy watchers are told
// when the config starts being worked on and when it stops. Kept in memory: it is about now.
func (s *Store) Working(userID, configID string) (done func()) {
	s.busyMu.Lock()
	s.busy[configID]++
	first := s.busy[configID] == 1
	if first {
		s.busyOf[configID] = userID
	}
	s.busyMu.Unlock()
	if first {
		s.tellBusy(userID)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			s.busyMu.Lock()
			s.busy[configID]--
			last := s.busy[configID] == 0
			if last {
				delete(s.busy, configID)
				delete(s.busyOf, configID)
			}
			s.busyMu.Unlock()
			if last {
				s.tellBusy(userID)
			}
		})
	}
}

// Busy is which of a user's email configs are being worked on now, in id order.
func (s *Store) Busy(userID string) []string {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	out := []string{}
	for id, user := range s.busyOf {
		if user == userID {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// WatchBusy is told when one of a user's email configs starts or stops being worked on, as
// Watch is told of changes: one buffered slot, dropped when full.
func (s *Store) WatchBusy(userID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.busyMu.Lock()
	s.busyWatchers[ch] = userID
	s.busyMu.Unlock()
	return ch, func() {
		s.busyMu.Lock()
		delete(s.busyWatchers, ch)
		s.busyMu.Unlock()
	}
}

func (s *Store) tellBusy(userID string) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	for ch, watching := range s.busyWatchers {
		if watching != userID {
			continue
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
