package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"emguio/internal/seal"
)

func draft() EmailConfigInput {
	return EmailConfigInput{
		Name:  "Work",
		Email: "misha@example.com",
		Incoming: Login{
			Server:   Server{Protocol: ProtocolIMAP, Host: "imap.example.com", Port: 993, TLS: TLSImplicit, Username: "misha@example.com"},
			Password: "incoming secret",
		},
	}
}

func withOutgoing(in EmailConfigInput, username, password string) EmailConfigInput {
	in.Outgoing = &Login{
		Server:   Server{Host: "smtp.example.com", Port: 465, TLS: TLSImplicit, Username: username},
		Password: password,
	}
	return in
}

func newUser(t *testing.T, st *Store, name string) *User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), name, "a good password")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func logins(t *testing.T, st *Store, userID, id string, in EmailConfigInput) *Logins {
	t.Helper()
	l, err := st.Logins(context.Background(), userID, id, in)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Another user's config is not found, not forbidden: whether an id exists is not theirs to learn.
func TestAnEmailConfigIsOnlyItsUsers(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	misha, robin := newUser(t, st, "misha"), newUser(t, st, "robin")
	cfg, err := st.CreateEmailConfig(ctx, misha.ID, draft())
	if err != nil {
		t.Fatal(err)
	}

	if list, _ := st.EmailConfigs(ctx, robin.ID); len(list) != 0 {
		t.Errorf("robin lists %d of misha's configs", len(list))
	}
	if _, err := st.EmailConfig(ctx, robin.ID, cfg.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin reading misha's = %v, want not found", err)
	}
	if _, err := st.UpdateEmailConfig(ctx, robin.ID, cfg.ID, draft()); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin editing misha's = %v, want not found", err)
	}
	if err := st.DeleteEmailConfig(ctx, robin.ID, cfg.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin deleting misha's = %v, want not found", err)
	}
	if _, err := st.Logins(ctx, robin.ID, cfg.ID, draft()); !errors.Is(err, ErrNotFound) {
		t.Errorf("robin testing misha's = %v, want not found", err)
	}
	if list, _ := st.EmailConfigs(ctx, misha.ID); len(list) != 1 {
		t.Errorf("misha lists %d configs, want hers", len(list))
	}
}

// A copied database file, or a backup, must not be a list of mail passwords.
func TestAPasswordIsStoredSealed(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, withOutgoing(draft(), "sender", "outgoing secret"))
	if err != nil {
		t.Fatal(err)
	}

	var in, out []byte
	if err := st.reader.QueryRow(`SELECT incoming_secret, outgoing_secret FROM email_configs WHERE id = ?`, cfg.ID).Scan(&in, &out); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(in, []byte("incoming secret")) || bytes.Contains(out, []byte("outgoing secret")) {
		t.Fatal("a password is readable in the database")
	}

	empty := withOutgoing(draft(), "sender", "")
	empty.Incoming.Password = ""
	l := logins(t, st, u.ID, cfg.ID, empty)
	if l.Incoming.Password != "incoming secret" || l.Outgoing.Password != "outgoing secret" {
		t.Errorf("opened %q and %q", l.Incoming.Password, l.Outgoing.Password)
	}
}

// The form shows a saved password as empty, so empty on an edit means "the one I saved".
func TestAnEditThatLeavesThePasswordEmptyKeepsIt(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, _ := st.CreateEmailConfig(ctx, u.ID, draft())

	edit := draft()
	edit.Name = "Personal"
	edit.Incoming.Password = ""
	if _, err := st.UpdateEmailConfig(ctx, u.ID, cfg.ID, edit); err != nil {
		t.Fatal(err)
	}
	if got := logins(t, st, u.ID, cfg.ID, edit).Incoming.Password; got != "incoming secret" {
		t.Errorf("password after the edit = %q", got)
	}

	edit.Incoming.Password = "a new one"
	st.UpdateEmailConfig(ctx, u.ID, cfg.ID, edit)
	edit.Incoming.Password = ""
	if got := logins(t, st, u.ID, cfg.ID, edit).Incoming.Password; got != "a new one" {
		t.Errorf("password after replacing it = %q", got)
	}
}

// Whoever holds a session could otherwise point the host at their own server and press Test.
func TestASavedPasswordIsNeverSentToANewHost(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, _ := st.CreateEmailConfig(ctx, u.ID, withOutgoing(draft(), "", ""))

	moved := withOutgoing(draft(), "", "")
	moved.Incoming.Password = ""
	moved.Incoming.Host = "imap.attacker.example"
	if _, err := st.Logins(ctx, u.ID, cfg.ID, moved); !errors.Is(err, ErrInvalid) {
		t.Errorf("incoming host moved = %v, want a refusal", err)
	}

	// The outgoing server signs in with the incoming password, so moving it is the same theft.
	moved = withOutgoing(draft(), "", "")
	moved.Incoming.Password = ""
	moved.Outgoing.Host = "smtp.attacker.example"
	if _, err := st.Logins(ctx, u.ID, cfg.ID, moved); !errors.Is(err, ErrInvalid) {
		t.Errorf("shared outgoing host moved = %v, want a refusal", err)
	}

	// Typed again, it goes wherever it is told: the person typing it is the one who knows it.
	moved.Incoming.Password = "incoming secret"
	if _, err := st.Logins(ctx, u.ID, cfg.ID, moved); err != nil {
		t.Errorf("with the password typed = %v", err)
	}
}

// An outgoing server with no username of its own signs in as the incoming one does.
func TestAnOutgoingServerCanShareTheIncomingSignIn(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, withOutgoing(draft(), "", "ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Outgoing == nil || cfg.Outgoing.Username != "" {
		t.Fatalf("outgoing = %+v, want one with no username of its own", cfg.Outgoing)
	}

	l := logins(t, st, u.ID, cfg.ID, withOutgoing(draft(), "", ""))
	if l.Outgoing.Username != "misha@example.com" || l.Outgoing.Password != "incoming secret" {
		t.Errorf("outgoing signs in as %q / %q", l.Outgoing.Username, l.Outgoing.Password)
	}

	var secret []byte
	st.reader.QueryRow(`SELECT outgoing_secret FROM email_configs WHERE id = ?`, cfg.ID).Scan(&secret)
	if secret != nil {
		t.Error("a shared sign-in stored a second copy of the password")
	}
}

func TestAnOutgoingServerWithItsOwnUsernameNeedsItsOwnPassword(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	if _, err := st.CreateEmailConfig(ctx, u.ID, withOutgoing(draft(), "sender", "")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("own username, no password = %v, want a refusal", err)
	}

	// Switching from sharing to its own on an edit has nothing stored to keep.
	cfg, _ := st.CreateEmailConfig(ctx, u.ID, withOutgoing(draft(), "", ""))
	edit := withOutgoing(draft(), "sender", "")
	edit.Incoming.Password = ""
	if _, err := st.UpdateEmailConfig(ctx, u.ID, cfg.ID, edit); !errors.Is(err, ErrInvalid) {
		t.Errorf("switched to its own sign-in with no password = %v, want a refusal", err)
	}
}

// Changing the key makes every saved password unreadable, and the form has to say so in words a
// person can act on rather than failing to dial.
func TestAPasswordSealedWithAnotherKeyAsksToBeEnteredAgain(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir, sealer(t))
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, _ := st.CreateEmailConfig(ctx, u.ID, draft())
	st.Close()

	other, _ := seal.New(bytes.Repeat([]byte{8}, seal.KeySize))
	st, err := Open(dir, other)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	edit := draft()
	edit.Incoming.Password = ""
	_, err = st.Logins(ctx, u.ID, cfg.ID, edit)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Enter it again") {
		t.Errorf("err = %v, want a refusal asking for the password", err)
	}
}

func TestADraftIsValidatedBeforeAnythingIsStored(t *testing.T) {
	st := openStore(t)
	u := newUser(t, st, "misha")
	for name, mutate := range map[string]func(*EmailConfigInput){
		"no email":         func(in *EmailConfigInput) { in.Email = "" },
		"not an email":     func(in *EmailConfigInput) { in.Email = "misha" },
		"another protocol": func(in *EmailConfigInput) { in.Incoming.Protocol = "jmap" },
		"no protocol":      func(in *EmailConfigInput) { in.Incoming.Protocol = "" },
		"a url as a host":  func(in *EmailConfigInput) { in.Incoming.Host = "imaps://imap.example.com" },
		"a port in a host": func(in *EmailConfigInput) { in.Incoming.Host = "imap.example.com:993" },
		"no port":          func(in *EmailConfigInput) { in.Incoming.Port = 0 },
		"plain text":       func(in *EmailConfigInput) { in.Incoming.TLS = "none" },
		"no username":      func(in *EmailConfigInput) { in.Incoming.Username = " " },
		"no password":      func(in *EmailConfigInput) { in.Incoming.Password = "" },
	} {
		in := draft()
		mutate(&in)
		if _, err := st.CreateEmailConfig(context.Background(), u.ID, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if list, _ := st.EmailConfigs(context.Background(), u.ID); len(list) != 0 {
		t.Errorf("%d refused drafts were stored", len(list))
	}
}

func TestAHostIsLowercasedAndAnAddressIsAccepted(t *testing.T) {
	st := openStore(t)
	u := newUser(t, st, "misha")
	in := draft()
	in.Incoming.Host = " IMAP.Example.com. "
	cfg, err := st.CreateEmailConfig(context.Background(), u.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Incoming.Host != "imap.example.com" {
		t.Errorf("host = %q", cfg.Incoming.Host)
	}

	in.Incoming.Host = "[2001:db8::1]"
	if cfg, err = st.CreateEmailConfig(context.Background(), u.ID, in); err != nil || cfg.Incoming.Host != "2001:db8::1" {
		t.Errorf("an IPv6 address = %v, %v", cfg, err)
	}
}

func TestDeletingAUserTakesTheirEmailConfigs(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	st.CreateEmailConfig(ctx, u.ID, draft())
	if _, err := st.writer.Exec(`DELETE FROM users WHERE id = ?`, u.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	st.reader.QueryRow(`SELECT count(*) FROM email_configs`).Scan(&n)
	if n != 0 {
		t.Errorf("%d email configs outlived their user", n)
	}
}
