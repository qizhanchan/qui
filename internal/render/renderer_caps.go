package render

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Optional renderer capabilities.
//
// Window talks to its renderer through Renderer (Begin/End) plus these
// narrow interfaces, never through a concrete type. That keeps window.go
// compiling in builds where GLRenderer is not a GL renderer at all (no cgo,
// GOOS=js — see renderer_nogl.go), and lets CPURenderer take part in the
// per-frame bookkeeping it does understand (sizes, damage) while skipping
// the GL hooks it cannot honor.

// FrameInfoRenderer receives the per-frame facts Window knows before Begin.
type FrameInfoRenderer interface {
	SetLogicalSize(s gfx.Size)
	SetDamage(rect image.Rectangle)
}

// TransparentRenderer keeps the framebuffer's alpha (overlay panels).
type TransparentRenderer interface {
	SetTransparent(on bool)
}

// glHookRenderer runs whole-framebuffer raw-GL hooks around the 2D layer.
type GLHookRenderer interface {
	SetPreRender(fn func(GLState))
	SetPostRender(fn func(GLState))
}

// gpuRasterRenderer can switch to the GPU raster backend.
type GPURasterRenderer interface {
	SetGPURaster(on bool)
}

// RetainedGLRenderer replays retained GL draws (gl_retained.go).
type RetainedGLRenderer interface {
	ForgetRetainedGLDraw(key any)
	RedrawRetained() bool
}

// gpuContextRenderer is implemented by renderers that cannot produce a
// frame without a current GPU context (GLRenderer in GL builds).
type GPUContextRenderer interface {
	RequiresGPUContext() bool
}

// FrameDamageSource reports which part of a CPUFrameSource's image the
// last frame changed, so a surface can composite only that.
type FrameDamageSource interface {
	FrameDamage() image.Rectangle
}
