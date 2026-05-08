// Cocoa-side implementation of the FrpDeck NSStatusItem tray menu.
//
// All AppKit calls are dispatched onto the main queue because Wails
// already owns the main thread (NSApplicationMain). Constructing the
// NSStatusItem from a goroutine via dispatch_async keeps the API
// thread-safe without us having to write our own runloop bookkeeping.
//
// The Go side talks to us through three C functions (tray_darwin_init,
// tray_darwin_set_status, tray_darwin_quit) and receives one
// callback (goTrayDarwinAction) which fires whenever the user clicks
// a menu item. The integer action IDs mirror constants declared in
// wails_tray_darwin.go — keep both files in sync when adding entries.

#import <Cocoa/Cocoa.h>

// Forward declaration of the cgo-exported Go entry point. cgo emits
// the symbol with C linkage, so a plain extern works without
// including _cgo_export.h (which Go does not regenerate for .m
// translation units).
extern void goTrayDarwinAction(int actionID);

@interface FrpDeckTrayController : NSObject
- (void)onShow:(id)sender;
- (void)onHide:(id)sender;
- (void)onQuit:(id)sender;
- (void)onPage:(id)sender;
@end

@implementation FrpDeckTrayController
- (void)onShow:(id)sender { goTrayDarwinAction(1); }
- (void)onHide:(id)sender { goTrayDarwinAction(2); }
- (void)onQuit:(id)sender { goTrayDarwinAction(3); }
- (void)onPage:(id)sender {
    NSMenuItem *mi = (NSMenuItem *)sender;
    goTrayDarwinAction((int)mi.tag);
}
@end

// Module-globals are intentional — the tray is a singleton for the
// life of the desktop process. ARC retains these as strong refs.
static NSStatusItem        *gStatusItem  = nil;
static NSMenuItem          *gStatusLine  = nil;
static FrpDeckTrayController *gController = nil;

// Helper that builds the menu skeleton. Called once on init.
static NSMenu *buildMenu(void) {
    NSMenu *menu = [[NSMenu alloc] init];
    [menu setAutoenablesItems:NO];

    NSMenuItem *header = [menu addItemWithTitle:@"FrpDeck" action:nil keyEquivalent:@""];
    [header setEnabled:NO];

    gStatusLine = [menu addItemWithTitle:@"…" action:nil keyEquivalent:@""];
    [gStatusLine setEnabled:NO];

    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *show = [menu addItemWithTitle:@"显示窗口 / Show"
                                       action:@selector(onShow:)
                                keyEquivalent:@""];
    show.target = gController;

    NSMenuItem *hide = [menu addItemWithTitle:@"隐藏窗口 / Hide"
                                       action:@selector(onHide:)
                                keyEquivalent:@""];
    hide.target = gController;

    [menu addItem:[NSMenuItem separatorItem]];

    // "Open page" submenu — same routes the Linux/Windows tray
    // exposes. Tags carry the action ID consumed by the Go side.
    NSMenu *pages = [[NSMenu alloc] init];
    [pages setAutoenablesItems:NO];

    struct PageEntry { NSString *title; int tag; };
    struct PageEntry entries[] = {
        {@"概览 / Dashboard",  10},
        {@"端点 / Endpoints",  11},
        {@"隧道 / Tunnels",    12},
        {@"用户 / Users",      13},
        {@"设置 / Settings",   14},
        {@"审计 / Audit log",  15},
    };
    NSUInteger entryCount = sizeof(entries) / sizeof(entries[0]);
    for (NSUInteger i = 0; i < entryCount; i++) {
        NSMenuItem *p = [pages addItemWithTitle:entries[i].title
                                         action:@selector(onPage:)
                                  keyEquivalent:@""];
        p.target = gController;
        p.tag    = entries[i].tag;
    }

    NSMenuItem *pagesItem = [menu addItemWithTitle:@"打开页面 / Open page"
                                            action:nil
                                     keyEquivalent:@""];
    [menu setSubmenu:pages forItem:pagesItem];

    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quit = [menu addItemWithTitle:@"退出 / Quit"
                                       action:@selector(onQuit:)
                                keyEquivalent:@""];
    quit.target = gController;

    return menu;
}

void tray_darwin_init(const char *title, const char *tooltip) {
    NSString *t  = title   ? [NSString stringWithUTF8String:title]   : @"FrpDeck";
    NSString *tp = tooltip ? [NSString stringWithUTF8String:tooltip] : @"";

    dispatch_async(dispatch_get_main_queue(), ^{
        if (gStatusItem != nil) {
            // Already initialised — refresh the title in case the
            // caller wants to rebrand without bouncing the tray.
            gStatusItem.button.title = t;
            gStatusItem.button.toolTip = tp;
            return;
        }
        gController = [[FrpDeckTrayController alloc] init];

        NSStatusItem *item = [[NSStatusBar systemStatusBar]
            statusItemWithLength:NSVariableStatusItemLength];
        item.button.title   = t;
        item.button.toolTip = tp;
        item.menu           = buildMenu();
        gStatusItem         = item;
    });
}

void tray_darwin_set_status(const char *status) {
    if (status == NULL) return;
    NSString *s = [NSString stringWithUTF8String:status];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gStatusLine != nil) {
            gStatusLine.title = s;
        }
    });
}

void tray_darwin_quit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (gStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:gStatusItem];
            gStatusItem = nil;
        }
        gStatusLine  = nil;
        gController  = nil;
    });
}
