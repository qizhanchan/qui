package widgets

import . "github.com/qizhanchan/qui"

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
