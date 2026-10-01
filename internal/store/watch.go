package store

/*
Watch is told when something a user can see has moved, so a browser does not have to ask.

One buffered slot each and a send that gives up when it is full: a watcher that has not read its
last signal already knows there is something to fetch, and a hundred messages arriving in a
second are one refetch. Dropping is the coalescing.

What a watcher is sent carries nothing — not what changed, only that something did — and it is
scoped to the user, so nobody learns when somebody else's mail arrives.
*/
func (s *Store) Watch(userID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.watchMu.Lock()
	s.watchers[ch] = userID
	s.watchMu.Unlock()

	return ch, func() {
		s.watchMu.Lock()
		delete(s.watchers, ch)
		s.watchMu.Unlock()
	}
}

// Notify wakes one user's watchers.
func (s *Store) Notify(userID string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	for ch, watching := range s.watchers {
		if watching != userID {
			continue
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
