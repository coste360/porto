// Package desktoptest is a minimal desktopport.Runtime for tests that need the desktop edition of the App without
// the launcher (m6a-plan §11.3): the LAN acceptance test builds the real App with it and its own local-network
// server. Status, the owner check of Settings and LAN work; the launcher operations answer ErrNotFound.
package desktoptest

import (
	"context"
	"sync"

	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
)

// Runtime is the minimal desktop runtime.
type Runtime struct {
	Version string

	mu  sync.RWMutex
	lan desktopport.LANController
}

var _ desktopport.Runtime = (*Runtime)(nil)

// NewRuntime returns a runtime whose LAN() is lan (may be nil, then set later with SetLAN: the LAN server needs
// the App, which needs the runtime).
func NewRuntime(lan desktopport.LANController) *Runtime { return &Runtime{Version: "test", lan: lan} }

// SetLAN replaces the local-network controller.
func (r *Runtime) SetLAN(lan desktopport.LANController) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lan = lan
}

func owner(p *auth.Principal) error {
	if !p.IsOwnerAnywhere() {
		return auth.ErrForbidden
	}
	return nil
}

// Status implements desktopport.Runtime.
func (r *Runtime) Status(context.Context) desktopport.Status {
	return desktopport.Status{Version: r.Version, Mode: desktopport.ModeLocal, SecretStore: desktopport.SecretStoreFile}
}

// FirstRun implements desktopport.Runtime.
func (*Runtime) FirstRun(context.Context, desktopport.FirstRunInput) (*desktopport.FirstRunResult, error) {
	return nil, desktopport.ErrFirstRunDone
}

// FirstRunRestore implements desktopport.Runtime.
func (*Runtime) FirstRunRestore(context.Context, string, desktopport.RestoreSource) error {
	return desktopport.ErrFirstRunToken
}

// Settings implements desktopport.Runtime.
func (*Runtime) Settings(_ context.Context, p *auth.Principal) (desktopport.Settings, error) {
	if err := owner(p); err != nil {
		return desktopport.Settings{}, err
	}
	return desktopport.Settings{Mode: desktopport.ModeLocal, LANPort: 8443, BackupKeepDaily: 14, BackupKeepExit: 5}, nil
}

// UpdateSettings implements desktopport.Runtime.
func (r *Runtime) UpdateSettings(ctx context.Context, p *auth.Principal, _ desktopport.SettingsPatch) (desktopport.Settings, error) {
	return r.Settings(ctx, p)
}

// LAN implements desktopport.Runtime: the controller checks the owner of the context like the real runtime.
func (r *Runtime) LAN() desktopport.LANController {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return guard{r.lan}
}

// Backups implements desktopport.Runtime.
func (*Runtime) Backups(_ context.Context, p *auth.Principal) ([]desktopport.BackupFile, desktopport.BackupStatus, error) {
	if err := owner(p); err != nil {
		return nil, desktopport.BackupStatus{}, err
	}
	return nil, desktopport.BackupStatus{}, nil
}

// BackupNow implements desktopport.Runtime.
func (*Runtime) BackupNow(_ context.Context, p *auth.Principal) (desktopport.BackupFile, error) {
	if err := owner(p); err != nil {
		return desktopport.BackupFile{}, err
	}
	return desktopport.BackupFile{}, desktopport.ErrNotFound
}

// Restore implements desktopport.Runtime.
func (*Runtime) Restore(_ context.Context, p *auth.Principal, _ desktopport.RestoreSource) error {
	if err := owner(p); err != nil {
		return err
	}
	return desktopport.ErrNotFound
}

// RevealSecretKey implements desktopport.Runtime.
func (*Runtime) RevealSecretKey(_ context.Context, p *auth.Principal, _ string) (string, error) {
	if err := owner(p); err != nil {
		return "", err
	}
	return "", desktopport.ErrNotFound
}

// Activate implements desktopport.Runtime.
func (*Runtime) Activate(context.Context, string) error { return desktopport.ErrInstanceToken }

// Open implements desktopport.Runtime.
func (*Runtime) Open(_ context.Context, p *auth.Principal, _ string) error {
	if err := owner(p); err != nil {
		return err
	}
	return desktopport.ErrNotFound
}

type guard struct{ inner desktopport.LANController }

func (g guard) Status() desktopport.LANStatus {
	if g.inner == nil {
		return desktopport.LANStatus{}
	}
	return g.inner.Status()
}

func (g guard) check(ctx context.Context) error {
	p, _ := auth.PrincipalFrom(ctx)
	if err := owner(p); err != nil {
		return err
	}
	if g.inner == nil {
		return desktopport.ErrLANNoAddress
	}
	return nil
}

func (g guard) Enable(ctx context.Context, o desktopport.LANOptions) (desktopport.LANStatus, error) {
	if err := g.check(ctx); err != nil {
		return g.Status(), err
	}
	return g.inner.Enable(ctx, o)
}

func (g guard) Disable(ctx context.Context) error {
	if err := g.check(ctx); err != nil {
		return err
	}
	return g.inner.Disable(ctx)
}

func (g guard) RotateCertificate(ctx context.Context, all bool) (desktopport.LANStatus, error) {
	if err := g.check(ctx); err != nil {
		return g.Status(), err
	}
	return g.inner.RotateCertificate(ctx, all)
}

// M6b methods of desktopport.Runtime: no peripherals, local mode only.

// Peripherals implements desktopport.Runtime.
func (*Runtime) Peripherals() desktopport.Peripherals { return desktopport.Peripherals{} }

// PeripheralSettings implements desktopport.Runtime.
func (*Runtime) PeripheralSettings(_ context.Context, a desktopport.AdminAuth) (desktopport.PeripheralSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	return desktopport.PeripheralSettings{}, nil
}

// UpdatePeripheralSettings implements desktopport.Runtime.
func (*Runtime) UpdatePeripheralSettings(_ context.Context, a desktopport.AdminAuth, in desktopport.PeripheralSettings) (desktopport.PeripheralSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.PeripheralSettings{}, err
	}
	return in, nil
}

// SetKiosk implements desktopport.Runtime.
func (*Runtime) SetKiosk(_ context.Context, a desktopport.AdminAuth, enabled bool) (desktopport.KioskSettings, error) {
	if err := owner(a.Principal); err != nil {
		return desktopport.KioskSettings{}, err
	}
	return desktopport.KioskSettings{Enabled: enabled}, nil
}

// ExitKiosk implements desktopport.Runtime.
func (*Runtime) ExitKiosk(context.Context, desktopport.AdminAuth, desktopport.KioskExitCredentials) error {
	return desktopport.ErrAppPinInvalid
}

// PairRemote implements desktopport.Runtime.
func (*Runtime) PairRemote(context.Context, desktopport.RemotePairInput) error {
	return desktopport.ErrFirstRunToken
}

// UnpairRemote implements desktopport.Runtime.
func (*Runtime) UnpairRemote(context.Context, desktopport.AdminAuth) error {
	return desktopport.ErrWrongMode
}
