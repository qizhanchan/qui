package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// RadarSeries draws one filled/stroked polygon on a radar coordinate
// system. Indicator maxima normalize each axis.
type RadarSeries struct {
	seriesBase
	Indicators  []RadarIndicator
	Values      []float32
	FillAlpha   float32
	StrokeWidth float32
}

func NewRadarSeries(name string, indicators []RadarIndicator, values []float32) *RadarSeries {
	return &RadarSeries{
		seriesBase: newSeriesBase(name), Indicators: indicators, Values: values,
		FillAlpha: 0, StrokeWidth: 2,
	}
}

func (s *RadarSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func (s *RadarSeries) Draw(canvas Canvas, plot Rect, _, _ Axis, hot HotSpot) {
	n := len(s.Indicators)
	if n < 3 {
		return
	}
	// ECharts positions a radar coordinate in the full chart viewport,
	// centered at 50%/50%, with its default radius set to 50% of half the
	// shorter viewport side.
	cx, cy := plot.X+plot.W/2, plot.Y+plot.H/2
	radius := minF32(plot.W, plot.H) * 0.25
	pointAt := func(i int, ratio float32) Point {
		angle := -float32(math.Pi)/2 - float32(i)*2*float32(math.Pi)/float32(n)
		return Point{X: cx + radius*ratio*float32(math.Cos(float64(angle))), Y: cy + radius*ratio*float32(math.Sin(float64(angle)))}
	}
	// ECharts defaults to five radar split areas. Alternate the subtle
	// background tint and the transparent area exactly like splitArea.
	gridColor := Color{R: 0xdb / 255.0, G: 0xde / 255.0, B: 0xe4 / 255.0, A: 1}
	axisColor := Color{R: 0x6e / 255.0, G: 0x70 / 255.0, B: 0x79 / 255.0, A: 1}
	splitArea := Color{R: 0xea / 255.0, G: 0xed / 255.0, B: 0xf5 / 255.0, A: 0.5}
	for level := 1; level <= 5; level += 2 {
		path := NewPath()
		for i := 0; i < n; i++ {
			p := pointAt(i, float32(level)/5)
			if i == 0 {
				path.MoveTo(p.X, p.Y)
			} else {
				path.LineTo(p.X, p.Y)
			}
		}
		path.Close()
		if level > 1 {
			for i := n - 1; i >= 0; i-- {
				p := pointAt(i, float32(level-1)/5)
				if i == n-1 {
					path.MoveTo(p.X, p.Y)
				} else {
					path.LineTo(p.X, p.Y)
				}
			}
			path.Close()
		}
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: splitArea, AntiAlias: true, FillRule: FillEvenOdd})
	}
	for level := 1; level <= 5; level++ {
		path := NewPath()
		for i := 0; i < n; i++ {
			p := pointAt(i, float32(level)/5)
			if i == 0 {
				path.MoveTo(p.X, p.Y)
			} else {
				path.LineTo(p.X, p.Y)
			}
		}
		path.Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: gridColor, Style: PaintStroke, StrokeWidth: 1, AntiAlias: true})
	}
	axisLine := Color{R: 0xcf / 255.0, G: 0xd2 / 255.0, B: 0xd7 / 255.0, A: 1}
	for i := range s.Indicators {
		p := pointAt(i, 1)
		canvas.DrawLine(Point{X: cx, Y: cy}, p, axisLine, 1)
	}
	// Data polygon.
	path := NewPath()
	for i := 0; i < n; i++ {
		ratio := float32(0)
		if i < len(s.Values) && s.Indicators[i].Max > 0 {
			ratio = clampf(s.Values[i]/float32(s.Indicators[i].Max), 0, 1)
		}
		p := pointAt(i, ratio)
		if i == 0 {
			path.MoveTo(p.X, p.Y)
		} else {
			path.LineTo(p.X, p.Y)
		}
	}
	path.Close()
	if s.FillAlpha > 0 {
		fill := s.color
		fill.A *= s.FillAlpha
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: fill, AntiAlias: true})
	}
	width := s.StrokeWidth
	if width <= 0 {
		width = 2
	}
	canvas.DrawShape(ShapePath{Path: path}, Paint{Color: s.color, Style: PaintStroke, StrokeWidth: width, AntiAlias: true})
	for i := 0; i < n; i++ {
		ratio := float32(0)
		if i < len(s.Values) && s.Indicators[i].Max > 0 {
			ratio = clampf(s.Values[i]/float32(s.Indicators[i].Max), 0, 1)
		}
		vertex := pointAt(i, ratio)
		canvas.FillRoundedRect(Rect{X: vertex.X - 4, Y: vertex.Y - 4, W: 8, H: 8}, 4, s.color)
	}
	// Indicator labels.
	theme := CurrentTheme()
	font := Font{Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	for i, ind := range s.Indicators {
		angle := -float32(math.Pi)/2 - float32(i)*2*float32(math.Pi)/float32(n)
		p := Point{
			X: cx + (radius+15)*float32(math.Cos(float64(angle))),
			Y: cy + (radius+15)*float32(math.Sin(float64(angle))),
		}
		w := float32(len(ind.Name)) * font.Size * 0.55
		canvas.DrawText(ind.Name, Rect{X: p.X - w/2, Y: p.Y - font.Size/2 - 1, W: w, H: font.Size + 2}, axisColor, font)
	}
}

func (s *RadarSeries) HitTest(p Point, plot Rect, _ Axis, _ Axis) int {
	if len(s.Values) == 0 {
		return -1
	}
	// Hover follows the nearest normalized axis vertex.
	// ECharts positions a radar coordinate in the full chart viewport,
	// centered at 50%/50%, with its default radius set to 50% of half the
	// shorter viewport side.
	cx, cy := plot.X+plot.W/2, plot.Y+plot.H/2
	radius := minF32(plot.W, plot.H) * 0.25
	best, bestD := -1, float32(8*8)
	for i := range s.Values {
		angle := -float32(math.Pi)/2 - float32(i)*2*float32(math.Pi)/float32(len(s.Values))
		ratio := float32(0)
		if i < len(s.Indicators) && s.Indicators[i].Max > 0 {
			ratio = clampf(s.Values[i]/float32(s.Indicators[i].Max), 0, 1)
		}
		x := cx + radius*ratio*float32(math.Cos(float64(angle)))
		y := cy + radius*ratio*float32(math.Sin(float64(angle)))
		dx, dy := p.X-x, p.Y-y
		if d := dx*dx + dy*dy; d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

func (s *RadarSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 4, s.color)
}
