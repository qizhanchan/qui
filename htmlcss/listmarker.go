package htmlcss

import "github.com/qizhanchan/qui"

// listMarkerWidget draws a ul bullet (disc / circle / square) as geometry
// rather than as a text glyph.
//
// The obvious implementation — a Label holding "•" — renders through the
// primary font, and qui's default primary is a system CJK face (PingFang on
// macOS, Noto CJK on Linux, YaHei on Windows). In those, U+2022 BULLET is
// FULL-WIDTH punctuation: a 1.0 em advance carrying a ~0.15 em dot. That made
// disc items indent ~9px further than circle or square ones in the same
// document, with a speck of a bullet adrift in its cell. Drawing the shape
// keeps every list-style-type the same weight and the marker column
// font-independent.
//
// Numeric markers ("1.") stay a Label — they are genuinely text, and digits
// carry no such full-width surprise.
type listMarkerShape int

const (
	markerDisc listMarkerShape = iota
	markerCircle
	markerSquare
)

// Marker geometry, in em of the list item's font size. All three shapes share
// one box, as they do in a browser — only the fill differs.
//
// 0.40 em is a hair larger than a browser disc (~0.375 em), chosen because it
// is the smallest box in which list-style-type:circle still rasterizes as a
// RING at a 14px body font: at 0.35 em the ring's left and right walls land in
// the same pixel columns as its hole and average away, leaving two dashes.
const (
	markerSizeEm       = 0.40
	markerStrokeEm     = 0.07 // ring thickness for list-style-type:circle
	markerMinStrokePx  = 1
	markerFallbackSize = 14
)

// markerShapeFor maps the marker STRING (which stays the clipboard/AX value)
// onto a drawn shape. Reports false for anything else — notably "N." — so
// those keep the text path.
func markerShapeFor(marker string) (listMarkerShape, bool) {
	switch marker {
	case "•":
		return markerDisc, true
	case "◦":
		return markerCircle, true
	case "▪":
		return markerSquare, true
	}
	return 0, false
}

type listMarkerWidget struct {
	qui.BaseWidget
	shape listMarkerShape
	font  qui.Font
	color qui.Color
}

func newListMarker(shape listMarkerShape, font qui.Font, color qui.Color) *listMarkerWidget {
	m := &listMarkerWidget{shape: shape, font: font, color: color}
	m.BaseWidget = qui.NewBaseWidget()
	m.SetSelf(m)
	return m
}

// set updates the marker in place — El widgets are reused across restyles, so
// every CSS-driven property has to be reassigned rather than assumed stale.
func (m *listMarkerWidget) set(shape listMarkerShape, font qui.Font, color qui.Color) {
	m.shape, m.font, m.color = shape, font, color
}

func (m *listMarkerWidget) fontSize() float32 {
	if m.font.Size > 0 {
		return m.font.Size
	}
	return markerFallbackSize
}

func (m *listMarkerWidget) size() float32 { return m.fontSize() * markerSizeEm }

// Measure claims one full line box vertically, so the marker occupies the
// item's FIRST line in the [marker | content] row (the row aligns to start)
// and can center itself on that line's text rather than on the whole item.
func (m *listMarkerWidget) Measure(available qui.Size) qui.Size {
	_, lineH := qui.TextMetrics("x", m.font)
	if lineH <= 0 {
		lineH = m.fontSize()
	}
	// Width is the shape itself — the [marker | content] row's Gap supplies
	// the spacing to the item text.
	return qui.Size{W: m.size(), H: lineH}
}

func (m *listMarkerWidget) Draw(canvas qui.Canvas) {
	b := m.Bounds()
	size := m.size()
	if size <= 0 {
		return
	}
	// Line up with the lowercase mass of the item's text, exactly as the
	// text renderer would place it in this same box.
	cy := qui.TextXHeightCenterY(b, m.font)
	rect := qui.Rect{X: b.X, Y: cy - size/2, W: size, H: size}
	switch m.shape {
	case markerSquare:
		canvas.FillRect(rect, m.color)
	case markerCircle:
		stroke := m.fontSize() * markerStrokeEm
		if stroke < markerMinStrokePx {
			stroke = markerMinStrokePx
		}
		canvas.StrokeRoundedRect(rect, size/2, m.color, stroke)
	default: // markerDisc
		canvas.FillRoundedRect(rect, size/2, m.color)
	}
}
