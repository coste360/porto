// Package desktop is the launcher of the desktop edition (m6a-plan §2.2-§2.9): data folder per OS, desktop.json
// settings and env overrides, the secret key in the OS keychain (file fallback), single instance, the first run,
// daily and exit backups with retention and restore, the optional version check and the rotated log. Runtime
// implements desktopport.Runtime over internal/app; every decision is pure Go testable without a window (the
// window itself lives in cmd/porto-desktop behind the webview build tag).
//
// It never imports internal/store: the composition root (cmd/porto-desktop) hands in a UserCounter.
package desktop
