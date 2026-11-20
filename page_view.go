package qui

import (
	"image"
	"sync/atomic"
)

// PageView shows a PageHost inside an ordinary window (RFC 0005 §3.1):
// it displays the page's latest committed frame, keeps the page viewport
// matched to its own bounds, and forwards input to the page thread. It
// never waits for the page: a page busy in a long script keeps showing its
// last frame while the rest of the window stays responsive.
type PageView struct {
	BaseWidget
	host *PageHost

	// front is the UI thread's copy of the committed frame (physical
	// pixels), refreshed from the host's store when a commit arrives.
	front     *image.RGBA
	frontSeq  uint64
	pulling   atomic.Bool
	win       atomic.Pointer[Window]
	scale     float32
	vpW, vpH  int
	focused   bool
	pressed   int
	hovering  bool
	lastLocal Point
}

// NewPageView returns a view of h. One view per host.
func NewPageView(h *PageHost) *PageView {
	v := &PageView{BaseWidget: NewBaseWidget(), host: h}
	v.SetSelf(v)
	h.OnFrame(v.frameCommitted)
	h.OnCursor(func(CursorShape) {
		if w := v.win.Load(); w != nil {
			w.PostJob(w.RefreshCursor)
		}
	})
	return v
}

// Host is the page shown.
func (v *PageView) Host() *PageHost { return v.host }

// frameCommitted runs on the page thread after each commit: ask the UI
// thread to pull it, once per burst.
func (v *PageView) frameCommitted() {
	w := v.win.Load()
	if w == nil || !v.pulling.CompareAndSwap(false, true) {
		return
	}
	w.PostJob(v.pullFrame)
}

// pullFrame copies the newly committed area into front and repaints it.
func (v *PageView) pullFrame() {
	v.pulling.Store(false)
	if v.host == nil {
		return
	}
	prev := v.front
	front, changed, seq, ok := v.host.TakeFrame(v.front)
	v.front = front
	if !ok {
		return
	}
	v.frontSeq = seq
	if prev != front {
		v.Invalidate()
		return
	}
	s := v.scale
	if s <= 0 {
		s = 1
	}
	b := v.Bounds()
	v.InvalidateRect(Rect{
		X: b.X + float32(changed.Min.X)/s, Y: b.Y + float32(changed.Min.Y)/s,
		W: float32(changed.Dx())/s + 1, H: float32(changed.Dy())/s + 1,
	})
}

// FrameSeq is the commit sequence number of the frame on screen.
func (v *PageView) FrameSeq() uint64 { return v.frontSeq }

func (v *PageView) Measure(avail Size) Size { return avail }

func (v *PageView) Layout(r Rect) {
	v.BaseWidget.Layout(r)
	w := v.Window()
	v.win.Store(w)
	scale := float32(1)
	if w != nil {
		scale = w.DevicePixelRatio()
	}
	vw, vh := int(r.W+0.5), int(r.H+0.5)
	if vw != v.vpW || vh != v.vpH || scale != v.scale {
		v.vpW, v.vpH, v.scale = vw, vh, scale
		v.host.SetViewport(vw, vh, scale)
	}
	// A commit that arrived before the view had a window was not announced.
	if w != nil && v.host.FrameSeq() != v.frontSeq && v.pulling.CompareAndSwap(false, true) {
		w.PostJob(v.pullFrame)
	}
}

func (v *PageView) Draw(c Canvas) {
	b := v.Bounds()
	c.FillRect(b, ColorWhite)
	if v.front == nil {
		return
	}
	s := v.scale
	if s <= 0 {
		s = 1
	}
	fb := v.front.Bounds()
	c.Save()
	c.ClipRect(b)
	c.DrawImage(v.front, Rect{X: b.X, Y: b.Y, W: float32(fb.Dx()) / s, H: float32(fb.Dy()) / s})
	c.Restore()
}

// Focusable: the page takes keyboard focus when clicked.
func (v *PageView) Focusable() bool { return true }

func (v *PageView) SetFocused(f bool) {
	if f == v.focused {
		return
	}
	v.focused = f
	v.host.SendFocus(f)
}

// WidgetCursor shows the shape the page asked for.
func (v *PageView) WidgetCursor() (CursorShape, bool) { return v.host.Cursor(), true }

func (v *PageView) local(x, y float32) (float64, float64) {
	p := WindowPointToLocal(v, Point{X: x, Y: y})
	b := v.Bounds()
	v.lastLocal = Point{X: p.X - b.X, Y: p.Y - b.Y}
	return float64(p.X - b.X), float64(p.Y - b.Y)
}

func (v *PageView) Handle(ev Event) bool {
	switch e := ev.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseMove:
			x, y := v.local(e.X, e.Y)
			v.hovering = true
			v.host.SendMouseMove(x, y, e.Mods)
			return true
		case EventMouseLeave:
			if v.hovering && v.pressed == 0 {
				v.hovering = false
				v.host.SendMouseMove(-1, -1, e.Mods)
			}
			return false
		case EventMouseDown:
			if w := v.Window(); w != nil && !v.focused {
				w.SetFocus(v)
			}
			v.pressed++
			x, y := v.local(e.X, e.Y)
			v.host.SendMouseButton(e.Button, true, x, y, e.Mods)
			return true
		case EventMouseUp:
			if v.pressed > 0 {
				v.pressed--
			}
			x, y := v.local(e.X, e.Y)
			v.host.SendMouseButton(e.Button, false, x, y, e.Mods)
			return true
		case EventScroll:
			x, y := v.local(e.X, e.Y)
			v.host.SendScroll(x, y, float64(e.DeltaX), float64(e.DeltaY), e.Mods, e.ScrollPhase, e.PreciseScroll)
			return true
		}
	case KeyEvent:
		if !v.focused {
			return false
		}
		v.host.SendKey(e.Key, e.ScanCode, e.Type() == EventKeyDown, e.Repeat, e.Mods)
		return true
	case CharEvent:
		if !v.focused {
			return false
		}
		v.host.SendChar(e.Rune)
		return true
	case GestureEvent:
		// Ctrl/Cmd+wheel pinches are synthesized from the wheel event the
		// page already received; the page window makes its own.
		if e.Synthetic {
			return false
		}
		v.host.SendGesture(e.Type(), e.GesturePhase, e.DScale, e.DRotation, e.Mods)
		return true
	}
	return false
}
