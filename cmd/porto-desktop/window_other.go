//go:build webview && !darwin

package main

import webview "github.com/webview/webview_go"

// WebView2 and WebKitGTK implement window.print() and keep the system edit shortcuts: no native menus needed.
func setupNativeMenus(webview.WebView, *windowUI) {}

func printWindow(w webview.WebView) { w.Eval("window.print()") }

func focusWindow(w webview.WebView) { w.Eval("window.focus()") }
