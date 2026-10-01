package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// The suite makes users by the dozen and is not testing bcrypt.
func TestMain(m *testing.M) {
	SetBcryptCost(bcrypt.MinCost)
	os.Exit(m.Run())
}

// A temporary file, not :memory:. WAL behaves differently in memory and WAL is what is being
// relied on, including the two pools.
func openStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenAppliesTheSchema(t *testing.T) {
	st := openStore(t)

	var version int
	if err := st.writer.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version == 0 {
		t.Fatal("nothing was applied")
	}

	for _, table := range []string{"users", "invites", "sessions"} {
		var n int
		if err := st.reader.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("no table %q", table)
		}
	}
}

// A pragma in a DSN is a request. One silently ignored permits orphaned rows for the life of
// the connection, so both pools are read back.
func TestBothPoolsHaveTheirPragmas(t *testing.T) {
	st := openStore(t)
	for name, db := range map[string]*sql.DB{"writer": st.writer, "reader": st.reader} {
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if mode != "wal" {
			t.Errorf("%s journal_mode = %q", name, mode)
		}
		var fk int
		if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}
		if fk != 1 {
			t.Errorf("%s has foreign_keys off", name)
		}
	}
}

// Running twice must be a no-op, or a restart re-applies the schema and fails.
func TestMigrationsAreIdempotent(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()

	st, err = Open(dir)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	st.Close()
}

// A binary that does not understand the schema in front of it cannot know which of its
// statements are still correct.
func TestAVersionAheadOfTheBuildRefusesToOpen(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.writer.Exec("PRAGMA user_version = 9999"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if _, err := Open(dir); err == nil {
		t.Fatal("opened a database written by a newer build")
	}
}

func TestTheClockIsInjectable(t *testing.T) {
	st := openStore(t)
	at := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return at })
	if !st.Now().Equal(at) {
		t.Errorf("now = %v, want %v", st.Now(), at)
	}
}

// The image declares no VOLUME, so a forgotten -v must be loud rather than a database
// written into the container layer and lost on the next replace.
func TestAMissingDataDirectoryRefusesToStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-mounted")
	_, err := Open(missing)
	if err == nil {
		t.Fatal("started without the data directory, and would have lost the database")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error does not name the directory: %v", err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Error("the directory was created; it must be mounted, not invented")
	}
}

func TestOpenRefusesAFileWhereTheDirectoryShouldBe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("accepted a file as the data directory")
	}
}

// A stopped instance should leave a directory holding one whole database rather than three
// files that have to be copied together.
func TestCloseEmptiesTheWriteAheadLog(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Enough writing that the -wal is not empty by accident.
	for i := 0; i < 200; i++ {
		if _, _, err := st.CreateInvite(ctx); err != nil {
			t.Fatal(err)
		}
	}
	wal := filepath.Join(dir, FileName+"-wal")
	if info, err := os.Stat(wal); err != nil || info.Size() == 0 {
		t.Skip("this build does not leave a -wal to truncate")
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(wal); err == nil && info.Size() > 0 {
		t.Errorf("the -wal is still %d bytes after a clean close", info.Size())
	}

	// And what was written is in the file that remains.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var n int
	if err := reopened.reader.QueryRow(`SELECT count(*) FROM invites`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 200 {
		t.Errorf("reopened with %d invites, want 200", n)
	}
}

// Case is not what makes two users different.
func TestUsernamesAreUniqueRegardlessOfCase(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "Misha", "a good password"); err != nil {
		t.Fatal(err)
	}
	_, err := st.CreateUser(ctx, "misha", "a good password")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second user in another case = %v, want a conflict", err)
	}
}

func TestAUsernameSignsInInAnyCase(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	made, err := st.CreateUser(ctx, "Misha", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.Authenticate(ctx, "MISHA", "a good password")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != made.ID || u.Username != "Misha" {
		t.Errorf("signed in as %+v, want %+v with the case it was made with", u, made)
	}
}

// bcrypt reads 72 bytes; a longer password would authenticate against any prefix of itself.
func TestAPasswordLongerThanTheHashReadsIsRefused(t *testing.T) {
	st := openStore(t)
	_, err := st.CreateUser(context.Background(), "misha", strings.Repeat("x", passwordMaxBytes+1))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want invalid", err)
	}
}
