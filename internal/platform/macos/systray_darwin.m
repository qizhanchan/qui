//go:build darwin && cgo
// ^ Advisory for IDE only; real selection happens via _darwin + .m.

#import <Cocoa/Cocoa.h>
#include <stdint.h>
#include <string.h>

// QuiMenuTarget is defined in menu_native_darwin.m. We declare its
// interface here so we can route the status-item button's click
// through the same action-id trampoline NSMenuItem uses.
@interface QuiMenuTarget : NSObject
+ (instancetype)shared;
- (void)quiMenuFire:(id)sender;
@end

// Each NSStatusItem we create gets retained on the Go side via a
// __bridge_retained handle. Destroying it both removes the item
// from the status bar and releases the Go-side retain.

void* quiCreateStatusItem(void) {
    NSStatusBar* bar = [NSStatusBar systemStatusBar];
    NSStatusItem* item = [bar statusItemWithLength:NSSquareStatusItemLength];
    // Default to highlight-on-click (modern AppKit ignores this for
    // menu-driven items but it's a no-op so we leave it alone).
    return (__bridge_retained void*)item;
}

void quiStatusItemSetImageRGBA(void* itemPtr, const uint8_t* pixels,
                               int pxW, int pxH, int ptW, int ptH,
                               int isTemplate) {
    if (itemPtr == NULL || pixels == NULL || pxW <= 0 || pxH <= 0) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;

    // Build an NSBitmapImageRep with the supplied premultiplied RGBA
    // bytes. The default bitmap format (no NSAlphaNonpremultiplied)
    // matches Go's image.RGBA semantics.
    NSBitmapImageRep* rep = [[NSBitmapImageRep alloc]
        initWithBitmapDataPlanes:NULL
                      pixelsWide:pxW
                      pixelsHigh:pxH
                   bitsPerSample:8
                 samplesPerPixel:4
                        hasAlpha:YES
                        isPlanar:NO
                  colorSpaceName:NSDeviceRGBColorSpace
                     bytesPerRow:pxW * 4
                    bitsPerPixel:32];
    if (rep == nil) return;
    memcpy([rep bitmapData], pixels, (size_t)pxW * (size_t)pxH * 4);
    // rep.size in points: callers pass logical point dims separately
    // so the rep advertises @2x correctly on retina displays.
    if (ptW > 0 && ptH > 0) {
        [rep setSize:NSMakeSize(ptW, ptH)];
    }

    NSImage* nsImg = [[NSImage alloc] initWithSize:NSMakeSize(ptW > 0 ? ptW : pxW,
                                                              ptH > 0 ? ptH : pxH)];
    [nsImg addRepresentation:rep];
    [nsImg setTemplate:isTemplate != 0];

    if (item.button != nil) {
        item.button.image = nsImg;
    }
}

void quiStatusItemClearImage(void* itemPtr) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    if (item.button != nil) {
        item.button.image = nil;
    }
}

void quiStatusItemSetTitle(void* itemPtr, const char* title) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    NSString* t = title ? [NSString stringWithUTF8String:title] : @"";
    if (item.button != nil) {
        item.button.title = t;
    }
}

void quiStatusItemSetTooltip(void* itemPtr, const char* tip) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    NSString* t = tip ? [NSString stringWithUTF8String:tip] : @"";
    if (item.button != nil) {
        item.button.toolTip = t;
    }
}

void quiStatusItemSetMenu(void* itemPtr, void* nsMenu) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    if (nsMenu == NULL) {
        [item setMenu:nil];
        return;
    }
    // The Go side handed us a retained NSMenu via quiBeginSubmenu;
    // consume that retain — NSStatusItem holds its own reference once
    // assigned, so the Go-side retain would otherwise leak.
    NSMenu* menu = (__bridge_transfer NSMenu*)nsMenu;
    [item setMenu:menu];
}

void quiStatusItemSetClickAction(void* itemPtr, int actionID) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    if (item.button == nil) return;
    if (actionID == 0) {
        item.button.target = nil;
        item.button.action = NULL;
        item.button.tag = 0;
        return;
    }
    item.button.target = [QuiMenuTarget shared];
    item.button.action = @selector(quiMenuFire:);
    item.button.tag = actionID;
}

void quiStatusItemSetVisible(void* itemPtr, int visible) {
    if (itemPtr == NULL) return;
    NSStatusItem* item = (__bridge NSStatusItem*)itemPtr;
    [item setVisible:(visible != 0)];
}

void quiStatusItemDestroy(void* itemPtr) {
    if (itemPtr == NULL) return;
    // __bridge_transfer reclaims the retain Go has held since
    // quiCreateStatusItem; ARC releases on scope exit.
    NSStatusItem* item = (__bridge_transfer NSStatusItem*)itemPtr;
    [item setMenu:nil];
    if (item.button != nil) {
        item.button.image = nil;
        item.button.target = nil;
        item.button.action = NULL;
    }
    [[NSStatusBar systemStatusBar] removeStatusItem:item];
}
