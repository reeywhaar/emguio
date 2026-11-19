package store

import (
	"context"
	"errors"
	"testing"
)

func TestReadingIsTheUsersAndEachEmailConfigCanChooseOtherwise(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := st.Reading(ctx, u.ID); err != nil || got.ReadOnArchive || got.ReadOnTrash {
		t.Fatalf("a new user's reading %+v, %v", got, err)
	}
	if err := st.SetReading(ctx, u.ID, Reading{ReadOnArchive: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Reading(ctx, u.ID); !got.ReadOnArchive || got.ReadOnTrash {
		t.Errorf("kept %+v", got)
	}

	if got, _ := st.EmailConfig(ctx, u.ID, cfg.ID); got.ReadOnArchive != nil || got.ReadOnTrash != nil {
		t.Fatalf("a new config chooses %v, %v", got.ReadOnArchive, got.ReadOnTrash)
	}
	no := false
	if err := st.SetEmailConfigReading(ctx, u.ID, cfg.ID, &no, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := st.EmailConfig(ctx, u.ID, cfg.ID)
	if got.ReadOnArchive == nil || *got.ReadOnArchive || got.ReadOnTrash != nil {
		t.Errorf("chose %v, %v", got.ReadOnArchive, got.ReadOnTrash)
	}
	if err := st.SetEmailConfigReading(ctx, u.ID, "ec_nope", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("another's config: %v", err)
	}
}

// An edit answers with the whole config, what the form does not hold among it.
func TestAnEditedEmailConfigKeepsWhatItsFormDoesNotHold(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	cfg, err := st.CreateEmailConfig(ctx, u.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	yes := true
	st.SetEmailConfigReading(ctx, u.ID, cfg.ID, nil, &yes)
	st.SetEmailConfigOff(ctx, u.ID, cfg.ID, true)
	edited, err := st.UpdateEmailConfig(ctx, u.ID, cfg.ID, draft())
	if err != nil {
		t.Fatal(err)
	}
	if !edited.Off || edited.ReadOnTrash == nil || !*edited.ReadOnTrash {
		t.Errorf("edited: off %v, read on trash %v", edited.Off, edited.ReadOnTrash)
	}
}
