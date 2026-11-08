package desktop

import (
	"net/url"
	"strconv"
	"strings"
)

// Kiosk mode of the front desk (m6b-plan §7.6, D14). The decision logic is pure Go; the platform window code
// (cmd/porto-desktop/kiosk_*.go, webview tag) only applies it.

// KioskDecision is how a launch shows the panel.
type KioskDecision struct {
	// Kiosk is true when the window runs full screen, undecorated, without menus and dev tools.
	Kiosk bool
	// StartPath is the first panel page after the start (the desk of the paired marina in remote mode).
	StartPath string
}

// KioskInput are the facts KioskPolicy decides on.
type KioskInput struct {
	Enabled  bool // desktop.json kiosk.enabled
	Window   bool // the UI is the webview window (not the browser, not headless)
	Remote   bool
	MarinaID string // remote mode: the paired marina
	FirstRun bool   // the first run is pending: never in kiosk (the owner must see the wizard)
}

// KioskPolicy decides the launch: kiosk only in the window (a browser cannot be locked), never on the first run;
// the start page is the desk of the paired marina in remote mode, the panel home otherwise.
func KioskPolicy(in KioskInput) KioskDecision {
	d := KioskDecision{StartPath: "/admin/"}
	if in.Remote && in.MarinaID != "" {
		d.StartPath = "/admin/m/" + url.PathEscape(in.MarinaID)
	}
	d.Kiosk = in.Enabled && in.Window && !in.FirstRun
	return d
}

// KioskExitPath is the page the exit shortcut (Cmd/Ctrl+Shift+K) opens.
const KioskExitPath = "/admin/desktop/kiosk-exit"

// KioskNavigationAllowed reports whether a kiosk window may navigate to target: only the panel origin.
func KioskNavigationAllowed(origin, target string) bool {
	o, err := url.Parse(origin)
	if err != nil || o.Host == "" {
		return false
	}
	t, err := url.Parse(target)
	if err != nil {
		return false
	}
	if !t.IsAbs() {
		return !strings.HasPrefix(target, "//")
	}
	return strings.EqualFold(t.Scheme, o.Scheme) && strings.EqualFold(t.Host, o.Host)
}

// KioskShortcutExit reports whether a key event is the kiosk exit shortcut: Cmd+Shift+K on macOS,
// Ctrl+Shift+K elsewhere.
func KioskShortcutExit(goos, key string, meta, ctrl, shift bool) bool {
	if !shift || !strings.EqualFold(key, "k") {
		return false
	}
	if goos == "darwin" {
		return meta
	}
	return ctrl
}

// KioskScript is the init script of a kiosk window: no context menu, no window.open, links to other origins and
// target=_blank links are blocked, the close and reload shortcuts are swallowed and Cmd/Ctrl+Shift+K opens the
// exit page. It runs before the panel scripts on every page.
func KioskScript(origin, goos string) string {
	mac := strconv.FormatBool(goos == "darwin")
	return `(function(){var O=` + strconv.Quote(origin) + `,MAC=` + mac + `;` +
		`window.open=function(){return null};` +
		`document.addEventListener('contextmenu',function(e){e.preventDefault()},true);` +
		`document.addEventListener('click',function(e){var a=e.target&&e.target.closest?e.target.closest('a[href]'):null;` +
		`if(!a)return;var u;try{u=new URL(a.href,location.href)}catch(_){e.preventDefault();return}` +
		`if(u.origin!==O||a.target==='_blank'){e.preventDefault()}},true);` +
		`document.addEventListener('keydown',function(e){var m=MAC?e.metaKey:e.ctrlKey,k=(e.key||'').toLowerCase();` +
		`if(m&&e.shiftKey&&k==='k'){e.preventDefault();location.assign(O+` + strconv.Quote(KioskExitPath) + `);return}` +
		`if(m&&(k==='w'||k==='q'||k==='r'||k==='n'||k==='t')){e.preventDefault()}` +
		`if(e.key==='F5'||e.key==='F11'||e.key==='F12'||(e.altKey&&e.key==='F4')){e.preventDefault()}},true);})();`
}
