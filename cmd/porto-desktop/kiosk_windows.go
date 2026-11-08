//go:build webview && windows

package main

import (
	"syscall"
	"unsafe"

	webview "github.com/webview/webview_go"
)

// Kiosk mode of the Windows window (m6b-plan §7.6): a topmost popup window (no caption, no borders) sized to the
// monitor it is on; restored when kiosk mode is left.

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procMonitorFromWnd   = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
)

const (
	gwlStyle           = ^uintptr(15) // -16
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000
	swpFrameChanged    = 0x0020
	swpShowWindow      = 0x0040
	monitorDefaultNear = 2
	hwndTopmost        = ^uintptr(0) // -1
	hwndNoTopmost      = ^uintptr(1) // -2
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

var (
	savedStyle uintptr
	savedRect  rect
	inKiosk    bool
)

func applyKiosk(w webview.WebView, on bool) {
	hwnd := uintptr(w.Window())
	if hwnd == 0 {
		return
	}
	if on && !inKiosk {
		savedStyle, _, _ = procGetWindowLongPtr.Call(hwnd, gwlStyle)
		_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&savedRect)))
		mon, _, _ := procMonitorFromWnd.Call(hwnd, monitorDefaultNear)
		mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
		_, _, _ = procGetMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
		_, _, _ = procSetWindowLongPtr.Call(hwnd, gwlStyle, wsPopup|wsVisible)
		r := mi.Monitor
		_, _, _ = procSetWindowPos.Call(hwnd, hwndTopmost, uintptr(r.Left), uintptr(r.Top),
			uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpFrameChanged|swpShowWindow)
		inKiosk = true
	} else if !on && inKiosk {
		_, _, _ = procSetWindowLongPtr.Call(hwnd, gwlStyle, savedStyle)
		r := savedRect
		_, _, _ = procSetWindowPos.Call(hwnd, hwndNoTopmost, uintptr(r.Left), uintptr(r.Top),
			uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), swpFrameChanged|swpShowWindow)
		inKiosk = false
	}
}
