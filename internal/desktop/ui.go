package desktop

import (
	"context"
	"log/slog"
	"sync"
)

// UI shows the panel (m6a-plan §2.13). The launcher calls Run on the main goroutine (the webview owns the main OS
// thread); the other methods are safe from any goroutine. The window lives in cmd/porto-desktop (build tag
// webview); this package has the browser and headless UIs, so every decision is testable without a window.
type UI interface {
	// Run shows url and blocks until the user closes the window, Quit is called or ctx is done.
	Run(ctx context.Context, url string) error
	// Navigate shows another page (after a restore the window reloads the panel).
	Navigate(url string)
	// Focus brings the window to the front (second launch).
	Focus()
	// Quit makes Run return.
	Quit()
}

// WindowOptions are the window choices (M6b: kiosk for the front desk, m6b-plan §7.6).
type WindowOptions struct {
	Kiosk bool
	// Origin is the panel origin: in kiosk mode the window never navigates elsewhere.
	Origin string
}

// KioskUI is implemented by a UI that can run in kiosk mode (the webview window). SetKiosk before Run sets the
// launch state; SetKiosk(false) afterwards leaves kiosk mode until the next launch.
type KioskUI interface {
	SetKiosk(on bool, origin string)
}

// HeadlessUI shows nothing: Run waits until Quit or ctx is done (tests, e2e, -headless).
type HeadlessUI struct {
	once sync.Once
	done chan struct{}
}

// NewHeadlessUI returns a HeadlessUI.
func NewHeadlessUI() *HeadlessUI { return &HeadlessUI{done: make(chan struct{})} }

// Run implements UI.
func (h *HeadlessUI) Run(ctx context.Context, _ string) error {
	select {
	case <-ctx.Done():
	case <-h.done:
	}
	return nil
}

// Navigate implements UI.
func (*HeadlessUI) Navigate(string) {}

// Focus implements UI.
func (*HeadlessUI) Focus() {}

// Quit implements UI.
func (h *HeadlessUI) Quit() { h.once.Do(func() { close(h.done) }) }

// BrowserUI opens the panel in the default browser and then runs like HeadlessUI until SIGINT/SIGTERM. It is the
// default build (no CGO) and the fallback when the system webview is missing.
type BrowserUI struct {
	*HeadlessUI
	Open Opener
	Log  *slog.Logger

	mu  sync.Mutex
	url string
}

// NewBrowserUI returns a BrowserUI that opens pages with open.
func NewBrowserUI(open Opener, log *slog.Logger) *BrowserUI {
	return &BrowserUI{HeadlessUI: NewHeadlessUI(), Open: open, Log: log}
}

// Run implements UI.
func (b *BrowserUI) Run(ctx context.Context, url string) error {
	b.show(ctx, url)
	return b.HeadlessUI.Run(ctx, url)
}

func (b *BrowserUI) show(ctx context.Context, url string) {
	b.mu.Lock()
	b.url = url
	b.mu.Unlock()
	if err := b.Open(ctx, url); err != nil && b.Log != nil {
		b.Log.WarnContext(ctx, "desktop: cannot open the browser", "error", err)
	}
}

// Navigate implements UI: the new page opens in the browser.
func (b *BrowserUI) Navigate(url string) { b.show(context.Background(), url) }

// Focus implements UI: a second launch opens the panel again (the browser brings its window forward). The
// first-run URL is not reopened: its token is in the fragment.
func (b *BrowserUI) Focus() {
	b.mu.Lock()
	u := b.url
	b.mu.Unlock()
	if u != "" {
		b.show(context.Background(), panelHome(u))
	}
}
