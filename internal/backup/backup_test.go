package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"emguio/internal/seal"
	"emguio/internal/store"
)

func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	sealer, _ := seal.New(bytes.Repeat([]byte{7}, seal.KeySize))
	st, err := store.Open(dir, sealer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestAnArchiveIsTheDatabaseSentSized(t *testing.T) {
	dir := t.TempDir()
	st := openStore(t, dir)
	var got []byte
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		// An agent may refuse a chunked body.
		if r.ContentLength != int64(len(got)) || len(r.TransferEncoding) > 0 {
			t.Errorf("sent %d bytes as %d, %v", len(got), r.ContentLength, r.TransferEncoding)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/gzip" {
			t.Errorf("sent as %q", ct)
		}
	}))
	defer agent.Close()

	if err := send(context.Background(), st, agent.URL, dir); err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		// Extracting cannot re-chmod a data directory that is already there.
		if h.Typeflag == tar.TypeDir || h.Mode != 0o600 {
			t.Errorf("%s is type %c, mode %o", h.Name, h.Typeflag, h.Mode)
		}
		names = append(names, h.Name)
	}
	if len(names) != 1 || names[0] != store.FileName {
		t.Errorf("the archive holds %v", names)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, scratch+"*")); len(left) != 0 {
		t.Errorf("a pass left %v", left)
	}
}

// A sign-in is the process noticing somebody, not somebody choosing something.
func TestOnlyWhatPeopleChooseCountsAsAChange(t *testing.T) {
	st := openStore(t, t.TempDir())
	ctx := context.Background()
	before := st.Changes()
	u, err := st.CreateUser(ctx, "misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if st.Changes() == before {
		t.Fatal("a new user is not a change")
	}
	before = st.Changes()
	if _, err := st.CreateSession(ctx, "token", u.ID, "curl/8"); err != nil {
		t.Fatal(err)
	}
	if st.Changes() != before {
		t.Error("a sign-in counted as a change")
	}
	if err := st.SetReading(ctx, u.ID, store.Reading{ReadOnTrash: true}); err != nil {
		t.Fatal(err)
	}
	if st.Changes() == before {
		t.Error("a setting chosen is not a change")
	}
}

func TestTheArchiveIsADatabaseOfItsOwn(t *testing.T) {
	st := openStore(t, t.TempDir())
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "misha", "a good password"); err != nil {
		t.Fatal(err)
	}
	restored := t.TempDir()
	if err := st.SnapshotTo(ctx, filepath.Join(restored, store.FileName)); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, restored)
	if _, err := again.Authenticate(ctx, "misha", "a good password"); err != nil {
		t.Errorf("the snapshot does not hold the user: %v", err)
	}
}

func TestAnAgentThatRefusesIsAFailure(t *testing.T) {
	dir := t.TempDir()
	st := openStore(t, dir)
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer agent.Close()
	if err := send(context.Background(), st, agent.URL, dir); err == nil {
		t.Fatal("a refusing agent was taken for a success")
	}
}

// A pass the process died in leaves its snapshot; the next start removes it, backups on or off,
// and leaves the database alone.
func TestLeftoversAreRemovedOnStart(t *testing.T) {
	dir := t.TempDir()
	st := openStore(t, dir)
	left := filepath.Join(dir, scratch+"123")
	if err := os.MkdirAll(left, 0o700); err != nil {
		t.Fatal(err)
	}
	Run(context.Background(), st, "", dir, slog.New(slog.DiscardHandler))
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Errorf("the leftover is still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, store.FileName)); err != nil {
		t.Errorf("the database went with it: %v", err)
	}
}
