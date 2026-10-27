//go:build darwin && cgo
// ^ advisory; the _darwin.m suffix is what gates it.

#import <Cocoa/Cocoa.h>
#include <stdint.h>
#include <string.h>

// Native image cursors, shared by the macOS backends through contract.Host.
// See native_cursor_image.go / cursor_darwin.go.

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

