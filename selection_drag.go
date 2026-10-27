package qui

import (
	"time"
)

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
