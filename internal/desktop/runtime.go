package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/backup"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/crypto"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/audit"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/migrations"
	"github.com/coste360/porto/pkg/ext"
)

// Secret key reveal limit (m6a-plan §2.6): 5 attempts per user in 15 minutes.
const (
	revealLimit  = 5
	revealWindow = 15 * time.Minute
)

// UserCounter counts the users of the App's database (first run detection). cmd/porto-desktop wires it to the
// store, which this package does not import.
type UserCounter func(ctx context.Context, a *app.App) (int, error)

// RuntimeConfig builds a Runtime.
type RuntimeConfig struct {
	Version     string
	Paths       Paths
	Env         Env
	Clock       clock.Clock
	Loc         *time.Location
	Log         *slog.Logger
	Settings    FileSettings
	SecretKey   string
	SecretKind  string
	Opener      Opener
	CountUsers  UserCounter
	Updater     *Updater
	InstanceTok string // hash of the activation token (instance.json)
}

// Runtime implements desktopport.Runtime over the running App. It outlives the App: a restore builds a new App
// and attaches it, the window, first-run token, update state and settings stay.
type Runtime struct {
	cfg     RuntimeConfig
	backups *Backups
	audit   *audit.Writer

	token   firstRunToken
	recheck chan struct{}

	setMu    sync.Mutex
	settings FileSettings

	mu       sync.RWMutex
	app      *app.App
	lan      LAN
	ui       UI
	restore  string // staging folder of a prepared restore, consumed by the launcher
	origin   string // loopback origin http://127.0.0.1:<port>
	attempts map[string][]time.Time
}

var _ desktopport.Runtime = (*Runtime)(nil)

// NewRuntime returns a Runtime with no App attached yet.
func NewRuntime(c RuntimeConfig) *Runtime {
	if c.Clock == nil {
		c.Clock = clock.System{}
	}
	if c.Log == nil {
		c.Log = slog.New(slog.DiscardHandler)
	}
	rt := &Runtime{
		cfg: c, settings: c.Settings, audit: audit.NewWriter(c.Clock), recheck: make(chan struct{}, 1),
		attempts: map[string][]time.Time{},
	}
	rt.backups = &Backups{Paths: c.Paths, Clock: c.Clock, Loc: c.Loc, Log: c.Log, Settings: rt.currentSettings}
	if c.Updater != nil {
		c.Updater.Enabled = rt.updateCheckOn
	}
	return rt
}

// BackupManager returns the backup manager (scheduler, exit backup).
func (rt *Runtime) BackupManager() *Backups { return rt.backups }

func (rt *Runtime) currentSettings() FileSettings {
	rt.setMu.Lock()
	defer rt.setMu.Unlock()
	return rt.settings
}

func (rt *Runtime) updateCheckOn() bool {
	return !rt.cfg.Env.UpdateCheckOff && rt.currentSettings().UpdateCheck
}

// saveSettings changes the settings under the lock and writes desktop.json.
func (rt *Runtime) saveSettings(change func(*FileSettings)) (FileSettings, error) {
	rt.setMu.Lock()
	defer rt.setMu.Unlock()
	s := rt.settings
	change(&s)
	if err := SaveSettings(rt.cfg.Paths.Settings(), s); err != nil {
		return rt.settings, err
	}
	rt.settings = s
	return s, nil
}

// Attach makes a the App the runtime works on, with its local-network server, UI and loopback origin.
func (rt *Runtime) Attach(a *app.App, lan LAN, ui UI, origin string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.app, rt.lan, rt.ui, rt.origin = a, lan, ui, origin
}

func (rt *Runtime) current() (*app.App, LAN) {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.app, rt.lan
}

// PairTarget is app.Options.PairTarget: the local-network URL while it listens, else the loopback URL marked
// unreachable for phones.
func (rt *Runtime) PairTarget() desktopport.PairTarget {
	rt.mu.RLock()
	lan, origin := rt.lan, rt.origin
	rt.mu.RUnlock()
	if lan != nil {
		if t := lan.PairTarget(); t.Origin != "" && t.Reachable {
			return t
		}
	}
	return desktopport.PairTarget{Origin: origin}
}

// MintFirstRunToken creates the first-run token (no users yet) and returns it.
func (rt *Runtime) MintFirstRunToken() (string, error) { return rt.token.mint() }

// TakeRestore returns and clears the staging folder of a prepared restore ("" when none).
func (rt *Runtime) TakeRestore() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	s := rt.restore
	rt.restore = ""
	return s
}

func (rt *Runtime) users(ctx context.Context) (int, error) {
	a, _ := rt.current()
	if a == nil || rt.cfg.CountUsers == nil {
		return 0, errors.New("desktop: runtime not attached")
	}
	return rt.cfg.CountUsers(ctx, a)
}

// Status implements desktopport.Runtime.
func (rt *Runtime) Status(ctx context.Context) desktopport.Status {
	st := desktopport.Status{Version: rt.cfg.Version, Mode: desktopport.ModeLocal, SecretStore: rt.cfg.SecretKind}
	if rt.token.pending() {
		if n, err := rt.users(ctx); err == nil && n == 0 {
			st.FirstRunPending = true
		}
	}
	if u := rt.cfg.Updater.Latest(); u != nil && u.Version != rt.currentSettings().DismissedUpdate {
		st.Update = u
	}
	return st
}

// FirstRun implements desktopport.Runtime: the first owner and organization, then a session for the window.
func (rt *Runtime) FirstRun(ctx context.Context, in desktopport.FirstRunInput) (*desktopport.FirstRunResult, error) {
	a, _ := rt.current()
	if a == nil {
		return nil, desktopport.ErrFirstRunDone
	}
	n, err := rt.users(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, desktopport.ErrFirstRunDone
	}
	finish, ok := rt.token.begin(in.Token)
	if !ok {
		return nil, desktopport.ErrFirstRunToken
	}
	done := false
	defer func() { finish(done) }()
	if n, err := rt.users(ctx); err != nil || n > 0 { // created meanwhile (CLI, bootstrap)
		if err == nil {
			done, err = true, desktopport.ErrFirstRunDone
		}
		return nil, err
	}
	res, err := a.Auth.CreateOwner(ctx, auth.CreateOwnerInput{
		Email: in.Email, Name: in.Name, Password: in.Password, Organization: in.Organization,
	})
	if err != nil {
		return nil, err // validation errors keep the token usable
	}
	done = true
	if _, err := rt.saveSettings(func(s *FileSettings) { s.UpdateCheck = in.UpdateCheck }); err != nil {
		rt.cfg.Log.WarnContext(ctx, "desktop: save settings after first run", "error", err)
	}
	if in.UpdateCheck {
		rt.kickUpdate()
	}
	_ = os.Remove(rt.cfg.Paths.FirstRunURL())
	login, err := a.Auth.Login(ctx, auth.LoginInput{Email: in.Email, Password: in.Password})
	if err != nil {
		return nil, fmt.Errorf("desktop: first run session: %w", err)
	}
	rt.cfg.Log.InfoContext(ctx, "desktop first run completed", "user_id", res.UserID, "organization_id", res.OrganizationID)
	return &desktopport.FirstRunResult{UserID: res.UserID, SessionToken: login.Token, ExpiresAt: login.ExpiresAt}, nil
}

// FirstRunRestore implements desktopport.Runtime: like FirstRun, it is refused once an owner exists, and the
// token is reserved for the attempt and burnt only when the restore is prepared (a corrupt archive leaves the
// first-run page usable).
func (rt *Runtime) FirstRunRestore(ctx context.Context, token string, src desktopport.RestoreSource) error {
	n, err := rt.users(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return desktopport.ErrFirstRunDone
	}
	finish, ok := rt.token.begin(token)
	if !ok {
		return desktopport.ErrFirstRunToken
	}
	done := false
	defer func() { finish(done) }()
	if n, err := rt.users(ctx); err != nil || n > 0 { // created meanwhile (CLI, bootstrap)
		if err == nil {
			done, err = true, desktopport.ErrFirstRunDone
		}
		return err
	}
	if err := rt.Restore(ctx, nil, src); err != nil {
		return err
	}
	done = true
	_ = os.Remove(rt.cfg.Paths.FirstRunURL())
	return nil
}

func owner(p *auth.Principal) error {
	if !p.IsOwnerAnywhere() {
		return auth.ErrForbidden
	}
	return nil
}

func (rt *Runtime) writeAudit(ctx context.Context, p *auth.Principal, action string, diff map[string]any) {
	a, _ := rt.current()
	if a == nil {
		return
	}
	if err := rt.auditTo(ctx, a, p, action, diff); err != nil {
		rt.cfg.Log.WarnContext(ctx, "desktop audit", "action", action, "error", err)
	}
}

// auditTo writes a desktop audit row into the database of a (the live App, or the staging one of a restore).
func (rt *Runtime) auditTo(ctx context.Context, a *app.App, p *auth.Principal, action string, diff map[string]any) error {
	e := audit.Entry{Action: action, EntityType: "desktop", EntityID: "desktop", Diff: diff, Actor: audit.System}
	if p != nil {
		e.OrganizationID, e.Actor = p.FirstOwnedOrganization(), p.Actor()
	}
	return rt.audit.Write(ctx, a.DB.DB, e)
}

// Settings implements desktopport.Runtime.
func (rt *Runtime) Settings(_ context.Context, p *auth.Principal) (desktopport.Settings, error) {
	if err := owner(p); err != nil {
		return desktopport.Settings{}, err
	}
	return rt.view(), nil
}

func (rt *Runtime) view() desktopport.Settings {
	v := rt.currentSettings().view(rt.cfg.Paths)
	if rt.cfg.Env.UpdateCheckOff {
		v.UpdateCheck = false
	}
	return v
}

// UpdateSettings implements desktopport.Runtime.
func (rt *Runtime) UpdateSettings(ctx context.Context, p *auth.Principal, in desktopport.SettingsPatch) (desktopport.Settings, error) {
	if err := owner(p); err != nil {
		return desktopport.Settings{}, err
	}
	var changed []string
	var applyErr error
	before := rt.currentSettings()
	_, err := rt.saveSettings(func(s *FileSettings) {
		next, ch, err := s.Apply(in)
		if err != nil {
			applyErr = err
			return
		}
		*s, changed = next, ch
	})
	if applyErr != nil {
		return desktopport.Settings{}, applyErr
	}
	if err != nil {
		return desktopport.Settings{}, err
	}
	if len(changed) > 0 {
		rt.writeAudit(ctx, p, "desktop.settings_update", map[string]any{"fields": changed})
	}
	if in.UpdateCheck != nil && *in.UpdateCheck && !before.UpdateCheck {
		rt.kickUpdate()
	}
	return rt.view(), nil
}

func (rt *Runtime) kickUpdate() {
	select {
	case rt.recheck <- struct{}{}:
	default:
	}
}

// Recheck is the channel the Updater listens on for an immediate check.
func (rt *Runtime) Recheck() <-chan struct{} { return rt.recheck }

// LAN implements desktopport.Runtime: the controller seen by the HTTP layer checks the owner (principal of the
// context), persists the switch and audits.
func (rt *Runtime) LAN() desktopport.LANController { return lanGuard{rt} }

type lanGuard struct{ rt *Runtime }

func (g lanGuard) inner() LAN {
	_, lan := g.rt.current()
	if lan == nil {
		return &unavailableLAN{}
	}
	return lan
}

func (g lanGuard) principal(ctx context.Context) (*auth.Principal, error) {
	p, _ := auth.PrincipalFrom(ctx)
	return p, owner(p)
}

func (g lanGuard) Status() desktopport.LANStatus { return g.inner().Status() }

func (g lanGuard) Enable(ctx context.Context, o desktopport.LANOptions) (desktopport.LANStatus, error) {
	p, err := g.principal(ctx)
	if err != nil {
		return desktopport.LANStatus{}, err
	}
	if o.Port < 0 || o.Port > 65535 {
		return desktopport.LANStatus{}, &desktopport.SettingsError{Fields: []string{"port"}}
	}
	st, err := g.inner().Enable(ctx, o)
	if err != nil {
		return st, err
	}
	if _, err := g.rt.saveSettings(func(s *FileSettings) {
		s.LANEnabled = true
		if o.Port != 0 {
			s.LANPort = o.Port
		}
		s.LANAddress = o.Address
	}); err != nil {
		return st, err
	}
	g.rt.writeAudit(ctx, p, "desktop.lan_enable", map[string]any{"port": st.Port, "automatic_address": o.Address == ""})
	return st, nil
}

func (g lanGuard) Disable(ctx context.Context) error {
	p, err := g.principal(ctx)
	if err != nil {
		return err
	}
	if err := g.inner().Disable(ctx); err != nil {
		return err
	}
	if _, err := g.rt.saveSettings(func(s *FileSettings) { s.LANEnabled = false }); err != nil {
		return err
	}
	g.rt.writeAudit(ctx, p, "desktop.lan_disable", nil)
	return nil
}

func (g lanGuard) RotateCertificate(ctx context.Context, all bool) (desktopport.LANStatus, error) {
	p, err := g.principal(ctx)
	if err != nil {
		return desktopport.LANStatus{}, err
	}
	st, err := g.inner().RotateCertificate(ctx, all)
	if err != nil {
		return st, err
	}
	g.rt.writeAudit(ctx, p, "desktop.lan_cert_rotate", map[string]any{"all": all})
	return st, nil
}

// Snapshot writes a portable backup of the attached App (database and file store).
func (rt *Runtime) Snapshot(ctx context.Context, w io.Writer) error {
	a, _ := rt.current()
	if a == nil {
		return errors.New("desktop: runtime not attached")
	}
	_, err := backup.Backup(ctx, a.DB, w, rt.cfg.Version, rt.cfg.Paths.Files())
	return err
}

// Backups implements desktopport.Runtime.
func (rt *Runtime) Backups(_ context.Context, p *auth.Principal) ([]desktopport.BackupFile, desktopport.BackupStatus, error) {
	if err := owner(p); err != nil {
		return nil, desktopport.BackupStatus{}, err
	}
	return rt.backups.List()
}

// BackupNow implements desktopport.Runtime.
func (rt *Runtime) BackupNow(ctx context.Context, p *auth.Principal) (desktopport.BackupFile, error) {
	if err := owner(p); err != nil {
		return desktopport.BackupFile{}, err
	}
	f, err := rt.backups.Take(ctx, desktopport.BackupManual, rt.Snapshot)
	if err != nil {
		return f, err
	}
	rt.writeAudit(ctx, p, "desktop.backup", map[string]any{"kind": f.Kind})
	return f, nil
}

// Restore implements desktopport.Runtime (m6a-plan §2.6): the archive is restored into a fresh database in the
// staging folder after a pre-restore backup; the launcher swaps it in on restart. Any failure leaves the live
// database untouched (ErrRestoreFailed).
func (rt *Runtime) Restore(ctx context.Context, p *auth.Principal, src desktopport.RestoreSource) error {
	if p != nil {
		if err := owner(p); err != nil {
			return err
		}
	}
	a, _ := rt.current()
	if a == nil {
		return desktopport.ErrRestoreFailed
	}
	if !rt.backups.TryLock() {
		return desktopport.ErrBackupInProgress
	}
	defer rt.backups.Unlock()

	archive, source := "", "upload"
	if src.Upload != nil {
		path, err := rt.receiveUpload(src.Upload)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(path) }()
		archive = path
	} else {
		path, err := rt.backups.Find(src.BackupID)
		if err != nil {
			return err
		}
		archive, source = path, "backup"
	}
	if _, err := rt.backups.TakeLocked(ctx, desktopport.BackupPreRestore, rt.Snapshot); err != nil {
		return fmt.Errorf("%w: pre-restore backup", desktopport.ErrRestoreFailed)
	}
	diff := map[string]any{"source": source}
	staging, err := rt.prepareRestore(ctx, a, archive, func(ctx context.Context, staged *app.App) error {
		return rt.auditTo(ctx, staged, p, "desktop.restore", diff)
	})
	if err != nil {
		rt.cfg.Log.WarnContext(ctx, "desktop restore failed", "error", err)
		if errors.Is(err, desktopport.ErrRestoreBackupNewer) {
			return err
		}
		return fmt.Errorf("%w: %w", desktopport.ErrRestoreFailed, err)
	}
	// The live database is moved aside on restart: the row there records that it was replaced; the restored
	// database got its own row in prepareRestore.
	rt.writeAudit(ctx, p, "desktop.restore", diff)
	rt.mu.Lock()
	rt.restore = staging
	rt.mu.Unlock()
	rt.cfg.Log.InfoContext(ctx, "desktop restore prepared, restarting", "source", source)
	a.RequestRestart()
	return nil
}

func (rt *Runtime) receiveUpload(r io.Reader) (string, error) {
	stamp := rt.cfg.Clock.Now().UTC().Format("20060102-150405")
	path := filepath.Join(rt.cfg.Paths.Root, "restore-"+stamp+".tar.gz")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // G304: fixed name in the data folder
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(r, desktopport.MaxRestoreUpload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > desktopport.MaxRestoreUpload {
		err = errors.New("archive too large")
	}
	if err == nil && n == 0 {
		err = errors.New("empty archive")
	}
	if err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("%w: upload: %w", desktopport.ErrRestoreFailed, err)
	}
	return path, nil
}

// prepareRestore migrates a fresh database in the staging folder and restores the archive into it (rows and
// files). The live database is only read (the pre-restore backup), never written.
// prepareRestore restores archive into a fresh database in the staging folder. The staging database is migrated
// to the archive's schema version, loaded, then migrated up to the current schema, so a backup taken before an
// app update still restores; an archive newer than the app is ErrRestoreBackupNewer. record writes into the
// restored database before it is closed (the restore's audit row).
func (rt *Runtime) prepareRestore(ctx context.Context, live *app.App, archive string, record func(context.Context, *app.App) error) (string, error) {
	schema, err := archiveSchema(archive)
	if err != nil {
		return "", fmt.Errorf("restore: %w", err)
	}
	latest, err := migrations.Latest(migrations.SQLite)
	if err != nil {
		return "", err
	}
	if schema > latest {
		return "", fmt.Errorf("%w: archive schema %d, app schema %d", desktopport.ErrRestoreBackupNewer, schema, latest)
	}
	staging := rt.cfg.Paths.RestoreStaging()
	if err := os.RemoveAll(staging); err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return "", fmt.Errorf("staging: %w", err)
	}
	fail := func(err error) (string, error) {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("restore: %w", err)
	}
	cfg := *live.Config
	cfg.DataDir, cfg.DatabaseURL, cfg.BootstrapOwnerEmail = staging, "", ""
	sa, err := app.NewWithOptions(ctx, &cfg, rt.cfg.Version, ext.Extensions{}, rt.cfg.Log, app.Options{Clock: rt.cfg.Clock})
	if err != nil {
		return fail(err)
	}
	err = restoreInto(ctx, sa, archive, schema, filepath.Join(staging, "files"), record)
	if cerr := sa.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fail(err)
	}
	return staging, nil
}

// restoreInto brings the empty database of sa to schema, restores archive, migrates up and calls record.
func restoreInto(ctx context.Context, sa *app.App, archive string, schema int64, filesDir string, record func(context.Context, *app.App) error) error {
	mig, err := sa.DB.Migrator()
	if err != nil {
		return err
	}
	if err := mig.UpTo(ctx, schema); err != nil {
		return err
	}
	f, err := os.Open(archive) //nolint:gosec // G304: a listed backup or the uploaded file in the data folder
	if err != nil {
		return err
	}
	_, err = backup.Restore(ctx, sa.DB, f, false, filesDir)
	_ = f.Close()
	if err != nil {
		return err
	}
	if err := sa.DB.Migrate(ctx); err != nil {
		return err
	}
	return record(ctx, sa)
}

func archiveSchema(archive string) (int64, error) {
	f, err := os.Open(archive) //nolint:gosec // G304: a listed backup or the uploaded file in the data folder
	if err != nil {
		return 0, err
	}
	defer f.Close()
	m, err := backup.ReadManifest(f)
	if err != nil {
		return 0, err
	}
	return m.SchemaVersion, nil
}

// RevealSecretKey implements desktopport.Runtime.
func (rt *Runtime) RevealSecretKey(ctx context.Context, p *auth.Principal, password string) (string, error) {
	if err := owner(p); err != nil {
		return "", err
	}
	if wait, ok := rt.takeAttempt(p.UserID); !ok {
		return "", &auth.RateLimitError{RetryAfter: wait}
	}
	a, _ := rt.current()
	if a == nil {
		return "", auth.ErrInvalidCredentials
	}
	me, err := a.Auth.Me(ctx, p)
	if err != nil {
		return "", err
	}
	ok, err := a.Auth.Hasher().Verify(password, me.User.PasswordHash)
	if err != nil || !ok || me.User.PasswordHash == "" {
		return "", auth.ErrInvalidCredentials
	}
	rt.writeAudit(ctx, p, "desktop.secret_reveal", nil)
	return rt.cfg.SecretKey, nil
}

func (rt *Runtime) takeAttempt(userID string) (time.Duration, bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	now := rt.cfg.Clock.Now()
	kept := rt.attempts[userID][:0]
	for _, t := range rt.attempts[userID] {
		if now.Sub(t) < revealWindow {
			kept = append(kept, t)
		}
	}
	if len(kept) >= revealLimit {
		rt.attempts[userID] = kept
		return revealWindow - now.Sub(kept[0]), false
	}
	rt.attempts[userID] = append(kept, now)
	return 0, true
}

// Activate implements desktopport.Runtime.
func (rt *Runtime) Activate(_ context.Context, instanceToken string) error {
	if rt.cfg.InstanceTok == "" || instanceToken == "" ||
		!crypto.ConstantTimeEqual(crypto.SHA256Hex(instanceToken), rt.cfg.InstanceTok) {
		return desktopport.ErrInstanceToken
	}
	rt.mu.RLock()
	ui := rt.ui
	rt.mu.RUnlock()
	if ui != nil {
		ui.Focus()
	}
	return nil
}

// SetInstanceToken records the hash of the activation token (instance.json).
func (rt *Runtime) SetInstanceToken(hash string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cfg.InstanceTok = hash
}

// Open implements desktopport.Runtime: only the fixed folders, never a client path.
func (rt *Runtime) Open(ctx context.Context, p *auth.Principal, target string) error {
	if err := owner(p); err != nil {
		return err
	}
	var path string
	switch target {
	case desktopport.OpenDataDir:
		path = rt.cfg.Paths.Root
	case desktopport.OpenBackupDir:
		path, _ = rt.backups.dir()
	case desktopport.OpenLogs:
		path = rt.cfg.Paths.Logs()
		_ = os.MkdirAll(path, 0o700)
	default:
		return &desktopport.SettingsError{Fields: []string{"target"}}
	}
	if rt.cfg.Opener == nil {
		return errors.New("desktop: no opener")
	}
	return rt.cfg.Opener(ctx, path)
}

// panelHome returns the panel home (/admin/) of the origin of u.
func panelHome(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return (&url.URL{Scheme: p.Scheme, Host: p.Host, Path: "/admin/"}).String()
}
