package store

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
)

func TestTheKeypairIsMadeOnceAndKept(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	_, first, err := st.VAPIDKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key, again, err := st.VAPIDKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) || len(first) != 65 {
		t.Errorf("public keys %x and %x", first, again)
	}
	point, _ := key.PublicKey.ECDH()
	if !bytes.Equal(point.Bytes(), first) {
		t.Error("the public key is not the private key's")
	}
}

func TestRegisteringAnEndpointAgainKeepsItsRow(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	a, err := st.RegisterDevice(ctx, u.ID, phone("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.RegisterDevice(ctx, u.ID, phone("a"))
	if err != nil {
		t.Fatal(err)
	}
	list, _ := st.Devices(ctx, u.ID)
	if a.ID != b.ID || len(list) != 1 {
		t.Errorf("ids %s and %s, %d rows", a.ID, b.ID, len(list))
	}
}

// A browser renews its subscription on its own, and the old endpoint goes on taking messages for
// a while: two rows would be two notifications on one screen.
func TestABrowsersNewSubscriptionReplacesItsOld(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	st.RegisterDevice(ctx, u.ID, phone("a"))
	renewed := phone("a")
	renewed.Endpoint = "https://push.example.com/renewed"
	st.RegisterDevice(ctx, u.ID, renewed)
	other := phone("b")
	other.ClientID = ""
	st.RegisterDevice(ctx, u.ID, other)

	list, _ := st.Devices(ctx, u.ID)
	var endpoints []string
	for _, d := range list {
		endpoints = append(endpoints, d.Endpoint)
	}
	slices.Sort(endpoints)
	if want := []string{other.Endpoint, renewed.Endpoint}; !slices.Equal(endpoints, want) {
		t.Errorf("endpoints %v, want %v", endpoints, want)
	}
}

func TestAnEndpointGoesToWhoeverRegisteredItLast(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	ann, bob := newUser(t, st, "ann"), newUser(t, st, "bob")
	st.RegisterDevice(ctx, ann.ID, phone("shared"))
	st.RegisterDevice(ctx, bob.ID, phone("shared"))
	if list, _ := st.Devices(ctx, ann.ID); len(list) != 0 {
		t.Errorf("ann's mail would still go to bob's browser: %+v", list)
	}
	if list, _ := st.Devices(ctx, bob.ID); len(list) != 1 {
		t.Errorf("bob has %d devices", len(list))
	}
}

func TestAnEndpointIsAnHTTPSAddress(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	u := newUser(t, st, "misha")
	for _, endpoint := range []string{"", "http://push.example.com/a", "https://", "file:///etc/passwd"} {
		in := phone("a")
		in.Endpoint = endpoint
		if _, err := st.RegisterDevice(ctx, u.ID, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", endpoint, err)
		}
	}
}

func TestAnotherUsersDeviceIsNotFound(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	ann, bob := newUser(t, st, "ann"), newUser(t, st, "bob")
	d, _ := st.RegisterDevice(ctx, ann.ID, phone("a"))
	if err := st.DeleteDevice(ctx, bob.ID, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("bob deleting ann's device: %v", err)
	}
}
