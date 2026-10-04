//go:build darwin && cgo
// ^ Advisory for IDE; file is compiled only on darwin because of .m
// + _darwin suffix.

#import <Cocoa/Cocoa.h>
#include <stdlib.h>
#include <string.h>

// Native file dialogs + alert. All three entry points below run the
// panel synchronously via -runModal on the main thread. Go's main
// goroutine is locked to the OS main thread (runtime.LockOSThread in
// app.go's init), and GLFW pumps events on that same thread, so Cgo
// calls from app callbacks land where NSOpenPanel requires.
//
// allowedFileTypes is deprecated on macOS 13+ in favor of
// allowedContentTypes (UTType). It still works and is the simpler
// API while avoiding a UniformTypeIdentifiers dependency — suppress
// the deprecation warning locally.

// Copy an NSString into a strdup'd heap buffer. Go caller frees with
// C.free. Returns strdup("") for nil to keep the contract "never
// returns NULL" simple on the Go side.
static char* copyNSStringToHeap(NSString* s) {
    const char* c = [s UTF8String];
    if (c == NULL) c = "";
    return strdup(c);
}

static NSArray<NSString*>* splitByNewline(const char* cstr) {
    if (cstr == NULL || cstr[0] == '\0') return @[];
    NSString* s = [NSString stringWithUTF8String:cstr];
    if (s == nil) return @[];
    NSArray* parts = [s componentsSeparatedByString:@"\n"];
    // Drop empty strings so a trailing "\n" or an all-empty input
    // doesn't turn into a bogus "" extension / button.
    NSMutableArray* out = [NSMutableArray arrayWithCapacity:parts.count];
    for (NSString* p in parts) {
        if (p.length > 0) [out addObject:p];
    }
    return out;
}

// Apply common options that NSOpenPanel and NSSavePanel share
// (they're both NSSavePanel under the hood).
static void applyCommonPanelOptions(NSSavePanel* panel,
                                    const char* title,
                                    const char* message,
                                    const char* startDir) {
    if (title && *title) panel.title = [NSString stringWithUTF8String:title];
    if (message && *message) panel.message = [NSString stringWithUTF8String:message];
    if (startDir && *startDir) {
        NSString* p = [NSString stringWithUTF8String:startDir];
        panel.directoryURL = [NSURL fileURLWithPath:p];
    }
}

char* quiShowOpenPanel(const char* title, const char* message,
                       const char* startDir, const char* allowedExts,
                       int multi, int dirsOnly) {
    @autoreleasepool {
        NSOpenPanel* panel = [NSOpenPanel openPanel];
        applyCommonPanelOptions(panel, title, message, startDir);

        panel.allowsMultipleSelection = (multi != 0);
        panel.canChooseFiles = (dirsOnly == 0);
        panel.canChooseDirectories = (dirsOnly != 0);
        panel.resolvesAliases = YES;

        NSArray<NSString*>* exts = splitByNewline(allowedExts);
        if (exts.count > 0) {
            #pragma clang diagnostic push
            #pragma clang diagnostic ignored "-Wdeprecated-declarations"
            panel.allowedFileTypes = exts;
            #pragma clang diagnostic pop
        }

        // GLFW's window may not be key when the dialog opens (e.g.
        // called from a just-clicked button). Activate the app so the
        // panel foregrounds instead of appearing behind.
        [NSApp activateIgnoringOtherApps:YES];

        NSInteger rc = [panel runModal];
        if (rc != NSModalResponseOK) return strdup("");

        NSMutableArray<NSString*>* picked = [NSMutableArray array];
        for (NSURL* url in panel.URLs) {
            NSString* p = url.path;
            if (p != nil) [picked addObject:p];
        }
        NSString* joined = [picked componentsJoinedByString:@"\n"];
        return copyNSStringToHeap(joined);
    }
}

char* quiShowSavePanel(const char* title, const char* message,
                       const char* startDir, const char* filename,
                       const char* allowedExts) {
    @autoreleasepool {
        NSSavePanel* panel = [NSSavePanel savePanel];
        applyCommonPanelOptions(panel, title, message, startDir);

        if (filename && *filename) {
            panel.nameFieldStringValue = [NSString stringWithUTF8String:filename];
        }

        NSArray<NSString*>* exts = splitByNewline(allowedExts);
        if (exts.count > 0) {
            #pragma clang diagnostic push
            #pragma clang diagnostic ignored "-Wdeprecated-declarations"
            panel.allowedFileTypes = exts;
            #pragma clang diagnostic pop
        }

        [NSApp activateIgnoringOtherApps:YES];

        NSInteger rc = [panel runModal];
        if (rc != NSModalResponseOK) return strdup("");
        NSString* p = panel.URL.path;
        return copyNSStringToHeap(p != nil ? p : @"");
    }
}

int quiShowAlert(const char* title, const char* message,
                 int style, const char* buttonsJoined) {
    @autoreleasepool {
        NSAlert* alert = [[NSAlert alloc] init];
        if (title && *title) alert.messageText = [NSString stringWithUTF8String:title];
        if (message && *message) alert.informativeText = [NSString stringWithUTF8String:message];

        switch (style) {
            case 1: alert.alertStyle = NSAlertStyleWarning; break;
            case 2: alert.alertStyle = NSAlertStyleCritical; break;
            default: alert.alertStyle = NSAlertStyleInformational; break;
        }

        NSArray<NSString*>* buttons = splitByNewline(buttonsJoined);
        if (buttons.count == 0) {
            [alert addButtonWithTitle:@"OK"];
        } else {
            for (NSString* label in buttons) {
                [alert addButtonWithTitle:label];
            }
        }

        [NSApp activateIgnoringOtherApps:YES];
        NSModalResponse rc = [alert runModal];
        // NSAlert numbers buttons starting at NSAlertFirstButtonReturn
        // (1000). Subtract to get a stable 0-based index mirroring the
        // order buttons were added.
        return (int)(rc - NSAlertFirstButtonReturn);
    }
}
