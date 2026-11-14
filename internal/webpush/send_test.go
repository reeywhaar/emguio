package webpush_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"emguio/internal/webpush"
	"emguio/internal/webpush/webpushtest"
)

func TestWhatIsSentOpensInTheBrowser(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := key.PublicKey.ECDH()
	sender := webpush.NewSender(key, pub.Bytes(), "https://mail.example.com")
	service := webpushtest.NewService(t, webpushtest.NewBrowser(t))
	sender.SetClient(service.Client())

	if err := sender.Send(t.Context(), service.Subscription(), []byte("Lunch on Thursday?"),
		webpush.Channel{Topic: "ectest", TTL: 60}); err != nil {
		t.Fatal(err)
	}
	got := service.Got()
	if len(got) != 1 || string(got[0].Payload) != "Lunch on Thursday?" {
		t.Fatalf("the service opened %+v", got)
	}
}
