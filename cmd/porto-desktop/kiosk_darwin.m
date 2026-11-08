//go:build webview && darwin

// Kiosk mode of the macOS window (m6b-plan §7.6).
#import <Cocoa/Cocoa.h>

static NSWindowStyleMask portoSavedStyle;
static NSRect portoSavedFrame;
static BOOL portoInKiosk = NO;

void portoKiosk(void *window, int on) {
	NSWindow *w = (__bridge NSWindow *)window;
	if (w == nil) {
		return;
	}
	if (on && !portoInKiosk) {
		portoSavedStyle = [w styleMask];
		portoSavedFrame = [w frame];
		[NSApp setPresentationOptions:NSApplicationPresentationHideDock | NSApplicationPresentationHideMenuBar |
			NSApplicationPresentationDisableProcessSwitching | NSApplicationPresentationDisableForceQuit |
			NSApplicationPresentationDisableSessionTermination | NSApplicationPresentationDisableHideApplication];
		[w setStyleMask:NSWindowStyleMaskBorderless];
		[w setFrame:[[w screen] ?: [NSScreen mainScreen] frame] display:YES];
		[w setLevel:NSMainMenuWindowLevel + 1];
		[w setMovable:NO];
		portoInKiosk = YES;
	} else if (!on && portoInKiosk) {
		[NSApp setPresentationOptions:NSApplicationPresentationDefault];
		[w setLevel:NSNormalWindowLevel];
		[w setStyleMask:portoSavedStyle];
		[w setFrame:portoSavedFrame display:YES];
		[w setMovable:YES];
		portoInKiosk = NO;
	}
	[w makeKeyAndOrderFront:nil];
}
