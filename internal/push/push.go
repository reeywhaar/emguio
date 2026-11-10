// Package push tells a user's devices about their new mail: each message as it arrives, or what
// arrived gathered into a digest, as each email config is set to. See docs/push.md.
package push

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"emguio/internal/connect"
	"emguio/internal/ids"
	"emguio/internal/store"
	"emguio/internal/webpush"
)

// Pusher sends what is due.
type Pusher struct {
	store  *store.Store
	sender *webpush.Sender
	log    *slog.Logger
}

func New(st *store.Store, sender *webpush.Sender, log *slog.Logger) *Pusher {
	return &Pusher{store: st, sender: sender, log: log}
}

// NewSender is the instance's sender, named by its address. It reaches a push service only
// through the screen, since an endpoint is an address a browser handed over.
func NewSender(ctx context.Context, st *store.Store, conn *connect.Connector, subject string) (*webpush.Sender, error) {
	key, pub, err := st.VAPIDKeys(ctx)
	if err != nil {
		return nil, err
	}
	s := webpush.NewSender(key, pub, subject)
	s.SetClient(&http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:           conn.DialContext,
			TLSClientConfig:       &tls.Config{RootCAs: conn.Roots(), MinVersion: tls.VersionTLS12},
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          10,
			IdleConnTimeout:       90 * time.Second,
		},
		// A push service answers where it is asked.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})
	return s, nil
}

// Key is what a browser subscribes with.
func (p *Pusher) Key() string { return p.sender.PublicKey() }

// Run sends what is due until ctx ends: as mail is kept to be told, when a digest's time comes,
// and every minute besides.
func (p *Pusher) Run(ctx context.Context) {
	for {
		next := p.Once(ctx)
		wait := time.Minute
		if !next.IsZero() {
			wait = min(wait, max(next.Sub(p.store.Now()), time.Second))
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.store.Arrivals():
		case <-timer.C:
		}
		timer.Stop()
	}
}

// Once sends every batch that is due, and says when the next will be, zero for none waiting.
func (p *Pusher) Once(ctx context.Context) time.Time {
	batches, next, err := p.store.Due(ctx)
	if err != nil {
		if ctx.Err() == nil {
			p.log.Error("notifications could not be read; they are tried again in a minute", "error", err.Error())
		}
		return time.Time{}
	}
	for _, b := range batches {
		// Settled before it is sent: a notification lost to a crash is better than one told twice.
		if err := p.store.Settle(ctx, b.Settles); err != nil {
			p.log.Error("notifications could not be marked sent, so they were not sent",
				"user", b.UserID, "email_config", b.EmailConfigID, "error", err.Error())
			continue
		}
		notes := compose(b)
		if len(notes) == 0 {
			continue
		}
		devices, err := p.store.Devices(ctx, b.UserID)
		if err != nil {
			p.log.Error("notifications could not be sent: the devices could not be read",
				"user", b.UserID, "email_config", b.EmailConfigID, "error", err.Error())
			continue
		}
		ch := webpush.Channel{Topic: b.EmailConfigID, TTL: int(mailTTL.Seconds())}
		delivered := 0
		for _, n := range notes {
			delivered += p.fanOut(ctx, devices, n, ch)
		}
		p.log.Info("new mail notified", "user", b.UserID, "email_config", b.EmailConfigID,
			"messages", len(b.Arrivals), "notifications", len(notes), "devices", len(devices), "delivered", delivered)
	}
	return next
}

// mailTTL is how long a push service holds a notification for a device that is off: a phone off
// overnight hears in the morning, and of each account only the newest.
const mailTTL = 24 * time.Hour

// Test sends a notification to every one of a user's devices, and says how many took it.
func (p *Pusher) Test(ctx context.Context, userID string) (int, error) {
	devices, err := p.store.Devices(ctx, userID)
	if err != nil {
		return 0, err
	}
	n := notification{Title: "emguio", Body: "New mail will show up like this.", Tag: "test", URL: "/settings"}
	return p.fanOut(ctx, devices, n, webpush.Channel{Topic: "test", TTL: 600}), nil
}

// notification is what the service worker shows: see web/public/sw.js.
type notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Tag is what a later notification with the same tag replaces.
	Tag string `json:"tag"`
	// URL is where a tap opens.
	URL string `json:"url"`
}

// oneByOne is how many arriving together are told each on its own; more are told as a digest.
const oneByOne = 3

// named is how many messages a digest names; the rest it counts.
const named = 5

func compose(b store.Batch) []notification {
	switch n := len(b.Arrivals); {
	case n == 0:
		return nil
	case n == 1 || (!b.Digest && n <= oneByOne):
		out := make([]notification, 0, n)
		// Oldest first, so the newest is on top.
		for i := n - 1; i >= 0; i-- {
			out = append(out, single(b, b.Arrivals[i]))
		}
		return out
	default:
		return []notification{digest(b)}
	}
}

func single(b store.Batch, a store.Arrival) notification {
	title := sender(a)
	if !b.Alone {
		title += " · " + b.Label
	}
	body := cmp.Or(a.Subject, "(no subject)")
	if a.Preview != "" {
		body += "\n" + a.Preview
	}
	msg := ids.Message(a.UIDValidity, a.UID)
	return notification{
		Title: cut(title, 120),
		Body:  cut(body, 400),
		Tag:   a.MailboxID + "/" + msg,
		URL:   "/c/" + b.EmailConfigID + "/" + a.MailboxID + "/" + msg,
	}
}

func digest(b store.Batch) notification {
	title := fmt.Sprintf("%d new messages", len(b.Arrivals))
	if !b.Alone {
		title += " · " + b.Label
	}
	var lines []string
	folder := b.Arrivals[0].MailboxID
	for i, a := range b.Arrivals {
		if a.MailboxID != folder {
			folder = ""
		}
		if i < named {
			lines = append(lines, cut(sender(a)+": "+cmp.Or(a.Subject, "(no subject)"), 100))
		}
	}
	if more := len(b.Arrivals) - named; more > 0 {
		lines = append(lines, fmt.Sprintf("and %d more", more))
	}
	url := "/c/" + b.EmailConfigID
	if folder != "" {
		url += "/" + folder
	}
	return notification{Title: cut(title, 120), Body: strings.Join(lines, "\n"), Tag: b.EmailConfigID, URL: url}
}

func sender(a store.Arrival) string {
	return cmp.Or(a.From.Name, a.From.Email, "Somebody")
}

// cut keeps s to n characters, marking where it was cut.
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// fanOut sends one notification to every device at once, and says how many took it. A device
// whose subscription is over is deleted; one refused for another reason keeps its row and the
// reason.
func (p *Pusher) fanOut(ctx context.Context, devices []*store.Device, n notification, ch webpush.Channel) int {
	payload, err := json.Marshal(n)
	if err != nil {
		return 0
	}
	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok int
	)
	for _, d := range devices {
		wg.Go(func() {
			err := p.sender.Send(ctx, webpush.Subscription{Endpoint: d.Endpoint, P256dh: d.P256dh, Auth: d.Auth}, payload, ch)
			switch {
			case err == nil:
				p.store.DeviceDelivered(ctx, d.ID)
				mu.Lock()
				ok++
				mu.Unlock()
			case webpush.Gone(err):
				p.log.Info("device dropped: its push service says the subscription is over", "user", d.UserID, "device", d.ID,
					"error", err.Error())
				p.store.DropDevice(ctx, d.ID)
			default:
				reason := "unknown"
				var f *webpush.Failure
				if errors.As(err, &f) {
					reason = f.Reason
				}
				p.log.Warn("a push service refused a notification", "user", d.UserID, "device", d.ID,
					"reason", reason, "error", err.Error())
				p.store.DeviceFailed(ctx, d.ID, err.Error())
			}
		})
	}
	wg.Wait()
	return ok
}
