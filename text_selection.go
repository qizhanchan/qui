package qui

import (
	"strconv"
	"strings"
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

// textSelectionDrag holds the state of an in-progress cross-widget text
// selection drag. It is owned by Window and lives only between the
// MouseDown that starts a selection and the MouseUp that ends it; the
// resulting per-widget highlight ranges persist on the widgets after the
// drag completes (so the selection stays visible and copyable).
type textSelectionDrag struct {
	active    bool
	anchor    TextSelectable // widget where MouseDown landed
	anchorIdx int            // anchor's index within flat
	anchorOff int            // rune offset of the press point in anchor
	unit      SelectionUnit  // gesture granularity (click count at press)
	// anchorStart/anchorEnd is the anchor offset expanded to unit bounds
	// (== anchorOff twice for char unit). Drag extension unions the focus
	// unit range with this range, browser-style.
	anchorStart int
	anchorEnd   int
	flat        []TextSelectable // all selectables in document order
	loTouched   int              // range of flat indices currently highlighted
	hiTouched   int
	lastMouse   Point          // most recent drag point (drives auto-scroll)
	scroller    AutoScrollable // nearest scroll ancestor of the anchor, if any
	autoTick    *selectionAutoScroller
	// fromBlank marks a drag armed by a press on empty space (no
	// TextSelectable under the cursor) whose anchor snapped to the
	// nearest document position. It is provisional: if a widget consumes
	// that MouseDown (a scrollbar thumb, a slider), dispatch disarms it
	// so the widget's own drag gesture wins.
	fromBlank bool
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

// beginTextSelectionDrag is called from dispatch on a left MouseDown. If
// the press lands on a TextSelectable it arms a cross-widget drag and
// clears any previous selection; otherwise it clears the selection and
// disarms (clicking blank space deselects, browser-style). The controller
// owns click counting: a double-click press immediately selects the word
// under the cursor, a triple-click the line — and the subsequent drag
// extends by that unit (via the anchor's SelectionGranular support).
// Shift+click extends the previous selection from its original anchor
// instead of starting over.
func (w *Window) beginTextSelectionDrag(target Widget, me MouseEvent) {
	if me.Button != MouseButtonLeft {
		return
	}
	// Tear down any auto-scroller left over from a previous drag before
	// the struct is replaced.
	if w.textSel.autoTick != nil {
		w.UnregisterAnimator(w.textSel.autoTick)
		w.textSel.autoTick = nil
	}
	w.textSel.active = false

	// Shift+click: keep the previous gesture's anchor and re-extend to
	// the new point (browser behavior). Falls through to a fresh press
	// when there is no usable prior anchor.
	if me.Mods&ModShift != 0 && w.textSel.anchor != nil {
		d := &w.textSel
		d.flat = w.collectTextSelectables()
		if idx := indexOfSelectable(d.flat, d.anchor); idx >= 0 {
			d.anchorIdx = idx
			if d.loTouched > len(d.flat)-1 {
				d.loTouched = idx
			}
			if d.hiTouched > len(d.flat)-1 {
				d.hiTouched = idx
			}
			d.active = true
			d.lastMouse = Point{X: me.X, Y: me.Y}
			if d.scroller == nil {
				d.scroller = ascendAutoScrollable(d.anchor)
			}
			if d.scroller != nil && d.autoTick == nil {
				d.autoTick = &selectionAutoScroller{w: w}
				w.RegisterAnimator(d.autoTick)
			}
			w.extendTextSelection(d.lastMouse)
			return
		}
	}

	anchor := ascendTextSelectable(target)
	// A fresh press always drops the prior selection.
	w.clearAllTextSelection()

	p := Point{X: me.X, Y: me.Y}
	fromBlank := false
	flat := w.collectTextSelectables()

	if anchor == nil {
		// Press on empty space: arm a provisional drag whose anchor snaps
		// to the nearest document position (browser behavior — a drag
		// started in a margin or gap still selects the text it sweeps
		// over). Dispatch disarms it if a widget consumes this MouseDown.
		w.selClicks = selectionClicks{}
		// Presses inside an interactive control (a button's padding, a
		// slider, an input) belong to the control, not to selection —
		// browsers don't start a selection drag from a button either.
		// Focusable containers (ScrollView, ListView) deliberately don't
		// count: dragging from a scrollable page's margin must select.
		for cur := target; cur != nil; cur = cur.Parent() {
			if selectionOwnerRoles[WidgetRole(cur)] {
				w.textSel = textSelectionDrag{}
				return
			}
		}
		idx, off := resolveSelectionFocusIn(flat, p)
		if idx < 0 {
			w.textSel = textSelectionDrag{}
			return
		}
		anchor = flat[idx]
		fromBlank = true
		w.textSel = textSelectionDrag{
			active:      true,
			anchor:      anchor,
			anchorIdx:   idx,
			anchorOff:   off,
			unit:        SelectionChar,
			anchorStart: off,
			anchorEnd:   off,
			flat:        flat,
			loTouched:   idx,
			hiTouched:   idx,
			lastMouse:   p,
			scroller:    ascendAutoScrollable(anchor),
			fromBlank:   fromBlank,
		}
		if w.textSel.scroller != nil {
			w.textSel.autoTick = &selectionAutoScroller{w: w}
			w.RegisterAnimator(w.textSel.autoTick)
		}
		return
	}

	idx := indexOfSelectable(flat, anchor)
	if idx < 0 {
		w.textSel = textSelectionDrag{}
		w.selClicks = selectionClicks{}
		return
	}
	unit := unitForClicks(w.selClicks.register(anchor, me.X, me.Y, me.When))
	off := anchor.SelectionOffsetAt(WindowPointToLocal(anchor, p))
	aStart, aEnd := expandToUnit(anchor, off, unit)
	w.textSel = textSelectionDrag{
		active:      true,
		anchor:      anchor,
		anchorIdx:   idx,
		anchorOff:   off,
		unit:        unit,
		anchorStart: aStart,
		anchorEnd:   aEnd,
		flat:        flat,
		loTouched:   idx,
		hiTouched:   idx,
		lastMouse:   p,
		scroller:    ascendAutoScrollable(anchor),
	}
	// A word / line press selects its unit range immediately (browsers
	// highlight the word on the second down, not on release).
	if aEnd > aStart {
		anchor.SetSelectionRange(aStart, aEnd)
	}
	// Arm edge auto-scroll for the whole drag when the anchor lives in a
	// scroll container. The animator idles (cheap) until the cursor
	// enters an edge band, and self-prunes when the drag ends.
	if w.textSel.scroller != nil {
		w.textSel.autoTick = &selectionAutoScroller{w: w}
		w.RegisterAnimator(w.textSel.autoTick)
	}
}

// updateTextSelectionDrag is called from dispatch on MouseMove while a
// drag is active. It returns true when the selection controller owns the
// gesture, telling dispatch NOT to also route the move to the captured
// widget (whose own handlers no longer implement selection drags).
func (w *Window) updateTextSelectionDrag(me MouseEvent) bool {
	if w.textSel.active {
		w.textSel.lastMouse = Point{X: me.X, Y: me.Y}
	}
	return w.extendTextSelection(Point{X: me.X, Y: me.Y})
}

// extendTextSelection applies the selection for a focus point — the
// single driver for intra-widget drags, cross-widget runs, and unit
// (word / line) extension. Split out from updateTextSelectionDrag so the
// auto-scroller can re-extend from the last mouse point without a
// MouseMove event.
func (w *Window) extendTextSelection(p Point) bool {
	d := &w.textSel
	if !d.active || d.anchor == nil {
		return false
	}
	focusIdx, focusOff := w.resolveSelectionFocus(p)
	if focusIdx < 0 {
		return true // controller still owns the gesture
	}
	// Expand the focus position to the gesture's unit (word / line drag
	// extends whole units, browser-style; char unit stays a point).
	fStart, fEnd := expandToUnit(d.flat[focusIdx], focusOff, d.unit)

	// Union the anchor's unit range with the focus unit range, expressed
	// as an ordered [ (loIdx,loOff), (hiIdx,hiOff) ] document span.
	var loIdx, loOff, hiIdx, hiOff int
	switch {
	case focusIdx < d.anchorIdx:
		loIdx, loOff, hiIdx, hiOff = focusIdx, fStart, d.anchorIdx, d.anchorEnd
	case focusIdx > d.anchorIdx:
		loIdx, loOff, hiIdx, hiOff = d.anchorIdx, d.anchorStart, focusIdx, fEnd
	default:
		loIdx, hiIdx = focusIdx, focusIdx
		loOff = minInt(d.anchorStart, fStart)
		hiOff = maxInt(d.anchorEnd, fEnd)
	}

	// Clear widgets that were highlighted last move but fall outside the
	// new [loIdx, hiIdx] band.
	for i := d.loTouched; i <= d.hiTouched && i < len(d.flat); i++ {
		if i < loIdx || i > hiIdx {
			d.flat[i].ClearTextSelection()
		}
	}
	for i := loIdx; i <= hiIdx; i++ {
		ts := d.flat[i]
		start, end := 0, ts.SelectableLength()
		if i == loIdx {
			start = loOff
		}
		if i == hiIdx {
			end = hiOff
		}
		ts.SetSelectionRange(start, end)
	}
	d.loTouched, d.hiTouched = loIdx, hiIdx
	return true
}

// endTextSelectionDrag marks the drag finished, leaving the resulting
// highlight ranges in place for copy, and disarms edge auto-scroll.
func (w *Window) endTextSelectionDrag() {
	w.textSel.active = false
	if w.textSel.autoTick != nil {
		w.UnregisterAnimator(w.textSel.autoTick)
		w.textSel.autoTick = nil
	}
}

// selectAllText highlights every selectable in the window (document-wide
// Cmd/Ctrl+A). It only takes over when the focused widget is itself a
// TextSelectable — so editable text widgets (Input / TextArea) keep
// their own field-scoped select-all. Returns true when it handled the
// event.
func (w *Window) selectAllText() bool {
	if _, ok := w.focused.(TextSelectable); !ok {
		return false
	}
	flat := w.collectTextSelectables()
	if len(flat) == 0 {
		return false
	}
	for _, ts := range flat {
		ts.SetSelectionRange(0, ts.SelectableLength())
	}
	return true
}

// selectionAutoScroller is the per-drag Animator that scrolls the
// anchor's scroll container while the cursor sits in an edge band, then
// re-extends the selection into the freshly revealed content. It idles
// (no scrolling, stays registered) when the cursor is away from the
// edges, and reports done once the drag ends.
type selectionAutoScroller struct{ w *Window }

func (s *selectionAutoScroller) Stop() {}

func (s *selectionAutoScroller) Tick(time.Time) (Rect, bool) {
	d := &s.w.textSel
	if !d.active || d.scroller == nil {
		return Rect{}, true // drag over — prune
	}
	vp := InteractionBoundsOf(d.scroller)
	if vp.H <= 0 {
		return Rect{}, false
	}
	const margin = 28 // edge band depth, logical px
	y := d.lastMouse.Y
	off := d.scroller.ScrollOffset()
	var delta float32
	switch {
	case y > vp.Y+vp.H-margin:
		delta = autoScrollStep(y - (vp.Y + vp.H - margin))
	case y < vp.Y+margin:
		delta = -autoScrollStep((vp.Y + margin) - y)
	default:
		return Rect{}, false // not in an edge band — idle
	}
	newOff := off + delta
	if newOff < 0 {
		newOff = 0
	}
	if max := d.scroller.MaxScroll(); newOff > max {
		newOff = max
	}
	if newOff == off {
		return Rect{}, false // already at the limit
	}
	d.scroller.ScrollTo(newOff)
	s.w.extendTextSelection(d.lastMouse)
	return vp, false
}

// autoScrollStep ramps the per-frame scroll distance with how deep the
// cursor is past the edge — slow near the border, faster the further out.
func autoScrollStep(depth float32) float32 {
	const base, perPx, cap = 5, 0.55, 36
	step := base + depth*perPx
	if step > cap {
		step = cap
	}
	return step
}

// ascendAutoScrollable walks up from w to the nearest enclosing
// AutoScrollable, or nil.
func ascendAutoScrollable(w Widget) AutoScrollable {
	for cur := w; cur != nil; cur = cur.Parent() {
		if s, ok := cur.(AutoScrollable); ok {
			return s
		}
	}
	return nil
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

// copyTextSelection aggregates the selected text of every selectable in
// document order and writes it to the clipboard (plain text plus an HTML
// flavor). It returns true (and handles the event) only when more than
// one widget contributes — a single-widget selection is left to that
// widget's own Cmd+C so existing behavior is unchanged.
// ClipboardListItem is implemented by an element/widget that represents a
// list item, so the window can rebuild a real <ol>/<ul> when a copied
// selection spans list items — instead of flattening every row to a <div>.
type ClipboardListItem interface {
	// ClipboardListItem reports the enclosing list tag ("ol" or "ul") and
	// the item's plain-text marker (e.g. "1." or "•"). An empty listTag
	// means the element is not a list item.
	ClipboardListItem() (listTag, marker string)
}

// ClipboardTableCell is implemented by a widget representing a table cell,
// so the window can rebuild a real <table> when a copied selection spans
// table cells — pasting as a genuine table into Google Docs / Word / Sheets
// instead of a flat run of text.
type ClipboardTableCell interface {
	// ClipboardTableCell reports the cell's table membership. table is an
	// opaque identity shared by every cell of the same table (nil = not a
	// table cell); row/col are 0-based grid coordinates; colSpan/rowSpan are
	// >= 1; header is true for a <th>.
	ClipboardTableCell() (table interface{}, row, col, colSpan, rowSpan int, header bool)
}

// ascendClipboardTableCell walks up from w to the nearest ancestor that
// reports itself as a table cell.
func ascendClipboardTableCell(w Widget) ClipboardTableCell {
	for cur := w; cur != nil; cur = cur.Parent() {
		if tc, ok := cur.(ClipboardTableCell); ok {
			if t, _, _, _, _, _ := tc.ClipboardTableCell(); t != nil {
				return tc
			}
		}
	}
	return nil
}

// ClipboardBlock is implemented by an element/widget that knows the
// semantic HTML block tag its text lives in (h1..h6, p, blockquote,
// pre, …) so the window's copy path can rebuild that tag instead of
// flattening every block to a generic <div> — pasting a copied heading
// into Word / Google Docs stays a heading.
type ClipboardBlock interface {
	// ClipboardBlockTag reports the block tag, or "" when the element is
	// not a semantic block (generic <div> handling applies).
	ClipboardBlockTag() string
}

// ascendClipboardBlock walks up from w to the nearest ancestor that
// reports a semantic block tag.
func ascendClipboardBlock(w Widget) ClipboardBlock {
	for cur := w; cur != nil; cur = cur.Parent() {
		if cb, ok := cur.(ClipboardBlock); ok {
			if tag := cb.ClipboardBlockTag(); tag != "" {
				return cb
			}
		}
	}
	return nil
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

// ascendClipboardListItem walks up from w to the nearest ancestor that
// reports itself as a list item (non-empty list tag).
func ascendClipboardListItem(w Widget) ClipboardListItem {
	for cur := w; cur != nil; cur = cur.Parent() {
		if li, ok := cur.(ClipboardListItem); ok {
			if tag, _ := li.ClipboardListItem(); tag != "" {
				return li
			}
		}
	}
	return nil
}

// clipPart is one selectable's contribution to a copy, tagged with its
// list-item / table-cell context so the aggregator can rebuild <ol>/<ul>
// and <table> structure.
type clipPart struct {
	plain, html      string
	li               ClipboardListItem // nil unless inside a list item
	listTag, marker  string
	tc               ClipboardTableCell // nil unless inside a table cell
	tbl              interface{}        // shared table identity
	row, col         int
	colSpan, rowSpan int
	header           bool
	blk              ClipboardBlock // nil unless inside a semantic block
	blkTag           string
}

// clipCell is a merged table cell (all parts sharing one cell), ready to
// emit as a <td>/<th>.
type clipCell struct {
	row, col         int
	colSpan, rowSpan int
	header           bool
	html, plain      string
}

func (w *Window) copyTextSelection() bool {
	flat := w.collectTextSelectables()
	var parts []clipPart
	for _, ts := range flat {
		s := ts.SelectedText()
		if s == "" {
			continue
		}
		p := clipPart{plain: s}
		if rts, ok := ts.(RichTextSelectable); ok {
			p.html = spansToHTML(rts.SelectedSpans())
		} else {
			p.html = htmlEscape(s)
		}
		if li := ascendClipboardListItem(ts); li != nil {
			p.li = li
			p.listTag, p.marker = li.ClipboardListItem()
		}
		if tc := ascendClipboardTableCell(ts); tc != nil {
			p.tc = tc
			p.tbl, p.row, p.col, p.colSpan, p.rowSpan, p.header = tc.ClipboardTableCell()
		}
		if blk := ascendClipboardBlock(ts); blk != nil {
			p.blk = blk
			p.blkTag = blk.ClipboardBlockTag()
		}
		parts = append(parts, p)
	}
	// Copy whenever any read-only selectable holds a selection — including
	// a selection confined to a single widget (one Label, or one folded
	// InlineBox paragraph). Editable widgets (Input / TextArea) aren't
	// TextSelectable, so they never appear here and keep their own
	// field-scoped Cmd/Ctrl+C.
	if len(parts) == 0 {
		return false
	}

	// Plain text: one line per block. List items keep their marker so a
	// plain-text paste still reads as a numbered / bulleted list. Parts
	// that share the same list-item element merge into one line.
	var plainLines []string
	for i := 0; i < len(parts); {
		// Table → tab-separated rows (a spreadsheet-friendly plain flavor),
		// with empty cells preserved so columns stay aligned.
		if parts[i].tc != nil {
			j := tableRunEnd(parts, i)
			if tableRunSingleCell(parts[i:j]) {
				plain, _ := mergeCellParts(parts[i:j])
				plainLines = append(plainLines, plain)
			} else {
				plainLines = append(plainLines, tablePlainRows(parts[i:j])...)
			}
			i = j
			continue
		}
		if li := parts[i].li; li != nil {
			var sb strings.Builder
			marker := parts[i].marker
			for i < len(parts) && parts[i].li == li {
				sb.WriteString(parts[i].plain)
				i++
			}
			line := sb.String()
			if marker != "" {
				line = marker + " " + line
			}
			plainLines = append(plainLines, line)
			continue
		}
		// Parts sharing one semantic block (several inline runs of the
		// same paragraph) merge into a single line.
		if blk := parts[i].blk; blk != nil {
			var sb strings.Builder
			for i < len(parts) && parts[i].blk == blk && parts[i].li == nil && parts[i].tc == nil {
				sb.WriteString(parts[i].plain)
				i++
			}
			plainLines = append(plainLines, sb.String())
			continue
		}
		plainLines = append(plainLines, parts[i].plain)
		i++
	}

	// HTML: rebuild <ol>/<ul> from runs of consecutive list items (markers
	// are regenerated by the list element, so they're not emitted as text);
	// every other block stays its own <div> so paragraph breaks survive.
	var b strings.Builder
	b.WriteString(`<meta charset="utf-8">`)
	for i := 0; i < len(parts); {
		// Table → rebuild <table><tr><td/th>, filling gaps so empty cells /
		// rows keep the columns aligned when pasted into Docs / Sheets.
		if parts[i].tc != nil {
			j := tableRunEnd(parts, i)
			if tableRunSingleCell(parts[i:j]) {
				_, html := mergeCellParts(parts[i:j])
				b.WriteString("<div>")
				if html == "" {
					b.WriteString("<br>")
				} else {
					b.WriteString(html)
				}
				b.WriteString("</div>")
			} else {
				writeTableHTML(&b, parts[i:j])
			}
			i = j
			continue
		}
		lt := parts[i].listTag
		if lt == "" {
			// A semantic block (h1..h6 / p / blockquote / pre / …) keeps its
			// tag so the paste stays a heading / paragraph; parts sharing
			// the same block element merge into one. Everything else stays
			// a generic <div>.
			tag := "div"
			var html string
			if blk := parts[i].blk; blk != nil {
				tag = parts[i].blkTag
				var htmls []string
				for i < len(parts) && parts[i].blk == blk &&
					parts[i].listTag == "" && parts[i].tc == nil {
					htmls = append(htmls, parts[i].html)
					i++
				}
				html = strings.Join(htmls, "")
			} else {
				html = parts[i].html
				i++
			}
			b.WriteString("<" + tag + ">")
			if html == "" {
				b.WriteString("<br>")
			} else {
				b.WriteString(html)
			}
			b.WriteString("</" + tag + ">")
			continue
		}
		b.WriteString("<" + lt + ">")
		for i < len(parts) && parts[i].listTag == lt {
			li := parts[i].li
			b.WriteString("<li>")
			for i < len(parts) && parts[i].li == li {
				b.WriteString(parts[i].html)
				i++
			}
			b.WriteString("</li>")
		}
		b.WriteString("</" + lt + ">")
	}

	SetClipboardRich(strings.Join(plainLines, "\n"), b.String())
	return true
}

// tableRunEnd returns the end index of the run of parts belonging to the
// same table as parts[i] (parts are in document / grid order).
func tableRunEnd(parts []clipPart, i int) int {
	tbl := parts[i].tbl
	j := i
	for j < len(parts) && parts[j].tbl == tbl {
		j++
	}
	return j
}

// groupTableCells merges each cell's parts and buckets the cells by row.
// Returns rows indexed 0..maxRow (sparse rows are empty slices) and the
// column count (max col+colSpan). Multiple parts in one cell (a cell holding
// several blocks / list items) join with a separator — nested block/list
// structure inside a cell flattens to line breaks.
func groupTableCells(tparts []clipPart) (rows [][]clipCell, maxCol int) {
	var cells []clipCell
	for k := 0; k < len(tparts); {
		cell := tparts[k].tc
		c := clipCell{
			row: tparts[k].row, col: tparts[k].col,
			colSpan: tparts[k].colSpan, rowSpan: tparts[k].rowSpan,
			header: tparts[k].header,
		}
		if c.colSpan < 1 {
			c.colSpan = 1
		}
		if c.rowSpan < 1 {
			c.rowSpan = 1
		}
		var htmls, plains []string
		for k < len(tparts) && tparts[k].tc == cell {
			htmls = append(htmls, tparts[k].html)
			plains = append(plains, tparts[k].plain)
			k++
		}
		c.html = strings.Join(htmls, "<br>")
		c.plain = strings.Join(plains, " ")
		cells = append(cells, c)
	}
	// Normalize to the selection's bounding box: subtract the minimum selected
	// row/col so a selection starting mid-table doesn't emit leading empty
	// rows/columns (e.g. copying a single "$99" cell becomes a 1×1 grid, not a
	// full table padded with blanks up to that cell's absolute position).
	if len(cells) > 0 {
		minRow, minCol := cells[0].row, cells[0].col
		for _, c := range cells {
			if c.row < minRow {
				minRow = c.row
			}
			if c.col < minCol {
				minCol = c.col
			}
		}
		for i := range cells {
			cells[i].row -= minRow
			cells[i].col -= minCol
		}
	}
	maxRow := 0
	for _, c := range cells {
		if c.row > maxRow {
			maxRow = c.row
		}
		if c.col+c.colSpan > maxCol {
			maxCol = c.col + c.colSpan
		}
	}
	rows = make([][]clipCell, maxRow+1)
	for _, c := range cells {
		rows[c.row] = append(rows[c.row], c)
	}
	return rows, maxCol
}

// tableRunSingleCell reports whether every part in a table run belongs to the
// same single cell — a selection confined to one <td>/<th>, which should copy
// as plain content rather than a one-cell <table> skeleton.
func tableRunSingleCell(parts []clipPart) bool {
	first := parts[0].tc
	for _, p := range parts {
		if p.tc != first {
			return false
		}
	}
	return true
}

// mergeCellParts joins a single cell's parts into plain / html strings, the
// same way groupTableCells merges multi-block cells (line breaks between blocks).
func mergeCellParts(parts []clipPart) (plain, html string) {
	plains := make([]string, 0, len(parts))
	htmls := make([]string, 0, len(parts))
	for _, p := range parts {
		plains = append(plains, p.plain)
		htmls = append(htmls, p.html)
	}
	return strings.Join(plains, " "), strings.Join(htmls, "<br>")
}

// writeTableHTML emits a <table> for one table's parts, inserting empty
// <td> for missing columns (and empty <tr> content for missing rows) so the
// pasted table keeps its column alignment.
func writeTableHTML(b *strings.Builder, tparts []clipPart) {
	rows, maxCol := groupTableCells(tparts)
	b.WriteString("<table>")
	for _, cells := range rows {
		b.WriteString("<tr>")
		expected := 0
		for _, c := range cells {
			for expected < c.col {
				b.WriteString("<td></td>")
				expected++
			}
			tag := "td"
			if c.header {
				tag = "th"
			}
			b.WriteString("<" + tag)
			if c.colSpan > 1 {
				b.WriteString(` colspan="` + strconv.Itoa(c.colSpan) + `"`)
			}
			if c.rowSpan > 1 {
				b.WriteString(` rowspan="` + strconv.Itoa(c.rowSpan) + `"`)
			}
			b.WriteString(">")
			b.WriteString(c.html)
			b.WriteString("</" + tag + ">")
			expected += c.colSpan
		}
		for expected < maxCol {
			b.WriteString("<td></td>")
			expected++
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// tablePlainRows renders one table's parts as tab-separated rows, empty
// cells preserved (placed by column index) so the TSV stays aligned.
func tablePlainRows(tparts []clipPart) []string {
	rows, maxCol := groupTableCells(tparts)
	if maxCol < 1 {
		maxCol = 1
	}
	var lines []string
	for _, cells := range rows {
		cols := make([]string, maxCol)
		for _, c := range cells {
			if c.col >= 0 && c.col < maxCol {
				cols[c.col] = c.plain
			}
		}
		lines = append(lines, strings.Join(cols, "\t"))
	}
	return lines
}

// spansToHTML renders styled spans into inline HTML.
func spansToHTML(spans []TextSpan) string {
	var b strings.Builder
	for _, sp := range spans {
		if sp.Image != "" {
			b.WriteString(`<img src="`)
			b.WriteString(htmlEscapeAttr(sp.Image))
			b.WriteString(`">`)
			continue
		}
		style := spanCSS(sp.Font, sp.Color)
		inner := htmlEscape(sp.Text)
		if style != "" {
			inner = `<span style="` + style + `">` + inner + `</span>`
		}
		if sp.Href != "" {
			b.WriteString(`<a href="`)
			b.WriteString(htmlEscapeAttr(sp.Href))
			b.WriteString(`">`)
			b.WriteString(inner)
			b.WriteString(`</a>`)
			continue
		}
		b.WriteString(inner)
	}
	return b.String()
}

// spanCSS derives an inline CSS declaration list from a span's font and
// color. Empty when neither carries information.
func spanCSS(font *Font, color *Color) string {
	var decls []string
	if font != nil {
		if font.effectiveWeight() >= FontWeightSemiBold {
			decls = append(decls, "font-weight:700")
		}
		if font.Italic {
			decls = append(decls, "font-style:italic")
		}
		if font.Family != "" {
			decls = append(decls, "font-family:'"+font.Family+"'")
		}
		if font.Size > 0 {
			decls = append(decls, "font-size:"+trimFloat(font.Size)+"px")
		}
	}
	if color != nil {
		decls = append(decls, "color:"+cssColor(*color))
	}
	return strings.Join(decls, ";")
}

func cssColor(c Color) string {
	to := func(v float32) int {
		n := int(v*255 + 0.5)
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return n
	}
	const hex = "0123456789abcdef"
	r, g, bl := to(c.R), to(c.G), to(c.B)
	return string([]byte{'#',
		hex[r>>4], hex[r&0xf],
		hex[g>>4], hex[g&0xf],
		hex[bl>>4], hex[bl&0xf],
	})
}

func trimFloat(f float32) string {
	s := strconv.FormatFloat(float64(f), 'f', 2, 32)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// htmlEscapeAttr escapes a string for use inside a double-quoted HTML
// attribute (no newline→<br> translation).
func htmlEscapeAttr(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// htmlEscape escapes HTML-significant runes and turns newlines into <br>
// so a multi-line plain selection keeps its breaks.
func htmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\n':
			b.WriteString("<br>")
		case '\r':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveSelectionFocus maps a window point to (flat index, rune offset)
// of the selectable under the cursor. When the cursor is over a gap or a
// non-text widget it snaps to the nearest selectable by vertical
// distance, clamping the offset to that widget's start or end so dragging
// through inter-block gaps still extends the selection naturally.
func (w *Window) resolveSelectionFocus(p Point) (int, int) {
	d := &w.textSel
	if ts := ascendTextSelectable(w.hitTestAll(p)); ts != nil {
		if i := indexOfSelectable(d.flat, ts); i >= 0 {
			return i, ts.SelectionOffsetAt(WindowPointToLocal(ts, p))
		}
	}
	return resolveSelectionFocusIn(d.flat, p)
}

// resolveSelectionFocusIn snaps a point with no selectable under it to
// the vertically nearest selectable in flat, clamping the offset to that
// widget's start or end. Shared by drag extension (sweeping through
// gaps) and blank-press anchoring (starting a drag in a margin).
func resolveSelectionFocusIn(flat []TextSelectable, p Point) (int, int) {
	bestIdx, bestDist := -1, float32(1e18)
	var best TextSelectable
	for i, ts := range flat {
		// On-screen bounds: p is a window point and the candidates may live
		// in different coordinate spaces (inside a scroll container, under a
		// CSS transform), so comparing raw Bounds() would mix spaces.
		b := InteractionBoundsOf(ts)
		if b.W <= 0 || b.H <= 0 {
			continue
		}
		var dist float32
		switch {
		case p.Y < b.Y:
			dist = b.Y - p.Y
		case p.Y > b.Y+b.H:
			dist = p.Y - (b.Y + b.H)
		default:
			dist = 0
		}
		if dist < bestDist {
			bestDist, bestIdx, best = dist, i, ts
		}
	}
	if bestIdx < 0 {
		return -1, 0
	}
	b := InteractionBoundsOf(best)
	if p.Y < b.Y {
		return bestIdx, 0
	}
	if p.Y > b.Y+b.H {
		return bestIdx, best.SelectableLength()
	}
	return bestIdx, best.SelectionOffsetAt(WindowPointToLocal(best, p))
}

// disarmTextSelection cancels a provisional (blank-press) drag after a
// widget consumed the arming MouseDown — its own gesture (scrollbar
// thumb, slider knob) owns the mouse, not the selection controller.
func (w *Window) disarmTextSelection() {
	if w.textSel.autoTick != nil {
		w.UnregisterAnimator(w.textSel.autoTick)
	}
	w.textSel = textSelectionDrag{}
}

// clearAllTextSelection drops the highlight on every selectable in the
// tree (used when a fresh click starts elsewhere).
func (w *Window) clearAllTextSelection() {
	for _, ts := range w.collectTextSelectables() {
		ts.ClearTextSelection()
	}
}

// collectTextSelectables walks the main root then overlays in pre-order,
// returning every visible TextSelectable in document (reading) order.
func (w *Window) collectTextSelectables() []TextSelectable {
	var out []TextSelectable
	walkTextSelectables(w.root, &out)
	for _, ov := range w.overlays {
		walkTextSelectables(ov, &out)
	}
	return out
}

func walkTextSelectables(node Widget, out *[]TextSelectable) {
	if node == nil {
		return
	}
	if ts, ok := node.(TextSelectable); ok {
		// A selectable can opt out while inert (e.g. a list-marker Label,
		// which is not user-selectable in a browser).
		eligible := true
		if tg, ok := node.(textSelectionToggle); ok {
			eligible = tg.TextSelectionEnabled()
		}
		if eligible {
			if b := ts.Bounds(); b.W > 0 && b.H > 0 {
				*out = append(*out, ts)
			}
		}
		// A node that represents its own descendants' selectable text (an
		// InlineBox folding atomic inline children) is a leaf for selection:
		// descending would double-count the atoms it already covers.
		if leaf, ok := node.(textSelectionLeaf); ok && leaf.OwnsDescendantSelection() {
			return
		}
	}
	if cl, ok := node.(childLister); ok {
		for _, c := range cl.ChildList() {
			walkTextSelectables(c, out)
		}
	}
}

// ascendTextSelectable walks up from w looking for the nearest enclosing
// TextSelectable (including w itself), or nil.
func ascendTextSelectable(w Widget) TextSelectable {
	for cur := w; cur != nil; cur = cur.Parent() {
		if ts, ok := cur.(TextSelectable); ok {
			return ts
		}
	}
	return nil
}

func indexOfSelectable(flat []TextSelectable, target TextSelectable) int {
	for i, ts := range flat {
		if ts == target {
			return i
		}
	}
	return -1
}
