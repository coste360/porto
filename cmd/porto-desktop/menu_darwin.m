//go:build webview && darwin

// Native macOS menus, printing and activation for the Porto window (m6a-plan §2.13).
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

extern void portoMenuOpenInBrowser(void);

static WKWebView *portoFindWebView(NSView *view) {
	if ([view isKindOfClass:[WKWebView class]]) {
		return (WKWebView *)view;
	}
	for (NSView *sub in [view subviews]) {
		WKWebView *found = portoFindWebView(sub);
		if (found != nil) {
			return found;
		}
	}
	return nil;
}

@interface PortoMenuTarget : NSObject
@property(nonatomic, weak) NSWindow *window;
@end

@implementation PortoMenuTarget
- (void)reloadPage:(id)sender {
	WKWebView *wv = portoFindWebView([self.window contentView]);
	[wv reload];
}
- (void)openInBrowser:(id)sender {
	portoMenuOpenInBrowser();
}
- (void)printPage:(id)sender {
	WKWebView *wv = portoFindWebView([self.window contentView]);
	if (wv == nil) {
		return;
	}
	NSPrintOperation *op = [wv printOperationWithPrintInfo:[NSPrintInfo sharedPrintInfo]];
	op.view.frame = wv.bounds;
	[op runOperationModalForWindow:self.window delegate:nil didRunSelector:nil contextInfo:nil];
}
@end

static PortoMenuTarget *portoTarget;

static NSMenuItem *portoItem(NSString *title, SEL action, NSString *key, id target) {
	NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title action:action keyEquivalent:key];
	if (target != nil) {
		item.target = target;
	}
	return item;
}

static NSString *S(const char *s) { return [NSString stringWithUTF8String:s]; }

void portoSetupMenus(void *window, const char *about, const char *quit, const char *edit, const char *undo,
	const char *redo, const char *cut, const char *copy, const char *paste, const char *selectAll,
	const char *view, const char *reload, const char *openBrowser, const char *print, const char *windowMenu,
	const char *minimize, const char *zoom) {
	portoTarget = [PortoMenuTarget new];
	portoTarget.window = (__bridge NSWindow *)window;

	NSMenu *bar = [NSMenu new];

	NSMenuItem *appItem = [NSMenuItem new];
	NSMenu *appMenu = [[NSMenu alloc] initWithTitle:@"Porto"];
	[appMenu addItem:portoItem(S(about), @selector(orderFrontStandardAboutPanel:), @"", nil)];
	[appMenu addItem:[NSMenuItem separatorItem]];
	[appMenu addItem:portoItem(S(quit), @selector(terminate:), @"q", nil)];
	appItem.submenu = appMenu;
	[bar addItem:appItem];

	NSMenuItem *editItem = [NSMenuItem new];
	NSMenu *editMenu = [[NSMenu alloc] initWithTitle:S(edit)];
	[editMenu addItem:portoItem(S(undo), @selector(undo:), @"z", nil)];
	NSMenuItem *redoItem = portoItem(S(redo), @selector(redo:), @"z", nil);
	redoItem.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
	[editMenu addItem:redoItem];
	[editMenu addItem:[NSMenuItem separatorItem]];
	[editMenu addItem:portoItem(S(cut), @selector(cut:), @"x", nil)];
	[editMenu addItem:portoItem(S(copy), @selector(copy:), @"c", nil)];
	[editMenu addItem:portoItem(S(paste), @selector(paste:), @"v", nil)];
	[editMenu addItem:portoItem(S(selectAll), @selector(selectAll:), @"a", nil)];
	editItem.submenu = editMenu;
	[bar addItem:editItem];

	NSMenuItem *viewItem = [NSMenuItem new];
	NSMenu *viewMenu = [[NSMenu alloc] initWithTitle:S(view)];
	[viewMenu addItem:portoItem(S(reload), @selector(reloadPage:), @"r", portoTarget)];
	[viewMenu addItem:portoItem(S(openBrowser), @selector(openInBrowser:), @"", portoTarget)];
	[viewMenu addItem:[NSMenuItem separatorItem]];
	[viewMenu addItem:portoItem(S(print), @selector(printPage:), @"p", portoTarget)];
	viewItem.submenu = viewMenu;
	[bar addItem:viewItem];

	NSMenuItem *winItem = [NSMenuItem new];
	NSMenu *winMenu = [[NSMenu alloc] initWithTitle:S(windowMenu)];
	[winMenu addItem:portoItem(S(minimize), @selector(performMiniaturize:), @"m", nil)];
	[winMenu addItem:portoItem(S(zoom), @selector(performZoom:), @"", nil)];
	winItem.submenu = winMenu;
	[bar addItem:winItem];
	[NSApp setWindowsMenu:winMenu];

	[NSApp setMainMenu:bar];
}

void portoPrint(void *window) {
	(void)window;
	[portoTarget printPage:nil];
}

void portoFocus(void *window) {
	NSWindow *w = (__bridge NSWindow *)window;
	[NSApp activateIgnoringOtherApps:YES];
	[w makeKeyAndOrderFront:nil];
}
