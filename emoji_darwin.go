//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework CoreText -framework CoreGraphics

#include <stdint.h>
#include <stdlib.h>

// Rasterize a UTF-8 string using Apple Color Emoji via Core Text.
// Returns: malloc'd RGBA pixel buffer (caller must free) + width/height.
// *outPixels set to NULL on failure. Pixel format is RGBA8888
// PREMULTIPLIED alpha, top-left origin.
// targetWidth > 0 fits the glyph to exactly that pixel width.
void quiRenderEmoji(const char* utf8, double fontSize, int targetWidth,
                    uint8_t** outPixels, int* outW, int* outH);
*/
import "C"

import (
	"image"
	"runtime"
	"unsafe"
)

// coreTextEmojiProvider uses macOS Core Text with Apple Color Emoji
// to rasterize emoji sequences into RGBA bitmaps. Handles the SBIX
// (PNG-embedded) color font format that Go's opentype can't read.
//
// Core Text shapes the whole input string as one paragraph, so emoji
// clusters that arrive as multi-rune UTF-8 (base + skin-tone modifier,
// regional-indicator flag pairs, ZWJ family/profession sequences,
// keycap suffixes) come back as the single combined glyph rather than
// per-component squares. The caller is responsible for splitting text
// into clusters before calling this — see EmojiClusterEnd in emoji.go.
type coreTextEmojiProvider struct{}

// EmojiImage rasterizes a UTF-8 emoji cluster via Core Text, already
// fitted to round(fontSize) pixels wide so normalizeEmojiBitmap passes
// it through without a second (blurring) resample.
func (coreTextEmojiProvider) EmojiImage(seq string, fontSize float32) image.Image {
	cstr := C.CString(seq)
	defer C.free(unsafe.Pointer(cstr))

	var pixels *C.uint8_t
	var w, h C.int
	target := int(fontSize + 0.5)
	if target < 1 {
		target = 1
	}
	C.quiRenderEmoji(cstr, C.double(fontSize), C.int(target), &pixels, &w, &h)
	if pixels == nil || w == 0 || h == 0 {
		return nil
	}
	defer C.free(unsafe.Pointer(pixels))

	iw, ih := int(w), int(h)
	img := image.NewRGBA(image.Rect(0, 0, iw, ih))
	// Copy into Go-managed memory. Core Text produced premultiplied
	// RGBA which matches Go's image.RGBA format — direct copy is OK.
	src := unsafe.Slice((*byte)(unsafe.Pointer(pixels)), iw*ih*4)
	copy(img.Pix, src)
	return img
}

// Auto-install the Core Text provider at package init. Callers can
// override with SetEmojiProvider(nil) to disable, or SetEmojiProvider(x)
// to swap in a custom (Twemoji atlas, etc.) implementation.
func init() {
	if runtime.GOOS == "darwin" {
		SetEmojiProvider(coreTextEmojiProvider{})
	}
}
