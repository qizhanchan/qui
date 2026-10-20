//go:build cgo && !js && !qui_nogl

package qui

import (
	"fmt"
	"image"
	"strings"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// glCompositor is the GL half of a RasterBackend: blitting its
// accumulated 2D layer onto the currently-bound GL framebuffer (typically
// the default one). Called from GLRenderer.End() AFTER the framebuffer
// clear + any pre-hook / deferred-GL replay, so the 2D layer lands on top
// of scene3d / video / webview textures that live under it.
//
// It is not part of RasterBackend because a backend has no business
// knowing whether a GL context exists: the CPU backend also feeds
// CPURenderer, which presents straight to a compositor on platforms
// without GL (the browser, X11 without GLX, headless).
//
// CPU backend uploads its RGBA into the shared texture and issues the
// full-screen quad draw. GPU backend binds its FBO's color texture
// (contents already accumulated during widget.Draw) and issues the same
// draw. GLRenderer owns the shader / VAO; backends borrow them via the
// *GLRenderer handle.
type glCompositor interface {
	CompositeToDefault(r *GLRenderer)
}

// CompositeToDefault uploads the CPU RGBA into the renderer's shared
// texture and draws it with the same full-screen quad program the
// pre-refactor End() used. Called from GLRenderer.End() after the
// clear/hook/deferredGL replay.
func (b *cpuBackend) CompositeToDefault(r *GLRenderer) {
	if r == nil || b.base == nil {
		return
	}
	compositeCPUImageToDefault(r, b.base)
}

// glRendererAvailable reports whether this build contains the OpenGL
// renderer.
const glRendererAvailable = true

// requiresGPUContext implements gpuContextRenderer: every GLRenderer frame
// issues GL calls, so it needs a surface with a context.
func (r *GLRenderer) requiresGPUContext() bool { return true }

// GLRenderer renders widgets into an RGBA buffer and blits it via OpenGL.
// Adds a second shader program (texQuadProg) for compositing
// pre-existing GPU textures (FBO color attachments, loaded images)
// via GPUCanvas.DrawTexture, and a deferred-GL queue that lets 3D
// widgets paint between the framebuffer clear and the 2D blit.
type GLRenderer struct {
	initialized bool
	initErr     error
	program     uint32
	programFlip int32 // uFlipY uniform location on program
	vao         uint32
	vbo         uint32
	texture     uint32
	img         *image.RGBA
	// texW / texH are the dimensions r.texture's storage was ALLOCATED
	// for. compositeCPUImageToDefault reallocates only when they change,
	// so a steady-state frame updates the existing storage instead of
	// handing the driver a fresh allocation every frame.
	texW, texH int32
	// texSrc is the image whose pixels r.texture currently mirrors (nil:
	// unknown), and damage the physical-pixel area of it the frame being
	// rendered changed (SetDamage; empty: unknown). Together they let a
	// frame upload just what changed instead of the whole framebuffer.
	texSrc *image.RGBA
	damage image.Rectangle
	// damageNone marks a frame known to have changed no CPU pixels (a
	// RequestGLRedraw frame), as opposed to an empty damage, which means
	// "unknown" and uploads everything.
	damageNone bool

	// retained holds the GL draws that persist across frames that do not
	// repaint their area. See gl_retained.go.
	retained retainedGLSet

	// GPU compositing path (DrawTexture). A separate program that
	// doesn't Y-flip by default so FBO textures composite correctly;
	// FlipY in TextureOpts re-enables the flip for image-origin data.
	texQuadProg    uint32
	texQuadVAO     uint32
	texQuadVBO     uint32
	texQuadTint    int32 // uniform location
	texQuadOpacity int32
	texQuadFlipY   int32

	// transparent makes End() clear to a fully transparent framebuffer and
	// composite the CPU frame with straight (non-premultiplied) alpha
	// preserved, so a compositor honoring the surface's alpha shows the
	// desktop through wherever the widget tree drew nothing. Set for
	// WindowOverlayPanel — see SetTransparent.
	transparent bool

	// Deferred GL draws queued by widgets during the 2D pass. Flushed
	// in End() after the framebuffer clear but before the 2D blit.
	deferredGL []func(GLState)

	// Pre/Post raw-GL hooks — Window-owned, set via SetPreRender /
	// SetPostRender. Invoked during End() around the 2D blit.
	preHook  func(GLState)
	postHook func(GLState)

	// LogicalSize is the window's logical (point) size. Set by
	// Window.Step via SetLogicalSize BEFORE Begin so GLState passed
	// to deferred GL hooks carries the true logical/framebuffer
	// ratio (= DPR). If not set, defaults to the framebuffer size
	// (ratio of 1) — safe for non-HiDPI cases and tests.
	logicalSize Size

	// gpuRaster selects the GPU-native raster backend when true.
	// Toggled via SetGPURaster / Window.SetGPURaster / the
	// QUI_GPU_RASTER=1 env var (see NewGLRenderer). The active
	// backend is chosen inside Begin, so a running app can flip
	// this between frames.
	gpuRaster bool

	// backend is retained between Begin and End so End() can call
	// CompositeToDefault on the same instance the widget tree drew
	// into. Reset on each Begin.
	backend RasterBackend

	// cachedCPU / cachedGPU persist the actual backend instances across
	// frames so their retained buffers (r.img for CPU, the FBO texture
	// for GPU) survive between calls to Begin. Both are lazy: allocated
	// on the first Begin that needs them; the flag flip in SetGPURaster
	// selects which one Begin binds.
	//
	// Without this cache each Begin would build a fresh gpuBackend and
	// reallocFBO would run every frame — erasing everything outside
	// the dirty region and defeating incremental repaint.
	cachedCPU *cpuBackend
	cachedGPU *gpuBackend
}

// SetGPURaster switches the raster backend selection for the next Begin.
// Off by default; the CPU backend remains the reference implementation.
// This is a per-renderer toggle so multi-window apps can opt in per
// window.
func (r *GLRenderer) SetGPURaster(on bool) {
	if r == nil {
		return
	}
	r.gpuRaster = on
}

// SetTransparent selects whether the framebuffer keeps an alpha channel
// the compositor honors, so pixels the widget tree never touched show what
// is behind the window instead of an opaque background.
//
// Window.SetRenderer calls this from the window's WindowKind, so an
// overlay panel gets it without the app asking. Setting it on a window
// whose OS surface is opaque is harmless but pointless: the compositor
// discards the alpha.
func (r *GLRenderer) SetTransparent(on bool) {
	if r == nil {
		return
	}
	r.transparent = on
}

// GPURaster reports whether the GPU raster backend is currently
// selected.
func (r *GLRenderer) GPURaster() bool {
	if r == nil {
		return false
	}
	return r.gpuRaster
}

// SetLogicalSize records the window's logical (point) size for the
// next frame. Must be called BEFORE Begin so deferred GL hooks see
// the correct GLState.LogicalSize. Window.Step is the only caller;
// no-op if r is nil.
func (r *GLRenderer) SetLogicalSize(s Size) {
	if r == nil {
		return
	}
	r.logicalSize = s
}

// NewGLRenderer creates a basic OpenGL renderer. Honors the
// QUI_GPU_RASTER=1 environment variable as an ergonomic opt-in for
// the GPU raster backend — apps that set the flag once at startup
// don't have to touch every callsite.
func NewGLRenderer() *GLRenderer {
	r := &GLRenderer{}
	if envGPURasterEnabled() {
		r.gpuRaster = true
	}
	return r
}

// lastActiveGLRenderer tracks the most recently-begun GLRenderer so
// widgets (GLViewport, raw-GL hooks) can reach DrawTexture without
// threading it through GLState. Safe because the main loop is
// single-threaded and only one Window's renderer is "in Begin→End"
// at any given time. Exposed via ActiveGLRenderer for subpackage use.
var lastActiveGLRenderer *GLRenderer

// ActiveGLRenderer returns the GLRenderer currently inside Begin→End,
// or nil if none. Subpackages (scene3d) call this from a QueueGLDraw
// callback to composite their own textures via DrawTexture.
func ActiveGLRenderer() *GLRenderer { return lastActiveGLRenderer }

func (r *GLRenderer) Begin(windowSize Size) Canvas {
	r.ensureInit()
	if !r.initialized {
		return noopCanvas{}
	}
	w := int(maxFloat(windowSize.W, 1))
	h := int(maxFloat(windowSize.H, 1))
	if r.img == nil || r.img.Rect.Dx() != w || r.img.Rect.Dy() != h {
		r.img = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	// Default LogicalSize to framebuffer size if the caller didn't
	// supply one via SetLogicalSize. Window.Step sets it explicitly;
	// tests / standalone Renderer use this default.
	if r.logicalSize.W == 0 || r.logicalSize.H == 0 {
		r.logicalSize = windowSize
	}
	lastActiveGLRenderer = r
	gl.Viewport(0, 0, int32(w), int32(h))
	state := newCanvasState(Rect{W: float32(w), H: float32(h)})
	var backend RasterBackend
	if r.gpuRaster {
		if r.cachedGPU == nil {
			r.cachedGPU = newGPUBackend(r, r.img)
		} else {
			// r.img may have been reallocated on resize; hand the
			// new base to the fallback CPU backend so text /
			// vectors / images keep landing in the right buffer.
			r.cachedGPU.fallback.SetBase(r.img)
		}
		backend = r.cachedGPU
	} else {
		if r.cachedCPU == nil {
			r.cachedCPU = newCPUBackend(r.img)
		} else {
			r.cachedCPU.SetBase(r.img)
		}
		backend = r.cachedCPU
	}
	backend.Begin(windowSize)
	r.backend = backend
	return gpuImageCanvas{imageCanvas: imageCanvas{canvasState: state, backend: backend, layers: newFrontendLayerTracker()}, renderer: r}
}

// FrameImage implements CPUFrameSource: the CPU-raster backend's
// framebuffer is the finished frame, so a surface that can composite CPU
// pixels may take it directly and skip End's texture upload.
//
// Returns nil under the GPU raster backend (QUI_GPU_RASTER=1): its pixels
// live in an FBO and were never in system memory, so there is nothing to
// hand over without a readback that would cost more than the blit.
func (r *GLRenderer) FrameImage() *image.RGBA {
	if r == nil || r.backend == nil {
		return nil
	}
	return r.backend.CPUImage()
}

func (r *GLRenderer) End() {
	if !r.initialized || r.img == nil {
		return
	}
	// Give the active backend a chance to finalize (e.g. gpuBackend
	// flushes any lingering CPU-fallback pixels into its FBO).
	if r.backend != nil {
		r.backend.End()
	}
	r.composite()
}

// composite is the GL half of a frame: clear, raw-GL hooks and deferred
// draws, then the 2D layer on top. Shared by End and by the GL-only
// RequestGLRedraw frame (redrawRetained), which re-composites the
// previous frame's 2D layer without re-rasterizing it.
func (r *GLRenderer) composite() {
	state := GLState{
		FramebufferSize: Size{W: float32(r.img.Rect.Dx()), H: float32(r.img.Rect.Dy())},
		LogicalSize:     r.logicalSize,
	}
	if r.transparent {
		gl.ClearColor(0, 0, 0, 0)
	} else {
		gl.ClearColor(0, 0, 0, 1)
	}
	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

	// Raw pre-render hook (full-screen shadertoy-style backgrounds).
	if r.preHook != nil {
		r.preHook(state)
	}
	// Retained draws whose owners were not drawn this frame and whose area
	// this frame left alone: their content is still meant to be on screen.
	for _, fn := range r.retained.carryOver(r.damage, r.damageNone) {
		fn(state)
	}
	// Deferred widget GL draws (GLViewport scene render, etc.).
	for _, fn := range r.deferredGL {
		fn(state)
	}
	// Wipe the deferred queue so next frame starts fresh.
	for i := range r.deferredGL {
		r.deferredGL[i] = nil
	}
	r.deferredGL = r.deferredGL[:0]
	r.retained.endFrame()

	// Re-establish the 2D blit state — user code above may have
	// left blend/depth/cull/vao in arbitrary configurations.
	// Crucially, restore the viewport to the full framebuffer: a
	// deferred GL draw (e.g. scene3d.Viewport rendering into an FBO
	// smaller than the window) sets gl.Viewport to its own size and
	// doesn't restore it. Without resetting here, the full-window 2D
	// blit below would be squished into that sub-viewport, shifting
	// the entire 2D UI left/up so it no longer lines up with the
	// (logical-coordinate) hit-testing.
	w, h := r.img.Rect.Dx(), r.img.Rect.Dy()
	gl.Viewport(0, 0, int32(w), int32(h))
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.SCISSOR_TEST)
	gl.Disable(gl.CULL_FACE)
	gl.Enable(gl.BLEND)
	if r.transparent {
		// The CPU rasterizer leaves PREMULTIPLIED color wherever alpha < 1
		// (verified: filling 50%-alpha pure red on a transparent clear
		// yields {128,0,0,128}, not {255,0,0,128}). Premultiplied src-over
		// is therefore (ONE, ONE_MINUS_SRC_ALPHA):
		//
		//   dstRGB = srcRGB + 0·(1−srcA) = srcRGB   (stays premultiplied)
		//   dstA   = srcA   + 0·(1−srcA) = srcA
		//
		// The SRC_ALPHA factor used below would multiply the already
		// premultiplied color by alpha a second time, darkening every
		// translucent pixel — invisible on an opaque surface, where the
		// clear makes every pixel alpha 1 and the two factors coincide,
		// but plainly wrong once the framebuffer keeps its alpha.
		gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	} else {
		gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	}

	// Backend blit: cpuBackend uploads r.img via r.texture; gpuBackend
	// binds its own FBO color texture. Falls back to the CPU path if
	// no backend was recorded (defensive — Begin always sets one).
	if c, ok := r.backend.(glCompositor); ok {
		c.CompositeToDefault(r)
	} else {
		compositeCPUImageToDefault(r, r.img)
	}

	// Post-render hook (overlays on top of the 2D layer).
	if r.postHook != nil {
		r.postHook(state)
	}
	// Damage describes one frame; the next must report its own.
	r.damage = image.Rectangle{}
	r.damageNone = false
}

// SetDamage tells the renderer which framebuffer pixels (physical, in
// r.img's coordinates) the current frame changed, so End uploads only
// those to the GPU. Call it between Begin and End; Window.Step does this
// with the frame's dirty region. Without it End uploads everything.
// Pixels outside rect must not have been touched by this frame's drawing.
func (r *GLRenderer) SetDamage(rect image.Rectangle) {
	if r == nil {
		return
	}
	r.damage = rect
}

// textureUploadRect is the part of img End must upload for r.texture to
// match it: all of it when the texture mirrors something else (first
// frame, a resize, the GPU backend borrowed it) or the damage is unknown,
// otherwise just the damaged pixels plus a one-pixel guard band.
func (r *GLRenderer) textureUploadRect(img *image.RGBA) image.Rectangle {
	if r.texSrc != img {
		return img.Rect
	}
	if r.damageNone {
		return image.Rectangle{}
	}
	if r.damage.Empty() {
		return img.Rect
	}
	return r.damage.Inset(-1).Intersect(img.Rect)
}

// compositeCPUImageToDefault uploads `img` into r.texture and draws
// the full-screen quad using r.program. Shared between cpuBackend
// (final blit) and gpuBackend (per-flush CPU→GPU sync). Assumes the
// caller has already set viewport / blend state.
//
// Row 0 of a Go image.RGBA is the TOP of the image, but GL uploads
// place the first byte at texture-y=0 which sits at the BOTTOM of the
// texture. r.program's fragment shader consumes uFlipY to compensate;
// this helper always writes 1.0 because it's the CPU-RGBA convention.
func compositeCPUImageToDefault(r *GLRenderer, img *image.RGBA) {
	if img == nil {
		return
	}
	w, h := int32(img.Rect.Dx()), int32(img.Rect.Dy())
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, r.texture)
	// TexImage2D ALLOCATES storage; calling it per frame makes the driver
	// orphan the previous allocation while frames are still in flight, so
	// several framebuffer-sized copies stay resident in graphics memory.
	// Allocate once per size and update in place after that.
	if r.texW != w || r.texH != h {
		gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
		r.texW, r.texH = w, h
		r.texSrc = nil
	}
	// The texture outlives the frame, so pixels this frame didn't touch
	// are already there: upload only the damage (a caret blink is a few
	// hundred pixels, the framebuffer several megabytes).
	switch up := r.textureUploadRect(img); {
	case up == img.Rect:
		gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	case !up.Empty():
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, int32(img.Stride/4))
		gl.TexSubImage2D(gl.TEXTURE_2D, 0,
			int32(up.Min.X-img.Rect.Min.X), int32(up.Min.Y-img.Rect.Min.Y),
			int32(up.Dx()), int32(up.Dy()),
			gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&img.Pix[img.PixOffset(up.Min.X, up.Min.Y)]))
		gl.PixelStorei(gl.UNPACK_ROW_LENGTH, 0)
	}
	r.texSrc = img
	gl.UseProgram(r.program)
	gl.Uniform1f(r.programFlip, 1.0)
	gl.BindVertexArray(r.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

// SetPreRender installs a raw-GL hook invoked between the framebuffer
// clear and the 2D blit. Pass nil to clear. The callback may freely
// change GL state; the renderer re-establishes blit state before
// compositing the 2D layer.
func (r *GLRenderer) SetPreRender(fn func(GLState)) { r.preHook = fn }

// SetPostRender installs a raw-GL hook invoked after the 2D blit
// but before SwapBuffers. Pass nil to clear.
func (r *GLRenderer) SetPostRender(fn func(GLState)) { r.postHook = fn }

func (r *GLRenderer) ensureInit() {
	if r.initialized || r.initErr != nil {
		return
	}
	if err := gl.Init(); err != nil {
		r.initErr = err
		return
	}
	program, err := newProgram(vertexShaderSource, fragmentShaderSource)
	if err != nil {
		r.initErr = err
		return
	}
	r.program = program

	vertices := []float32{
		-1, 1, 0, 1,
		-1, -1, 0, 0,
		1, -1, 1, 0,
		-1, 1, 0, 1,
		1, -1, 1, 0,
		1, 1, 1, 1,
	}

	gl.GenVertexArrays(1, &r.vao)
	gl.GenBuffers(1, &r.vbo)
	gl.BindVertexArray(r.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(vertices)*4, gl.Ptr(vertices), gl.STATIC_DRAW)

	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(2*4))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	gl.GenTextures(1, &r.texture)
	gl.BindTexture(gl.TEXTURE_2D, r.texture)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)

	gl.UseProgram(r.program)
	texUniform := gl.GetUniformLocation(r.program, gl.Str("uTex\x00"))
	gl.Uniform1i(texUniform, 0)
	r.programFlip = gl.GetUniformLocation(r.program, gl.Str("uFlipY\x00"))
	// Default: preserve historic behavior — top-origin CPU RGBA uploads
	// need the Y-flip. gpuBackend flips this off just before its
	// FBO->default blit.
	gl.Uniform1f(r.programFlip, 1.0)
	gl.UseProgram(0)

	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	// Second program: textured-quad compositor for DrawTexture. Takes
	// NDC vertices + UV in attribute 0/1; uniforms pick tint / opacity
	// / Y-flip. Dynamic VBO — vertices are computed per-call.
	texProg, err := newProgram(texQuadVertexShader, texQuadFragmentShader)
	if err != nil {
		r.initErr = err
		return
	}
	r.texQuadProg = texProg
	gl.GenVertexArrays(1, &r.texQuadVAO)
	gl.GenBuffers(1, &r.texQuadVBO)
	gl.BindVertexArray(r.texQuadVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.texQuadVBO)
	gl.BufferData(gl.ARRAY_BUFFER, 6*4*4, nil, gl.DYNAMIC_DRAW)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(0))
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 4*4, gl.PtrOffset(2*4))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)
	gl.BindVertexArray(0)

	gl.UseProgram(r.texQuadProg)
	texLoc := gl.GetUniformLocation(r.texQuadProg, gl.Str("uTex\x00"))
	gl.Uniform1i(texLoc, 0)
	r.texQuadTint = gl.GetUniformLocation(r.texQuadProg, gl.Str("uTint\x00"))
	r.texQuadOpacity = gl.GetUniformLocation(r.texQuadProg, gl.Str("uOpacity\x00"))
	r.texQuadFlipY = gl.GetUniformLocation(r.texQuadProg, gl.Str("uFlipY\x00"))
	gl.UseProgram(0)

	r.initialized = true
}

// DrawTexture issues the compositing draw call. Coordinates are in
// physical framebuffer pixels (DPR-scaled). srcRect is in texture
// pixel space — 0..1 UV is derived by dividing against the texture's
// intrinsic dimensions when they're non-zero, or by assuming the
// whole texture when srcRect.W or H is zero.
func (r *GLRenderer) DrawTexture(tex uint32, srcRect, dstRect Rect, texW, texH int32, opts TextureOpts) {
	if !r.initialized {
		return
	}
	fbW := float32(r.img.Rect.Dx())
	fbH := float32(r.img.Rect.Dy())
	if fbW <= 0 || fbH <= 0 {
		return
	}
	// Destination in clip space. Y is flipped because our scaleCanvas
	// / imageCanvas treat (0,0) as top-left, but OpenGL NDC has
	// (0,0) at the center with +Y up.
	x0 := dstRect.X/fbW*2 - 1
	y0 := 1 - dstRect.Y/fbH*2
	x1 := (dstRect.X+dstRect.W)/fbW*2 - 1
	y1 := 1 - (dstRect.Y+dstRect.H)/fbH*2

	// Source UVs.
	var u0, v0, u1, v1 float32 = 0, 0, 1, 1
	if srcRect.W > 0 && srcRect.H > 0 && texW > 0 && texH > 0 {
		u0 = srcRect.X / float32(texW)
		v0 = srcRect.Y / float32(texH)
		u1 = (srcRect.X + srcRect.W) / float32(texW)
		v1 = (srcRect.Y + srcRect.H) / float32(texH)
	}

	// The dst TOP edge (y0) samples v1 and the BOTTOM edge samples v0,
	// so that with FlipY=false a bottom-up texture (an FBO color
	// attachment, v=0 at the scene's bottom) displays upright — per the
	// TextureOpts contract. FlipY=true inverts in the shader for
	// top-down image-origin data.
	vertices := [...]float32{
		x0, y0, u0, v1,
		x0, y1, u0, v0,
		x1, y1, u1, v0,
		x0, y0, u0, v1,
		x1, y1, u1, v0,
		x1, y0, u1, v1,
	}

	// The NDC vertices above are computed against the full framebuffer
	// (fbW×fbH), so the viewport must cover it. A caller that rendered
	// into a smaller FBO just before this (scene3d.Viewport) will have
	// left gl.Viewport at the FBO size — reset it so the composite lands
	// where dstRect says, not squished into the sub-viewport.
	gl.Viewport(0, 0, int32(fbW), int32(fbH))
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.SCISSOR_TEST)
	gl.Disable(gl.CULL_FACE)

	gl.UseProgram(r.texQuadProg)
	tint := opts.Tint
	if tint == (Color{}) {
		tint = Color{R: 1, G: 1, B: 1, A: 1}
	}
	gl.Uniform4f(r.texQuadTint, tint.R, tint.G, tint.B, tint.A)
	opacity := opts.Opacity
	if opacity <= 0 {
		opacity = 1
	}
	gl.Uniform1f(r.texQuadOpacity, opacity)
	flipY := float32(0)
	if opts.FlipY {
		flipY = 1
	}
	gl.Uniform1f(r.texQuadFlipY, flipY)

	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, tex)

	gl.BindVertexArray(r.texQuadVAO)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.texQuadVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(vertices)*4, gl.Ptr(&vertices[0]))
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	gl.UseProgram(0)
}

// gpuImageCanvas embeds imageCanvas and adds GPU-aware methods so
// the canvas passed to Draw satisfies GPUCanvas. Widgets type-assert
// to GPUCanvas to reach DrawTexture / QueueGLDraw; if the assertion
// fails (during tests with NoopRenderer, or headless builds) the
// widget falls back to a CPU-only render.
type gpuImageCanvas struct {
	imageCanvas
	renderer *GLRenderer
}

// DrawTexture queues the compositing call. It runs in End() AFTER
// the framebuffer clear and BEFORE the 2D CPU blit, so the 2D
// layer paints on top. A widget that wants the texture to be
// visible (not covered) should punch a hole in the CPU image by
// calling FillRect with alpha=0 at the same rect — 2D blit's
// alpha blending then leaves the texture showing through. Higher-
// priority 2D widgets drawn later (tooltips, dropdowns) cover the
// texture naturally because they paint opaque pixels into the
// CPU image after the hole was punched.
//
// For "overlay on top of 2D" use Window.SetPostRender directly.
func (c gpuImageCanvas) DrawTexture(tex uint32, srcRect, dstRect Rect, opts TextureOpts) {
	r := c.renderer
	if r == nil {
		return
	}
	// Query the texture's intrinsic size once so srcRect semantics
	// can use pixel coords the same way DrawImage does.
	var w, h int32
	gl.BindTexture(gl.TEXTURE_2D, tex)
	gl.GetTexLevelParameteriv(gl.TEXTURE_2D, 0, gl.TEXTURE_WIDTH, &w)
	gl.GetTexLevelParameteriv(gl.TEXTURE_2D, 0, gl.TEXTURE_HEIGHT, &h)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	// Defer the actual draw so it runs in End() AFTER the 2D blit,
	// which is where user expects overlays to appear. Alternatively
	// widgets can call QueueGLDraw for pre-blit placement.
	rr := r
	r.deferredGL = append(r.deferredGL, func(state GLState) {
		// Re-bind blit state after raw user code may have trashed it.
		rr.DrawTexture(tex, srcRect, dstRect, w, h, opts)
	})
}

// QueueGLDraw schedules a raw GL callback to run between the
// framebuffer clear and the 2D blit. See GPUCanvas.QueueGLDraw.
func (c gpuImageCanvas) QueueGLDraw(fn func(GLState)) {
	if c.renderer == nil || fn == nil {
		return
	}
	c.renderer.deferredGL = append(c.renderer.deferredGL, fn)
}
func newProgram(vertexSrc, fragmentSrc string) (uint32, error) {
	vs, err := CompileShader(vertexSrc, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}
	fs, err := CompileShader(fragmentSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vs)
		return 0, err
	}
	program := gl.CreateProgram()
	gl.AttachShader(program, vs)
	gl.AttachShader(program, fs)
	gl.LinkProgram(program)
	gl.DeleteShader(vs)
	gl.DeleteShader(fs)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))
		return 0, fmt.Errorf("link program failed: %s", strings.TrimRight(log, "\x00"))
	}
	return program, nil
}

// CompileShader compiles a single GL shader stage and returns its ID.
// Returns an error on compile failure with the info log inlined. Must
// be called on a goroutine with the target GL context current.
func CompileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)
	csources, free := gl.Strs(source + "\x00")
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)
		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))
		return 0, fmt.Errorf("compile shader failed: %s", strings.TrimRight(log, "\x00"))
	}
	return shader, nil
}

const vertexShaderSource = `
#version 330 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    vUV = aUV;
    gl_Position = vec4(aPos, 0.0, 1.0);
}
`

// fragmentShaderSource — main 2D blit. uFlipY == 1 flips V (used for
// the CPU RGBA upload path where row 0 is top-of-image but GL's
// texture-y=0 is bottom-of-texture); uFlipY == 0 samples straight
// (used for gpuBackend's FBO where the shader already wrote pixels
// in GL's native bottom-origin space).
const fragmentShaderSource = `
#version 330 core
in vec2 vUV;
out vec4 FragColor;
uniform sampler2D uTex;
uniform float uFlipY;
void main() {
    vec2 uv = vec2(vUV.x, mix(vUV.y, 1.0 - vUV.y, uFlipY));
    FragColor = texture(uTex, uv);
}
`

// Textured-quad compositor used by GPUCanvas.DrawTexture. Separate
// from the 2D CPU blit so we can expose FlipY, tinting, and opacity
// without bending the main blit's contract.
const texQuadVertexShader = `
#version 330 core
layout(location = 0) in vec2 aPos;
layout(location = 1) in vec2 aUV;
out vec2 vUV;
void main() {
    vUV = aUV;
    gl_Position = vec4(aPos, 0.0, 1.0);
}
`

const texQuadFragmentShader = `
#version 330 core
in vec2 vUV;
out vec4 FragColor;
uniform sampler2D uTex;
uniform vec4 uTint;
uniform float uOpacity;
uniform float uFlipY;
void main() {
    vec2 uv = vec2(vUV.x, mix(vUV.y, 1.0 - vUV.y, uFlipY));
    vec4 c = texture(uTex, uv) * uTint;
    c.a *= uOpacity;
    FragColor = c;
}
`

// Snapshot returns a deep copy of the last fully-rendered frame, or
// nil if no frame has been rendered yet (the window's first Step has
// not completed). The returned image's bounds match the framebuffer
// size (physical pixels on HiDPI displays).
//
// Where the pixels come from depends on the active raster backend, and the
// caller should not have to care:
//
//   - CPU raster (the default): the retained frame buffer, copied.
//   - GPU raster (QUI_GPU_RASTER=1): read back from the backend's FBO. The
//     pixels were never in system memory — and the CPU-fallback buffer is
//     zeroed once uploaded — so without the readback this returned an empty
//     image, which encodes as a blank white PNG.
//
// Because the GPU path issues GL calls, call this from the goroutine that
// owns the window's GL context: the main goroutine, inside Step (animators,
// event handlers, or a PostJob job — Step makes the context current before
// draining jobs). Off-main-goroutine callers should use SnapshotScaled and
// friends, which serialize through that queue for exactly this reason.
func (r *GLRenderer) Snapshot() *image.RGBA {
	if r == nil {
		return nil
	}
	// A backend whose pixels live on the GPU offers a readback; one whose
	// pixels are already in memory does not, and falls through to the copy
	// below. Readback failure (no FBO allocated yet) also falls through
	// rather than reporting a blank frame as success.
	if rb, ok := r.backend.(CPUReadback); ok {
		if img := rb.ReadbackCPU(); img != nil {
			return img
		}
	}
	if r.img == nil {
		return nil
	}
	src := r.img
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}
