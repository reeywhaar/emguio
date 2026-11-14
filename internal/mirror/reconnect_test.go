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

// One closed before it has worked a minute is a failure like any other, said and waited after.
func TestAConnectionClosedSoonIsAFailure(t *testing.T) {
	w, front := newFrontedWorld(t)
	logs := w.logged()
	w.running()

	front.HangUp()
	eventually(t, func() bool { return logs.says("mail sync failed") })
	eventually(t, func() bool {
		cfg, err := w.store.EmailConfig(context.Background(), w.target.UserID, w.target.ID)
		return err == nil && cfg.SyncError != ""
	})
	time.Sleep(200 * time.Millisecond)
	if logs.says("mail sync reconnecting") {
		t.Errorf("logged %q", logs.buf.String())
	}
}
