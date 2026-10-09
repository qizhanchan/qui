//go:build darwin && cgo
// ^ advisory; the _darwin.m suffix is what gates it.

#import <Cocoa/Cocoa.h>
#include <stdint.h>
#include <string.h>

// Native image cursors shared by both macOS backends (GLFW and cocoa).
// See cursor_image.go / cursor_darwin.go.

// quiCursorCreate builds an NSCursor from premultiplied RGBA pixels. scale
// pixels make one point, so the NSImage carries a full-resolution
// representation at its point size — crisp on a 2x display. Returns a +1
// reference (release with quiCursorRelease).
void* quiCursorCreate(const uint8_t* rgba, int w, int h, double scale,
                      double hotX, double hotY) {
    @autoreleasepool {
        if (rgba == NULL || w <= 0 || h <= 0 || scale <= 0) return NULL;
        NSBitmapImageRep* rep = [[NSBitmapImageRep alloc]
            initWithBitmapDataPlanes:NULL
                          pixelsWide:w
                          pixelsHigh:h
                       bitsPerSample:8
                     samplesPerPixel:4
                            hasAlpha:YES
                            isPlanar:NO
                      colorSpaceName:NSDeviceRGBColorSpace
                         bytesPerRow:w * 4
                        bitsPerPixel:32];
        if (rep == nil) return NULL;
        memcpy([rep bitmapData], rgba, (size_t)w * (size_t)h * 4);
        NSSize points = NSMakeSize(w / scale, h / scale);
        [rep setSize:points];
        NSImage* img = [[NSImage alloc] initWithSize:points];
        [img addRepresentation:rep];
        NSCursor* cursor = [[NSCursor alloc] initWithImage:img
                                                   hotSpot:NSMakePoint(hotX, hotY)];
        if (cursor == nil) return NULL;
        return (__bridge_retained void*)cursor;
    }
}

void quiCursorRelease(void* cursor) {
    if (cursor) CFRelease(cursor);
}

// quiGLFWCursorAdopt replaces the NSCursor inside a GLFWcursor with ours,
// so GLFW keeps doing all of its own cursor management (cursorUpdate:,
// focus changes, hidden mode) while showing a high-density image that
// glfwCreateCursor cannot express (it sizes images 1 pixel = 1 point).
//
// GLFW 3.3's private layout is struct _GLFWcursor { _GLFWcursor* next;
// _GLFWcursorNS { id object; } ns; }. The swap is refused unless the slot
// really holds an NSCursor, so a layout change degrades to the 1x image
// instead of corrupting memory. GLFW releases the object (non-ARC) on
// glfwDestroyCursor, which consumes the reference we hand it.
int quiGLFWCursorAdopt(void* glfwCursor, void* nsCursor) {
    if (glfwCursor == NULL || nsCursor == NULL) return 0;
    void** slots = (void**)glfwCursor;
    void* old = slots[1];
    if (old == NULL) return 0;
    id oldObj = (__bridge id)old;
    if (![oldObj isKindOfClass:[NSCursor class]]) return 0;
    CFRetain(nsCursor);
    slots[1] = nsCursor;
    CFRelease(old);
    return 1;
}
