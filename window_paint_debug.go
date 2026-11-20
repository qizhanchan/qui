package qui

import (
	"fmt"
)

// drawMutationDebugEnabled is QUI_DEBUG_DRAW=1: assert that Draw does not mutate
// the retained tree or request a layout. The dirty-region renderer assumes
// the paint pass is read-only; a widget that re-parents, detaches, or
// invalidates layout while painting moves geometry after the clip for this
// frame was already chosen (and lazy lists lose focus when a row is
// released mid-paint).
var drawMutationDebugEnabled = envFlag("QUI_DEBUG_DRAW")

// assertNotPainting panics when called during Draw with QUI_DEBUG_DRAW=1.
// Keep it cheap in the default build: one bool check.
func (w *Window) assertNotPainting(op string) {
	if w == nil || !w.inPaint || !drawMutationDebugEnabled.Load() {
		return
	}
	panic(fmt.Sprintf("qui: %s during Draw; the paint pass must not mutate the widget tree or layout (set QUI_DEBUG_DRAW=0 to disable this check)", op))
}

// InPaintPass reports whether the window is in its frame's paint pass.
// A Draw outside it (a snapshot drawn straight into an image, a test)
// must not trust state cached per paint, such as a content cache keyed
// by PaintSeq.
func (w *Window) InPaintPass() bool { return w != nil && w.inPaint }
