package webpush

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// RFC 8291 §5's worked example. Only this tells what a browser will decrypt from what merely
// agrees with itself: a wrong info string round-trips and fails only at a real push service.
const (
	vectorSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	vectorASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	vectorUAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	vectorAuth      = "BTBZMqHH6r4Tts7J_aSIgg"
	vectorPlaintext = "When I grow up, I want to be a watermelon"
	vectorBody      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

var test = Channel{Topic: "ectest", TTL: 600}

func decode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

func TestSealMatchesRFC8291(t *testing.T) {
	ephemeral, err := ecdh.P256().NewPrivateKey(decode(t, vectorASPrivate))
	if err != nil {
		t.Fatal(err)
	}
	got, err := seal(decode(t, vectorUAPublic), decode(t, vectorAuth), []byte(vectorPlaintext),
		ephemeral, decode(t, vectorSalt))
	if err != nil {
		t.Fatal(err)
	}
	if want := decode(t, vectorBody); !bytes.Equal(got, want) {
		t.Errorf("seal() does not match RFC 8291 §5\n got %x\nwant %x", got, want)
	}
}

func TestSealFramesTheHeader(t *testing.T) {
	ephemeral, _ := ecdh.P256().NewPrivateKey(decode(t, vectorASPrivate))
	body, err := seal(decode(t, vectorUAPublic), decode(t, vectorAuth), []byte(vectorPlaintext),
		ephemeral, decode(t, vectorSalt))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body[:16], decode(t, vectorSalt)) {
		t.Error("the body does not begin with the salt")
	}
	if got := binary.BigEndian.Uint32(body[16:20]); got != recordSize {
		t.Errorf("record size %d, want %d", got, recordSize)
	}
	if body[20] != 65 {
		t.Errorf("key id length %d, want 65", body[20])
	}
	if !bytes.Equal(body[21:86], ephemeral.PublicKey().Bytes()) {
		t.Error("the key id is not the ephemeral public key")
	}
}

func TestAPayloadTooLargeIsNeverSent(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer srv.Close()

	err := testSender(t).Send(t.Context(), Subscription{Endpoint: srv.URL, P256dh: vectorUAPublic, Auth: vectorAuth},
		bytes.Repeat([]byte("x"), MaxBody+1), test)
	var f *Failure
	if !errors.As(err, &f) || f.Reason != ReasonTooLarge {
		t.Fatalf("Send() = %v, want %s", err, ReasonTooLarge)
	}
	if reached {
		t.Error("it was sent anyway")
	}
}

func TestTheAuthorizationIsVAPIDsSingleHeader(t *testing.T) {
	got, err := testSender(t).authorization("https://push.example.com/subscription/abc?token=xyz")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "vapid t=") || !strings.Contains(got, ",k=") {
		t.Fatalf("authorization() = %q", got)
	}
	parts := strings.Split(strings.TrimPrefix(strings.Split(got, ",k=")[0], "vapid t="), ".")
	if len(parts) != 3 {
		t.Fatalf("the token has %d segments", len(parts))
	}
	// r ‖ s; DER would be longer, and refused.
	if sig := decode(t, parts[2]); len(sig) != 64 {
		t.Errorf("the signature is %d bytes, want 64", len(sig))
	}
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(decode(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != "https://push.example.com" {
		t.Errorf("aud %q, want the push service's origin alone", claims.Aud)
	}
	if claims.Sub != "https://mail.example.com" {
		t.Errorf("sub %q, want the instance's address", claims.Sub)
	}
	if exp := time.Unix(claims.Exp, 0); !exp.After(time.Now()) || exp.After(time.Now().Add(24*time.Hour)) {
		t.Errorf("exp %s, want within a day", exp)
	}
}

func TestAGoneSubscriptionIsGone(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unsubscribed", status)
		}))
		err := testSender(t).Send(t.Context(), Subscription{Endpoint: srv.URL, P256dh: vectorUAPublic, Auth: vectorAuth},
			[]byte("hi"), test)
		srv.Close()
		if !Gone(err) {
			t.Errorf("%d: Send() = %v, want gone", status, err)
		}
	}
}

func TestABusyPushServiceIsNotGone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	err := testSender(t).Send(t.Context(), Subscription{Endpoint: srv.URL, P256dh: vectorUAPublic, Auth: vectorAuth},
		[]byte("hi"), test)
	var f *Failure
	if !errors.As(err, &f) || f.Reason != ReasonBusy {
		t.Fatalf("Send() = %v, want %s", err, ReasonBusy)
	}
	if Gone(err) {
		t.Error("busy was taken for gone")
	}
}

func TestTheChannelDecidesTopicAndTTL(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	if err := testSender(t).Send(t.Context(), Subscription{Endpoint: srv.URL, P256dh: vectorUAPublic, Auth: vectorAuth},
		[]byte("hi"), test); err != nil {
		t.Fatal(err)
	}
	if got.Get("Topic") != test.Topic || got.Get("TTL") != strconv.Itoa(test.TTL) {
		t.Errorf("Topic %q and TTL %q, want %q and %d", got.Get("Topic"), got.Get("TTL"), test.Topic, test.TTL)
	}
	if got.Get("Content-Encoding") != "aes128gcm" {
		t.Errorf("Content-Encoding %q", got.Get("Content-Encoding"))
	}
}

// Apple refuses a topic with an underscore as BadWebPushTopic, though RFC 8030 allows it.
func TestATopicPushServicesRefuseIsNotSent(t *testing.T) {
	sent := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	for _, topic := range []string{"", "ec_01m70egwar0rjd12kte9x2ggnh", "ec-test", strings.Repeat("a", 33)} {
		if err := testSender(t).Send(t.Context(), Subscription{Endpoint: srv.URL, P256dh: vectorUAPublic, Auth: vectorAuth},
			[]byte("hi"), Channel{Topic: topic, TTL: 60}); err == nil {
			t.Errorf("topic %q was taken", topic)
		}
	}
	if sent {
		t.Error("a refused topic was sent")
	}
}

func testSender(t *testing.T) *Sender {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	return NewSender(key, pub.Bytes(), "https://mail.example.com")
}
