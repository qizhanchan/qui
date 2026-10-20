//go:build !cgo || js || qui_nogl

package qui

// Builds without OpenGL (CGO_ENABLED=0, GOOS=js, or -tags qui_nogl).
//
// GLRenderer is an alias for CPURenderer here, so desktop-shaped app code —
// window.SetRenderer(qui.NewGLRenderer()), r.SetGPURaster(true) — compiles
// and runs unchanged on the browser and headless backends. The GL-only
// methods are accepted and ignored.

// glRendererAvailable reports whether this build contains the OpenGL
// renderer.
const glRendererAvailable = false

// GLRenderer is CPURenderer in builds without OpenGL.
type GLRenderer = CPURenderer

// NewGLRenderer returns a CPURenderer in builds without OpenGL.
func NewGLRenderer() *GLRenderer { return NewCPURenderer() }

// ActiveGLRenderer is always nil without OpenGL.
func ActiveGLRenderer() *GLRenderer { return nil }

// SetGPURaster is ignored without OpenGL.
func (r *CPURenderer) SetGPURaster(bool) {}

// GPURaster is always false without OpenGL.
func (r *CPURenderer) GPURaster() bool { return false }

// SetPreRender is ignored without OpenGL.
func (r *CPURenderer) SetPreRender(func(GLState)) {}

// SetPostRender is ignored without OpenGL.
func (r *CPURenderer) SetPostRender(func(GLState)) {}

// DrawTexture is ignored without OpenGL.
func (r *CPURenderer) DrawTexture(tex uint32, srcRect, dstRect Rect, texW, texH int32, opts TextureOpts) {
}
