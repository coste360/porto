// Package config reads Porto's configuration from PORTO_* environment variables (m0-plan §5). Load is pure: it
// takes a lookup function and reports every problem at once. Error messages name the variable but never echo
// its value, so a misconfigured secret does not end up in logs.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coste360/porto/internal/crypto"
)

// Config is the validated configuration.
type Config struct {
	Addr        string
	BaseURL     *url.URL
	DataDir     string
	DatabaseURL string

	// SecretKey is the raw PORTO_SECRET_KEY (comma-separated base64 keys). Empty in dev mode until
	// EnsureDevSecret fills it from DATA_DIR/secret.key.
	SecretKey string
	// Keys is the parsed key ring (nil while SecretKey is empty in dev mode).
	Keys *crypto.KeyRing

	AutoMigrate     bool
	AutocertDomains []string

	SMTPURL  string
	MailFrom string
	MailDir  string

	HoldTTL time.Duration

	LogLevel  slog.Level
	LogFormat string // json | text

	TrustedProxies []netip.Prefix

	RequireTOTPForOwners bool
	Telemetry            bool
	SensorRawRetention   time.Duration

	DemoPairingUser   string
	DemoPairingMarina string

	Dev                  bool
	WebhooksAllowPrivate bool
	PublicReadsPerMinute int
	// StripeAPIBase overrides https://api.stripe.com for the optional Stripe driver (M5); honoured only with
	// PORTO_DEV=true (local fake servers in tests and development).
	StripeAPIBase string

	BootstrapOwnerEmail string
	BootstrapOwnerName  string
	BootstrapOwnerOrg   string
	AdminPassword       string
}

// SQLitePath is the SQLite file used when DatabaseURL is empty.
func (c *Config) SQLitePath() string { return filepath.Join(c.DataDir, "porto.db") }

// UsesPostgres reports whether DATABASE_URL selects PostgreSQL.
func (c *Config) UsesPostgres() bool { return c.DatabaseURL != "" }

// DemoPairingEnabled reports whether the demo pairing endpoint is on.
func (c *Config) DemoPairingEnabled() bool {
	return c.DemoPairingUser != "" && c.DemoPairingMarina != ""
}

// BootstrapOwnerEnabled reports whether serve should create the first owner.
func (c *Config) BootstrapOwnerEnabled() bool { return c.BootstrapOwnerEmail != "" }

// SecureCookies reports whether cookies get the Secure flag by default: always, except dev mode over plain http
// and a plain http base URL on a loopback IP literal (the desktop edition on 127.0.0.1, m6a-plan §3.3). Requests
// that arrive over TLS (the desktop local-network listener) set Secure regardless (middleware).
func (c *Config) SecureCookies() bool {
	if c.BaseURL == nil || c.BaseURL.Scheme != "http" {
		return true
	}
	if c.Dev {
		return false
	}
	h := c.BaseURL.Hostname()
	return h != "127.0.0.1" && h != "::1"
}

// FromEnviron loads the configuration from the process environment.
func FromEnviron() (*Config, error) { return Load(os.LookupEnv) }

type loader struct {
	env  func(string) (string, bool)
	errs []error
}

func (l *loader) get(name, def string) string {
	if v, ok := l.env(name); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func (l *loader) fail(name, format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf("%s: %s", name, fmt.Sprintf(format, args...)))
}

func (l *loader) bool(name string, def bool) bool {
	v := l.get(name, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(name, "must be true or false")
		return def
	}
	return b
}

func (l *loader) int(name string, def, lo, hi int) int {
	v := l.get(name, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		l.fail(name, "must be an integer between %d and %d", lo, hi)
		return def
	}
	return n
}

// duration parses Go durations plus a "d" (days) suffix.
func (l *loader) duration(name, def string, lo, hi time.Duration) time.Duration {
	v := l.get(name, def)
	d, err := parseDuration(v)
	if err != nil || d < lo || d > hi {
		l.fail(name, "must be a duration between %s and %s", fmtDuration(lo), fmtDuration(hi))
		d, _ = parseDuration(def)
	}
	return d
}

func parseDuration(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func fmtDuration(d time.Duration) string {
	if d >= 24*time.Hour && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

func (l *loader) list(name string) []string {
	v := l.get(name, "")
	if v == "" {
		return nil
	}
	var out []string
	for p := range strings.SplitSeq(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load builds a Config from env. Every problem is reported, joined with errors.Join.
func Load(env func(string) (string, bool)) (*Config, error) {
	l := &loader{env: env}
	c := &Config{}

	c.Dev = l.bool("PORTO_DEV", false)

	c.Addr = l.get("PORTO_ADDR", ":8080")
	if _, port, err := net.SplitHostPort(c.Addr); err != nil || port == "" {
		l.fail("PORTO_ADDR", "must be host:port")
	}

	c.BaseURL = l.baseURL(c.Dev)

	c.DataDir = l.get("PORTO_DATA_DIR", "./data")

	c.DatabaseURL = l.get("DATABASE_URL", "")
	if c.DatabaseURL != "" {
		u, err := url.Parse(c.DatabaseURL)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
			l.fail("DATABASE_URL", "must be a postgres:// or postgresql:// URL")
		}
	}

	c.SecretKey = l.get("PORTO_SECRET_KEY", "")
	switch {
	case c.SecretKey != "":
		keys, err := crypto.ParseKeyRing(c.SecretKey)
		if err != nil {
			l.fail("PORTO_SECRET_KEY", "must be a comma-separated list of base64 32-byte keys")
		} else {
			c.Keys = keys
		}
	case !c.Dev:
		l.fail("PORTO_SECRET_KEY", "is required (generate one with: openssl rand -base64 32)")
	}

	c.AutoMigrate = l.bool("PORTO_AUTO_MIGRATE", true)

	c.AutocertDomains = l.list("PORTO_TLS_AUTOCERT_DOMAINS")
	for _, d := range c.AutocertDomains {
		if !validHostname(d) {
			l.fail("PORTO_TLS_AUTOCERT_DOMAINS", "contains an invalid hostname")
			break
		}
	}

	c.SMTPURL = l.get("PORTO_SMTP_URL", "")
	if c.SMTPURL != "" {
		u, err := url.Parse(c.SMTPURL)
		if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" {
			l.fail("PORTO_SMTP_URL", "must be smtp://[user:pass@]host[:port] or smtps://…")
		}
	}
	c.MailFrom = l.get("PORTO_MAIL_FROM", "Porto <noreply@localhost>")
	if addr, err := mail.ParseAddress(c.MailFrom); err != nil {
		l.fail("PORTO_MAIL_FROM", "must be an RFC 5322 address")
	} else if c.SMTPURL != "" && !c.Dev {
		_, domain, _ := strings.Cut(addr.Address, "@")
		if domain == "localhost" || !strings.Contains(domain, ".") {
			l.fail("PORTO_MAIL_FROM", "must use a real domain when PORTO_SMTP_URL is set")
		}
	}
	c.MailDir = l.get("PORTO_MAIL_DIR", "")

	c.HoldTTL = l.duration("PORTO_HOLD_TTL", "10m", time.Minute, time.Hour)

	defLevelFormat := "json"
	if c.Dev {
		defLevelFormat = "text"
	}
	switch v := strings.ToLower(l.get("PORTO_LOG_LEVEL", "info")); v {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		l.fail("PORTO_LOG_LEVEL", "must be debug, info, warn or error")
	}
	c.LogFormat = strings.ToLower(l.get("PORTO_LOG_FORMAT", defLevelFormat))
	if c.LogFormat != "json" && c.LogFormat != "text" {
		l.fail("PORTO_LOG_FORMAT", "must be json or text")
	}

	for _, p := range l.list("PORTO_TRUSTED_PROXIES") {
		prefix, err := netip.ParsePrefix(p)
		if err != nil {
			addr, aerr := netip.ParseAddr(p)
			if aerr != nil {
				l.fail("PORTO_TRUSTED_PROXIES", "must be a comma-separated list of CIDR prefixes")
				break
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
	}

	c.RequireTOTPForOwners = l.bool("PORTO_REQUIRE_TOTP_FOR_OWNERS", false)
	c.Telemetry = l.bool("PORTO_TELEMETRY", false)
	if c.Telemetry {
		l.fail("PORTO_TELEMETRY", "is not supported: Porto has no telemetry")
	}
	c.SensorRawRetention = l.duration("PORTO_SENSOR_RAW_RETENTION", "30d", 24*time.Hour, 3650*24*time.Hour)

	c.DemoPairingUser = strings.ToLower(l.get("PORTO_DEMO_PAIRING_USER", ""))
	c.DemoPairingMarina = l.get("PORTO_DEMO_PAIRING_MARINA", "")
	if (c.DemoPairingUser == "") != (c.DemoPairingMarina == "") {
		l.fail("PORTO_DEMO_PAIRING_USER", "and PORTO_DEMO_PAIRING_MARINA must be set together")
	}
	if c.DemoPairingUser != "" {
		if _, err := mail.ParseAddress(c.DemoPairingUser); err != nil {
			l.fail("PORTO_DEMO_PAIRING_USER", "must be an email address")
		}
	}

	c.WebhooksAllowPrivate = l.bool("PORTO_WEBHOOKS_ALLOW_PRIVATE", false)
	c.PublicReadsPerMinute = l.int("PORTO_PUBLIC_READS_PER_MINUTE", 120, 1, 10000)

	if base := l.get("PORTO_STRIPE_API_BASE", ""); base != "" {
		u, err := url.Parse(base)
		switch {
		case !c.Dev:
			l.fail("PORTO_STRIPE_API_BASE", "is allowed only with PORTO_DEV=true")
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "":
			l.fail("PORTO_STRIPE_API_BASE", "must be an http(s) URL without query")
		default:
			c.StripeAPIBase = strings.TrimRight(base, "/")
		}
	}

	c.BootstrapOwnerEmail = strings.ToLower(l.get("PORTO_BOOTSTRAP_OWNER_EMAIL", ""))
	c.BootstrapOwnerName = l.get("PORTO_BOOTSTRAP_OWNER_NAME", "")
	c.BootstrapOwnerOrg = l.get("PORTO_BOOTSTRAP_OWNER_ORG", "")
	set := 0
	for _, v := range []string{c.BootstrapOwnerEmail, c.BootstrapOwnerName, c.BootstrapOwnerOrg} {
		if v != "" {
			set++
		}
	}
	if set != 0 && set != 3 {
		l.fail("PORTO_BOOTSTRAP_OWNER_EMAIL", "PORTO_BOOTSTRAP_OWNER_NAME and PORTO_BOOTSTRAP_OWNER_ORG must be set together")
	}
	if c.BootstrapOwnerEmail != "" {
		if _, err := mail.ParseAddress(c.BootstrapOwnerEmail); err != nil {
			l.fail("PORTO_BOOTSTRAP_OWNER_EMAIL", "must be an email address")
		}
	}
	// Read the password with os-style lookup but keep surrounding spaces: they may be part of it.
	if v, ok := env("PORTO_ADMIN_PASSWORD"); ok {
		c.AdminPassword = v
	}

	if len(l.errs) > 0 {
		return nil, fmt.Errorf("config: %w", errors.Join(l.errs...))
	}
	return c, nil
}

func (l *loader) baseURL(dev bool) *url.URL {
	const name = "PORTO_BASE_URL"
	raw := l.get(name, "http://localhost:8080")
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		l.fail(name, "must be an absolute http(s) URL")
		u, _ = url.Parse("http://localhost:8080")
		return u
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		l.fail(name, "must not contain credentials, a query or a fragment")
	}
	if u.Scheme == "http" && !dev && !isLocalHost(u.Hostname()) {
		l.fail(name, "must use https (http is allowed only with PORTO_DEV=true or on localhost)")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u
}

func isLocalHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func validHostname(h string) bool {
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// EnsureDevSecret fills SecretKey/Keys in dev mode when PORTO_SECRET_KEY is unset: it reads DATA_DIR/secret.key
// or generates it (0600). generated is true when a new key was written, so that the caller can tell the operator
// once. Outside dev mode, or when a key is configured, it does nothing.
func (c *Config) EnsureDevSecret() (generated bool, err error) {
	if c.Keys != nil || !c.Dev {
		return false, nil
	}
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return false, fmt.Errorf("config: create data dir: %w", err)
	}
	path := filepath.Join(c.DataDir, "secret.key")
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		c.SecretKey = strings.TrimSpace(string(b))
	case errors.Is(err, os.ErrNotExist):
		key, gerr := crypto.GenerateKey()
		if gerr != nil {
			return false, gerr
		}
		if werr := os.WriteFile(path, []byte(key+"\n"), 0o600); werr != nil {
			return false, fmt.Errorf("config: write dev secret: %w", werr)
		}
		c.SecretKey = key
		generated = true
	default:
		return false, fmt.Errorf("config: read dev secret: %w", err)
	}
	keys, err := crypto.ParseKeyRing(c.SecretKey)
	if err != nil {
		return false, fmt.Errorf("config: %s is not a valid key", path)
	}
	c.Keys = keys
	return generated, nil
}
