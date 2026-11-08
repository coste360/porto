package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coste360/porto/internal/desktop/remote"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/http/api"
	"github.com/coste360/porto/internal/http/bridge"
	"github.com/coste360/porto/internal/peripheral"
)

// M6b methods of the local-mode Runtime (m6b-plan §7, §8): peripherals, kiosk and the pairing to an online
// instance on the first run. Administration needs an owner (AdminAuth.Principal); the app PIN does not apply in
// local mode.

// m6bState is the M6b part of the local Runtime.
type m6bState struct {
	periph *peripheralSet
	tokens TokenStore
	pin    *pinGuard

	mu          sync.Mutex
	kioskActive bool // launched in kiosk mode and not exited
	toRemote    bool // paired: the launcher restarts in remote mode
	kioskTries  []time.Time
}

func (rt *Runtime) initM6b() {
	c := rt.cfg
	rt.m6b.periph = newPeripheralSet(c.Paths.Peripherals(), peripheral.TargetOptions{Dev: c.Env.Dev, GOOS: c.GOOS})
	rt.m6b.periph.load(rt.currentSettings().Peripherals.Port())
	rt.m6b.tokens = deviceTokens(c.Paths, c.Env, c.Tokens)
	rt.m6b.pin = newPinGuard(c.Argon2, c.Clock)
}

// SetKioskActive records that the window runs in kiosk mode (the launcher, at start).
func (rt *Runtime) SetKioskActive(on bool) {
	rt.m6b.mu.Lock()
	rt.m6b.kioskActive = on
	rt.m6b.mu.Unlock()
}

func (rt *Runtime) kioskActive() bool {
	rt.m6b.mu.Lock()
	defer rt.m6b.mu.Unlock()
	return rt.m6b.kioskActive
}

// TakeModeSwitch reports (and clears) a pairing that asks the launcher to restart in remote mode.
func (rt *Runtime) TakeModeSwitch() bool {
	rt.m6b.mu.Lock()
	defer rt.m6b.mu.Unlock()
	v := rt.m6b.toRemote
	rt.m6b.toRemote = false
	return v
}

// FakeReader returns the fake QR reader of the peripherals (tests, e2e).
func (rt *Runtime) FakeReader() *peripheral.FakeReader { return rt.m6b.periph.FakeReader() }

// Peripherals implements desktopport.Runtime.
func (rt *Runtime) Peripherals() desktopport.Peripherals { return rt.m6b.periph.get() }

// PeripheralSettings implements desktopport.Runtime.
func (rt *Runtime) PeripheralSettings(_ context.Context, a desktopport.AdminAuth) (desktopport.PeripheralSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	return rt.currentSettings().Peripherals.Port(), nil
}

// UpdatePeripheralSettings implements desktopport.Runtime.
func (rt *Runtime) UpdatePeripheralSettings(ctx context.Context, a desktopport.AdminAuth, in desktopport.PeripheralSettings) (desktopport.PeripheralSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	if err := rt.m6b.periph.validate(in); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	s, err := rt.saveSettings(func(s *FileSettings) { s.Peripherals = peripheralsFileOf(in) })
	if err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	out := s.Peripherals.Port()
	rt.m6b.periph.load(out)
	rt.writeAudit(ctx, a.Principal, "desktop.peripherals_update", peripheralAudit(out))
	return out, nil
}

// SetKiosk implements desktopport.Runtime: applied from the next launch.
func (rt *Runtime) SetKiosk(ctx context.Context, a desktopport.AdminAuth, enabled bool) (desktopport.KioskSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.KioskSettings{}, err
	}
	s, err := rt.saveSettings(func(s *FileSettings) { s.Kiosk.Enabled = enabled })
	if err != nil {
		return desktopport.KioskSettings{}, err
	}
	rt.writeAudit(ctx, a.Principal, "desktop.kiosk_update", map[string]any{"enabled": enabled})
	return desktopport.KioskSettings{Enabled: s.Kiosk.Enabled}, nil
}

// ExitKiosk implements desktopport.Runtime: any signed-in user of the window plus the credentials of an owner
// (the front desk user is usually not one). 5 failures in 15 minutes, then 429.
func (rt *Runtime) ExitKiosk(ctx context.Context, a desktopport.AdminAuth, cred desktopport.KioskExitCredentials) error {
	if a.Principal == nil {
		return auth.ErrUnauthenticated
	}
	if wait, ok := rt.kioskAttempt(); !ok {
		return &auth.RateLimitError{RetryAfter: wait}
	}
	// Wrong credentials are a 400 on the password field, not a 401: the window's own session is fine and a 401
	// would sign the front desk user out.
	badCred := &auth.ValidationError{Fields: []auth.FieldError{{Field: "password", Code: "invalid_credentials"}}}
	app, _ := rt.current()
	if app == nil || cred.Email == "" || cred.Password == "" {
		return badCred
	}
	res, err := app.Auth.Login(ctx, auth.LoginInput{Email: cred.Email, Password: cred.Password})
	if err != nil {
		if errors.Is(err, auth.ErrRateLimited) {
			return err
		}
		return badCred
	}
	_ = app.Auth.Logout(ctx, res.Token) // the check needs no session
	if !res.Principal.IsOwnerAnywhere() {
		return badCred
	}
	rt.kioskReset()
	rt.SetKioskActive(false)
	rt.mu.RLock()
	ui := rt.ui
	rt.mu.RUnlock()
	if k, ok := ui.(KioskUI); ok {
		k.SetKiosk(false, "")
	}
	rt.writeAudit(ctx, res.Principal, "desktop.kiosk_exit", nil)
	return nil
}

func (rt *Runtime) kioskAttempt() (time.Duration, bool) {
	rt.m6b.mu.Lock()
	defer rt.m6b.mu.Unlock()
	now := rt.cfg.Clock.Now()
	kept := rt.m6b.kioskTries[:0]
	for _, t := range rt.m6b.kioskTries {
		if now.Sub(t) < pinWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= pinLimit {
		rt.m6b.kioskTries = kept
		return pinWindow - now.Sub(kept[0]), false
	}
	kept = append(kept, now)
	rt.m6b.kioskTries = kept
	return 0, true
}

func (rt *Runtime) kioskReset() {
	rt.m6b.mu.Lock()
	rt.m6b.kioskTries = nil
	rt.m6b.mu.Unlock()
}

// PairRemote implements desktopport.Runtime (m6b-plan §7.2): on the first run only, pair with the online
// instance, keep the token in the SecretStore, set the app PIN and restart in remote mode. A failure leaves the
// first-run token usable.
func (rt *Runtime) PairRemote(ctx context.Context, in desktopport.RemotePairInput) error {
	a, _ := rt.current()
	if a == nil {
		return desktopport.ErrAlreadyPaired
	}
	if n, err := rt.users(ctx); err != nil {
		return err
	} else if n > 0 || rt.currentSettings().Mode == desktopport.ModeRemote {
		return desktopport.ErrAlreadyPaired
	}
	link := remote.PairLink{URL: in.URL, Code: in.Code, Fingerprint: in.Fingerprint}
	if in.Link != "" {
		l, err := remote.ParsePairLink(in.Link)
		if err != nil {
			return err
		}
		link = l
	}
	if !ValidPin(in.Pin) {
		return &desktopport.SettingsError{Fields: []string{"pin"}}
	}
	finish, ok := rt.token.begin(in.FirstRunToken)
	if !ok {
		return desktopport.ErrFirstRunToken
	}
	done := false
	defer func() { finish(done) }()
	hash, err := rt.m6b.pin.hash(in.Pin)
	if err != nil {
		return err
	}
	paired, err := remote.Pair(ctx, remote.PairInput{
		URL: link.URL, Code: link.Code, Fingerprint: link.Fingerprint, Name: in.Name, Platform: rt.cfg.GOOS,
		AppVersion: rt.cfg.Version, Dev: rt.cfg.Env.Dev,
	})
	if err != nil {
		return err
	}
	if err := rt.m6b.tokens.Set(paired.Token); err != nil {
		return err
	}
	now := rt.cfg.Clock.Now().UTC()
	if _, err := rt.saveSettings(func(s *FileSettings) {
		s.Mode, s.AppPinHash = desktopport.ModeRemote, hash
		s.Remote = &RemoteFile{
			URL: paired.Origin.String(), DeviceID: paired.DeviceID, MarinaID: paired.MarinaID,
			MarinaName: paired.MarinaName, LeafFP: paired.Pins.LeafFP, CAFP: paired.Pins.CAFP, PairedAt: now,
		}
	}); err != nil {
		_ = rt.m6b.tokens.Delete()
		return err
	}
	done = true
	rt.cfg.Log.InfoContext(ctx, "desktop: paired with an online instance, restarting in remote mode",
		"device_id", paired.DeviceID, "marina_id", paired.MarinaID, "pinned", paired.Pins.LeafFP != "")
	rt.m6b.mu.Lock()
	rt.m6b.toRemote = true
	rt.m6b.mu.Unlock()
	a.RequestRestart()
	return nil
}

// UnpairRemote implements desktopport.Runtime: local mode has nothing to unpair.
func (rt *Runtime) UnpairRemote(context.Context, desktopport.AdminAuth) error {
	return desktopport.ErrWrongMode
}

// BridgeReceipt implements bridge.ReceiptSource: the receipt data of a payment of a marina the principal can
// collect for (getPaymentReceipt format=json in process; the cash service checks the permissions).
func (rt *Runtime) BridgeReceipt(ctx context.Context, p *auth.Principal, paymentID string) (desktopport.ReceiptDoc, error) {
	a, _ := rt.current()
	if a == nil || p == nil {
		return desktopport.ReceiptDoc{}, desktopport.ErrNotFound
	}
	orgs, err := a.Marinas.ListOrganizations(ctx, p)
	if err != nil {
		return desktopport.ReceiptDoc{}, err
	}
	for _, o := range orgs {
		ms, err := a.Marinas.ListMarinas(ctx, p, o.ID)
		if err != nil {
			continue
		}
		for _, m := range ms {
			b, _, err := a.Cash.Receipt(ctx, p, m.ID, paymentID, "json")
			if err != nil {
				continue
			}
			var d api.ReceiptData
			if err := json.Unmarshal(b, &d); err != nil {
				return desktopport.ReceiptDoc{}, err
			}
			return bridge.ReceiptDocOf(d), nil
		}
	}
	return desktopport.ReceiptDoc{}, desktopport.ErrNotFound
}

var _ bridge.ReceiptSource = (*Runtime)(nil)
