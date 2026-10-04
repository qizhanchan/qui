package graphs

import . "github.com/qizhanchan/qui"

// LegendPosition selects where the legend docks relative to the plot.
// v1 supports LegendBottom (default) and LegendRight; LegendTop and
// LegendLeft are reserved for wave 2.
type LegendPosition int

const (
	LegendBottom LegendPosition = iota
	LegendRight
	LegendTop
	LegendLeft
)

// legendSwatchSize is the bounding box for the Series.LegendMarker call.
const legendSwatchSize = 16

// legendItemPad is the gap between swatch and label, and between items.
const legendItemPad = 6

// Legend renders a swatch + label for each series in its owning Chart.
// Clicking a row toggles that series' visibility and invalidates the
// chart so the next frame re-measures auto-range and repaints.
type Legend struct {
	BaseWidget
	Position LegendPosition
	ItemFont Font

	chart *Chart
	// rowRects caches the click targets (one per visible entry).
	rowRects []Rect
	// layoutOrient: true for horizontal layout (Bottom/Top), false for vertical.
	layoutOrient bool
}

// NewLegend constructs a legend at the given position. Call
// chart.SetLegend(legend) to install it on a Chart.
func NewLegend(position LegendPosition) *Legend {
	l := &Legend{
		BaseWidget: NewBaseWidget(),
		Position:   position,
	}
	l.SetSelf(l)
	return l
}

// Measure reports the intrinsic size of the legend given the active
// font. For horizontal positions, height is one text line + padding;
// for vertical, height is N lines.
func (l *Legend) Measure(available Size) Size {
	font := l.itemFont()
	if l.chart == nil {
		return Size{}
	}
	l.layoutOrient = l.Position == LegendBottom || l.Position == LegendTop
	if l.layoutOrient {
		var w, h float32
		h = font.Size + legendItemPad
		for _, s := range l.chart.Series {
			w += legendSwatchSize + legendItemPad + labelWidth(s.Name(), font) + legendItemPad*2
		}
		return Size{W: minF32(w, available.W), H: h}
	}
	rowH := font.Size + 4
	var maxLabel float32
	for _, s := range l.chart.Series {
		lw := labelWidth(s.Name(), font)
		if lw > maxLabel {
			maxLabel = lw
		}
	}
	w := legendSwatchSize + legendItemPad + maxLabel + legendItemPad*2
	h := rowH * float32(len(l.chart.Series))
	return Size{W: w, H: h}
}

// Layout caches the row rectangles so Draw and Handle share them.
func (l *Legend) Layout(r Rect) {
	l.BaseWidget.Layout(r)
	font := l.itemFont()
	if l.chart == nil {
		l.rowRects = nil
		return
	}
	l.rowRects = l.rowRects[:0]
	if l.layoutOrient {
		x := r.X
		y := r.Y
		for _, s := range l.chart.Series {
			rowW := legendSwatchSize + legendItemPad + labelWidth(s.Name(), font) + legendItemPad
			l.rowRects = append(l.rowRects, Rect{X: x, Y: y, W: rowW, H: r.H})
			x += rowW + legendItemPad
		}
	} else {
		rowH := font.Size + 4
		for i := range l.chart.Series {
			l.rowRects = append(l.rowRects, Rect{X: r.X, Y: r.Y + float32(i)*rowH, W: r.W, H: rowH})
		}
	}
}

func (l *Legend) Draw(canvas Canvas) {
	if l.chart == nil {
		return
	}
	theme := CurrentTheme()
	font := l.itemFont()
	for i, s := range l.chart.Series {
		if i >= len(l.rowRects) {
			break
		}
		row := l.rowRects[i]
		swatch := Rect{
			X: row.X,
			Y: row.Y + (row.H-legendSwatchSize)/2,
			W: legendSwatchSize,
			H: legendSwatchSize,
		}
		// Dim hidden series so users see the toggle state clearly.
		swatchColor := s.Color()
		textColor := theme.Text
		if !s.Visible() {
			swatchColor.A *= 0.3
			textColor = theme.TextMuted
		}
		// Swap in a muted-color legend marker by temporarily replacing
		// the series color via interface extension — cheaper to just
		// paint via the Series marker and overpaint a fade rect when
		// hidden. Fade rect option keeps Series.LegendMarker pure.
		s.LegendMarker(canvas, swatch)
		if !s.Visible() {
			canvas.FillRect(swatch, Color{R: theme.Surface.R, G: theme.Surface.G, B: theme.Surface.B, A: 0.55})
		}
		labelRect := Rect{
			X: swatch.X + legendSwatchSize + legendItemPad,
			Y: row.Y + (row.H-font.Size)/2,
			W: row.W - legendSwatchSize - legendItemPad,
			H: font.Size + 2,
		}
		canvas.DrawText(s.Name(), labelRect, textColor, font)
		_ = swatchColor
	}
}

// Handle toggles visibility on click.
func (l *Legend) Handle(event Event) bool {
	me, ok := event.(MouseEvent)
	if !ok {
		return false
	}
	if me.Type() != EventMouseDown || me.Button != MouseButtonLeft {
		return false
	}
	p := Point{X: me.X, Y: me.Y}
	if l.chart == nil {
		return false
	}
	for i, row := range l.rowRects {
		if !row.Contains(p) {
			continue
		}
		if i >= len(l.chart.Series) {
			return false
		}
		s := l.chart.Series[i]
		s.SetVisible(!s.Visible())
		l.chart.InvalidateLayout()
		return true
	}
	return false
}

func (l *Legend) HitTest(p Point) Widget {
	if !l.Bounds().Contains(p) {
		return nil
	}
	return l
}

func (l *Legend) itemFont() Font {
	if l.ItemFont.Size > 0 {
		return l.ItemFont
	}
	sz := CurrentTheme().FontSmall
	if sz <= 0 {
		sz = 12
	}
	return Font{Size: sz}
}

// labelWidth estimates text width without calling into font
// measurement. The 0.55×size factor matches the font-face-char-avg
// estimate used by drawAxes — close enough for legend layout.
func labelWidth(label string, font Font) float32 {
	return float32(len(label)) * font.Size * 0.55
}
