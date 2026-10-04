package qui

import (
	"image"
	"os"

	"github.com/go-gl/gl/v3.3-core/gl"
)

// gpuBackend renders 2D primitives into an offscreen FBO using OpenGL
// shaders where a GPU-native fast path is implemented (currently the
// rect / rounded-rect / stroke / line family — Steps 4-7 of the E2.3
// plan). Everything else — text, images, vectors, paths, shadow — is
// forwarded to a wrapped cpuBackend that writes into a companion CPU
// RGBA of matching physical size.
//
// # Z-order — "flushing hybrid"
//
// The CPU RGBA and the GPU FBO are two separate targets. To preserve
// pixel-perfect Z-order across arbitrary interleaving of CPU and GPU
// primitives, every GPU-native call first checks a `cpuDirty` flag: if
// set, the CPU RGBA is uploaded as a texture and blitted into the FBO
// (so far-behind CPU pixels don't survive a later GPU-native rect that
// should cover them), then cleared to transparent, then the GPU call
// runs. At End(), any remaining CPU pixels flush one more time so the
// FBO holds the final composited 2D layer.
//
// # Rendering targets
//
// Begin allocates (or reuses) a single color-attached FBO of the
// physical framebuffer size. Every GPU-native draw binds this FBO;
// End leaves it unbound so GLRenderer.End's usual clear + deferred-GL
// replay lands in the DEFAULT framebuffer. Finally
// CompositeToDefault binds the FBO's color texture and issues the
// same full-screen quad the CPU backend uses, so scene3d /
// webview / video textures underneath still show through the 2D
// layer's alpha=0 regions (hole-punch preserved).
//
// # State assumed constant during widget.Draw
//
//   - The FBO is bound at Begin's end, so widget.Draw sees whatever we
//     set up.
//   - Alpha blending is SRC_ALPHA / ONE_MINUS_SRC_ALPHA to match
//     `fillRectBlend` / `blendPixel`, which write PREMULTIPLIED RGBA on
//     the CPU side.
//   - Viewport is the full physical framebuffer (matching GLRenderer's
//     view of "physical pixels").
//   - Scissor is enabled and used per-draw for physical-pixel clipping.
//
// # Fallback path
//
// Every RasterBackend method that isn't overridden in this file
// forwards to `fallback` (a *cpuBackend that owns a peer *image.RGBA).
// This is what makes the "skeleton" build correctly today: with no
// GPU-native primitives implemented, the FBO stays transparent and
// CompositeToDefault ends up drawing the fallback pixels via the flush
// path. Same visual output as pure CPU, just with two blits per frame
// instead of one.
type gpuBackend struct {
	r *GLRenderer

	// fallback owns a peer *image.RGBA of physical framebuffer size.
	// Every un-accelerated primitive routes through it, setting
	// cpuDirty=true. flushCPUIfDirty uploads its base into the FBO
	// then clears it in-place, so the same buffer is reused for the
	// next batch of CPU-fallback draws.
	fallback *cpuBackend

	// cpuDirty tracks whether the fallback RGBA has any pixels
	// waiting to be flushed into the FBO. Set by any fallback
	// primitive; cleared inside flushCPUIfDirty.
	cpuDirty bool

	// fboSize is the physical size at which fbo/fboTex were last
	// allocated. Begin resizes when the framebuffer size changes.
	fboSize Size
	fbo     uint32
	fboTex  uint32
	// fboReady indicates the FBO + texture have been created. GL
	// resources are allocated lazily on the first Begin (which is
	// guaranteed to run after GLRenderer.ensureInit).
	fboReady bool

	// pixTmp is a scratch buffer sized to physical framebuffer used
	// to zero out fallback.base after each flush. Growing-only.
	pixTmp []byte

	// Shape-family shader program shared by FillRect / FillRoundedRect /
	// StrokeRect / StrokeRoundedRect / DrawLine (Steps 4-7). Vertex
	// input is a static unit quad (0..1 in shapeVBO); the fragment
	// shader receives uRect (pixel-space bounds), uFBSize (framebuffer
	// size), uColor (unpremul RGBA), and shape parameters (uRadius,
	// uStroke, uMode — see backend_gpu_shaders for the mode enum).
	// Initialized lazily on the first draw that needs it so failures
	// leave the fallback path intact.
	shapeProg      uint32
	shapeVAO       uint32
	shapeVBO       uint32
	shapeReady     bool
	shapeFailed    bool
	uShapeRect     int32
	uShapeFBSize   int32
	uShapeColor    int32
	uShapeRadius   int32
	uShapeStroke   int32
	uShapeMode     int32
	uShapeSoftness int32
	uShapeLine     int32

	// layerDepth counts active PushLayer frames. When >0 all draws
	// route through the CPU fallback so the layer's offscreen holds
	// every pixel that made it into the layer. Without this a
	// GPU-native draw would write directly to the FBO (bypassing the
	// layer buffer) and PopLayer would composite a layer that's
	// missing those pixels.
	//
	// A future Step-8 follow-up can allocate a per-layer FBO and
	// switch layerActive() to bind that FBO instead — the current
	// fallback path is correct but not accelerated.
	layerDepth int
}

// layerActive reports whether a SaveLayer / ClipPath is currently in
// scope. GPU-native primitives check this and route to fallback while
// true; otherwise they'd write into the FBO instead of the layer
// buffer that PopLayer eventually composites.
func (b *gpuBackend) layerActive() bool { return b.layerDepth > 0 }

// Shape shader modes — kept in sync with backend_gpu_shaders.go.
const (
	shapeModeSolidRect     int32 = 0
	shapeModeFillRounded   int32 = 1
	shapeModeStrokeRect    int32 = 2
	shapeModeStrokeRounded int32 = 3
	shapeModeLine          int32 = 4
)

// newGPUBackend constructs a fresh GPU raster backend. `base` is the
// same *image.RGBA that GLRenderer allocates for the frame; we hand
// it straight to a cpuBackend for the fallback path. The GL FBO is
// created lazily inside Begin because Begin is the first call after
// GLRenderer.ensureInit(), guaranteeing a live GL context.
func newGPUBackend(r *GLRenderer, base *image.RGBA) *gpuBackend {
	return &gpuBackend{
		r:        r,
		fallback: newCPUBackend(base),
	}
}

// Begin ensures the FBO exists at the physical size, binds it, clears
// it to transparent, and resets the fallback's layer stack for a
// fresh frame.
func (b *gpuBackend) Begin(size Size) {
	if b == nil {
		return
	}
	b.fallback.Begin(size)
	b.cpuDirty = false

	w := int(size.W)
	h := int(size.H)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if !b.fboReady || int(b.fboSize.W) != w || int(b.fboSize.H) != h {
		b.reallocFBO(w, h)
	}
	if !b.fboReady {
		return
	}

	// Bind the FBO for the frame. Do NOT clear it — the CPU backend's
	// r.img is retained across frames so that Window.Step can rely on
	// dirty-region repaints (widgets only re-rasterize the changed
	// rectangle and the surrounding pixels from the previous frame
	// survive). The GPU FBO must behave the same way; a gl.Clear here
	// would erase everything outside the dirty region — exactly the
	// symptom "focus a button, the rest of the UI blanks out".
	//
	// The FBO IS cleared once inside reallocFBO on allocation / resize,
	// so it never starts with undefined texture memory.
	gl.BindFramebuffer(gl.FRAMEBUFFER, b.fbo)
	gl.Viewport(0, 0, int32(w), int32(h))
	// Premultiplied source-over: gpuBackend + cpuBackend both write
	// premul RGBA, so the standard non-premul SRC_ALPHA blend is
	// wrong here (would double-multiply). Use ONE / ONE_MINUS_SRC_ALPHA.
	gl.Enable(gl.BLEND)
	gl.BlendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA)
	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.CULL_FACE)
}

// End drains any pending CPU-side pixels into the FBO and unbinds it
// so subsequent GLRenderer work targets the default framebuffer.
func (b *gpuBackend) End() {
	if b == nil {
		return
	}
	if b.fboReady {
		b.flushCPUIfDirty()
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	}
}

// CPUImage returns nil because the final 2D pixels live in the FBO's
// color texture, not in a CPU RGBA. CompositeToDefault does the blit.
//
// Note this is not merely "we didn't fill r.img": End's flushCPUIfDirty
// ZEROES the fallback buffer after uploading it, so r.img is genuinely
// empty by the time a frame is done. Anything wanting real pixels must go
// through ReadbackCPU.
func (b *gpuBackend) CPUImage() *image.RGBA { return nil }

// ReadbackCPU implements CPUReadback: it pulls the FBO's color attachment
// back into system memory so the snapshot family works under
// QUI_GPU_RASTER=1.
//
// This is a synchronous GPU→CPU transfer — it stalls the pipeline until the
// GPU has caught up, which is why it is deliberately NOT on the per-frame
// path (see CPUImage). Snapshots are a debugging / CI / agent facility taken
// at human or test frequency, where a millisecond-scale stall is irrelevant.
//
// Requirements on the caller:
//
//   - The window's GL context must be current. In practice this means the
//     main goroutine inside Step (Window.Step calls makeCurrent before
//     draining jobs, which is how agent captures reach here safely).
//   - Called between frames. The returned pixels are the FBO as of the last
//     End, which is precisely "the last fully-rendered frame". We do not
//     flush a pending CPU batch here: outside a frame the FBO is not bound,
//     so a flush would blit into the default framebuffer instead.
//
// The result is NOT bit-identical to a CPU-raster snapshot of the same UI.
// Measured in practice: ~98.6% of pixels match exactly, and the differences
// sit entirely on stroked rectangle outlines — the primitives this backend
// rasterizes in a shader rather than forwarding to the CPU fallback. Text,
// fills and lines match exactly. So golden snapshots cannot be shared
// across raster modes; capture goldens in the mode you assert them in.
func (b *gpuBackend) ReadbackCPU() *image.RGBA {
	if b == nil || !b.fboReady {
		return nil
	}
	w, h := int(b.fboSize.W), int(b.fboSize.H)
	if w <= 0 || h <= 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Restore whatever read target was bound. Nothing in the engine leaves
	// a non-default read framebuffer bound today, but a raw-GL hook
	// (SetPreRender / QueueGLDraw) legitimately might, and silently
	// stealing its binding would be a bug that only shows up in someone
	// else's code.
	var prevRead int32
	gl.GetIntegerv(gl.READ_FRAMEBUFFER_BINDING, &prevRead)
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, b.fbo)
	// image.RGBA's stride is width*4, so rows are 4-byte aligned — which
	// is also GL's default. Set it anyway: a hook that changed it would
	// otherwise skew every row of the capture.
	gl.PixelStorei(gl.PACK_ALIGNMENT, 4)
	gl.ReadPixels(0, 0, int32(w), int32(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	gl.BindFramebuffer(gl.READ_FRAMEBUFFER, uint32(prevRead))

	// glReadPixels returns rows starting at the framebuffer's BOTTOM,
	// while image.RGBA row 0 is the top. Both writers into this FBO (the
	// shape shader, and flushCPUIfDirty's uFlipY=1 blit) use GL's
	// bottom-origin convention, so exactly one flip is needed.
	flipRowsVertically(img)
	// Channel order and alpha match without conversion: GL_RGBA/UNSIGNED_BYTE
	// is R,G,B,A in memory like image.RGBA, and the FBO holds premultiplied
	// alpha, which is what image.RGBA means by convention.
	return img
}

// flipRowsVertically reverses the row order of img in place, converting
// between GL's bottom-origin framebuffer layout and image.RGBA's
// top-origin one.
func flipRowsVertically(img *image.RGBA) {
	if img == nil {
		return
	}
	h := img.Rect.Dy()
	stride := img.Stride
	if h < 2 || stride <= 0 {
		return
	}
	scratch := make([]byte, stride)
	for y := 0; y < h/2; y++ {
		top := img.Pix[y*stride : y*stride+stride]
		bottom := img.Pix[(h-1-y)*stride : (h-1-y)*stride+stride]
		copy(scratch, top)
		copy(top, bottom)
		copy(bottom, scratch)
	}
}

// CompositeToDefault binds the FBO's color texture and issues the
// same full-screen quad the CPU backend uses. Called from
// GLRenderer.End() after the framebuffer clear + deferredGL replay.
//
// Y-orientation contract: the FBO holds pixels written directly by
// GL (shape shader + a Y-flipped CPU-flush blit — see flushCPUIfDirty),
// so texture-y=0 is the BOTTOM of the physical image, matching GL's
// native convention. r.program's fragment shader applies uFlipY to
// convert top-origin CPU RGBAs; the FBO doesn't need that flip, so
// we clear uFlipY to 0 for this draw.
func (b *gpuBackend) CompositeToDefault(r *GLRenderer) {
	if b == nil || r == nil || !b.fboReady {
		return
	}
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, b.fboTex)
	gl.UseProgram(r.program)
	gl.Uniform1f(r.programFlip, 0.0)
	gl.BindVertexArray(r.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

// flushCPUIfDirty uploads the fallback RGBA into the FBO and clears
// the CPU buffer for the next batch. No-op when nothing has hit the
// fallback since the last flush.
//
// Ordering: any GPU-native primitive must call this BEFORE issuing
// its own draw so far-behind CPU pixels don't survive a covering GPU
// draw. End() calls it once more to catch trailing fallback work.
func (b *gpuBackend) flushCPUIfDirty() {
	if b == nil || !b.cpuDirty || !b.fboReady {
		return
	}
	img := b.fallback.base
	if img == nil {
		b.cpuDirty = false
		return
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	// Blit fallback → FBO via the shared textured-quad program. The
	// FBO is already bound; sampler unit 0 is already wired to r.program.
	// uFlipY=1 because the source is a top-origin CPU RGBA and we want
	// physical top to land at FBO texture-y=MAX (matching the shape
	// shader's convention, so subsequent CompositeToDefault with
	// uFlipY=0 samples them consistently).
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, b.r.texture)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(img.Pix))
	gl.UseProgram(b.r.program)
	gl.Uniform1f(b.r.programFlip, 1.0)
	gl.BindVertexArray(b.r.vao)
	gl.DrawArrays(gl.TRIANGLES, 0, 6)
	gl.BindVertexArray(0)
	gl.UseProgram(0)

	// Zero the fallback buffer for the next batch. Using a single
	// grow-only scratch to avoid re-allocating N kilobytes per frame.
	need := len(img.Pix)
	if cap(b.pixTmp) < need {
		b.pixTmp = make([]byte, need)
	}
	// Copy zeros in. `copy` beats a range loop by a factor of 4-10
	// on typical framebuffer sizes.
	copy(img.Pix, b.pixTmp[:need])
	b.cpuDirty = false
}

// reallocFBO tears down the previous FBO/texture (if any) and creates
// fresh ones at (w, h) physical pixels. The color attachment is a
// standard 8-bit RGBA texture — same format as GLRenderer.texture so
// the CompositeToDefault blit reuses the existing program without a
// separate uniform for sampler format.
func (b *gpuBackend) reallocFBO(w, h int) {
	if b.fboReady {
		gl.DeleteFramebuffers(1, &b.fbo)
		gl.DeleteTextures(1, &b.fboTex)
		b.fbo, b.fboTex = 0, 0
		b.fboReady = false
	}
	gl.GenFramebuffers(1, &b.fbo)
	gl.GenTextures(1, &b.fboTex)
	gl.BindTexture(gl.TEXTURE_2D, b.fboTex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(w), int32(h), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindFramebuffer(gl.FRAMEBUFFER, b.fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, b.fboTex, 0)
	if status := gl.CheckFramebufferStatus(gl.FRAMEBUFFER); status != gl.FRAMEBUFFER_COMPLETE {
		// Fail soft — the GLRenderer.End() blit falls back to CPU
		// path when fboReady is false.
		gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
		gl.DeleteFramebuffers(1, &b.fbo)
		gl.DeleteTextures(1, &b.fboTex)
		b.fbo, b.fboTex = 0, 0
		return
	}
	// Prime the fresh texture to transparent black so the first frame
	// doesn't sample undefined memory. Subsequent Begin calls MUST NOT
	// clear the FBO — it's retained across frames like r.img on the
	// CPU path (see the comment in Begin).
	gl.Viewport(0, 0, int32(w), int32(h))
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	b.fboReady = true
	b.fboSize = Size{W: float32(w), H: float32(h)}
}

// --- RasterBackend primitive forwarders --------------------------
//
// Everything below is the "CPU fallback" baseline: each method
// routes to the wrapped cpuBackend and marks the buffer dirty.
// Step 4-7 override the shape family with GPU-native implementations
// (each still calling flushCPUIfDirty first).

func (b *gpuBackend) Clear(color Color) {
	b.fallback.Clear(color)
	b.cpuDirty = true
}

func (b *gpuBackend) FillRect(dst image.Rectangle, color Color) {
	// Frontend already clipped `dst` to physical framebuffer bounds and
	// to the current clip. Nothing to do for degenerate rects.
	if dst.Dx() <= 0 || dst.Dy() <= 0 {
		return
	}
	if b.layerActive() {
		b.fallback.FillRect(dst, color)
		b.cpuDirty = true
		return
	}
	rgba := toRGBA(color)
	// Hole-punch idiom: (0,0,0,0) means "make this rect transparent so
	// the deferred-GL widget underneath shows through." CPU path uses
	// stddraw.Src; GPU-native equivalent is gl.Scissor + gl.Clear with
	// a zero clear color. Must flush any pending CPU pixels FIRST so
	// they don't survive the punch.
	if rgba.A == 0 && rgba.R == 0 && rgba.G == 0 && rgba.B == 0 {
		if !b.fboReady {
			b.fallback.FillRect(dst, color)
			b.cpuDirty = true
			return
		}
		b.flushCPUIfDirty()
		b.holePunchRect(dst)
		return
	}
	// Non-degenerate translucent (A=0, RGB≠0) is a no-op — same as the
	// CPU path's early return in fillRectBlend.
	if rgba.A == 0 {
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.FillRect(dst, color)
		b.cpuDirty = true
		return
	}
	b.flushCPUIfDirty()
	b.bindShapeProgram()
	gl.Uniform4f(b.uShapeRect,
		float32(dst.Min.X), float32(dst.Min.Y),
		float32(dst.Max.X), float32(dst.Max.Y))
	gl.Uniform4f(b.uShapeColor,
		float32(rgba.R)/255.0, float32(rgba.G)/255.0,
		float32(rgba.B)/255.0, float32(rgba.A)/255.0)
	gl.Uniform1f(b.uShapeRadius, 0)
	gl.Uniform1f(b.uShapeStroke, 0)
	gl.Uniform1i(b.uShapeMode, shapeModeSolidRect)
	b.emitShapeQuad()
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

// emitShape issues one shape-family GPU draw with the given mode +
// uniforms + inflated quad + clip. Callers pre-flush CPU pixels and
// know the quad + clip; this helper only touches uniforms + scissor +
// DrawArrays. `clip` is in physical (top-origin) pixels; passing a
// zero-W/H clip means "no clip".
func (b *gpuBackend) emitShape(mode int32, quadX0, quadY0, quadX1, quadY1 float32, color Color, radius, strokeHalf float32, clip Rect) {
	rgba := toRGBA(color)
	// Scissor to the clip intersected with the quad's integer bounds.
	// Frontend has typically already intersected but the shader draws
	// the WHOLE quad so an extra scissor around the clip lets any
	// SDF over-coverage (softness band) fall outside the clip safely.
	usedScissor := false
	if clip.W > 0 && clip.H > 0 {
		fbH := int(b.fboSize.H)
		cx0 := int(clip.X)
		cy0 := int(clip.Y)
		cx1 := int(clip.X + clip.W)
		cy1 := int(clip.Y + clip.H)
		if cx1 < cx0 {
			cx0, cx1 = cx1, cx0
		}
		if cy1 < cy0 {
			cy0, cy1 = cy1, cy0
		}
		if cx1 > cx0 && cy1 > cy0 {
			gl.Enable(gl.SCISSOR_TEST)
			gl.Scissor(int32(cx0), int32(fbH-cy1), int32(cx1-cx0), int32(cy1-cy0))
			usedScissor = true
		}
	}
	gl.Uniform4f(b.uShapeRect, quadX0, quadY0, quadX1, quadY1)
	gl.Uniform4f(b.uShapeColor,
		float32(rgba.R)/255.0, float32(rgba.G)/255.0,
		float32(rgba.B)/255.0, float32(rgba.A)/255.0)
	gl.Uniform1f(b.uShapeRadius, radius)
	gl.Uniform1f(b.uShapeStroke, strokeHalf)
	gl.Uniform1i(b.uShapeMode, mode)
	b.emitShapeQuad()
	if usedScissor {
		gl.Disable(gl.SCISSOR_TEST)
	}
}

// holePunchRect clears `dst` on the FBO to (0,0,0,0). Uses a scissored
// gl.Clear because that's cheaper than a shader draw with a special
// blend mode, and it precisely matches the CPU path's stddraw.Src
// semantics — overwrite, don't blend.
func (b *gpuBackend) holePunchRect(dst image.Rectangle) {
	// GL scissor is bottom-origin. Convert from top-origin dst to the
	// GL convention using the FBO height.
	fbH := int(b.fboSize.H)
	x := int32(dst.Min.X)
	y := int32(fbH - dst.Max.Y)
	w := int32(dst.Dx())
	h := int32(dst.Dy())
	gl.Enable(gl.SCISSOR_TEST)
	gl.Scissor(x, y, w, h)
	gl.ClearColor(0, 0, 0, 0)
	gl.Clear(gl.COLOR_BUFFER_BIT)
	gl.Disable(gl.SCISSOR_TEST)
}

func (b *gpuBackend) FillRoundedRect(rect Rect, radius float32, clip Rect, color Color) {
	if rect.W <= 0 || rect.H <= 0 {
		return
	}
	if b.layerActive() {
		b.fallback.FillRoundedRect(rect, radius, clip, color)
		b.cpuDirty = true
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.FillRoundedRect(rect, radius, clip, color)
		b.cpuDirty = true
		return
	}
	// Clamp radius the same way cpuBackend does — negative or > half
	// min-dimension collapses to a plain fill.
	if radius < 0 {
		radius = 0
	}
	maxR := rect.W / 2
	if rect.H/2 < maxR {
		maxR = rect.H / 2
	}
	if radius > maxR {
		radius = maxR
	}
	b.flushCPUIfDirty()
	b.bindShapeProgram()
	if radius <= 0.5 {
		// Degenerate corner — solid rect shader; keeps AA math out of
		// the hot path for flat backgrounds.
		b.emitShape(shapeModeSolidRect,
			rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H,
			color, 0, 0, clip)
	} else {
		b.emitShape(shapeModeFillRounded,
			rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H,
			color, radius, 0, clip)
	}
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

func (b *gpuBackend) StrokeRect(rect Rect, clip Rect, color Color, width float32) {
	if width <= 0 || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if b.layerActive() {
		b.fallback.StrokeRect(rect, clip, color, width)
		b.cpuDirty = true
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.StrokeRect(rect, clip, color, width)
		b.cpuDirty = true
		return
	}
	// Inflate the quad so the AA softness band on the OUTSIDE edge of
	// the stroke stays inside the drawn area. Half a pixel of softness
	// each side is plenty for width>=1.
	halfW := width / 2
	pad := halfW + 1
	x0 := rect.X - pad
	y0 := rect.Y - pad
	x1 := rect.X + rect.W + pad
	y1 := rect.Y + rect.H + pad
	b.flushCPUIfDirty()
	b.drawStrokedRect(rect, clip, color, width, 0, x0, y0, x1, y1, halfW)
}

// drawStrokedRect and drawStrokedRounded issue a stroke draw where the
// QUAD bounds and the SDF-reference rect differ (needed because the
// stroke SDF band extends `width/2` px past the geometric rect on both
// sides). Callers pass the ideal rect + the inflated quad box.
func (b *gpuBackend) drawStrokedRect(rect Rect, clip Rect, color Color, width, radius float32, qx0, qy0, qx1, qy1, halfW float32) {
	b.bindShapeProgram()
	rgba := toRGBA(color)
	mode := shapeModeStrokeRect
	if radius > 0 {
		mode = shapeModeStrokeRounded
	}
	// Scissor to the outer bbox of the ideal edge, inflated by pad so
	// AA doesn't clip. This is the scissor for the whole draw.
	usedScissor := b.applyPaddedScissor(clip)
	// The vertex shader mixes aPos (0..1) with uRect; we want the quad
	// to cover (qx0..qx1, qy0..qy1) but the SDF math to reference
	// (rect.X..rect.X+rect.W, rect.Y..rect.Y+rect.H). Do this by
	// setting uRect to the SDF-reference rect and using a SEPARATE
	// per-draw viewport... no — much simpler: encode the quad expansion
	// as an extra padding uniform. We don't have one wired, so route
	// through: set uRect to the SDF-rect, then transiently expand the
	// vertex shader's understanding by rebuilding the aPos VBO with
	// out-of-range corners. Cheapest option: rewrite aPos to be
	// (0-inflate .. 1+inflate). Since we own shapeVBO, upload a
	// per-call vertex buffer.
	insetX := (rect.X - qx0) / rect.W
	insetY := (rect.Y - qy0) / rect.H
	quadX0 := -insetX
	quadY0 := -insetY
	quadX1 := 1 + (qx1-(rect.X+rect.W))/rect.W
	quadY1 := 1 + (qy1-(rect.Y+rect.H))/rect.H
	verts := []float32{
		quadX0, quadY0,
		quadX1, quadY0,
		quadX1, quadY1,
		quadX0, quadY0,
		quadX1, quadY1,
		quadX0, quadY1,
	}
	gl.BindBuffer(gl.ARRAY_BUFFER, b.shapeVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(verts)*4, gl.Ptr(verts))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)

	gl.Uniform4f(b.uShapeRect, rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H)
	gl.Uniform4f(b.uShapeColor,
		float32(rgba.R)/255.0, float32(rgba.G)/255.0,
		float32(rgba.B)/255.0, float32(rgba.A)/255.0)
	gl.Uniform1f(b.uShapeRadius, radius)
	gl.Uniform1f(b.uShapeStroke, halfW)
	gl.Uniform1i(b.uShapeMode, mode)
	b.emitShapeQuad()

	// Restore the static unit-quad VBO for subsequent draws — cheap
	// (24 floats) and keeps the fast-path callers non-fragile.
	unit := []float32{
		0, 0,
		1, 0,
		1, 1,
		0, 0,
		1, 1,
		0, 1,
	}
	gl.BindBuffer(gl.ARRAY_BUFFER, b.shapeVBO)
	gl.BufferSubData(gl.ARRAY_BUFFER, 0, len(unit)*4, gl.Ptr(unit))
	gl.BindBuffer(gl.ARRAY_BUFFER, 0)

	if usedScissor {
		gl.Disable(gl.SCISSOR_TEST)
	}
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

// applyPaddedScissor enables gl.Scissor at `clip` in the FBO's bottom-
// origin coordinate system, or returns false if the clip is empty or
// unset. Callers must gl.Disable(GL_SCISSOR_TEST) when done.
func (b *gpuBackend) applyPaddedScissor(clip Rect) bool {
	if clip.W <= 0 || clip.H <= 0 {
		return false
	}
	fbH := int(b.fboSize.H)
	cx0 := int(clip.X)
	cy0 := int(clip.Y)
	cx1 := int(clip.X + clip.W)
	cy1 := int(clip.Y + clip.H)
	if cx1 <= cx0 || cy1 <= cy0 {
		return false
	}
	gl.Enable(gl.SCISSOR_TEST)
	gl.Scissor(int32(cx0), int32(fbH-cy1), int32(cx1-cx0), int32(cy1-cy0))
	return true
}

func (b *gpuBackend) StrokeRoundedRect(rect Rect, radius float32, clip Rect, color Color, width float32) {
	if width <= 0 || rect.W <= 0 || rect.H <= 0 {
		return
	}
	if b.layerActive() {
		b.fallback.StrokeRoundedRect(rect, radius, clip, color, width)
		b.cpuDirty = true
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.StrokeRoundedRect(rect, radius, clip, color, width)
		b.cpuDirty = true
		return
	}
	maxR := rect.W / 2
	if rect.H/2 < maxR {
		maxR = rect.H / 2
	}
	if radius < 0 {
		radius = 0
	}
	if radius > maxR {
		radius = maxR
	}
	// If width >= radius the ring degenerates to a filled rounded rect,
	// matching the CPU path's branch.
	if width > radius && radius > 0 {
		b.FillRoundedRect(rect, radius, clip, color)
		return
	}
	if radius <= 0 {
		b.StrokeRect(rect, clip, color, width)
		return
	}
	halfW := width / 2
	pad := halfW + 1
	x0 := rect.X - pad
	y0 := rect.Y - pad
	x1 := rect.X + rect.W + pad
	y1 := rect.Y + rect.H + pad
	b.flushCPUIfDirty()
	b.drawStrokedRect(rect, clip, color, width, radius, x0, y0, x1, y1, halfW)
}

func (b *gpuBackend) FillPath(subs [][]Point, clip image.Rectangle, paint Paint, invMatrix Matrix) {
	b.fallback.FillPath(subs, clip, paint, invMatrix)
	b.cpuDirty = true
}

func (b *gpuBackend) StrokePath(subs [][]Point, closedFlags []bool, clip image.Rectangle, width float32, paint Paint) {
	b.fallback.StrokePath(subs, closedFlags, clip, width, paint)
	b.cpuDirty = true
}

func (b *gpuBackend) DrawLine(p1, p2 Point, clip Rect, color Color, width float32, cap StrokeCap) {
	if width <= 0 {
		return
	}
	if b.layerActive() {
		b.fallback.DrawLine(p1, p2, clip, color, width, cap)
		b.cpuDirty = true
		return
	}
	// Butt / Square caps aren't representable by the current capsule
	// SDF (which draws round caps by construction). Fall back to CPU
	// for those; round-cap is the common UI case (Slider tracks,
	// chart lines) so the GPU fast path covers the visible majority.
	if cap != CapRound {
		b.fallback.DrawLine(p1, p2, clip, color, width, cap)
		b.cpuDirty = true
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.DrawLine(p1, p2, clip, color, width, cap)
		b.cpuDirty = true
		return
	}
	b.flushCPUIfDirty()
	b.emitLineGPU(p1, p2, clip, color, width)
}

func (b *gpuBackend) DrawPolyline(pts []Point, clip Rect, color Color, width float32) {
	if width <= 0 || len(pts) < 2 {
		return
	}
	if b.layerActive() {
		b.fallback.DrawPolyline(pts, clip, color, width)
		b.cpuDirty = true
		return
	}
	if !b.ensureShapeProgram() {
		b.fallback.DrawPolyline(pts, clip, color, width)
		b.cpuDirty = true
		return
	}
	b.flushCPUIfDirty()
	// Emit each segment as a capsule. Batching lands in Step 9; today
	// this is one GL draw call per segment, matching the CPU path's
	// per-segment cost.
	for i := 0; i < len(pts)-1; i++ {
		b.emitLineGPU(pts[i], pts[i+1], clip, color, width)
	}
}

// emitLineGPU issues a capsule SDF draw between p1 and p2 with the
// given clip / color / width. Assumes shape program is compiled and
// the CPU dirty region has already been flushed. The QUAD is the
// axis-aligned padded bbox of the capsule (via uRect + unit aPos);
// capsule endpoints ride in uLine so the fragment shader has the
// true geometry regardless of the quad's orientation.
func (b *gpuBackend) emitLineGPU(p1, p2 Point, clip Rect, color Color, width float32) {
	halfW := width / 2
	pad := halfW + 1
	minX, maxX := p1.X, p2.X
	if p2.X < p1.X {
		minX, maxX = p2.X, p1.X
	}
	minY, maxY := p1.Y, p2.Y
	if p2.Y < p1.Y {
		minY, maxY = p2.Y, p1.Y
	}
	qx0 := minX - pad
	qy0 := minY - pad
	qx1 := maxX + pad
	qy1 := maxY + pad
	// Guarantee a positive-area quad even for degenerate zero-length
	// lines (they'll appear as a filled disc at p1 of radius halfW).
	if qx1 <= qx0 {
		qx1 = qx0 + 1
	}
	if qy1 <= qy0 {
		qy1 = qy0 + 1
	}
	b.bindShapeProgram()
	usedScissor := b.applyPaddedScissor(clip)
	rgba := toRGBA(color)
	gl.Uniform4f(b.uShapeRect, qx0, qy0, qx1, qy1)
	gl.Uniform4f(b.uShapeLine, p1.X, p1.Y, p2.X, p2.Y)
	gl.Uniform4f(b.uShapeColor,
		float32(rgba.R)/255.0, float32(rgba.G)/255.0,
		float32(rgba.B)/255.0, float32(rgba.A)/255.0)
	gl.Uniform1f(b.uShapeRadius, 0)
	gl.Uniform1f(b.uShapeStroke, halfW)
	gl.Uniform1i(b.uShapeMode, shapeModeLine)
	b.emitShapeQuad()
	if usedScissor {
		gl.Disable(gl.SCISSOR_TEST)
	}
	gl.BindVertexArray(0)
	gl.UseProgram(0)
}

func (b *gpuBackend) DrawShadow(rect Rect, radius float32, spec ElevationSpec, shadowColor Color, clip Rect) {
	b.fallback.DrawShadow(rect, radius, spec, shadowColor, clip)
	b.cpuDirty = true
}

func (b *gpuBackend) DrawText(text string, rect Rect, clip Rect, color Color, fontSpec Font) {
	b.fallback.DrawText(text, rect, clip, color, fontSpec)
	b.cpuDirty = true
}

func (b *gpuBackend) DrawImage(img image.Image, dst Rect, clip Rect) {
	b.fallback.DrawImage(img, dst, clip)
	b.cpuDirty = true
}

// DrawImageTransformed goes to the CPU fallback like the other raster
// primitives. A native path (textured quad with transformed vertices) is
// straightforward here, but it buys nothing until DrawImage itself is
// GPU-native — both would still round-trip through the fallback's buffer.
func (b *gpuBackend) DrawImageTransformed(img image.Image, m Matrix, clip Rect) {
	b.fallback.DrawImageTransformed(img, m, clip)
	b.cpuDirty = true
}

func (b *gpuBackend) DrawVector(src VectorSource, dst Rect, clip Rect, tint Color) {
	b.fallback.DrawVector(src, dst, clip, tint)
	b.cpuDirty = true
}

// --- Layers ------------------------------------------------------
//
// Step 8 will replace these with FBO-based layers. For now,
// SaveLayer / ClipPath route through the cpuBackend's RGBA-based
// stack. Because the fallback owns its own layer stack, and every
// draw made INSIDE the layer goes through the fallback (either
// natively or via a flushCPUIfDirty preceding a would-be GPU
// primitive), the layer composite semantics stay correct — the CPU
// backend is the single source of truth while a layer is active.

func (b *gpuBackend) PushLayer(physBounds, clip image.Rectangle, paint Paint) {
	// Flush any pending CPU pixels into the FBO BEFORE entering the
	// layer. Otherwise pre-layer fallback work would end up in the
	// layer's offscreen and get re-composited when PopLayer flushes.
	b.flushCPUIfDirty()
	b.fallback.PushLayer(physBounds, clip, paint)
	b.layerDepth++
}

func (b *gpuBackend) PopLayer(mask *image.Alpha) {
	b.fallback.PopLayer(mask)
	if b.layerDepth > 0 {
		b.layerDepth--
	}
	// The layer composite happened in fallback.base — mark it dirty
	// so the next flushCPUIfDirty carries it into the FBO in Z-order.
	b.cpuDirty = true
}

// envGPURasterEnabled reports whether QUI_GPU_RASTER is truthy in the
// process environment. Read once per NewGLRenderer call — cheap
// enough not to cache. Anything besides "" or "0" counts as on.
func envGPURasterEnabled() bool {
	v := os.Getenv("QUI_GPU_RASTER")
	return v != "" && v != "0"
}
