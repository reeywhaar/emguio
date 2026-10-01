package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnInvitationIsSpentOnce(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	_, token, err := st.CreateInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.AcceptInvite(ctx, token, "misha", "a good password"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptInvite(ctx, token, "other", "a good password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second acceptance = %v, want the link dead", err)
	}
	if ok, _ := st.HasUsers(ctx); !ok {
		t.Error("the accepted invitation made nobody")
	}
}

// The user and the spending are one transaction, so a refusal leaves the link for another try.
func TestATakenUsernameLeavesTheInvitationUnspent(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	if _, err := st.CreateUser(ctx, "misha", "a good password"); err != nil {
		t.Fatal(err)
	}
	_, token, err := st.CreateInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := st.AcceptInvite(ctx, token, "Misha", "a good password"); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken username = %v, want a conflict", err)
	}
	if _, err := st.InviteByToken(ctx, token); err != nil {
		t.Fatalf("the refused acceptance spent the link: %v", err)
	}
	if _, err := st.AcceptInvite(ctx, token, "robin", "a good password"); err != nil {
		t.Fatalf("second try with a free username = %v", err)
	}
}

func TestAnExpiredInvitationIsDeadAndSwept(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 1, 4, 0, 0, 0, time.UTC)
	st.SetClock(func() time.Time { return at })
	_, token, err := st.CreateInvite(ctx)
	if err != nil {
		t.Fatal(err)
	}

	st.SetClock(func() time.Time { return at.Add(InviteLifetime) })
	if _, err := st.AcceptInvite(ctx, token, "misha", "a good password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired acceptance = %v, want the link dead", err)
	}
	if n, err := st.SweepInvites(ctx); err != nil || n != 1 {
		t.Errorf("swept %d, %v; want the one expired link", n, err)
	}
}

// One sentence for used, expired and never issued: telling them apart tells a stranger which
// tokens were real.
func TestADeadInvitationSaysTheSameWhateverKilledIt(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	_, used, _ := st.CreateInvite(ctx)
	if _, err := st.AcceptInvite(ctx, used, "misha", "a good password"); err != nil {
		t.Fatal(err)
	}

	_, errUsed := st.InviteByToken(ctx, used)
	_, errMissing := st.InviteByToken(ctx, "not-a-token")
	if errUsed == nil || errMissing == nil || errUsed.Error() != errMissing.Error() {
		t.Errorf("used = %v, missing = %v; want one refusal", errUsed, errMissing)
	}
}
