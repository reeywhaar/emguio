// Package webpush delivers one encrypted message to one push subscription: RFC 8030 for the
// protocol, RFC 8291 for the encryption, RFC 8292 for naming the sender. See docs/push.md.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MaxBody is what a push service is required to accept, header and tag included.
const MaxBody = 4096

// Channel is one stream of messages as a push service sees it. The topic and the TTL are one
// decision: a push service keeps only the newest undelivered message of a topic.
type Channel struct {
	// Topic is at most 32 characters of the URL-safe alphabet, per RFC 8030.
	Topic string
	// TTL is how long, in seconds, a push service holds a message it could not deliver.
	TTL int
}

// recordSize is what the header says. One record, so it only has to hold the whole payload.
const recordSize = MaxBody

var b64 = base64.RawURLEncoding

// Subscription is what a browser hands the page when it subscribes.
type Subscription struct {
	Endpoint string
	// P256dh is the browser's public key, base64url, 65 uncompressed bytes.
	P256dh string
	// Auth is the shared secret, base64url, 16 bytes.
	Auth string
}

// Sender is the instance's keypair, the contact it names itself by, and one HTTP client.
type Sender struct {
	key     *ecdsa.PrivateKey
	pub     []byte
	subject string
	client  *http.Client
}

// NewSender takes the VAPID keypair and subject, the instance's own address: RFC 8292 allows an
// https URI as well as a mailto:.
func NewSender(key *ecdsa.PrivateKey, pub []byte, subject string) *Sender {
	return &Sender{
		key:     key,
		pub:     pub,
		subject: subject,
		// A push service that hangs must not hold up the next account's notifications.
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// SetClient replaces the HTTP client, for a test's push service.
func (s *Sender) SetClient(c *http.Client) { s.client = c }

// PublicKey is what a browser subscribes with, as applicationServerKey.
func (s *Sender) PublicKey() string { return b64.EncodeToString(s.pub) }

// Send encrypts a payload to one subscription and posts it. An answer from the push service
// that is not a success is a *Failure, so a subscription that is gone can be told from one
// that is busy.
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, ch Channel) error {
	body, err := s.seal(sub, payload)
	if err != nil {
		return err
	}
	if len(body) > MaxBody {
		// A 413 is not retryable: the same bytes would be refused again.
		return &Failure{Reason: ReasonTooLarge, Detail: fmt.Sprintf("%d bytes, limit %d", len(body), MaxBody)}
	}
	auth, err := s.authorization(sub.Endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("TTL", strconv.Itoa(ch.TTL))
	req.Header.Set("Topic", ch.Topic)
	req.Header.Set("Urgency", "normal")

	resp, err := s.client.Do(req)
	if err != nil {
		return &Failure{Reason: ReasonUnreachable, Detail: err.Error()}
	}
	defer resp.Body.Close()
	// A refusal's body is a sentence; one that answers with a page of HTML gets its first lines.
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return classify(resp, string(bytes.TrimSpace(detail)))
}

func (s *Sender) seal(sub Subscription, payload []byte) ([]byte, error) {
	uaPublic, err := b64.DecodeString(strings.TrimRight(sub.P256dh, "="))
	if err != nil {
		return nil, fmt.Errorf("decode subscription key: %w", err)
	}
	authSecret, err := b64.DecodeString(strings.TrimRight(sub.Auth, "="))
	if err != nil {
		return nil, fmt.Errorf("decode subscription auth: %w", err)
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	var salt [16]byte
	rand.Read(salt[:])
	return seal(uaPublic, authSecret, payload, ephemeral, salt[:])
}

// seal is RFC 8291 with the ephemeral key and the salt given, so a test can drive it against the
// RFC's own example.
func seal(uaPublicRaw, authSecret, payload []byte, ephemeral *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaPublic, err := ecdh.P256().NewPublicKey(uaPublicRaw)
	if err != nil {
		return nil, fmt.Errorf("subscription key is not a P-256 point: %w", err)
	}
	shared, err := ephemeral.ECDH(uaPublic)
	if err != nil {
		return nil, fmt.Errorf("key agreement: %w", err)
	}
	asPublic := ephemeral.PublicKey().Bytes()

	// RFC 8291 §3.4: both public keys go into the derivation, so a message for one
	// subscription opens for no other.
	keyInfo := make([]byte, 0, len("WebPush: info\x00")+len(uaPublicRaw)+len(asPublic))
	keyInfo = append(keyInfo, "WebPush: info\x00"...)
	keyInfo = append(keyInfo, uaPublicRaw...)
	keyInfo = append(keyInfo, asPublic...)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, fmt.Errorf("derive ikm: %w", err)
	}
	// RFC 8188 §2.2, the aes128gcm content encoding.
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("derive nonce: %w", err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	// 0x02 marks the last record, which one record is; 0x01 would say more follow.
	record := append(append([]byte{}, payload...), 0x02)
	ciphertext := gcm.Seal(nil, nonce, record, nil)

	// salt(16) ‖ record size(4) ‖ key id length(1) ‖ key id, the ephemeral public key the
	// browser runs the key agreement against.
	out := make([]byte, 0, 16+4+1+len(asPublic)+len(ciphertext))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPublic)))
	out = append(out, asPublic...)
	out = append(out, ciphertext...)
	return out, nil
}

// authorization is RFC 8292's single header: vapid t=<jwt>,k=<public key>.
func (s *Sender) authorization(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	// The push service's origin and no more: scoped to the endpoint, the token would be a
	// bearer token for that one subscription.
	aud := u.Scheme + "://" + u.Host

	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{
		"aud": aud,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": s.subject,
	})
	if err != nil {
		return "", fmt.Errorf("build claims: %w", err)
	}
	signing := header + "." + b64.EncodeToString(claims)

	digest := sha256.Sum256([]byte(signing))
	r, sInt, err := ecdsa.Sign(rand.Reader, s.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign vapid token: %w", err)
	}
	// JWS is r ‖ s, each 32 bytes. SignASN1's DER is refused by every push service as a 400.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	sInt.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ",k=" + b64.EncodeToString(s.pub), nil
}

// Why a send did not land, as a device's last_error says.
const (
	// ReasonGone is 404 or 410: the subscription is no more.
	ReasonGone = "gone"
	// ReasonRefused is 401 or 403: the push service did not accept who sent it.
	ReasonRefused = "refused"
	// ReasonBusy is 429 or 5xx.
	ReasonBusy = "busy"
	// ReasonTooLarge is 413, which the same bytes would meet again.
	ReasonTooLarge = "too-large"
	// ReasonInvalid is any other 4xx, usually a malformed VAPID signature.
	ReasonInvalid = "invalid"
	// ReasonUnreachable is nothing answering: DNS, a refused connection, a timeout.
	ReasonUnreachable = "unreachable"
)

// Failure is a push service's answer, classified.
type Failure struct {
	Reason string
	Status int
	Detail string
}

func (f *Failure) Error() string {
	if f.Status == 0 {
		return fmt.Sprintf("push %s: %s", f.Reason, f.Detail)
	}
	return fmt.Sprintf("push %s (%d): %s", f.Reason, f.Status, f.Detail)
}

// Gone reports whether the subscription is over, and its device is to be deleted rather than
// tried again.
func Gone(err error) bool {
	var f *Failure
	return errors.As(err, &f) && f.Reason == ReasonGone
}

func classify(resp *http.Response, detail string) error {
	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusNotFound, code == http.StatusGone:
		return &Failure{Reason: ReasonGone, Status: code, Detail: detail}
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return &Failure{Reason: ReasonRefused, Status: code, Detail: detail}
	case code == http.StatusRequestEntityTooLarge:
		return &Failure{Reason: ReasonTooLarge, Status: code, Detail: detail}
	case code == http.StatusTooManyRequests, code >= 500:
		return &Failure{Reason: ReasonBusy, Status: code, Detail: detail}
	default:
		return &Failure{Reason: ReasonInvalid, Status: code, Detail: detail}
	}
}
