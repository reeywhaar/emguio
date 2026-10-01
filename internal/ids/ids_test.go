package ids

import (
	"testing"
	"time"
)

func TestAULIDSortsChronologically(t *testing.T) {
	base := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC).UnixMilli()
	earlier := New(User, base)
	later := New(User, base+1000)
	if !(earlier < later) {
		t.Errorf("%q should sort before %q", earlier, later)
	}
	if !Valid(User, earlier) || !Valid(User, later) {
		t.Error("a minted ULID does not validate")
	}
}

// Whoever holds the thing can work its id out without asking the server anything.
func TestDeriveIsStableAndPrefixed(t *testing.T) {
	a := Derive(Session, []byte("a secret"))
	b := Derive(Session, []byte("a secret"))
	if a != b {
		t.Errorf("%q and %q differ for one input", a, b)
	}
	if Derive(Session, []byte("another")) == a {
		t.Error("two inputs derived the same id")
	}
	if !Valid(Session, a) {
		t.Errorf("%q does not validate", a)
	}
}

func TestValidRefusesTheWrongShape(t *testing.T) {
	for name, id := range map[string]string{
		"wrong prefix":     Invite + "0123456789abcdefghjkmnpqrs",
		"no prefix":        "0123456789abcdefghjkmnpqrs",
		"wrong length":     User + "abc",
		"outside alphabet": User + "iiiiiiiiiiiiiiiiiiiiiiiiii",
	} {
		if Valid(User, id) {
			t.Errorf("%s: %q validated", name, id)
		}
	}
}
