//go:build darwin && cgo
// ^ Go build tag above is advisory for IDE; the file is compiled on
// macOS only because of its .m + _darwin suffix, so the Go build
// system picks it up automatically under darwin.

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <stdint.h>

// Go-exported callbacks (emitted by cgo from gesture_darwin.go).
#include "_cgo_export.h"

// -------------------------------------------------------------------
// macOS multi-touch gesture bridge.
//
// GLFW 3.3's content view is a plain NSView subclass: it implements
// scrollWheel: but none of the gesture selectors, so trackpad pinch
// (NSEventTypeMagnify) and rotation reach the view and then die on
// NSResponder's default "forward to next responder". GLFW has no
// gesture API in 3.3 or 3.4 and upstream isn't adding one, so we install
// the missing selectors on its class ourselves — the same technique
// ime_darwin.m already uses for NSTextInputClient.
//
// Two kinds of installation happen here:
//
//   * magnify / rotate / smartMagnify — selectors GLFW's class does NOT
//     implement. class_addMethod adds a real override on GLFWContentView
//     without touching NSResponder's shared implementation. We do not
//     chain into the inherited IMP: its only job is to forward the event
//     further up the responder chain, which for our window is a no-op.
//
//   * scrollWheel: — a selector GLFW DOES implement, and whose behavior
//     we must preserve. We wrap it: snapshot the NSEvent's real modifier
//     flags and phase (neither survives GLFW's _glfwInputScroll
//     signature), then call GLFW's own implementation, which
//     synchronously fires the Go scroll callback that consumes the
//     snapshot. Without this wrapper, Ctrl+wheel zoom and momentum-aware
//     scrolling are impossible on any qui app.
// -------------------------------------------------------------------

// Per-view association: which Qui window this view belongs to.
static const void* kQuiGestureHandleKey = &kQuiGestureHandleKey;

// GLFW's own scrollWheel: implementation, preserved so we can chain.
static void (*gPrevScrollWheel)(id, SEL, NSEvent*) = NULL;

// quiHandleOf resolves the Qui window handle associated with a view.
// Returns 0 when the view isn't one of ours.
static uintptr_t quiHandleOf(id self) {
    NSNumber* num = objc_getAssociatedObject(self, kQuiGestureHandleKey);
    if (num == nil) return 0;
    return (uintptr_t)[num unsignedLongValue];
}

// quiPhaseOf reduces NSEventPhase (a bitmask) to the small integer the
// Go side maps onto GesturePhase. A legacy mouse wheel reports
// NSEventPhaseNone and therefore 0 — correctly meaning "this source has
// no gesture lifecycle".
static int quiPhaseOf(NSEvent* event) {
    NSEventPhase phase = [event phase];
    if (phase & NSEventPhaseBegan) return 1;
    if (phase & (NSEventPhaseChanged | NSEventPhaseStationary)) return 2;
    if (phase & NSEventPhaseEnded) return 3;
    if (phase & NSEventPhaseCancelled) return 4;
    return 0;
}

// quiScrollPhaseOf distinguishes inertial scrolling from fingers-down
// scrolling. Momentum wins when present: the fingers have already left
// the trackpad, so anything that commits or snaps on release must not
// treat these as user-driven.
static int quiScrollPhaseOf(NSEvent* event) {
    if ([event momentumPhase] != NSEventPhaseNone) return 5;
    return quiPhaseOf(event);
}

// quiModsOf normalizes NSEvent modifier flags into the bit layout
// gesture_darwin.go's mapNSMods expects (shift 1, control 2, option 4,
// command 8).
static int quiModsOf(NSEvent* event) {
    NSEventModifierFlags flags = [event modifierFlags];
    int mods = 0;
    if (flags & NSEventModifierFlagShift)   mods |= 1;
    if (flags & NSEventModifierFlagControl) mods |= 2;
    if (flags & NSEventModifierFlagOption)  mods |= 4;
    if (flags & NSEventModifierFlagCommand) mods |= 8;
    return mods;
}

// -------------------------------------------------------------------
// Selector implementations

static void quiMagnifyWithEvent(id self, SEL _cmd, NSEvent* event) {
    uintptr_t handle = quiHandleOf(self);
    if (handle == 0) return;
    quiGestureMagnify(handle, (double)[event magnification],
                      quiPhaseOf(event), quiModsOf(event));
}

static void quiRotateWithEvent(id self, SEL _cmd, NSEvent* event) {
    uintptr_t handle = quiHandleOf(self);
    if (handle == 0) return;
    quiGestureRotate(handle, (double)[event rotation],
                     quiPhaseOf(event), quiModsOf(event));
}

static void quiSmartMagnifyWithEvent(id self, SEL _cmd, NSEvent* event) {
    uintptr_t handle = quiHandleOf(self);
    if (handle == 0) return;
    quiGestureSmartMagnify(handle, quiModsOf(event));
}

// Wrapper around GLFW's scrollWheel:. Snapshot → chain → clear.
//
// The clear matters: GLFW skips _glfwInputScroll when both deltas round
// to zero, so without it a snapshot could survive and be misattributed
// to a later scroll that a different device produced.
static void quiScrollWheel(id self, SEL _cmd, NSEvent* event) {
    uintptr_t handle = quiHandleOf(self);
    if (handle != 0) {
        quiNoteScroll(handle, quiModsOf(event), quiScrollPhaseOf(event));
    }
    if (gPrevScrollWheel != NULL) {
        gPrevScrollWheel(self, _cmd, event);
    }
    if (handle != 0) {
        quiClearScroll(handle);
    }
}

// -------------------------------------------------------------------
// Installer

// quiInstallMethod installs imp for sel on cls and returns the previous
// implementation (NULL when there was none).
//
// class_addMethod is tried first because it adds an override on cls
// itself; falling back to method_setImplementation on an INHERITED
// Method would mutate the parent class globally, which for NSResponder
// selectors would affect every view in the process. fallbackTypes is
// only consulted when the selector is unknown to the runtime entirely —
// relying on class_getInstanceMethod finding an inherited declaration
// would silently no-op if Apple ever stopped declaring these.
static IMP quiInstallMethod(Class cls, SEL sel, IMP imp, const char* fallbackTypes) {
    const char* types = fallbackTypes;
    IMP prev = NULL;
    Method existing = class_getInstanceMethod(cls, sel);
    if (existing != NULL) {
        types = method_getTypeEncoding(existing);
        prev = method_getImplementation(existing);
    }
    if (!class_addMethod(cls, sel, imp, types)) {
        // cls implements the selector itself — swap its IMP in place and
        // take the real previous one so callers can chain.
        Method own = class_getInstanceMethod(cls, sel);
        if (own != NULL) {
            prev = method_setImplementation(own, imp);
        }
    }
    return prev;
}

static BOOL quiGesturesInstalled = NO;
static int gInstallReport = 0;

// quiVerifyInstalled reports whether cls now dispatches sel to imp.
static int quiVerifyInstalled(Class cls, SEL sel, IMP imp) {
    return class_getMethodImplementation(cls, sel) == imp ? 1 : 0;
}

// quiInstallGestures returns a bitmask describing what was installed, so
// the Go side can surface it under QUI_DEBUG_GESTURE:
//   bit 0  magnifyWithEvent:      installed
//   bit 1  rotateWithEvent:       installed
//   bit 2  smartMagnifyWithEvent: installed
//   bit 3  scrollWheel:           wrapped
//   bit 4  scrollWheel:           previous IMP captured (chaining works)
//
// Bit 4 is the one that matters most: if we replaced GLFW's scrollWheel:
// without capturing its implementation, scrolling would silently die in
// every qui app. It's cheap to assert and expensive to discover late.
int quiInstallGestures(void* nsWindowPtr, uintptr_t handle) {
    if (nsWindowPtr == NULL) return 0;
    NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
    NSView* view = [win contentView];
    if (view == nil) return 0;

    // Per-view: stash the Qui window handle so callbacks can route to
    // the right Window in a multi-window app.
    objc_setAssociatedObject(view, kQuiGestureHandleKey,
                             @((unsigned long)handle), OBJC_ASSOCIATION_RETAIN);

    // Class-level installs happen once per process; the per-view
    // association above is what makes them multi-window safe. Later
    // windows get the first install's report.
    if (quiGesturesInstalled) return gInstallReport;
    quiGesturesInstalled = YES;

    Class cls = [view class];
    int report = 0;

    // Gesture selectors GLFW doesn't implement — "v@:@" is
    // void(id self, SEL _cmd, id event).
    quiInstallMethod(cls, @selector(magnifyWithEvent:),
                     (IMP)quiMagnifyWithEvent, "v@:@");
    quiInstallMethod(cls, @selector(rotateWithEvent:),
                     (IMP)quiRotateWithEvent, "v@:@");
    quiInstallMethod(cls, @selector(smartMagnifyWithEvent:),
                     (IMP)quiSmartMagnifyWithEvent, "v@:@");

    // scrollWheel: — keep GLFW's implementation and chain into it.
    gPrevScrollWheel = (void (*)(id, SEL, NSEvent*))quiInstallMethod(
        cls, @selector(scrollWheel:), (IMP)quiScrollWheel, "v@:@");

    report |= quiVerifyInstalled(cls, @selector(magnifyWithEvent:),
                                 (IMP)quiMagnifyWithEvent) << 0;
    report |= quiVerifyInstalled(cls, @selector(rotateWithEvent:),
                                 (IMP)quiRotateWithEvent) << 1;
    report |= quiVerifyInstalled(cls, @selector(smartMagnifyWithEvent:),
                                 (IMP)quiSmartMagnifyWithEvent) << 2;
    report |= quiVerifyInstalled(cls, @selector(scrollWheel:),
                                 (IMP)quiScrollWheel) << 3;
    report |= (gPrevScrollWheel != NULL ? 1 : 0) << 4;

    gInstallReport = report;
    return report;
}
