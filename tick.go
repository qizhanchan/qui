package qui

import "time"

// Tickable is implemented by widgets that need per-frame state updates
// (cursor blink, ongoing animations, hover timers). Tick returns the
// region of the WINDOW that should be redrawn this frame, in window
// coordinates — map a widget-space rect with RectInWindow (a bare Bounds()
// is off by the scroll offset inside a ScrollView). An empty Rect means no
// change. A non-empty Rect is unioned into the window's dirty region and
// only those pixels are re-rasterized.
//
// Widgets that do not animate simply do not implement this interface.
type Tickable interface {
	Tick(now time.Time) Rect
}

// TickWidget invokes Tick on a widget. If the widget implements
// Tickable it is responsible for its own subtree (Container does this).
// Otherwise we walk ChildList() and recurse — that way any
// container-like widget (ScrollView, Frame, TabView, Dialog body, etc.)
// gets per-frame propagation by default, without each one having to
// remember to implement Tick + dispatch.
//
// Returns the union of dirty rects reported by the widget (or its
// children, in the fallback path). Zero Rect means nothing to repaint.
//
// A container that implements Tickable for its own animation owns its
// subtree's ticks, and should forward to each child through TickWidget
// rather than a bare Tickable assertion — the assertion skips a child
// that isn't Tickable itself (a ScrollView, a third-party container)
// along with everything under it.
func TickWidget(w Widget, now time.Time) Rect { return tickWidget(w, now) }

func tickWidget(w Widget, now time.Time) Rect {
	if w == nil {
		return Rect{}
	}
	if t, ok := w.(Tickable); ok {
		return t.Tick(now)
	}
	if l, ok := w.(childLister); ok {
		var dirty Rect
		for _, child := range l.ChildList() {
			dirty = dirty.Union(tickWidget(child, now))
		}
		return dirty
	}
	return Rect{}
}
