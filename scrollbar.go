package qui

// Shared scrollbar geometry. ScrollView / ListView / TableView each own
// their own scroll state, but the geometry + drag math is identical
// across them — ScrollBar keeps the derivations in one place so a
// thumb-sizing tweak or appearance change is applied once.

const (
	VBarWidth    = 10
	vbarMinThumb = 24
)

// VBarTrackRect returns the scrollbar track rect pinned to the right
// edge of the given widget bounds.
func VBarTrackRect(widgetBounds Rect) Rect {
	return Rect{
		X: widgetBounds.X + widgetBounds.W - VBarWidth,
		Y: widgetBounds.Y,
		W: VBarWidth,
		H: widgetBounds.H,
	}
}

// HBarTrackRect returns the scrollbar track rect pinned to the bottom
// edge of the given widget bounds.
func HBarTrackRect(widgetBounds Rect) Rect {
	return Rect{
		X: widgetBounds.X,
		Y: widgetBounds.Y + widgetBounds.H - VBarWidth,
		W: widgetBounds.W,
		H: VBarWidth,
	}
}

// VBarThumbRect computes the thumb position + size for a vertical bar
// whose track spans the viewport. Returns the full track if content
// fits entirely.
func VBarThumbRect(track Rect, contentH, scrollY float32) Rect {
	return ScrollBar{Track: track, Content: contentH, Viewport: track.H, Offset: scrollY}.Thumb()
}

// VBarScrollFromDrag maps a mouse-Y during a thumb drag to an absolute
// scroll position, preserving the grab offset so the thumb doesn't
// snap under the cursor.
func VBarScrollFromDrag(track Rect, contentH, startScrollY, startMouseY, currentMouseY float32) float32 {
	b := ScrollBar{Track: track, Content: contentH, Viewport: track.H, Offset: startScrollY}
	d := ScrollBarDrag{Active: true, startMouse: startMouseY, startOffset: startScrollY}
	return d.Offset(b, Point{Y: currentMouseY})
}

// ScrollBar is the geometry of one scrollbar: a track along one axis and
// the scroll model it represents. Every scroll-owning widget (ScrollView,
// ListView, TableView) builds one from its own state and asks it for the
// thumb, track-click paging and drag mapping, so a thumb-sizing tweak
// lands everywhere at once. Value type — build a fresh one per query.
type ScrollBar struct {
	Track      Rect
	Horizontal bool    // track runs along X (a horizontal bar)
	Content    float32 // total content extent on the scroll axis
	Viewport   float32 // visible extent on the scroll axis
	Offset     float32 // current scroll offset
}

// main projects p onto the bar's axis.
func (b ScrollBar) main(p Point) float32 {
	if b.Horizontal {
		return p.X
	}
	return p.Y
}

func (b ScrollBar) trackLen() float32 {
	if b.Horizontal {
		return b.Track.W
	}
	return b.Track.H
}

// MaxScroll is the largest valid offset: content beyond the viewport.
func (b ScrollBar) MaxScroll() float32 {
	return max(b.Content-b.Viewport, 0)
}

// Clamp constrains off to [0, MaxScroll].
func (b ScrollBar) Clamp(off float32) float32 {
	return min(max(off, 0), b.MaxScroll())
}

// Thumb returns the thumb rectangle. Its length is the track's share of
// viewport/content with a 24 px floor so it stays draggable at extreme
// content lengths; it fills the track when the content fits.
func (b ScrollBar) Thumb() Rect {
	track := b.trackLen()
	if b.Content <= 0 {
		return b.Track
	}
	view := b.Viewport
	if view <= 0 {
		view = track
	}
	thumb := min(max(track*view/b.Content, vbarMinThumb), track)
	progress := float32(0)
	if m := b.MaxScroll(); m > 0 {
		progress = min(max(b.Offset/m, 0), 1)
	}
	pos := (track - thumb) * progress
	if b.Horizontal {
		return Rect{X: b.Track.X + pos, Y: b.Track.Y, W: thumb, H: b.Track.H}
	}
	return Rect{X: b.Track.X, Y: b.Track.Y + pos, W: b.Track.W, H: thumb}
}

// Press routes a primary-button press at p. On the thumb it starts drag
// (and returns the offset unchanged); on the track either side of the
// thumb it pages by 90% of the viewport toward the click. hit reports
// whether the bar took the press at all.
func (b ScrollBar) Press(p Point, drag *ScrollBarDrag) (offset float32, hit bool) {
	thumb := b.Thumb()
	if thumb.Contains(p) {
		drag.Active = true
		drag.startMouse = b.main(p)
		drag.startOffset = b.Offset
		return b.Offset, true
	}
	if !b.Track.Contains(p) {
		return b.Offset, false
	}
	page := b.Viewport * 0.9
	if b.main(p) < b.main(Point{X: thumb.X, Y: thumb.Y}) {
		page = -page
	}
	return b.Clamp(b.Offset + page), true
}

// Draw paints the bar in palette c (zero fields = theme); the thumb
// takes the active color while drag is in progress.
func (b ScrollBar) Draw(canvas Canvas, dragging bool, c ScrollbarColors) {
	VBarDrawColors(canvas, b.Track, b.Thumb(), dragging, c)
}

// ScrollBarDrag is the in-flight thumb drag a widget keeps between
// events. Press starts it; Offset maps each move; End stops it.
type ScrollBarDrag struct {
	Active      bool
	startMouse  float32 // pointer on the bar's axis at press time
	startOffset float32
}

// Offset maps the pointer at p to a clamped scroll offset for bar b,
// preserving the grab point so the thumb doesn't snap under the cursor.
func (d *ScrollBarDrag) Offset(b ScrollBar, p Point) float32 {
	travel := b.trackLen() - b.thumbLen() // the thumb's length ignores Offset
	m := b.MaxScroll()
	if travel <= 0 || m <= 0 {
		return b.Clamp(b.Offset)
	}
	return b.Clamp(d.startOffset + (b.main(p)-d.startMouse)*(m/travel))
}

// End stops the drag and reports whether one was in progress.
func (d *ScrollBarDrag) End() bool {
	was := d.Active
	d.Active = false
	return was
}

// thumbLen is the thumb's length on the bar's axis.
func (b ScrollBar) thumbLen() float32 {
	t := b.Thumb()
	if b.Horizontal {
		return t.W
	}
	return t.H
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
// Text toward its Surface: a barely-there track and a mid-gray thumb that
// darkens while dragged (a thin rounded overlay-style bar). Opaque
// results, because the rounded-rect fill doesn't alpha-blend.
func DefaultScrollbarColors() ScrollbarColors {
	th := CurrentTheme()
	return ScrollbarColors{
		Track:       LerpColor(th.Surface, th.Text, 0.03),
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

// VBarDrawColors is VBarDraw with a palette (zero fields = theme). The
// thumb paints as a pill inset 2 px from the track's long edges (a 6 px
// bar in the 10 px track); the hit area stays the full track width.
func VBarDrawColors(canvas Canvas, track, thumb Rect, dragging bool, c ScrollbarColors) {
	c = c.Resolve()
	canvas.FillRect(track, c.Track)
	color := c.Thumb
	if dragging {
		color = c.ThumbActive
	}
	const inset = 2
	pill := thumb
	if track.W <= track.H {
		if pill.W > 2*inset+2 {
			pill.X, pill.W = pill.X+inset, pill.W-2*inset
		}
	} else if pill.H > 2*inset+2 {
		pill.Y, pill.H = pill.Y+inset, pill.H-2*inset
	}
	r := pill.W
	if pill.H < r {
		r = pill.H
	}
	canvas.FillRoundedRect(pill, r/2, color)
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
