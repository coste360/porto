package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coste360/porto/internal/crypto"
)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func validKey(t *testing.T) string {
	t.Helper()
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestConfig_Defaults(t *testing.T) {
	c, err := Load(envOf(map[string]string{"PORTO_SECRET_KEY": validKey(t)}))
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case c.Addr != ":8080", c.BaseURL.String() != "http://localhost:8080", c.DataDir != "./data",
		c.DatabaseURL != "", !c.AutoMigrate, c.MailFrom != "Porto <noreply@localhost>", c.HoldTTL != 10*time.Minute,
		c.LogLevel != slog.LevelInfo, c.LogFormat != "json", c.RequireTOTPForOwners, c.Telemetry,
		c.SensorRawRetention != 30*24*time.Hour, c.Dev, c.WebhooksAllowPrivate, c.PublicReadsPerMinute != 120,
		c.Keys == nil, c.UsesPostgres(), c.DemoPairingEnabled(), c.BootstrapOwnerEnabled(), !c.SecureCookies():
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.SQLitePath() != filepath.Join("data", "porto.db") {
		t.Fatalf("sqlite path %s", c.SQLitePath())
	}
}

func TestConfig_DevDefaults(t *testing.T) {
	c, err := Load(envOf(map[string]string{"PORTO_DEV": "true", "PORTO_BASE_URL": "http://porto.lan:8080"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.LogFormat != "text" || c.Keys != nil || c.SecureCookies() {
		t.Fatalf("dev: %+v", c)
	}
	c.DataDir = t.TempDir()
	gen, err := c.EnsureDevSecret()
	if err != nil || !gen || c.Keys == nil {
		t.Fatalf("ensure: %v %v", gen, err)
	}
	info, err := os.Stat(filepath.Join(c.DataDir, "secret.key"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file: %v %v", info, err)
	}
	first := c.SecretKey
	c.Keys, c.SecretKey = nil, ""
	gen, err = c.EnsureDevSecret()
	if err != nil || gen || c.SecretKey != first {
		t.Fatalf("second ensure: %v %v", gen, err)
	}
}

func TestConfig_Valid(t *testing.T) {
	c, err := Load(envOf(map[string]string{
		"PORTO_SECRET_KEY":              validKey(t) + "," + validKey(t),
		"PORTO_BASE_URL":                "https://porto.example.com/",
		"DATABASE_URL":                  "postgres://u:p@db:5432/porto",
		"PORTO_TRUSTED_PROXIES":         "10.0.0.0/8, 192.168.1.1",
		"PORTO_SENSOR_RAW_RETENTION":    "90d",
		"PORTO_DEMO_PAIRING_USER":       "Dock@Demo.Porto.Test",
		"PORTO_DEMO_PAIRING_MARINA":     "demo",
		"PORTO_BOOTSTRAP_OWNER_EMAIL":   "owner@example.com",
		"PORTO_BOOTSTRAP_OWNER_NAME":    "Owner",
		"PORTO_BOOTSTRAP_OWNER_ORG":     "Org",
		"PORTO_ADMIN_PASSWORD":          " spaced password ",
		"PORTO_SMTP_URL":                "smtps://u:p@smtp.example.com:465",
		"PORTO_MAIL_FROM":               "Porto <noreply@example.com>",
		"PORTO_TLS_AUTOCERT_DOMAINS":    "porto.example.com,www.porto.example.com",
		"PORTO_REQUIRE_TOTP_FOR_OWNERS": "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL.String() != "https://porto.example.com" || len(c.TrustedProxies) != 2 ||
		c.TrustedProxies[1].String() != "192.168.1.1/32" || c.SensorRawRetention != 90*24*time.Hour ||
		c.DemoPairingUser != "dock@demo.porto.test" || c.AdminPassword != " spaced password " ||
		len(c.AutocertDomains) != 2 || !c.RequireTOTPForOwners || !c.UsesPostgres() {
		t.Fatalf("parsed: %+v", c)
	}
}

func TestConfig_Invalid(t *testing.T) {
	secret := "not-a-valid-secret-value-xyz"
	cases := []struct {
		name, key, value, want string
	}{
		{"addr", "PORTO_ADDR", "8080", "PORTO_ADDR"},
		{"base url relative", "PORTO_BASE_URL", "/porto", "PORTO_BASE_URL"},
		{"base url http public", "PORTO_BASE_URL", "http://porto.example.com", "PORTO_BASE_URL"},
		{"base url query", "PORTO_BASE_URL", "https://porto.example.com/?x=1", "PORTO_BASE_URL"},
		{"database url", "DATABASE_URL", "mysql://x", "DATABASE_URL"},
		{"secret key", "PORTO_SECRET_KEY", secret, "PORTO_SECRET_KEY"},
		{"auto migrate", "PORTO_AUTO_MIGRATE", "maybe", "PORTO_AUTO_MIGRATE"},
		{"autocert", "PORTO_TLS_AUTOCERT_DOMAINS", "bad_host!", "PORTO_TLS_AUTOCERT_DOMAINS"},
		{"smtp", "PORTO_SMTP_URL", "http://smtp.example.com", "PORTO_SMTP_URL"},
		{"mail from", "PORTO_MAIL_FROM", "not an address", "PORTO_MAIL_FROM"},
		{"hold ttl", "PORTO_HOLD_TTL", "2h", "PORTO_HOLD_TTL"},
		{"log level", "PORTO_LOG_LEVEL", "trace", "PORTO_LOG_LEVEL"},
		{"log format", "PORTO_LOG_FORMAT", "xml", "PORTO_LOG_FORMAT"},
		{"trusted proxies", "PORTO_TRUSTED_PROXIES", "10.0.0.0/33", "PORTO_TRUSTED_PROXIES"},
		{"require totp", "PORTO_REQUIRE_TOTP_FOR_OWNERS", "yes please", "PORTO_REQUIRE_TOTP_FOR_OWNERS"},
		{"telemetry", "PORTO_TELEMETRY", "true", "PORTO_TELEMETRY"},
		{"sensor retention", "PORTO_SENSOR_RAW_RETENTION", "0d", "PORTO_SENSOR_RAW_RETENTION"},
		{"demo pairing half", "PORTO_DEMO_PAIRING_USER", "a@b.c", "PORTO_DEMO_PAIRING_MARINA"},
		{"dev", "PORTO_DEV", "sometimes", "PORTO_DEV"},
		{"stripe base without dev", "PORTO_STRIPE_API_BASE", "http://127.0.0.1:9999", "PORTO_STRIPE_API_BASE"},
		{"allow private", "PORTO_WEBHOOKS_ALLOW_PRIVATE", "2", "PORTO_WEBHOOKS_ALLOW_PRIVATE"},
		{"public reads", "PORTO_PUBLIC_READS_PER_MINUTE", "0", "PORTO_PUBLIC_READS_PER_MINUTE"},
		{"bootstrap half", "PORTO_BOOTSTRAP_OWNER_EMAIL", "a@b.c", "PORTO_BOOTSTRAP_OWNER_NAME"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"PORTO_SECRET_KEY": validKey(t)}
			env[tc.key] = tc.value
			_, err := Load(envOf(env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error naming %s, got %v", tc.want, err)
			}
		})
	}
	t.Run("missing secret", func(t *testing.T) {
		if _, err := Load(envOf(nil)); err == nil || !strings.Contains(err.Error(), "PORTO_SECRET_KEY") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("smtp needs real from", func(t *testing.T) {
		_, err := Load(envOf(map[string]string{"PORTO_SECRET_KEY": validKey(t), "PORTO_SMTP_URL": "smtp://mail.example.com:587"}))
		if err == nil || !strings.Contains(err.Error(), "PORTO_MAIL_FROM") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("all errors joined", func(t *testing.T) {
		_, err := Load(envOf(map[string]string{"PORTO_ADDR": "x", "PORTO_LOG_LEVEL": "x"}))
		for _, name := range []string{"PORTO_ADDR", "PORTO_LOG_LEVEL", "PORTO_SECRET_KEY"} {
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("missing %s in %v", name, err)
			}
		}
	})
}

func TestConfig_SecretNotEchoed(t *testing.T) {
	secrets := map[string]string{
		"PORTO_SECRET_KEY":     "c3VwZXItc2VjcmV0LXZhbHVlLXRoYXQtaXMtd3Jvbmc",
		"DATABASE_URL":         "mysql://admin:hunter2-db-password@db/porto",
		"PORTO_SMTP_URL":       "ftp://user:hunter2-smtp-password@mail",
		"PORTO_ADMIN_PASSWORD": "hunter2-admin",
	}
	_, err := Load(envOf(secrets))
	if err == nil {
		t.Fatal("want error")
	}
	for _, needle := range []string{"c3VwZXItc2VjcmV0", "hunter2"} {
		if strings.Contains(err.Error(), needle) {
			t.Fatalf("error echoes a secret: %v", err)
		}
	}
}

func TestConfig_StripeAPIBaseDevOnly(t *testing.T) {
	c, err := Load(envOf(map[string]string{
		"PORTO_SECRET_KEY": validKey(t), "PORTO_DEV": "true", "PORTO_STRIPE_API_BASE": "http://127.0.0.1:9999/",
	}))
	if err != nil || c.StripeAPIBase != "http://127.0.0.1:9999" {
		t.Fatalf("dev base: %q %v", c.StripeAPIBase, err)
	}
	if _, err := Load(envOf(map[string]string{
		"PORTO_SECRET_KEY": validKey(t), "PORTO_DEV": "true", "PORTO_STRIPE_API_BASE": "ftp://x",
	})); err == nil || !strings.Contains(err.Error(), "PORTO_STRIPE_API_BASE") {
		t.Fatalf("bad scheme: %v", err)
	}
}
