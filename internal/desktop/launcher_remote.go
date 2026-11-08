package desktop

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/coste360/porto/internal/desktop/remote"
	"github.com/coste360/porto/internal/desktopport"
)

// serveRemote runs the remote mode (m6b-plan §7.4) on the loopback listener until ctx is done (nil) or the app is
// unpaired or revoked (errSwitchMode: the loop restarts in local mode on the first run).
func (l *Launcher) serveRemote(ctx context.Context, ln net.Listener, s FileSettings, ready chan<- string, first *bool) error {
	secretKind := desktopport.SecretStoreKeyring
	if l.env.Keyring == KeyringFile {
		secretKind = desktopport.SecretStoreFile
	}
	rr := NewRemoteRuntime(RemoteRuntimeConfig{
		Version: l.o.Version, Paths: l.paths, Env: l.env, Clock: l.o.Clock, Log: l.log, Settings: s,
		SecretKind: secretKind, GOOS: l.o.GOOS, Argon2: l.o.Argon2, Tokens: l.o.DeviceTokens,
	})
	rr.SetInstanceToken(l.instHash)
	origin, err := remote.ParseOrigin(s.Remote.URL, true)
	if err != nil || rr.Token() == "" {
		// Settings without a token (keychain wiped, token file deleted): back to the first run.
		l.log.WarnContext(ctx, "desktop: remote mode without a device token, back to the first run")
		_ = ln.Close()
		if err := rr.Reset(ctx, false); err != nil {
			return err
		}
		return errSwitchMode
	}

	reset := make(chan bool, 1)
	local := remote.NewLocalAPI(rr, s.Remote.MarinaID, l.env.Dev)
	proxy := remote.New(remote.Options{
		Origin: origin, Pins: remote.Pins{LeafFP: s.Remote.LeafFP, CAFP: s.Remote.CAFP}, Token: rr.Token,
		Port: l.port(), CacheDir: l.paths.RemoteCache(), Dev: l.env.Dev, Local: local, Log: l.log,
		Transport: l.o.RemoteTransport,
		OnRevoked: func() {
			if err := rr.Reset(context.WithoutCancel(ctx), true); err != nil {
				l.log.WarnContext(ctx, "desktop: reset after revocation", "error", err)
			}
		},
	})
	local.Proxy = proxy
	rr.Attach(proxy, l.ui, func(revoked bool) {
		select {
		case reset <- revoked:
		default:
		}
	})

	srv := &http.Server{Handler: proxy, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	bgCtx, stopBg := context.WithCancel(ctx)
	defer stopBg()
	agent := &remote.FiscalAgent{
		Upstream: proxy.Upstream(), Reachable: proxy.Reachable, Log: l.log, Journal: l.paths.FiscalJournal(),
		Driver: func() remote.FiscalDriver { return rr.Peripherals().Fiscal },
	}
	go agent.Run(bgCtx)
	go rr.CheckVersion(bgCtx)

	d := KioskPolicy(KioskInput{Enabled: s.Kiosk.Enabled, Window: l.window, Remote: true, MarinaID: s.Remote.MarinaID})
	l.applyKiosk(d, "http://"+l.host, rr.SetKioskActive)
	enter, err := proxy.EnterURL(d.StartPath)
	if err != nil {
		_ = srv.Close()
		return err
	}
	if l.env.FirstRunFile {
		_ = writeFileAtomic(l.paths.RemoteEnterURL(), []byte(enter+"\n"), 0o600)
	}
	l.log.InfoContext(ctx, "desktop: remote mode started", "marina_id", s.Remote.MarinaID, "pinned", s.Remote.LeafFP != "")
	l.show(enter, ready, first)

	var result error
	select {
	case <-ctx.Done():
	case revoked := <-reset:
		l.revoked = revoked
		result = errSwitchMode
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	stopBg()
	_ = os.Remove(l.paths.RemoteEnterURL())
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
	}
	return result
}
