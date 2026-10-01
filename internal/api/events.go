package api

import (
	"fmt"
	"net/http"
	"time"
)

// ping keeps a stream from being collected by something between here and the browser. A proxy
// that sees nothing for a minute is entitled to assume the connection is dead.
const ping = 25 * time.Second

/*
events tells a browser that something changed, so it can ask what.

Server-sent events rather than a websocket: what travels is one word in one direction, the
browser's EventSource reconnects on its own with backoff, and it is plain HTTP.

The message carries nothing. What changed is a question the caller already has endpoints for,
and a payload here would be a second copy of the model to keep true.
*/
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, CodeInternal, "This connection cannot stream.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Nginx buffers a proxied response by default, which holds every event until the stream
	// ends — that is, until the one moment they are no longer worth having.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	changes, stop := s.store.Watch(userOf(r).ID)
	defer stop()

	beat := time.NewTicker(ping)
	defer beat.Stop()

	for {
		select {
		case <-r.Context().Done():
			// The browser went away, or the server is shutting down: serve cancels the context
			// every request is built on, so a held-open stream does not outlast it.
			return
		case <-changes:
			fmt.Fprint(w, "event: changed\ndata: 1\n\n")
			flusher.Flush()
		case <-beat.C:
			// A comment. EventSource ignores it; everything in between counts it as traffic.
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
