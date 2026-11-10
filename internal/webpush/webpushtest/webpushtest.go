// Package webpushtest is the browser's half of web push, for tests: a subscription's keys, and a
// push service that opens what it is sent with them.
package webpushtest

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"emguio/internal/webpush"
)

var b64 = base64.RawURLEncoding

// Browser is one subscription's keypair and auth secret.
type Browser struct {
	private *ecdh.PrivateKey
	auth    []byte
}

func NewBrowser(t testing.TB) *Browser {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return &Browser{private: key, auth: auth}
}

// Subscription is what this browser would hand a page, for endpoint.
func (b *Browser) Subscription(endpoint string) webpush.Subscription {
	return webpush.Subscription{
		Endpoint: endpoint,
		P256dh:   b64.EncodeToString(b.private.PublicKey().Bytes()),
		Auth:     b64.EncodeToString(b.auth),
	}
}

// Open decrypts what was sent to this browser.
func (b *Browser) Open(body []byte) ([]byte, error) {
	return Open(body, b.private.Bytes(), b.private.PublicKey().Bytes(), b.auth)
}

// Open is RFC 8291 from the browser's side: read the aes128gcm header, run the key agreement
// against the sender's ephemeral key, and decrypt.
func Open(body, uaPrivateRaw, uaPublicRaw, authSecret []byte) ([]byte, error) {
	if len(body) < 21 || len(body) < 21+int(body[20]) {
		return nil, errors.New("too short for an aes128gcm header")
	}
	idLen := int(body[20])
	salt := body[:16]
	asPublicRaw := body[21 : 21+idLen]
	ciphertext := body[21+idLen:]

	uaPrivate, err := ecdh.P256().NewPrivateKey(uaPrivateRaw)
	if err != nil {
		return nil, err
	}
	asPublic, err := ecdh.P256().NewPublicKey(asPublicRaw)
	if err != nil {
		return nil, err
	}
	shared, err := uaPrivate.ECDH(asPublic)
	if err != nil {
		return nil, err
	}
	keyInfo := append([]byte("WebPush: info\x00"), uaPublicRaw...)
	keyInfo = append(keyInfo, asPublicRaw...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	record, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	if len(record) == 0 || record[len(record)-1] != 0x02 {
		return nil, errors.New("the record does not end as the last one")
	}
	return record[:len(record)-1], nil
}

// Pushed is one message a Service took: its headers, and its payload opened.
type Pushed struct {
	Header  http.Header
	Payload []byte
}

// Service is a push service over TLS for one browser, answering every message with Status.
type Service struct {
	*httptest.Server
	browser *Browser

	mu     sync.Mutex
	got    []Pushed
	status int
}

func NewService(t testing.TB, b *Browser) *Service {
	t.Helper()
	s := &Service{browser: b, status: http.StatusCreated}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.status >= 300 {
			w.WriteHeader(s.status)
			return
		}
		payload, err := b.Open(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.got = append(s.got, Pushed{Header: r.Header.Clone(), Payload: bytes.Clone(payload)})
		w.WriteHeader(s.status)
	}))
	t.Cleanup(s.Close)
	return s
}

// Endpoint is where the browser's subscription points.
func (s *Service) Endpoint() string { return s.URL + "/push/" + b64.EncodeToString(s.browser.auth) }

// Subscription is the browser's, at this service.
func (s *Service) Subscription() webpush.Subscription {
	return s.browser.Subscription(s.Endpoint())
}

// Answer makes the service answer every message with status from now on.
func (s *Service) Answer(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// Got is every message taken so far.
func (s *Service) Got() []Pushed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Pushed(nil), s.got...)
}
