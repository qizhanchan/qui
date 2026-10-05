package qui

import (
	"fmt"
	"image"
	"image/color"
	stddraw "image/draw"
	"io"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/go-gl/gl/v3.3-core/gl"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

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
	// Deferred widget GL draws (GLViewport scene render, etc.).
	for _, fn := range r.deferredGL {
		fn(state)
	}
	// Wipe the deferred queue so next frame starts fresh.
	for i := range r.deferredGL {
		r.deferredGL[i] = nil
	}
	r.deferredGL = r.deferredGL[:0]

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
	if r.backend != nil {
		r.backend.CompositeToDefault(r)
	} else {
		compositeCPUImageToDefault(r, r.img)
	}

	// Post-render hook (overlays on top of the 2D layer).
	if r.postHook != nil {
		r.postHook(state)
	}
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
	}
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, w, h, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
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

// imageCanvas is the frontend Canvas. It owns the state stack (matrix +
// clip), does DrawShape switch dispatch, and projects logical inputs
// to physical pixels — then delegates ALL rasterization to a
// RasterBackend. The CPU reference backend (cpuBackend) writes into
// an *image.RGBA; a future gpuBackend issues GL draw calls.
//
// Value-receiver methods with pointer-inside mutation (via `*canvasState`
// and the `layers` tracker pointer) so copies still share state — same
// convention the pre-refactor imageCanvas used.
type imageCanvas struct {
	*canvasState
	// backend is where pixels actually get produced. Never nil in a
	// well-constructed canvas; the constructor helpers (Begin /
	// NewImageCanvas / newImageCanvasForTest) wire a cpuBackend by
	// default.
	backend RasterBackend
	// layers tracks the frontend-side (save-depth, clip-mask) pairs so
	// Restore knows when to pop and pass the right mask to
	// backend.PopLayer. Pointer so value-copies of imageCanvas share
	// the stack — matches the earlier `*layerStack` convention.
	layers *frontendLayerTracker
}

// frontendLayerTracker records what the frontend needs to know about
// each active layer — the state-stack depth at which it was pushed,
// and any clip mask attached by ClipPath. The BACKEND owns the actual
// layer buffers / FBOs.
type frontendLayerTracker struct {
	entries []frontendLayerEntry
}

type frontendLayerEntry struct {
	saveDepth int
	mask      *image.Alpha
}

func newFrontendLayerTracker() *frontendLayerTracker { return &frontendLayerTracker{} }

// SaveLayer composes:
//  1. State-stack Save + narrow clip to bounds.
//  2. backend.PushLayer with the physical bounds and the clip in effect.
//  3. Record (saveDepth, nil-mask) in the frontend tracker.
//
// Returns the depth to pass to RestoreTo. See canvas_layer.go for the
// composite-time paint semantics.
//
// **The clip applies to the COMPOSITE, not to the layer's content — and
// for a filtered layer those are different rects.** An unfiltered layer
// is pixel-local: a pixel of the result depends only on the same pixel of
// the content, so allocating (and clipping) to bounds ∩ clip is a free
// win. An ImageFilter breaks that — the blur at a pixel reads its
// NEIGHBORS. Feed it a slice cut at the dirty rect and it blurs the cut
// edge, so a partial repaint renders a shadow the full repaint never
// would. Filtered layers therefore get the whole of `bounds` as both
// allocation and content clip, and the outer clip is re-applied at
// composite time by the backend.
func (c imageCanvas) SaveLayer(bounds Rect, paint Paint) int {
	if c.backend == nil || c.layers == nil {
		// Degenerate — still balance the state stack.
		depth := c.canvasState.Save()
		c.canvasState.ClipRect(bounds)
		return depth
	}
	top := c.topCopy()
	physBounds := top.matrix.TransformRect(bounds)
	physAlloc := top.clip.Intersect(physBounds)
	if paint.ImageFilter != nil {
		physAlloc = physBounds.Intersect(c.canvasState.surfaceBounds())
	}
	physRect := rectToPixelRectOut(physAlloc)
	depth := c.canvasState.Save()
	c.canvasState.ClipRect(bounds)
	if paint.ImageFilter != nil {
		// Widen the content clip back to the layer itself: everything drawn
		// into it is cropped to `clip` when it composites, so nothing
		// escapes — it just gets to influence the filter first.
		c.canvasState.setClip(physAlloc)
	}
	c.backend.PushLayer(physRect, rectToPixelRectOut(top.clip), paint)
	c.layers.entries = append(c.layers.entries, frontendLayerEntry{saveDepth: depth})
	return depth
}

// ClipPath narrows the current clip to the interior of `path`
// (non-zero winding + AA). Push a default-paint layer sized to the
// path's transformed bounds, then attach an alpha coverage mask
// rasterized from the same path. The mask lives on the frontend
// tracker; backend.PopLayer receives it at composite time and applies
// DstIn semantics.
func (c imageCanvas) ClipPath(path *Path) {
	if c.backend == nil || c.layers == nil || path == nil || path.IsEmpty() {
		return
	}
	// Compute physical bounds + mask BEFORE pushing so we can capture
	// the outer matrix. After SaveLayer the top-of-stack is the newly
	// pushed frame with a narrower clip.
	logicalBounds := path.Bounds()
	top := c.topCopy()
	physBounds := top.matrix.TransformRect(logicalBounds)
	physClip := top.clip.Intersect(physBounds)
	physRect := image.Rect(
		int(math.Floor(float64(physClip.X))),
		int(math.Floor(float64(physClip.Y))),
		int(math.Ceil(float64(physClip.X+physClip.W))),
		int(math.Ceil(float64(physClip.Y+physClip.H))),
	)
	project := func(p Point) Point { return top.matrix.TransformPoint(p) }
	subs := path.flatten(project)
	mask := rasterizePathToAlpha(subs, physRect)
	// SaveLayer with default paint — composite step just blits mask-
	// multiplied content through SrcOver / Alpha=1.
	c.SaveLayer(logicalBounds, Paint{})
	if n := len(c.layers.entries); n > 0 {
		c.layers.entries[n-1].mask = mask
	}
}

// restoreLayers pops any layer frames whose saveDepth ≥ keepDepth,
// letting the backend composite each back onto its parent using the
// paint remembered at PushLayer time and the mask attached by
// ClipPath (if any). Called from Restore / RestoreTo BEFORE unwinding
// the state stack — matches Skia's contract that composite reads the
// layer's own clip/matrix.
func (c imageCanvas) restoreLayers(keepDepth int) {
	if c.layers == nil || c.backend == nil {
		return
	}
	for len(c.layers.entries) > 0 {
		top := c.layers.entries[len(c.layers.entries)-1]
		if top.saveDepth < keepDepth {
			return
		}
		c.layers.entries = c.layers.entries[:len(c.layers.entries)-1]
		c.backend.PopLayer(top.mask)
	}
}

// Restore overrides the promoted canvasState.Restore so layer-aware
// composite runs before the state stack unwinds.
func (c imageCanvas) Restore() {
	if c.canvasState == nil {
		return
	}
	if len(c.canvasState.stack) <= 1 {
		return
	}
	c.restoreLayers(len(c.canvasState.stack) - 1)
	c.canvasState.Restore()
}

// RestoreTo overrides the promoted canvasState.RestoreTo. Composites
// every layer pushed at depth ≥ the target depth before unwinding.
func (c imageCanvas) RestoreTo(depth int) {
	if c.canvasState == nil {
		return
	}
	if depth < 1 {
		depth = 1
	}
	if depth >= len(c.canvasState.stack) {
		return
	}
	c.restoreLayers(depth)
	c.canvasState.RestoreTo(depth)
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

var (
	// RWMutex (not Mutex) because GetFontFacesFor takes only an RLock
	// on the cache-hit fast path. Hits dominate by orders of magnitude
	// once the chain is built — every text draw call goes through here.
	fontCacheMu sync.RWMutex
	fontCache   = map[fontCacheKey]font.Face{}
	fontChain   = map[fontCacheKey][]font.Face{}
	// fontInitialized tracks whether fontParsed has been populated —
	// either by the default lazy goregular init or by a prior
	// LoadFontFromFile / LoadSystemCJKFont call.
	fontInitialized bool
	fontParsed      *opentype.Font
	// fontUsingDefaultInit marks whether the active font came from the
	// lazy default init path in GetFontFacesFor (system CJK probe or
	// bundled goregular), as opposed to explicit app-level font loads.
	fontUsingDefaultInit bool
	// fontDefaultIsSystemCJK narrows fontUsingDefaultInit to the case
	// where the probe actually FOUND a system CJK font (rather than
	// falling through to goregular). populateAutoFallbacksUnlocked reads
	// it to reuse that font instead of loading the same file twice.
	fontDefaultIsSystemCJK bool
	// fontAutoLoadSystemCJK toggles whether lazy init probes system CJK
	// fonts before falling back to bundled goregular.
	fontAutoLoadSystemCJK = true
	// test hook for system CJK probing in lazy init.
	probeSystemCJKFontUnlocked = tryLoadCJKUnlocked
	// Keep font file handles alive for ParseReaderAt-backed fonts.
	fontPrimarySource   io.Closer
	fontFallbacks       []*opentype.Font
	fontFallbackSources []io.Closer
	// autoFallbacks / autoFallbackSources are populated lazily on first
	// font access with a per-OS symbol font (covers chevrons, ⟳, etc.)
	// plus bundled goregular as a last-resort Latin catch-all. They are
	// appended to the per-glyph fallback chain AFTER user-registered
	// fontFallbacks, so explicit fallbacks always win on overlap. The
	// existing per-glyph dispatch in faceForRune means each fallback is
	// only consulted for glyphs the higher-priority faces lack.
	autoFallbacks       []*opentype.Font
	autoFallbackSources []io.Closer
	autoFallbacksInit   bool
	// test hook for symbol-font probing in lazy init.
	probeSymbolFontUnlocked = tryLoadSymbolFontUnlocked
	// test hook for complex-script (Arabic / Hebrew / Thai / Indic)
	// probing in lazy init.
	probeComplexScriptFontUnlocked = tryLoadComplexScriptFontUnlocked
	fontVariants                   = map[fontVariantKey]*opentype.Font{}
	fontVariantSources             = map[fontVariantKey]io.Closer{}
	// fontFamilyNames maps the lowercase family key to the display name
	// given at registration (drives RegisteredFontFamilies).
	fontFamilyNames = map[string]string{}
	// defaultWeightFonts holds bundled gofont weights (medium=500, bold=700)
	// keyed by FontWeight, so a Font with an empty Family but a non-normal
	// Weight resolves to the right glyphs without an explicit
	// LoadFontVariantFromBytes call. Populated lazily inside
	// populateAutoFallbacksUnlocked. The system CJK font (loaded as
	// fontParsed) does not provide weight axes, so this table is what
	// drives qui.ThemeFont(TextLabel)'s weight=500 request.
	defaultWeightFonts = map[FontWeight]*opentype.Font{}
	fontErr            error
)

type fontCacheKey struct {
	family   string
	size     float32
	weight   FontWeight
	italic   bool
	features string
	variants string
}

type fontVariantKey struct {
	family string
	weight FontWeight
	italic bool
}

// markFontOnceDone is called by font.go's installFont after it
// successfully loads a user font so the lazy init inside
// GetFontFace doesn't later overwrite it with goregular.
func markFontOnceDone() {
	fontInitialized = true
	fontUsingDefaultInit = false
	// The primary is now an app-supplied font, so the auto-fallback chain
	// must probe for its own CJK entry rather than aliasing the primary.
	fontDefaultIsSystemCJK = false
}

func GetFontFace(size float32) font.Face {
	return GetFontFaceFor(Font{Size: size})
}

// GetFontFaceFor resolves a font face for a full Font spec.
func GetFontFaceFor(spec Font) font.Face {
	faces := GetFontFacesFor(spec)
	if len(faces) == 0 {
		return basicfont.Face7x13
	}
	return faces[0]
}

// GetFontFaces returns primary + fallback faces for the requested size.
// Order is stable: primary first, then fallback registration order.
func GetFontFaces(size float32) []font.Face {
	return GetFontFacesFor(Font{Size: size})
}

// GetFontFacesFor returns primary + fallback faces for the requested
// font spec. Order is stable: primary first, then fallback registration
// order.
//
// The returned slice aliases the internal cache — callers must not
// mutate or append. Every call site today only iterates with `for _,
// face := range faces`, so handing back the cached slice (instead of
// a defensive copy) saves ~2 MB of allocations per 10s in a busy
// editor without changing observable behavior. If you need to keep a
// mutated copy, do `append([]font.Face(nil), faces...)` at the call
// site.
func GetFontFacesFor(spec Font) []font.Face {
	if spec.Size <= 0 {
		spec.Size = 14
	}
	key := makeFontCacheKey(spec)

	// Fast path: cache hit under a read lock. The cached chain is
	// set-once + read-many per (font spec, fallback set) — once the
	// font system is initialized and the chain is built, this is the
	// only branch the hot draw loop takes.
	fontCacheMu.RLock()
	if fontInitialized && fontErr == nil {
		if faces, ok := fontChain[key]; ok {
			fontCacheMu.RUnlock()
			return faces
		}
	}
	fontCacheMu.RUnlock()

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if !fontInitialized {
		// Default: probe system CJK fonts first so non-Latin text
		// renders out of the box. Fall back to goregular when no
		// candidate is installed (Latin-only, but at least something
		// draws). Apps can still override via LoadFontFromFile /
		// LoadFontFromBytes before any Draw.
		fontDefaultIsSystemCJK = false
		if fontAutoLoadSystemCJK {
			if f, src := probeSystemCJKFontUnlocked(); f != nil {
				fontParsed = f
				fontErr = nil
				replacePrimaryFontSourceUnlocked(src)
				fontDefaultIsSystemCJK = true
			} else {
				fontParsed, fontErr = opentype.Parse(goregular.TTF)
				replacePrimaryFontSourceUnlocked(nil)
			}
		} else {
			fontParsed, fontErr = opentype.Parse(goregular.TTF)
			replacePrimaryFontSourceUnlocked(nil)
		}
		fontInitialized = true
		fontUsingDefaultInit = true
	}
	populateAutoFallbacksUnlocked()
	if fontErr != nil {
		return []font.Face{basicfont.Face7x13}
	}
	// Re-check after acquiring the write lock — another goroutine may
	// have built the chain between our read-unlock and write-lock.
	if faces, ok := fontChain[key]; ok {
		return faces
	}
	primaryParsed := selectPrimaryFontUnlocked(spec)
	primary, err := opentype.NewFace(primaryParsed, &opentype.FaceOptions{
		Size: float64(spec.Size),
		DPI:  72,
		// Keep glyph advances scale-proportional across DPI factors so
		// widget-side text metrics (logical coordinates) stay aligned
		// with the HiDPI draw path (scaled font size).
		Hinting: font.HintingNone,
	})
	if err != nil {
		return []font.Face{basicfont.Face7x13}
	}
	chain := []font.Face{primary}
	appendFallback := func(parsed *opentype.Font) {
		if parsed == nil || parsed == primaryParsed {
			return
		}
		fallback, err := opentype.NewFace(parsed, &opentype.FaceOptions{
			Size:    float64(spec.Size),
			DPI:     72,
			Hinting: font.HintingNone,
		})
		if err != nil {
			return
		}
		chain = append(chain, fallback)
	}
	for _, parsed := range fontFallbacks {
		appendFallback(parsed)
	}
	// Auto fallbacks run last so explicit user fallbacks always win for
	// glyphs both can render. Per-glyph dispatch in faceForRune skips
	// these entries when the primary or a user fallback already covers
	// the codepoint, so the cost on the hot path is one map lookup per
	// rune until a hit.
	for _, parsed := range autoFallbacks {
		appendFallback(parsed)
	}
	fontCache[key] = primary
	fontChain[key] = chain
	return chain
}

// populateAutoFallbacksUnlocked seeds the per-glyph fallback chain with
// (a) the OS-best symbol font (Apple Symbols on macOS, Noto Sans Symbols
// 2 / DejaVu on Linux, Segoe UI Symbol on Windows) and (b) bundled
// goregular for Latin / chevron coverage. Idempotent — runs once per
// process. The symbol-font probe is best-effort; goregular is always
// available because it's compiled into the binary.
//
// Caller must hold fontCacheMu.
func populateAutoFallbacksUnlocked() {
	if autoFallbacksInit {
		return
	}
	autoFallbacksInit = true
	if f, src := probeSymbolFontUnlocked(); f != nil {
		autoFallbacks = append(autoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, src)
	}
	if f, err := opentype.Parse(goregular.TTF); err == nil {
		autoFallbacks = append(autoFallbacks, f)
		autoFallbackSources = append(autoFallbackSources, nil)
	}
	// System CJK font as a fallback (not just as the lazy-init primary).
	// In the default setup the CJK font IS the primary and covers Latin +
	// CJK, so this is redundant. But once an app swaps the primary to a
	// Latin-only face — e.g. SetDefaultFont / jetbrainsmono.Use() — the
	// primary no longer covers Chinese/Japanese/Korean, and without a CJK
	// entry in the fallback chain those glyphs render as tofu. Keeping CJK
	// here means a font swap never silently drops CJK coverage. Gated on
	// the same auto-load toggle so apps that opted out don't pay the probe.
	if fontAutoLoadSystemCJK {
		if fontDefaultIsSystemCJK && fontParsed != nil {
			// The lazy init above already probed the system CJK font into
			// the primary slot. Reuse that POINTER rather than calling the
			// probe again: a second call re-reads and re-parses the same
			// file, and a system CJK collection is tens of megabytes (its
			// bytes stay resident for the font's lifetime, so the duplicate
			// was resident too). appendFallback drops this entry while it
			// is still the primary, and it starts counting the moment an
			// app swaps the primary to a Latin-only face — which is the
			// only reason the entry exists.
			//
			// The source stays nil: the handle belongs to
			// fontPrimarySource, and recording it twice would close it
			// twice on reset.
			autoFallbacks = append(autoFallbacks, fontParsed)
			autoFallbackSources = append(autoFallbackSources, nil)
		} else if f, src := probeSystemCJKFontUnlocked(); f != nil {
			autoFallbacks = append(autoFallbacks, f)
			autoFallbackSources = append(autoFallbackSources, src)
		}
	}
	// Complex-script coverage (Arabic / Hebrew / Thai / Indic). Neither
	// the bundled Latin face nor a CJK face carries these, so without
	// this entry an app that switches to Arabic renders a screen of tofu
	// while bidi, shaping and RTL alignment all work correctly on glyphs
	// that do not exist. Gated on the same toggle as the CJK probe: an
	// app that opted out of automatic system-font probing gets neither.
	if fontAutoLoadSystemCJK {
		if f, src := probeComplexScriptFontUnlocked(); f != nil {
			autoFallbacks = append(autoFallbacks, f)
			autoFallbackSources = append(autoFallbackSources, src)
		}
	}
	if len(defaultWeightFonts) == 0 {
		if f, err := opentype.Parse(gomedium.TTF); err == nil {
			defaultWeightFonts[FontWeightMedium] = f
		}
		if f, err := opentype.Parse(gobold.TTF); err == nil {
			defaultWeightFonts[FontWeightBold] = f
		}
	}
}

// setDefaultWeightFontUnlocked overrides (or, with f == nil, removes) the
// bundled-weight face used to satisfy empty-Family + non-normal-weight
// lookups. SetDefaultFont calls this so a swapped-in family's Medium /
// Bold typescales resolve to that family's own weights instead of the
// Go font's gomedium / gobold. Caller must hold fontCacheMu.
func setDefaultWeightFontUnlocked(weight FontWeight, f *opentype.Font) {
	// Ensure the bundled defaults are seeded first so a partial override
	// (e.g. only Medium supplied) doesn't leave Bold pointing at nothing.
	populateAutoFallbacksUnlocked()
	if f == nil {
		delete(defaultWeightFonts, weight)
		return
	}
	defaultWeightFonts[weight] = f
}

// clearAutoFallbacksUnlocked drops auto-installed fallbacks and closes
// any file-backed sources. Used by tests that swap out the symbol-font
// probe; production code never calls this.
func clearAutoFallbacksUnlocked() {
	for _, src := range autoFallbackSources {
		closeCloser(src)
	}
	autoFallbacks = nil
	autoFallbackSources = nil
	autoFallbacksInit = false
	clearFontFaceCachesUnlocked()
}

func selectPrimaryFontUnlocked(spec Font) *opentype.Font {
	// No family + non-normal weight: consult the bundled-weight table so
	// MD3 Label/Title typescales (weight 500) actually pick up gomedium
	// instead of falling through to the regular default. goregular's
	// "Weight" axis doesn't exist, so without this we silently lose the
	// visual weight distinction.
	defaultFace := func() *opentype.Font {
		if w := spec.effectiveWeight(); w != FontWeightNormal {
			if f, ok := defaultWeightFonts[w]; ok && f != nil {
				return f
			}
		}
		return fontParsed
	}
	family := strings.TrimSpace(spec.Family)
	if family == "" {
		return defaultFace()
	}
	want := fontVariantKey{
		family: strings.ToLower(family),
		weight: spec.effectiveWeight(),
		italic: spec.Italic,
	}
	if f, ok := fontVariants[want]; ok && f != nil {
		return f
	}
	// Nearest fallback: same family and closest weight, prefer exact italic.
	// A family with no registered faces at all (a CSS generic like
	// sans-serif, a font the app never loaded) is the default family, so
	// it keeps the requested weight rather than collapsing to regular.
	best := defaultFace()
	bestScore := int(^uint(0) >> 1)
	for k, f := range fontVariants {
		if f == nil || k.family != want.family {
			continue
		}
		score := absInt(int(k.weight) - int(want.weight))
		if k.italic != want.italic {
			score += 1000
		}
		if score < bestScore {
			bestScore = score
			best = f
		}
	}
	return best
}

func makeFontCacheKey(spec Font) fontCacheKey {
	return fontCacheKey{
		family:   strings.ToLower(strings.TrimSpace(spec.Family)),
		size:     spec.Size,
		weight:   spec.effectiveWeight(),
		italic:   spec.Italic,
		features: canonicalFeatureKey(spec.Features),
		variants: canonicalVariationKey(spec.Variations),
	}
}

func canonicalFeatureKey(features []string) string {
	if len(features) == 0 {
		return ""
	}
	cp := append([]string(nil), features...)
	sort.Strings(cp)
	return strings.Join(cp, ",")
}

func canonicalVariationKey(vars map[string]float32) string {
	if len(vars) == 0 {
		return ""
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(fmt.Sprintf("%g", vars[k]))
	}
	return b.String()
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Clear fills the current clip with `color`. Routes through the same
// fillRectBlend the rest of the renderer uses, so opacity / hole-punch
// semantics stay consistent with FillRect.
// Clear fills the CURRENT CLIP (not the entire framebuffer) with `color`.
// This matches the pre-refactor semantics — Window.Step relies on
// Clear-clipped-to-dirty-region so a partial repaint (e.g. hover) leaves
// the rest of the framebuffer intact from the previous frame. Calling
// backend.Clear directly would wipe everything and defeat retained
// rendering.
func (c imageCanvas) Clear(color Color) {
	if c.backend == nil {
		return
	}
	dst := rectToPixelRect(c.ClipBounds())
	if dst.Empty() {
		return
	}
	c.backend.FillRect(dst, color)
}

// DrawShape is the single rasterization entry point. Every shape +
// paint combination dispatches here; the public convenience methods
// (FillRect / StrokeRoundedRect / …) build a (shape, paint) pair and
// forward. Adding a new visual property to Paint means touching ONE
// switch, not eight method tables.
func (c imageCanvas) DrawShape(shape Shape, paint Paint) {
	if c.backend == nil {
		return
	}
	top := c.topCopy()
	s := top.matrix.avgScale()
	axisAligned := top.matrix.IsAxisAligned()
	// Fill primitives with a shader route through the path rasterizer so
	// gradient handling lives in exactly one place. Strokes always use
	// Paint.Color — Paint.Shader is fill-only in v1.
	shaderFill := paint.Shader != nil && paint.Style != PaintStroke
	switch sh := shape.(type) {
	case ShapeRect:
		if !axisAligned || shaderFill {
			// Rotated rect / shader-fill rect → convert to a closed Path
			// and fall through to the path rasterizer.
			path := NewPath().AddRect(Rect(sh))
			c.DrawShape(ShapePath{Path: path}, paint)
			return
		}
		physR := projectAxisAlignedRect(top.matrix, Rect(sh))
		if paint.Style == PaintStroke {
			if paint.StrokeWidth <= 0 {
				return
			}
			c.backend.StrokeRect(physR, top.clip, paint.Color, paint.StrokeWidth*s)
			return
		}
		dst := rectToPixelRect(physR).Intersect(rectToPixelRect(top.clip))
		if dst.Empty() {
			return
		}
		c.backend.FillRect(dst, paint.Color)
	case ShapeRRect:
		if !axisAligned || shaderFill {
			path := NewPath().AddRRect(sh.Rect, sh.Radius)
			c.DrawShape(ShapePath{Path: path}, paint)
			return
		}
		physRect, physRadius := projectAARoundedShape(top.matrix, sh.Rect, sh.Radius)
		if paint.Style == PaintStroke {
			if paint.StrokeWidth <= 0 {
				return
			}
			c.backend.StrokeRoundedRect(physRect, physRadius, top.clip, paint.Color, paint.StrokeWidth*s)
			return
		}
		c.backend.FillRoundedRect(physRect, physRadius, top.clip, paint.Color)
	case ShapeLine:
		c.backend.DrawLine(
			top.matrix.TransformPoint(sh.P1),
			top.matrix.TransformPoint(sh.P2),
			top.clip, paint.Color, paint.StrokeWidth*s, paint.Cap,
		)
	case ShapePolyline:
		if len(sh) < 2 {
			return
		}
		// Project points to physical, then dispatch as a single-sub
		// path stroke so join / cap options come through uniformly.
		pts := make([]Point, len(sh))
		for i, p := range sh {
			pts[i] = top.matrix.TransformPoint(p)
		}
		c.backend.StrokePath([][]Point{pts}, []bool{false}, rectToPixelRect(top.clip),
			paint.StrokeWidth*s, paint)
	case ShapePath:
		if sh.Path == nil || sh.Path.IsEmpty() {
			return
		}
		pxClip := rectToPixelRect(top.clip)
		if pxClip.Empty() {
			return
		}
		project := func(p Point) Point { return top.matrix.TransformPoint(p) }
		subs := sh.Path.flatten(project)
		if paint.Style == PaintStroke {
			closedFlags := make([]bool, len(subs))
			for i, sub := range subs {
				closedFlags[i] = sh.Path.subIsClosed(sub)
			}
			c.backend.StrokePath(subs, closedFlags, pxClip, paint.StrokeWidth*s, paint)
			return
		}
		// Fill: non-zero winding rule with 4x supersampled AA. When a
		// shader is set, sampling happens at the pixel center in LOGICAL
		// coords — the inverse of the current matrix carries the physical
		// pixel back to shader space.
		inv := IdentityMatrix()
		if paint.Shader != nil {
			var ok bool
			inv, ok = top.matrix.Invert()
			if !ok {
				return // degenerate transform — skip shader fill
			}
		}
		c.backend.FillPath(subs, pxClip, paint, inv)
	}
}

// fillPolygonAA is the path-fill rasterizer. Walks each scanline at
// `subSamples` subpixel offsets, collects signed edge crossings
// (dy > 0 = +1, dy < 0 = -1) per subscan, sorts the crossings by X,
// and walks left-to-right tracking the winding count. For each pixel
// column it accumulates the fraction of subscans whose winding count
// is "inside" (non-zero for FillNonZero, odd for FillEvenOdd) — that
// fraction is the pixel's coverage.
//
// AA contract:
//   - aa == true: 4 subscans per pixel row, edge AA via fractional
//     subscan coverage, sub-pixel X edges blended via partial-pixel
//     fill at the leftmost / rightmost crossing in each row.
//   - aa == false: 1 subscan, binary coverage — matches the original
//     even-odd rasterizer behavior so pixel-perfect tests stay stable.
//
// Inspired by Skia's SkAnalyticEdge walker and the classic
// "supersampled scanline" approach in Foley & van Dam. Skia uses
// SkRunHead for run-length encoded coverage on horizontal spans; ours
// is simpler but the shape-level visual difference at 4x subsampling is
// small enough to defer that optimization.
// fillPolygonAA is a legacy alias — new callers should use fillPolygon
// which accepts a Shader / invMatrix pair. Kept because a few internal
// callers still pass a solid color.
func fillPolygonAA(img *image.RGBA, clip image.Rectangle, subs [][]Point, color Color, rule FillRule, aa bool) {
	fillPolygon(img, clip, subs, color, nil, IdentityMatrix(), rule, aa)
}

// fillPolygon is the unified path rasterizer. Coverage is computed once
// per row (regardless of solid vs shader) using a 4× supersampled
// scanline; the composite step branches ONCE per row on `shader == nil`
// so the shader-sampling path stays a tight per-pixel loop when active.
// When shader is nil, invMatrix is ignored — solid color goes through
// the straight blendPixel path.
func fillPolygon(img *image.RGBA, clip image.Rectangle, subs [][]Point, solid Color, shader Shader, invMatrix Matrix, rule FillRule, aa bool) {
	if len(subs) == 0 {
		return
	}
	// Bounding box across all subpaths, intersected with the clip.
	first := true
	var minX, minY, maxX, maxY float32
	for _, sub := range subs {
		for _, p := range sub {
			if first {
				minX, minY, maxX, maxY = p.X, p.Y, p.X, p.Y
				first = false
				continue
			}
			if p.X < minX {
				minX = p.X
			}
			if p.X > maxX {
				maxX = p.X
			}
			if p.Y < minY {
				minY = p.Y
			}
			if p.Y > maxY {
				maxY = p.Y
			}
		}
	}
	if first {
		return
	}
	y0 := int(math.Floor(float64(minY)))
	y1 := int(math.Ceil(float64(maxY)))
	x0 := int(math.Floor(float64(minX)))
	x1 := int(math.Ceil(float64(maxX)))
	if y0 < clip.Min.Y {
		y0 = clip.Min.Y
	}
	if y1 > clip.Max.Y {
		y1 = clip.Max.Y
	}
	if x0 < clip.Min.X {
		x0 = clip.Min.X
	}
	if x1 > clip.Max.X {
		x1 = clip.Max.X
	}
	if y0 >= y1 || x0 >= x1 {
		return
	}

	subSamples := 4
	if !aa {
		subSamples = 1
	}
	rowWidth := x1 - x0
	coverage := make([]float32, rowWidth) // per-column accumulator (0..subSamples)
	type signedX struct {
		x float32
		d int8 // +1 or -1
	}
	crossings := make([]signedX, 0, 16)
	rgba := toRGBA(solid)
	pxRow := image.Rect(x0, 0, x1, 0)

	scanInside := func(count int) bool {
		if rule == FillEvenOdd {
			return count%2 != 0
		}
		return count != 0
	}

	for y := y0; y < y1; y++ {
		for i := range coverage {
			coverage[i] = 0
		}
		for sub := 0; sub < subSamples; sub++ {
			// Sample at the center of each subscan: y + (sub+0.5)/subSamples
			fy := float32(y) + (float32(sub)+0.5)/float32(subSamples)
			crossings = crossings[:0]
			for _, poly := range subs {
				if len(poly) < 2 {
					continue
				}
				for i := 0; i < len(poly)-1; i++ {
					p0 := poly[i]
					p1 := poly[i+1]
					dy := p1.Y - p0.Y
					if dy == 0 {
						continue
					}
					// Half-open scan rule: include p.Y == fy on the
					// ascending side only. Avoids double-counting a
					// vertex shared by two segments.
					if p0.Y > p1.Y {
						if fy < p1.Y || fy >= p0.Y {
							continue
						}
					} else {
						if fy < p0.Y || fy >= p1.Y {
							continue
						}
					}
					t := (fy - p0.Y) / dy
					x := p0.X + t*(p1.X-p0.X)
					sign := int8(1)
					if dy < 0 {
						sign = -1
					}
					crossings = append(crossings, signedX{x: x, d: sign})
				}
			}
			if len(crossings) < 2 {
				continue
			}
			// Insertion sort by x — crossings are short (<32 typically).
			for i := 1; i < len(crossings); i++ {
				v := crossings[i]
				j := i - 1
				for j >= 0 && crossings[j].x > v.x {
					crossings[j+1] = crossings[j]
					j--
				}
				crossings[j+1] = v
			}
			// Walk crossings left to right, tracking winding count.
			winding := 0
			prevX := crossings[0].x
			for i := 0; i < len(crossings); i++ {
				curX := crossings[i].x
				if scanInside(winding) {
					// Fill subpixel coverage for the [prevX, curX] span
					// into the per-column accumulator.
					sx0 := prevX
					sx1 := curX
					if sx0 < float32(x0) {
						sx0 = float32(x0)
					}
					if sx1 > float32(x1) {
						sx1 = float32(x1)
					}
					if sx1 > sx0 {
						addSubpixelSpan(coverage, x0, sx0, sx1)
					}
				}
				winding += int(crossings[i].d)
				prevX = curX
			}
		}
		// Composite the per-row coverage into the framebuffer. Pixels
		// with cov >= subSamples are solid; in-between are AA-blended.
		// Branch ONCE per row on shader != nil so the hot pixel loop
		// stays tight for both solid and shader paths.
		_ = pxRow
		if shader == nil {
			for col := 0; col < rowWidth; col++ {
				cov := coverage[col]
				if cov <= 0 {
					continue
				}
				alpha := cov / float32(subSamples)
				if alpha > 1 {
					alpha = 1
				}
				blendPixel(img, x0+col, y, rgba, alpha)
			}
		} else {
			fy := float32(y) + 0.5
			for col := 0; col < rowWidth; col++ {
				cov := coverage[col]
				if cov <= 0 {
					continue
				}
				alpha := cov / float32(subSamples)
				if alpha > 1 {
					alpha = 1
				}
				px := x0 + col
				logical := invMatrix.TransformPoint(Point{X: float32(px) + 0.5, Y: fy})
				c := shader.ColorAt(logical.X, logical.Y)
				blendPixel(img, px, y, toRGBA(c), alpha)
			}
		}
	}
}

// rasterizePathToAlpha turns a flattened subpath set into an
// *image.Alpha coverage mask. Every scanline collects signed edge
// crossings at 4 subpixel offsets (matching the AA subsample count of
// fillPolygonAA) and accumulates a per-column coverage; the mask
// stores coverage/subSamples * 255 per pixel. Non-zero winding rule.
// bounds is the alpha's Rect — the mask covers exactly that region
// and unbounded coordinates outside are clipped by the caller (which
// pairs the mask with a same-bounds image via compositeLayerMasked).
func rasterizePathToAlpha(subs [][]Point, bounds image.Rectangle) *image.Alpha {
	alpha := image.NewAlpha(bounds)
	if len(subs) == 0 || bounds.Empty() {
		return alpha
	}
	const subSamples = 4
	y0 := bounds.Min.Y
	y1 := bounds.Max.Y
	x0 := bounds.Min.X
	x1 := bounds.Max.X
	rowWidth := x1 - x0
	coverage := make([]float32, rowWidth)
	type signedX struct {
		x float32
		d int8
	}
	crossings := make([]signedX, 0, 16)
	for y := y0; y < y1; y++ {
		for i := range coverage {
			coverage[i] = 0
		}
		for sub := 0; sub < subSamples; sub++ {
			fy := float32(y) + (float32(sub)+0.5)/float32(subSamples)
			crossings = crossings[:0]
			for _, poly := range subs {
				if len(poly) < 2 {
					continue
				}
				for i := 0; i < len(poly)-1; i++ {
					p0 := poly[i]
					p1 := poly[i+1]
					dy := p1.Y - p0.Y
					if dy == 0 {
						continue
					}
					if p0.Y > p1.Y {
						if fy < p1.Y || fy >= p0.Y {
							continue
						}
					} else {
						if fy < p0.Y || fy >= p1.Y {
							continue
						}
					}
					t := (fy - p0.Y) / dy
					x := p0.X + t*(p1.X-p0.X)
					sign := int8(1)
					if dy < 0 {
						sign = -1
					}
					crossings = append(crossings, signedX{x: x, d: sign})
				}
			}
			if len(crossings) < 2 {
				continue
			}
			for i := 1; i < len(crossings); i++ {
				v := crossings[i]
				j := i - 1
				for j >= 0 && crossings[j].x > v.x {
					crossings[j+1] = crossings[j]
					j--
				}
				crossings[j+1] = v
			}
			winding := 0
			prevX := crossings[0].x
			for i := 0; i < len(crossings); i++ {
				curX := crossings[i].x
				// Non-zero winding rule (matches fillPolygonAA default).
				if winding != 0 {
					sx0 := prevX
					sx1 := curX
					if sx0 < float32(x0) {
						sx0 = float32(x0)
					}
					if sx1 > float32(x1) {
						sx1 = float32(x1)
					}
					if sx1 > sx0 {
						addSubpixelSpan(coverage, x0, sx0, sx1)
					}
				}
				winding += int(crossings[i].d)
				prevX = curX
			}
		}
		rowBase := (y - alpha.Rect.Min.Y) * alpha.Stride
		for col := 0; col < rowWidth; col++ {
			cov := coverage[col]
			if cov <= 0 {
				continue
			}
			v := cov / float32(subSamples)
			if v > 1 {
				v = 1
			}
			alpha.Pix[rowBase+col] = uint8(v * 255)
		}
	}
	return alpha
}

// addSubpixelSpan adds [sx0, sx1] (in absolute X) to the per-column
// coverage accumulator anchored at xStart. Whole-pixel runs add 1.0;
// fractional ends add the partial coverage of the cropped pixel.
func addSubpixelSpan(coverage []float32, xStart int, sx0, sx1 float32) {
	if sx1 <= sx0 {
		return
	}
	left := int(math.Floor(float64(sx0)))
	right := int(math.Ceil(float64(sx1)))
	if left < xStart {
		left = xStart
	}
	if right > xStart+len(coverage) {
		right = xStart + len(coverage)
	}
	for col := left; col < right; col++ {
		// Per-pixel overlap = min(sx1, col+1) - max(sx0, col).
		a := float32(col)
		b := a + 1
		if a < sx0 {
			a = sx0
		}
		if b > sx1 {
			b = sx1
		}
		if b > a {
			coverage[col-xStart] += b - a
		}
	}
}

// strokePolyline rasterizes a polyline (open or closed) with the
// given Paint cap / join settings. Points are projected through m
// (caller passes either the canvas matrix for unprojected polylines
// or the identity for already-projected path subs).
//
// CapRound is the existing capsule rasterizer (drawLineInto). CapButt
// trims the rounded cap; CapSquare extends each end by halfW so the
// stroke renders with a flat-ended square.
//
// Joins: at width 1 they all look the same (within half a pixel) so
// we skip the join geometry for width < 1.5 — this saves a noticeable
// chunk of pixel work on grid lines / hairlines. Wider strokes get
// JoinMiter (with miterLimit fallback to bevel), JoinBevel (an outer-
// edge filled triangle), or JoinRound (a disk stamp at the corner —
// reuses stampDot).
func strokePolyline(img *image.RGBA, pxClip image.Rectangle, pts []Point, m Matrix, width float32, rgba color.RGBA, cap StrokeCap, join StrokeJoin, miterLimit float32, closed bool) {
	if len(pts) < 2 {
		return
	}
	if miterLimit <= 0 {
		miterLimit = 10
	}
	// Project endpoints once so segment / join math operates in
	// physical pixels.
	proj := make([]Point, len(pts))
	for i := range pts {
		proj[i] = m.TransformPoint(pts[i])
	}
	halfW := width * 0.5
	// Hairline shortcut — caps/joins indistinguishable at width <= 1.5.
	thin := width < 1.5
	for i := 1; i < len(proj); i++ {
		segCap := cap
		// Interior segment endpoints render with cap=butt so the
		// per-segment capsule doesn't leak past the polyline's join.
		// Skia does the equivalent inside its hairline / stroker.
		if !thin {
			if i > 1 {
				// not the polyline's start — interior side is butt
			}
			if i < len(proj)-1 || closed {
				// not the polyline's end — interior side is butt
			}
		}
		drawLineInto(img, pxClip, proj[i-1], proj[i], width, rgba, segCap)
	}
	// Closed polyline — draw the closing segment + treat both ends as
	// joins, not caps.
	if closed && len(proj) >= 3 {
		drawLineInto(img, pxClip, proj[len(proj)-1], proj[0], width, rgba, cap)
	}
	if thin {
		return // caps/joins blend into the segment overlap at width 1
	}
	// Joins between consecutive segments.
	for i := 1; i < len(proj)-1; i++ {
		paintJoin(img, pxClip, proj[i-1], proj[i], proj[i+1], halfW, rgba, join, miterLimit)
	}
	if closed && len(proj) >= 3 {
		paintJoin(img, pxClip, proj[len(proj)-2], proj[len(proj)-1], proj[0], halfW, rgba, join, miterLimit)
		paintJoin(img, pxClip, proj[len(proj)-1], proj[0], proj[1], halfW, rgba, join, miterLimit)
	}
}

// paintJoin paints the corner geometry where segments (a -> b) and
// (b -> c) meet. For JoinRound this is a filled disk; for JoinBevel
// it's a filled triangle; for JoinMiter it's a quadrilateral extending
// to the intersection of the outer offset lines.
func paintJoin(img *image.RGBA, pxClip image.Rectangle, a, b, c Point, halfW float32, rgba color.RGBA, join StrokeJoin, miterLimit float32) {
	if join == JoinRound {
		stampDot(img, pxClip, b, halfW, rgba)
		return
	}
	// Unit vectors along the two segments.
	ux := b.X - a.X
	uy := b.Y - a.Y
	ulen := float32(math.Sqrt(float64(ux*ux + uy*uy)))
	vx := c.X - b.X
	vy := c.Y - b.Y
	vlen := float32(math.Sqrt(float64(vx*vx + vy*vy)))
	if ulen < 1e-4 || vlen < 1e-4 {
		return
	}
	ux /= ulen
	uy /= ulen
	vx /= vlen
	vy /= vlen
	// Outer corner: perpendicular to each segment, pointing AWAY from
	// the bend. Cross product (u x v) sign tells us which side.
	cross := ux*vy - uy*vx
	if cross == 0 {
		return // collinear segments — no join needed
	}
	// Perpendiculars (left-hand for u, left-hand for v).
	nu := Point{X: -uy, Y: ux}
	nv := Point{X: -vy, Y: vx}
	if cross < 0 {
		nu.X, nu.Y = -nu.X, -nu.Y
		nv.X, nv.Y = -nv.X, -nv.Y
	}
	outer1 := Point{X: b.X + nu.X*halfW, Y: b.Y + nu.Y*halfW}
	outer2 := Point{X: b.X + nv.X*halfW, Y: b.Y + nv.Y*halfW}

	if join == JoinMiter {
		// Find the intersection of the two outer offset lines.
		// Line 1: outer1 + t*(ux,uy)
		// Line 2: outer2 - s*(vx,vy)
		denom := ux*(-vy) - uy*(-vx)
		if denom != 0 {
			rx := outer2.X - outer1.X
			ry := outer2.Y - outer1.Y
			t := (rx*(-vy) - ry*(-vx)) / denom
			miter := Point{X: outer1.X + t*ux, Y: outer1.Y + t*uy}
			miterLen := dist(miter, b)
			if miterLen <= miterLimit*halfW {
				fillTriangle(img, pxClip, b, outer1, miter, rgba)
				fillTriangle(img, pxClip, b, miter, outer2, rgba)
				return
			}
		}
		// Fall through to bevel.
	}
	// Bevel.
	fillTriangle(img, pxClip, b, outer1, outer2, rgba)
}

// dist returns the Euclidean distance between two points.
func dist(p, q Point) float32 {
	dx := p.X - q.X
	dy := p.Y - q.Y
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

// fillTriangle rasterizes a triangle into img clipped to pxClip with
// 4x supersampled AA — small enough that we use the polygon
// rasterizer directly (one subpath, the three vertices).
func fillTriangle(img *image.RGBA, pxClip image.Rectangle, a, b, c Point, rgba color.RGBA) {
	col := Color{
		R: float32(rgba.R) / 255,
		G: float32(rgba.G) / 255,
		B: float32(rgba.B) / 255,
		A: float32(rgba.A) / 255,
	}
	sub := [][]Point{{a, b, c, a}}
	fillPolygonAA(img, pxClip, sub, col, FillNonZero, true)
}

// FillRect — convenience over DrawShape.
func (c imageCanvas) FillRect(rect Rect, color Color) {
	c.DrawShape(ShapeRect(rect), Paint{Color: color})
}

// fillRectBlend writes color into dst with the right Porter-Duff op for
// its alpha: stddraw.Src (replace) for opaque colors — the fast path the
// background fills take — and a manual per-pixel src-over for
// translucent colors. Without this the translucent state-layer overlays
// (DrawStateLayer, ripple tail) would REPLACE the widget's opaque fill
// with their own near-transparent alpha, leaking the framebuffer
// background through and producing the "filled button goes black on
// hover" artifact the render calibration tests caught.
//
// Going through stddraw.Over with image.Uniform won't work — Go's
// color.RGBA is documented as premultiplied, but qui's toRGBA stores
// straight RGBA so the corner AA math (drawCornerAA) and the rest of
// the renderer agree on the format. Premultiplying at this single
// blend boundary keeps that contract intact.
func fillRectBlend(img *image.RGBA, dst image.Rectangle, c Color) {
	if c.A < 0 {
		return
	}
	rgba := toRGBA(c)
	// Hole-punch idiom: callers (VideoView, scene3d.Viewport, WebView)
	// paint an opaque letterbox background, then FillRect with
	// Color{A:0} (i.e. fully zero RGBA) to clear the pixels the GL
	// texture should show through. Use stddraw.Src so the bitmap is
	// overwritten with (0,0,0,0).
	//
	// Crucially the (R,G,B) check is required: a translucent paint
	// (e.g. DrawStateLayer's hover overlay) computes c.A = layerA *
	// opacity in float space; if opacity is tiny (the first ~1.6 ms of
	// the 200 ms hover transition), toByte rounds c.A to 0 even though
	// it's non-zero in float. Without the RGB-zero guard, fillRectBlend
	// hole-punched the rounded-rect strips to (0,0,0,0); the corners
	// (drawCornerAA, which no-ops on a==0) survived, producing the
	// "black rectangle in the button's center, rounded ends intact"
	// flash a fast hover triggered.
	if rgba.A == 0 {
		if rgba.R == 0 && rgba.G == 0 && rgba.B == 0 {
			stddraw.Draw(img, dst, &image.Uniform{C: rgba}, image.Point{}, stddraw.Src)
		}
		// Non-zero RGB with A=0 is a degenerate translucent paint —
		// invisible by definition. Skip.
		return
	}
	if rgba.A == 0xff {
		stddraw.Draw(img, dst, &image.Uniform{C: rgba}, image.Point{}, stddraw.Src)
		return
	}
	a := uint32(rgba.A)
	r := uint32(rgba.R) * a / 255
	g := uint32(rgba.G) * a / 255
	b := uint32(rgba.B) * a / 255
	ia := 255 - a
	stride := img.Stride
	pix := img.Pix
	imgMin := img.Rect.Min
	// Row-base offset in Pix for dst's top row. Adding (x-imgMin.X)*4
	// on top gives the byte offset for pixel (x, y). Non-zero-origin
	// images (SaveLayer offscreens) rely on this.
	for y := dst.Min.Y; y < dst.Max.Y; y++ {
		rowBase := (y - imgMin.Y) * stride
		for x := dst.Min.X; x < dst.Max.X; x++ {
			i := rowBase + (x-imgMin.X)*4
			pix[i+0] = uint8(r + uint32(pix[i+0])*ia/255)
			pix[i+1] = uint8(g + uint32(pix[i+1])*ia/255)
			pix[i+2] = uint8(b + uint32(pix[i+2])*ia/255)
			outA := a + uint32(pix[i+3])*ia/255
			if outA > 255 {
				outA = 255
			}
			pix[i+3] = uint8(outA)
		}
	}
}

// FillRoundedRect — convenience over DrawShape.
func (c imageCanvas) FillRoundedRect(rect Rect, radius float32, color Color) {
	c.DrawShape(ShapeRRect{Rect: rect, Radius: radius}, Paint{Color: color})
}

// drawCornerAA paints a single rounded corner into the image.
// (cx, cy) is the corner's circle-center in logical coordinates; the
// [x0,y0]..[x1,y1] rectangle is the pixel box to iterate over.
// Pixels whose distance from (cx, cy) is <= radius receive full
// coverage; pixels within a 1-pixel transition zone receive partial
// coverage (blended over the existing image pixel).
func drawCornerAA(img *image.RGBA, cx, cy, radius float32, x0, y0, x1, y1 int, src color.RGBA) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			dx := float32(px) + 0.5 - cx
			dy := float32(py) + 0.5 - cy
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			cov := radius + 0.5 - dist
			if cov <= 0 {
				continue
			}
			if cov > 1 {
				cov = 1
			}
			// Inside-the-arc fast path: when src is fully opaque AND
			// coverage is full, an unblended SetRGBA is correct (this
			// is the hot path for solid widget backgrounds). Anything
			// translucent must go through the src-over blend so a
			// state-layer overlay over a previously-filled corner
			// doesn't wipe out the underlying color with its alpha —
			// that's the "pill ends turn black on press" bug.
			if cov >= 1 && src.A == 0xff {
				img.SetRGBA(px, py, src)
				continue
			}
			dst := img.RGBAAt(px, py)
			a := float32(src.A) / 255 * cov
			ia := 1 - a
			out := color.RGBA{
				R: uint8(float32(src.R)*a + float32(dst.R)*ia),
				G: uint8(float32(src.G)*a + float32(dst.G)*ia),
				B: uint8(float32(src.B)*a + float32(dst.B)*ia),
				A: uint8(float32(src.A)*cov + float32(dst.A)*ia),
			}
			img.SetRGBA(px, py, out)
		}
	}
}

// StrokeRect — convenience over DrawShape.
func (c imageCanvas) StrokeRect(rect Rect, color Color, width float32) {
	c.DrawShape(ShapeRect(rect), Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// StrokeRoundedRect — convenience over DrawShape.
func (c imageCanvas) StrokeRoundedRect(rect Rect, radius float32, color Color, width float32) {
	c.DrawShape(ShapeRRect{Rect: rect, Radius: radius},
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// strokeRoundedRectClipped is the rasterization workhorse for stroked
// rounded rects, taking physical inputs + explicit pixel clip. Called
// from the state-aware StrokeRoundedRect.
// drawCornerRingAA paints the annular slice of a corner. Coverage at
// each pixel is the outer-disc coverage minus the inner-disc coverage,
// giving a uniform AA stroke that joins seamlessly with the straight
// edges painted by StrokeRoundedRect.
func drawCornerRingAA(img *image.RGBA, cx, cy, rOuter, rInner float32, x0, y0, x1, y1 int, src color.RGBA) {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			dx := float32(px) + 0.5 - cx
			dy := float32(py) + 0.5 - cy
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			outerCov := rOuter + 0.5 - dist
			if outerCov <= 0 {
				continue
			}
			if outerCov > 1 {
				outerCov = 1
			}
			innerCov := rInner + 0.5 - dist
			if innerCov < 0 {
				innerCov = 0
			} else if innerCov > 1 {
				innerCov = 1
			}
			cov := outerCov - innerCov
			if cov <= 0 {
				continue
			}
			dst := img.RGBAAt(px, py)
			a := float32(src.A) / 255 * cov
			ia := 1 - a
			out := color.RGBA{
				R: uint8(float32(src.R)*a + float32(dst.R)*ia),
				G: uint8(float32(src.G)*a + float32(dst.G)*ia),
				B: uint8(float32(src.B)*a + float32(dst.B)*ia),
				A: uint8(float32(src.A)*cov + float32(dst.A)*ia),
			}
			img.SetRGBA(px, py, out)
		}
	}
}

// DrawText scales fontSpec.Size + rect through the state stack, then
// hands the physical-coord parameters to the backend. Rotated / skewed
// frames divert to drawTextTransformed — see there for why.
func (c imageCanvas) DrawText(text string, rect Rect, color Color, fontSpec Font) {
	if c.backend == nil {
		return
	}
	if text != "" {
		if !strings.ContainsRune(text, '\n') {
			if line, ok := shapeSingleLine([]rune(text), fontSpec, TextDirectionAuto); ok {
				c.drawShapedLine(line, rect, color, fontSpec)
				return
			}
		} else if c.drawShapedMultiline(text, rect, color, fontSpec) {
			return
		}
	}
	top := c.topCopy()
	scaledFont := fontSpec
	s := top.matrix.avgScale()
	scaledFont.Size = fontSpec.Size * s
	// Tracking is a per-glyph pixel amount: it must scale with the canvas
	// like the glyphs do, or letter/word-spacing (and the justify stretch
	// riding on WordSpacing) paints at half strength on a 2× display while
	// measurement assumed full strength.
	scaledFont.LetterSpacing = fontSpec.LetterSpacing * s
	scaledFont.WordSpacing = fontSpec.WordSpacing * s
	if !top.matrix.IsAxisAligned() {
		c.drawTextTransformed(text, rect, top, color, scaledFont, s)
		return
	}
	c.backend.DrawText(text, top.matrix.TransformRect(rect), top.clip, color, scaledFont)
}

func (c imageCanvas) drawShapedMultiline(text string, rect Rect, color Color, fontSpec Font) bool {
	layout := BuildTextLayout(text, fontSpec, TextLayoutOptions{})
	for _, line := range layout.Lines {
		if line.Text != "" && line.shaped == nil {
			return false
		}
	}
	metrics := GetFontFaceFor(fontSpec).Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	lineHeight := metrics.Height.Ceil()
	totalInk := (len(layout.Lines)-1)*lineHeight + ascent + descent
	extraY := int(rect.H) - totalInk
	if extraY < 0 {
		extraY = 0
	} else {
		extraY /= 2
	}
	baseline := rect.Y + float32(ascent+extraY)
	for _, line := range layout.Lines {
		if line.shaped != nil {
			c.drawShapedLineAtBaseline(line.shaped, rect.X+line.Offset, baseline, color, fontSpec)
		}
		baseline += float32(lineHeight)
	}
	return true
}

// textTransformPad is the slack, in physical pixels, added around the
// offscreen text buffer. Glyph ink is not bounded by the layout rect:
// descenders, italic overhang, accents and emoji bitmaps all spill, and
// on the axis-aligned path they simply spill onto the framebuffer. The
// offscreen has no such neighbours, so anything past its edge would be
// cut off.
const textTransformPad = 8

// drawTextTransformed renders rotated text by rasterizing it upright into
// an offscreen buffer at physical scale, then blitting that buffer
// through the canvas affine.
//
// Why not draw rotated glyphs directly: the underlying font.Drawer walks
// a horizontal baseline and blits axis-aligned glyph masks — it has no
// transform input. Rasterize-then-transform is the standard answer, it
// keeps hinting and the emoji bitmap path intact, and it means the
// backend needs exactly one new primitive rather than a rotated variant
// of every text entry point.
//
// Cost is one allocation plus one resample per rotated DrawText call.
// That is fine because rotation is rare and opt-in (the axis-aligned
// fast path above is untouched), but it is why a widget animating rotated
// text at 60 Hz wants a cached layer rather than this path.
func (c imageCanvas) drawTextTransformed(text string, rect Rect, top stateFrame, color Color, scaledFont Font, s float32) {
	if text == "" || s <= 0 {
		return
	}
	// Offscreen is sized in physical pixels: the rect at display scale,
	// plus pad on every side.
	wpx := int(rect.W*s+0.5) + 2*textTransformPad
	hpx := int(rect.H*s+0.5) + 2*textTransformPad
	if wpx <= 0 || hpx <= 0 {
		return
	}
	buf := image.NewRGBA(image.Rect(0, 0, wpx, hpx))
	drawTextIntoRaw(buf, text, Rect{
		X: textTransformPad,
		Y: textTransformPad,
		W: rect.W * s,
		H: rect.H * s,
	}, color, scaledFont)

	// Map buffer pixels back to logical space: undo the physical scale,
	// then shift so the padded origin lands on the rect's origin.
	pad := textTransformPad / s
	m, ok := srcToDeviceMatrix(top.matrix, Rect{
		X: rect.X - pad,
		Y: rect.Y - pad,
		W: float32(wpx) / s,
		H: float32(hpx) / s,
	}, wpx, hpx)
	if !ok {
		return
	}
	c.backend.DrawImageTransformed(buf, m, top.clip)
}

// drawTextLine walks the line rune-by-rune, dispatching contiguous
// "plain" runs to font.Drawer (fast path, grayscale outline) and
// emoji runs to the EmojiProvider (color bitmap blit). Text advances
// consistently on both paths so mixed text/emoji stays aligned.
//
// Baseline alignment for emoji: the provider returns a rectangular
// bitmap whose logical center matches text mid-height. We anchor it
// vertically so the center of the bitmap lands at text center
// (baseline - ascent/2) — close enough to how native text layout
// handles emoji in a line of mixed content.
func drawTextLineRaw(emojiDst *image.RGBA, line string, drawer *font.Drawer, fontSpec Font, faces []font.Face,
	x0, y, ascent, capHeight, lineHeight int) {

	x := x0
	runes := []rune(line)

	// Partition into contiguous segments of same "kind" (emoji vs plain).
	// Process one segment at a time to keep text shaping hints intact.
	segStart := 0
	segEmoji := false
	if len(runes) > 0 {
		segEmoji = isEmojiRune(runes[0])
	}

	flush := func(end int) {
		if segStart >= end {
			return
		}
		if segEmoji {
			// Walk by extended emoji cluster (base + modifiers / ZWJ
			// composition / regional-indicator pair) and rasterize the
			// joined UTF-8 string. Letting the provider see the whole
			// cluster lets Core Text produce the combined glyph (e.g.
			// 👌🏻 instead of 👌 + a standalone skin-tone square).
			i := segStart
			for i < end {
				j := EmojiClusterEnd(runes, i)
				if j <= i {
					i++
					continue
				}
				img := lookupEmojiSequence(string(runes[i:j]), fontSpec.Size)
				i = j
				if img == nil {
					// Provider declined — skip advance so layout
					// doesn't leak a hole.
					continue
				}
				bounds := img.Bounds()
				// Center the bitmap vertically on the text's OPTICAL center
				// — the middle of the cap-height box (baseline up to cap
				// height), which is exactly where uppercase letters and
				// digits sit. The earlier formula centered on `y - ascent/2`
				// (the ascent midline), which works only when ascent ≈ cap
				// height. Fonts with generous internal leading break that
				// assumption: JetBrains Mono at 16px has ascent 16.3 but cap
				// height 11.7, so the ascent midline floats ~2.6px above the
				// glyphs and the emoji reads as too high. cap-height
				// centering is font-metric-independent and keeps the emoji
				// aligned with neighboring digits across any face.
				capCenter := y - capHeight/2
				dstY := capCenter - bounds.Dy()/2
				dst := image.Rect(x, dstY, x+bounds.Dx(), dstY+bounds.Dy())
				stddraw.Draw(emojiDst, dst, img, bounds.Min, stddraw.Over)
				x += bounds.Dx()
			}
		} else {
			plain := runes[segStart:end]
			if len(plain) == 0 {
				return
			}
			faceStart := 0
			currFace := faceForRune(plain[0], faces)
			for i := 1; i < len(plain); i++ {
				nextFace := faceForRune(plain[i], faces)
				if nextFace != currFace {
					drawer.Face = currFace
					x = drawStyledRunRaw(emojiDst, drawer, string(plain[faceStart:i]), fontSpec, x, y, ascent, lineHeight)
					faceStart = i
					currFace = nextFace
				}
			}
			drawer.Face = currFace
			x = drawStyledRunRaw(emojiDst, drawer, string(plain[faceStart:]), fontSpec, x, y, ascent, lineHeight)
		}
	}

	for i := 1; i < len(runes); i++ {
		kind := isEmojiRune(runes[i])
		if kind != segEmoji {
			flush(i)
			segStart = i
			segEmoji = kind
		}
	}
	flush(len(runes))
}

// drawStyledRun draws one plain-text run using drawer.Face while applying
// style fallbacks when a true variant is unavailable:
//   - Bold: draw a second pass at x+1.
//   - Italic: shear a temporary run image to emulate oblique text.
func drawStyledRunRaw(dst *image.RGBA, drawer *font.Drawer, text string, fontSpec Font, x, y, ascent, lineHeight int) int {
	if text == "" {
		return x
	}
	// CSS letter-spacing / word-spacing: draw glyph-by-glyph, advancing the
	// dot by the real glyph advance plus the tracking so painted positions
	// match runeAdvanceWithFaces (which adds the same). Only taken when
	// spacing is set, so the common run stays on the whole-string fast path.
	if fontSpec.LetterSpacing != 0 || fontSpec.WordSpacing != 0 {
		return drawSpacedRunRaw(drawer, text, fontSpec, x, y)
	}
	drawer.Dot = fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}
	if !fontSpec.Bold && !fontSpec.Italic {
		drawer.DrawString(text)
		return drawer.Dot.X.Round()
	}
	advance := drawer.MeasureString(text).Round()
	if advance < 0 {
		advance = 0
	}
	// Bold without italic can stay on the fast direct-draw path.
	if !fontSpec.Italic {
		drawer.DrawString(text)
		if fontSpec.Bold {
			drawer.Dot = fixed.Point26_6{X: fixed.I(x + 1), Y: fixed.I(y)}
			drawer.DrawString(text)
		}
		return advance + x
	}

	// Italic fallback: render to a temporary image, then row-shear.
	// Use a gentle slope so normal body text remains legible.
	shear := int(math.Ceil(float64(float32(lineHeight) * italicFallbackShearFactor)))
	if shear < 1 {
		shear = 1
	}
	boldPad := 0
	if fontSpec.Bold {
		boldPad = 1
	}
	tmpW := advance + shear + boldPad + 1
	if tmpW < 1 {
		tmpW = 1
	}
	tmpH := lineHeight
	if tmpH < 1 {
		tmpH = 1
	}
	tmp := image.NewRGBA(image.Rect(0, 0, tmpW, tmpH))
	baseline := ascent
	if baseline < 0 {
		baseline = 0
	}
	if baseline >= tmpH {
		baseline = tmpH - 1
	}
	tmpDrawer := font.Drawer{
		Dst:  tmp,
		Src:  drawer.Src,
		Face: drawer.Face,
		Dot:  fixed.Point26_6{X: fixed.I(0), Y: fixed.I(baseline)},
	}
	tmpDrawer.DrawString(text)
	if fontSpec.Bold {
		tmpDrawer.Dot = fixed.Point26_6{X: fixed.I(1), Y: fixed.I(baseline)}
		tmpDrawer.DrawString(text)
	}
	rowDenom := tmpH - 1
	top := y - baseline
	for row := 0; row < tmpH; row++ {
		shift := shear
		if rowDenom > 0 {
			shift = ((tmpH - 1 - row) * shear) / rowDenom
		}
		dstRow := image.Rect(x+shift, top+row, x+shift+tmpW, top+row+1)
		stddraw.Draw(dst, dstRow, tmp, image.Point{X: 0, Y: row}, stddraw.Over)
	}
	return advance + x
}

// drawSpacedRunRaw draws text one glyph at a time, advancing the dot by the
// glyph's own advance plus CSS letter-spacing (and word-spacing after a
// space). This mirrors runeAdvanceWithFaces so caret / selection / wrap math
// stays aligned with the painted glyphs. Bold double-draws each glyph;
// italic shear is skipped in this path (spacing + synthetic italic is rare).
func drawSpacedRunRaw(drawer *font.Drawer, text string, fontSpec Font, x, y int) int {
	ls := fixed.Int26_6(math.Round(float64(fontSpec.LetterSpacing) * 64))
	ws := fixed.Int26_6(math.Round(float64(fontSpec.WordSpacing) * 64))
	dotX := fixed.I(x)
	yy := fixed.I(y)
	for _, r := range text {
		drawer.Dot = fixed.Point26_6{X: dotX, Y: yy}
		drawer.DrawString(string(r))
		if fontSpec.Bold {
			drawer.Dot = fixed.Point26_6{X: dotX + fixed.I(1), Y: yy}
			drawer.DrawString(string(r))
		}
		adv, _ := drawer.Face.GlyphAdvance(r)
		dotX += adv + ls
		if r == ' ' {
			dotX += ws
		}
	}
	return dotX.Round()
}

func faceForRune(r rune, faces []font.Face) font.Face {
	if len(faces) == 0 {
		return basicfont.Face7x13
	}
	for _, f := range faces {
		if f == nil {
			continue
		}
		if _, ok := f.GlyphAdvance(r); ok {
			return f
		}
	}
	return faces[0]
}

// DrawImage projects rect through the state stack and delegates to
// the backend for resampled blit. Under a rotated / skewed matrix
// TransformRect would only yield an axis-aligned bounding box, so those
// frames route to DrawImageTransformed instead.
func (c imageCanvas) DrawImage(img image.Image, rect Rect) {
	if c.backend == nil || img == nil {
		return
	}
	top := c.topCopy()
	if !top.matrix.IsAxisAligned() {
		if m, ok := srcToDeviceMatrix(top.matrix, rect, img.Bounds().Dx(), img.Bounds().Dy()); ok {
			c.backend.DrawImageTransformed(img, m, top.clip)
		}
		return
	}
	c.backend.DrawImage(img, top.matrix.TransformRect(rect), top.clip)
}

// DrawVector projects rect through the state stack and delegates to
// the backend for on-demand rasterization at physical pixel size.
//
// Rotated frames rasterize at the matrix's average scale (so the glyph /
// icon is sampled at display resolution rather than at logical size) and
// then blit through the affine.
func (c imageCanvas) DrawVector(src VectorSource, rect Rect, tint Color) {
	if c.backend == nil || src == nil {
		return
	}
	top := c.topCopy()
	if !top.matrix.IsAxisAligned() {
		s := top.matrix.avgScale()
		rw, rh := int(rect.W*s+0.5), int(rect.H*s+0.5)
		if rw <= 0 || rh <= 0 {
			return
		}
		raster := src.Rasterize(rw, rh, tint)
		if raster == nil {
			return
		}
		if m, ok := srcToDeviceMatrix(top.matrix, rect, rw, rh); ok {
			c.backend.DrawImageTransformed(raster, m, top.clip)
		}
		return
	}
	c.backend.DrawVector(src, top.matrix.TransformRect(rect), top.clip, tint)
}

// srcToDeviceMatrix composes the affine that maps a source raster's own
// pixel space — (0,0) to (srcW,srcH) — onto physical device space, given
// the logical `rect` the raster should fill and the canvas matrix.
//
// Concat applies right-to-left, so this reads as: scale pixels down to
// the rect's logical size, translate to the rect's origin, then apply the
// canvas transform.
func srcToDeviceMatrix(canvas Matrix, rect Rect, srcW, srcH int) (Matrix, bool) {
	if srcW <= 0 || srcH <= 0 || rect.W == 0 || rect.H == 0 {
		return Matrix{}, false
	}
	return canvas.
		Concat(TranslateMatrix(rect.X, rect.Y)).
		Concat(ScaleMatrix(rect.W/float32(srcW), rect.H/float32(srcH))), true
}

// DrawLine — convenience over DrawShape.
func (c imageCanvas) DrawLine(p1, p2 Point, color Color, width float32) {
	c.DrawShape(ShapeLine{P1: p1, P2: p2},
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// DrawPolyline — convenience over DrawShape.
func (c imageCanvas) DrawPolyline(points []Point, color Color, width float32) {
	c.DrawShape(ShapePolyline(points),
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// drawLineInto is the shared capsule rasterizer. pxClip bounds the
// pixel-iteration rectangle and is expected to already be clamped to
// the image's bounds. Width clamps to a 0.5px minimum so hairlines
// still produce a visible 1-pixel trace.
//
// `cap` controls the endpoint shape:
//   - CapRound: classic capsule (rounded ends). Distance from the
//     parametric foot is the same at every t.
//   - CapButt: endpoints are perpendicular to the segment — t is
//     clamped strict-equality outside [0, segLen] so pixels past the
//     endpoint contribute zero coverage.
//   - CapSquare: behave like CapRound's perpendicular-foot for the
//     interior but extend the segment by halfW at each end so the
//     square cap juts out by halfW past the endpoint.
func drawLineInto(img *image.RGBA, pxClip image.Rectangle, p1, p2 Point, width float32, rgba color.RGBA, cap StrokeCap) {
	if pxClip.Empty() {
		return
	}
	halfW := width * 0.5
	if halfW < 0.5 {
		halfW = 0.5
	}
	// Segment direction + length.
	dx := p2.X - p1.X
	dy := p2.Y - p1.Y
	lenSq := dx*dx + dy*dy
	if lenSq < 1e-6 {
		// Degenerate segment — still draw a round dot of diameter=width
		// (round/square) or nothing (butt).
		if cap != CapButt {
			stampDot(img, pxClip, p1, halfW, rgba)
		}
		return
	}
	segLen := float32(math.Sqrt(float64(lenSq)))
	invLen := 1 / segLen
	ux := dx * invLen
	uy := dy * invLen

	// CapSquare extends the segment endpoints by halfW so the square
	// cap juts out. The interior clamp + perpendicular-distance math
	// then renders the extended capsule with butt-ish ends but the
	// segment is now longer.
	tMin := float32(0)
	tMax := segLen
	if cap == CapSquare {
		tMin = -halfW
		tMax = segLen + halfW
	}

	// Iteration bbox = segment bbox inflated by halfW + 1 for the AA
	// transition band. CapSquare's extension is captured via tMin/tMax;
	// expand the bbox to match.
	pad := halfW + 1
	bbMinX := minF(p1.X, p2.X) - pad
	bbMinY := minF(p1.Y, p2.Y) - pad
	bbMaxX := maxF(p1.X, p2.X) + pad
	bbMaxY := maxF(p1.Y, p2.Y) + pad
	if cap == CapSquare {
		// Bbox extension along the segment direction.
		bbMinX -= halfW * absF(ux)
		bbMinY -= halfW * absF(uy)
		bbMaxX += halfW * absF(ux)
		bbMaxY += halfW * absF(uy)
	}
	x0 := int(math.Floor(float64(bbMinX)))
	y0 := int(math.Floor(float64(bbMinY)))
	x1 := int(math.Ceil(float64(bbMaxX)))
	y1 := int(math.Ceil(float64(bbMaxY)))
	if x0 < pxClip.Min.X {
		x0 = pxClip.Min.X
	}
	if y0 < pxClip.Min.Y {
		y0 = pxClip.Min.Y
	}
	if x1 > pxClip.Max.X {
		x1 = pxClip.Max.X
	}
	if y1 > pxClip.Max.Y {
		y1 = pxClip.Max.Y
	}

	edge := halfW + 0.5
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			cx := float32(px) + 0.5 - p1.X
			cy := float32(py) + 0.5 - p1.Y
			t := cx*ux + cy*uy
			if cap == CapButt {
				// Skip pixels past the endpoints entirely (no round cap).
				if t < 0 || t > segLen {
					continue
				}
			} else if t < tMin || t > tMax {
				continue
			}
			if t < tMin {
				t = tMin
			} else if t > tMax {
				t = tMax
			}
			// For CapRound we still want round caps — clamp t to [0,
			// segLen] so the distance includes the radial extent past
			// the endpoint.
			if cap == CapRound {
				if t < 0 {
					t = 0
				} else if t > segLen {
					t = segLen
				}
			}
			ex := cx - t*ux
			ey := cy - t*uy
			dist := float32(math.Sqrt(float64(ex*ex + ey*ey)))
			cov := edge - dist
			if cov <= 0 {
				continue
			}
			if cov >= 1 {
				cov = 1
			}
			blendPixel(img, px, py, rgba, cov)
		}
	}
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func stampDot(img *image.RGBA, pxClip image.Rectangle, p Point, halfW float32, rgba color.RGBA) {
	pad := halfW + 1
	x0 := int(math.Floor(float64(p.X - pad)))
	y0 := int(math.Floor(float64(p.Y - pad)))
	x1 := int(math.Ceil(float64(p.X + pad)))
	y1 := int(math.Ceil(float64(p.Y + pad)))
	if x0 < pxClip.Min.X {
		x0 = pxClip.Min.X
	}
	if y0 < pxClip.Min.Y {
		y0 = pxClip.Min.Y
	}
	if x1 > pxClip.Max.X {
		x1 = pxClip.Max.X
	}
	if y1 > pxClip.Max.Y {
		y1 = pxClip.Max.Y
	}
	edge := halfW + 0.5
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			dx := float32(px) + 0.5 - p.X
			dy := float32(py) + 0.5 - p.Y
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			cov := edge - dist
			if cov <= 0 {
				continue
			}
			if cov >= 1 {
				cov = 1
			}
			blendPixel(img, px, py, rgba, cov)
		}
	}
}

func blendPixel(img *image.RGBA, px, py int, src color.RGBA, cov float32) {
	if cov >= 1 && src.A == 255 {
		img.SetRGBA(px, py, src)
		return
	}
	dst := img.RGBAAt(px, py)
	a := float32(src.A) / 255 * cov
	ia := 1 - a
	out := color.RGBA{
		R: uint8(float32(src.R)*a + float32(dst.R)*ia),
		G: uint8(float32(src.G)*a + float32(dst.G)*ia),
		B: uint8(float32(src.B)*a + float32(dst.B)*ia),
		A: uint8(float32(src.A)*cov + float32(dst.A)*ia),
	}
	img.SetRGBA(px, py, out)
}

func clampRect(bounds image.Rectangle, rect Rect) image.Rectangle {
	x0 := int(rect.X)
	y0 := int(rect.Y)
	x1 := int(rect.X + rect.W)
	y1 := int(rect.Y + rect.H)
	if x1 < x0 {
		x1 = x0
	}
	if y1 < y0 {
		y1 = y0
	}
	r := image.Rect(x0, y0, x1, y1)
	return r.Intersect(bounds)
}

func toRGBA(c Color) color.RGBA {
	return color.RGBA{
		R: toByte(c.R),
		G: toByte(c.G),
		B: toByte(c.B),
		A: toByte(c.A),
	}
}

// toPremultipliedRGBA is toRGBA for the one consumer that needs the
// STANDARD LIBRARY's convention rather than this package's.
//
// Color (and toRGBA) are straight/unassociated: R is the ink's own red
// whatever the alpha is, which is what the hand-written blend helpers in
// backend_cpu.go expect. Go's color.RGBA is alpha-PREMULTIPLIED, and
// image/draw — which the glyph rasterizer composites through — reads it
// that way. Handing it a straight colour makes any channel above the alpha
// an invalid premultiplied value, and semi-transparent text came out dark
// and hue-shifted (grey #b7b7b7 at 50% painted as #373737, red as grey).
func toPremultipliedRGBA(c Color) color.RGBA {
	a := c.A
	switch {
	case a <= 0:
		return color.RGBA{}
	case a > 1:
		a = 1
	}
	return color.RGBA{
		R: toByte(c.R * a),
		G: toByte(c.G * a),
		B: toByte(c.B * a),
		A: toByte(a),
	}
}

// uniformCache memoizes the result of image.NewUniform per color so
// the text-drawing hot path (which builds a font.Drawer per call)
// doesn't allocate a fresh Uniform on every glyph run. Keyed by the
// already-quantized color.RGBA (4 bytes, comparable). Set-once +
// read-many — a typical theme + syntax palette has under 100 distinct
// colors, so cap and eviction aren't needed.
var (
	uniformCacheMu sync.RWMutex
	uniformCache   = map[color.RGBA]*image.Uniform{}
)

func cachedUniform(c color.RGBA) *image.Uniform {
	uniformCacheMu.RLock()
	if u, ok := uniformCache[c]; ok {
		uniformCacheMu.RUnlock()
		return u
	}
	uniformCacheMu.RUnlock()

	uniformCacheMu.Lock()
	defer uniformCacheMu.Unlock()
	if u, ok := uniformCache[c]; ok {
		return u
	}
	u := image.NewUniform(c)
	uniformCache[c] = u
	return u
}

func toByte(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
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
