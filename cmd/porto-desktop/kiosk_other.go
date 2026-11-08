//go:build webview && !darwin && !windows && !linux

package main

import webview "github.com/webview/webview_go"

// applyKiosk is not available on this platform: the window stays a normal window.
func applyKiosk(webview.WebView, bool) {}
