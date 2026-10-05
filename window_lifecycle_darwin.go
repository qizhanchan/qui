//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

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
*/
import "C"

import "unsafe"

func nativeWindowMinimized(ns unsafe.Pointer) bool {
	return ns != nil && C.quiNativeMiniaturized(ns) != 0
}

func nativeAttachChild(parent, child unsafe.Pointer) bool {
	if parent == nil || child == nil {
		return false
	}
	C.quiNativeAddChild(parent, child)
	return true
}

func nativeDetachChild(parent, child unsafe.Pointer) {
	if parent != nil && child != nil {
		C.quiNativeRemoveChild(parent, child)
	}
}

func nativeBeginSheet(parent, child unsafe.Pointer) bool {
	if parent == nil || child == nil {
		return false
	}
	C.quiNativeBeginSheet(parent, child)
	return true
}

func nativeEndSheet(parent, child unsafe.Pointer) {
	if parent != nil && child != nil {
		C.quiNativeEndSheet(parent, child)
	}
}
