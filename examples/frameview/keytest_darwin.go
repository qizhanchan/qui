//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>

// postKey queues a synthetic key event for the key window, exactly as if
// it came from the keyboard: it goes through -[NSApplication sendEvent:],
// local monitors and menu key-equivalent matching.
static void postKey(int keyCode, const char* chars, unsigned long flags, int down) {
    dispatch_async(dispatch_get_main_queue(), ^{
        NSWindow* win = [NSApp keyWindow];
        if (win == nil) win = [[NSApp windows] firstObject];
        NSString* s = [NSString stringWithUTF8String:chars];
        NSEvent* ev = [NSEvent keyEventWithType:(down ? NSEventTypeKeyDown : NSEventTypeKeyUp)
                                       location:NSZeroPoint
                                  modifierFlags:flags
                                      timestamp:[[NSProcessInfo processInfo] systemUptime]
                                   windowNumber:[win windowNumber]
                                        context:nil
                                     characters:s
                    charactersIgnoringModifiers:s
                                      isARepeat:NO
                                        keyCode:(unsigned short)keyCode];
        [NSApp postEvent:ev atStart:NO];
    });
}
*/
import "C"

import "unsafe"

const (
	nsCmd  = 1 << 20
	nsCtrl = 1 << 18
	nsAlt  = 1 << 19
)

// postSyntheticKey posts one key event through AppKit (FRAMEVIEW_KEYTEST).
func postSyntheticKey(keyCode int, chars string, flags uint, down bool) bool {
	c := C.CString(chars)
	defer C.free(unsafe.Pointer(c))
	d := C.int(0)
	if down {
		d = 1
	}
	C.postKey(C.int(keyCode), c, C.ulong(flags), d)
	return true
}
