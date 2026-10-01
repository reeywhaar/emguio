// Package sweep deletes what is due, on a timer.
//
// One loop, each pass with its own interval, logging when it deletes something and staying
// silent when it does not.
package sweep

import (
	"context"
	"log/slog"
	"time"

	"emguio/internal/store"
)

// How often each pass runs.
const (
	Sessions = 10 * time.Minute
	Hourly   = time.Hour
)

// Run sweeps until ctx is done.
//
// A ticker rather than a chain that reschedules itself: the spacing is identical, but a chain
// has no owner — lose the goroutine between finishing one pass and queueing the next and the
// work stops with nothing to notice.
func Run(ctx context.Context, st *store.Store, log *slog.Logger) {
	sessions := time.NewTicker(Sessions)
	defer sessions.Stop()
	hourly := time.NewTicker(Hourly)
	defer hourly.Stop()

	Once(ctx, st, log)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sessions.C:
			run(ctx, log, "expired sessions", st.SweepSessions)
		case <-hourly.C:
			Once(ctx, st, log)
		}
	}
}

// Once runs every pass.
func Once(ctx context.Context, st *store.Store, log *slog.Logger) {
	run(ctx, log, "expired sessions", st.SweepSessions)
	run(ctx, log, "invites", st.SweepInvites)
}

func run(ctx context.Context, log *slog.Logger, what string, f func(context.Context) (int64, error)) {
	n, err := f(ctx)
	switch {
	case err != nil:
		log.Error("sweep failed", "what", what, "err", err)
	case n > 0:
		log.Info("swept", "what", what, "count", n)
	}
}
