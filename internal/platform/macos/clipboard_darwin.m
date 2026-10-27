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

// quiPasteboardImage reads an image off the pasteboard into a malloc'd
// buffer the caller frees: a copied image file (Finder) as its own bytes,
// else PNG data, else TIFF (screenshots, browsers) converted to PNG. ext
// gets the file extension without the dot. Returns NULL when there is none.
void *quiPasteboardImage(int *len, char **ext) {
    *len = 0;
    *ext = NULL;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSData *data = nil;
        NSString *e = nil;
        NSArray *urls = [pb readObjectsForClasses:@[ [NSURL class] ]
                                          options:@{NSPasteboardURLReadingFileURLsOnlyKey : @YES}];
        if ([urls count] > 0) {
            NSURL *u = urls[0];
            NSString *x = [[u pathExtension] lowercaseString];
            NSSet *ok = [NSSet setWithArray:@[ @"png", @"jpg", @"jpeg", @"gif", @"webp", @"svg", @"bmp" ]];
            if ([ok containsObject:x]) {
                data = [NSData dataWithContentsOfURL:u];
                e = x;
            } else {
                return NULL; // a non-image file: its icon is not the content
            }
        }
        if (!data) {
            data = [pb dataForType:NSPasteboardTypePNG];
            e = @"png";
        }
        if (!data) {
            NSData *tiff = [pb dataForType:NSPasteboardTypeTIFF];
            if (tiff) {
                NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:tiff];
                data = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                e = @"png";
            }
        }
        if (!data || [data length] == 0) {
            return NULL;
        }
        void *buf = malloc([data length]);
        memcpy(buf, [data bytes], [data length]);
        *len = (int)[data length];
        *ext = strdup([e UTF8String]);
        return buf;
    }
}

// quiSetPasteboardImage publishes an image (PNG bytes) together with a
// plain-text flavor, so image consumers paste the picture and text
// editors the text (e.g. the Markdown that references it).
void quiSetPasteboardImage(const void *png, int len, const char *plain) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        NSData *d = [NSData dataWithBytes:png length:len];
        NSString *p = plain ? [NSString stringWithUTF8String:plain] : @"";
        [pb declareTypes:@[ NSPasteboardTypePNG, NSPasteboardTypeString ] owner:nil];
        [pb setData:d forType:NSPasteboardTypePNG];
        [pb setString:p forType:NSPasteboardTypeString];
    }
}

// quiPasteboardFiles returns the paths of the file URLs on the pasteboard
// (files copied in Finder) as a malloc'd array of malloc'd strings the
// caller frees; *n gets their count. NULL when there are none.
char **quiPasteboardFiles(int *n) {
    *n = 0;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSArray *urls = [pb readObjectsForClasses:@[ [NSURL class] ]
                                          options:@{NSPasteboardURLReadingFileURLsOnlyKey : @YES}];
        if ([urls count] == 0) {
            return NULL;
        }
        char **out = malloc(sizeof(char *) * [urls count]);
        int k = 0;
        for (NSURL *u in urls) {
            const char *p = [[u path] fileSystemRepresentation];
            if (p) {
                out[k++] = strdup(p);
            }
        }
        *n = k;
        return out;
    }
}

// quiSetPasteboardFiles publishes file URLs the way Finder's Copy does —
// one pasteboard item per file, so Finder (and any app reading file URLs)
// pastes the files themselves — and the paths, one per line, as the first
// item's plain-text flavor, so a text editor pastes the paths rather than
// file:// URLs.
void quiSetPasteboardFiles(char **paths, int n) {
    @autoreleasepool {
        NSMutableArray<NSPasteboardItem *> *items = [NSMutableArray arrayWithCapacity:n];
        NSMutableArray<NSString *> *lines = [NSMutableArray arrayWithCapacity:n];
        NSFileManager *fm = [NSFileManager defaultManager];
        for (int i = 0; i < n; i++) {
            NSString *p = [fm stringWithFileSystemRepresentation:paths[i] length:strlen(paths[i])];
            if (!p) {
                continue;
            }
            NSPasteboardItem *it = [[NSPasteboardItem alloc] init];
            [it setString:[[NSURL fileURLWithPath:p] absoluteString] forType:NSPasteboardTypeFileURL];
            [items addObject:it];
            [lines addObject:p];
        }
        if ([items count] == 0) {
            return;
        }
        [items[0] setString:[lines componentsJoinedByString:@"\n"] forType:NSPasteboardTypeString];
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        [pb writeObjects:items];
    }
}
