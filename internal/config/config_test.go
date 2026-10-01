package config

import (
	"log/slog"
	"testing"
)

// set clears everything this package reads, so no test passes on another's leftovers.
func set(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{PublicURLEnv, DataDirEnv, LogLevelEnv} {
		t.Setenv(k, "")
	}
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
