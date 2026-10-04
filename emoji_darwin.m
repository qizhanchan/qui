#import <Cocoa/Cocoa.h>
#import <CoreText/CoreText.h>
#include <stdint.h>
#include <stdlib.h>

// Rasterize a UTF-8 string using Apple Color Emoji + Core Text. Result:
// a malloc'd RGBA8888 premultiplied-alpha buffer with top-left origin.
// Sized to the typographic bounds of the text — caller blits at its
// text baseline offset.
//
// Why Core Text: CoreGraphics' CGContextShowText path doesn't handle
// SBIX color glyphs (Apple's emoji font format). CTLineDraw does —
// it knows about color glyph tables and stamps the right color PNG
// rasters into the bitmap context.
void quiRenderEmoji(const char* utf8, double fontSize,
                    uint8_t** outPixels, int* outW, int* outH) {
    *outPixels = NULL;
    *outW = 0;
    *outH = 0;
    if (utf8 == NULL || *utf8 == '\0') return;

    NSString* text = [NSString stringWithUTF8String:utf8];
    if (text == nil || [text length] == 0) return;

    // Apple Color Emoji is the canonical emoji font on macOS. It
    // exists as a TTC at /System/Library/Fonts/Apple Color Emoji.ttc.
    CTFontRef font = CTFontCreateWithName(
        (CFStringRef)@"AppleColorEmoji",
        (CGFloat)fontSize,
        NULL);
    if (font == NULL) return;

    NSDictionary* attrs = @{
        (NSString*)kCTFontAttributeName: (__bridge id)font,
    };
    NSAttributedString* attrStr = [[NSAttributedString alloc]
        initWithString:text attributes:attrs];
    CFAttributedStringRef cfAttr = (__bridge CFAttributedStringRef)attrStr;
    CTLineRef line = CTLineCreateWithAttributedString(cfAttr);
    if (line == NULL) {
        CFRelease(font);
        return;
    }

    // Ink bounds give the visible pixel extent (tighter than advance
    // bounds — avoids wasteful margins). Typographic bounds include
    // ascent/descent so the raster matches text line height.
    CGFloat ascent = 0, descent = 0, leading = 0;
    double width = CTLineGetTypographicBounds(line, &ascent, &descent, &leading);
    int w = (int)ceil(width);
    int h = (int)ceil(ascent + descent);
    if (w <= 0 || h <= 0) {
        CFRelease(line);
        CFRelease(font);
        return;
    }

    size_t rowBytes = (size_t)w * 4;
    uint8_t* pixels = (uint8_t*)calloc((size_t)h * rowBytes, 1);
    if (pixels == NULL) {
        CFRelease(line);
        CFRelease(font);
        return;
    }

    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    // Premultiplied RGBA, big-endian order — matches Go's image.RGBA
    // layout for a direct byte copy. If this were BGRA we'd need a
    // channel swap pass; kCGBitmapByteOrder32Big keeps R first.
    CGContextRef ctx = CGBitmapContextCreate(
        pixels, (size_t)w, (size_t)h, 8, rowBytes, cs,
        kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
    CGColorSpaceRelease(cs);
    if (ctx == NULL) {
        free(pixels);
        CFRelease(line);
        CFRelease(font);
        return;
    }

    // Move the baseline up by `descent` so the glyph's descender fits
    // inside the bitmap rather than dropping off the bottom.
    //
    // Why no explicit Y-flip afterwards: CGBitmapContext stores pixels
    // with row 0 at the TOP of the image (PNG / Go image.RGBA
    // convention), independently of Quartz's bottom-left drawing
    // coordinate system. Drawing at y=0 in Quartz writes to the LAST
    // memory row; drawing at y=h writes to the FIRST memory row. So
    // after `translateCTM(0, descent)` + `CTLineDraw`, the glyph's
    // ascender sits near memory row 0 — exactly what Go consumers want.
    // An extra flip would undo this natural orientation.
    CGContextTranslateCTM(ctx, 0, (CGFloat)descent);
    CTLineDraw(line, ctx);
    CGContextRelease(ctx);

    CFRelease(line);
    CFRelease(font);

    *outPixels = pixels;
    *outW = w;
    *outH = h;
}
