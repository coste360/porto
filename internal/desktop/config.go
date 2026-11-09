package desktop

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// DefaultUpdateURL is the version manifest of the desktop edition (m6a-plan §2.8, D16): the latest.json of the
// shared Coste360 updates service, written by the private build pipeline for every stable release
// (docs/release.md). It is only read when the version check is on; Porto never needs it to work.
const DefaultUpdateURL = "https://updates.coste360.it/porto/latest.json"

// Keyring modes of PORTO_DESKTOP_KEYRING.
const (
	KeyringAuto = "auto" // OS keychain, file fallback
	KeyringFile = "file" // file only (CI, e2e)
)

// Env is the desktop part of the environment (m6a-plan §2.4). Errors name the variable, never the value.
type Env struct {
	// UpdateCheckOff is true when PORTO_UPDATE_CHECK=false: no version request at all, whatever desktop.json says.
	UpdateCheckOff bool
	// Port and LANPort override desktop.json when not zero.
	Port    int
	LANPort int
	Keyring string
	Dev     bool

	// Dev only (PORTO_DEV=true).
	UpdateURL    string
	LANLoopback  bool
	FirstRunFile bool
}

// LoadEnv reads the desktop variables through lookup (os.LookupEnv).
func LoadEnv(lookup func(string) (string, bool)) (Env, error) {
	var errs []error
	fail := func(name, msg string) { errs = append(errs, fmt.Errorf("%s %s", name, msg)) }
	get := func(name string) string {
		v, _ := lookup(name)
		return strings.TrimSpace(v)
	}
	boolean := func(name string) (val, set bool) {
		v := get(name)
		if v == "" {
			return false, false
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			fail(name, "must be true or false")
			return false, false
		}
		return b, true
	}
	port := func(name string) int {
		v := get(name)
		if v == "" {
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			fail(name, "must be a port number (1-65535)")
			return 0
		}
		return n
	}

	var e Env
	e.Dev, _ = boolean("PORTO_DEV")
	if on, set := boolean("PORTO_UPDATE_CHECK"); set && !on {
		e.UpdateCheckOff = true
	}
	e.Port = port("PORTO_DESKTOP_PORT")
	e.LANPort = port("PORTO_DESKTOP_LAN_PORT")
	switch k := strings.ToLower(get("PORTO_DESKTOP_KEYRING")); k {
	case "", KeyringAuto:
		e.Keyring = KeyringAuto
	case KeyringFile:
		e.Keyring = KeyringFile
	default:
		fail("PORTO_DESKTOP_KEYRING", "must be auto or file")
	}

	devOnly := func(name string) bool {
		if get(name) == "" {
			return false
		}
		if !e.Dev {
			fail(name, "is allowed only with PORTO_DEV=true")
			return false
		}
		return true
	}
	if devOnly("PORTO_UPDATE_URL") {
		u, err := url.Parse(get("PORTO_UPDATE_URL"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			fail("PORTO_UPDATE_URL", "must be an absolute http(s) URL")
		} else {
			e.UpdateURL = u.String()
		}
	}
	if devOnly("PORTO_DESKTOP_LAN_LOOPBACK") {
		e.LANLoopback, _ = boolean("PORTO_DESKTOP_LAN_LOOPBACK")
	}
	if devOnly("PORTO_DESKTOP_FIRST_RUN_FILE") {
		e.FirstRunFile, _ = boolean("PORTO_DESKTOP_FIRST_RUN_FILE")
	}
	if len(errs) > 0 {
		return Env{}, fmt.Errorf("desktop config: %w", errors.Join(errs...))
	}
	return e, nil
}

// passThrough lists the variables of the real environment the embedded server sees (m6a-plan §2.2). Everything
// else (DATABASE_URL included: the desktop edition is SQLite only, D3) is not passed.
var passThrough = []string{
	"PORTO_LOG_LEVEL", "PORTO_SMTP_URL", "PORTO_MAIL_FROM", "PORTO_DEV",
	"PORTO_BOOTSTRAP_OWNER_EMAIL", "PORTO_BOOTSTRAP_OWNER_NAME", "PORTO_BOOTSTRAP_OWNER_ORG",
	"PORTO_ADMIN_PASSWORD", "PORTO_REQUIRE_TOTP_FOR_OWNERS",
}

// ServerEnvInput is what the launcher decided for the embedded server.
type ServerEnvInput struct {
	Port      int
	DataDir   string
	SecretKey string
	MailDir   string
	// Lookup is the real environment (os.LookupEnv) for the pass-through variables.
	Lookup func(string) (string, bool)
}

// ServerEnv returns the environment config.Load reads for the embedded server: loopback address and base URL,
// data folder, secret key, automatic migrations, file mail transport (unless PORTO_SMTP_URL is set), plus the
// allow-listed pass-through variables. It is pure.
func ServerEnv(in ServerEnvInput) func(string) (string, bool) {
	m := map[string]string{
		"PORTO_ADDR":         fmt.Sprintf("127.0.0.1:%d", in.Port),
		"PORTO_BASE_URL":     fmt.Sprintf("http://127.0.0.1:%d", in.Port),
		"PORTO_DATA_DIR":     in.DataDir,
		"PORTO_SECRET_KEY":   in.SecretKey,
		"PORTO_AUTO_MIGRATE": "true",
		"PORTO_LOG_FORMAT":   "json",
	}
	if in.Lookup != nil {
		for _, k := range passThrough {
			if v, ok := in.Lookup(k); ok {
				m[k] = v
			}
		}
	}
	if m["PORTO_SMTP_URL"] == "" {
		delete(m, "PORTO_SMTP_URL")
		m["PORTO_MAIL_DIR"] = in.MailDir
	}
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}
