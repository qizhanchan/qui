//go:build darwin

#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

// quiSetPasteboardRich publishes both a plain-text and an HTML flavor to
// the general pasteboard in one declaration, so rich consumers (Word,
// Pages, browsers) read public.html while plain-text editors read the
// NSPasteboardTypeString fallback.
void quiSetPasteboardRich(const char *plain, const char *html) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        NSString *p = plain ? [NSString stringWithUTF8String:plain] : @"";
        NSString *h = html ? [NSString stringWithUTF8String:html] : @"";
        [pb declareTypes:@[ NSPasteboardTypeHTML, NSPasteboardTypeString ]
                   owner:nil];
        [pb setString:h forType:NSPasteboardTypeHTML];
        [pb setString:p forType:NSPasteboardTypeString];
    }
}

// quiReadHTMLFlavor reads the pasteboard's HTML flavor, or nil when it has
// none.
//
// stringForType is the normal read, but the flavor is legitimately DATA: an
// app may declare public.html and write bytes rather than an NSString, and
// AppKit will not transcode those for us. Word and some browsers write UTF-16
// with a BOM, which is why the fallback tries that too — read as UTF-8 it
// yields nil (or, worse, mojibake) and the paste would silently arrive empty.
static NSString *quiReadHTMLFlavor(NSPasteboard *pb) {
    NSString *type = [pb availableTypeFromArray:@[ NSPasteboardTypeHTML ]];
    if (!type) {
        return nil;
    }
    NSString *s = [pb stringForType:type];
    if (s) {
        return s;
    }
    NSData *d = [pb dataForType:type];
    if (!d || [d length] == 0) {
        return nil;
    }
    s = [[NSString alloc] initWithData:d encoding:NSUTF8StringEncoding];
    if (!s) {
        s = [[NSString alloc] initWithData:d encoding:NSUnicodeStringEncoding];
    }
    return s;
}

// quiDupUTF8 copies an NSString into a malloc'd UTF-8 buffer the caller frees.
static char *quiDupUTF8(NSString *s) {
    if (!s) {
        return NULL;
    }
    const char *utf8 = [s UTF8String];
    return utf8 ? strdup(utf8) : NULL;
}

// quiPasteboardRich reads the plain-text and HTML flavors as one snapshot, into
// malloc'd UTF-8 strings the caller frees (either may come back NULL).
//
// changeCount is what makes it a snapshot: it increments on every write, so
// reading it either side of the two flavors detects a copy that landed in
// between and the read is retried. Without that, a paste could pair one copy's
// markup with the next copy's text — a small window, but the two flavors are
// compared against each other, so a mismatch is not a harmless stale read.
// A few attempts are enough; a caller racing the clipboard forever has no
// answer to be given, so the last read wins rather than blocking the UI.
void quiPasteboardRich(char **plain, char **html) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSString *p = nil;
        NSString *h = nil;
        for (int attempt = 0; attempt < 4; attempt++) {
            NSInteger before = [pb changeCount];
            p = [pb stringForType:NSPasteboardTypeString];
            h = quiReadHTMLFlavor(pb);
            if ([pb changeCount] == before) {
                break;
            }
        }
        if (plain) {
            *plain = quiDupUTF8(p);
        }
        if (html) {
            *html = quiDupUTF8(h);
        }
    }
}
