package qui

// WithClipRect is the lexically-scoped clip helper. It pushes a save
// frame, intersects the current clip with `rect`, runs `fn` against
// the same canvas (so the state stack is the only source of truth for
// the current clip), and restores when fn returns — even on panic.
//
// Replaces the old wrapper-based pattern:
//
//	inner := qui.NewClipCanvas(canvas, rect)
//	// draw using inner
//
// New code that needs a scope guard but wants to share a canvas
// reference with side-by-side drawing uses Save / RestoreTo manually:
//
//	id := canvas.Save()
//	canvas.ClipRect(gutterRect)
//	paintGutter(canvas)
//	canvas.RestoreTo(id)
//	id = canvas.Save()
//	canvas.ClipRect(textRect)
//	paintText(canvas)
//	canvas.RestoreTo(id)
func WithClipRect(canvas Canvas, rect Rect, fn func(Canvas)) {
	if canvas == nil || fn == nil {
		return
	}
	id := canvas.Save()
	defer canvas.RestoreTo(id)
	canvas.ClipRect(rect)
	fn(canvas)
}
