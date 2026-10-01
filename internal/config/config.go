// Package config is the environment, parsed once. See docs/deploy.md.
package config

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"emguio/internal/seal"
)

// Environment variables read by this package.
const (
	// PublicURLEnv is the address a browser opens. Required, and never inferred: Host and
	// X-Forwarded-Host are client-supplied, so a link built from one is a link a stranger
	// controls.
	//
	// It decides the cookie's Secure flag and what invitation links say.
	PublicURLEnv = "EMGUIO_PUBLIC_URL"

	// DataDirEnv is where emguio.db lives.
	DataDirEnv = "EMGUIO_DATA_DIR"

	// LogLevelEnv sets the slog level: debug, info, warn or error.
	LogLevelEnv = "EMGUIO_LOG_LEVEL"

	// SecretKeyEnv is 32 random bytes, base64, that seal the passwords of email configs.
	// Required, and never stored beside what it seals — see docs/email-configs.md.
	SecretKeyEnv = "EMGUIO_SECRET_KEY"

	// AllowNetworksEnv names private networks emguio may connect to anyway, for a mail server
	// on the same LAN: addresses or CIDR ranges, separated by commas.
	AllowNetworksEnv = "EMGUIO_ALLOW_NETWORKS"
)

// Defaults for everything that has one.
const DefaultDataDir = "/data"

// Config is everything the process was told at startup.
type Config struct {
	// PublicURL is normalized: lowercased, no trailing slash, no path, query or fragment.
	PublicURL *url.URL

	DataDir  string
	LogLevel slog.Level

	// Secure is PublicURL being https, derived once rather than at each Set-Cookie.
	Secure bool

	SecretKey []byte

	// AllowNetworks are dialed although they are private.
	AllowNetworks []netip.Prefix
}

// Link builds an absolute URL into this instance.
func (c *Config) Link(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.PublicURL.String() + path
}

// Load reads the environment. Errors are written for somebody looking at a container that
// refused to start.
func Load() (*Config, error) {
	raw := strings.TrimSpace(os.Getenv(PublicURLEnv))
	if raw == "" {
		return nil, fmt.Errorf("%s is required: set it to the address you open in a browser, e.g. https://mail.example.com", PublicURLEnv)
	}
	public, err := parsePublicURL(raw)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		PublicURL: public,
		DataDir:   DefaultDataDir,
		LogLevel:  slog.LevelInfo,
		Secure:    public.Scheme == "https",
	}
	if dir := strings.TrimSpace(os.Getenv(DataDirEnv)); dir != "" {
		cfg.DataDir = dir
	}
	if cfg.SecretKey, err = parseSecretKey(os.Getenv(SecretKeyEnv)); err != nil {
		return nil, err
	}
	if cfg.AllowNetworks, err = parseNetworks(os.Getenv(AllowNetworksEnv)); err != nil {
		return nil, err
	}

	// A startup error rather than a fall back to info: a level that quietly works is one
	// nobody finds until the log lacks what they came for.
	if v := strings.TrimSpace(os.Getenv(LogLevelEnv)); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToLower(v))); err != nil {
			return nil, fmt.Errorf("%s: %q is not a level (debug, info, warn, error)", LogLevelEnv, v)
		}
	}
	return cfg, nil
}

// parseSecretKey takes either base64 alphabet, padded or not, because `openssl rand -base64`
// and a hand-copied value disagree about both.
func parseSecretKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required: it seals the passwords of email configs. Generate one with `openssl rand -base64 32` and keep it apart from the database's backups", SecretKeyEnv)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(raw); err == nil {
			if len(key) != seal.KeySize {
				return nil, fmt.Errorf("%s is %d bytes, and it has to be %d: generate one with `openssl rand -base64 32`", SecretKeyEnv, len(key), seal.KeySize)
			}
			return key, nil
		}
	}
	return nil, fmt.Errorf("%s is not base64: generate one with `openssl rand -base64 32`", SecretKeyEnv)
}

// parseNetworks reads addresses and CIDR ranges. A bare address is a range of one.
func parseNetworks(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an address or a CIDR range", AllowNetworksEnv, part)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// parsePublicURL validates and normalizes.
//
// A trailing slash is trimmed: it is the commonest way to write one down. A path is refused —
// emguio serves from the root, and a prefix would produce links that half work.
func parsePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a URL: %w", PublicURLEnv, raw, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%s: %q needs an http:// or https:// scheme", PublicURLEnv, raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%s: %q names no host", PublicURLEnv, raw)
	}
	u.Host = strings.ToLower(u.Host)
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%s: %q should be a scheme and a host, with no query or fragment", PublicURLEnv, raw)
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return nil, fmt.Errorf("%s: %q has a path in it, and emguio serves from the root", PublicURLEnv, raw)
	}
	u.Path = ""
	u.User = nil
	return u, nil
}
