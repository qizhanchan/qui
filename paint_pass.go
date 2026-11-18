package qui

// Paint passes.
//
// While a window paints, the widget tree must not change (QUI_DEBUG_DRAW
// enforces it), so whatever a widget derives from its subtree's geometry
// — the union of its descendants' paint bounds, which culling asks of
// every child of every drawn box — holds for the whole pass. PaintPass
// numbers the passes so such values can be memoized for exactly one:
// recomputing them per query made culling a long page's boxes cost a
// subtree walk per box, per dirty rectangle.

var (
	paintPassGen    uint64
	paintPassActive bool
)

// PaintPass reports the current paint pass: its number (never 0) and
// whether a window is painting right now. A value memoized under a pass
// number is valid only while active is true and the number is the same.
func PaintPass() (gen uint64, active bool) { return paintPassGen, paintPassActive }

func beginPaintPass() {
	paintPassGen++
	paintPassActive = true
}

func endPaintPass() { paintPassActive = false }
