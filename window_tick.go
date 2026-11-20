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
// Ticks are not on a fixed clock: an idle window is not Stepped at all
// (see window_idle.go). Returning a dirty rect keeps frames coming, so an
// animation needs nothing more. A Tickable that changes after a quiet
// stretch — a blink, a delayed reveal — must call Window.RequestTickAt
// with the moment it next needs a Tick, or it waits for the next event.
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
		r := t.Tick(now)
		if debugIdle && !r.IsEmpty() {
			if _, isC := w.(*Container); !isC {
				idleDebugTicker(w)
			}
		}
		return r
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

// treeTickSafetyNet bounds how long the tree walk may be skipped, for
// Tickables that poll state without asking for a tick (see Tickable).
const treeTickSafetyNet = time.Second

// treeTickDue reports whether this Step walks the widget tree's
// Tickables. The walk visits every widget, which on a large document
// (a web page of tens of thousands of boxes) costs far more than the
// job that woke the loop — a page's setInterval that changes nothing
// would pay a full tree walk sixty times a second. Like a browser that
// runs its animation/lifecycle steps only when a frame was requested,
// the walk runs when something could have started or continued an
// animation: the last walk reported a change, the last Step rasterized
// widgets (GPU-only redraws of retained draws do not count),
// a RequestTickAt deadline arrived,
// input was dispatched, or this Step has dirty paint or layout (a job
// changed something). Otherwise only the safety net keeps it going.
func (w *Window) treeTickDue(now time.Time, frameWanted bool) bool {
	due := frameWanted || w.tickWanted || !w.dirtyRegion.IsEmpty() ||
		w.root.IsLayoutDirty() || now.Sub(w.lastTreeTick) >= treeTickSafetyNet
	if due {
		w.tickWanted = false
		w.lastTreeTick = now
	}
	return due
}
