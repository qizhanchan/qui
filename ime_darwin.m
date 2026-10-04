//go:build darwin && cgo
// ^ Go build tag above is advisory for IDE; the file is compiled on
// macOS only because of its .m + _darwin suffix, so the Go build
// system picks it up automatically under darwin.

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <stdint.h>

// Go-exported callbacks (emitted by cgo from ime_darwin.go).
#include "_cgo_export.h"

// -------------------------------------------------------------------
// macOS IME bridge — hooks into GLFW's _GLFWContentView NSTextInputClient
// conformance by swapping out three selector implementations at runtime.
// GLFW 3.3's existing implementations for these are effectively no-ops
// (empty bodies or returning NSZeroRect), so nothing of GLFW's own
// logic is lost.
//
// Keys for objc_setAssociatedObject on each view — one storing the
// Qui window handle (so callbacks know which Window to route to) and
// one tracking whether the view currently has active preedit text
// (some IMEs, notably kotoeri for Japanese, query this explicitly).
// -------------------------------------------------------------------

static const void* kQuiHandleKey = &kQuiHandleKey;
static const void* kQuiHasMarkedKey = &kQuiHasMarkedKey;

// Bumped every time the IME touches composition via any NSTextInputClient
// entry point (setMarkedText / unmarkText / insertText / doCommandBySelector).
// Used by quiKeyDown to detect the "stuck preedit" case: if a Backspace
// goes in while hasMarkedText is YES and the counter doesn't change
// afterwards, the IME decided to do nothing but also didn't tell us to
// clear — typically macOS 拼音 when reducing a single-letter preedit past
// the empty state. In that case we force-clear on our side.
static uint64_t gIMEEventCounter = 0;

// Replacement: setMarkedText:selectedRange:replacementRange:
// Called by the OS during composition whenever the preedit text changes.
static void quiSetMarkedText(id self, SEL _cmd,
                             id string, NSRange selectedRange, NSRange replacementRange) {
    gIMEEventCounter++;
    NSString* plain = [string isKindOfClass:[NSAttributedString class]]
                          ? [(NSAttributedString*)string string]
                          : (NSString*)string;
    if (plain == nil) plain = @"";

    objc_setAssociatedObject(self, kQuiHasMarkedKey,
                             @([plain length] > 0), OBJC_ASSOCIATION_RETAIN);

    NSNumber* handleNum = objc_getAssociatedObject(self, kQuiHandleKey);
    if (handleNum == nil) return;

    const char* utf8 = [plain UTF8String];
    if (utf8 == NULL) utf8 = "";
    quiIMESetPreedit((uintptr_t)[handleNum unsignedLongValue],
                     (char*)utf8,
                     (int)selectedRange.location);
}

// Replacement: unmarkText
// OS tells us composition is canceled/finalized; clear preedit.
static void quiUnmarkText(id self, SEL _cmd) {
    gIMEEventCounter++;
    objc_setAssociatedObject(self, kQuiHasMarkedKey, @NO, OBJC_ASSOCIATION_RETAIN);

    NSNumber* handleNum = objc_getAssociatedObject(self, kQuiHandleKey);
    if (handleNum == nil) return;

    quiIMESetPreedit((uintptr_t)[handleNum unsignedLongValue], (char*)"", 0);
}

// Replacement: hasMarkedText
// GLFW's version returns NO unconditionally, confusing some IMEs.
// We track the real state via an associated BOOL.
static BOOL quiHasMarkedText(id self, SEL _cmd) {
    NSNumber* n = objc_getAssociatedObject(self, kQuiHasMarkedKey);
    return n != nil && [n boolValue];
}

// Replacement: doCommandBySelector:
// Called when the IME decides to hand a key command back to the app
// instead of consuming it as part of composition. macOS 拼音 does
// this when the marked text is a single Latin letter and the user
// presses Backspace — instead of calling setMarkedText(@"") or
// unmarkText, it invokes deleteBackward: on the view and quietly
// ends its own composition. GLFW's base implementation is a no-op,
// so without this override our preedit stays stuck on the last
// character (e.g. "w") while the IME has already closed its
// candidate window; Backspace can never clear it because our
// KeyEvent handler swallows Backspace whenever preedit != "".
//
// Fix: when the IME delegates *any* command while we still think we
// have marked text, treat it as "composition ended" and clear the
// preedit. We don't forward the command to the widget's normal key
// path — the user's intent when backspacing the last preedit char
// is to cancel composition, not to also delete a rune of real text.
static void quiDoCommandBySelector(id self, SEL _cmd, SEL aSelector) {
    gIMEEventCounter++;
    NSNumber* markedNum = objc_getAssociatedObject(self, kQuiHasMarkedKey);
    if (markedNum == nil || ![markedNum boolValue]) return;

    objc_setAssociatedObject(self, kQuiHasMarkedKey, @NO, OBJC_ASSOCIATION_RETAIN);

    NSNumber* handleNum = objc_getAssociatedObject(self, kQuiHandleKey);
    if (handleNum == nil) return;

    quiIMESetPreedit((uintptr_t)[handleNum unsignedLongValue], (char*)"", 0);
}

// Wrap insertText:replacementRange: — we need to bump the event
// counter so quiKeyDown's "IME did nothing" detector doesn't
// misfire when the IME commits a character. GLFW's original IMP
// still runs (chained via gPrevInsertText) so CharCallback fires.
static void (*gPrevInsertText)(id, SEL, id, NSRange) = NULL;
static void quiInsertText(id self, SEL _cmd, id string, NSRange replacementRange) {
    gIMEEventCounter++;
    // Committing replaces any marked text — the IME's internal state
    // says "composition done" at this point, so our mirror must agree.
    objc_setAssociatedObject(self, kQuiHasMarkedKey, @NO, OBJC_ASSOCIATION_RETAIN);
    if (gPrevInsertText) {
        gPrevInsertText(self, _cmd, string, replacementRange);
    }
}

// Wrap keyDown: — swizzled over GLFW's own keyDown: so we can detect
// the "stuck preedit" case. GLFW's keyDown: fires _glfwInputKey
// (our KeyCallback) and then calls interpretKeyEvents: which feeds
// the event to the active input method. After interpretKeyEvents
// returns, we know which path the IME took: if hasMarkedText stayed
// YES and gIMEEventCounter didn't change, the IME silently ignored
// the key while leaving marked text intact. That's the macOS 拼音
// bug where a final Backspace on a single-letter preedit closes the
// candidate window but leaves our marked text stuck. Force-clear
// here so the user can proceed.
static void (*gPrevKeyDown)(id, SEL, NSEvent*) = NULL;
static void quiKeyDown(id self, SEL _cmd, NSEvent* event) {
    NSNumber* markedBefore = objc_getAssociatedObject(self, kQuiHasMarkedKey);
    BOOL wasMarked = markedBefore != nil && [markedBefore boolValue];
    uint64_t counterBefore = gIMEEventCounter;

    if (gPrevKeyDown) {
        gPrevKeyDown(self, _cmd, event);
    }

    if (!wasMarked) return;
    if (gIMEEventCounter != counterBefore) return; // IME did something
    NSNumber* markedAfter = objc_getAssociatedObject(self, kQuiHasMarkedKey);
    BOOL stillMarked = markedAfter != nil && [markedAfter boolValue];
    if (!stillMarked) return;

    // Only force-clear for keys that typically drive composition
    // (Backspace, Escape). Don't touch marked text for Cmd-shortcuts,
    // modifier-only events, function keys — the IME may legitimately
    // leave composition untouched for those. Delete = 51, Escape = 53.
    unsigned short keyCode = [event keyCode];
    if (keyCode != 51 && keyCode != 53) return;

    objc_setAssociatedObject(self, kQuiHasMarkedKey, @NO, OBJC_ASSOCIATION_RETAIN);
    NSNumber* handleNum = objc_getAssociatedObject(self, kQuiHandleKey);
    if (handleNum == nil) return;
    quiIMESetPreedit((uintptr_t)[handleNum unsignedLongValue], (char*)"", 0);
}

// Replacement: firstRectForCharacterRange:actualRange:
// The OS uses this to position the candidate-selection popup next to
// the current caret. We ask the Go side for Qui's caret rect (window
// coords) and convert to screen coords via the NSView's window.
static NSRect quiFirstRectForCharacterRange(id self, SEL _cmd,
                                            NSRange range, NSRangePointer actualRange) {
    NSNumber* handleNum = objc_getAssociatedObject(self, kQuiHandleKey);
    if (handleNum == nil) return NSZeroRect;

    double x = 0, y = 0, w = 0, h = 0;
    quiIMECaretRect((uintptr_t)[handleNum unsignedLongValue], &x, &y, &w, &h);

    NSView* view = self;
    // Qui uses top-left origin in window coords; NSView's default
    // coordinate system is bottom-left. Flip Y relative to the view.
    NSRect viewBounds = [view bounds];
    BOOL flipped = [view isFlipped];
    CGFloat nsY = flipped ? y : (viewBounds.size.height - y - h);
    NSRect local = NSMakeRect(x, nsY, w, h);
    NSRect inWindow = [view convertRect:local toView:nil];
    NSRect onScreen = [[view window] convertRectToScreen:inWindow];
    return onScreen;
}

// -------------------------------------------------------------------
// Installer — called once per Window from Go. Replaces selector
// implementations the first time and associates the window handle
// onto the NSView for per-view routing in callbacks.

static BOOL quiIMEInstalled = NO;

void quiInstallIME(void* nsWindowPtr, uintptr_t handle) {
    if (nsWindowPtr == NULL) return;
    NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
    NSView* view = [win contentView];
    if (view == nil) return;

    // Per-view: stash the Qui window handle so callbacks can route.
    objc_setAssociatedObject(view, kQuiHandleKey,
                             @((unsigned long)handle), OBJC_ASSOCIATION_RETAIN);
    objc_setAssociatedObject(view, kQuiHasMarkedKey, @NO, OBJC_ASSOCIATION_RETAIN);

    if (quiIMEInstalled) return;
    quiIMEInstalled = YES;

    Class cls = [view class];

    // Install an override on the class — class_addMethod if the class
    // itself doesn't implement this selector (adds an override without
    // mutating the parent class's IMP), else method_setImplementation
    // on the class's own method. Using method_setImplementation on an
    // inherited Method would modify the parent class GLOBALLY, which
    // is catastrophic for NSResponder selectors like doCommandBySelector:.
    #define QUI_INSTALL(sel, imp) do { \
        Method _m = class_getInstanceMethod(cls, (sel)); \
        if (_m) { \
            const char* _types = method_getTypeEncoding(_m); \
            if (!class_addMethod(cls, (sel), (IMP)(imp), _types)) { \
                method_setImplementation(_m, (IMP)(imp)); \
            } \
        } \
    } while (0)

    QUI_INSTALL(@selector(setMarkedText:selectedRange:replacementRange:), quiSetMarkedText);
    QUI_INSTALL(@selector(unmarkText), quiUnmarkText);
    QUI_INSTALL(@selector(hasMarkedText), quiHasMarkedText);
    QUI_INSTALL(@selector(doCommandBySelector:), quiDoCommandBySelector);
    QUI_INSTALL(@selector(firstRectForCharacterRange:actualRange:), quiFirstRectForCharacterRange);

    // Wrap insertText:replacementRange: — preserve GLFW's own IMP so
    // CharCallback still fires for committed characters. Need a
    // pointer to the previous IMP to chain into it.
    {
        Method _m = class_getInstanceMethod(cls, @selector(insertText:replacementRange:));
        if (_m) {
            gPrevInsertText = (void (*)(id, SEL, id, NSRange))method_getImplementation(_m);
            const char* _types = method_getTypeEncoding(_m);
            if (!class_addMethod(cls, @selector(insertText:replacementRange:),
                                 (IMP)quiInsertText, _types)) {
                method_setImplementation(_m, (IMP)quiInsertText);
            }
        }
    }

    // Wrap keyDown: — we chain into GLFW's implementation so its
    // _glfwInputKey + interpretKeyEvents: behavior is preserved.
    // Our wrapper runs AFTER the original so it can observe whether
    // the IME updated composition during interpretKeyEvents.
    {
        Method _m = class_getInstanceMethod(cls, @selector(keyDown:));
        if (_m) {
            gPrevKeyDown = (void (*)(id, SEL, NSEvent*))method_getImplementation(_m);
            const char* _types = method_getTypeEncoding(_m);
            if (!class_addMethod(cls, @selector(keyDown:),
                                 (IMP)quiKeyDown, _types)) {
                method_setImplementation(_m, (IMP)quiKeyDown);
            }
        }
    }
    #undef QUI_INSTALL
}
