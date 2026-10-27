//go:build darwin && cgo

package macos

/*
#import <Cocoa/Cocoa.h>

static int quiWindowIsFullscreen(void* nsWindowPtr) {
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	return ([win styleMask] & NSWindowStyleMaskFullScreen) ? 1 : 0;
}

// quiWindowSetFullscreen toggles native fullscreen toward the desired
// state. toggleFullScreen: enters/exits a real macOS fullscreen Space, so
// the auto-hidden menu bar and window traffic-light controls remain
// reachable by moving the pointer to the top of the screen — unlike GLFW's
// monitor-fullscreen, which hides them entirely.
static void quiWindowSetFullscreen(void* nsWindowPtr, int on) {
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	int isFs = ([win styleMask] & NSWindowStyleMaskFullScreen) ? 1 : 0;
	if (isFs != on) {
		[win toggleFullScreen:nil];
	}
}

static int quiNativeMiniaturized(void* p) {
	NSWindow* win = (__bridge NSWindow*)p;
	return [win isMiniaturized] ? 1 : 0;
}

static void quiNativeAddChild(void* parent, void* child) {
	NSWindow* pw = (__bridge NSWindow*)parent;
	NSWindow* cw = (__bridge NSWindow*)child;
	[pw addChildWindow:cw ordered:NSWindowAbove];
}

static void quiNativeRemoveChild(void* parent, void* child) {
	NSWindow* pw = (__bridge NSWindow*)parent;
	NSWindow* cw = (__bridge NSWindow*)child;
	if ([cw parentWindow] == pw) {
		[pw removeChildWindow:cw];
	}
}

static void quiNativeBeginSheet(void* parent, void* child) {
	NSWindow* pw = (__bridge NSWindow*)parent;
	NSWindow* cw = (__bridge NSWindow*)child;
	[pw beginSheet:cw completionHandler:nil];
}

static void quiNativeEndSheet(void* parent, void* child) {
	NSWindow* pw = (__bridge NSWindow*)parent;
	NSWindow* cw = (__bridge NSWindow*)child;
	if ([cw sheetParent] == pw) {
		[pw endSheet:cw];
	}
}

// quiTitlebarSetOverlay switches the window between a native title bar and
// a transparent one over full-height content.
//
// FullSizeContentView (rather than a borderless window) is what keeps the
// deal cheap: the window frame still exists, so resize edges, the traffic
// lights, native fullscreen and window snapping all keep working — only the
// title bar's drawing and its title text go away. A borderless window would
// hand us all of that to reimplement.
static void quiTitlebarSetOverlay(void* nsWindowPtr, int on) {
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	if (win == nil) {
		return;
	}
	NSWindowStyleMask mask = [win styleMask];
	if (on) {
		mask |= NSWindowStyleMaskFullSizeContentView;
	} else {
		mask &= ~NSWindowStyleMaskFullSizeContentView;
	}
	[win setStyleMask:mask];
	[win setTitlebarAppearsTransparent:on ? YES : NO];
	[win setTitleVisibility:on ? NSWindowTitleHidden : NSWindowTitleVisible];
	// Explicitly OFF: with movableByWindowBackground a drag anywhere in the
	// content moves the window, which would make every empty area of an app
	// a title bar. Drag regions are opt-in via performWindowDragWithEvent.
	[win setMovableByWindowBackground:NO];
}

// quiTitlebarInsets measures the room the system controls need.
//
// The title bar height is derived from the style mask rather than from the
// live frame: with FullSizeContentView the content rect IS the frame, so
// subtracting them would report zero. Asking AppKit what the content rect
// would be without that one flag gives the real strip height (28pt today,
// but it has changed across releases and differs in fullscreen).
static void quiTitlebarInsets(void* nsWindowPtr, double* left, double* right, double* top) {
	*left = 0;
	*right = 0;
	*top = 0;
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	if (win == nil) {
		return;
	}
	NSRect frame = [win frame];
	NSWindowStyleMask mask = [win styleMask] & ~NSWindowStyleMaskFullSizeContentView;
	NSRect probe = NSMakeRect(0, 0, frame.size.width, frame.size.height);
	NSRect content = [NSWindow contentRectForFrameRect:probe styleMask:mask];
	double h = frame.size.height - content.size.height;
	if (h < 0) {
		h = 0;
	}
	*top = h;

	// Traffic lights sit at the left. Reserve up to their right edge plus a
	// gap; a window whose buttons are hidden reserves nothing.
	NSWindowButton buttons[3] = {
		NSWindowCloseButton, NSWindowMiniaturizeButton, NSWindowZoomButton};
	double maxX = 0;
	for (int i = 0; i < 3; i++) {
		NSButton* btn = [win standardWindowButton:buttons[i]];
		if (btn == nil || [btn isHidden]) {
			continue;
		}
		NSRect r = [btn frame];
		if (NSMaxX(r) > maxX) {
			maxX = NSMaxX(r);
		}
	}
	if (maxX > 0) {
		*left = maxX + 8;
	}
}

// quiWindowBeginDrag hands the in-flight press to AppKit's own window drag,
// which brings snapping, multi-display handling and double-click-to-zoom
// with it. Returns 0 when the current event is not a press we can hand over
// (called outside a mouse-down handler, say), so the caller can fall back.
static int quiWindowBeginDrag(void* nsWindowPtr) {
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	if (win == nil) {
		return 0;
	}
	NSEvent* ev = [NSApp currentEvent];
	if (ev == nil) {
		return 0;
	}
	NSEventType t = [ev type];
	if (t != NSEventTypeLeftMouseDown && t != NSEventTypeLeftMouseDragged) {
		return 0;
	}
	if (![win respondsToSelector:@selector(performWindowDragWithEvent:)]) {
		return 0;
	}
	[win performWindowDragWithEvent:ev];
	return 1;
}

static void quiWindowToggleZoom(void* nsWindowPtr) {
	NSWindow* win = (__bridge NSWindow*)nsWindowPtr;
	if (win == nil) {
		return;
	}
	[win zoom:nil];
}
*/
import "C"

import "unsafe"

// NSWindow chrome. Every function takes the NSWindow pointer the platform
// window reports; callers check it is non-nil.

// WindowIsFullscreen reports whether the window is in a fullscreen Space.
func WindowIsFullscreen(ns unsafe.Pointer) bool { return C.quiWindowIsFullscreen(ns) != 0 }

// WindowSetFullscreen toggles native fullscreen toward on.
func WindowSetFullscreen(ns unsafe.Pointer, on bool) { C.quiWindowSetFullscreen(ns, cBool(on)) }

// WindowMiniaturized reports whether the window is minimized to the Dock.
func WindowMiniaturized(ns unsafe.Pointer) bool { return C.quiNativeMiniaturized(ns) != 0 }

// WindowAddChild attaches child above parent.
func WindowAddChild(parent, child unsafe.Pointer) { C.quiNativeAddChild(parent, child) }

// WindowRemoveChild detaches child if parent owns it.
func WindowRemoveChild(parent, child unsafe.Pointer) { C.quiNativeRemoveChild(parent, child) }

// WindowBeginSheet presents child as a sheet on parent.
func WindowBeginSheet(parent, child unsafe.Pointer) { C.quiNativeBeginSheet(parent, child) }

// WindowEndSheet dismisses child if it is parent's sheet.
func WindowEndSheet(parent, child unsafe.Pointer) { C.quiNativeEndSheet(parent, child) }

// TitlebarSetOverlay switches between the native title bar and a
// transparent one over full-height content.
func TitlebarSetOverlay(ns unsafe.Pointer, on bool) { C.quiTitlebarSetOverlay(ns, cBool(on)) }

// TitlebarInsets returns the room the system title bar controls need.
func TitlebarInsets(ns unsafe.Pointer) (left, right, top float64) {
	var l, r, t C.double
	C.quiTitlebarInsets(ns, &l, &r, &t)
	return float64(l), float64(r), float64(t)
}

// WindowBeginDrag hands the in-flight press to AppKit's window drag; false
// when the current event is not a press it can take.
func WindowBeginDrag(ns unsafe.Pointer) bool { return C.quiWindowBeginDrag(ns) != 0 }

// WindowToggleZoom zooms or unzooms the window.
func WindowToggleZoom(ns unsafe.Pointer) { C.quiWindowToggleZoom(ns) }
