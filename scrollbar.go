package qui

// Shared vertical-scrollbar helpers. ScrollView / ListView / TreeView /
// TableView each own their own scroll state, but the geometry +
// drag-math is identical across them — this file keeps the
// derivations in one place so a thumb-sizing tweak or appearance
// change doesn't have to be applied four times.

const (
	VBarWidth    = 10
	vbarMinThumb = 24
)

// vbarTrackRect returns the scrollbar track rect pinned to the right
// edge of the given widget bounds.
func VBarTrackRect(widgetBounds Rect) Rect {
	return Rect{
		X: widgetBounds.X + widgetBounds.W - VBarWidth,
		Y: widgetBounds.Y,
		W: VBarWidth,
		H: widgetBounds.H,
	}
}

// vbarThumbRect computes the thumb position + size given the current
// scroll position. Returns the full track if content fits entirely.
func VBarThumbRect(track Rect, contentH, scrollY float32) Rect {
	if contentH <= 0 {
		return track
	}
	thumbH := track.H * track.H / contentH
	if thumbH < vbarMinThumb {
		thumbH = vbarMinThumb
	}
	if thumbH > track.H {
		thumbH = track.H
	}
	travel := track.H - thumbH
	progress := float32(0)
	maxScroll := contentH - track.H
	if maxScroll > 0 {
		progress = scrollY / maxScroll
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	return Rect{X: track.X, Y: track.Y + travel*progress, W: track.W, H: thumbH}
}

// vbarScrollFromDrag maps a mouse-Y during a thumb drag to an absolute
// scroll position, preserving the grab offset so the thumb doesn't
// snap under the cursor.
func VBarScrollFromDrag(track Rect, contentH, startScrollY, startMouseY, currentMouseY float32) float32 {
	thumb := VBarThumbRect(track, contentH, startScrollY)
	travel := track.H - thumb.H
	if travel <= 0 {
		return startScrollY
	}
	maxScroll := contentH - track.H
	if maxScroll <= 0 {
		return 0
	}
	dy := currentMouseY - startMouseY
	return startScrollY + dy*(maxScroll/travel)
}

// vbarDraw renders the track + thumb onto canvas. Caller decides when
// to highlight (usually while dragging). Colors follow the light UI
// convention: a faint gutter track with a mid-grey thumb that darkens
// while dragged (matching desktop-spreadsheet scrollbars).
func VBarDraw(canvas Canvas, track, thumb Rect, dragging bool) {
	canvas.FillRoundedRect(track, 2, Color{R: 0.95, G: 0.95, B: 0.96, A: 1})
	color := Color{R: 0.74, G: 0.75, B: 0.77, A: 1}
	if dragging {
		color = Color{R: 0.56, G: 0.57, B: 0.60, A: 1}
	}
	canvas.FillRoundedRect(thumb, 2, color)
}

// ClampScroll returns scrollY constrained to [0, max(0, contentH - viewH)].
func ClampScroll(scrollY, contentH, viewH float32) float32 {
	max := contentH - viewH
	if max < 0 {
		max = 0
	}
	if scrollY < 0 {
		return 0
	}
	if scrollY > max {
		return max
	}
	return scrollY
}
