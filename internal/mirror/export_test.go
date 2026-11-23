package mirror

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
)

// Every message of a folder, oldest first, with its flags, its arrival and its whole text; or
// those asked for.
func TestAFolderIsExportedOneMessageAtATime(t *testing.T) {
	w := newWorld(t, "hunter2")
	ctx := context.Background()
	w.create("Archive")
	w.deliver("Archive", 2, "bob@example.com", "Second", imap.FlagSeen, imap.FlagFlagged)
	w.deliver("Archive", 1, "alice@example.com", "First")
	w.deliver("Archive", 3, "carol@example.com", "Third", imap.FlagAnswered)

	var got []Exporting
	var texts []string
	uidValidity, err := w.mirror.Export(ctx, w.target, "Archive", 0, nil, func(e Exporting, body io.Reader) error {
		b, err := io.ReadAll(body)
		got, texts = append(got, e), append(texts, string(b))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || uidValidity == 0 {
		t.Fatalf("exported %+v under %d", got, uidValidity)
	}
	if got[0].From != "bob@example.com" || !got[0].Seen || !got[0].Flagged || got[0].Answered ||
		!got[0].Date.Equal(start.Add(2*time.Minute)) {
		t.Errorf("first = %+v", got[0])
	}
	if !got[2].Answered || got[2].Seen {
		t.Errorf("third = %+v", got[2])
	}
	if !strings.Contains(texts[1], "Subject: First\r\n") || !strings.HasSuffix(texts[1], "Hello.\r\n") {
		t.Errorf("text = %q", texts[1])
	}

	got = nil
	if _, err := w.mirror.Export(ctx, w.target, "Archive", uidValidity, []uint32{3, 1}, func(e Exporting, body io.Reader) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].UID != 1 {
		t.Errorf("asked for two, got %+v", got)
	}
	if _, err := w.mirror.Export(ctx, w.target, "Archive", uidValidity+1, []uint32{1}, func(Exporting, io.Reader) error { return nil }); err != ErrGone {
		t.Errorf("under another UIDVALIDITY = %v", err)
	}
}

// What was exported goes to Trash, or for good from Trash; what came after stays.
func TestWhatWasExportedIsTakenOut(t *testing.T) {
	w := newWorld(t, "hunter2")
	ctx := context.Background()
	w.create("Archive", "Trash")
	w.deliver("Archive", 1, "alice@example.com", "Exported")
	w.deliver("Archive", 2, "bob@example.com", "Exported too")
	uidValidity, err := w.mirror.Export(ctx, w.target, "Archive", 0, nil, func(Exporting, io.Reader) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	w.deliver("Archive", 3, "carol@example.com", "Came after")

	took, err := w.mirror.Discard(ctx, w.target, "Archive", uidValidity, []uint32{1, 2, 9}, "Trash", true)
	if err != nil || len(took) != 2 {
		t.Fatalf("took %v, %v", took, err)
	}
	st, err := w.server.Status("Archive", &imap.StatusOptions{NumMessages: true}).Wait()
	if err != nil || deref(st.NumMessages) != 1 {
		t.Errorf("Archive holds %v, %v", st, err)
	}
	if _, err := w.mirror.Discard(ctx, w.target, "Archive", uidValidity+1, []uint32{3}, "", false); err != ErrGone {
		t.Errorf("under another UIDVALIDITY = %v", err)
	}
}
