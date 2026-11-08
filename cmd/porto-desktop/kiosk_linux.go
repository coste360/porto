//go:build webview && linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>
static void portoKiosk(void *window, int on) {
	GtkWindow *w = GTK_WINDOW(window);
	if (w == NULL) {
		return;
	}
	if (on) {
		gtk_window_set_decorated(w, FALSE);
		gtk_window_set_resizable(w, FALSE);
		gtk_window_set_keep_above(w, TRUE);
		gtk_window_set_deletable(w, FALSE);
		gtk_window_fullscreen(w);
	} else {
		gtk_window_unfullscreen(w);
		gtk_window_set_keep_above(w, FALSE);
		gtk_window_set_decorated(w, TRUE);
		gtk_window_set_resizable(w, TRUE);
		gtk_window_set_deletable(w, TRUE);
	}
}
*/
import "C"

import webview "github.com/webview/webview_go"

// applyKiosk switches the GTK window in or out of kiosk mode (m6b-plan §7.6): full screen, undecorated, kept
// above, not resizable, no close button.
func applyKiosk(w webview.WebView, on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.portoKiosk(w.Window(), v)
}
