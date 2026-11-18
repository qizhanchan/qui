package widgets

import (
	"sort"

	. "github.com/qizhanchan/qui"
)

// CSS stacking (CSS 2.1 Appendix E, simplified): within a stacking
// context, positioned descendants paint after the in-flow content in
// z-index order (negative ones before it), whatever their depth in the
// tree. A Box marked Positioned is skipped where its parent paints its
// children; the nearest stacking root above it paints it as a layer.
//
// A stacking root is a Box that forms a stacking context, clips its
// children (an overflow: hidden box keeps its positioned descendants inside
// its clip), or whose parent is not a Box (the top of an htmlcss tree, the
// content of a scroll view, an atom inside an inline run) — every
// positioned Box is reached by exactly one root.

// stackBox is implemented by Box and everything embedding it.
type stackBox interface {
	stackBox() *Box
}

func (b *Box) stackBox() *Box { return b }

func asStackBox(w Widget) *Box {
	if sb, ok := w.(stackBox); ok {
		return sb.stackBox()
	}
	return nil
}

// isPositionedBox reports a positioned Box child: its stacking root paints
// it, not its parent.
func isPositionedBox(w Widget) bool {
	b := asStackBox(w)
	return b != nil && b.Positioned
}

// stackRoot reports whether b paints (and hit-tests) the positioned
// descendants it reaches.
func (b *Box) stackRoot() bool {
	if b.StackingContext || b.ClipChildren {
		return true
	}
	p := b.Parent()
	return p == nil || asStackBox(p) == nil
}

// stackLayers lists the positioned descendants b paints as layers, sorted
// by z-index (stable: tree order within one z-index). It descends through
// Boxes that are not stacking roots themselves; a positioned box that is
// not a stacking context (z-index: auto) is a layer, and its own positioned
// descendants join the same list.
func (b *Box) stackLayers() []Widget {
	var out []Widget
	var walk func(n *Box)
	walk = func(n *Box) {
		for _, c := range n.ChildList() {
			cb := asStackBox(c)
			if cb == nil || boxChildHidden(c) {
				continue
			}
			if cb.Positioned {
				out = append(out, c)
			}
			if !cb.StackingContext && !cb.ClipChildren {
				walk(cb)
			}
		}
	}
	walk(b)
	if len(out) > 1 {
		sort.SliceStable(out, func(i, j int) bool { return zIndexOf(out[i]) < zIndexOf(out[j]) })
	}
	return out
}

func zIndexOf(w Widget) int {
	if z, ok := w.(ZIndexed); ok {
		return z.ZIndex()
	}
	return 0
}

// isWidgetHiddenForHit reports a box that takes no pointer input at all.
func isWidgetHiddenForHit(b *Box) bool {
	self := b.Self()
	if self == nil {
		self = b
	}
	return boxChildHidden(self)
}
