package config

import (
	"log/slog"
	"net/netip"
	"strings"
	"testing"
)

// key is 32 bytes of 0x01, base64.
const key = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="

// set clears everything this package reads, so no test passes on another's leftovers, and
// supplies a key unless the test says otherwise.
func set(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{PublicURLEnv, DataDirEnv, LogLevelEnv, SecretKeyEnv, AllowNetworksEnv} {
		t.Setenv(k, "")
	}
	t.Setenv(SecretKeyEnv, key)
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestThePublicURLIsRequired(t *testing.T) {
	set(t, nil)
	if _, err := Load(); err == nil {
		t.Fatal("started with no public url, which would leave invitation links to a guess")
	}
}

// It cannot be inferred from a request, so a bad one must fail here.
func TestThePublicURLIsValidated(t *testing.T) {
	for name, raw := range map[string]string{
		"no scheme":    "mail.example.com",
		"wrong scheme": "ftp://mail.example.com",
		"no host":      "https://",
		"has a path":   "https://example.com/mail",
		"has a query":  "https://example.com?a=1",
	} {
		t.Run(name, func(t *testing.T) {
			set(t, map[string]string{PublicURLEnv: raw})
			if _, err := Load(); err == nil {
				t.Errorf("accepted %q", raw)
			}
		})
	}
}

func TestATrailingSlashIsTrimmedRatherThanRefused(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://Mail.Example.com/"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.PublicURL.String(); got != "https://mail.example.com" {
		t.Errorf("public url = %q, want it normalized and lowercased", got)
	}
	if got := cfg.Link("/invite/abc"); got != "https://mail.example.com/invite/abc" {
		t.Errorf("link = %q, want one slash between host and path", got)
	}
}

func TestSecureFollowsTheScheme(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "http://localhost:3014"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secure {
		t.Error("http public url produced a Secure cookie, which no browser would send back")
	}
}

func TestAnUnrecognizedLogLevelRefusesToStart(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://mail.example.com", LogLevelEnv: "verbose"})
	if _, err := Load(); err == nil {
		t.Fatal("accepted a level that is not one, and would have silently run at info")
	}
}

func TestDefaults(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://mail.example.com"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Errorf("data dir = %q", cfg.DataDir)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("level = %v, want info", cfg.LogLevel)
	}
}

// Without it no email config can be saved, and finding that out at the first save is too late.
func TestTheSecretKeyIsRequired(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://mail.example.com", SecretKeyEnv: ""})
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "openssl rand -base64 32") {
		t.Fatalf("err = %v, want a refusal saying how to make one", err)
	}
}

func TestASecretKeyOfTheWrongLengthIsRefused(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://mail.example.com", SecretKeyEnv: "AQEBAQEBAQEBAQEBAQEBAQ=="})
	if _, err := Load(); err == nil {
		t.Fatal("accepted a 16-byte key")
	}
}

// openssl pads and uses + and /; a value copied out of somewhere else may do neither.
func TestTheSecretKeyIsReadInEitherAlphabet(t *testing.T) {
	for _, raw := range []string{key, strings.TrimRight(key, "="), " " + key + "\n"} {
		set(t, map[string]string{PublicURLEnv: "https://mail.example.com", SecretKeyEnv: raw})
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if len(cfg.SecretKey) != 32 || cfg.SecretKey[0] != 1 {
			t.Errorf("%q decoded to %v", raw, cfg.SecretKey)
		}
	}
}

func TestAllowedNetworksAreRangesOrAddresses(t *testing.T) {
	set(t, map[string]string{PublicURLEnv: "https://mail.example.com", AllowNetworksEnv: "10.0.0.0/8, 192.168.1.20 ,fd00::/8"})
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0/8", "192.168.1.20/32", "fd00::/8"}
	if len(cfg.AllowNetworks) != len(want) {
		t.Fatalf("networks = %v", cfg.AllowNetworks)
	}
	for i, w := range want {
		if cfg.AllowNetworks[i] != netip.MustParsePrefix(w) {
			t.Errorf("network %d = %v, want %s", i, cfg.AllowNetworks[i], w)
		}
	}

	set(t, map[string]string{PublicURLEnv: "https://mail.example.com", AllowNetworksEnv: "my-lan"})
	if _, err := Load(); err == nil {
		t.Error("accepted a network that is neither an address nor a range")
	}
}
