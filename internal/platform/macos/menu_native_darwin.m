//go:build darwin && cgo
// ^ Advisory for IDE only; real selection happens via _darwin + .m.

#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// Menu action dispatch. NSMenuItem's target/action pairs route to a
// shared Go trampoline object; the item's tag carries the Go-side
// action ID, which we forward back to Go via the exported callback.

extern void quiMenuActionCallback(int id);

@interface QuiMenuTarget : NSObject
+ (instancetype)shared;
- (void)quiMenuFire:(id)sender;
@end

@implementation QuiMenuTarget
+ (instancetype)shared {
    static QuiMenuTarget* s = nil;
    static dispatch_once_t once;
    dispatch_once(&once, ^{ s = [[QuiMenuTarget alloc] init]; });
    return s;
}
- (void)quiMenuFire:(id)sender {
    NSMenuItem* item = (NSMenuItem*)sender;
    quiMenuActionCallback((int)item.tag);
}
@end

// Top-level "main menu" we build up across calls and install at the end.
static NSMenu* g_mainMenu = nil;

void quiResetMenuBar(void) {
    g_mainMenu = [[NSMenu alloc] initWithTitle:@""];
}

void* quiBeginSubmenu(const char* title) {
    NSString* t = title ? [NSString stringWithUTF8String:title] : @"";
    NSMenu* m = [[NSMenu alloc] initWithTitle:t];
    m.autoenablesItems = NO;
    return (__bridge_retained void*)m;
}

void quiEndSubmenu(void* parent, void* submenu) {
    NSMenu* sub = (__bridge_transfer NSMenu*)submenu;
    if (parent == NULL) {
        // Top-level: attach to the main menu via a containing item.
        if (g_mainMenu == nil) {
            g_mainMenu = [[NSMenu alloc] initWithTitle:@""];
        }
        NSMenuItem* container = [[NSMenuItem alloc] initWithTitle:sub.title action:nil keyEquivalent:@""];
        container.submenu = sub;
        [g_mainMenu addItem:container];
    } else {
        NSMenu* p = (__bridge NSMenu*)parent;
        NSMenuItem* container = [[NSMenuItem alloc] initWithTitle:sub.title action:nil keyEquivalent:@""];
        container.submenu = sub;
        [p addItem:container];
    }
}

void quiAddItem(void* parent, const char* title, const char* keyEq, int mods, int actionID, int enabled, int checked) {
    NSMenu* p = (__bridge NSMenu*)parent;
    if (p == nil) return;

    NSString* t = title ? [NSString stringWithUTF8String:title] : @"";
    NSString* k = keyEq ? [NSString stringWithUTF8String:keyEq] : @"";
    // macOS expects lowercase for single-letter shortcuts; Shift is
    // carried in the modifier mask. Lowercase the first char if the
    // caller passed something like "S".
    if (k.length == 1) {
        k = [k lowercaseString];
    }
    NSMenuItem* item = [[NSMenuItem alloc] initWithTitle:t
                                                  action:@selector(quiMenuFire:)
                                           keyEquivalent:k];
    NSEventModifierFlags flags = 0;
    if (mods & 1) flags |= NSEventModifierFlagShift;
    if (mods & 2) flags |= NSEventModifierFlagControl;
    if (mods & 4) flags |= NSEventModifierFlagOption;
    if (mods & 8) flags |= NSEventModifierFlagCommand;
    item.keyEquivalentModifierMask = flags;
    item.target = [QuiMenuTarget shared];
    item.tag = actionID;
    item.enabled = (enabled != 0);
    item.state = (checked != 0) ? NSControlStateValueOn : NSControlStateValueOff;
    [p addItem:item];
}

void quiAddSeparator(void* parent) {
    NSMenu* p = (__bridge NSMenu*)parent;
    [p addItem:[NSMenuItem separatorItem]];
}

void quiInstallMainMenu(void) {
    if (g_mainMenu == nil) return;
    [NSApp setMainMenu:g_mainMenu];
    // GLFW installs its default menu bar when the app finishes launching,
    // which can be after an app sets its own (a menu built at startup).
    // Put ours back once launching is over; later calls replace it as
    // usual.
    if (![[NSRunningApplication currentApplication] isFinishedLaunching]) {
        static id observer = nil;
        if (observer != nil) return;
        observer = [[NSNotificationCenter defaultCenter]
            addObserverForName:NSApplicationDidFinishLaunchingNotification
                        object:nil
                         queue:[NSOperationQueue mainQueue]
                    usingBlock:^(NSNotification* note) {
                        if (g_mainMenu != nil) [NSApp setMainMenu:g_mainMenu];
                        [[NSNotificationCenter defaultCenter] removeObserver:observer];
                        observer = nil;
                    }];
    }
}

// quiAddRoleItem adds a standard item AppKit performs itself (no Go
// action). Roles match qui.MenuRole.
void quiAddRoleItem(void* parent, const char* title, int role, const char* keyEq, int mods) {
    NSMenu* p = (__bridge NSMenu*)parent;
    if (p == nil) return;
    NSString* t = title ? [NSString stringWithUTF8String:title] : @"";
    NSString* k = keyEq ? [NSString stringWithUTF8String:keyEq] : @"";
    if (k.length == 1) k = [k lowercaseString];
    if (role == 4) { // Services
        NSMenuItem* item = [[NSMenuItem alloc] initWithTitle:t action:nil keyEquivalent:@""];
        NSMenu* services = [[NSMenu alloc] initWithTitle:t];
        item.submenu = services;
        [NSApp setServicesMenu:services];
        [p addItem:item];
        return;
    }
    SEL sel = nil;
    id target = nil; // nil: first responder (the key window)
    switch (role) {
    case 1: sel = @selector(hide:); target = NSApp; break;
    case 2: sel = @selector(hideOtherApplications:); target = NSApp; break;
    case 3: sel = @selector(unhideAllApplications:); target = NSApp; break;
    case 5: sel = @selector(performMiniaturize:); break;
    case 6: sel = @selector(performZoom:); break;
    case 7: sel = @selector(arrangeInFront:); target = NSApp; break;
    default: return;
    }
    NSMenuItem* item = [[NSMenuItem alloc] initWithTitle:t action:sel keyEquivalent:k];
    NSEventModifierFlags flags = 0;
    if (mods & 1) flags |= NSEventModifierFlagShift;
    if (mods & 2) flags |= NSEventModifierFlagControl;
    if (mods & 4) flags |= NSEventModifierFlagOption;
    if (mods & 8) flags |= NSEventModifierFlagCommand;
    item.keyEquivalentModifierMask = flags;
    item.target = target;
    [p addItem:item];
}

// quiSetWindowsMenu makes menu the Window menu (AppKit lists windows in it).
void quiSetWindowsMenu(void* menu) {
    [NSApp setWindowsMenu:(__bridge NSMenu*)menu];
}

// quiSetHelpMenu makes menu the Help menu (AppKit adds its search field).
void quiSetHelpMenu(void* menu) {
    [NSApp setHelpMenu:(__bridge NSMenu*)menu];
}
