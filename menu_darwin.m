// The macOS menu bar. Go builds it with the calls below, from any
// goroutine; each one runs on the main thread in call order. A chosen
// item calls lensmMenuAction with its tag.

#import <AppKit/AppKit.h>

#include "_cgo_export.h"

@interface LensmMenuTarget : NSObject
@end

@implementation LensmMenuTarget
- (void)choose:(NSMenuItem *)item {
	lensmMenuAction((int)item.tag);
}
@end

static LensmMenuTarget *target;
static NSMenu *menuBar;
static NSMenu *current;
static NSMutableDictionary<NSNumber *, NSMenuItem *> *items;

static void onMain(dispatch_block_t block) {
	dispatch_async(dispatch_get_main_queue(), block);
}

void lensmMenuAdd(const char *title) {
	NSString *t = [NSString stringWithUTF8String:title];
	onMain(^{
		if (menuBar == nil) {
			target = [LensmMenuTarget new];
			menuBar = [NSMenu new];
			items = [NSMutableDictionary new];
		}
		NSMenuItem *top = [NSMenuItem new];
		current = [[NSMenu alloc] initWithTitle:t];
		current.autoenablesItems = NO;
		top.submenu = current;
		[menuBar addItem:top];
		if ([t isEqualToString:@"Window"]) {
			NSApp.windowsMenu = current;
		}
	});
}

// lensmMenuItem adds an item that reports tag, or with a selector
// name sends that action to the first responder instead. key is the
// shortcut with Command, plus Option when alt is set.
void lensmMenuItem(const char *label, const char *key, int alt, int tag, const char *selector) {
	NSString *l = [NSString stringWithUTF8String:label];
	NSString *k = [NSString stringWithUTF8String:key];
	NSString *s = [NSString stringWithUTF8String:selector];
	onMain(^{
		NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:l action:nil keyEquivalent:k];
		if (alt) {
			item.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagOption;
		}
		if (s.length > 0) {
			item.action = NSSelectorFromString(s);
		} else {
			item.action = @selector(choose:);
			item.target = target;
			item.tag = tag;
			items[@(tag)] = item;
		}
		[current addItem:item];
	});
}

void lensmMenuSeparator(void) {
	onMain(^{
		[current addItem:[NSMenuItem separatorItem]];
	});
}

void lensmMenuInstall(void) {
	onMain(^{
		NSApp.mainMenu = menuBar;
	});
}

void lensmMenuSetState(int tag, int checked, int enabled) {
	onMain(^{
		NSMenuItem *item = items[@(tag)];
		item.state = checked ? NSControlStateValueOn : NSControlStateValueOff;
		item.enabled = enabled ? YES : NO;
	});
}
