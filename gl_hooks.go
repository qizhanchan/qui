package qui

// GL escape hatch + texture compositing primitives.
//
// The stock GLRenderer is a CPU-first 2D rasterizer: the widget tree
// draws into an RGBA buffer which is then blitted with one textured
// quad in End(). For 3D, custom shaders, or GPU-side textures, we
// expose three layered affordances:
//
//  1. GPUCanvas.QueueGLDraw(fn)  — schedule raw GL between the
//     framebuffer clear and the 2D blit. Widgets drawing 3D content
//     into the default framebuffer (depth-tested) call this and
//     handle all GL state themselves.
//  2. GPUCanvas.DrawTexture(...) — blit a ready-made OpenGL texture
//     (an FBO color attachment, a pre-uploaded image) to a rect.
//     Uses the renderer's own compositing shader and respects the
//     2D blit's alpha-blending.
//  3. Window.SetPreRender / SetPostRender — whole-framebuffer hooks
//     for effects that don't belong to any single widget (a
//     shadertoy background, a post-process overlay).
//
// GLViewport (Phase 4) will combine (1) + (2): render a scene into
// an FBO, then DrawTexture that FBO onto its widget bounds.

// GLState carries framebuffer/window sizing info to raw GL callbacks.
// It deliberately doesn't expose the *Window or *GLRenderer so
// callbacks stay decoupled from the Window's internal state — they
// just need physical pixel dimensions to set glViewport/glScissor.
type GLState struct {
	FramebufferSize Size // physical pixels (multiplied by DPR)
	LogicalSize     Size // logical pixels
}

// TextureOpts tunes a DrawTexture call.
type TextureOpts struct {
	// FlipY controls whether the texture is sampled top-down (true,
	// typical for image data uploaded from stb_image / image.RGBA) or
	// bottom-up (false, the native OpenGL convention, matches FBO
	// color attachments). Defaults to false — GLViewport / FBO paths
	// pass FlipY=false; DrawImage-style paths that blit from a
	// uploaded RGBA should pass FlipY=true.
	FlipY bool
	// Tint multiplies the sampled color. Use white (1,1,1,1) for the
	// identity; useful for theming monochrome icons.
	Tint Color
	// Opacity scales the output alpha. 0..1. Defaults to 1 when unset.
	Opacity float32
}

// GPUCanvas is an optional extension to Canvas for canvases backed
// by a GPU pipeline. Widgets that need direct OpenGL access do a
// type assertion on the canvas passed to Draw:
//
//	if gpu, ok := canvas.(qui.GPUCanvas); ok {
//	    gpu.QueueGLDraw(func(s qui.GLState) { ... })
//	}
//
// When the assertion fails (noopCanvas during tests, CPU-only
// backends in the future), widgets should no-op or fall back to
// a CPU-only approximation.
type GPUCanvas interface {
	Canvas
	// DrawTexture blits an OpenGL 2D texture ID to dstRect (logical
	// pixels). srcRect is in texture pixel space; pass
	// Rect{W: texW, H: texH} to sample the full texture. The
	// rendering happens AFTER the 2D CPU blit (so the texture draws
	// on top), unless the caller punches a transparent hole in the
	// 2D layer first (FillRect with A=0). Respects alpha blending.
	DrawTexture(tex uint32, srcRect, dstRect Rect, opts TextureOpts)

	// QueueGLDraw schedules a raw GL callback to run between the
	// framebuffer clear and the 2D blit. Depth/scissor/blend state
	// is the caller's responsibility — set what you need, and the
	// renderer will re-establish the blit state afterwards.
	QueueGLDraw(fn func(GLState))
}

// PhysicalScissor converts a logical-pixel clip rectangle into the
// physical-pixel, bottom-origin GL scissor box that glScissor expects.
// Returns (x, y, w, h, true) ready for `gl.Scissor(x, y, w, h)`, or
// (0,0,0,0,false) when the clip has zero area.
//
// Use case: a widget that issues a QueueGLDraw closure to composite a
// texture, and wants its draw constrained to the current canvas clip
// (so the widget plays well inside ScrollView, Tabs, or any nested
// clipCanvas). The widget queries `canvas.(ClipAware).ClipBounds()`
// before queueing, then calls PhysicalScissor inside the closure.
//
// The conversion handles:
//   - logical → physical pixel scale (HiDPI) via GLState.FramebufferSize
//     and GLState.LogicalSize.
//   - Y-axis flip: qui's logical coords are top-origin; OpenGL's
//     scissor box is bottom-origin.
//
// Empty or zero-area clips return ok=false so callers can skip the
// scissor enable entirely. A clip whose physical box is wider than
// the framebuffer is clamped — glScissor with out-of-range values
// is harmless but clamping keeps the contract tidy.
func PhysicalScissor(state GLState, clip Rect) (x, y, w, h int32, ok bool) {
	if clip.IsEmpty() {
		return 0, 0, 0, 0, false
	}
	scaleX, scaleY := float32(1), float32(1)
	if state.LogicalSize.W > 0 {
		scaleX = state.FramebufferSize.W / state.LogicalSize.W
	}
	if state.LogicalSize.H > 0 {
		scaleY = state.FramebufferSize.H / state.LogicalSize.H
	}
	// Physical rect in top-origin.
	px := clip.X * scaleX
	py := clip.Y * scaleY
	pw := clip.W * scaleX
	ph := clip.H * scaleY
	// Y-flip to OpenGL's bottom-origin scissor space.
	fbH := state.FramebufferSize.H
	flippedY := fbH - (py + ph)
	// Clamp to framebuffer bounds. Negative width/height collapses to 0.
	xi := int32(px + 0.5)
	yi := int32(flippedY + 0.5)
	wi := int32(pw + 0.5)
	hi := int32(ph + 0.5)
	if wi <= 0 || hi <= 0 {
		return 0, 0, 0, 0, false
	}
	return xi, yi, wi, hi, true
}
