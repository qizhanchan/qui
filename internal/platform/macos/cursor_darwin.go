//go:build darwin && cgo

package macos

/*
#include <stdint.h>
void* quiCursorCreate(const uint8_t* rgba, int w, int h, double scale, double hotX, double hotY);
void  quiCursorRelease(void* cursor);
*/
import "C"

import "unsafe"

// CursorCreate builds an NSCursor from premultiplied RGBA pixels; nil on
// failure. Release it with CursorRelease.
func CursorCreate(premulRGBA []byte, w, h int, scale, hotX, hotY float64) unsafe.Pointer {
	if len(premulRGBA) == 0 {
		return nil
	}
	return C.quiCursorCreate((*C.uint8_t)(unsafe.Pointer(&premulRGBA[0])), C.int(w), C.int(h),
		C.double(scale), C.double(hotX), C.double(hotY))
}

// CursorRelease frees a cursor from CursorCreate.
func CursorRelease(cursor unsafe.Pointer) { C.quiCursorRelease(cursor) }
