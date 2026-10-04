//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

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

func platformSetTitlebarStyle(ns unsafe.Pointer, s TitlebarStyle) bool {
	on := C.int(0)
	if s == TitlebarOverlay {
		on = 1
	}
	C.quiTitlebarSetOverlay(ns, on)
	return true
}

func platformTitlebarInsets(ns unsafe.Pointer) Insets {
	var left, right, top C.double
	C.quiTitlebarInsets(ns, &left, &right, &top)
	return Insets{Top: float32(top), Left: float32(left), Right: float32(right)}
}

func platformBeginWindowDrag(ns unsafe.Pointer) bool {
	return C.quiWindowBeginDrag(ns) != 0
}

func platformToggleMaximize(ns unsafe.Pointer) bool {
	C.quiWindowToggleZoom(ns)
	return true
}
