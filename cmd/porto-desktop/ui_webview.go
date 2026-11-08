//go:build webview

package main

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"sync"

	webview "github.com/webview/webview_go"

	"github.com/coste360/porto/internal/desktop"
)

func init() {
	uiKind = "window"
	newWindow = func(log *slog.Logger) desktop.UI { return newWindowUI(log) }
}

// Window geometry (m6a-plan §2.13).
const (
	windowWidth     = 1280
	windowHeight    = 800
	windowMinWidth  = 1024
	windowMinHeight = 680
)

// windowUI shows the panel in the system webview (WKWebView, WebView2, WebKitGTK 4.1). When the webview cannot be
// created (Windows without the WebView2 runtime, Linux without WebKitGTK) it falls back to the default browser.
// Closing the window quits the app (M6a, D17).
type windowUI struct {
	log  *slog.Logger
	open desktop.Opener
	dev  bool
	home string

	mu       sync.Mutex
	w        webview.WebView
	url      string
	fallback *desktop.BrowserUI
	// opts is the kiosk decision of the launcher (M6b, m6b-plan §7.6), set before Run.
	opts desktop.WindowOptions
}

var _ desktop.KioskUI = (*windowUI)(nil)

// SetKiosk implements desktop.KioskUI: before Run it sets the launch state; afterwards SetKiosk(false) leaves kiosk
// mode until the next launch (the init script stays until the next page load).
func (u *windowUI) SetKiosk(on bool, origin string) {
	u.mu.Lock()
	was := u.opts.Kiosk
	u.opts.Kiosk = on
	if origin != "" {
		u.opts.Origin = origin
	}
	w := u.w
	u.mu.Unlock()
	if w != nil && was != on {
		w.Dispatch(func() { applyKiosk(w, on) })
	}
}

func (u *windowUI) kiosk() (bool, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.opts.Kiosk, u.opts.Origin
}

func newWindowUI(log *slog.Logger) *windowUI {
	home, _ := os.UserHomeDir()
	dev := os.Getenv("PORTO_DEV") == "true"
	return &windowUI{log: log, open: desktop.SystemOpener(runtime.GOOS), dev: dev, home: home}
}

// Run implements desktop.UI. It must run on the main thread (webview_go locks it in its init).
func (u *windowUI) Run(ctx context.Context, url string) error {
	kiosk, origin := u.kiosk()
	w := webview.New(u.dev && !kiosk) // no developer tools in kiosk mode
	if w == nil || w.Window() == nil {
		u.log.WarnContext(ctx, "desktop: system webview unavailable, opening the browser")
		fb := desktop.NewBrowserUI(u.open, u.log)
		u.mu.Lock()
		u.fallback = fb
		u.mu.Unlock()
		return fb.Run(ctx, url)
	}
	defer w.Destroy()
	w.SetTitle("Porto")
	w.SetSize(windowMinWidth, windowMinHeight, webview.HintMin)
	w.SetSize(windowWidth, windowHeight, webview.HintNone)
	w.Init(desktop.BridgeScript(version))
	if kiosk {
		w.Init(desktop.KioskScript(origin, runtime.GOOS))
		applyKiosk(w, true)
	}
	u.bind(ctx, w)
	setupNativeMenus(w, u)
	u.mu.Lock()
	u.w, u.url = w, url
	u.mu.Unlock()
	w.Navigate(url)

	stop := context.AfterFunc(ctx, func() { w.Dispatch(w.Terminate) })
	defer stop()
	w.Run()
	u.mu.Lock()
	u.w = nil
	u.mu.Unlock()
	return nil
}

func (u *windowUI) bind(ctx context.Context, w webview.WebView) {
	must := func(err error) {
		if err != nil {
			u.log.WarnContext(ctx, "desktop: bridge binding", "error", err)
		}
	}
	must(w.Bind(desktop.BindSaveFile, func(name, b64 string) (string, error) {
		dir, err := desktop.DownloadsDir(u.home)
		if err != nil {
			return "", err
		}
		path, err := desktop.SaveDownload(dir, name, b64)
		if err != nil {
			return "", err
		}
		go func() { _ = u.open(context.Background(), dir) }()
		return path, nil
	}))
	must(w.Bind(desktop.BindOpenExternal, func(target string) error {
		if on, _ := u.kiosk(); on || !desktop.ExternalURLAllowed(target) { // kiosk: never leave the panel
			return desktop.ErrSaveRefused
		}
		return u.open(context.Background(), target)
	}))
	must(w.Bind(desktop.BindPrint, func() error {
		w.Dispatch(func() { printWindow(w) })
		return nil
	}))
}

// Navigate implements desktop.UI.
func (u *windowUI) Navigate(url string) {
	u.mu.Lock()
	w, fb := u.w, u.fallback
	u.url = url
	u.mu.Unlock()
	switch {
	case fb != nil:
		fb.Navigate(url)
	case w != nil:
		w.Dispatch(func() { w.Navigate(url) })
	}
}

// Focus implements desktop.UI.
func (u *windowUI) Focus() {
	u.mu.Lock()
	w, fb := u.w, u.fallback
	u.mu.Unlock()
	switch {
	case fb != nil:
		fb.Focus()
	case w != nil:
		w.Dispatch(func() { focusWindow(w) })
	}
}

// Quit implements desktop.UI.
func (u *windowUI) Quit() {
	u.mu.Lock()
	w, fb := u.w, u.fallback
	u.mu.Unlock()
	switch {
	case fb != nil:
		fb.Quit()
	case w != nil:
		w.Dispatch(w.Terminate)
	}
}

// openInBrowser opens the current panel page in the default browser (menu "Apri nel browser").
func (u *windowUI) openInBrowser() {
	u.mu.Lock()
	url := u.url
	u.mu.Unlock()
	if url != "" {
		_ = u.open(context.Background(), url)
	}
}
