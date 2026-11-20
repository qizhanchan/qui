package qui

import "sync/atomic"

// Paint passes.
//
// While a window paints, the widget tree must not change (QUI_DEBUG_DRAW
// enforces it), so whatever a widget derives from its subtree's geometry
// — the union of its descendants' paint bounds, which culling asks of
// every child of every drawn box — holds for the whole pass. PaintPass
// numbers the passes so such values can be memoized for exactly one:
// recomputing them per query made culling a long page's boxes cost a
// subtree walk per box, per dirty rectangle.
//
// Passes are per window: page windows (RFC 0005) paint on their own
// threads, concurrently with the UI thread. Numbers come from one
// process-wide counter so a widget moved between windows can never see a
// number reused.

var paintPassSeq atomic.Uint64

// PaintPass reports w's current paint pass: its number (never 0) and
// whether w is painting right now. A value memoized under a pass number is
// valid only while active is true and the number is the same. UI thread
// of w.
func (w *Window) PaintPass() (gen uint64, active bool) {
	if w == nil {
		return 0, false
	}
	return w.paintGen, w.inPaint
}

func (w *Window) beginPaintPass() { w.paintGen = paintPassSeq.Add(1) }
