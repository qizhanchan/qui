package qui

import (
	"time"
)

// TextSelectable is implemented by widgets whose text can participate in
// a window-level, cross-widget text selection (browser-style: press in
// one widget, drag across several, Cmd/Ctrl+C copies the whole run in
// document order). It is the selection counterpart to Focusable /
// IMEClient — an optional interface the dispatch layer probes for.
//
// Offsets are rune indices into the widget's flattened text content
// (the same space the widget uses for its own intra-widget selection),
// clamped to [0, SelectableLength()]. All methods are called on the
// main goroutine from Window dispatch.
type TextSelectable interface {
	Widget

	// SelectionOffsetAt maps a point in THIS WIDGET's coordinate space —
	// the space its Bounds() and text layout live in — to a rune offset in
	// its content, clamped to [0, SelectableLength()]. The window's
	// selection controller works in window coordinates and maps each point
	// through WindowPointToLocal before calling in, so a widget inside a
	// scroll container (whose content bounds are scroll-independent) needs
	// no transform awareness of its own.
	SelectionOffsetAt(p Point) int

	// SetSelectionRange sets the highlighted range to [start, end] (rune
	// offsets, order-independent). A negative start, or start == end,
	// clears the highlight.
	SetSelectionRange(start, end int)

	// ClearTextSelection removes any selection highlight.
	ClearTextSelection()

	// SelectedText returns the currently-selected substring as plain
	// text, or "" when nothing is selected.
	SelectedText() string

	// SelectableLength returns the total rune count of the content, used
	// to express "select to the end of this widget".
	SelectableLength() int
}

// SelectionUnit is the granularity of a text selection gesture: single
// click anchors by character, double-click by word, triple-click by
// line/paragraph. The window's selection controller owns click counting
// and unit expansion; widgets only translate (offset, unit) → range via
// the optional SelectionGranular interface.
type SelectionUnit int

const (
	SelectionChar SelectionUnit = iota
	SelectionWord
	SelectionLine
)

// SelectionGranular is an optional companion to TextSelectable: widgets
// implementing it get browser-style double-click (word) and triple-click
// (line/paragraph) selection — plus word/line-granular drag extension —
// driven entirely by the window controller. Widgets that don't implement
// it fall back to character granularity for every gesture.
type SelectionGranular interface {
	TextSelectable

	// SelectionRangeAt expands the position at rune offset off to the
	// enclosing unit in the widget's own text semantics (word bounds,
	// hard-line bounds, whole paragraph, …). Char unit returns (off, off).
	SelectionRangeAt(off int, unit SelectionUnit) (start, end int)
}

// IsWordRune reports whether r belongs to a "word" for double-click
// selection purposes (letters, digits, underscore, and CJK ideographs
// group together; everything else — punctuation, spaces — breaks a word).
func IsWordRune(r rune) bool {
	return textWordRune(r)
}

// WordRange expands the position at rune offset off in text to the
// surrounding word run (or the surrounding run of non-word characters
// when off sits on punctuation/whitespace, matching editor conventions).
// Never crosses a '\n'. The shared implementation behind every widget's
// double-click.
func WordRange(text string, off int) (int, int) {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0, 0
	}
	if off >= len(runes) {
		off = len(runes) - 1
	}
	if off < 0 {
		off = 0
	}
	isWord := textWordRune(runes[off])
	start := off
	for start > 0 && runes[start-1] != '\n' && textWordRune(runes[start-1]) == isWord {
		start--
	}
	end := off + 1
	for end < len(runes) && runes[end] != '\n' && textWordRune(runes[end]) == isWord {
		end++
	}
	return start, end
}

// LineRange expands the position at rune offset off in text to the
// surrounding hard line (delimited by '\n' or the text edges). The
// shared implementation behind triple-click for widgets whose offset
// space uses '\n' for hard breaks.
func LineRange(text string, off int) (int, int) {
	runes := []rune(text)
	if len(runes) == 0 {
		return 0, 0
	}
	if off > len(runes) {
		off = len(runes)
	}
	if off < 0 {
		off = 0
	}
	start := off
	if start == len(runes) && start > 0 {
		start--
	}
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	end := off
	if end < start {
		end = start
	}
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	return start, end
}

// defaultTextSelection is the highlight used when the theme leaves the
// TextSelection token at its zero value — the historical selection blue.
var defaultTextSelection = Color{R: 0.24, G: 0.48, B: 0.86, A: 0.32}

// SelectionHighlight resolves the color a widget should paint behind its
// selected text: the theme's TextSelection token, switched to a light
// overlay when the widget's effective background is dark or saturated
// enough that a translucent blue would disappear into it (e.g. white
// text on a brand-colored card).
func SelectionHighlight(w Widget) Color {
	sel := CurrentTheme().TextSelection
	if sel.A == 0 {
		sel = defaultTextSelection
	}
	bg, ok := effectiveBackground(w)
	if !ok {
		bg = CurrentTheme().Surface
	}
	if relativeLuma(bg) < 0.5 {
		return Color{R: 1, G: 1, B: 1, A: 0.4}
	}
	return sel
}

// effectiveBackground walks up from w to the nearest ancestor (inclusive)
// painting a mostly-opaque background, mirroring what actually sits
// behind the widget's glyphs.
func effectiveBackground(w Widget) (Color, bool) {
	for cur := w; cur != nil; cur = cur.Parent() {
		if bg := cur.Style().Background; bg.A > 0.5 {
			return bg, true
		}
	}
	return Color{}, false
}

// relativeLuma is the perceptual luma (ITU-R BT.601) of a color, 0..1.
func relativeLuma(c Color) float32 {
	return 0.299*c.R + 0.587*c.G + 0.114*c.B
}

// selectionOwnerRoles are the widget roles whose press owns the mouse
// gesture, so a blank press inside them must not arm a selection drag
// (browsers don't drag-select from a button or slider either). Scroll
// and list containers are intentionally absent — a drag from a page
// margin inside a ScrollView is exactly the blank-press case.
var selectionOwnerRoles = map[string]bool{
	RoleButton:   true,
	RoleTextbox:  true,
	RoleTextarea: true,
	RoleCheckbox: true,
	RoleRadio:    true,
	RoleSlider:   true,
	RoleCombobox: true,
	RoleTab:      true,
	RoleMenu:     true,
	RoleMenuitem: true,
	RoleLink:     true,
	"switch":     true,
	"select":     true,
	"input":      true,
}

// selectionClicks tracks consecutive clicks for the window's selection
// controller (double-click = word, triple-click = line). It outlives the
// per-drag state so the second/third press of a multi-click sees the
// earlier ones.
type selectionClicks struct {
	lastMS int64
	x, y   float32
	anchor TextSelectable
	count  int
}

// register counts a press at (x, y, when) on anchor, mirroring the
// classic 400ms / 4px multi-click rules; a fourth click restarts at 1.
func (c *selectionClicks) register(anchor TextSelectable, x, y float32, when time.Time) int {
	now := when.UnixMilli()
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	sameSpot := absf32(x-c.x) <= 4 && absf32(y-c.y) <= 4
	if now-c.lastMS <= 400 && sameSpot && anchor == c.anchor {
		c.count++
	} else {
		c.count = 1
	}
	if c.count > 3 {
		c.count = 1
	}
	c.lastMS = now
	c.x, c.y = x, y
	c.anchor = anchor
	return c.count
}

// clickCounter derives MouseEvent.Clicks: consecutive presses of one
// button within 400ms and 4px. Unlike selectionClicks it doesn't wrap, and
// a release reports the count of the press it ends.
type clickCounter struct {
	lastMS int64
	x, y   float32
	button MouseButton
	n      int
}

func (c *clickCounter) count(me MouseEvent) int {
	if me.eventType == EventMouseUp {
		if c.n == 0 || c.button != me.Button {
			return 1
		}
		return c.n
	}
	now := me.When.UnixMilli()
	if me.When.IsZero() {
		now = time.Now().UnixMilli()
	}
	sameSpot := absf32(me.X-c.x) <= 4 && absf32(me.Y-c.y) <= 4
	if c.n > 0 && now-c.lastMS <= 400 && sameSpot && me.Button == c.button {
		c.n++
	} else {
		c.n = 1
	}
	c.lastMS, c.x, c.y, c.button = now, me.X, me.Y, me.Button
	return c.n
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// unitForClicks maps a click count to its selection granularity.
func unitForClicks(count int) SelectionUnit {
	switch count {
	case 2:
		return SelectionWord
	case 3:
		return SelectionLine
	}
	return SelectionChar
}

// expandToUnit widens a rune offset in ts to the gesture's unit bounds.
// Char unit — or a widget without granular support — stays a point.
func expandToUnit(ts TextSelectable, off int, unit SelectionUnit) (int, int) {
	if unit == SelectionChar {
		return off, off
	}
	if g, ok := ts.(SelectionGranular); ok {
		return g.SelectionRangeAt(off, unit)
	}
	return off, off
}

// AutoScrollable is implemented by scroll containers (widgets.ScrollView)
// so a selection drag that reaches the viewport edge can keep scrolling
// to reveal — and select — content beyond the visible area. It is probed
// structurally; ScrollView already satisfies it.
type AutoScrollable interface {
	Widget
	ScrollOffset() float32
	MaxScroll() float32
	ScrollTo(off float32)
}

// RichTextSelectable is an optional companion to TextSelectable: widgets
// that carry styling expose their selected content as styled spans so the
// window aggregator can build an HTML clipboard flavor (bold / italic /
// size / color preserved when pasting into Word, Pages, browsers).
// Widgets that don't implement it contribute plain (escaped) text.
type RichTextSelectable interface {
	TextSelectable
	// SelectedSpans returns the styled spans within the current
	// selection, with nil Font/Color resolved to the widget's own base
	// style so each span is self-describing.
	SelectedSpans() []TextSpan
}

// textSelectionToggle lets a TextSelectable opt out of window-driven
// selection while it's inert — e.g. a list-marker Label, which browsers
// don't let you select. A TextSelectable without this method always
// participates.
type textSelectionToggle interface {
	TextSelectionEnabled() bool
}

// textSelectionLeaf marks a TextSelectable that fully represents its own
// subtree's selectable text (an InlineBox that folds atomic inline
// children into its own offset space). The window's collector stops
// descending into it so those atoms aren't also collected on their own.
type textSelectionLeaf interface {
	OwnsDescendantSelection() bool
}
