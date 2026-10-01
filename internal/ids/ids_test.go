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

func TestAMessageIdIsReadBackAsItWasWritten(t *testing.T) {
	id := Message(1700000000, 4521)
	if id != "1700000000-4521" {
		t.Errorf("id = %q", id)
	}
	if v, u, ok := ParseMessage(id); !ok || v != 1700000000 || u != 4521 {
		t.Errorf("parsed %d-%d %v", v, u, ok)
	}
	for _, bad := range []string{"", "4521", "1.2", "1-0", "0-1", "01-2", "1-02", "1-2-3", "a-b", "1-4294967296", "+1-2", "1- 2", "-1-2"} {
		if _, _, ok := ParseMessage(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
