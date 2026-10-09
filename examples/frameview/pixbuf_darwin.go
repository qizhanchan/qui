//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework CoreVideo -framework CoreFoundation -framework IOSurface
#include <CoreFoundation/CoreFoundation.h>
#include <CoreVideo/CoreVideo.h>

// newPool makes a CVPixelBufferPool producing what a VideoToolbox session
// configured for the viewer produces: 32BGRA, IOSurface-backed,
// OpenGL-compatible — the zero-copy path into FrameView.
static CVPixelBufferPoolRef newPool(int w, int h) {
    CFMutableDictionaryRef attrs = CFDictionaryCreateMutable(kCFAllocatorDefault, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionaryRef empty = CFDictionaryCreate(kCFAllocatorDefault, NULL, NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    int32_t fmt = kCVPixelFormatType_32BGRA;
    CFNumberRef fmtNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberSInt32Type, &fmt);
    CFNumberRef wNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &w);
    CFNumberRef hNum = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &h);
    CFDictionarySetValue(attrs, kCVPixelBufferPixelFormatTypeKey, fmtNum);
    CFDictionarySetValue(attrs, kCVPixelBufferWidthKey, wNum);
    CFDictionarySetValue(attrs, kCVPixelBufferHeightKey, hNum);
    CFDictionarySetValue(attrs, kCVPixelBufferIOSurfacePropertiesKey, empty);
    CFDictionarySetValue(attrs, kCVPixelBufferOpenGLCompatibilityKey, kCFBooleanTrue);
    CVPixelBufferPoolRef pool = NULL;
    CVPixelBufferPoolCreate(kCFAllocatorDefault, NULL, attrs, &pool);
    CFRelease(fmtNum); CFRelease(wNum); CFRelease(hNum);
    CFRelease(empty); CFRelease(attrs);
    return pool;
}

static void* poolBuffer(CVPixelBufferPoolRef pool, void** base, size_t* stride) {
    CVPixelBufferRef pb = NULL;
    if (CVPixelBufferPoolCreatePixelBuffer(kCFAllocatorDefault, pool, &pb) != kCVReturnSuccess) return NULL;
    CVPixelBufferLockBaseAddress(pb, 0);
    *base = CVPixelBufferGetBaseAddress(pb);
    *stride = CVPixelBufferGetBytesPerRow(pb);
    return pb;
}

static void finishBuffer(void* pb) { CVPixelBufferUnlockBaseAddress((CVPixelBufferRef)pb, 0); }
static void releaseBuffer(void* pb) { CFRelease(pb); }
*/
import "C"

import (
	"unsafe"

	"github.com/qizhanchan/qui/media"
)

// cvSource renders synthetic frames into pooled IOSurface pixel buffers
// and pushes them through the zero-copy path.
type cvSource struct {
	pool C.CVPixelBufferPoolRef
	w, h int
}

func newCVSource(w, h int) *cvSource {
	pool := C.newPool(C.int(w), C.int(h))
	if pool == 0 {
		return nil
	}
	return &cvSource{pool: pool, w: w, h: h}
}

// push draws frame n and hands it to fv. The buffer is released right
// after the push: FrameView holds its own reference for as long as it
// needs the frame — exactly the contract of a VideoToolbox output
// callback.
func (s *cvSource) push(fv *media.FrameView, n int) {
	var base unsafe.Pointer
	var stride C.size_t
	pb := C.poolBuffer(s.pool, &base, &stride)
	if pb == nil {
		return
	}
	pix := unsafe.Slice((*byte)(base), int(stride)*s.h)
	paint(pix, int(stride), s.w, s.h, n, true)
	C.finishBuffer(pb)
	_ = fv.PushCVPixelBuffer(pb)
	C.releaseBuffer(pb)
}

const haveCV = true
