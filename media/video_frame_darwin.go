//go:build darwin && cgo

package media

/*
#include <stdint.h>
#include <stdlib.h>

// Forward declarations — full definitions live in the cgo preamble
// of backend_darwin.go. Re-declared here so this file compiles even
// when the Go tooling parses files in isolation; cgo links by name.
void quiVideoReleasePixBuf(void* pixBuf);
*/
import "C"

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// darwinVideoFrame is the darwin backend's VideoFrame. It wraps:
//
//   - a CVPixelBufferRef (cvBuf) — IOSurface-backed BGRA pixels
//     produced by AVAssetReader on a background dispatch queue.
//     CFRetained when received by quiVideoDecodeOutput, CFReleased
//     in Release.
//
// Composite hands the buffer to the compositor, which imports its
// IOSurface as a GPU texture (no copy).
//
// Reference counting: every frame starts with refs=1 (the implicit
// reference returned to the caller, or held by currentFrame).
// CurrentFrame / DecodeFrameAt callers receive a pre-Retained frame
// and must Release. The pump promotes a frame from the pending
// channel into currentFrame; the previous currentFrame is Released.
//
// All CF release calls run from Release, on whichever goroutine the
// last reference is dropped. CFRelease is thread-safe.
type darwinVideoFrame struct {
	pts   time.Duration
	w, h  int
	cvBuf unsafe.Pointer // CVPixelBufferRef, retained
	refs  int32          // atomic
}

func newDarwinVideoFrame(cvBuf unsafe.Pointer, pts time.Duration, w, h int) *darwinVideoFrame {
	return &darwinVideoFrame{
		pts:   pts,
		w:     w,
		h:     h,
		cvBuf: cvBuf,
		refs:  1,
	}
}

func (f *darwinVideoFrame) PTS() time.Duration { return f.pts }
func (f *darwinVideoFrame) Width() int         { return f.w }
func (f *darwinVideoFrame) Height() int        { return f.h }

func (f *darwinVideoFrame) Retain() {
	atomic.AddInt32(&f.refs, 1)
}

// Composite implements VideoFrame: the IOSurface-backed buffer is shown
// zero-copy.
func (f *darwinVideoFrame) Composite(c qui.Compositor, _ any, src, physDst qui.Rect) bool {
	if f.cvBuf == nil {
		return false
	}
	return c.DrawPixelBuffer(f.cvBuf, src, physDst)
}

func (f *darwinVideoFrame) Release() {
	if atomic.AddInt32(&f.refs, -1) != 0 {
		return
	}
	if f.cvBuf != nil {
		C.quiVideoReleasePixBuf(f.cvBuf)
		f.cvBuf = nil
	}
}
