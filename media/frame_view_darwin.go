//go:build darwin && cgo

package media

/*
#include <stdint.h>
#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <CoreVideo/CoreVideo.h>

void quiVideoReleaseGLTex(void* cvTex); // backend_darwin.m

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
    CFDictionarySetValue(attrs, kCVPixelBufferOpenGLCompatibilityKey, kCFBooleanTrue);
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

	"github.com/go-gl/gl/v3.3-core/gl"
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
// kCVPixelFormatType_32BGRA with kCVPixelBufferIOSurfacePropertiesKey and
// kCVPixelBufferOpenGLCompatibilityKey: such a buffer is bound as a GL
// texture through CVOpenGLTextureCache at draw time, no pixels copied.
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
	pb    unsafe.Pointer // CVPixelBufferRef, retained by us
	w, h  int
	cvTex unsafe.Pointer // CVOpenGLTextureRef once bound (UI thread)
	tex   uint32
	// cpuOnly is set once the texture cache refused the buffer (not
	// IOSurface-backed); later draws go straight to the upload path.
	cpuOnly bool
}

func (f *cvFrame) size() (int, int) { return f.w, f.h }

// cpuImage is nil: a CVPixelBuffer has no CPU canvas fallback (the GPU
// path is the point of pushing one).
func (f *cvFrame) cpuImage() image.Image { return nil }

func (f *cvFrame) draw(v *FrameView, state qui.GLState, src, dst qui.Rect) bool {
	if f.pb == nil {
		return false
	}
	if f.tex == 0 && !f.cpuOnly {
		cvTex, name, ok := bindFramePixelBuffer(f.pb)
		if ok {
			f.cvTex, f.tex = cvTex, name
		} else {
			f.cpuOnly = true
		}
	}
	if f.tex != 0 {
		drawRectTextureSrc(f.tex, src, dst, state.FramebufferSize,
			qui.Size{W: float32(f.w), H: float32(f.h)}, true)
		if flushTextureCache != nil {
			flushTextureCache()
		}
		return true
	}
	var stride C.size_t
	base := C.qfvLock(f.pb, &stride)
	if base == nil {
		return false
	}
	defer C.qfvUnlock(f.pb)
	return v.uploadAndDraw(base, int(stride), f.w, f.h, gl.BGRA, src, dst)
}

// release drops the GL texture view first (it aliases the IOSurface), then
// the buffer.
func (f *cvFrame) release() {
	if f.cvTex != nil {
		C.quiVideoReleaseGLTex(f.cvTex)
		f.cvTex, f.tex = nil, 0
	}
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
