//go:build webview && darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
void portoSetupMenus(void *window, const char *about, const char *quit, const char *edit, const char *undo,
	const char *redo, const char *cut, const char *copy, const char *paste, const char *selectAll,
	const char *view, const char *reload, const char *openBrowser, const char *print, const char *windowMenu,
	const char *minimize, const char *zoom);
void portoPrint(void *window);
void portoFocus(void *window);
*/
import "C"

import (
	"sync"

	webview "github.com/webview/webview_go"
)

var (
	menuMu  sync.Mutex
	menuUI  *windowUI
	menuStr []*C.char
)

// setupNativeMenus installs the macOS menus (m6a-plan §2.13): WKWebView gets ⌘C/⌘V only through an Edit menu.
// Labels are Italian, the source language of Porto.
func setupNativeMenus(w webview.WebView, u *windowUI) {
	menuMu.Lock()
	menuUI = u
	menuMu.Unlock()
	s := func(v string) *C.char {
		c := C.CString(v)
		menuStr = append(menuStr, c) // kept for the life of the process
		return c
	}
	C.portoSetupMenus(w.Window(), s("Informazioni su Porto"), s("Esci da Porto"), s("Modifica"), s("Annulla"),
		s("Ripeti"), s("Taglia"), s("Copia"), s("Incolla"), s("Seleziona tutto"), s("Vista"), s("Ricarica"),
		s("Apri nel browser"), s("Stampa…"), s("Finestra"), s("Riduci"), s("Zoom"))
}

//export portoMenuOpenInBrowser
func portoMenuOpenInBrowser() {
	menuMu.Lock()
	u := menuUI
	menuMu.Unlock()
	if u != nil {
		go u.openInBrowser()
	}
}

func printWindow(w webview.WebView) { C.portoPrint(w.Window()) }

func focusWindow(w webview.WebView) { C.portoFocus(w.Window()) }
