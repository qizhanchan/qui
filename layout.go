package qui

import "sort"

// Layout positions child widgets inside a container.
type Layout interface {
	Apply(children []Widget, bounds Rect)
}

// LayoutMeasurer is the optional half of a Layout engine: it reports the
// intrinsic content size (excluding the container's own padding) that
// children laid out by the engine need within avail. A non-positive avail
// axis means "unconstrained". Container.Measure uses it, so a custom engine
// that implements it sizes correctly inside a ScrollView, Popup or any
// content-sized parent; one that doesn't measures as "all available space".
// FlexLayout, GridLayout and FlowLayout implement it (by value, so pointers
// to them do too).
type LayoutMeasurer interface {
	Measure(children []Widget, avail Size) Size
}

// Direction indicates layout flow.
type Direction int

const (
	Horizontal Direction = iota
	Vertical
)

// Justify controls how the main axis distributes free space among
// flex items (CSS justify-content semantics).
type Justify int

const (
	JustifyStart        Justify = iota // default — items at start, Gap between
	JustifyCenter                      // whole group centered
	JustifyEnd                         // pushed to end
	JustifySpaceBetween                // first at start, last at end, equal gaps between (overrides Gap)
	JustifySpaceAround                 // half-gap at each end, equal gaps between (overrides Gap)
	JustifySpaceEvenly                 // equal gaps everywhere including ends (overrides Gap)
)

// AlignContent controls how a wrapping FlexLayout distributes its LINES
// along the cross axis when the container has more cross space than the
// lines occupy (CSS align-content). Zero value packs lines at the start —
// today's behavior — so existing wrap layouts are unaffected. CSS's
// initial value for flex containers is stretch; callers wanting spec
// behavior opt in with AlignContentStretch (htmlcss does).
type AlignContent int

const (
	AlignContentStart        AlignContent = iota // lines packed at cross start (default)
	AlignContentCenter                           // line block centered
	AlignContentEnd                              // lines packed at cross end
	AlignContentSpaceBetween                     // first/last lines at edges, equal gaps between
	AlignContentSpaceAround                      // half-gap at each end (adds to CrossGap)
	AlignContentSpaceEvenly                      // equal gaps everywhere including ends
	AlignContentStretch                          // free space grows every line equally
)

// AlignCross controls how items align on the cross axis (CSS align-items
// for container / align-self for per-item override via FlexItem.Align).
type AlignCross int

const (
	AlignDefault AlignCross = iota // per-item only: "inherit container AlignItems"
	AlignStart
	AlignCenter
	AlignEnd
	AlignStretch // fill the cross axis (default for items without explicit Align)
)

// FlexItem carries per-child sizing + alignment hints that FlexLayout
// reads via the flexible interface (implemented by BaseWidget). Zero
// value = pure natural sizing (no grow, shrink-on-overflow=1, basis
// from Measure, cross axis inherits container AlignItems).
//
// CSS analogs:
//
//	Grow     ← flex-grow
//	Shrink   ← flex-shrink            (Shrink=0 + NoShrink=false → treated as 1)
//	Basis    ← flex-basis             (0 / unset + Grow>0 → 0; otherwise Measure result)
//	Align    ← align-self             (AlignDefault → align-items)
//	NoShrink ← flex-shrink: 0 (CSS)   (overrides Shrink default; cannot shrink)
//
// Two semantic guards that deliberately match CSS — they avoid the
// most common "my child blew out the container" surprises:
//
//  1. When Grow > 0 and Basis is the zero default, basis is 0 (CSS
//     `flex: <grow>` = `<grow> 1 0`). Without this, widgets whose
//     Measure returns the full available size (ScrollView, TabView,
//     Editor) would claim ALL the main-axis space as basis and push
//     siblings off-screen.
//  2. Shrink defaults to 1, so overflow triggers proportional shrink
//     instead of silent clipping. Opt out with NoShrink: true when
//     a child must keep its natural size even at the cost of overflow
//     (e.g., toolbar buttons whose icons can't compress).
//
// Use SetFlex(grow) on BaseWidget for the common case "give me all
// remaining space" — it sets Grow + zeroes Basis in one call.
type FlexItem struct {
	Grow     float32
	Shrink   float32
	Basis    float32
	Align    AlignCross
	NoShrink bool
	// Order is the CSS `order` value: FlexLayout places items in ascending
	// Order (stable within equal values), independent of DOM/child order.
	// Zero is the default (source order preserved).
	Order int
}

// effectiveShrink resolves the FlexItem.Shrink default. Explicit
// NoShrink wins (0). Otherwise unset (0) means CSS-default 1.
func (f FlexItem) effectiveShrink() float32 {
	if f.NoShrink {
		return 0
	}
	if f.Shrink == 0 {
		return 1
	}
	return f.Shrink
}

// flexItem is one child's runtime state during FlexLayout.Apply.
// Package-private; exposed only to layout_debug.go's overflow logger.
//
// All sizes (basis / size / natural / min / max) are OUTER sizes —
// they include the child's Style().Margin. The margin is subtracted
// back out when the final Layout rect is computed, so grow/shrink/
// justify math treats margin as part of the space a child occupies
// (CSS margin-box distribution).
type flexItem struct {
	w        Widget
	flex     FlexItem
	basis    float32 // target size on main axis (pre grow/shrink)
	size     float32 // final main-axis size
	natural  Size    // post-constraints Measure() result (for cross axis)
	minMain  float32
	minCross float32
	maxMain  float32 // 0 = unconstrained
	maxCross float32 // 0 = unconstrained

	// explicitCross is the child's DECLARED cross-axis size (Style.Width in
	// a column container, Style.Height in a row one, percentages resolved
	// against the container) — 0 when the cross size is `auto`. CSS stretches
	// only auto cross sizes, so a non-zero value wins over align-items:
	// stretch (see resolveCross).
	explicitCross float32

	// Margin split into main/cross-axis components (already UIScale'd).
	// *Start is the leading edge (left/top); *Sum is both edges.
	marginMainStart  float32
	marginMainSum    float32
	marginCrossStart float32
	marginCrossSum   float32

	// Auto-margin flags per edge (CSS `margin-*: auto`), mapped to the
	// main/cross axes. Auto margins reserve no fixed space; they absorb
	// the line's positive free space at placement time (Apply).
	autoMainStart  bool
	autoMainEnd    bool
	autoCrossStart bool
	autoCrossEnd   bool
}

// innerRect converts an outer (margin-box) placement back to the rect
// handed to child.Layout: offset by the leading margins, shrunk by the
// margin sums, clamped at zero.
func (it flexItem) innerRect(outer Rect, dir Direction) Rect {
	var r Rect
	if dir == Horizontal {
		r = Rect{
			X: outer.X + it.marginMainStart,
			Y: outer.Y + it.marginCrossStart,
			W: outer.W - it.marginMainSum,
			H: outer.H - it.marginCrossSum,
		}
	} else {
		r = Rect{
			X: outer.X + it.marginCrossStart,
			Y: outer.Y + it.marginMainStart,
			W: outer.W - it.marginCrossSum,
			H: outer.H - it.marginMainSum,
		}
	}
	if r.W < 0 {
		r.W = 0
	}
	if r.H < 0 {
		r.H = 0
	}
	return r
}

// FlexLayout arranges children in a row or column with CSS-flexbox
// style grow/shrink/basis distribution, cross-axis alignment, and
// main-axis justification.
//
// Default behavior (all zero fields):
//   - Items laid out at their natural size (child.Measure result)
//   - Stacked from start of main axis with Gap between
//   - Cross axis stretches to fill container (AlignItems = AlignStretch
//     effective default when unset, see alignItemsOrDefault)
//
// To make a child take remaining space, set its FlexItem.Grow > 0:
//
//	middle := qui.NewLabel("body")
//	middle.SetFlex(1)
//	qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical},
//	    header, middle, footer)
type FlexLayout struct {
	Direction  Direction
	Gap        float32
	Justify    Justify
	AlignItems AlignCross
	// Wrap, when true, breaks the children into multiple lines when the
	// main-axis basis sum exceeds the container's available main extent
	// (CSS `flex-wrap: wrap`). Each line packs as many children as fit
	// at their measured basis — grow / shrink are intentionally NOT
	// applied within a line, because the whole point of wrapping is to
	// avoid resizing the children. Cross axis grows by the sum of line
	// heights so a wrapping toolbar gets a second row instead of an
	// ellipsed one. CrossGap controls vertical spacing between lines.
	Wrap     bool
	CrossGap float32
	// AlignContent distributes wrap lines in leftover cross space (CSS
	// align-content). Only meaningful when Wrap is true and the container
	// is larger on the cross axis than the lines it packed.
	AlignContent AlignContent
}

type minSized interface {
	MinSize() Size
}

type maxSized interface {
	MaxSize() Size
}

type preferredSized interface {
	PreferredSize() Size
}

// Style-vs-interface precedence for size constraints:
//   The interface method (BaseWidget.MinSize/MaxSize/PreferredSize,
//   populated by SetMinSize/SetMaxSize/SetPreferredSize) is primary.
//   Style.MinWidth/MinHeight/MaxWidth/MaxHeight/Width/Height fills in
//   PER AXIS when the interface returned 0 on that axis, so a caller
//   can declare "min height in Style" without disturbing an
//   interface-supplied MinWidth.

func widgetMinSize(w Widget) Size {
	var min Size
	if ms, ok := w.(minSized); ok {
		min = ms.MinSize()
	}
	if min.W < 0 {
		min.W = 0
	}
	if min.H < 0 {
		min.H = 0
	}
	if s := w.Style(); s != nil {
		if min.W == 0 && s.MinWidth > 0 {
			min.W = s.MinWidth
		}
		if min.H == 0 && s.MinHeight > 0 {
			min.H = s.MinHeight
		}
	}
	return min
}

// widgetMaxSize returns the widget's max-size cap. Zero on an axis
// means "unconstrained" — callers should skip the cap on that axis.
func widgetMaxSize(w Widget) Size {
	var max Size
	if ms, ok := w.(maxSized); ok {
		max = ms.MaxSize()
	}
	if max.W < 0 {
		max.W = 0
	}
	if max.H < 0 {
		max.H = 0
	}
	if s := w.Style(); s != nil {
		if max.W == 0 && s.MaxWidth > 0 {
			max.W = s.MaxWidth
		}
		if max.H == 0 && s.MaxHeight > 0 {
			max.H = s.MaxHeight
		}
	}
	return max
}

// widgetPreferredSize returns the widget's preferred-size override.
// Zero on an axis means "no override" — callers should fall back to
// Measure() on that axis. Style.Width/Height serve as the declarative
// equivalent to SetPreferredSize.
func widgetPreferredSize(w Widget) Size {
	var p Size
	if ps, ok := w.(preferredSized); ok {
		p = ps.PreferredSize()
	}
	if p.W < 0 {
		p.W = 0
	}
	if p.H < 0 {
		p.H = 0
	}
	if s := w.Style(); s != nil {
		if p.W == 0 && s.Width > 0 {
			p.W = s.Width
		}
		if p.H == 0 && s.Height > 0 {
			p.H = s.Height
		}
	}
	return p
}

// applyMinSize floors a size by the per-axis minimums.
func applyMinSize(size, min Size) Size {
	if size.W < min.W {
		size.W = min.W
	}
	if size.H < min.H {
		size.H = min.H
	}
	return size
}

// applyMaxSize caps a size by the per-axis maximums. A zero on an axis
// means "unconstrained" — only positive maxes clamp.
func applyMaxSize(size, max Size) Size {
	if max.W > 0 && size.W > max.W {
		size.W = max.W
	}
	if max.H > 0 && size.H > max.H {
		size.H = max.H
	}
	return size
}

// applyConstraints floors by min then caps by max in one call. Used
// every place a layout engine takes a raw Measure result and turns it
// into the size passed to child.Layout.
func applyConstraints(size, min, max Size) Size {
	return applyMaxSize(applyMinSize(size, min), max)
}

// resolvedMinSize / resolvedMaxSize are the containing-block-aware forms
// of widgetMinSize / widgetMaxSize: they additionally resolve the CSS
// percentage min/max fields (Style.Min*Pct / Max*Pct, e.g. `max-width:
// 100%`) against cb, the containing block's content box on each axis.
// The absolute Min*/Max* fields still win when set; percentages only
// fill an axis the absolute size left at zero. Only positive cb axes
// resolve (a zero-height containing block leaves height-% unresolved).
func resolvedMinSize(w Widget, cb Size) Size {
	min := widgetMinSize(w)
	if s := w.Style(); s != nil {
		if min.W == 0 && s.MinWidthPct > 0 && cb.W > 0 {
			min.W = cb.W * s.MinWidthPct
		}
		if min.H == 0 && s.MinHeightPct > 0 && cb.H > 0 {
			min.H = cb.H * s.MinHeightPct
		}
	}
	return min
}

func resolvedMaxSize(w Widget, cb Size) Size {
	max := widgetMaxSize(w)
	if s := w.Style(); s != nil {
		if max.W == 0 && s.MaxWidthPct > 0 && cb.W > 0 {
			max.W = cb.W * s.MaxWidthPct
		}
		if max.H == 0 && s.MaxHeightPct > 0 && cb.H > 0 {
			max.H = cb.H * s.MaxHeightPct
		}
	}
	return max
}

// measureWithConstraints is the canonical pipeline for "what size
// does this child want here?". It calls Measure with the available
// budget, overlays any PreferredSize axes, then applies min/max. Use
// this anywhere a layout pass currently calls Measure + applyMinSize.
func measureWithConstraints(w Widget, avail Size) Size {
	m := MeasureChild(w, avail)
	if p := widgetPreferredSize(w); p.W > 0 || p.H > 0 {
		if p.W > 0 {
			m.W = p.W
		}
		if p.H > 0 {
			m.H = p.H
		}
	}
	return applyConstraints(m, widgetMinSize(w), widgetMaxSize(w))
}

// MeasureConstrained answers "what size does this widget want inside a box
// of avail?" the way the layout engines do: Measure, then any explicit
// PreferredSize / Style Width-Height (percentages resolved against avail),
// then min/max. Code that positions a widget ITSELF instead of going
// through a layout engine — overlay hosts (portals, popovers) above all —
// must measure through this, or an explicitly sized child (a CSS-sized
// dialog, say) silently keeps whatever its own Measure returned.
func MeasureConstrained(w Widget, avail Size) Size {
	m := MeasureChild(w, avail)
	p := widgetPreferredSize(w)
	if s := w.Style(); s != nil {
		if p.W == 0 && s.WidthPct > 0 && avail.W > 0 {
			p.W = avail.W * s.WidthPct
		}
		if p.H == 0 && s.HeightPct > 0 && avail.H > 0 {
			p.H = avail.H * s.HeightPct
		}
	}
	if p.W > 0 {
		m.W = p.W
	}
	if p.H > 0 {
		m.H = p.H
	}
	return applyConstraints(m, resolvedMinSize(w, avail), resolvedMaxSize(w, avail))
}

// widgetFlexItem resolves a child's flex hints from BOTH the controlled
// FlexItemValue path (set via SetFlex/SetFlexItem etc.) and the
// declarative Style fields (Grow/Shrink/Basis/AlignSelf). Interface
// wins field-by-field; Style fills in unset fields — matching the
// widgetMinSize / widgetMaxSize precedence.
//
// NoShrink is interface-only (Style has no "explicit non-shrink"
// distinguishable-from-unset marker — 0 already collapses with
// unspecified). Callers that need NoShrink go through SetFlex + the
// FlexItem struct.
func widgetFlexItem(w Widget) FlexItem {
	var fx FlexItem
	fx = FlexItemOf(w)
	if s := w.Style(); s != nil {
		if fx.Grow == 0 && s.Grow != 0 {
			fx.Grow = s.Grow
		}
		if fx.Shrink == 0 && s.Shrink != 0 {
			fx.Shrink = s.Shrink
		}
		if fx.Basis == 0 && s.Basis != 0 {
			fx.Basis = s.Basis
		}
		if fx.Align == AlignDefault && s.AlignSelf != AlignDefault {
			fx.Align = s.AlignSelf
		}
	}
	return fx
}

// measureFlexItems runs the per-child Measure pass, resolving basis
// (CSS `flex: <grow>` shorthand → basis=0) and min/max sizes. Extracted
// so both the single-line Apply path and the wrap path see identical
// measurement.
func (l FlexLayout) measureFlexItems(children []Widget, bounds Rect) []flexItem {
	items := make([]flexItem, len(children))
	for i, ch := range children {
		fx := widgetFlexItem(ch)
		cb := Size{W: bounds.W, H: bounds.H}
		min := resolvedMinSize(ch, cb)
		max := resolvedMaxSize(ch, cb)
		nat := measureWithConstraints(ch, cb)
		basis := fx.Basis
		minMain := min.W
		minCross := min.H
		maxMain := max.W
		maxCross := max.H
		if l.Direction == Vertical {
			minMain, minCross = minCross, minMain
			maxMain, maxCross = max.H, max.W
		}
		// Percentage preferred sizes resolve against the flex container's
		// content box (CSS width/height % on flex items). Main-axis pct
		// becomes the basis (like an explicit width with flex-basis:auto);
		// cross-axis pct replaces the natural size resolveCross aligns with.
		if s := ch.Style(); s != nil {
			if s.Width == 0 && s.WidthPct > 0 {
				nat.W = bounds.W * s.WidthPct
				if l.Direction == Horizontal && basis <= 0 {
					basis = nat.W
				}
			}
			if s.Height == 0 && s.HeightPct > 0 {
				nat.H = bounds.H * s.HeightPct
				if l.Direction == Vertical && basis <= 0 {
					basis = nat.H
				}
			}
		}
		// An explicit cross-axis size is NOT stretched away by align-items:
		// stretch — CSS stretches only an `auto` cross size. Read it from the
		// same place the layout engines read explicit sizes (PreferredSize,
		// then Style.Width/Height), with percentages resolved against the
		// container like the block above.
		var explicitCross float32
		if p := widgetPreferredSize(ch); l.Direction == Vertical {
			explicitCross = p.W
		} else {
			explicitCross = p.H
		}
		if explicitCross == 0 {
			if s := ch.Style(); s != nil {
				if l.Direction == Vertical && s.WidthPct > 0 {
					explicitCross = bounds.W * s.WidthPct
				} else if l.Direction == Horizontal && s.HeightPct > 0 {
					explicitCross = bounds.H * s.HeightPct
				}
			}
		}
		if basis <= 0 {
			if fx.Grow > 0 {
				basis = 0
			} else if l.Direction == Horizontal {
				basis = nat.W
			} else {
				basis = nat.H
			}
		}
		if basis < minMain {
			basis = minMain
		}
		if maxMain > 0 && basis > maxMain {
			basis = maxMain
		}

		// Margin (CSS margin-box): inflate every outer size by the
		// child's Style().Margin so distribution reserves the space;
		// innerRect subtracts it back out at placement time.
		margin := ch.Style().Margin
		it := flexItem{w: ch, flex: fx, basis: basis, natural: nat, minMain: minMain, minCross: minCross, maxMain: maxMain, maxCross: maxCross, explicitCross: explicitCross}
		if l.Direction == Horizontal {
			it.marginMainStart, it.marginMainSum = margin.Left, margin.Horizontal()
			it.marginCrossStart, it.marginCrossSum = margin.Top, margin.Vertical()
			if s := ch.Style(); s != nil {
				it.autoMainStart, it.autoMainEnd = s.MarginLeftAuto, s.MarginRightAuto
				it.autoCrossStart, it.autoCrossEnd = s.MarginTopAuto, s.MarginBottomAuto
			}
		} else {
			it.marginMainStart, it.marginMainSum = margin.Top, margin.Vertical()
			it.marginCrossStart, it.marginCrossSum = margin.Left, margin.Horizontal()
			if s := ch.Style(); s != nil {
				it.autoMainStart, it.autoMainEnd = s.MarginTopAuto, s.MarginBottomAuto
				it.autoCrossStart, it.autoCrossEnd = s.MarginLeftAuto, s.MarginRightAuto
			}
		}
		it.basis += it.marginMainSum
		it.minMain += it.marginMainSum
		it.minCross += it.marginCrossSum
		if it.explicitCross > 0 {
			it.explicitCross += it.marginCrossSum
		}
		if it.maxMain > 0 {
			it.maxMain += it.marginMainSum
		}
		if it.maxCross > 0 {
			it.maxCross += it.marginCrossSum
		}
		it.natural.W += margin.Horizontal()
		it.natural.H += margin.Vertical()
		items[i] = it
	}
	// CSS `order`: place items in ascending order value, stable within equal
	// values (so same-order items keep source order). Only reorders placement;
	// totals used by Measure are order-independent.
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].flex.Order < items[j].flex.Order
	})
	return items
}

// Apply runs the flex algorithm:
//  1. Measure each child → derive main-axis Basis
//  2. Compute free = available - ΣBasis - gaps
//  3. If free > 0 and ΣGrow > 0: distribute free ∝ Grow
//     If free < 0 and ΣShrink·Basis > 0: shrink ∝ Shrink·Basis (CSS rule)
//  4. Resolve cross-axis size per Align
//  5. Walk items, compute main-axis start + spacing per Justify, Layout each
//
// Wrap mode dispatches to applyWrap which packs children into multiple
// lines on overflow instead of shrinking.
func (l FlexLayout) Apply(children []Widget, bounds Rect) {
	n := len(children)
	if n == 0 {
		return
	}
	mainAvail := bounds.W
	crossAvail := bounds.H
	if l.Direction == Vertical {
		mainAvail, crossAvail = bounds.H, bounds.W
	}

	if l.Wrap {
		l.applyWrap(children, bounds, mainAvail, crossAvail)
		return
	}

	items := l.measureFlexItems(children, bounds)
	l.resolveMainSizes(items, mainAvail)
	// Re-measure each item's cross size at its resolved main size, so a
	// wrapping-text item (whose height depends on its final width) reports
	// its true cross extent instead of the 1-line wide-measure. Without
	// this, growing flex items with multi-line text overflow their box.
	l.remeasureCross(items, bounds)

	totalGap := l.Gap * float32(n-1)

	// After sizing, recompute actual leftover for justify.
	var sizedSum float32
	for _, it := range items {
		sizedSum += it.size
	}
	leftover := mainAvail - sizedSum - totalGap

	// Diagnostic: when QUI_DEBUG_LAYOUT=1, log overflows that the
	// caller probably didn't intend. With CSS-aligned defaults this
	// fires almost exclusively when a child explicitly opts out of
	// shrink (NoShrink:true) but the basisSum still exceeds available.
	if flexDebugLayoutEnabled && leftover < -0.5 {
		var basisSum float32
		for _, it := range items {
			basisSum += it.basis
		}
		logFlexOverflow(l, bounds, items, basisSum, totalGap, mainAvail)
	}

	// CSS auto margins on the main axis absorb positive free space BEFORE
	// justify-content, and suppress it entirely when they do. Count the
	// auto edges; if there is slack to give, each auto edge takes an equal
	// share and justify is ignored (mainStart/extraBetween stay 0).
	autoMainEdges := 0
	for _, it := range items {
		if it.autoMainStart {
			autoMainEdges++
		}
		if it.autoMainEnd {
			autoMainEdges++
		}
	}
	var mainStart, extraBetween, extraEnd, autoMainShare float32
	if autoMainEdges > 0 && leftover > 0 {
		autoMainShare = leftover / float32(autoMainEdges)
	} else {
		mainStart, extraBetween, extraEnd = distributeJustify(leftover, n, l.Justify)
	}

	// Walk items and Layout.
	mainOffset := mainStart
	for i, it := range items {
		if it.autoMainStart {
			mainOffset += autoMainShare
		}
		cross := resolveCross(it.flex.Align, l.AlignItems, crossAvail, it.natural, it.explicitCross, it.minCross, it.maxCross, l.Direction)
		crossOffset, crossSize := cross.offset, cross.size
		// Auto cross margins override align-items/self: the item keeps its
		// natural (clamped) cross size and centers/pushes within the free
		// cross space.
		if it.autoCrossStart || it.autoCrossEnd {
			crossOffset, crossSize = autoCrossPlace(it, crossAvail, l.Direction)
		}

		var r Rect
		if l.Direction == Horizontal {
			r = Rect{X: bounds.X + mainOffset, Y: bounds.Y + crossOffset, W: it.size, H: crossSize}
		} else {
			r = Rect{X: bounds.X + crossOffset, Y: bounds.Y + mainOffset, W: crossSize, H: it.size}
		}
		it.w.Layout(it.innerRect(r, l.Direction))

		mainOffset += it.size
		if it.autoMainEnd {
			mainOffset += autoMainShare
		}
		mainOffset += l.Gap + extraBetween
		_ = extraEnd
		_ = i
	}
}

// autoCrossPlace resolves the cross-axis offset+size for a flex item that
// has an auto cross margin. The item takes its natural (clamped) cross
// size — auto margins take precedence over stretch — and the leftover
// cross space is split by which edges are auto (both = center, start-only
// = push to far edge, end-only = leading).
func autoCrossPlace(it flexItem, crossAvail float32, dir Direction) (offset, size float32) {
	naturalCross := it.natural.H
	if dir == Vertical {
		naturalCross = it.natural.W
	}
	size = naturalCross
	if size < it.minCross {
		size = it.minCross
	}
	if it.maxCross > 0 && size > it.maxCross {
		size = it.maxCross
	}
	free := crossAvail - size
	if free < 0 {
		free = 0
	}
	switch {
	case it.autoCrossStart && it.autoCrossEnd:
		offset = free / 2
	case it.autoCrossStart:
		offset = free
	default: // end-only auto
		offset = 0
	}
	return offset, size
}

// resolveMainSizes runs the CSS flex grow/shrink resolution, setting
// each item's final main-axis size. mainAvail is the content main extent
// and l.Gap is already UIScale-adjusted by the caller. Extracted from
// Apply so Container.Measure can resolve the same sizes before it
// re-measures cross extents.
func (l FlexLayout) resolveMainSizes(items []flexItem, mainAvail float32) {
	n := len(items)
	if n == 0 {
		return
	}
	var basisSum float32
	for _, it := range items {
		basisSum += it.basis
	}
	totalGap := l.Gap * float32(n-1)
	free := mainAvail - basisSum - totalGap

	if free > 0 {
		var growSum float32
		for _, it := range items {
			growSum += it.flex.Grow
		}
		if growSum > 0 {
			for i := range items {
				items[i].size = items[i].basis + free*items[i].flex.Grow/growSum
			}
		} else {
			for i := range items {
				items[i].size = items[i].basis
			}
		}
	} else if free < 0 {
		// CSS-style weighted shrink with min-size freezing.
		needReduce := -free
		active := make([]bool, len(items))
		shrink := make([]float32, len(items))
		for i := range items {
			items[i].size = items[i].basis
			shrink[i] = items[i].flex.effectiveShrink()
			active[i] = shrink[i] > 0 && items[i].size > items[i].minMain
		}
		for needReduce > 0 {
			var weightSum float32
			for i := range items {
				if active[i] {
					weightSum += shrink[i] * items[i].basis
				}
			}
			if weightSum <= 0 {
				break
			}
			frozeAny := false
			for i := range items {
				if !active[i] {
					continue
				}
				capacity := items[i].size - items[i].minMain
				if capacity <= 0 {
					active[i] = false
					continue
				}
				reduce := needReduce * shrink[i] * items[i].basis / weightSum
				if reduce >= capacity {
					items[i].size = items[i].minMain
					needReduce -= capacity
					active[i] = false
					frozeAny = true
				}
			}
			if frozeAny {
				continue
			}
			for i := range items {
				if !active[i] {
					continue
				}
				reduce := needReduce * shrink[i] * items[i].basis / weightSum
				items[i].size -= reduce
			}
			needReduce = 0
		}
	} else {
		for i := range items {
			items[i].size = items[i].basis
		}
	}
	for i := range items {
		if items[i].size < items[i].minMain {
			items[i].size = items[i].minMain
		}
		if items[i].maxMain > 0 && items[i].size > items[i].maxMain {
			items[i].size = items[i].maxMain
		}
	}
}

// remeasureCross re-measures each item's cross-axis extent at its
// resolved main-axis (inner) size, updating it.natural on the cross axis.
// This is what makes wrapping-text flex items report their true height:
// their basis measure ran at the full container width (1 line), but their
// resolved width is narrower (N lines). Items whose cross size does not
// depend on main size (buttons, fixed boxes) re-measure to the same value.
func (l FlexLayout) remeasureCross(items []flexItem, bounds Rect) {
	for i := range items {
		it := &items[i]
		innerMain := it.size - it.marginMainSum
		if innerMain < 0 {
			innerMain = 0
		}
		var av Size
		if l.Direction == Horizontal {
			av = Size{W: innerMain, H: bounds.H}
		} else {
			av = Size{W: bounds.W, H: innerMain}
		}
		sz := measureWithConstraints(it.w, av)
		if l.Direction == Horizontal {
			it.natural.H = sz.H + it.marginCrossSum
		} else {
			it.natural.W = sz.W + it.marginCrossSum
		}
	}
}

// packLines splits items into wrap-mode lines. Each line takes as many
// children as fit at their basis (with Gap between siblings) without
// exceeding mainAvail. A single item wider than mainAvail still gets
// its own line (and gets clamped to mainAvail downstream so it doesn't
// overflow). Returns []line where each line is the slice of indices
// into items belonging to that line.
func packLines(items []flexItem, mainAvail, gap float32) [][]int {
	if len(items) == 0 {
		return nil
	}
	var lines [][]int
	var cur []int
	var used float32
	for i, it := range items {
		add := it.basis
		if len(cur) > 0 {
			add += gap
		}
		if len(cur) > 0 && used+add > mainAvail {
			lines = append(lines, cur)
			cur = []int{i}
			used = it.basis
			continue
		}
		cur = append(cur, i)
		used += add
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// applyWrap implements the flex-wrap pass: pack children into lines on
// main-axis overflow, then walk each line with its own main-axis
// justify and cross-axis line height. Within a line, items keep their
// basis (no grow / shrink) — wrap exists precisely to avoid resizing
// children. CrossGap separates the lines.
//
// Cross axis: each line's height = max child cross size. The
// containing widget should report Σline-heights + CrossGap*(lines-1)
// from its Measure so the parent allocates enough room for all rows;
// Container.Measure does that when LayoutEngine is a wrap FlexLayout.
func (l FlexLayout) applyWrap(children []Widget, bounds Rect, mainAvail, crossAvail float32) {
	items := l.measureFlexItems(children, bounds)
	lines := packLines(items, mainAvail, l.Gap)
	if len(lines) == 0 {
		return
	}
	// First pass: compute each line's cross extent.
	lineCross := make([]float32, len(lines))
	for li, line := range lines {
		var maxCross float32
		for _, idx := range line {
			c := items[idx].natural.H
			if l.Direction == Vertical {
				c = items[idx].natural.W
			}
			if c > maxCross {
				maxCross = c
			}
		}
		lineCross[li] = maxCross
	}

	// align-content: distribute leftover cross space among the lines.
	// Free space is what remains after line heights + CrossGaps; when the
	// lines overflow (free < 0) every mode degrades to start-packing,
	// matching distributeJustify's main-axis convention.
	totalCross := l.CrossGap * float32(len(lines)-1)
	for _, c := range lineCross {
		totalCross += c
	}
	var crossStart, extraLineGap float32
	if free := crossAvail - totalCross; free > 0 {
		switch l.AlignContent {
		case AlignContentStretch:
			grow := free / float32(len(lines))
			for li := range lineCross {
				lineCross[li] += grow
			}
		case AlignContentCenter:
			crossStart = free / 2
		case AlignContentEnd:
			crossStart = free
		case AlignContentSpaceBetween:
			if len(lines) > 1 {
				extraLineGap = free / float32(len(lines)-1)
			}
		case AlignContentSpaceAround:
			each := free / float32(len(lines))
			crossStart = each / 2
			extraLineGap = each
		case AlignContentSpaceEvenly:
			each := free / float32(len(lines)+1)
			crossStart = each
			extraLineGap = each
		}
	}

	// Second pass: place items.
	crossCursor := crossStart
	for li, line := range lines {
		// Main-axis: items at their basis (clamped to mainAvail in case
		// a single child is wider than the row), separated by Gap.
		// Justify uses the leftover after packing — for the most common
		// case (JustifyStart on a toolbar) this is just trailing slack.
		var lineMain float32
		for k, idx := range line {
			s := items[idx].basis
			if s > mainAvail {
				s = mainAvail
			}
			items[idx].size = s
			lineMain += s
			if k > 0 {
				lineMain += l.Gap
			}
		}
		mainStart, extraBetween, _ := distributeJustify(mainAvail-lineMain, len(line), l.Justify)
		mainOffset := mainStart

		// This line's cross-axis slot extends from crossCursor → crossCursor+lineCross[li].
		// resolveCross expects the line's own crossAvail.
		for k, idx := range line {
			it := items[idx]
			cross := resolveCross(it.flex.Align, l.AlignItems, lineCross[li], it.natural, it.explicitCross, it.minCross, it.maxCross, l.Direction)
			var r Rect
			if l.Direction == Horizontal {
				r = Rect{
					X: bounds.X + mainOffset,
					Y: bounds.Y + crossCursor + cross.offset,
					W: it.size,
					H: cross.size,
				}
			} else {
				r = Rect{
					X: bounds.X + crossCursor + cross.offset,
					Y: bounds.Y + mainOffset,
					W: cross.size,
					H: it.size,
				}
			}
			it.w.Layout(it.innerRect(r, l.Direction))
			mainOffset += it.size + l.Gap + extraBetween
			_ = k
		}
		crossCursor += lineCross[li]
		if li < len(lines)-1 {
			crossCursor += l.CrossGap + extraLineGap
		}
	}
}

// distributeJustify returns (start, extraBetween, extraEnd) offsets
// given free space and justify mode. The Gap field still applies —
// justify modes add ON TOP for SpaceBetween/Around/Evenly.
func distributeJustify(free float32, n int, j Justify) (start, extraBetween, extraEnd float32) {
	if free <= 0 || n <= 0 {
		return 0, 0, 0
	}
	switch j {
	case JustifyCenter:
		return free / 2, 0, free / 2
	case JustifyEnd:
		return free, 0, 0
	case JustifySpaceBetween:
		if n == 1 {
			return 0, 0, free
		}
		return 0, free / float32(n-1), 0
	case JustifySpaceAround:
		if n == 0 {
			return 0, 0, 0
		}
		each := free / float32(n)
		return each / 2, each, each / 2
	case JustifySpaceEvenly:
		each := free / float32(n+1)
		return each, each, each
	default: // JustifyStart
		return 0, 0, free
	}
}

type crossResult struct {
	offset float32
	size   float32
}

// resolveCross computes cross-axis size + offset for one item.
// natural is the post-constraints Measure result; we pull .H or .W
// depending on main-axis direction. explicitCross is the item's DECLARED
// cross size (0 = auto): CSS stretches only an auto cross size, so a
// declared one wins over AlignStretch. maxCross is the per-item cap on
// this axis (0 = unconstrained) — applied even for AlignStretch so a
// widget with a max width can't be stretched past it.
func resolveCross(itemAlign, containerAlign AlignCross, crossAvail float32, natural Size, explicitCross, minCross, maxCross float32, dir Direction) crossResult {
	align := itemAlign
	if align == AlignDefault {
		align = containerAlign
	}
	if align == AlignDefault {
		align = AlignStretch
	}
	var naturalCross float32
	if dir == Horizontal {
		naturalCross = natural.H
	} else {
		naturalCross = natural.W
	}
	clamp := func(size float32) float32 {
		if size < minCross {
			size = minCross
		}
		if maxCross > 0 && size > maxCross {
			size = maxCross
		}
		return size
	}
	switch align {
	case AlignStretch:
		// `stretch` fills the line only when the cross size is auto; an
		// explicit width (column) / height (row) keeps the item at that size,
		// flush to the line's start edge — same as a browser.
		fill := crossAvail
		if explicitCross > 0 {
			fill = explicitCross
		}
		return crossResult{offset: 0, size: clamp(fill)}
	case AlignCenter:
		size := clamp(naturalCross)
		return crossResult{offset: (crossAvail - size) / 2, size: size}
	case AlignEnd:
		size := clamp(naturalCross)
		return crossResult{offset: crossAvail - size, size: size}
	default: // AlignStart
		size := clamp(naturalCross)
		return crossResult{offset: 0, size: size}
	}
}

// ----------------------------------------------------------------------
// Grid — CSS-grid-like track sizing
//
// Columns and Rows are now described by per-track specs rather than
// simple counts. Each track can be:
//
//   - GridFixed:    exact pixel width  (Size field)
//   - GridFraction: share of remaining space, weight = Size  (the "fr" unit)
//   - GridAuto:     max-content — Measure children in this track, take max
//
// Children may either accept auto row-major placement or opt into a
// specific cell via GridItem { Col, Row, ColSpan, RowSpan }.

// GridTrackKind labels how a track's size is computed.
type GridTrackKind int

const (
	GridFixed    GridTrackKind = iota // Size is literal pixels
	GridFraction                      // Size is the weight for fraction distribution
	GridAuto                          // size = max child Measure on this track
)

// GridTrack specifies a column or row. Construct via GridFixedTrack /
// GridFractionTrack / GridAutoTrack helpers for readability.
type GridTrack struct {
	Kind GridTrackKind
	Size float32 // pixels for Fixed, weight for Fraction, unused for Auto
	Min  float32 // optional floor after fraction / auto resolution
}

// Convenience constructors.
func GridFixedTrack(pixels float32) GridTrack    { return GridTrack{Kind: GridFixed, Size: pixels} }
func GridFractionTrack(weight float32) GridTrack { return GridTrack{Kind: GridFraction, Size: weight} }
func GridAutoTrack() GridTrack                   { return GridTrack{Kind: GridAuto} }

// GridItem is per-child grid placement data. Zero value = auto-place
// in row-major order with span 1×1. Set Col/Row via SetCell for
// explicit placement (the Explicit flag tells auto-placement to skip
// this entry); zero Col/Row with Explicit=false auto-places.
type GridItem struct {
	Col      int
	Row      int
	ColSpan  int // 0 or 1 = single-cell
	RowSpan  int // 0 or 1 = single-cell
	Explicit bool
}

// SetCell is the canonical way to place a widget at an explicit grid
// cell. Sets Col, Row, and the Explicit flag together so zero-value
// (0, 0) placements don't get confused with unplaced widgets.
func (g *GridItem) SetCell(col, row int) {
	g.Col = col
	g.Row = row
	g.Explicit = true
}

// GridLayout arranges widgets in a grid with explicitly-sized tracks.
type GridLayout struct {
	Columns []GridTrack
	Rows    []GridTrack
	ColGap  float32
	RowGap  float32
}

// Apply resolves track sizes, determines each child's cell placement,
// and calls child.Layout with the computed rect.
//
// The child's final cell rect spans [colStart..colStart+colSpan-1]
// columns and [rowStart..rowStart+rowSpan-1] rows, including any
// col/row gaps internal to that span.
func (l GridLayout) Apply(children []Widget, bounds Rect) {
	if len(l.Columns) == 0 || len(l.Rows) == 0 || len(children) == 0 {
		return
	}

	// Gather placements and enumerate auto-placed children.
	placements := make([]GridItem, len(children))
	for i, ch := range children {
		placements[i] = GridItemOf(ch)
		if placements[i].ColSpan < 1 {
			placements[i].ColSpan = 1
		}
		if placements[i].RowSpan < 1 {
			placements[i].RowSpan = 1
		}
	}
	// Auto-place children whose placement isn't marked Explicit.
	l.autoPlace(placements)

	// Resolve track sizes — Fixed/Auto first, then Fraction. Columns first,
	// so auto ROW heights can measure each cell at its resolved column width
	// (wrapping content reports its true multi-line height, matching Measure).
	colWidths := l.resolveTracks(l.Columns, bounds.W, l.ColGap,
		children, placements, true, nil, 0)
	rowHeights := l.resolveTracks(l.Rows, bounds.H, l.RowGap,
		children, placements, false, colWidths, l.ColGap)
	// Grow auto rows so multi-row-spanning children (e.g. table rowspan) fit.
	growAutoRowsForRowSpans(rowHeights, l.Rows, l.RowGap, colWidths, l.ColGap,
		children, placements)

	// Walk children and lay each out into its spanning cell rect.
	for i, ch := range children {
		p := placements[i]
		if !p.Explicit || p.Col < 0 || p.Row < 0 || p.Col >= len(l.Columns) || p.Row >= len(l.Rows) {
			continue
		}
		cellX := bounds.X + trackOffset(colWidths, l.ColGap, p.Col)
		cellY := bounds.Y + trackOffset(rowHeights, l.RowGap, p.Row)
		cellW := trackSpan(colWidths, l.ColGap, p.Col, p.ColSpan)
		cellH := trackSpan(rowHeights, l.RowGap, p.Row, p.RowSpan)
		clamped := applyConstraints(Size{W: cellW, H: cellH}, widgetMinSize(ch), widgetMaxSize(ch))
		ch.Layout(Rect{X: cellX, Y: cellY, W: clamped.W, H: clamped.H})
	}
}

// Measure returns the grid's intrinsic size for `avail`: columns are
// resolved against avail.W, then each Auto/Fraction row's height is the max
// natural height of the single-row-span children in it (measured at their
// resolved column width so wrapping content reports its true height), and
// Fixed rows keep their size. Without this, a grid measured with H<=0 (the
// ScrollView / FlowLayout content-measure path) reported `available` verbatim
// and every sibling laid out after the grid overflowed the measured content
// and became unreachable.
func (l GridLayout) Measure(children []Widget, avail Size) Size {
	if len(l.Columns) == 0 || len(l.Rows) == 0 || len(children) == 0 {
		return Size{}
	}
	gl := l

	placements := make([]GridItem, len(children))
	for i, ch := range children {
		placements[i] = GridItemOf(ch)
		if placements[i].ColSpan < 1 {
			placements[i].ColSpan = 1
		}
		if placements[i].RowSpan < 1 {
			placements[i].RowSpan = 1
		}
	}
	gl.autoPlace(placements)

	colWidths := gl.resolveTracks(gl.Columns, avail.W, gl.ColGap, children, placements, true, nil, 0)

	rowHeights := make([]float32, len(gl.Rows))
	for i, tr := range gl.Rows {
		if tr.Kind == GridFixed {
			rowHeights[i] = tr.Size
		}
	}
	// Content rows: measure single-row children at their real cell width.
	for ci, ch := range children {
		p := placements[ci]
		if !p.Explicit || p.Row < 0 || p.Row >= len(gl.Rows) || p.RowSpan != 1 {
			continue
		}
		if gl.Rows[p.Row].Kind == GridFixed {
			continue
		}
		cw := trackSpan(colWidths, gl.ColGap, p.Col, p.ColSpan)
		nat := measureWithConstraints(ch, Size{W: cw, H: 0})
		if nat.H > rowHeights[p.Row] {
			rowHeights[p.Row] = nat.H
		}
	}
	// Grow auto rows so multi-row-spanning children fit (mirrors Apply).
	growAutoRowsForRowSpans(rowHeights, gl.Rows, gl.RowGap, colWidths, gl.ColGap,
		children, placements)
	for i, tr := range gl.Rows {
		if tr.Min > 0 && rowHeights[i] < tr.Min {
			rowHeights[i] = tr.Min
		}
	}

	var w float32
	for _, cw := range colWidths {
		w += cw
	}
	if len(colWidths) > 1 {
		w += gl.ColGap * float32(len(colWidths)-1)
	}
	var h float32
	for _, rh := range rowHeights {
		h += rh
	}
	if len(rowHeights) > 1 {
		h += gl.RowGap * float32(len(rowHeights)-1)
	}
	return Size{W: w, H: h}
}

// autoPlace fills in cells for non-Explicit placements via row-major
// scanning, respecting span sizes and already-occupied cells.
func (l GridLayout) autoPlace(placements []GridItem) {
	nCols, nRows := len(l.Columns), len(l.Rows)
	occupied := make([]bool, nCols*nRows)
	mark := func(col, row, colSpan, rowSpan int) {
		for r := row; r < row+rowSpan && r < nRows; r++ {
			for c := col; c < col+colSpan && c < nCols; c++ {
				if c >= 0 && r >= 0 {
					occupied[r*nCols+c] = true
				}
			}
		}
	}
	// First pass: mark all explicit placements.
	for _, p := range placements {
		if p.Explicit {
			mark(p.Col, p.Row, p.ColSpan, p.RowSpan)
		}
	}
	// Second pass: place autos into first free slot fitting their span.
	cursor := 0
	for i := range placements {
		if placements[i].Explicit {
			continue
		}
		p := placements[i]
		for ; cursor < nCols*nRows; cursor++ {
			c := cursor % nCols
			r := cursor / nCols
			if c+p.ColSpan > nCols || r+p.RowSpan > nRows {
				continue
			}
			ok := true
			for rr := r; rr < r+p.RowSpan && ok; rr++ {
				for cc := c; cc < c+p.ColSpan && ok; cc++ {
					if occupied[rr*nCols+cc] {
						ok = false
					}
				}
			}
			if ok {
				placements[i].Col = c
				placements[i].Row = r
				placements[i].Explicit = true
				mark(c, r, p.ColSpan, p.RowSpan)
				cursor++
				break
			}
		}
	}
}

// resolveTracks returns resolved pixel sizes for each track on one axis.
// Fixed tracks keep their Size. Auto tracks measure single-cell (span=1)
// children that land in them and take the max. Fraction tracks then
// split whatever remains by weight.
// crossSizes/crossGap carry the already-resolved cross-axis track sizes so
// that auto ROW heights (columnAxis == false) measure each cell at its true
// column-span width — wrapping content then reports its real multi-line
// height. They are nil/0 for the column pass (max-content width is measured
// against avail).
func (l GridLayout) resolveTracks(tracks []GridTrack, avail, gap float32,
	children []Widget, placements []GridItem, columnAxis bool,
	crossSizes []float32, crossGap float32) []float32 {
	sizes := make([]float32, len(tracks))
	used := gap * float32(len(tracks)-1) // total inter-track gaps
	var fractionSum float32

	// Pass 1: Fixed and Auto.
	for i, tr := range tracks {
		switch tr.Kind {
		case GridFixed:
			sizes[i] = tr.Size
			used += tr.Size
		case GridAuto:
			var max float32
			for ci, ch := range children {
				p := placements[ci]
				if columnAxis && (p.Col != i || p.ColSpan != 1) {
					continue
				}
				if !columnAxis && (p.Row != i || p.RowSpan != 1) {
					continue
				}
				if columnAxis {
					nat := measureWithConstraints(ch, Size{W: avail, H: avail})
					if nat.W > max {
						max = nat.W
					}
				} else {
					// Measure at the cell's resolved column-span width so
					// wrapping text contributes its true height.
					cw := trackSpan(crossSizes, crossGap, p.Col, p.ColSpan)
					nat := measureWithConstraints(ch, Size{W: cw, H: 0})
					if nat.H > max {
						max = nat.H
					}
				}
			}
			sizes[i] = max
			used += max
		case GridFraction:
			fractionSum += tr.Size
		}
	}

	// Pass 2: Fraction.
	if fractionSum > 0 {
		remaining := avail - used
		if remaining < 0 {
			remaining = 0
		}
		for i, tr := range tracks {
			if tr.Kind == GridFraction {
				sizes[i] = remaining * tr.Size / fractionSum
			}
		}
	}

	// Apply per-track Min after resolution.
	for i, tr := range tracks {
		if tr.Min > 0 && sizes[i] < tr.Min {
			sizes[i] = tr.Min
		}
	}
	return sizes
}

// growAutoRowsForRowSpans grows Auto rows so a multi-row-spanning child fits
// within its spanned rows (CSS grid span distribution). resolveTracks sizes
// tracks only from single-span children, so a rowspan cell taller than the sum
// of its spanned rows would otherwise overflow. For each such cell, measured at
// its resolved column-span width, any deficit over the current spanned-row
// extent (+ internal gaps) is split equally across the spanned Auto rows.
// Fixed/Fraction rows are left untouched.
func growAutoRowsForRowSpans(rowHeights []float32, rowTracks []GridTrack, rowGap float32,
	colWidths []float32, colGap float32, children []Widget, placements []GridItem) {
	for ci, ch := range children {
		p := placements[ci]
		if p.RowSpan < 2 || p.Row < 0 || p.Row >= len(rowTracks) {
			continue
		}
		end := p.Row + p.RowSpan
		if end > len(rowTracks) {
			end = len(rowTracks)
		}
		cur := rowGap * float32(end-p.Row-1)
		var autoRows []int
		for i := p.Row; i < end; i++ {
			cur += rowHeights[i]
			if rowTracks[i].Kind == GridAuto {
				autoRows = append(autoRows, i)
			}
		}
		if len(autoRows) == 0 {
			continue // no flexible row to absorb the overflow
		}
		cw := trackSpan(colWidths, colGap, p.Col, p.ColSpan)
		nat := measureWithConstraints(ch, Size{W: cw, H: 0})
		if nat.H <= cur {
			continue
		}
		share := (nat.H - cur) / float32(len(autoRows))
		for _, i := range autoRows {
			rowHeights[i] += share
		}
	}
}

// trackOffset sums track sizes + gaps up to (but not including) idx.
func trackOffset(sizes []float32, gap float32, idx int) float32 {
	var off float32
	for i := 0; i < idx; i++ {
		off += sizes[i] + gap
	}
	return off
}

// trackSpan returns the total extent of `span` tracks starting at idx,
// including internal gaps between those tracks.
func trackSpan(sizes []float32, gap float32, idx, span int) float32 {
	if idx < 0 || idx >= len(sizes) {
		return 0
	}
	end := idx + span
	if end > len(sizes) {
		end = len(sizes)
	}
	var total float32
	for i := idx; i < end; i++ {
		total += sizes[i]
	}
	total += gap * float32(end-idx-1)
	return total
}

// ----------------------------------------------------------------------
// AbsoluteLayout — anchor-based positioning.
//
// Each child specifies which parent edges to pin to via Anchor (a
// bitmask), distances from those edges, and optional Width / Height
// when not fully constrained by opposing anchors.
//
// Typical patterns:
//
//   - Pinned top-right corner (tooltip / badge): Anchor = Top|Right
//   - Full-height sidebar on left: Anchor = Left|Top|Bottom, Width = 240
//   - Stretch to fill parent: Anchor = Left|Right|Top|Bottom
//   - Centered dialog: skip this layout — use Dialog's built-in centering

// AnchorSide is a bitmask for AbsolutePosition.Anchor.
type AnchorSide int

const (
	AnchorLeft AnchorSide = 1 << iota
	AnchorTop
	AnchorRight
	AnchorBottom
)

// AbsolutePosition stores per-child anchor constraints.
type AbsolutePosition struct {
	Left, Top, Right, Bottom float32
	// Explicit size used when the corresponding axis isn't pinned at
	// both ends. Ignored otherwise (axis derives from parent edges).
	Width, Height float32
	Anchor        AnchorSide
}

// AbsoluteLayout positions children by their AbsolutePosition anchors.
// Children without an Anchor bitmask set fall back to Measure at the
// parent origin — useful for mixing a few unconstrained widgets with
// anchored peers in the same container.
type AbsoluteLayout struct{}

func (AbsoluteLayout) Apply(children []Widget, bounds Rect) {
	for _, ch := range children {
		pos := AbsolutePositionOf(ch)
		x, w := resolveAnchorAxis(pos.Anchor, AnchorLeft, AnchorRight,
			pos.Left, pos.Right, pos.Width, bounds.X, bounds.W, ch, true)
		y, h := resolveAnchorAxis(pos.Anchor, AnchorTop, AnchorBottom,
			pos.Top, pos.Bottom, pos.Height, bounds.Y, bounds.H, ch, false)
		clamped := applyConstraints(Size{W: w, H: h}, widgetMinSize(ch), widgetMaxSize(ch))
		ch.Layout(Rect{X: x, Y: y, W: clamped.W, H: clamped.H})
	}
}

// resolveAnchorAxis computes position + size for one axis. When both
// anchors are pinned, the widget stretches between them. When only
// one is pinned, the explicit size (or Measure) is placed flush with
// that edge. When neither is pinned, the widget sits at the parent
// origin at its natural size.
func resolveAnchorAxis(anchor, startBit, endBit AnchorSide,
	startDist, endDist, explicit, parentStart, parentExtent float32,
	child Widget, widthAxis bool) (pos, size float32) {

	start := anchor&startBit != 0
	end := anchor&endBit != 0
	min := widgetMinSize(child)
	minAxis := min.H
	if widthAxis {
		minAxis = min.W
	}

	switch {
	case start && end:
		pos = parentStart + startDist
		size = parentExtent - startDist - endDist
		if size < 0 {
			size = 0
		}
		if size < minAxis {
			size = minAxis
		}
	case start:
		pos = parentStart + startDist
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
	case end:
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
		pos = parentStart + parentExtent - endDist - size
	default:
		// Neither anchor — place at parent origin with natural size.
		pos = parentStart
		size = explicit
		if size <= 0 {
			size = anchorMeasure(child, parentExtent, widthAxis)
		}
		if size < minAxis {
			size = minAxis
		}
	}
	return pos, size
}

func anchorMeasure(child Widget, parentExtent float32, widthAxis bool) float32 {
	sz := MeasureChild(child, Size{W: parentExtent, H: parentExtent})
	if widthAxis {
		return sz.W
	}
	return sz.H
}

// Measure reports the intrinsic content size (excluding the container's
// padding) of children laid out by this engine within padded.
func (fl FlexLayout) Measure(children []Widget, padded Size) Size {
	if len(children) == 0 {
		return Size{}
	}
	// Per-child measurement, with main/cross extracted once.
	type childMeasure struct {
		main, cross, minMain float32
	}
	measures := make([]childMeasure, len(children))
	for i, child := range children {
		min := widgetMinSize(child)
		size := measureWithConstraints(child, padded)
		// Margin-box: a child's natural footprint includes its own
		// Style().Margin, matching FlexLayout.measureFlexItems.
		margin := child.Style().Margin
		size.W += margin.Horizontal()
		size.H += margin.Vertical()
		mainSize := size.W
		crossSize := size.H
		minMain := min.W
		marginMain := margin.Horizontal()
		if fl.Direction == Vertical {
			mainSize = size.H
			crossSize = size.W
			minMain = min.H
			marginMain = margin.Vertical()
		}
		if basis := widgetFlexItem(child).Basis; basis > 0 {
			mainSize = basis + marginMain
			if mainSize < minMain+marginMain {
				mainSize = minMain + marginMain
			}
		}
		measures[i] = childMeasure{main: mainSize, cross: crossSize, minMain: minMain}
	}

	// Wrap path: pack into lines, sum line heights for cross axis,
	// take the widest line for the main axis (caller may have given
	// us less; the line that overflowed already broke).
	if fl.Wrap {
		mainAvail := padded.W
		if fl.Direction == Vertical {
			mainAvail = padded.H
		}
		var lines [][]int
		var cur []int
		var used float32
		for i, m := range measures {
			add := m.main
			if len(cur) > 0 {
				add += fl.Gap
			}
			if len(cur) > 0 && used+add > mainAvail {
				lines = append(lines, cur)
				cur = []int{i}
				used = m.main
				continue
			}
			cur = append(cur, i)
			used += add
		}
		if len(cur) > 0 {
			lines = append(lines, cur)
		}
		var mainMax, crossSum float32
		for li, line := range lines {
			var lineMain, lineCross float32
			for k, idx := range line {
				lineMain += measures[idx].main
				if k > 0 {
					lineMain += fl.Gap
				}
				if measures[idx].cross > lineCross {
					lineCross = measures[idx].cross
				}
			}
			if lineMain > mainMax {
				mainMax = lineMain
			}
			crossSum += lineCross
			if li < len(lines)-1 {
				crossSum += fl.CrossGap
			}
		}
		var result Size
		if fl.Direction == Horizontal {
			result = Size{W: mainMax, H: crossSum}
		} else {
			result = Size{W: crossSum, H: mainMax}
		}
		return result
	}

	// Single-line path. Resolve the flex main sizes, then re-measure each
	// child's cross extent at its resolved main size — so a growing item
	// with wrapping text reports its true (multi-line) height instead of
	// the 1-line width-of-the-whole-row measure. Otherwise the parent
	// under-allocates the row and the item's content overflows its box.
	paddedRect := Rect{W: padded.W, H: padded.H}
	items := fl.measureFlexItems(children, paddedRect)
	mainAvail := padded.W
	if fl.Direction == Vertical {
		mainAvail = padded.H
	}
	// A non-positive main-axis available means "unconstrained" — the caller
	// (e.g. GridLayout.Measure passing H:0) wants our natural/max-content
	// main size, not a size shrunk to fit zero. Resolving against the basis
	// sum keeps grow/shrink off so we report the true content extent; the
	// parent decides whether to shrink us at Apply time. Without this, a
	// flex column measured for its intrinsic height collapses its children
	// toward their (often zero) min-main and reports a too-short height.
	if mainAvail <= 0 {
		var basisSum float32
		for i := range items {
			if i > 0 {
				basisSum += fl.Gap
			}
			basisSum += items[i].basis
		}
		mainAvail = basisSum
	}
	fl.resolveMainSizes(items, mainAvail)
	fl.remeasureCross(items, paddedRect)

	var mainSum, crossMax float32
	for i := range items {
		mainSum += items[i].size
		cross := items[i].natural.H
		if fl.Direction == Vertical {
			cross = items[i].natural.W
		}
		if cross > crossMax {
			crossMax = cross
		}
		if i > 0 {
			mainSum += fl.Gap
		}
	}
	var result Size
	if fl.Direction == Horizontal {
		result = Size{W: mainSum, H: crossMax}
	} else {
		result = Size{W: crossMax, H: mainSum}
	}
	return result
}
