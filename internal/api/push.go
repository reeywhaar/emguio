package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"emguio/internal/store"
)

// Pusher tells a user's devices about their mail.
type Pusher interface {
	// Key is what a browser subscribes with.
	Key() string
	// Test sends a notification to every one of a user's devices and says how many took it.
	Test(ctx context.Context, userID string) (int, error)
}

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "Notifications are not available here.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": s.push.Key()})
}

// deviceJSON is a device without its endpoint and keys: whoever holds those can put text on its
// screen.
type deviceJSON struct {
	ID string `json:"id"`
	// ClientID is what the browser calls itself, for a browser to know its own row.
	ClientID     string `json:"client_id"`
	Label        string `json:"label"`
	CreatedAt    int64  `json:"created_at"`
	LastOKAt     *int64 `json:"last_ok_at"`
	FailureCount int    `json:"failure_count"`
	LastError    string `json:"last_error"`
}

func deviceOut(d *store.Device) deviceJSON {
	return deviceJSON{
		ID:           d.ID,
		ClientID:     d.ClientID,
		Label:        d.Label,
		CreatedAt:    d.CreatedAt.Unix(),
		LastOKAt:     unixOrNil(d.LastOKAt),
		FailureCount: d.FailureCount,
		LastError:    d.LastError,
	}
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Devices(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]deviceJSON, len(list))
	for i, d := range list {
		out[i] = deviceOut(d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": out})
}

type deviceBody struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	Label    string `json:"label"`
	ClientID string `json:"client_id"`
}

func (s *Server) createDevice(w http.ResponseWriter, r *http.Request) {
	var body deviceBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	d, err := s.store.RegisterDevice(r.Context(), u.ID, store.DeviceInput(body))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("device registered", "user", u.ID, "device", d.ID, "label", d.Label)
	writeJSON(w, http.StatusCreated, deviceOut(d))
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if err := s.store.DeleteDevice(r.Context(), u.ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("device removed", "user", u.ID, "device", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// testDevices sends a notification to every one of the user's devices, which shows the whole way
// there works: permission, subscription, keys, encryption, the service worker.
func (s *Server) testDevices(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if s.push == nil {
		refuse(w, http.StatusServiceUnavailable, CodeUnreachable, "Notifications are not available here.")
		return
	}
	if !s.pushTests.allow(u.ID) {
		refuse(w, http.StatusTooManyRequests, CodeRateLimited, "Too many test notifications. Wait a minute.")
		return
	}
	n, err := s.push.Test(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"delivered": n})
}

type notificationsBody struct {
	Notify string `json:"notify"`
	// DigestEvery is in minutes; 0 keeps the one set.
	DigestEvery int `json:"digest_every"`
	// Mailboxes are folders turned on or off; the others stay as they are.
	Mailboxes map[string]bool `json:"mailboxes"`
}

// putNotifications chooses how an email config tells the user's devices about new mail, and
// which of its folders do. See docs/push.md.
func (s *Server) putNotifications(w http.ResponseWriter, r *http.Request) {
	var body notificationsBody
	if !decode(w, r, &body) {
		return
	}
	u := userOf(r)
	id := r.PathValue("id")
	if err := s.store.SetNotifications(r.Context(), u.ID, id, body.Notify, body.DigestEvery, body.Mailboxes); err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.store.EmailConfig(r.Context(), u.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("notifications chosen", "user", u.ID, "email_config", c.ID, "notify", c.Notify,
		"digest_every", c.DigestEvery, "folders", len(body.Mailboxes))
	s.store.Notify(u.ID)
	writeJSON(w, http.StatusOK, toJSON(c))
}

// quietJSON is quiet hours as a person writes them, on a 24-hour clock.
type quietJSON struct {
	From  string `json:"from"`
	Until string `json:"until"`
	Zone  string `json:"zone"`
}

type pushSettingsJSON struct {
	Silent bool       `json:"silent"`
	Quiet  *quietJSON `json:"quiet"`
}

var clockTime = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

func minutesOf(s string) (int, error) {
	m := clockTime.FindStringSubmatch(s)
	if m == nil {
		return 0, store.Invalid("Quiet hours are written on a 24-hour clock, as 23:00; %q is not.", s)
	}
	h, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	return h*60 + mm, nil
}

func clockOf(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

func pushSettingsOut(p store.PushSettings) pushSettingsJSON {
	out := pushSettingsJSON{Silent: p.Silent}
	if q := p.Quiet; q != nil {
		out.Quiet = &quietJSON{From: clockOf(q.From), Until: clockOf(q.Until), Zone: q.Zone}
	}
	return out
}

func (s *Server) pushSettings(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.PushSettings(r.Context(), userOf(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, pushSettingsOut(p))
}

// putPushSettings chooses how all of a user's notifications come. See docs/push.md.
func (s *Server) putPushSettings(w http.ResponseWriter, r *http.Request) {
	var body pushSettingsJSON
	if !decode(w, r, &body) {
		return
	}
	p := store.PushSettings{Silent: body.Silent}
	if q := body.Quiet; q != nil {
		from, err := minutesOf(q.From)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		until, err := minutesOf(q.Until)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		p.Quiet = &store.Quiet{From: from, Until: until, Zone: q.Zone}
	}
	u := userOf(r)
	if err := s.store.SetPushSettings(r.Context(), u.ID, p); err != nil {
		s.fail(w, r, err)
		return
	}
	s.log.Info("notification settings chosen", "user", u.ID, "silent", p.Silent, "quiet_hours", p.Quiet != nil)
	writeJSON(w, http.StatusOK, pushSettingsOut(p))
}
