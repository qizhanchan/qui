package widgets

import (
	"slices"

	. "github.com/qizhanchan/qui"
)

// fixedRowContentHeight returns the total vertical extent of a fixed-height
// row model. It centralizes the math shared by ListView and TableView
// so future changes to row virtualization stay consistent.
func fixedRowContentHeight(count int, rowHeight float32) float32 {
	if count <= 0 || rowHeight <= 0 {
		return 0
	}
	return float32(count) * rowHeight
}

// fixedRowVisibleRange returns the half-open [start, end) range of rows that
// intersect viewport at scrollY.
func fixedRowVisibleRange(count int, rowHeight, scrollY float32, viewport Rect) (int, int) {
	if count <= 0 || rowHeight <= 0 || viewport.H <= 0 {
		return 0, 0
	}
	start := int(scrollY / rowHeight)
	end := int((scrollY+viewport.H)/rowHeight) + 1
	if start < 0 {
		start = 0
	}
	if end > count {
		end = count
	}
	if end < start {
		end = start
	}
	return start, end
}

// fixedRowRect returns a row's window-coordinate rectangle inside viewport.
func fixedRowRect(viewport Rect, index int, rowHeight, scrollY, contentW float32) Rect {
	rowY := viewport.Y + float32(index)*rowHeight - scrollY
	return Rect{X: viewport.X, Y: rowY, W: contentW, H: rowHeight}
}

// fixedRowAt maps a window-coordinate point to a row index, returning -1
// when the point is outside viewport/contentW or the row range.
func fixedRowAt(viewport Rect, contentW float32, count int, rowHeight, scrollY, x, y float32) int {
	if !viewport.Contains(Point{X: x, Y: y}) {
		return -1
	}
	if x > viewport.X+contentW {
		return -1
	}
	if count <= 0 || rowHeight <= 0 {
		return -1
	}
	idx := int((y - viewport.Y + scrollY) / rowHeight)
	if idx < 0 || idx >= count {
		return -1
	}
	return idx
}

// fixedRowEnsureVisible returns a scrollY value that keeps index visible in
// viewport with one row of breathing room, capped to one third of viewport
// height. This matches the pre-existing ListView/TableView/TreeView behavior.
func fixedRowEnsureVisible(scrollY float32, index, count int, rowHeight float32, viewport Rect) float32 {
	if index < 0 || index >= count || rowHeight <= 0 || viewport.H <= 0 {
		return ClampScroll(scrollY, fixedRowContentHeight(count, rowHeight), viewport.H)
	}
	rowTop := float32(index) * rowHeight
	rowBot := rowTop + rowHeight
	viewTop := scrollY
	viewBot := scrollY + viewport.H
	pad := rowHeight
	if maxPad := viewport.H / 3; pad > maxPad {
		pad = maxPad
	}
	if rowTop < viewTop+pad {
		scrollY = rowTop - pad
	} else if rowBot > viewBot-pad {
		scrollY = rowBot + pad - viewport.H
	}
	return ClampScroll(scrollY, fixedRowContentHeight(count, rowHeight), viewport.H)
}

// lazyRows backs the factory flavor of widget-row mode
// (ListView / TableView SetRowFactory): row widgets are built only when
// their row scrolls into view and released once it scrolls well out, so a
// 100k-row list of widgets costs what its visible rows cost.
type lazyRows struct {
	count int
	build func(index int) Widget
	live  map[int]Widget
}

// lazyRowOverscan is how many rows beyond the viewport stay built, so
// small scrolls don't rebuild at the edges.
const lazyRowOverscan = 8

// get returns row i, building and adopting it under parent on first use.
func (l *lazyRows) get(i int, parent Widget, w *Window) Widget {
	if l == nil || i < 0 || i >= l.count || l.build == nil {
		return nil
	}
	if row, ok := l.live[i]; ok {
		return row
	}
	row := l.build(i)
	if row == nil || !AdoptWidgetTree(row, parent, w) {
		return nil
	}
	if l.live == nil {
		l.live = map[int]Widget{}
	}
	l.live[i] = row
	return row
}

// prune releases rows outside [start-overscan, end+overscan).
func (l *lazyRows) prune(start, end int) {
	for i, row := range l.live {
		if i < start-lazyRowOverscan || i >= end+lazyRowOverscan || i >= l.count {
			delete(l.live, i)
			DetachWidgetTree(row)
		}
	}
}

// releaseAll drops every built row (data changed, or leaving lazy mode).
func (l *lazyRows) releaseAll() {
	if l == nil {
		return
	}
	for i, row := range l.live {
		delete(l.live, i)
		DetachWidgetTree(row)
	}
}

// release forgets child if it is a live row (a transfer out of the list).
func (l *lazyRows) release(child Widget) bool {
	if l == nil {
		return false
	}
	for i, row := range l.live {
		if row == child {
			delete(l.live, i)
			return true
		}
	}
	return false
}

// children returns the built rows in row order.
func (l *lazyRows) children() []Widget {
	if l == nil || len(l.live) == 0 {
		return nil
	}
	idx := make([]int, 0, len(l.live))
	for i := range l.live {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	out := make([]Widget, len(idx))
	for k, i := range idx {
		out[k] = l.live[i]
	}
	return out
}
