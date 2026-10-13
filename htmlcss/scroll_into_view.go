package htmlcss

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// ScrollBlock is where ScrollIntoView places the element along the scroll
// axis — the DOM's scrollIntoView({block}) values.
type ScrollBlock int

const (
	// ScrollBlockNearest scrolls the least distance that makes the element
	// fully visible, and not at all when it already is.
	ScrollBlockNearest ScrollBlock = iota
	// ScrollBlockCenter centers the element in the viewport.
	ScrollBlockCenter
	// ScrollBlockStart aligns the element's top with the viewport's top.
	ScrollBlockStart
	// ScrollBlockEnd aligns the element's bottom with the viewport's bottom.
	ScrollBlockEnd
	// ScrollBlockCenterIfNeeded leaves a fully visible element alone and
	// centers one that is not — WebKit's scrollIntoViewIfNeeded(true), the
	// IDE "locate in tree" behaviour.
	ScrollBlockCenterIfNeeded
)

// ScrollIntoView scrolls every enclosing overflow:auto / scroll element,
// innermost first, so e becomes visible — Element.scrollIntoView. Geometry
// is the on-screen box (qui.InteractionBoundsOf), re-read after each
// container scrolls so nested containers compose. The element must be laid
// out; one that has not been is left alone.
func (e *El) ScrollIntoView(block ScrollBlock) {
	if e == nil {
		return
	}
	for w := e.Parent(); w != nil; w = w.Parent() {
		host, ok := w.(*El)
		if !ok || !host.overflowScroll || host.scrollView == nil {
			continue
		}
		sv := host.scrollView
		target := qui.InteractionBoundsOf(e)
		view := qui.InteractionBoundsOf(sv)
		if target.H <= 0 || view.H <= 0 {
			return
		}
		horizontal := sv.Orientation == widgets.ScrollHorizontal
		pos, size, vpos, vsize := target.Y, target.H, view.Y, view.H
		if horizontal {
			pos, size, vpos, vsize = target.X, target.W, view.X, view.W
		}
		if d := scrollDelta(block, pos-vpos, size, vsize); d != 0 {
			sv.ScrollTo(sv.ScrollOffset() + d)
		}
	}
}

// scrollDelta is how far to move the scroll offset for an element at rel
// (its offset from the viewport start) of length size in a viewport of
// length vsize.
func scrollDelta(block ScrollBlock, rel, size, vsize float32) float32 {
	switch block {
	case ScrollBlockStart:
		return rel
	case ScrollBlockEnd:
		return rel + size - vsize
	case ScrollBlockCenter:
		return rel + size/2 - vsize/2
	case ScrollBlockCenterIfNeeded:
		if rel >= 0 && rel+size <= vsize {
			return 0
		}
		return rel + size/2 - vsize/2
	}
	// Nearest: already visible → no move; taller than the viewport →
	// align the start.
	switch {
	case rel < 0 || size > vsize:
		return rel
	case rel+size > vsize:
		return rel + size - vsize
	}
	return 0
}
