package qui

// Draw suppression: a retained layer that renders a subtree into its own
// buffer (ScrollView's content cache) leaves out the boxes it composites
// separately on top (fixed and sticky boxes). Draw implementations that
// can be such a box check DrawSuppressed first.
//
// The suppressed set belongs to the window that is painting — page windows
// (RFC 0005) paint on their own threads — and suppressions nest: a nested
// scroller recording its own tiles inside its parent's recording adds its
// boxes and, when it is done, restores the parent's set.

// SuppressDraw makes DrawSuppressed report ws (in addition to what is
// already suppressed) until the returned restore runs. UI thread of w.
func (w *Window) SuppressDraw(ws []Widget) (restore func()) {
	if w == nil {
		return func() {}
	}
	prev := w.drawSuppressed
	if len(ws) > 0 {
		m := make(map[Widget]struct{}, len(prev)+len(ws))
		for k := range prev {
			m[k] = struct{}{}
		}
		for _, x := range ws {
			m[x] = struct{}{}
		}
		w.drawSuppressed = m
	}
	return func() { w.drawSuppressed = prev }
}

// DrawSuppressed reports whether widget must not draw now (its window is
// rendering a layer that composites it separately).
func DrawSuppressed(widget Widget) bool {
	if widget == nil {
		return false
	}
	w := widgetWindow(widget)
	if w == nil || len(w.drawSuppressed) == 0 {
		return false
	}
	_, ok := w.drawSuppressed[widget]
	return ok
}
