package qui

import (
	"image"
	"log"
	"sync"
)

// Optional renderer capabilities.
//
// Window talks to its renderer through Renderer (Begin/End) plus these
// narrow interfaces, never through a concrete type. That keeps window.go
// compiling in builds where GLRenderer is not a GL renderer at all (no cgo,
// GOOS=js — see renderer_nogl.go), and lets CPURenderer take part in the
// per-frame bookkeeping it does understand (sizes, damage) while skipping
// the GL hooks it cannot honor.

// frameInfoRenderer receives the per-frame facts Window knows before Begin.
type frameInfoRenderer interface {
	SetLogicalSize(s Size)
	SetDamage(rect image.Rectangle)
}

// transparentRenderer keeps the framebuffer's alpha (overlay panels).
type transparentRenderer interface {
	SetTransparent(on bool)
}

// glHookRenderer runs whole-framebuffer raw-GL hooks around the 2D layer.
type glHookRenderer interface {
	SetPreRender(fn func(GLState))
	SetPostRender(fn func(GLState))
}

// gpuRasterRenderer can switch to the GPU raster backend.
type gpuRasterRenderer interface {
	SetGPURaster(on bool)
}

// retainedGLRenderer replays retained GL draws (gl_retained.go).
type retainedGLRenderer interface {
	ForgetRetainedGLDraw(key any)
	redrawRetained() bool
}

// gpuContextRenderer is implemented by renderers that cannot produce a
// frame without a current GPU context (GLRenderer in GL builds).
type gpuContextRenderer interface {
	requiresGPUContext() bool
}

// frameDamageSource reports which part of a CPUFrameSource's image the
// last frame changed, so a surface can composite only that.
type frameDamageSource interface {
	frameDamage() image.Rectangle
}

var cpuFallbackLogOnce sync.Once

// rendererForPlatform adapts the renderer an app asked for to what the
// window's surface can run. Apps written for the desktop call
// SetRenderer(NewGLRenderer()) unconditionally; on a surface with no GPU
// context (browser canvas, X11 without GLX, headless) a GL renderer would
// draw nothing, so it is replaced by a CPURenderer — same rasterizer, same
// pixels, presented by the compositor instead of a buffer swap. Raw-GL
// content (QueueGLDraw, scene3d) is what is lost, and widgets already
// degrade when the canvas lacks GPUCanvas.
func rendererForPlatform(pw platformWindow, r Renderer) Renderer {
	if pw == nil || pw.caps().GPUContext {
		return r
	}
	if g, ok := r.(gpuContextRenderer); ok && g.requiresGPUContext() {
		cpuFallbackLogOnce.Do(func() {
			log.Printf("qui: this window has no GPU context; rendering with CPURenderer instead of GLRenderer")
		})
		return NewCPURenderer()
	}
	return r
}

// defaultRenderer is the renderer a platform window starts with, so an app
// that never calls SetRenderer still shows its UI.
func defaultRenderer(pw platformWindow) Renderer {
	if pw != nil && pw.caps().GPUContext && glRendererAvailable {
		return NewGLRenderer()
	}
	return NewCPURenderer()
}
