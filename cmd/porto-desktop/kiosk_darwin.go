//go:build webview && darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
void portoKiosk(void *window, int on);
*/
import "C"

import webview "github.com/webview/webview_go"

// applyKiosk switches the macOS window in or out of kiosk mode (m6b-plan §7.6): presentation options hide the
// Dock and the menu bar and disable process switching and force quit; the window is borderless, covers the
// screen and cannot be resized or closed. It runs on the main thread (callers dispatch).
func applyKiosk(w webview.WebView, on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.portoKiosk(w.Window(), v)
}
