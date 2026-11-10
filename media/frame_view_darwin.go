//go:build darwin && cgo

package media

/*
#include <stdint.h>
#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <CoreVideo/CoreVideo.h>

static void qfvRetain(void* pb) { CFRetain((CFTypeRef)pb); }
static void qfvRelease(void* pb) { CFRelease((CFTypeRef)pb); }
static long qfvRetainCount(void* pb) { return (long)CFGetRetainCount((CFTypeRef)pb); }

static int qfvIsPixelBuffer(void* pb) {
    return CFGetTypeID((CFTypeRef)pb) == CVPixelBufferGetTypeID();
}

static void qfvSize(void* pb, int* w, int* h) {
    *w = (int)CVPixelBufferGetWidth((CVPixelBufferRef)pb);
    *h = (int)CVPixelBufferGetHeight((CVPixelBufferRef)pb);
}

static uint32_t qfvFormat(void* pb) {
    return (uint32_t)CVPixelBufferGetPixelFormatType((CVPixelBufferRef)pb);
}

// qfvLock locks the base address read-only for the CPU-upload fallback.
static void* qfvLock(void* pb, size_t* stride) {
    CVPixelBufferRef b = (CVPixelBufferRef)pb;
    if (CVPixelBufferLockBaseAddress(b, kCVPixelBufferLock_ReadOnly) != kCVReturnSuccess) return NULL;
    *stride = CVPixelBufferGetBytesPerRow(b);
    return CVPixelBufferGetBaseAddress(b);
}

static void qfvUnlock(void* pb) {
    CVPixelBufferUnlockBaseAddress((CVPixelBufferRef)pb, kCVPixelBufferLock_ReadOnly);
}

// qfvNewBGRA creates an IOSurface-backed, OpenGL-compatible 32BGRA
// pixel buffer (+1 retained) — the shape VideoToolbox is asked to decode
// into. Used by tests; producers create their own.
static void* qfvNewBGRA(int w, int h) {
    CFMutableDictionaryRef attrs = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionaryRef empty = CFDictionaryCreate(kCFAllocatorDefault, NULL, NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionarySetValue(attrs, kCVPixelBufferIOSurfacePropertiesKey, empty);
    CFDictionarySetValue(attrs, kCVPixelBufferMetalCompatibilityKey, kCFBooleanTrue);
    CVPixelBufferRef pb = NULL;
    CVReturn r = CVPixelBufferCreate(kCFAllocatorDefault, w, h, kCVPixelFormatType_32BGRA, attrs, &pb);
    CFRelease(empty);
    CFRelease(attrs);
    return r == kCVReturnSuccess ? (void*)pb : NULL;
}
*/
import "C"

import (
	"errors"
	"image"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// kCVPixelFormatType_32BGRA ('BGRA').
const cvPixelFormat32BGRA = 0x42475241

var errNotPixelBuffer = errors.New("media: PushCVPixelBuffer: not a CVPixelBufferRef")

// PushCVPixelBuffer shows a CVPixelBufferRef. FrameView CFRetains it — the
// caller keeps (and releases) its own reference — and CFReleases it when a
// newer frame replaces it. Safe from any goroutine; never blocks on the UI.
//
// The zero-copy path wants what VideoToolbox produces when asked for
// kCVPixelFormatType_32BGRA with kCVPixelBufferIOSurfacePropertiesKey:
// such a buffer's IOSurface is imported as a GPU texture at draw time, no
// pixels copied.
// Other 32BGRA buffers fall back to a CPU upload; other pixel formats are
// rejected.
func (v *FrameView) PushCVPixelBuffer(pb unsafe.Pointer) error {
	if pb == nil || C.qfvIsPixelBuffer(pb) == 0 {
		return errNotPixelBuffer
	}
	if uint32(C.qfvFormat(pb)) != cvPixelFormat32BGRA {
		return ErrCodecUnsupported
	}
	var w, h C.int
	C.qfvSize(pb, &w, &h)
	if w <= 0 || h <= 0 {
		return errNotPixelBuffer
	}
	C.qfvRetain(pb)
	v.push(&cvFrame{pb: pb, w: int(w), h: int(h)})
	return nil
}

// cvFrame is a pushed CVPixelBuffer.
type cvFrame struct {
	pb   unsafe.Pointer // CVPixelBufferRef, retained by us
	w, h int
	cpu  *image.RGBA // RGBA copy for the CPU path (made on first use)
}

func (f *cvFrame) size() (int, int) { return f.w, f.h }

// cpuImage converts the buffer to RGBA for the CPU path (no Dawn, or the
// window fell back to CPU presentation), once per frame.
func (f *cvFrame) cpuImage() image.Image {
	if f.cpu != nil {
		return f.cpu
	}
	if f.pb == nil {
		return nil
	}
	img := pixelBufferRGBA(f.pb, f.w, f.h)
	if img == nil {
		return nil
	}
	f.cpu = img
	return img
}

// lockPixelBuffer locks a 32BGRA CVPixelBuffer's memory for reading; the
// caller must call unlock when done.
func lockPixelBuffer(pb unsafe.Pointer) (base unsafe.Pointer, stride int, unlock func()) {
	var st C.size_t
	base = C.qfvLock(pb, &st)
	if base == nil {
		return nil, 0, nil
	}
	return base, int(st), func() { C.qfvUnlock(pb) }
}

// pixelBufferRGBA copies a w×h 32BGRA CVPixelBuffer into a new RGBA image.
func pixelBufferRGBA(pb unsafe.Pointer, w, h int) *image.RGBA {
	base, stride, unlock := lockPixelBuffer(pb)
	if base == nil {
		return nil
	}
	defer unlock()
	src := unsafe.Slice((*byte)(base), stride*(h-1)+w*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		s := src[y*stride : y*stride+w*4]
		d := img.Pix[y*img.Stride : y*img.Stride+w*4]
		for x := 0; x < len(s); x += 4 {
			d[x], d[x+1], d[x+2], d[x+3] = s[x+2], s[x+1], s[x], s[x+3]
		}
	}
	return img
}

// draw shows the buffer zero-copy through its IOSurface, or uploads its
// pixels when it has none.
func (f *cvFrame) draw(v *FrameView, c qui.Compositor, src, dst qui.Rect) bool {
	if f.pb == nil {
		return false
	}
	if c.DrawPixelBuffer(f.pb, src, dst) {
		return true
	}
	base, stride, unlock := lockPixelBuffer(f.pb)
	if base == nil {
		return false
	}
	defer unlock()
	return c.DrawPixels(v, v.curSeq, base, stride, f.w, f.h, true, src, dst)
}

func (f *cvFrame) release() {
	f.cpu = nil
	if f.pb != nil {
		C.qfvRelease(f.pb)
		f.pb = nil
	}
}

// newTestPixelBuffer and cfRetainCount exist for frame_view_darwin_test.go
// (test files cannot use cgo).
func newTestPixelBuffer(w, h int) unsafe.Pointer { return C.qfvNewBGRA(C.int(w), C.int(h)) }
func cfRetainCount(p unsafe.Pointer) int         { return int(C.qfvRetainCount(p)) }
func cfRelease(p unsafe.Pointer)                 { C.qfvRelease(p) }
