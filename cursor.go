package qui

// Cursor resolution: which pointer shape the window shows for the current
// hover position.
//
// Two sources feed it, in strict priority order (see resolveCursor):
//
//  1. A DECLARED shape — what an author (or a stylesheet: CSS `cursor`)
//     asked for via BaseWidget.SetCursorShape. Innermost declaration wins.
//  2. A widget's BUILT-IN shape — the behavior baked into the widget type
//     (an I-beam over selectable text, a hand over a hyperlink), published
//     through CursorProvider / CursorAtProvider. Also innermost-first, but
//     only consulted when nothing in the hover path declared a shape.
//
// The two-tier rule is what makes CSS `cursor` behave like the browser:
// `cursor:pointer` on a box wins over the I-beam its own text label would
// otherwise claim (tier 1 beats tier 2 regardless of depth), while a page
// that declares no cursor at all keeps every widget's native feel.
//
// Resolution is driven from Window.syncHoverPath — one pass per mouse move,
// the same hook that drives hover enter/leave and tooltips. Widgets must
// therefore NOT call Window.SetCursor from their own event handlers: that
// runs after syncHoverPath and would fight the resolution every frame.
// Publish a provider instead.

// CursorProvider is implemented by widgets whose whole extent means one
// cursor shape — a hyperlink anchor (hand), a resize gripper (arrows).
// Return ok=false to decline (e.g. while disabled) and let an ancestor or
// the default answer.
type CursorProvider interface {
	WidgetCursor() (CursorShape, bool)
}

// CursorAtProvider is the per-POINT counterpart, for widgets that paint
// several interactive regions themselves instead of composing children —
// a text label that carries hyperlink runs wants a hand over the link and
// an I-beam over the rest, and it is one dispatch target either way.
//
// Checked before CursorProvider on the same widget: a per-point answer is
// strictly more specific. Same contract as TooltipAtProvider.
type CursorAtProvider interface {
	CursorAt(p Point) (CursorShape, bool)
}

// declaredCursorer is the tier-1 source. BaseWidget implements it, so
// every widget participates without per-widget wiring.
type declaredCursorer interface {
	DeclaredCursor() (CursorShape, bool)
}

// DeclaredCursor returns the shape set via SetCursorShape and whether one
// was set at all. Satisfies declaredCursorer for every widget embedding
// BaseWidget.
func (b *BaseWidget) DeclaredCursor() (CursorShape, bool) {
	return b.cursor, b.hasCursor
}

// SetCursorShape declares the pointer shape for this widget's extent,
// overriding any built-in shape the widget or its descendants would
// choose. This is the sink CSS `cursor` lands in.
//
// Unsupported shapes degrade to CursorDefault at the platform seam (see
// Window.SetCursor); declaring one still counts as a declaration, which is
// what makes `cursor: not-allowed` suppress a nested I-beam even though
// the platform draws a plain arrow for it.
func (b *BaseWidget) SetCursorShape(shape CursorShape) {
	b.assertUIThread("BaseWidget.SetCursorShape")
	b.cursor, b.hasCursor = shape, true
}

// ClearCursorShape drops a declaration made with SetCursorShape, handing
// the decision back to built-in widget behavior. Restyling calls this when
// an element no longer computes a `cursor` value.
func (b *BaseWidget) ClearCursorShape() {
	b.assertUIThread("BaseWidget.ClearCursorShape")
	b.cursor, b.hasCursor = CursorDefault, false
}

// updateCursorFromHover applies the shape resolved for the current
// hoverPath. Called from syncHoverPath after the path is updated, next to
// the tooltip pass.
func (w *Window) updateCursorFromHover(cursorX, cursorY float32) {
	w.SetCursor(w.resolveCursor(Point{X: cursorX, Y: cursorY}))
}

// resolveCursor implements the two-tier rule documented at the top of this
// file. Nothing claiming a shape means CursorDefault — the pointer resets
// itself when it slides off the last widget that cared.
func (w *Window) resolveCursor(p Point) CursorShape {
	// Tier 1: declared (CSS / author), innermost first.
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		if dc, ok := w.hoverPath[i].(declaredCursorer); ok {
			if shape, ok := dc.DeclaredCursor(); ok {
				return shape
			}
		}
	}
	// Tier 2: built-in widget behavior, innermost first.
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		hw := w.hoverPath[i]
		if cp, ok := hw.(CursorAtProvider); ok {
			// Localized: the provider tests the point against its own
			// geometry, which is scroll-independent (content coordinates).
			if shape, ok := cp.CursorAt(WindowPointToLocal(hw, p)); ok {
				return shape
			}
		}
		if cp, ok := hw.(CursorProvider); ok {
			if shape, ok := cp.WidgetCursor(); ok {
				return shape
			}
		}
	}
	return CursorDefault
}
