//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreText -framework CoreGraphics
#include <stdint.h>
#include <stdlib.h>

// Rasterize a UTF-8 string with Apple Color Emoji via Core Text into a
// malloc'd premultiplied RGBA buffer (top-left origin). targetWidth > 0
// fits the glyph to exactly that pixel width.
void quiRenderEmoji(const char* utf8, double fontSize, int targetWidth,
                    uint8_t** outPixels, int* outW, int* outH);
*/
import "C"

import "unsafe"

// RenderEmoji rasterizes one emoji cluster with Core Text, fitted to
// targetWidth pixels. It returns premultiplied RGBA rows, or nil.
func RenderEmoji(seq string, fontSize float64, targetWidth int) (pix []byte, w, h int) {
	cstr, free := cStr(seq)
	defer free()
	var pixels *C.uint8_t
	var cw, ch C.int
	C.quiRenderEmoji(cstr, C.double(fontSize), C.int(targetWidth), &pixels, &cw, &ch)
	if pixels == nil || cw == 0 || ch == 0 {
		return nil, 0, 0
	}
	defer C.free(unsafe.Pointer(pixels))
	w, h = int(cw), int(ch)
	return C.GoBytes(unsafe.Pointer(pixels), C.int(w*h*4)), w, h
}
