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

// ScrollbarColors is a scrollbar palette. Zero fields fall back to the
// theme-derived default (see DefaultScrollbarColors), so a widget can carry
// a partial override — e.g. one set from CSS `scrollbar-color`.
type ScrollbarColors struct {
	Track       Color
	Thumb       Color
	ThumbActive Color // while dragged
}

// DefaultScrollbarColors derives the bar colors by mixing the theme's
// Text toward its Surface: a faint track and a mid-gray thumb that darkens
// while dragged. Opaque results, because the rounded-rect fill doesn't
// alpha-blend.
func DefaultScrollbarColors() ScrollbarColors {
	th := CurrentTheme()
	return ScrollbarColors{
		Track:       LerpColor(th.Surface, th.Text, 0.08),
		Thumb:       LerpColor(th.Surface, th.Text, 0.30),
		ThumbActive: LerpColor(th.Surface, th.Text, 0.45),
	}
}

// Resolve fills zero fields from DefaultScrollbarColors.
func (c ScrollbarColors) Resolve() ScrollbarColors {
	d := DefaultScrollbarColors()
	if c.Track == (Color{}) {
		c.Track = d.Track
	}
	if c.Thumb == (Color{}) {
		c.Thumb = d.Thumb
	}
	if c.ThumbActive == (Color{}) {
		c.ThumbActive = d.ThumbActive
	}
	return c
}

// VBarDraw renders the track + thumb in the theme's scrollbar colors.
// Caller decides when to highlight (usually while dragging).
func VBarDraw(canvas Canvas, track, thumb Rect, dragging bool) {
	VBarDrawColors(canvas, track, thumb, dragging, ScrollbarColors{})
}

// VBarDrawColors is VBarDraw with a palette (zero fields = theme).
func VBarDrawColors(canvas Canvas, track, thumb Rect, dragging bool, c ScrollbarColors) {
	c = c.Resolve()
	canvas.FillRoundedRect(track, 2, c.Track)
	color := c.Thumb
	if dragging {
		color = c.ThumbActive
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
