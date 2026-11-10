package media

import (
	"image"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
)

// FrameView displays frames pushed from outside — a decoder running on its
// own thread, a remote-desktop stream, a camera — rather than frames it
// decodes itself (that is VideoView).
//
//	fv := media.NewFrameView()
//	fv.SetFlex(1)
//	go func() {
//		for pb := range decoded { // CVPixelBufferRef from VideoToolbox
//			fv.PushCVPixelBuffer(pb) // retains; caller keeps its own reference
//		}
//	}()
//
// # Latency and threading
//
// Push* is callable from any goroutine and never blocks on the UI: it
// swaps the frame into a one-slot mailbox (latest wins — a frame replaced
// before it was drawn is released and counted as dropped) and asks the
// window for a frame. The picture is bound and drawn on the UI thread at
// composite time, so the frame shown is the newest one available when the
// GPU work is issued, not when the widget was laid out.
//
// A new frame of an unchanged size costs a redraw-only frame
// (qui.Window.RequestGPURedraw): no widget is re-rasterized and no CPU
// pixels are uploaded. While no frames arrive the window idles — the
// picture stays on screen through partial repaints of the rest of the
// window via a retained compositor draw (qui.RetainedCompositorCanvas),
// not by repainting every frame.
//
// # Input
//
// FrameView is focusable and routes every event aimed at it to the
// handler set with OnEvent (mouse, scroll, key, char, gesture). Use
// MapToFrame to translate pointer positions to frame pixels, and
// SetRawKeyboard to receive keys unfiltered (see qui.RawKeyboardTarget).
type FrameView struct {
	qui.BaseWidget

	mu      sync.Mutex
	pending framePayload // latest pushed, not yet presented
	pendSeq uint64
	pendAt  time.Time
	seq     uint64 // last assigned sequence number
	frameW  int    // size of the newest frame (pending or current)
	frameH  int
	fit     FitMode
	// clearReq asks the UI thread to drop the current frame (Clear).
	clearReq  bool
	onPresent func(PresentInfo)

	// UI-thread state.
	current  framePayload
	curSeq   uint64
	curAt    time.Time
	shownSeq uint64 // last seq reported as presented
	physBox  qui.Rect
	onEvent  func(qui.Event) bool

	focusable bool
	focused   bool
	raw       bool

	// win is the window the widget was last drawn / laid out in, readable
	// from pushing goroutines.
	win atomic.Pointer[qui.Window]
	// retained reports that the last Draw registered a retained draw, so
	// a same-size frame can be shown with a redraw-only frame.
	retained atomic.Bool
	// invalidatePosted dedups the PostJob a push falls back to.
	invalidatePosted atomic.Bool

	pushed    atomic.Uint64
	presented atomic.Uint64
	dropped   atomic.Uint64
}

// framePayload is one pushed frame. Implementations: rgbaFrame (portable)
// and the darwin CVPixelBuffer frame.
type framePayload interface {
	size() (w, h int)
	// draw composites the src texel rect of the frame onto physDst
	// through the renderer's Compositor. UI thread.
	draw(v *FrameView, c qui.Compositor, src, physDst qui.Rect) bool
	// cpuImage returns the frame as an image for CPU presentation, or
	// nil if it has none.
	cpuImage() image.Image
	// release drops the frame's resources. Called exactly once.
	release()
}

// FrameStats are cumulative counters, for latency / throughput readouts.
type FrameStats struct {
	// Pushed counts frames handed to Push*.
	Pushed uint64
	// Presented counts frames drawn at least once.
	Presented uint64
	// Dropped counts frames replaced by a newer push before they were
	// drawn (or discarded by Clear before being drawn).
	Dropped uint64
}

// PresentInfo describes a frame the moment it is first drawn.
type PresentInfo struct {
	// Seq numbers pushes from 1; a gap from the previous Seq is the
	// number of frames dropped in between.
	Seq uint64
	// PushedAt is when Push* accepted the frame; PresentedAt when its
	// draw was issued to the GPU (before the buffer swap / vsync).
	PushedAt    time.Time
	PresentedAt time.Time
	Width       int
	Height      int
}

// Latency is PresentedAt - PushedAt.
func (p PresentInfo) Latency() time.Duration { return p.PresentedAt.Sub(p.PushedAt) }

// NewFrameView returns an empty, focusable FrameView in FitContain mode.
func NewFrameView() *FrameView {
	v := &FrameView{BaseWidget: qui.NewBaseWidget(), fit: FitContain, focusable: true}
	v.SetSelf(v)
	return v
}

// PushRGBA shows img. Portable fallback for producers without a GPU
// surface: the pixels are uploaded to a texture at draw time. FrameView
// takes ownership of img — do not modify it after the call. Safe from any
// goroutine.
func (v *FrameView) PushRGBA(img *image.RGBA) {
	if img == nil || img.Rect.Dx() <= 0 || img.Rect.Dy() <= 0 {
		return
	}
	v.push(&rgbaFrame{img: img})
}

// push installs f as the pending frame (latest wins) and schedules its
// presentation. Never blocks on the UI thread.
func (v *FrameView) push(f framePayload) {
	w, h := f.size()
	now := time.Now()
	v.mu.Lock()
	old := v.pending
	v.seq++
	v.pending, v.pendSeq, v.pendAt = f, v.seq, now
	resized := w != v.frameW || h != v.frameH
	v.frameW, v.frameH = w, h
	v.mu.Unlock()

	v.pushed.Add(1)
	if old != nil {
		old.release()
		v.dropped.Add(1)
	}
	v.schedule(resized)
}

// schedule asks the window to show the pending frame: a redraw-only frame
// when the retained draw is in place and the geometry is unchanged, a
// repaint (plus relayout for a new size) otherwise.
func (v *FrameView) schedule(resized bool) {
	win := v.win.Load()
	if win == nil {
		return // not on screen yet; the first Draw picks the frame up
	}
	if !resized && v.retained.Load() {
		win.RequestGPURedraw()
		return
	}
	if v.invalidatePosted.Swap(true) {
		return
	}
	win.PostJob(func() {
		v.invalidatePosted.Store(false)
		if resized {
			v.InvalidateLayout()
		}
		v.Invalidate()
	})
}

// Clear drops the shown and pending frames; the view shows its background
// until the next push. Safe from any goroutine.
func (v *FrameView) Clear() {
	v.mu.Lock()
	old := v.pending
	v.pending = nil
	v.frameW, v.frameH = 0, 0
	v.clearReq = true
	v.mu.Unlock()
	if old != nil {
		old.release()
		v.dropped.Add(1)
	}
	v.schedule(true)
}

// Destroy releases every frame and the GPU texture. Call on the UI thread
// once the view is no longer shown.
func (v *FrameView) Destroy() {
	v.mu.Lock()
	old := v.pending
	v.pending = nil
	v.frameW, v.frameH = 0, 0
	v.mu.Unlock()
	if old != nil {
		old.release()
	}
	if v.current != nil {
		v.current.release()
		v.current = nil
	}
	if w := v.Window(); w != nil {
		w.ForgetRetainedDraw(v)
	}
	v.retained.Store(false)
}

// promote moves the pending frame to current (UI thread), releasing the
// one it replaces. Returns the frame to show.
func (v *FrameView) promote() framePayload {
	v.mu.Lock()
	next, seq, at := v.pending, v.pendSeq, v.pendAt
	v.pending = nil
	clear := v.clearReq
	v.clearReq = false
	v.mu.Unlock()
	if clear && v.current != nil && next == nil {
		v.current.release()
		v.current = nil
	}
	if next != nil {
		if v.current != nil {
			v.current.release()
		}
		v.current, v.curSeq, v.curAt = next, seq, at
	}
	return v.current
}

// notePresented fires the stats hook the first time a frame is drawn.
func (v *FrameView) notePresented(f framePayload) {
	if v.curSeq == v.shownSeq {
		return
	}
	v.shownSeq = v.curSeq
	v.presented.Add(1)
	v.mu.Lock()
	fn := v.onPresent
	v.mu.Unlock()
	if fn != nil {
		w, h := f.size()
		fn(PresentInfo{Seq: v.curSeq, PushedAt: v.curAt, PresentedAt: time.Now(), Width: w, Height: h})
	}
}

// Stats returns the cumulative counters. Safe from any goroutine.
func (v *FrameView) Stats() FrameStats {
	return FrameStats{Pushed: v.pushed.Load(), Presented: v.presented.Load(), Dropped: v.dropped.Load()}
}

// OnPresent registers fn to run when a frame is drawn for the first time.
// It runs on the UI thread in the middle of frame composition: record
// timings there, do not touch the widget tree. nil removes it.
func (v *FrameView) OnPresent(fn func(PresentInfo)) {
	v.mu.Lock()
	v.onPresent = fn
	v.mu.Unlock()
}

// OnEvent sets the handler for events targeted at the view — mouse
// buttons and motion, scroll (MouseEvent with EventScroll), keys, chars,
// gestures, focus. Return true to consume. UI thread.
func (v *FrameView) OnEvent(fn func(qui.Event) bool) { v.onEvent = fn }

// Handle implements qui.Widget.
func (v *FrameView) Handle(e qui.Event) bool {
	if v.onEvent != nil {
		return v.onEvent(e)
	}
	return false
}

// SetFitMode selects how the frame is placed in the view: FitContain
// (default, letterboxed), FitCover (fill, cropped) or FitStretch.
func (v *FrameView) SetFitMode(m FitMode) {
	v.mu.Lock()
	v.fit = m
	v.mu.Unlock()
	v.Invalidate()
}

// FitMode returns the current fit mode.
func (v *FrameView) FitMode() FitMode {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.fit
}

// FrameSize returns the pixel size of the newest frame, or (0, 0) before
// the first push. Safe from any goroutine.
func (v *FrameView) FrameSize() (w, h int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.frameW, v.frameH
}

// ContentRect is where the frame is drawn, in the view's own coordinate
// space (the space of Bounds and of the mouse events it receives). Empty
// before the first frame. Under FitCover it extends past Bounds. UI thread.
func (v *FrameView) ContentRect() qui.Rect {
	w, h := v.FrameSize()
	if w <= 0 || h <= 0 {
		return qui.Rect{}
	}
	return fitDstRect(v.FitMode(), v.Bounds(), float32(w), float32(h))
}

// MapToFrame converts p — in the view's own coordinate space, as mouse
// events delivered to the view are — into frame pixel coordinates.
// inside reports whether p lies on the visible picture; outside it (the
// letterbox, or no frame yet) x and y are still the extrapolated
// coordinates, so a drag that leaves the picture can be clamped by the
// caller. UI thread.
func (v *FrameView) MapToFrame(p qui.Point) (x, y float64, inside bool) {
	w, h := v.FrameSize()
	return mapToFrame(v.ContentRect(), v.Bounds(), w, h, p)
}

func mapToFrame(content, bounds qui.Rect, fw, fh int, p qui.Point) (x, y float64, inside bool) {
	if fw <= 0 || fh <= 0 || content.W <= 0 || content.H <= 0 {
		return 0, 0, false
	}
	x = float64(p.X-content.X) * float64(fw) / float64(content.W)
	y = float64(p.Y-content.Y) * float64(fh) / float64(content.H)
	inside = x >= 0 && y >= 0 && x < float64(fw) && y < float64(fh) && bounds.Contains(p)
	return x, y, inside
}

// Focusable implements focus participation (default true).
func (v *FrameView) Focusable() bool { return v.focusable && v.Enabled() }

// SetFocusable toggles whether clicks and Tab focus the view.
func (v *FrameView) SetFocusable(b bool) { v.focusable = b }

// SetFocused is called by the window as focus moves.
func (v *FrameView) SetFocused(b bool) { v.focused = b }

// IsFocused reports whether the view holds keyboard focus.
func (v *FrameView) IsFocused() bool { return v.focused }

// SetRawKeyboard turns raw keyboard capture on or off for while the view
// is focused: keys bypass the input method, Tab focus navigation, window
// accelerators and (on macOS) menu key equivalents, and arrive as
// KeyEvents with the native key code in ScanCode. See
// qui.RawKeyboardTarget.
func (v *FrameView) SetRawKeyboard(on bool) { v.raw = on }

// WantsRawKeyboard implements qui.RawKeyboardTarget.
func (v *FrameView) WantsRawKeyboard() bool { return v.raw }

// HitTest makes the whole view a hit target.
func (v *FrameView) HitTest(p qui.Point) qui.Widget {
	if v.Bounds().Contains(p) {
		return v
	}
	return nil
}

// Measure reports the frame's natural size scaled to fit available,
// aspect preserved; 320×180 before the first frame.
func (v *FrameView) Measure(available qui.Size) qui.Size {
	w, h := v.FrameSize()
	if w <= 0 || h <= 0 {
		w, h = 320, 180
	}
	fw, fh := float32(w), float32(h)
	aw, ah := available.W, available.H
	switch {
	case aw <= 0 && ah <= 0:
		return qui.Size{W: fw, H: fh}
	case aw <= 0:
		return qui.Size{W: ah * fw / fh, H: ah}
	case ah <= 0:
		return qui.Size{W: aw, H: aw * fh / fw}
	case aw/fw <= ah/fh:
		return qui.Size{W: aw, H: aw * fh / fw}
	default:
		return qui.Size{W: ah * fw / fh, H: ah}
	}
}

// Layout records the owning window so pushes from other goroutines can
// reach it.
func (v *FrameView) Layout(r qui.Rect) {
	v.BaseWidget.Layout(r)
	v.win.Store(v.Window())
}

// Draw paints the letterbox background and registers the retained
// compositor draw that shows the frame. Without a GPU-presenting canvas
// (tests, headless, CPU presentation) an RGBA frame is drawn through the
// CPU canvas instead.
func (v *FrameView) Draw(canvas qui.Canvas) {
	v.win.Store(v.Window())
	bounds := v.Bounds()
	bg := v.Style().Background
	if bg == (qui.Color{}) {
		bg = qui.Color{A: 1}
	}
	canvas.FillRect(bounds, bg)

	content := v.ContentRect()
	rc, ok := canvas.(qui.RetainedCompositorCanvas)
	if !ok {
		v.retained.Store(false)
		if f := v.promote(); f != nil && !content.IsEmpty() {
			if img := f.cpuImage(); img != nil {
				canvas.DrawImage(img, content)
				v.notePresented(f)
			}
		}
		return
	}
	if content.IsEmpty() {
		// Nothing pushed yet: keep the background, but still let a
		// redraw-only frame find nothing to replay rather than a stale
		// picture.
		v.retained.Store(false)
		return
	}
	// Punch the 2D layer so the picture underneath shows through.
	if hole := content.Intersect(bounds); !hole.IsEmpty() {
		canvas.ClearRect(hole)
	}
	v.physBox = canvas.CurrentMatrix().TransformRect(bounds)
	rc.QueueRetainedDraw(v, v.physBox, v.composite)
	v.retained.Store(true)
}

// composite is the retained draw: promote the newest frame and composite
// it. Geometry is recomputed from the frame's own size so a redraw-only
// frame of a resized frame (before the relayout lands) is still
// proportioned.
func (v *FrameView) composite(c qui.Compositor) {
	f := v.promote()
	if f == nil {
		return
	}
	fw, fh := f.size()
	dst := fitDstRect(v.FitMode(), v.physBox, float32(fw), float32(fh))
	src, dst, ok := cropToVisible(dst, v.physBox, float32(fw), float32(fh))
	if !ok {
		return
	}
	if f.draw(v, c, src, dst) {
		v.notePresented(f)
	}
}

// cropToVisible clips dst (the frame's full on-screen rect) to visible and
// returns the matching source rect in frame pixels, so a FitCover overflow
// is cropped by sampling rather than by scissoring.
func cropToVisible(dst, visible qui.Rect, fw, fh float32) (src, out qui.Rect, ok bool) {
	if dst.W <= 0 || dst.H <= 0 || fw <= 0 || fh <= 0 {
		return qui.Rect{}, qui.Rect{}, false
	}
	out = dst.Intersect(visible)
	if out.IsEmpty() {
		return qui.Rect{}, qui.Rect{}, false
	}
	sx, sy := fw/dst.W, fh/dst.H
	src = qui.Rect{
		X: (out.X - dst.X) * sx,
		Y: (out.Y - dst.Y) * sy,
		W: out.W * sx,
		H: out.H * sy,
	}
	return src, out, true
}

// rgbaFrame is a PushRGBA frame.
type rgbaFrame struct{ img *image.RGBA }

func (f *rgbaFrame) size() (int, int)      { return f.img.Rect.Dx(), f.img.Rect.Dy() }
func (f *rgbaFrame) cpuImage() image.Image { return f.img }
func (f *rgbaFrame) release()              { f.img = nil }

func (f *rgbaFrame) draw(v *FrameView, c qui.Compositor, src, dst qui.Rect) bool {
	img := f.img
	if img == nil || len(img.Pix) == 0 {
		return false
	}
	return c.DrawPixels(v, unsafe.Pointer(&img.Pix[img.PixOffset(img.Rect.Min.X, img.Rect.Min.Y)]),
		img.Stride, img.Rect.Dx(), img.Rect.Dy(), false, src, dst)
}
