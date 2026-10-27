package qui

import (
	"sort"
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
	// BasisAuto: with Grow > 0, start from the measured content size
	// rather than 0 (CSS flex-basis: auto). See Style.BasisAuto.
	BasisAuto bool
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
// Package-private; exposed only to layout_debug_log.go's overflow logger.
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
		if s.BasisAuto {
			fx.BasisAuto = true
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
				nat.W = s.pctWidth(s.WidthPct, bounds.W)
				if l.Direction == Horizontal && basis <= 0 {
					basis = nat.W
				}
			}
			if s.Height == 0 && s.HeightPct > 0 {
				nat.H = s.pctHeight(s.HeightPct, bounds.H)
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
					explicitCross = s.pctWidth(s.WidthPct, bounds.W)
				} else if l.Direction == Horizontal && s.HeightPct > 0 {
					explicitCross = s.pctHeight(s.HeightPct, bounds.H)
				}
			}
		}
		if basis <= 0 {
			if fx.Grow > 0 && !fx.BasisAuto {
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
	if flexDebugLayoutEnabled.Load() && leftover < -0.5 {
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
			// CSS flex grow: a grow-factor sum below 1 hands out only that
			// fraction of the free space, and an item clamped at its max
			// size freezes so its surplus goes to its siblings.
			active := make([]bool, n)
			for i := range items {
				items[i].size = items[i].basis
				active[i] = items[i].flex.Grow > 0
			}
			remaining := free
			for remaining > 0 {
				var gs float32
				for i := range items {
					if active[i] {
						gs += items[i].flex.Grow
					}
				}
				if gs <= 0 {
					break
				}
				dist := remaining
				if gs < 1 && free*gs < dist {
					dist = free * gs
				}
				frozeAny := false
				for i := range items {
					if !active[i] || items[i].maxMain <= 0 {
						continue
					}
					if items[i].size+dist*items[i].flex.Grow/gs > items[i].maxMain {
						remaining -= max(items[i].maxMain-items[i].size, 0)
						items[i].size = max(items[i].maxMain, items[i].size)
						active[i] = false
						frozeAny = true
					}
				}
				if frozeAny {
					continue
				}
				for i := range items {
					if active[i] {
						items[i].size += dist * items[i].flex.Grow / gs
					}
				}
				break
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
