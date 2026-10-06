//go:build darwin && cgo
// ^ advisory for editors; the _darwin.m suffix is what actually gates it.

#import <Cocoa/Cocoa.h>
#include <float.h>
#include <stdint.h>
#include <string.h>

#include "_cgo_export.h"

// Defined below; the window delegate needs it before its definition.
void quiCocoaWake(void);

// -------------------------------------------------------------------
// Native Cocoa implementation of the platform seam (see platform.go).
//
// This is the same contract platform_glfw.go implements, written
// directly against AppKit. It exists because everything qui actually
// wants from macOS beyond "a window and a GL context" — IME, menus,
// tray, dialogs, gestures, native fullscreen — was already implemented
// by going around GLFW. Owning the window and the event loop removes
// the layer those bridges have to reach through.
//
// Deliberate structural choices:
//
//   - Windows are addressed by an integer handle, never by pointer.
//     cgo forbids storing Go pointers in C and ARC makes hand-rolled
//     bridging casts a liability, so C keeps the objects in a
//     dictionary and Go only ever passes uintptr_t. The one exception
//     is quiCocoaNSWindow, which the IME and fullscreen bridges need.
//
//   - QuiView adopts NSTextInputClient with the same method set and the
//     same keyDown: ordering as GLFW's content view (key callback
//     first, then interpretKeyEvents:). That is not incidental: the IME
//     bridge in ime_darwin.m installs itself over exactly those
//     selectors on whatever class [window contentView] happens to be,
//     so matching the shape means IME keeps working with no changes
//     there.
//
//   - Gestures are real responder methods here rather than the runtime
//     swizzle the GLFW backend needs (platform_glfw_gesture_darwin.m).
//     Same events, no class patching.
// -------------------------------------------------------------------

// NSOpenGLContext is formally deprecated but remains the only way to
// attach a GL context to an NSView, and qui's renderer is GL-based on
// every platform. The Metal/CALayer presentation path is a separate
// surface (see platform.go's platformSurface); until it lands, silence
// the noise rather than have every build print it.
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

// --- shared encodings ------------------------------------------------

// quiCocoaModsOf reduces NSEventModifierFlags to the 4-bit mask the Go
// side decodes with mapNSMods (shift/control/alt/super).
static int quiCocoaModsOf(NSEventModifierFlags flags) {
    int mods = 0;
    if (flags & NSEventModifierFlagShift)   mods |= 1;
    if (flags & NSEventModifierFlagControl) mods |= 2;
    if (flags & NSEventModifierFlagOption)  mods |= 4;
    if (flags & NSEventModifierFlagCommand) mods |= 8;
    return mods;
}

// quiCocoaPhaseOf collapses the NSEventPhase bitmask into the small
// integer mapNSGesturePhase decodes. Momentum is reported separately by
// AppKit and folded in by the caller.
static int quiCocoaPhaseOf(NSEventPhase phase) {
    if (phase & NSEventPhaseBegan)     return 1;
    if (phase & NSEventPhaseChanged)   return 2;
    if (phase & NSEventPhaseEnded)     return 3;
    if (phase & NSEventPhaseCancelled) return 4;
    return 0;
}

static NSCursor* quiCocoaCursorFor(int shape) {
    switch (shape) {
        case 1:  return [NSCursor IBeamCursor];
        case 2:  return [NSCursor crosshairCursor];
        case 3:  return [NSCursor pointingHandCursor];
        case 4:  return [NSCursor resizeLeftRightCursor];
        case 5:  return [NSCursor resizeUpDownCursor];
        default: return [NSCursor arrowCursor];
    }
}

// --- view ------------------------------------------------------------

@interface QuiView : NSView <NSTextInputClient>
@property (nonatomic, assign) uintptr_t handle;
@property (nonatomic, assign) int cursorShape;
@end

@implementation QuiView

// Flipped so view coordinates are top-left origin, matching qui's
// coordinate space. Every mouse position below is then a straight
// convertPoint: with no per-event Y arithmetic to get wrong.
- (BOOL)isFlipped          { return YES; }
- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)canBecomeKeyView   { return YES; }
- (BOOL)isOpaque           { return YES; }
// Clicking an inactive window should both activate it AND deliver the
// click, so a user can press a button in one motion.
- (BOOL)acceptsFirstMouse:(NSEvent*)event { return YES; }

- (void)resetCursorRects {
    [self addCursorRect:[self bounds] cursor:quiCocoaCursorFor(self.cursorShape)];
}

- (NSPoint)quiLocation:(NSEvent*)event {
    return [self convertPoint:[event locationInWindow] fromView:nil];
}

// --- mouse ---

- (void)mouseMoved:(NSEvent*)event {
    NSPoint p = [self quiLocation:event];
    quiCocoaOnMouseMove(self.handle, p.x, p.y, quiCocoaModsOf([event modifierFlags]));
}
// Dragged events are motion too: qui tracks its own mouse capture, so it
// needs positions while a button is held exactly as when it isn't.
- (void)mouseDragged:(NSEvent*)event       { [self mouseMoved:event]; }
- (void)rightMouseDragged:(NSEvent*)event  { [self mouseMoved:event]; }
- (void)otherMouseDragged:(NSEvent*)event  { [self mouseMoved:event]; }

- (void)quiButton:(NSEvent*)event index:(int)btn down:(int)down {
    quiCocoaOnMouseButton(self.handle, btn, down, quiCocoaModsOf([event modifierFlags]));
}
- (void)mouseDown:(NSEvent*)event       { [self quiButton:event index:0 down:1]; }
- (void)mouseUp:(NSEvent*)event         { [self quiButton:event index:0 down:0]; }
- (void)rightMouseDown:(NSEvent*)event  { [self quiButton:event index:1 down:1]; }
- (void)rightMouseUp:(NSEvent*)event    { [self quiButton:event index:1 down:0]; }
- (void)otherMouseDown:(NSEvent*)event  { [self quiButton:event index:2 down:1]; }
- (void)otherMouseUp:(NSEvent*)event    { [self quiButton:event index:2 down:0]; }

// --- scroll ---

- (void)scrollWheel:(NSEvent*)event {
    double dx = [event scrollingDeltaX];
    double dy = [event scrollingDeltaY];
    // Precise (trackpad / Magic Mouse) deltas are in points; line-based
    // (wheel) deltas are in lines. Scale the former down by the same
    // 0.1 factor GLFW uses so scroll distance per gesture is unchanged
    // between the two backends.
    if ([event hasPreciseScrollingDeltas]) {
        dx *= 0.1;
        dy *= 0.1;
    }
    if (dx == 0.0 && dy == 0.0) return;

    int phase = quiCocoaPhaseOf([event phase]);
    // A trackpad flick continues to emit scroll events after the fingers
    // lift; those carry momentumPhase instead of phase. Reporting them as
    // a distinct phase is what lets a widget tell a deliberate scroll
    // from inertia.
    if (phase == 0 && [event momentumPhase] != NSEventPhaseNone) phase = 5;

    NSPoint p = [self quiLocation:event];
    quiCocoaOnScroll(self.handle, p.x, p.y, dx, dy,
                     quiCocoaModsOf([event modifierFlags]), phase);
}

// --- gestures ---

- (void)magnifyWithEvent:(NSEvent*)event {
    quiCocoaOnGesture(self.handle, 0, quiCocoaPhaseOf([event phase]),
                      (float)[event magnification], 0.0f,
                      quiCocoaModsOf([event modifierFlags]));
}

- (void)rotateWithEvent:(NSEvent*)event {
    quiCocoaOnGesture(self.handle, 1, quiCocoaPhaseOf([event phase]),
                      0.0f, (float)[event rotation],
                      quiCocoaModsOf([event modifierFlags]));
}

- (void)smartMagnifyWithEvent:(NSEvent*)event {
    quiCocoaOnGesture(self.handle, 2, 0, 0.0f, 0.0f,
                      quiCocoaModsOf([event modifierFlags]));
}

// --- keyboard ---

- (void)keyDown:(NSEvent*)event {
    // The virtual keycode goes to Go untranslated: the macOS keycode →
    // Key table is data, and keeping it on the Go side makes it directly
    // testable instead of only observable through a running window.
    quiCocoaOnKey(self.handle, (int)[event keyCode], 1,
                  [event isARepeat] ? 1 : 0,
                  quiCocoaModsOf([event modifierFlags]));
    // Order matters and mirrors GLFW: the key event reaches the widget
    // tree first, then the input method gets a chance to turn it into
    // composition or committed text. ime_darwin.m's keyDown: wrapper
    // observes what the IME did afterwards and depends on this shape.
    [self interpretKeyEvents:@[event]];
}

- (void)keyUp:(NSEvent*)event {
    quiCocoaOnKey(self.handle, (int)[event keyCode], 0, 0,
                  quiCocoaModsOf([event modifierFlags]));
}

// --- NSTextInputClient ---
//
// These are intentionally minimal: the real IME behavior is installed
// over them by ime_darwin.m, exactly as it is over GLFW's versions. What
// must be right here is that the selectors EXIST on this class (so
// class_addMethod has a type encoding to copy) and that insertText:
// still produces character input when no IME is active.

- (void)insertText:(id)string replacementRange:(NSRange)replacementRange {
    NSString* plain = [string isKindOfClass:[NSAttributedString class]]
                          ? [(NSAttributedString*)string string]
                          : (NSString*)string;
    if (plain == nil) return;
    NSUInteger len = [plain length];
    for (NSUInteger i = 0; i < len; i++) {
        unichar unit = [plain characterAtIndex:i];
        uint32_t cp = unit;
        // Recombine surrogate pairs so astral-plane characters (emoji)
        // arrive as one rune rather than two broken halves.
        if (unit >= 0xD800 && unit <= 0xDBFF && i + 1 < len) {
            unichar low = [plain characterAtIndex:i + 1];
            if (low >= 0xDC00 && low <= 0xDFFF) {
                cp = 0x10000 + ((unit - 0xD800) << 10) + (low - 0xDC00);
                i++;
            }
        }
        // Control characters are key presses, not text; the key path
        // already delivered them.
        if (cp < 0x20 || cp == 0x7F) continue;
        quiCocoaOnChar(self.handle, cp);
    }
}

- (void)setMarkedText:(id)string
        selectedRange:(NSRange)selectedRange
     replacementRange:(NSRange)replacementRange {}
- (void)unmarkText {}
- (BOOL)hasMarkedText { return NO; }
- (NSRange)markedRange { return NSMakeRange(NSNotFound, 0); }
- (NSRange)selectedRange { return NSMakeRange(NSNotFound, 0); }
- (NSAttributedString*)attributedSubstringForProposedRange:(NSRange)range
                                               actualRange:(NSRangePointer)actualRange { return nil; }
- (NSArray<NSAttributedStringKey>*)validAttributesForMarkedText { return @[]; }
- (NSRect)firstRectForCharacterRange:(NSRange)range actualRange:(NSRangePointer)actualRange {
    return NSZeroRect;
}
- (NSUInteger)characterIndexForPoint:(NSPoint)point { return NSNotFound; }
// NSResponder's default beeps at every unhandled command. qui routes key
// handling itself, so swallow these silently — same as GLFW does.
- (void)doCommandBySelector:(SEL)selector {}

// --- file drop ---

- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
    return NSDragOperationCopy;
}

- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
    NSPasteboard* pb = [sender draggingPasteboard];
    NSArray* urls = [pb readObjectsForClasses:@[[NSURL class]]
                                      options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
    if ([urls count] == 0) return NO;
    for (NSURL* url in urls) {
        const char* path = [[url path] UTF8String];
        if (path) quiCocoaOnDropPath(self.handle, (char*)path);
    }
    NSPoint p = [self convertPoint:[sender draggingLocation] fromView:nil];
    quiCocoaOnDropCommit(self.handle, p.x, p.y);
    return YES;
}

@end

// --- window delegate --------------------------------------------------

@interface QuiWindowDelegate : NSObject <NSWindowDelegate>
@property (nonatomic, assign) uintptr_t handle;
@end

// QuiWindowBox owns one window's AppKit objects for the lifetime of the
// handle. A struct-of-objects rather than an NSWindow subclass so the
// window stays a plain NSWindow — several native bridges (menus,
// fullscreen, IME) reach it via quiCocoaNSWindow and shouldn't have to
// know about a custom class.
@interface QuiWindowBox : NSObject
@property (nonatomic, strong) NSWindow* window;
@property (nonatomic, strong) QuiView* view;
@property (nonatomic, strong) QuiWindowDelegate* delegate;
@property (nonatomic, strong) NSOpenGLContext* glContext;
@property (nonatomic, assign) BOOL shouldClose;
@end

@implementation QuiWindowBox
@end

static NSMutableDictionary<NSNumber*, QuiWindowBox*>* gQuiWindows = nil;

static QuiWindowBox* quiBox(uintptr_t handle) {
    if (gQuiWindows == nil) return nil;
    return gQuiWindows[@((unsigned long)handle)];
}

// quiCocoaNotifyGeometry reports the window's logical and physical sizes
// and asks for a repaint. Called from every path that can change either
// (interactive resize, zoom, a move to a display with a different
// backing scale) because AppKit has no single "geometry changed" hook and
// the two sizes change independently.
static void quiCocoaNotifyGeometry(QuiWindowBox* box, uintptr_t handle) {
    if (box == nil) return;
    NSRect bounds = [box.view bounds];
    NSRect backing = [box.view convertRectToBacking:bounds];
    if (box.glContext) [box.glContext update];
    quiCocoaOnResize(handle, (int)bounds.size.width, (int)bounds.size.height);
    quiCocoaOnFramebufferResize(handle, (int)backing.size.width, (int)backing.size.height);
    quiCocoaOnRefresh(handle);
}

@implementation QuiWindowDelegate

// Refuse the close and record the request instead. qui's main loop polls
// shouldClose, runs OnClose handlers while the window is still alive, and
// destroys it deliberately — letting AppKit close the window here would
// pull the GL context out from under that teardown.
- (BOOL)windowShouldClose:(NSWindow*)sender {
    QuiWindowBox* box = quiBox(self.handle);
    if (box) box.shouldClose = YES;
    quiCocoaWake();
    return NO;
}

// Resize runs inside a nested AppKit event-tracking loop, so qui's own
// loop is not running: the notify path renders synchronously, which is
// what keeps content live under the cursor during a drag.
- (void)windowDidResize:(NSNotification*)note {
    quiCocoaNotifyGeometry(quiBox(self.handle), self.handle);
}

- (void)windowDidChangeScreen:(NSNotification*)note {
    quiCocoaNotifyGeometry(quiBox(self.handle), self.handle);
}

// Moving between displays of different scale changes the framebuffer
// size while the logical size stays put.
- (void)windowDidChangeBackingProperties:(NSNotification*)note {
    quiCocoaNotifyGeometry(quiBox(self.handle), self.handle);
}

- (void)windowDidBecomeKey:(NSNotification*)note {
    quiCocoaOnFocus(self.handle, 1);
}

- (void)windowDidResignKey:(NSNotification*)note {
    quiCocoaOnFocus(self.handle, 0);
}

- (void)windowDidExpose:(NSNotification*)note {
    quiCocoaOnRefresh(self.handle);
}

// State the loop polls (OnMove / OnMinimize, fullscreen) can change from
// inside an idle wait with no input event of its own — a Dock minimize, a
// fullscreen animation settling. Cut the wait short so the loop notices.
- (void)windowDidMove:(NSNotification*)note { quiCocoaOnWindowState(); }
- (void)windowDidMiniaturize:(NSNotification*)note { quiCocoaOnWindowState(); }
- (void)windowDidDeminiaturize:(NSNotification*)note { quiCocoaOnWindowState(); }
- (void)windowDidEnterFullScreen:(NSNotification*)note { quiCocoaOnWindowState(); }
- (void)windowDidExitFullScreen:(NSNotification*)note { quiCocoaOnWindowState(); }

@end

// --- application -----------------------------------------------------

// quiCocoaMinimalMenu installs an app menu with Quit if the process has
// none. Without a main menu the standard Cmd+Q / Cmd+H / Hide Others do
// nothing, which reads as a broken app. Apps that build real menus
// (menu_native_darwin.m) replace this wholesale.
static void quiCocoaMinimalMenu(void) {
    if ([NSApp mainMenu] != nil) return;
    NSMenu* bar = [[NSMenu alloc] init];
    NSMenuItem* appItem = [[NSMenuItem alloc] init];
    [bar addItem:appItem];
    NSMenu* appMenu = [[NSMenu alloc] init];
    NSString* name = [[NSProcessInfo processInfo] processName];
    [appMenu addItemWithTitle:[@"Hide " stringByAppendingString:name]
                       action:@selector(hide:)
                keyEquivalent:@"h"];
    [appMenu addItem:[NSMenuItem separatorItem]];
    [appMenu addItemWithTitle:[@"Quit " stringByAppendingString:name]
                       action:@selector(terminate:)
                keyEquivalent:@"q"];
    [appItem setSubmenu:appMenu];
    [NSApp setMainMenu:bar];
}

// quiCocoaInit initializes NSApplication. policy mirrors
// qui.AppActivationPolicy: 0 regular, 1 accessory, 2 prohibited.
//
// A prohibited/accessory process must NOT activate itself. For an input
// method that is not cosmetic: activating would pull focus off the app the
// user is typing into at the exact moment the input method loads.
int quiCocoaInit(int policy) {
    @autoreleasepool {
        gQuiWindows = [[NSMutableDictionary alloc] init];
        [NSApplication sharedApplication];
        NSApplicationActivationPolicy nsPolicy;
        switch (policy) {
            case 1:  nsPolicy = NSApplicationActivationPolicyAccessory;  break;
            case 2:  nsPolicy = NSApplicationActivationPolicyProhibited; break;
            default: nsPolicy = NSApplicationActivationPolicyRegular;    break;
        }
        [NSApp setActivationPolicy:nsPolicy];
        if (policy == 0) {
            // The app menu only makes sense for a regular app; a
            // background process has no menu bar to put it in.
            quiCocoaMinimalMenu();
        }
        // finishLaunching is mandatory when driving the event loop by
        // hand: without it AppKit never completes activation and the
        // first window can come up unable to take key events.
        [NSApp finishLaunching];
        if (policy == 0) {
            [NSApp activateIgnoringOtherApps:YES];
        }
        return 1;
    }
}

void quiCocoaPump(double seconds) {
    @autoreleasepool {
        // < 0 waits for an event, 0 polls, > 0 waits at most that long.
        NSDate* until = seconds < 0 ? [NSDate distantFuture]
            : seconds > 0 ? [NSDate dateWithTimeIntervalSinceNow:seconds]
            : [NSDate distantPast];
        for (;;) {
            NSEvent* event = [NSApp nextEventMatchingMask:NSEventMaskAny
                                                untilDate:until
                                                   inMode:NSDefaultRunLoopMode
                                                  dequeue:YES];
            if (event == nil) break;
            [NSApp sendEvent:event];
            // Block at most once per pump; drain whatever else is already
            // queued so a burst of motion events doesn't cost a frame each.
            until = [NSDate distantPast];
        }
        [NSApp updateWindows];
    }
}

// quiCocoaWake is called from arbitrary goroutines (PostJob). postEvent:
// is documented thread-safe and is the same mechanism GLFW's
// PostEmptyEvent uses.
void quiCocoaWake(void) {
    @autoreleasepool {
        NSEvent* event = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                            location:NSZeroPoint
                                       modifierFlags:0
                                           timestamp:0
                                        windowNumber:0
                                             context:nil
                                             subtype:0
                                               data1:0
                                               data2:0];
        [NSApp postEvent:event atStart:YES];
    }
}

// --- window creation -------------------------------------------------

// quiCocoaCreateWindow builds the NSWindow (kind 0) or the NSPanel that
// backs qui.WindowOverlayPanel (kind 1).
//
// The panel is deliberately an NSPanel and not a borderless NSWindow:
// NSWindowStyleMaskNonactivatingPanel — the property that keeps the app the
// user is typing into active when the panel is clicked — is only honored on
// NSPanel. Everything else about the panel (level, collection behavior,
// opacity) is a plain property, but that one is a class requirement.
int quiCocoaCreateWindow(uintptr_t handle, int width, int height,
                         const char* title, uintptr_t shareHandle,
                         int kind) {
    @autoreleasepool {
        const BOOL overlay = (kind == 1);
        NSRect rect = NSMakeRect(0, 0, width, height);
        NSWindow* window = nil;
        if (overlay) {
            NSPanel* panel = [[NSPanel alloc]
                initWithContentRect:rect
                          styleMask:NSWindowStyleMaskBorderless |
                                    NSWindowStyleMaskNonactivatingPanel
                            backing:NSBackingStoreBuffered
                              defer:NO];
            if (panel == nil) return 0;
            // Float above ordinary windows. PopUpMenu level is what menus
            // and other input methods' candidate windows use — high enough
            // to clear a fullscreen app's content, low enough to stay under
            // the menu bar's own menus.
            [panel setLevel:NSPopUpMenuWindowLevel];
            [panel setFloatingPanel:YES];
            // The panel belongs to no Space in particular and must survive
            // the user switching desktops or entering fullscreen. Without
            // FullScreenAuxiliary it simply does not appear over a
            // fullscreen app, which is where people type most.
            [panel setCollectionBehavior:NSWindowCollectionBehaviorCanJoinAllSpaces |
                                         NSWindowCollectionBehaviorTransient |
                                         NSWindowCollectionBehaviorFullScreenAuxiliary];
            // An input method's process is never the active app, so the
            // default "hide my windows when I'm not frontmost" would hide
            // the candidate bar exactly when it is needed.
            [panel setHidesOnDeactivate:NO];
            // Transparent framebuffer: the widget tree draws its own
            // rounded card, and everything outside it must composite
            // through. hasShadow picks up the alpha shape, so the shadow
            // follows the rounded corners rather than the window rect.
            [panel setOpaque:NO];
            [panel setBackgroundColor:[NSColor clearColor]];
            [panel setHasShadow:YES];
            // Not in the window cycle, not in Exposé, no title.
            [panel setExcludedFromWindowsMenu:YES];
            [panel setBecomesKeyOnlyIfNeeded:YES];
            window = panel;
        } else {
            NSWindowStyleMask style = NSWindowStyleMaskTitled |
                                      NSWindowStyleMaskClosable |
                                      NSWindowStyleMaskMiniaturizable |
                                      NSWindowStyleMaskResizable;
            window = [[NSWindow alloc] initWithContentRect:rect
                                                styleMask:style
                                                  backing:NSBackingStoreBuffered
                                                    defer:NO];
        }
        if (window == nil) return 0;

        QuiView* view = [[QuiView alloc] initWithFrame:rect];
        view.handle = handle;
        view.cursorShape = 0;
        // Opt into the full pixel grid on Retina; without this the GL
        // surface is upscaled from logical size and everything is soft.
        [view setWantsBestResolutionOpenGLSurface:YES];

        QuiWindowDelegate* delegate = [[QuiWindowDelegate alloc] init];
        delegate.handle = handle;

        [window setContentView:view];
        [window setDelegate:delegate];
        [window setTitle:[NSString stringWithUTF8String:title ? title : ""]];
        // Motion without a button held is off by default; qui needs it for
        // hover, cursor shape and tooltip tracking.
        [window setAcceptsMouseMovedEvents:YES];
        [window setReleasedWhenClosed:NO];
        [window makeFirstResponder:view];

        NSOpenGLPixelFormatAttribute attrs[] = {
            NSOpenGLPFAAccelerated,
            NSOpenGLPFADoubleBuffer,
            NSOpenGLPFAClosestPolicy,
            // macOS exposes only 3.2 and 4.1 as core profiles; 4.1 is a
            // superset of the 3.3 qui's shaders target.
            NSOpenGLPFAOpenGLProfile, NSOpenGLProfileVersion4_1Core,
            NSOpenGLPFAColorSize,   24,
            NSOpenGLPFAAlphaSize,    8,
            NSOpenGLPFADepthSize,   24,
            NSOpenGLPFAStencilSize,  8,
            0
        };
        NSOpenGLPixelFormat* format =
            [[NSOpenGLPixelFormat alloc] initWithAttributes:attrs];
        if (format == nil) return 0;

        NSOpenGLContext* share = nil;
        if (shareHandle != 0) {
            QuiWindowBox* other = quiBox(shareHandle);
            if (other) share = other.glContext;
        }
        NSOpenGLContext* context =
            [[NSOpenGLContext alloc] initWithFormat:format shareContext:share];
        if (context == nil) return 0;
        [context setView:view];
        [context makeCurrentContext];
        GLint interval = 1;   // vsync
        [context setValues:&interval
             forParameter:NSOpenGLContextParameterSwapInterval];
        if (overlay) {
            // Without this the GL surface is composited as opaque no matter
            // what alpha the framebuffer holds, and the panel shows up as a
            // black rectangle with the card drawn inside it. The pixel
            // format already requests NSOpenGLPFAAlphaSize 8.
            GLint surfaceOpaque = 0;
            [context setValues:&surfaceOpaque
                 forParameter:NSOpenGLContextParameterSurfaceOpacity];
        }

        QuiWindowBox* box = [[QuiWindowBox alloc] init];
        box.window = window;
        box.view = view;
        box.delegate = delegate;
        box.glContext = context;
        box.shouldClose = NO;
        gQuiWindows[@((unsigned long)handle)] = box;

        if (overlay) {
            // Panels start hidden and are positioned by the app before the
            // first show — see qui.Window.ShowAt. Centering and ordering
            // front here would flash the panel at the wrong place.
            return 1;
        }
        [window center];
        [window makeKeyAndOrderFront:nil];
        return 1;
    }
}

// quiCocoaSetVisible orders a window on or off screen. activate selects
// makeKeyAndOrderFront: (take focus) versus orderFront: (appear without
// disturbing who is active) — the latter is what an overlay panel needs.
void quiCocoaSetVisible(uintptr_t handle, int visible, int activate) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        if (!visible) {
            [box.window orderOut:nil];
            return;
        }
        if (activate) {
            [box.window makeKeyAndOrderFront:nil];
        } else {
            // orderFrontRegardless rather than orderFront: — the latter is
            // a no-op when the calling process is not the active app, which
            // for an input method is always.
            [box.window orderFrontRegardless];
        }
    }
}

int quiCocoaIsVisible(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return 0;
    return [box.window isVisible] ? 1 : 0;
}

void quiCocoaDestroyWindow(uintptr_t handle) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        [box.glContext clearDrawable];
        [box.window setDelegate:nil];
        [box.window orderOut:nil];
        [box.window close];
        [gQuiWindows removeObjectForKey:@((unsigned long)handle)];
    }
}

void* quiCocoaNSWindow(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return NULL;
    // Unretained: every caller (IME install, fullscreen toggle) uses it
    // synchronously while the box still holds the strong reference.
    return (__bridge void*)box.window;
}

// --- geometry --------------------------------------------------------

void quiCocoaGetSize(uintptr_t handle, int* width, int* height) {
    *width = 0; *height = 0;
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return;
    NSRect bounds = [box.view bounds];
    *width  = (int)bounds.size.width;
    *height = (int)bounds.size.height;
}

void quiCocoaGetFramebufferSize(uintptr_t handle, int* width, int* height) {
    *width = 0; *height = 0;
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return;
    NSRect backing = [box.view convertRectToBacking:[box.view bounds]];
    *width  = (int)backing.size.width;
    *height = (int)backing.size.height;
}

double quiCocoaBackingScale(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return 1.0;
    NSWindow* window = box.window;
    return window ? [window backingScaleFactor] : 1.0;
}

// quiCocoaFlipY converts between AppKit's bottom-left screen origin and
// the top-left origin qui (and every other platform in the seam) uses.
// The reference height is the PRIMARY screen's, which is where AppKit
// puts the global origin.
static CGFloat quiCocoaPrimaryHeight(void) {
    NSArray<NSScreen*>* screens = [NSScreen screens];
    if ([screens count] == 0) return 0;
    return NSMaxY([screens[0] frame]);
}

void quiCocoaGetPos(uintptr_t handle, int* x, int* y) {
    *x = 0; *y = 0;
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return;
    NSRect content = [box.window contentRectForFrameRect:[box.window frame]];
    *x = (int)content.origin.x;
    *y = (int)(quiCocoaPrimaryHeight() - content.origin.y - content.size.height);
}

void quiCocoaSetPos(uintptr_t handle, int x, int y) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        NSRect content = [box.window contentRectForFrameRect:[box.window frame]];
        content.origin.x = x;
        content.origin.y = quiCocoaPrimaryHeight() - y - content.size.height;
        NSRect frame = [box.window frameRectForContentRect:content];
        [box.window setFrameOrigin:frame.origin];
    }
}

void quiCocoaSetTitle(uintptr_t handle, const char* title) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        [box.window setTitle:[NSString stringWithUTF8String:title ? title : ""]];
    }
}

// quiCocoaSetSize resizes the content area, pinning the window's TOP edge.
//
// setContentSize: alone keeps the frame ORIGIN — the bottom-left corner —
// so a window that grows taller pushes its top edge upward. For a panel
// anchored under a text caret that means the anchor drifts every time the
// content changes height, which is every keystroke that adds a candidate
// row. Recompute the origin so the top-left is what stays put.
void quiCocoaSetSize(uintptr_t handle, int width, int height) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        NSRect content = [box.window contentRectForFrameRect:[box.window frame]];
        CGFloat topY = content.origin.y + content.size.height;
        content.size = NSMakeSize(width, height);
        content.origin.y = topY - height;
        [box.window setFrame:[box.window frameRectForContentRect:content]
                     display:YES];
    }
}

void quiCocoaSetSizeLimits(uintptr_t handle, int minW, int minH, int maxW, int maxH) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        [box.window setContentMinSize:NSMakeSize(minW > 0 ? minW : 0,
                                                minH > 0 ? minH : 0)];
        [box.window setContentMaxSize:NSMakeSize(maxW > 0 ? maxW : FLT_MAX,
                                                maxH > 0 ? maxH : FLT_MAX)];
    }
}

void quiCocoaIconify(uintptr_t handle) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box) [box.window miniaturize:nil];
    }
}

void quiCocoaMaximize(uintptr_t handle) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box && ![box.window isZoomed]) [box.window zoom:nil];
    }
}

void quiCocoaRestore(uintptr_t handle) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        if ([box.window isMiniaturized]) {
            [box.window deminiaturize:nil];
        } else if ([box.window isZoomed]) {
            [box.window zoom:nil];
        }
    }
}

void quiCocoaFocus(uintptr_t handle) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        [NSApp activateIgnoringOtherApps:YES];
        [box.window makeKeyAndOrderFront:nil];
    }
}

void quiCocoaSetFullscreen(uintptr_t handle, int on, int x, int y, int w, int h) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        BOOL isFullscreen =
            ([box.window styleMask] & NSWindowStyleMaskFullScreen) != 0;
        if ((on != 0) != isFullscreen) {
            [box.window toggleFullScreen:nil];
        }
        if (on == 0 && w > 0 && h > 0) {
            quiCocoaSetPos(handle, x, y);
            [box.window setContentSize:NSMakeSize(w, h)];
        }
    }
}

int quiCocoaShouldClose(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    return (box != nil && box.shouldClose) ? 1 : 0;
}

void quiCocoaSetShouldClose(uintptr_t handle, int value) {
    QuiWindowBox* box = quiBox(handle);
    if (box) box.shouldClose = (value != 0);
}

// --- cursor / input state --------------------------------------------

void quiCocoaGetCursorPos(uintptr_t handle, double* x, double* y) {
    *x = 0; *y = 0;
    QuiWindowBox* box = quiBox(handle);
    if (box == nil) return;
    NSPoint inWindow = [box.window mouseLocationOutsideOfEventStream];
    NSPoint inView = [box.view convertPoint:inWindow fromView:nil];
    *x = inView.x;
    *y = inView.y;
}

void quiCocoaSetCursor(uintptr_t handle, int shape) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        box.view.cursorShape = shape;
        // Set immediately for the current position, and refresh the
        // cursor rect so re-entering the view keeps the new shape.
        [quiCocoaCursorFor(shape) set];
        [box.window invalidateCursorRectsForView:box.view];
    }
}

// quiCocoaSetDropEnabled registers (or unregisters) OS file drops. Opt-in
// rather than always-on so a window that never asked for drops keeps
// showing the "no" cursor over dragged files instead of silently
// accepting them.
void quiCocoaSetDropEnabled(uintptr_t handle, int enabled) {
    @autoreleasepool {
        QuiWindowBox* box = quiBox(handle);
        if (box == nil) return;
        if (enabled) {
            [box.view registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
        } else {
            [box.view unregisterDraggedTypes];
        }
    }
}

int quiCocoaCurrentMods(void) {
    return quiCocoaModsOf([NSEvent modifierFlags]);
}

// --- clipboard -------------------------------------------------------

char* quiCocoaClipboardText(void) {
    @autoreleasepool {
        NSString* text = [[NSPasteboard generalPasteboard]
                             stringForType:NSPasteboardTypeString];
        if (text == nil) return NULL;
        const char* utf8 = [text UTF8String];
        return utf8 ? strdup(utf8) : NULL;
    }
}

void quiCocoaSetClipboardText(const char* text) {
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        if (text == NULL) return;
        [pb setString:[NSString stringWithUTF8String:text]
              forType:NSPasteboardTypeString];
    }
}

// --- presentation ----------------------------------------------------

void quiCocoaMakeCurrent(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    if (box) [box.glContext makeCurrentContext];
}

void quiCocoaPresent(uintptr_t handle) {
    QuiWindowBox* box = quiBox(handle);
    if (box) [box.glContext flushBuffer];
}

// --- displays --------------------------------------------------------

int quiCocoaScreenCount(void) {
    return (int)[[NSScreen screens] count];
}

static NSScreen* quiCocoaScreenAt(int index) {
    NSArray<NSScreen*>* screens = [NSScreen screens];
    if (index < 0 || index >= (int)[screens count]) return nil;
    return screens[index];
}

// quiCocoaScreenInfo fills one display's geometry, converted to the
// top-left-origin logical space the seam uses.
void quiCocoaScreenInfo(int index,
                        int* x, int* y, int* w, int* h,
                        int* wx, int* wy, int* ww, int* wh,
                        double* scale, int* refreshHz) {
    *x = 0; *y = 0; *w = 0; *h = 0;
    *wx = 0; *wy = 0; *ww = 0; *wh = 0;
    *scale = 1.0; *refreshHz = 0;
    @autoreleasepool {
        NSScreen* screen = quiCocoaScreenAt(index);
        if (screen == nil) return;
        CGFloat primaryH = quiCocoaPrimaryHeight();

        NSRect frame = [screen frame];
        *x = (int)frame.origin.x;
        *y = (int)(primaryH - frame.origin.y - frame.size.height);
        *w = (int)frame.size.width;
        *h = (int)frame.size.height;

        NSRect visible = [screen visibleFrame];
        *wx = (int)visible.origin.x;
        *wy = (int)(primaryH - visible.origin.y - visible.size.height);
        *ww = (int)visible.size.width;
        *wh = (int)visible.size.height;

        *scale = [screen backingScaleFactor];
        if (@available(macOS 12.0, *)) {
            *refreshHz = (int)[screen maximumFramesPerSecond];
        }
    }
}

char* quiCocoaScreenName(int index) {
    @autoreleasepool {
        NSScreen* screen = quiCocoaScreenAt(index);
        if (screen == nil) return NULL;
        const char* utf8 = [[screen localizedName] UTF8String];
        return utf8 ? strdup(utf8) : NULL;
    }
}

#pragma clang diagnostic pop
