//go:build darwin && cgo

package media

/*
#include <stdint.h>
#include <stdlib.h>

// Forward declarations — full definitions live in the cgo preamble
// of backend_darwin.go. Re-declared here so this file compiles even
// when the Go tooling parses files in isolation; cgo links by name.
void quiVideoReleasePixBuf(void* pixBuf);
void quiVideoReleaseGLTex(void* cvTex);
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
//   - an optional CVOpenGLTextureRef (cvTex) — a GL_TEXTURE_2D view
//     of cvBuf created on the main thread (with the GL context
//     current) inside drawFrame. nil until first bind. CFReleased
//     in Release (before cvBuf, to satisfy IOSurface lifetime).
//   - a GL texture name (tex) — extracted from cvTex once bound.
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
	tex   uint32         // 0 until first bind
	cvTex unsafe.Pointer // CVOpenGLTextureRef, nil until first bind
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
func (f *darwinVideoFrame) GLTexture() uint32  { return f.tex }

// EnsureTexture lazily binds the frame's CVPixelBuffer to a GL
// texture via the process-wide CVOpenGLTextureCache. MUST be called
// on the main thread with the GL context current (i.e. inside a
// GPUCanvas.QueueGLDraw callback). Subsequent calls are no-ops —
// the binding is cached on the frame and released only by Release.
func (f *darwinVideoFrame) EnsureTexture() bool {
	if f.tex != 0 {
		return true
	}
	if f.cvBuf == nil {
		return false
	}
	cvTex, name, ok := bindFramePixelBuffer(f.cvBuf)
	if !ok {
		return false
	}
	f.cvTex = cvTex
	f.tex = name
	return true
}

// DrawIntoFramebuffer blits the frame's GL_TEXTURE_RECTANGLE onto
// the current framebuffer at physDst (framebuffer pixels) using
// the package-private sampler2DRect shader. Returns false if the
// texture isn't bound and EnsureTexture failed.
//
// Implements the framebufferDrawer interface defined in
// video_view.go so VideoView's QueueGLDraw closure can route
// darwin frames around GLRenderer.DrawTexture (which only handles
// GL_TEXTURE_2D).
func (f *darwinVideoFrame) DrawIntoFramebuffer(state qui.GLState, physDst qui.Rect, flipY bool) bool {
	if !f.EnsureTexture() {
		return false
	}
	drawRectTexture(f.tex, physDst, state.FramebufferSize,
		qui.Size{W: float32(f.w), H: float32(f.h)}, flipY)
	return true
}

func (f *darwinVideoFrame) Retain() {
	atomic.AddInt32(&f.refs, 1)
}

// Release drops one reference. When the count hits zero the
// underlying Core Foundation objects are released. Order matters:
// cvTex aliases cvBuf's IOSurface, so cvTex must die first.
func (f *darwinVideoFrame) Release() {
	if atomic.AddInt32(&f.refs, -1) != 0 {
		return
	}
	if f.cvTex != nil {
		C.quiVideoReleaseGLTex(f.cvTex)
		f.cvTex = nil
		f.tex = 0
	}
	if f.cvBuf != nil {
		C.quiVideoReleasePixBuf(f.cvBuf)
		f.cvBuf = nil
	}
}
