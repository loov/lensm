// The window's toolbar doubles as its title bar on macOS: Gio leaves
// the content under a transparent title bar but hides the window
// buttons, so this shows them again, centred in the toolbar.

#import <AppKit/AppKit.h>

#include "_cgo_export.h"

static NSWindow *window;
static CGFloat barHeight;
static CGFloat buttonsLeft;

static void placeButtons(void) {
	if (window == nil) {
		return;
	}
	NSButton *buttons[] = {
		[window standardWindowButton:NSWindowCloseButton],
		[window standardWindowButton:NSWindowMiniaturizeButton],
		[window standardWindowButton:NSWindowZoomButton],
	};
	for (int i = 0; i < 3; i++) {
		buttons[i].hidden = NO;
	}
	// In full screen the title bar slides in over the content on its
	// own.
	if (window.styleMask & NSWindowStyleMaskFullScreen) {
		return;
	}
	// The buttons sit in a title bar view that is shorter than the
	// toolbar; make it the toolbar's height and centre them in it.
	NSView *container = buttons[0].superview.superview;
	if (container == nil) {
		return;
	}
	NSRect r = container.frame;
	r.size.height = barHeight;
	r.origin.y = window.frame.size.height - barHeight;
	container.frame = r;
	CGFloat step = NSMinX(buttons[1].frame) - NSMinX(buttons[0].frame);
	for (int i = 0; i < 3; i++) {
		NSRect f = buttons[i].frame;
		f.origin.x = buttonsLeft + i * step;
		f.origin.y = round((barHeight - NSHeight(f)) / 2);
		[buttons[i] setFrameOrigin:f.origin];
	}
}

// lensmTitleBar takes over the title bar of the window that shows view.
// Call it again after the window changes; it runs on the main thread.
void lensmTitleBar(uintptr_t view, double height, double left) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSWindow *w = ((__bridge NSView *)(void *)view).window;
		barHeight = height;
		buttonsLeft = left;
		if (w != nil && w != window) {
			window = w;
			NSNotificationCenter *center = [NSNotificationCenter defaultCenter];
			NSArray *names = @[
				NSWindowDidResizeNotification,
				NSWindowDidEndLiveResizeNotification,
				NSWindowDidExitFullScreenNotification,
				NSWindowDidBecomeKeyNotification,
			];
			for (NSString *name in names) {
				[center addObserverForName:name object:w queue:nil usingBlock:^(NSNotification *n) {
					placeButtons();
					// AppKit lays out the title bar again after some of
					// these, so place them once more after it.
					dispatch_async(dispatch_get_main_queue(), ^{ placeButtons(); });
				}];
			}
			// A double-click on empty toolbar space does what the
			// user set for title bars: zoom, minimize or nothing. Gio
			// starts a window drag on the press and never sees it.
			[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown handler:^NSEvent *(NSEvent *e) {
				if (e.window != window || e.clickCount != 2 || !lensmOverTitleDrag()) {
					return e;
				}
				NSString *action = [[NSUserDefaults standardUserDefaults] stringForKey:@"AppleActionOnDoubleClick"];
				if ([action isEqualToString:@"Minimize"]) {
					[window performMiniaturize:nil];
				} else if (![action isEqualToString:@"None"]) {
					[window performZoom:nil];
				}
				return nil;
			}];
		}
		placeButtons();
	});
}
