package mirror

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// logged is what the mirror logs from here on, for a test that cares what it said.
func (w *world) logged() *transcript {
	logs := &transcript{}
	w.mirror.log = slog.New(slog.NewTextHandler(logs, nil))
	return logs
}

func (s *transcript) says(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Contains(s.buf.String(), text)
}

// A server closing a connection that has worked a while, as servers do to one held open long
// enough, is opened again at once, and nothing is shown for it.
func TestAWorkingConnectionTheServerClosesIsOpenedAgainQuietly(t *testing.T) {
	was := held
	held = 0
	t.Cleanup(func() { held = was })
	w, front := newFrontedWorld(t)
	logs := w.logged()
	w.running()
	idles := w.said.commands("IDLE")

	front.HangUp()
	eventually(t, func() bool { return w.said.commands("IDLE") > idles })
	if !logs.says("mail sync reconnecting") || logs.says("mail sync failed") {
		t.Errorf("logged %q", logs.buf.String())
	}
	cfg, err := w.store.EmailConfig(context.Background(), w.target.UserID, w.target.ID)
	if err != nil || cfg.SyncError != "" {
		t.Errorf("sync error = %q, %v", cfg.SyncError, err)
	}
}

// One closed before it has worked a minute is a failure, logged and waited after, but one a retry
// usually gets past: the config shows it only once it has lasted.
func TestAConnectionClosedSoonIsAFailureNotYetShown(t *testing.T) {
	w, front := newFrontedWorld(t)
	logs := w.logged()
	w.running()

	front.HangUp()
	eventually(t, func() bool { return logs.says("mail sync failed") })
	if !logs.says("in_a_row=1") || logs.says("mail sync reconnecting") {
		t.Errorf("logged %q", logs.buf.String())
	}
	cfg, err := w.store.EmailConfig(context.Background(), w.target.UserID, w.target.ID)
	if err != nil || cfg.SyncError != "" {
		t.Errorf("sync error = %q, %v", cfg.SyncError, err)
	}
}

// A server that goes on turning connections away is shown, once three tries in a row have failed.
func TestAFailureThatLastsIsShown(t *testing.T) {
	was := backoff
	backoff = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { backoff = was })
	w, front := newFrontedWorld(t)
	logs := w.logged()
	front.Drop(true)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.mirror.Run(ctx)

	var shown string
	eventually(t, func() bool {
		cfg, err := w.store.EmailConfig(context.Background(), w.target.UserID, w.target.ID)
		shown = cfg.SyncError
		return err == nil && shown != ""
	})
	if !strings.Contains(shown, "closed the connection") || !logs.says("in_a_row=3") {
		t.Errorf("shown %q, logged %q", shown, logs.buf.String())
	}
}
