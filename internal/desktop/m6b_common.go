package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/peripheral"
)

// Paths of the M6b files inside the data folder.

// Peripherals is the folder of the fake peripheral drivers (JSON lines, development and e2e).
func (p Paths) Peripherals() string { return filepath.Join(p.Root, "peripherals") }

// RemoteCache is the shell cache of the remote mode.
func (p Paths) RemoteCache() string { return filepath.Join(p.Root, "remote-cache") }

// DeviceTokenFile is the file fallback of the device token of the remote mode.
func (p Paths) DeviceTokenFile() string { return filepath.Join(p.Root, "device.token") }

// FiscalJournal is the journal of the fiscal outcomes the remote instance has not acknowledged yet (INV-86).
func (p Paths) FiscalJournal() string { return filepath.Join(p.Root, "fiscal-journal.json") }

// RemoteEnterURL is the dev-only file with the enter URL of the remote proxy (e2e), like FirstRunURL.
func (p Paths) RemoteEnterURL() string { return filepath.Join(p.Root, "remote-enter.url") }

// peripheralSet holds the drivers built from the peripheral settings; the fake QR reader survives reloads so that
// a pushed scan is not lost.
type peripheralSet struct {
	mu     sync.RWMutex
	p      desktopport.Peripherals
	reader *peripheral.FakeReader
	dir    string
	target peripheral.TargetOptions
}

func newPeripheralSet(dir string, target peripheral.TargetOptions) *peripheralSet {
	return &peripheralSet{reader: peripheral.NewFakeReader(), dir: dir, target: target}
}

func (ps *peripheralSet) load(s desktopport.PeripheralSettings) {
	p := peripheral.Build(s, peripheral.BuildOptions{FakeDir: ps.dir, Reader: ps.reader, Target: ps.target})
	ps.mu.Lock()
	ps.p = p
	ps.mu.Unlock()
}

func (ps *peripheralSet) get() desktopport.Peripherals {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.p
}

// FakeReader returns the fake QR reader (tests push scans into it).
func (ps *peripheralSet) FakeReader() *peripheral.FakeReader { return ps.reader }

// validatePeripherals checks settings against the platform transport rules.
func (ps *peripheralSet) validate(s desktopport.PeripheralSettings) error {
	return peripheral.Validate(s, ps.target)
}

// peripheralAudit lists the configured drivers (never the targets).
func peripheralAudit(s desktopport.PeripheralSettings) map[string]any {
	return map[string]any{
		"receipt": s.Receipt.Driver, "label": s.Label.Driver, "reader": s.Reader.Driver, "fiscal": s.Fiscal.Driver,
	}
}

// App PIN of the remote mode (m6b-plan §7.2, §7.5, D13): 6 to 12 digits, Argon2id, 5 attempts in 15 minutes.
const (
	pinLimit  = 5
	pinWindow = 15 * time.Minute
)

var pinFormat = regexp.MustCompile(`^[0-9]{6,12}$`)

// ValidPin reports whether pin has the app PIN format.
func ValidPin(pin string) bool { return pinFormat.MatchString(pin) }

// pinGuard checks the app PIN against its hash with a rate limit. Neither the PIN nor its hash is ever logged.
type pinGuard struct {
	hasher *auth.Hasher
	clock  clock.Clock

	mu       sync.Mutex
	attempts []time.Time
}

func newPinGuard(p auth.Argon2Params, clk clock.Clock) *pinGuard {
	if p.MemoryKiB == 0 {
		p = auth.DefaultArgon2Params
	}
	return &pinGuard{hasher: &auth.Hasher{Params: p}, clock: clk}
}

func (g *pinGuard) hash(pin string) (string, error) {
	if !ValidPin(pin) {
		return "", &desktopport.SettingsError{Fields: []string{"pin"}}
	}
	return g.hasher.Hash(pin)
}

// check verifies pin against hash: ErrAppPinInvalid, or *auth.RateLimitError after 5 failures in 15 minutes.
func (g *pinGuard) check(pin, hash string) error {
	g.mu.Lock()
	now := g.clock.Now()
	kept := g.attempts[:0]
	for _, t := range g.attempts {
		if now.Sub(t) < pinWindow {
			kept = append(kept, t)
		}
	}
	g.attempts = kept
	if len(kept) >= pinLimit {
		wait := pinWindow - now.Sub(kept[0])
		g.mu.Unlock()
		return &auth.RateLimitError{RetryAfter: wait}
	}
	g.mu.Unlock()
	ok := false
	if pin != "" && hash != "" && ValidPin(pin) {
		ok, _ = g.hasher.Verify(pin, hash)
	}
	if !ok {
		g.mu.Lock()
		g.attempts = append(g.attempts, now)
		g.mu.Unlock()
		return desktopport.ErrAppPinInvalid
	}
	return nil
}

// TokenStore keeps the device token of the remote mode.
type TokenStore interface {
	Get() (string, error)
	Set(token string) error
	Delete() error
}

// keyringToken keeps the token in the OS keychain (account device-token/<data folder hash>).
type keyringToken struct{ account string }

func newKeyringToken(dataDir string) keyringToken {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	sum := sha256.Sum256([]byte(abs))
	return keyringToken{account: "device-token/" + hex.EncodeToString(sum[:])[:12]}
}

func (k keyringToken) Get() (string, error) {
	v, err := keyring.Get(KeyringService, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	if err != nil {
		return "", fmt.Errorf("desktop: keychain: %w", err)
	}
	return v, nil
}

func (k keyringToken) Set(token string) error { return keyring.Set(KeyringService, k.account, token) }

func (k keyringToken) Delete() error {
	err := keyring.Delete(KeyringService, k.account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

type fileToken struct{ f FileStore }

func (t fileToken) Get() (string, error)   { return t.f.Get() }
func (t fileToken) Set(token string) error { return t.f.Set(token) }
func (t fileToken) Delete() error          { return t.f.Remove() }

// fallbackToken tries the keychain first and falls back to the 0600 file (like the secret key).
type fallbackToken struct {
	primary TokenStore // nil with PORTO_DESKTOP_KEYRING=file
	file    TokenStore
}

func (t fallbackToken) Get() (string, error) {
	if t.primary != nil {
		if v, err := t.primary.Get(); err == nil {
			return v, nil
		}
	}
	return t.file.Get()
}

func (t fallbackToken) Set(token string) error {
	if t.primary != nil && t.primary.Set(token) == nil {
		if back, err := t.primary.Get(); err == nil && back == token {
			_ = t.file.Delete()
			return nil
		}
	}
	return t.file.Set(token)
}

func (t fallbackToken) Delete() error {
	if t.primary != nil {
		_ = t.primary.Delete()
	}
	return t.file.Delete()
}

// deviceTokens returns the token store of a data folder; override replaces the keychain (tests).
func deviceTokens(p Paths, env Env, override TokenStore) TokenStore {
	file := fileToken{FileStore{Path: p.DeviceTokenFile()}}
	if env.Keyring == KeyringFile {
		return fallbackToken{file: file}
	}
	if override != nil {
		return fallbackToken{primary: override, file: file}
	}
	return fallbackToken{primary: newKeyringToken(p.Root), file: file}
}
