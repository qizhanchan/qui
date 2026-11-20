package qui

import (
	"image"
	"log"
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

	// layers: the committed scroll layers and what the UI thread shows of
	// them (page_view_scroll.go).
	layers   []*viewLayer
	layerSeq uint64

	// draws: the page's committed overlay draws (page_host_retained.go).
	draws    []PageRetainedDraw
	drawsSeq uint64

	// meta: page state from the last commit (IME).
	meta PageMeta
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
	u, ok := v.host.TakeFrame(v.front)
	v.front = u.Image
	if !ok {
		return
	}
	v.frontSeq = u.Seq
	v.meta = u.Meta
	v.adoptDraws(u)
	moved := v.adoptLayers(u.Layers)
	if prev != v.front {
		v.Invalidate()
		return
	}
	if !u.Changed.Empty() {
		v.invalidateDevice(u.Changed)
	}
	for _, r := range moved {
		v.invalidateDevice(r)
	}
}

// invalidateDevice repaints r, in page device pixels.
func (v *PageView) invalidateDevice(r image.Rectangle) {
	s := v.pageScale()
	b := v.Bounds()
	v.InvalidateRect(Rect{
		X: b.X + float32(r.Min.X)/s, Y: b.Y + float32(r.Min.Y)/s,
		W: float32(r.Dx())/s + 1, H: float32(r.Dy())/s + 1,
	})
}

func (v *PageView) pageScale() float32 {
	if v.scale <= 0 {
		return 1
	}
	return v.scale
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
	v.drawMovedLayers(c)
	c.Restore()
	v.queueDraws(c)
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
			if debugPageView {
				log.Printf("qui/pageview: mouse up at page %.0f,%.0f", e.X-v.Bounds().X, e.Y-v.Bounds().Y)
			}
			if v.pressed > 0 {
				v.pressed--
			}
			x, y := v.local(e.X, e.Y)
			v.host.SendMouseButton(e.Button, false, x, y, e.Mods)
			return true
		case EventScroll:
			x, y := v.local(e.X, e.Y)
			if v.scrollLayer(e, x, y) {
				return true
			}
			v.host.SendScroll(x, y, float64(e.DeltaX), float64(e.DeltaY), e.Mods, e.ScrollPhase, e.PreciseScroll)
			return true
		}
	case KeyEvent:
		if !v.focused {
			return false
		}
		// The host window's shortcuts win over the page's (a browser's
		// Back, New Tab, Close Tab): leave those keys to its accelerators.
		if w := v.Window(); w != nil && w.Accelerators().Has(e.Key, e.Mods, v) {
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
	case DragEvent:
		if e.Type() == EventDrop && e.Source == nil && e.Data != nil && len(e.Data.Files) > 0 {
			x, y := v.local(e.X, e.Y)
			v.host.SendFileDrop(e.Data.Files, x, y)
			return true
		}
		return e.Source == nil && (e.Type() == EventDragEnter || e.Type() == EventDragOver)
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

// Droppable: files dropped on the view go to the page.
func (v *PageView) Droppable() bool { return true }

// AcceptsDrop takes OS file drops only; drags inside the page stay there.
func (v *PageView) AcceptsDrop(d *DragData) bool { return d != nil && len(d.Files) > 0 }

// IME: the view stands in for the page's focused widget, using what the
// last commit reported (PageMeta).

// IMEActive implements IMEActivity.
func (v *PageView) IMEActive() bool { return v.focused && v.meta.IMEActive }

// SetPreedit implements IMEClient.
func (v *PageView) SetPreedit(text string, cursor int) { v.host.SendPreedit(text, cursor) }

// CommitIME implements IMEClient.
func (v *PageView) CommitIME(text string) { v.host.SendCommitText(text) }

// CaretRect implements IMEClient: the page's caret in the view's space.
func (v *PageView) CaretRect() Rect {
	b := v.Bounds()
	c := v.meta.Caret
	return Rect{X: b.X + c.X, Y: b.Y + c.Y, W: c.W, H: c.H}
}
