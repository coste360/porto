package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/coste360/porto/internal/app"
	"github.com/coste360/porto/internal/clock"
	"github.com/coste360/porto/internal/config"
	"github.com/coste360/porto/internal/desktopport"
	"github.com/coste360/porto/internal/domain/auth"
	"github.com/coste360/porto/internal/domain/event"
	"github.com/coste360/porto/internal/reqctx"
	"github.com/coste360/porto/pkg/ext"
)

// Options configure the launcher. Zero values are the production defaults where noted.
type Options struct {
	Version string
	// DataDir is the -data-dir flag ("" = DataDir(GOOS, …)).
	DataDir  string
	Headless bool
	Browser  bool
	GOOS     string
	GOARCH   string
	Home     string
	// LookupEnv reads the process environment (os.LookupEnv).
	LookupEnv func(string) (string, bool)
	// Stderr receives the text log in headless mode.
	Stderr io.Writer

	// NewWindow returns the window UI (webview build); nil, or Browser, uses the default browser.
	NewWindow func(log *slog.Logger) UI
	// UI overrides the UI choice (tests).
	UI UI
	// CountUsers is required: see UserCounter.
	CountUsers UserCounter
	// NewLAN builds the local-network server; nil = the local network cannot be enabled.
	NewLAN LANFactory
	// Keyring overrides the OS keychain store (tests); ignored with PORTO_DESKTOP_KEYRING=file.
	Keyring SecretStore
	// Opener opens folders and the browser (default SystemOpener(GOOS)).
	Opener Opener
	Clock  clock.Clock
	Loc    *time.Location
	// UpdateClient overrides the egress client of the version check (tests).
	UpdateClient *http.Client
	// Argon2 overrides the password hashing cost (tests).
	Argon2 auth.Argon2Params
	// OnReady is called with the panel URL once the server answers (tests).
	OnReady func(url string)
	// OnNavigate is called with every later URL shown after a restart or a mode switch (tests).
	OnNavigate func(url string)
	// DeviceTokens overrides the OS keychain store of the remote-mode device token (tests).
	DeviceTokens TokenStore
	// RemoteTransport overrides the transport to the online instance (tests).
	RemoteTransport http.RoundTripper
}

// Launcher runs the desktop edition (m6a-plan §2.2).
type Launcher struct {
	o      Options
	paths  Paths
	env    Env
	log    *slog.Logger
	ui     UI
	window bool // the UI is the webview window
	cfg    *config.Config
	host   string // 127.0.0.1:<port>

	secret   *ResolvedSecret
	instHash string
	revoked  bool // the remote device was revoked: the next first run says so

	rtMu sync.Mutex
	rt   *Runtime
}

// Run executes the start sequence, shows the UI until it closes and stops in order: local network off, HTTP
// shutdown, jobs stop, exit backup, database close, lock release. A second launch on a running data folder
// activates the running instance and returns nil.
func Run(ctx context.Context, o Options) error {
	if o.CountUsers == nil {
		return errors.New("desktop: Options.CountUsers is required")
	}
	if o.LookupEnv == nil {
		o.LookupEnv = func(string) (string, bool) { return "", false }
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Opener == nil {
		o.Opener = SystemOpener(o.GOOS)
	}
	getenv := func(k string) string { v, _ := o.LookupEnv(k); return v }

	root := o.DataDir
	if root == "" {
		var err error
		if root, err = DataDir(o.GOOS, getenv, o.Home); err != nil {
			return err
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("desktop: create data folder: %w", err)
	}
	l := &Launcher{o: o, paths: Paths{Root: root}}

	logFile, err := OpenLogWriter(l.paths.Logs(), 0, 0)
	if err != nil {
		return fmt.Errorf("desktop: open log: %w", err)
	}
	defer logFile.Close()
	l.log = newLogger(logFile, o.Headless, o.Stderr, getenv("PORTO_LOG_LEVEL"))

	lock, err := AcquireLock(l.paths.Lock())
	if errors.Is(err, ErrAlreadyRunning) {
		actx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if aerr := ActivateRunning(actx, l.paths, nil); aerr != nil {
			return fmt.Errorf("%w (activation failed: %w)", ErrAlreadyRunning, aerr)
		}
		l.log.InfoContext(ctx, "desktop: activated the running instance")
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	return l.run(ctx)
}

func newLogger(w io.Writer, headless bool, stderr io.Writer, level string) *slog.Logger {
	lv := slog.LevelInfo
	_ = lv.UnmarshalText([]byte(level))
	opts := &slog.HandlerOptions{Level: lv}
	h := slog.Handler(slog.NewJSONHandler(w, opts))
	if headless {
		h = slog.NewMultiHandler(h, slog.NewTextHandler(stderr, opts))
	}
	return slog.New(h).With("component", "desktop")
}

func (l *Launcher) run(ctx context.Context) error {
	o := l.o
	env, err := LoadEnv(o.LookupEnv)
	if err != nil {
		return err
	}
	l.env = env
	settings, err := LoadSettings(l.paths.Settings())
	if err != nil {
		return err
	}
	if _, ok := o.LookupEnv("DATABASE_URL"); ok {
		l.log.WarnContext(ctx, "desktop: DATABASE_URL is ignored, the desktop edition uses SQLite")
	}
	l.ui = l.chooseUI()
	if settings.Mode != desktopport.ModeRemote {
		// The remote mode has no database and needs no secret key; the local mode cannot start without it.
		if err := l.prepareSecret(ctx); err != nil {
			if errors.Is(err, ErrSecretMissing) || errors.Is(err, ErrSecretInvalid) {
				l.log.ErrorContext(ctx, "desktop: secret key missing or invalid for the existing database")
				return l.showMissingKey(ctx, err)
			}
			return err
		}
	}

	port := settings.Port
	if env.Port != 0 {
		port = env.Port
	}
	ln, err := listenLoopback(ctx, port, env.Port != 0)
	if err != nil {
		return err
	}
	port = listenerPort(ln)
	if settings.Port != port && env.Port == 0 {
		settings.Port = port
	}
	if err := SaveSettings(l.paths.Settings(), settings); err != nil {
		_ = ln.Close()
		return err
	}
	l.host = "127.0.0.1:" + strconv.Itoa(port)
	hash, err := WriteInstance(l.paths, os.Getpid(), port)
	if err != nil {
		_ = ln.Close()
		return err
	}
	defer RemoveInstance(l.paths)
	l.instHash = hash

	srvCtx, stopSrv := context.WithCancel(context.WithoutCancel(ctx))
	defer stopSrv()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- l.loop(srvCtx, ln, ready) }()

	var url string
	select {
	case url = <-ready:
	case err := <-done:
		return err
	case <-ctx.Done():
		stopSrv()
		return <-done
	}
	if o.OnReady != nil {
		o.OnReady(url)
	}
	uiErr := l.ui.Run(ctx, url)
	l.log.InfoContext(ctx, "desktop: stopping")
	if rt := l.localRuntime(); rt != nil {
		if _, lan := rt.current(); lan != nil {
			_ = lan.Disable(context.WithoutCancel(ctx))
		}
	}
	stopSrv()
	err = <-done
	if err == nil {
		err = uiErr
	}
	return err
}

// errSwitchMode makes loop serve the other mode (paired: remote; unpaired or revoked: local first run).
var errSwitchMode = errors.New("desktop: switch mode")

// loop serves the mode of desktop.json, switching between local and remote mode on the same loopback port.
func (l *Launcher) loop(ctx context.Context, ln net.Listener, ready chan<- string) error {
	first := true
	for {
		s, err := LoadSettings(l.paths.Settings())
		if err != nil {
			_ = ln.Close()
			return err
		}
		if l.env.UpdateCheckOff {
			s.UpdateCheck = false
		}
		if s.Mode == desktopport.ModeRemote {
			err = l.serveRemote(ctx, ln, s, ready, &first)
		} else if err = l.prepareLocal(ctx, s); err == nil {
			err = l.serve(ctx, ln, ready, &first)
		} else {
			_ = ln.Close()
		}
		if !errors.Is(err, errSwitchMode) {
			return err
		}
		l.log.InfoContext(ctx, "desktop: switching mode")
		var lc net.ListenConfig
		if ln, err = lc.Listen(ctx, "tcp", l.host); err != nil {
			return fmt.Errorf("desktop: listen after a mode switch: %w", err)
		}
	}
}

// prepareSecret resolves the secret key of the local mode once.
func (l *Launcher) prepareSecret(ctx context.Context) error {
	if l.secret != nil {
		return nil
	}
	var kr SecretStore
	if l.env.Keyring != KeyringFile {
		kr = l.o.Keyring
		if kr == nil {
			kr = NewKeyringStore(l.paths.Root)
		}
	}
	envKey, _ := l.o.LookupEnv("PORTO_SECRET_KEY")
	secret, err := ResolveSecretKey(envKey, kr, FileStore{Path: l.paths.SecretFile()}, fileExists(l.paths.DB()))
	if err != nil {
		return err
	}
	if secret.Kind == desktopport.SecretStoreFile {
		l.log.WarnContext(ctx, "desktop: the secret key is in a file of the data folder (no OS keychain available)")
	}
	l.secret = &secret
	return nil
}

// prepareLocal builds the configuration and the runtime of the local mode once; later calls (back from the remote
// mode) give the runtime the current settings.
func (l *Launcher) prepareLocal(ctx context.Context, settings FileSettings) error {
	if err := l.prepareSecret(ctx); err != nil {
		return err
	}
	if rt := l.localRuntime(); rt != nil {
		rt.replaceSettings(settings)
		return nil
	}
	o, env := l.o, l.env
	port := l.port()
	cfg, err := config.Load(ServerEnv(ServerEnvInput{
		Port: port, DataDir: l.paths.Root, SecretKey: l.secret.Key, MailDir: l.paths.Mail(), Lookup: o.LookupEnv,
	}))
	if err != nil {
		return err
	}
	l.cfg = cfg
	updURL := DefaultUpdateURL
	policy := event.EgressPolicy{}
	if env.UpdateURL != "" {
		updURL, policy = env.UpdateURL, event.EgressPolicy{AllowPrivate: true, AllowHTTP: true}
	}
	client := o.UpdateClient
	if client == nil {
		client = event.NewEgressClient(policy, updateTimeout)
	}
	upd := &Updater{URL: updURL, Client: client, Version: o.Version, GOOS: o.GOOS, GOARCH: o.GOARCH, Log: l.log}
	rt := NewRuntime(RuntimeConfig{
		Version: o.Version, Paths: l.paths, Env: env, Clock: o.Clock, Loc: o.Loc, Log: l.log, Settings: settings,
		SecretKey: l.secret.Key, SecretKind: l.secret.Kind, Opener: o.Opener, CountUsers: o.CountUsers, Updater: upd,
		GOOS: o.GOOS, Argon2: o.Argon2, Tokens: o.DeviceTokens,
	})
	rt.SetInstanceToken(l.instHash)
	l.rtMu.Lock()
	l.rt = rt
	l.rtMu.Unlock()
	go upd.Run(ctx, rt.Recheck())
	return nil
}

func (l *Launcher) localRuntime() *Runtime {
	l.rtMu.Lock()
	defer l.rtMu.Unlock()
	return l.rt
}

// show sends the first URL on ready and navigates the UI to later ones.
func (l *Launcher) show(url string, ready chan<- string, first *bool) {
	if *first {
		ready <- url
		*first = false
		return
	}
	l.ui.Navigate(url)
	if l.o.OnNavigate != nil {
		l.o.OnNavigate(url)
	}
}

// applyKiosk tells a kiosk-capable window the launch decision and records it in the runtime status.
func (l *Launcher) applyKiosk(d KioskDecision, origin string, record func(bool)) {
	if k, ok := l.ui.(KioskUI); ok {
		k.SetKiosk(d.Kiosk, origin)
	}
	record(d.Kiosk)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (l *Launcher) chooseUI() UI {
	switch {
	case l.o.UI != nil:
		return l.o.UI
	case l.o.Headless:
		return NewHeadlessUI()
	case l.o.Browser || l.o.NewWindow == nil:
		return NewBrowserUI(l.o.Opener, l.log)
	default:
		l.window = true
		return l.o.NewWindow(l.log)
	}
}

// listenLoopback binds 127.0.0.1:port; a remembered port that is taken falls back to a free one, a port forced by
// PORTO_DESKTOP_PORT does not.
func listenLoopback(ctx context.Context, port int, forced bool) (net.Listener, error) {
	var lc net.ListenConfig
	if port != 0 {
		ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil || forced {
			return ln, err
		}
	}
	return lc.Listen(ctx, "tcp", "127.0.0.1:0")
}

// serve runs the App until ctx is done, restarting it after a restore. The first start sends the panel URL on
// ready; later starts reload the UI.
func (l *Launcher) serve(ctx context.Context, ln net.Listener, ready chan<- string, first *bool) error {
	for {
		a, err := app.NewWithOptions(ctx, l.cfg, l.o.Version, ext.Extensions{}, l.log, app.Options{
			Clock: l.o.Clock, Argon2: l.o.Argon2, Desktop: l.rt, PairTarget: l.rt.PairTarget,
		})
		if err != nil {
			_ = ln.Close()
			return err
		}
		if err := a.Prepare(ctx); err != nil {
			_ = a.Close()
			_ = ln.Close()
			return err
		}
		lan := l.newLAN(a)
		origin := "http://" + l.host
		l.rt.Attach(a, lan, l.ui, origin)

		url := origin + "/admin/"
		firstRun := false
		if n, err := l.o.CountUsers(ctx, a); err == nil && n == 0 {
			tok, err := l.rt.MintFirstRunToken()
			if err != nil {
				_ = a.Close()
				_ = ln.Close()
				return err
			}
			firstRun = true
			url = origin + "/admin/desktop/first-run#t=" + tok
			if l.revoked {
				url += "&revoked=1"
				l.revoked = false
			}
			if l.env.FirstRunFile {
				_ = writeFileAtomic(l.paths.FirstRunURL(), []byte(url+"\n"), 0o600)
			}
		}
		if l.rt.currentSettings().LANEnabled {
			if _, err := lan.Enable(ctx, desktopport.LANOptions{}); err != nil {
				l.log.WarnContext(ctx, "desktop: local network not started", "error", err)
			}
		}

		bgCtx, stopBg := context.WithCancel(ctx)
		go l.rt.BackupManager().RunScheduler(bgCtx, l.rt.Snapshot)
		go refreshLAN(bgCtx, lan, l.o.Clock, LANRefreshEvery, lanRefreshPoll, l.log)
		l.applyKiosk(KioskPolicy(KioskInput{
			Enabled: l.rt.currentSettings().Kiosk.Enabled, Window: l.window, FirstRun: firstRun,
		}), origin, l.rt.SetKioskActive)
		l.show(url, ready, first)

		runErr := a.Run(ctx, app.Listener{
			Listener: ln, Tag: reqctx.ListenerLoopback,
			AllowedHosts: []string{l.host, "localhost:" + strconv.Itoa(l.port())},
		})
		stopBg()
		_ = lan.Disable(context.WithoutCancel(ctx))

		if errors.Is(runErr, app.ErrRestart) && l.rt.TakeModeSwitch() {
			// Paired on the first run: the empty local database is closed and the remote mode starts.
			if err := a.Close(); err != nil {
				l.log.WarnContext(ctx, "desktop: close before the remote mode", "error", err)
			}
			return errSwitchMode
		}
		if errors.Is(runErr, app.ErrRestart) {
			wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ExitBackupWait)
			l.rt.BackupManager().WaitIdle(wctx)
			cancel()
			if err := a.Close(); err != nil {
				l.log.WarnContext(ctx, "desktop: close before restore", "error", err)
			}
			if staging := l.rt.TakeRestore(); staging != "" {
				if err := swapRestored(l.paths, staging, l.o.Clock.Now()); err != nil {
					l.log.ErrorContext(ctx, "desktop: restore swap failed", "error", err)
				} else {
					l.log.InfoContext(ctx, "desktop: restored database in place")
				}
			}
			var lc net.ListenConfig
			ln, err = lc.Listen(ctx, "tcp", l.host)
			if err != nil {
				return fmt.Errorf("desktop: listen after restore: %w", err)
			}
			continue
		}

		// Normal stop: HTTP and jobs are down, take the exit backup before closing the database.
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ExitBackupWait)
		if _, err := l.rt.BackupManager().TakeWait(bctx, desktopport.BackupExit, l.rt.Snapshot); err != nil {
			l.log.WarnContext(bctx, "desktop: exit backup failed", "error", err)
		}
		cancel()
		if err := a.Close(); err != nil && runErr == nil {
			runErr = err
		}
		return runErr
	}
}

// LANRefreshEvery is how often a running local-network listener re-checks its addresses (DHCP, another Wi-Fi) and
// the leaf certificate expiry (m6a-plan §2.10).
const LANRefreshEvery = 15 * time.Minute

// lanRefreshPoll is how often refreshLAN looks at the clock; the decision to refresh uses the injected clock.
const lanRefreshPoll = time.Minute

// lanRefresher is implemented by internal/lan.Server; the unavailable controller and test fakes do not need it.
type lanRefresher interface {
	Refresh(ctx context.Context) (desktopport.LANStatus, error)
}

// refreshLAN calls Refresh on lan once every interval of clk until ctx is done, checking the clock every poll; a
// stopped listener is left alone by Refresh.
func refreshLAN(ctx context.Context, lan LAN, clk clock.Clock, every, poll time.Duration, log *slog.Logger) {
	r, ok := lan.(lanRefresher)
	if !ok {
		return
	}
	last := clk.Now()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if now := clk.Now(); now.Sub(last) >= every {
				last = now
				if _, err := r.Refresh(ctx); err != nil {
					log.WarnContext(ctx, "desktop: local network refresh", "error", err)
				}
			}
		}
	}
}

func listenerPort(ln net.Listener) int {
	if a, ok := ln.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

func (l *Launcher) port() int {
	_, p, _ := net.SplitHostPort(l.host)
	n, _ := strconv.Atoi(p)
	return n
}

func (l *Launcher) newLAN(a *app.App) LAN {
	s := l.rt.currentSettings()
	port := s.LANPort
	if l.env.LANPort != 0 {
		port = l.env.LANPort
	}
	if l.o.NewLAN != nil {
		lan, err := l.o.NewLAN(LANDeps{
			App: a, Dir: l.paths.LAN(), Keys: a.Config.Keys, Clock: l.o.Clock, Log: l.log,
			Port: port, Address: s.LANAddress, Loopback: l.env.LANLoopback, Origin: "http://" + l.host,
		})
		if err == nil && lan != nil {
			return lan
		}
		l.log.WarnContext(context.Background(), "desktop: local network unavailable", "error", err)
	}
	return &unavailableLAN{port: port, origin: "http://" + l.host}
}

// swapRestored moves the current database and file store aside (<name>.before-<ts>) and the restored ones in
// place. It runs with the App closed.
func swapRestored(p Paths, staging string, now time.Time) error {
	stamp := now.UTC().Format("20060102-150405")
	moves := [][2]string{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if fileExists(p.DB() + suffix) {
			moves = append(moves, [2]string{p.DB() + suffix, p.DB() + ".before-" + stamp + suffix})
		}
	}
	if fileExists(p.Files()) {
		moves = append(moves, [2]string{p.Files(), p.Files() + ".before-" + stamp})
	}
	for _, m := range moves {
		if err := os.Rename(m[0], m[1]); err != nil {
			return err
		}
	}
	src := Paths{Root: staging}
	if err := os.Rename(src.DB(), p.DB()); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if fileExists(src.DB() + suffix) {
			_ = os.Rename(src.DB()+suffix, p.DB()+suffix)
		}
	}
	if fileExists(src.Files()) {
		if err := os.Rename(src.Files(), p.Files()); err != nil {
			return err
		}
	} else if err := os.MkdirAll(p.Files(), 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return os.RemoveAll(staging)
}
