package qui

import (
	"image"
	"log"
	"slices"
	"time"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/render"
)

// paintDebugEnabled turns on QUI_DEBUG_PAINT=1 diagnostics: logs when a
// frame's repaint is promoted from scoped dirty rects to a full-window
// repaint because the layout pass moved a widget, naming the first mover.
// Use it to find what keeps a supposedly-idle window repainting.
var paintDebugEnabled = envFlag("QUI_DEBUG_PAINT")

// Invalidate marks the entire window as needing a redraw on the next frame.
// Use this when the change area is unknown or spans the whole window
// (theme change, global resize, uncertain event side-effects).
func (w *Window) Invalidate() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Invalidate")
	w.dirtyRegion = Rect{W: w.lastSize.W, H: w.lastSize.H}
}

// resizeTo records a logical window-size change and wakes both layout
// and paint. Step calls this defensively because GLFW may deliver size
// and framebuffer callbacks in platform-specific orders during live
// window drags.
func (w *Window) resizeTo(size Size) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.resizeTo")
	if w.lastSize.W == size.W && w.lastSize.H == size.H {
		return
	}
	w.lastSize = size
	w.noteResizeProtocol(size)
	w.Invalidate()
	w.InvalidateLayout()
	// Defer the overlay notification until AFTER this frame's layout pass:
	// an overlay anchored to a widget in the main tree (a dropdown pinned
	// to a button) needs the trigger's NEW bounds to re-place correctly,
	// and those only refresh once root.Layout runs later in Step.
	w.overlayResizePending = true
}

// InvalidateRect marks a sub-region as dirty. The region is unioned into
// the current dirty region so multiple calls within a single frame
// accumulate. Coordinates are in logical (post-layout) window space.
func (w *Window) InvalidateRect(r Rect) {
	if w == nil || r.IsEmpty() {
		return
	}
	w.assertUIThread("Window.InvalidateRect")
	w.dirtyRegion = w.dirtyRegion.Union(r)
}

// noteBoundsChange records that a widget's layout bounds changed (see
// BaseWidget.Layout). During the frame's layout pass this flags the
// frame for a full repaint; outside the pass (caller-positioned
// overlays, programmatic Layout calls, tests) the widget's old and new
// paint extents are invalidated directly so the move is visible without
// a layout pass to promote it.
func (w *Window) noteBoundsChange(oldPaint, newPaint Rect) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.noteBoundsChange")
	if w.inLayoutPass {
		// Sub-pixel jitter (float text advances make flex hand a widget
		// 581.95px one frame and 582px the next) must not promote the
		// whole frame: both extents cover the same pixels, so repainting
		// them in place is exact. Only a real move (≥ boundsEpsilon on
		// some edge) needs the full repaint.
		if rectsNearlyEqual(oldPaint, newPaint, boundsEpsilon) {
			w.InvalidateRect(oldPaint)
			w.InvalidateRect(newPaint)
			return
		}
		if paintDebugEnabled.Load() && !w.boundsChanged {
			log.Printf("qui: paint promoted to full repaint; first moved widget: %+v -> %+v", oldPaint, newPaint)
		}
		w.boundsChanged = true
		return
	}
	w.InvalidateRect(oldPaint)
	w.InvalidateRect(newPaint)
}

// boundsEpsilon is the logical-pixel threshold below which a bounds
// change counts as sub-pixel jitter rather than a move. Real layout
// changes shift widgets by whole pixels; float text measurement noise
// is in the hundredths.
const boundsEpsilon = 0.25

// rectsNearlyEqual reports whether every edge of a and b is within eps.
func rectsNearlyEqual(a, b Rect, eps float32) bool {
	return render.AbsF(a.X-b.X) <= eps && render.AbsF(a.Y-b.Y) <= eps &&
		render.AbsF(a.X+a.W-b.X-b.W) <= eps && render.AbsF(a.Y+a.H-b.Y-b.H) <= eps
}

// RegisterAnimator adds an Animator to the window. The animator's
// Tick is invoked once per frame; when Tick reports done=true the
// animator is removed. Register forces an immediate paint so the
// first Tick observes a non-zero elapsed on frame 2.
//
// Animators keep the loop at display cadence only while frames are being
// painted. One that stays registered without changing anything is
// throttled to a slow poll after a short grace and logged (see window_idle.go):
// drain goroutine results with PostJob rather than from a Tick, and wake
// for a known moment with RequestTickAt.
func (w *Window) RegisterAnimator(a Animator) {
	if w == nil || a == nil {
		return
	}
	w.assertUIThread("Window.RegisterAnimator")
	w.animators = append(w.animators, a)
	w.Invalidate()
}

// tickAnimators ticks every currently-registered animator once, prunes
// any that reported done=true, and carries forward animators that were
// registered DURING a Tick (e.g. reactive's RequestRender path —
// state setter → RegisterAnimator — that commonly fires from inside
// another animator's Tick when async results bridge back into reactive
// state). Newcomers are appended past the walk and first tick next Step.
//
// The walk only ever nils slots (done animators here, unregistered ones
// in UnregisterAnimator) and compacts once it's over, so a Tick may
// register or unregister any animator without disturbing the walk.
func (w *Window) tickAnimators(now time.Time) {
	if len(w.animators) == 0 {
		return
	}
	w.tickingAnimators = true
	n := len(w.animators)
	for i := 0; i < n; i++ {
		a := w.animators[i]
		if a == nil { // unregistered by an earlier Tick this pass
			continue
		}
		dirty, done := a.Tick(now)
		w.dirtyRegion = w.dirtyRegion.Union(dirty)
		if done {
			w.animators[i] = nil
		}
	}
	w.tickingAnimators = false
	end := 0
	for _, a := range w.animators {
		if a != nil {
			w.animators[end] = a
			end++
		}
	}
	// Zero the tail so removed animators can be GC'd.
	clear(w.animators[end:])
	w.animators = w.animators[:end]
}

// UnregisterAnimator removes an animator before it signals done.
// Safe to call on an animator that's already been pruned.
func (w *Window) UnregisterAnimator(a Animator) {
	if w == nil || a == nil {
		return
	}
	w.assertUIThread("Window.UnregisterAnimator")
	for i, existing := range w.animators {
		if existing == a {
			if w.tickingAnimators {
				// tickAnimators compacts the slice once its walk ends.
				w.animators[i] = nil
				return
			}
			w.animators = append(w.animators[:i], w.animators[i+1:]...)
			return
		}
	}
}

// InvalidateLayout forces the next Step to run Measure + Layout on
// the tree, discarding every cached measurement. Use when changing a
// widget's size-affecting state from outside the normal invalidation path
// (e.g., programmatic edits without a wrapping Container.AddChild or
// widget SetText), and for changes that reach every widget at once.
func (w *Window) InvalidateLayout() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.InvalidateLayout")
	invalidateAllMeasures()
	// Overlays too: a theme or locale switch re-measures their text, and
	// relayoutDirtyOverlays only revisits overlays flagged dirty.
	for _, ov := range w.overlays {
		ov.InvalidateLayout()
	}
	if w.root == nil {
		return
	}
	w.root.InvalidateLayout()
}

// SetPreRender installs a raw-GL callback invoked each frame between
// the framebuffer clear and the 2D blit. Useful for shadertoy-style
// backgrounds that should sit under the widget tree. Pass nil to
// clear. No-ops when the current renderer doesn't implement raw GL
// hooks (e.g., NoopRenderer in tests).
func (w *Window) SetPreRender(fn func(GLState)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPreRender")
	if hr, ok := w.renderer.(render.GLHookRenderer); ok {
		hr.SetPreRender(fn)
	}
}

// SetPostRender installs a raw-GL callback invoked each frame after
// the 2D blit, before SwapBuffers. Useful for GPU-composited overlays
// that should sit above the widget tree. Pass nil to clear.
func (w *Window) SetPostRender(fn func(GLState)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPostRender")
	if hr, ok := w.renderer.(render.GLHookRenderer); ok {
		hr.SetPostRender(fn)
	}
}

// SetGPURaster toggles the GPU raster backend on this window's
// renderer. No-op unless the current renderer is a *GLRenderer (e.g.
// NoopRenderer during tests). Also honored via the QUI_GPU_RASTER=1
// environment variable — set that once at startup and every
// GLRenderer created by NewGLRenderer picks it up.
func (w *Window) SetGPURaster(on bool) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetGPURaster")
	if gr, ok := w.renderer.(render.GPURasterRenderer); ok {
		gr.SetGPURaster(on)
	}
}

// SetRenderer assigns the renderer implementation.
func (w *Window) SetRenderer(renderer Renderer) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetRenderer")
	if renderer == nil {
		w.renderer = NoopRenderer{}
		return
	}
	renderer = rendererForPlatform(w.plat, renderer)
	w.renderer = renderer
	// An overlay panel's framebuffer must stay transparent where the widget
	// tree drew nothing. Wire it here rather than making every caller
	// remember: the window already knows its kind, and forgetting produces
	// a black rectangle, which looks like a rendering bug rather than a
	// missing call.
	if tr, ok := renderer.(render.TransparentRenderer); ok {
		tr.SetTransparent(w.kind == WindowOverlayPanel)
	}
}

// SetCustomRender registers a custom render hook invoked every frame.
func (w *Window) SetCustomRender(fn func(Canvas)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetCustomRender")
	w.onRender = fn
}

// Step advances one frame of layout and rendering.
//
// Pipeline:
//  1. Tick animation-driven widgets (e.g., cursor blink). Each Tickable
//     returns its dirty Rect; the union is merged into dirtyRegion.
//  2. If dirtyRegion is empty, return without painting — the CPU image
//     buffer and front framebuffer retain the previous frame, so the
//     screen is stable. A window with nothing changing is not even
//     Stepped: App.Run sleeps until an event or deadline (see window_idle.go).
//  3. Otherwise Measure/Layout the tree, then route Draw through a
//     clipCanvas scoped to dirtyRegion. Only pixels inside the clip are
//     touched; widgets outside the region are effectively no-ops at the
//     canvas layer. Background inside the region is reset to black so
//     text/cursor changes don't composite on top of stale pixels.
func (w *Window) Step() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Step")
	if w.plat == nil || w.inStep {
		return
	}
	defer w.reportPanic("frame")
	w.inStep = true
	defer func() {
		// Whatever path the frame took — painted, nothing to paint, ended
		// early — work waiting on a finished frame sees it now.
		w.runAfterFrame()
		w.inStep = false
		w.noteAnimatorActivity(time.Now())
	}()
	w.framePainted = false

	// Multi-window discipline: each Window owns an independent GL
	// context (optionally shared via NewSharedWindow). The renderer's
	// ensureInit and every GL call routed through Begin/End assume
	// "my" context is current — make it so before any GL work.
	w.plat.makeCurrent()
	// Drain cross-goroutine jobs (agent actions, deferred callbacks)
	// before any Tick / layout work so their state mutations show up
	// in this frame. New jobs posted during drain land in the next
	// Step — keeps frame time bounded.
	w.runJobs()
	// A job may have closed this window (app.CloseWindow / Destroy).
	if w.plat == nil {
		return
	}
	// Raw-mode wishes can change in any handler or job; keep the system
	// key tap in step.
	w.syncKeyCapture()
	// Fullscreen can change behind the app's back (traffic-light button,
	// Ctrl+Cmd+F); a change callback may close this window, ending the frame.
	if !w.pollFullscreenChange() {
		return
	}
	winW, winH := w.plat.size()
	viewport := w.noteWindowSize(winW, winH)
	w.resizeTo(viewport)
	now := time.Now()
	// A reached RequestTickAt deadline is spent; the Tickables that asked
	// for it re-request during the tick walk below if they need another.
	if !w.tickAt.IsZero() && !now.Before(w.tickAt) {
		w.tickAt = time.Time{}
	}
	// Close a grace-pending tooltip before the overlay tick loop so
	// removing it doesn't mutate the slice ranged below.
	w.tickTooltip(now)
	w.tickAnimators(w.anim.now(now))
	if w.root != nil {
		w.dirtyRegion = w.dirtyRegion.Union(tickWidget(w.root, now))
	}
	// Overlays animate independently. A popup with its own blink/timer
	// contributes its Tick Rect to the dirty region.
	for _, ov := range w.overlays {
		w.dirtyRegion = w.dirtyRegion.Union(tickWidget(ov, now))
	}
	for _, ov := range slices.Clone(w.exitingOverlays) {
		w.dirtyRegion = w.dirtyRegion.Union(tickWidget(ov, now))
	}
	// Layout caching: skip Measure+Layout unless something explicitly
	// invalidated the layout. This is the win for high-frequency
	// repaints like cursor blink — the layout hasn't changed, only
	// pixels need refreshing.
	//
	// Layout runs BEFORE the paint decision so the repaint can be
	// scoped: InvalidateLayout dirty-rects only its caller's extent; if
	// the relayout then MOVES any widget (boundsChanged), the frame is
	// promoted to a full repaint — moved widgets need their old and new
	// pixels (plus shadows/halos) refreshed and the single-rect dirty
	// region can't represent that precisely. A relayout that moves
	// nothing (text tick measuring to the same size) keeps the paint
	// clipped to the mutated widget's rect.
	w.lastSize = viewport
	w.layoutPass()
	// A RequestGLRedraw is satisfied by any frame: a painted one replays
	// retained GL draws anyway, so only a frame with nothing dirty needs
	// the GL-only path.
	glOnly := w.glRedraw.Swap(false)
	// An animator, Tick or layout callback may have closed the window.
	if w.plat == nil {
		return
	}
	if w.dirtyRegion.IsEmpty() {
		if glOnly {
			w.redrawGLOnly()
		}
		return
	}
	fbWidth, fbHeight := w.plat.framebufferSize()

	// Clamp dirty region to current window bounds (a widget could return
	// a Rect that extends past a resized window).
	clip := w.dirtyRegion.Intersect(Rect{W: w.lastSize.W, H: w.lastSize.H})
	if clip.IsEmpty() {
		w.dirtyRegion = Rect{}
		return
	}

	// Tell the renderer the true logical (point) size so deferred
	// GL hooks see a correct GLState.LogicalSize. Without this the
	// renderer would default to the framebuffer size and DPR math
	// inside QueueGLDraw closures would always compute to 1×.
	if fi, ok := w.renderer.(render.FrameInfoRenderer); ok {
		fi.SetLogicalSize(w.lastSize)
	}
	canvas := w.renderer.Begin(Size{W: float32(fbWidth), H: float32(fbHeight)})
	if canvas != nil {
		// HiDPI: push the device-pixel ratio onto the canvas state stack
		// once per frame. Every subsequent draw call sees logical inputs
		// scaled to physical pixels. The Save/RestoreTo bookends below
		// keep this DPR isolated from any state widgets push internally.
		drawCanvas := canvas
		dprID := drawCanvas.Save()
		// Dividing the framebuffer by the VIEWPORT (not the window) makes
		// this ratio dpr×zoom, so viewport zoom needs no multiply of its
		// own here — and text, GL scissors and shadows all pick it up.
		if viewport.W > 0 && viewport.H > 0 && fbWidth > 0 && fbHeight > 0 {
			scaleX := float32(fbWidth) / viewport.W
			scaleY := float32(fbHeight) / viewport.H
			if scaleX != 1 || scaleY != 1 {
				drawCanvas.Scale(scaleX, scaleY)
			}
			// Grow the dirty region out to whole PHYSICAL pixels. Widget
			// bounds are fractional, so an un-snapped dirty rect cuts through
			// the middle of an edge pixel — and the primitives disagree about
			// what that means. An AA fill covers such a pixel partially (it
			// is clipped by the float rect), while a layer composite is a
			// per-pixel blit that either writes the pixel or doesn't. The
			// mismatch shows up as a one-pixel line of whatever was UNDER the
			// widget along the dirty rect's edge — a shape's drop shadow
			// bleeding through the bottom row of a repainted menu row, say.
			// Snapping outward costs at most one pixel per edge and makes
			// every primitive agree; a partial repaint then reproduces the
			// full repaint exactly.
			clip = gfx.SnapRectOutward(clip, scaleX, scaleY)
			// Everything below is clipped to clip, so only those pixels
			// need to reach the GPU.
			if fi, ok := w.renderer.(render.FrameInfoRenderer); ok {
				fi.SetDamage(gfx.PhysicalRect(clip, scaleX, scaleY))
			}
		}
		// Route everything through clipCanvas so draws outside the dirty
		// region are elided. Clear only repaints the clipped area; it no
		// longer wipes the whole buffer, which is what preserves last
		// frame's un-dirty pixels.
		//
		// Clear to the active theme's Surface so any gap between widgets
		// (root container padding, inter-row gutters) reads as the
		// background color the rest of the UI sits on, instead of an
		// out-of-theme black that made showcases look like a
		// half-painted page. A theme swap automatically picks up the new
		// surface on the next paint.
		// Dirty-region clip: scope every widget draw to the invalidated
		// area so untouched pixels survive from the previous frame.
		drawCanvas.ClipRect(clip)
		// An overlay panel has no background of its own: it draws a rounded
		// card and lets the corners composite through to whatever is behind
		// the window. Clearing to Surface there would paint an opaque
		// rectangle over exactly the pixels the transparency exists for.
		if w.kind == WindowOverlayPanel {
			drawCanvas.Clear(ColorTransparent)
		} else {
			drawCanvas.Clear(CurrentTheme().Surface)
		}
		w.inPaint = true
		if w.root != nil {
			w.root.Draw(drawCanvas)
		}
		// Overlays paint after the main tree, bottom-of-stack first so
		// a later-pushed overlay sits on top. They share the same clip
		// so they're dirty-region-aware too.
		for _, ov := range w.overlays {
			if PaintBoundsOf(ov).Intersects(clip) {
				ov.Draw(drawCanvas)
			}
		}
		for _, ov := range w.exitingOverlays {
			if PaintBoundsOf(ov).Intersects(clip) {
				ov.Draw(drawCanvas)
			}
		}
		if img := w.drag.image; img != nil && PaintBoundsOf(img).Intersects(clip) {
			img.Draw(drawCanvas)
		}
		if w.onRender != nil {
			w.onRender(drawCanvas)
		}
		if w.debugOverlayEnabled {
			w.drawDebugOverlay(drawCanvas)
		}
		w.drawHighlight(drawCanvas)
		w.inPaint = false
		drawCanvas.RestoreTo(dprID)
	}
	w.renderer.End()
	w.present()
	w.afterPaintProtocol(clip, fullRepaintClip(clip, viewport))
	w.dirtyRegion = Rect{}
	w.framePainted = true
}

// runAfterFrame runs the work queued for the end of this Step. Work queued
// while it runs waits for the next Step's end.
func (w *Window) runAfterFrame() {
	if len(w.afterFrame) == 0 {
		return
	}
	fns := w.afterFrame
	w.afterFrame = nil
	for _, fn := range fns {
		fn()
	}
}

// present hands the finished frame to the platform.
//
// The CPU path is preferred when both sides support it: the reference
// rasterizer has already produced an *image.RGBA, so a compositor that can
// take it directly (CALayer contents, a wl_shm buffer) skips uploading a
// full-screen texture only to blit it straight back down — which is what
// the GL path must do every frame. When either side declines, the GPU path
// presents whatever was drawn into the backbuffer.
func (w *Window) present() {
	surface := w.plat.surface()
	if surface == nil {
		return
	}
	// A renderer that draws through GL (GLRenderer) has already composited
	// into the backbuffer — raw-GL content included, which its CPU image
	// lacks — so its frame is shown by the swap even though it can also
	// hand out that image.
	if g, ok := w.renderer.(render.GPUContextRenderer); ok && g.RequiresGPUContext() {
		surface.present()
		return
	}
	if src, ok := w.renderer.(CPUFrameSource); ok {
		var damage image.Rectangle
		if d, ok := w.renderer.(render.FrameDamageSource); ok {
			damage = d.FrameDamage()
		}
		if img := src.FrameImage(); img != nil && surface.presentCPU(img, damage) {
			return
		}
	}
	surface.present()
}

// layoutPass runs the frame's layout work: the main tree when it is
// layout-dirty, overlay resize notification and overlay relayout — then the
// AfterLayout callbacks, which see final geometry. A callback that dirties
// layout again (a popover positioning itself against what it just measured)
// gets one more pass before paint, up to a small bound.
func (w *Window) layoutPass() {
	for pass := 0; pass < 4; pass++ {
		if w.root != nil && w.root.IsLayoutDirty() {
			w.inLayoutPass = true
			w.boundsChanged = false
			MeasureChild(w.root, w.lastSize)
			w.root.Layout(Rect{X: 0, Y: 0, W: w.lastSize.W, H: w.lastSize.H})
			w.root.ClearLayoutDirty()
			w.inLayoutPass = false
			if widgetDebugEnabled.Load() {
				debugCheckTree(w.root)
			}
			if w.boundsChanged {
				w.Invalidate()
			}
		}
		// Overlay resize handling runs AFTER the layout pass so anchored
		// overlays (dropdowns pinned to a main-tree trigger) re-place against
		// the trigger's freshly-laid-out bounds. Their reposition dirties its
		// own rects, so this must precede the empty-dirty-region early return.
		if w.overlayResizePending {
			w.overlayResizePending = false
			w.notifyOverlaysResize(w.lastSize)
		}
		// Overlays are pre-positioned by their caller, so the layout pass
		// above skips them; one whose subtree invalidated layout re-lays
		// itself out here (OverlayLayouter). Its relayout dirties its own
		// rects, so this too must precede the early return.
		w.relayoutDirtyOverlays()
		if len(w.afterLayout) == 0 {
			return
		}
		fns := w.afterLayout
		w.afterLayout = nil
		for _, fn := range fns {
			fn()
		}
		if (w.root == nil || !w.root.IsLayoutDirty()) && len(w.afterLayout) == 0 {
			return
		}
	}
}

// AfterLayout schedules fn to run once, right after the next layout pass
// and before that frame paints: every widget's Bounds() is final, so fn can
// measure, scroll a new row into view, or place a popover against its
// anchor. If fn invalidates layout, the window lays out again before
// painting. Runs on the UI thread; a headless test window runs it from
// LayoutForTest.
func (w *Window) AfterLayout(fn func()) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.AfterLayout")
	w.afterLayout = append(w.afterLayout, fn)
}

// LayoutForTest runs the layout pass a frame would (main tree, overlays,
// AfterLayout callbacks) on a headless window.
func (w *Window) LayoutForTest() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.LayoutForTest")
	invalidateAllMeasures()
	if w.root != nil {
		w.root.InvalidateLayout()
	}
	w.layoutPass()
}
