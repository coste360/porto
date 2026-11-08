package desktop

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktop/remote"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/peripheral"
)

// RemoteRuntime implements desktopport.Runtime in remote mode (m6b-plan §7): no database, no local users. The
// local administration (peripherals, kiosk, unpair) needs the app PIN; the M6a operations do not exist
// (ErrWrongMode, 404). It is served by the remote proxy, never by a Porto server.
type RemoteRuntime struct {
	cfg    RemoteRuntimeConfig
	periph *peripheralSet
	pin    *pinGuard
	tokens TokenStore

	setMu    sync.Mutex
	settings FileSettings

	mu          sync.Mutex
	proxy       *remote.Proxy
	ui          UI
	kioskActive bool
	instanceTok string
	mismatch    bool
	onReset     func(revoked bool)
}

// RemoteRuntimeConfig builds a RemoteRuntime.
type RemoteRuntimeConfig struct {
	Version    string
	Paths      Paths
	Env        Env
	Clock      clock.Clock
	Log        *slog.Logger
	Settings   FileSettings
	SecretKind string
	GOOS       string
	Argon2     auth.Argon2Params
	Tokens     TokenStore
}

var (
	_ desktopport.Runtime = (*RemoteRuntime)(nil)
	_ remote.PinChecker   = (*RemoteRuntime)(nil)
)

// NewRemoteRuntime returns the runtime of the remote mode.
func NewRemoteRuntime(c RemoteRuntimeConfig) *RemoteRuntime {
	if c.Clock == nil {
		c.Clock = clock.System{}
	}
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	rr := &RemoteRuntime{
		cfg: c, settings: c.Settings, pin: newPinGuard(c.Argon2, c.Clock), tokens: deviceTokens(c.Paths, c.Env, c.Tokens),
		periph: newPeripheralSet(c.Paths.Peripherals(), peripheral.TargetOptions{Dev: c.Env.Dev, GOOS: c.GOOS}),
	}
	rr.periph.load(c.Settings.Peripherals.Port())
	return rr
}

// Attach sets the proxy, the UI and the callback that resets the app to the first run (unpair, revoked).
func (rr *RemoteRuntime) Attach(p *remote.Proxy, ui UI, onReset func(revoked bool)) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	rr.proxy, rr.ui, rr.onReset = p, ui, onReset
}

// Token returns the device token ("" when missing).
func (rr *RemoteRuntime) Token() string {
	t, err := rr.tokens.Get()
	if err != nil {
		return ""
	}
	return t
}

// FakeReader returns the fake QR reader (tests, e2e).
func (rr *RemoteRuntime) FakeReader() *peripheral.FakeReader { return rr.periph.FakeReader() }

func (rr *RemoteRuntime) currentSettings() FileSettings {
	rr.setMu.Lock()
	defer rr.setMu.Unlock()
	return rr.settings
}

func (rr *RemoteRuntime) saveSettings(change func(*FileSettings)) (FileSettings, error) {
	rr.setMu.Lock()
	defer rr.setMu.Unlock()
	s := rr.settings
	change(&s)
	if err := SaveSettings(rr.cfg.Paths.Settings(), s); err != nil {
		return rr.settings, err
	}
	rr.settings = s
	return s, nil
}

// SetKioskActive records that the window runs in kiosk mode.
func (rr *RemoteRuntime) SetKioskActive(on bool) {
	rr.mu.Lock()
	rr.kioskActive = on
	rr.mu.Unlock()
}

// SetInstanceToken records the hash of the activation token (instance.json).
func (rr *RemoteRuntime) SetInstanceToken(hash string) {
	rr.mu.Lock()
	rr.instanceTok = hash
	rr.mu.Unlock()
}

// CheckVersion compares the major version of the online instance with the app's (banner "aggiorna l'app").
func (rr *RemoteRuntime) CheckVersion(ctx context.Context) {
	rr.mu.Lock()
	p := rr.proxy
	rr.mu.Unlock()
	if p == nil {
		return
	}
	var m api.Meta
	if code, err := p.Upstream().JSON(ctx, http.MethodGet, "/api/admin/v1/meta", nil, nil, &m); err == nil && code == http.StatusOK {
		mismatch := major(m.Version) != "" && major(rr.cfg.Version) != "" && major(m.Version) != major(rr.cfg.Version)
		rr.mu.Lock()
		rr.mismatch = mismatch
		rr.mu.Unlock()
	}
}

// major returns the major version of "v1.2.3" / "1.2.3" ("" for dev builds).
func major(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	m, _, _ := strings.Cut(v, ".")
	if m == "" || strings.Trim(m, "0123456789") != "" {
		return ""
	}
	return m
}

// CheckAppPin implements remote.PinChecker.
func (rr *RemoteRuntime) CheckAppPin(pin string) error {
	return rr.pin.check(pin, rr.currentSettings().AppPinHash)
}

func (rr *RemoteRuntime) admin(a desktopport.AdminAuth) error { return rr.CheckAppPin(a.AppPin) }

// Status implements desktopport.Runtime.
func (rr *RemoteRuntime) Status(context.Context) desktopport.Status {
	s := rr.currentSettings()
	rr.mu.Lock()
	p, kiosk, mismatch := rr.proxy, rr.kioskActive, rr.mismatch
	rr.mu.Unlock()
	st := desktopport.Status{
		Version: rr.cfg.Version, Mode: desktopport.ModeRemote, SecretStore: rr.cfg.SecretKind, Kiosk: kiosk,
	}
	if s.Remote != nil {
		st.Remote = &desktopport.RemoteStatus{
			URL: s.Remote.URL, MarinaID: s.Remote.MarinaID, MarinaName: s.Remote.MarinaName,
			Reachable: p != nil && p.Reachable(), PairedAt: s.Remote.PairedAt, VersionMismatch: mismatch,
		}
	}
	return st
}

// FirstRun implements desktopport.Runtime.
func (rr *RemoteRuntime) FirstRun(context.Context, desktopport.FirstRunInput) (*desktopport.FirstRunResult, error) {
	return nil, desktopport.ErrWrongMode
}

// FirstRunRestore implements desktopport.Runtime.
func (rr *RemoteRuntime) FirstRunRestore(context.Context, string, desktopport.RestoreSource) error {
	return desktopport.ErrWrongMode
}

// Settings implements desktopport.Runtime.
func (rr *RemoteRuntime) Settings(context.Context, *auth.Principal) (desktopport.Settings, error) {
	return desktopport.Settings{}, desktopport.ErrWrongMode
}

// UpdateSettings implements desktopport.Runtime.
func (rr *RemoteRuntime) UpdateSettings(context.Context, *auth.Principal, desktopport.SettingsPatch) (desktopport.Settings, error) {
	return desktopport.Settings{}, desktopport.ErrWrongMode
}

// LAN implements desktopport.Runtime: the local network belongs to the local mode.
func (rr *RemoteRuntime) LAN() desktopport.LANController { return wrongModeLAN{} }

// Backups implements desktopport.Runtime.
func (rr *RemoteRuntime) Backups(context.Context, *auth.Principal) ([]desktopport.BackupFile, desktopport.BackupStatus, error) {
	return nil, desktopport.BackupStatus{}, desktopport.ErrWrongMode
}

// BackupNow implements desktopport.Runtime.
func (rr *RemoteRuntime) BackupNow(context.Context, *auth.Principal) (desktopport.BackupFile, error) {
	return desktopport.BackupFile{}, desktopport.ErrWrongMode
}

// Restore implements desktopport.Runtime.
func (rr *RemoteRuntime) Restore(context.Context, *auth.Principal, desktopport.RestoreSource) error {
	return desktopport.ErrWrongMode
}

// RevealSecretKey implements desktopport.Runtime.
func (rr *RemoteRuntime) RevealSecretKey(context.Context, *auth.Principal, string) (string, error) {
	return "", desktopport.ErrWrongMode
}

// Activate implements desktopport.Runtime.
func (rr *RemoteRuntime) Activate(_ context.Context, instanceToken string) error {
	rr.mu.Lock()
	hash, ui := rr.instanceTok, rr.ui
	rr.mu.Unlock()
	if hash == "" || instanceToken == "" || !crypto.ConstantTimeEqual(crypto.SHA256Hex(instanceToken), hash) {
		return desktopport.ErrInstanceToken
	}
	if ui != nil {
		ui.Focus()
	}
	return nil
}

// Open implements desktopport.Runtime.
func (rr *RemoteRuntime) Open(context.Context, *auth.Principal, string) error {
	return desktopport.ErrWrongMode
}

// Peripherals implements desktopport.Runtime.
func (rr *RemoteRuntime) Peripherals() desktopport.Peripherals { return rr.periph.get() }

// PeripheralSettings implements desktopport.Runtime: any user of the window may read them.
func (rr *RemoteRuntime) PeripheralSettings(context.Context, desktopport.AdminAuth) (desktopport.PeripheralSettings, error) {
	return rr.currentSettings().Peripherals.Port(), nil
}

// UpdatePeripheralSettings implements desktopport.Runtime (app PIN).
func (rr *RemoteRuntime) UpdatePeripheralSettings(ctx context.Context, a desktopport.AdminAuth, in desktopport.PeripheralSettings) (desktopport.PeripheralSettings, error) {
	if err := rr.admin(a); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	if err := rr.periph.validate(in); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	s, err := rr.saveSettings(func(s *FileSettings) { s.Peripherals = peripheralsFileOf(in) })
	if err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	out := s.Peripherals.Port()
	rr.periph.load(out)
	rr.cfg.Log.InfoContext(ctx, "desktop: peripherals updated", "receipt", out.Receipt.Driver, "label", out.Label.Driver,
		"reader", out.Reader.Driver, "fiscal", out.Fiscal.Driver)
	return out, nil
}

// SetKiosk implements desktopport.Runtime (app PIN).
func (rr *RemoteRuntime) SetKiosk(ctx context.Context, a desktopport.AdminAuth, enabled bool) (desktopport.KioskSettings, error) {
	if err := rr.admin(a); err != nil {
		return desktopport.KioskSettings{}, err
	}
	s, err := rr.saveSettings(func(s *FileSettings) { s.Kiosk.Enabled = enabled })
	if err != nil {
		return desktopport.KioskSettings{}, err
	}
	rr.cfg.Log.InfoContext(ctx, "desktop: kiosk setting changed", "enabled", enabled)
	return desktopport.KioskSettings{Enabled: s.Kiosk.Enabled}, nil
}

// ExitKiosk implements desktopport.Runtime (app PIN).
func (rr *RemoteRuntime) ExitKiosk(ctx context.Context, a desktopport.AdminAuth, _ desktopport.KioskExitCredentials) error {
	if err := rr.admin(a); err != nil {
		return err
	}
	rr.mu.Lock()
	rr.kioskActive = false
	ui := rr.ui
	rr.mu.Unlock()
	if k, ok := ui.(KioskUI); ok {
		k.SetKiosk(false, "")
	}
	rr.cfg.Log.InfoContext(ctx, "desktop: kiosk left")
	return nil
}

// PairRemote implements desktopport.Runtime: the app is already paired.
func (rr *RemoteRuntime) PairRemote(context.Context, desktopport.RemotePairInput) error {
	return desktopport.ErrAlreadyPaired
}

// UnpairRemote implements desktopport.Runtime (app PIN): revoke upstream (best effort), then reset locally.
func (rr *RemoteRuntime) UnpairRemote(ctx context.Context, a desktopport.AdminAuth) error {
	if err := rr.admin(a); err != nil {
		return err
	}
	rr.mu.Lock()
	p := rr.proxy
	rr.mu.Unlock()
	if p != nil && !p.Revoked() {
		if _, err := p.Upstream().JSON(ctx, http.MethodDelete, "/api/device/v1/self", nil, nil, nil); err != nil {
			rr.cfg.Log.WarnContext(ctx, "desktop: unpair upstream failed, resetting locally", "error", err)
		}
	}
	return rr.Reset(ctx, false)
}

// Reset deletes the token, the shell cache and the remote settings and asks the launcher to restart on the first
// run (local mode). revoked tells the first-run page to show "Dispositivo revocato".
func (rr *RemoteRuntime) Reset(ctx context.Context, revoked bool) error {
	if err := rr.tokens.Delete(); err != nil {
		rr.cfg.Log.WarnContext(ctx, "desktop: delete device token", "error", err)
	}
	rr.mu.Lock()
	p, reset := rr.proxy, rr.onReset
	rr.mu.Unlock()
	if p != nil {
		_ = p.Cache().Clear()
	} else {
		_ = (&remote.ShellCache{Dir: rr.cfg.Paths.RemoteCache()}).Clear()
	}
	if _, err := rr.saveSettings(func(s *FileSettings) {
		s.Mode, s.Remote, s.AppPinHash = desktopport.ModeLocal, nil, ""
	}); err != nil {
		return err
	}
	rr.cfg.Log.InfoContext(ctx, "desktop: remote mode reset", "revoked", revoked)
	if reset != nil {
		reset(revoked)
	}
	return nil
}

// wrongModeLAN is the local-network controller of the remote mode: nothing listens.
type wrongModeLAN struct{}

func (wrongModeLAN) Status() desktopport.LANStatus { return desktopport.LANStatus{} }

func (wrongModeLAN) Enable(context.Context, desktopport.LANOptions) (desktopport.LANStatus, error) {
	return desktopport.LANStatus{}, desktopport.ErrWrongMode
}

func (wrongModeLAN) Disable(context.Context) error { return desktopport.ErrWrongMode }

func (wrongModeLAN) RotateCertificate(context.Context, bool) (desktopport.LANStatus, error) {
	return desktopport.LANStatus{}, desktopport.ErrWrongMode
}
