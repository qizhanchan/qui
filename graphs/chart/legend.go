package chart

import . "github.com/qizhanchan/qui"

// LegendPosition selects where the legend docks relative to the plot.
// Supports LegendBottom (default), LegendRight, LegendTop and LegendLeft.
type LegendPosition int

const (
	LegendBottom LegendPosition = iota
	LegendRight
	LegendTop
	LegendLeft
)

// ECharts' default legend item is a 24x14 symbol with 6px to its label
// and 8px trailing space before the next item.
const (
	legendSwatchW  float32 = 24
	legendSwatchH  float32 = 14
	legendLabelGap float32 = 6
	legendTrailPad float32 = 5
	legendRowH     float32 = 24
)

type legendEntry struct {
	label     string
	color     Color
	visible   bool
	seriesIdx int
	pointIdx  int
	marker    Series
}

func (l *Legend) entries() []legendEntry {
	if l.chart == nil {
		return nil
	}
	var entries []legendEntry
	for si, series := range l.chart.Series {
		if pie, ok := series.(*PieSeries); ok {
			for pi, slice := range pie.Slices {
				color := slice.Color
				if isZeroColor(color) {
					color = pie.Color()
				}
				entries = append(entries, legendEntry{
					label: slice.Label, color: color, visible: !slice.Hidden,
					seriesIdx: si, pointIdx: pi,
				})
			}
			continue
		}
		entries = append(entries, legendEntry{
			label: series.Name(), color: series.Color(), visible: series.Visible(),
			seriesIdx: si, pointIdx: -1, marker: series,
		})
	}
	return entries
}

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
	entries := l.entries()
	if l.layoutOrient {
		var w float32
		for _, e := range entries {
			w += legendSwatchW + legendLabelGap + labelWidth(e.label, font) + legendTrailPad
		}
		return Size{W: minF32(w, available.W), H: legendRowH}
	}
	rowH := legendRowH
	var maxLabel float32
	for _, e := range entries {
		lw := labelWidth(e.label, font)
		if lw > maxLabel {
			maxLabel = lw
		}
	}
	w := legendSwatchW + legendLabelGap + maxLabel + legendTrailPad
	h := rowH * float32(len(entries))
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
	entries := l.entries()
	if l.layoutOrient {
		var totalW float32
		for _, e := range entries {
			totalW += legendSwatchW + legendLabelGap + labelWidth(e.label, font) + legendTrailPad
		}
		x := r.X + maxF32(0, (r.W-totalW)/2) + 5
		y := r.Y
		for _, e := range entries {
			rowW := legendSwatchW + legendLabelGap + labelWidth(e.label, font) + legendTrailPad
			l.rowRects = append(l.rowRects, Rect{X: x, Y: y, W: rowW, H: r.H})
			x += rowW
		}
	} else {
		rowH := legendRowH
		for i := range entries {
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
	entries := l.entries()
	for i, entry := range entries {
		if i >= len(l.rowRects) {
			break
		}
		row := l.rowRects[i]
		swatch := Rect{
			X: row.X,
			Y: row.Y + (row.H-legendSwatchH)/2,
			W: legendSwatchW,
			H: legendSwatchH,
		}
		// Dim hidden series so users see the toggle state clearly.
		swatchColor := entry.color
		textColor := theme.Text
		if l.chart.TextColor.A > 0 {
			textColor = l.chart.TextColor
		}
		if !entry.visible {
			swatchColor.A *= 0.3
			textColor = theme.TextMuted
		}
		// Swap in a muted-color legend marker by temporarily replacing
		// the series color via interface extension — cheaper to just
		// paint via the Series marker and overpaint a fade rect when
		// hidden. Fade rect option keeps Series.LegendMarker pure.
		if entry.pointIdx >= 0 {
			canvas.FillRoundedRect(swatch, 2, swatchColor)
		} else {
			entry.marker.LegendMarker(canvas, swatch)
		}
		if !entry.visible {
			canvas.FillRect(swatch, Color{R: theme.Surface.R, G: theme.Surface.G, B: theme.Surface.B, A: 0.55})
		}
		labelRect := Rect{
			X: swatch.X + legendSwatchW + legendLabelGap,
			Y: row.Y + (row.H-font.Size)/2 - 3,
			W: row.W - legendSwatchW - legendLabelGap,
			H: font.Size + 2,
		}
		canvas.DrawText(entry.label, labelRect, textColor, font)
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
	entries := l.entries()
	for i, row := range l.rowRects {
		if !row.Contains(p) || i >= len(entries) {
			continue
		}
		entry := entries[i]
		if entry.pointIdx >= 0 {
			pie := l.chart.Series[entry.seriesIdx].(*PieSeries)
			pie.Slices[entry.pointIdx].Hidden = !pie.Slices[entry.pointIdx].Hidden
		} else {
			l.chart.Series[entry.seriesIdx].SetVisible(!entry.visible)
		}
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
	if width, _ := TextMetrics(label, font); width > 0 {
		return width
	}
	return float32(len(label)) * font.Size * 0.55
}
