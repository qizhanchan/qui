package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// PictorialBarSeries renders one symbol column per category. Common
// ECharts shapes are native: circle, roundRect, rect, diamond and triangle.
type PictorialBarSeries struct {
	seriesBase
	Values       []float32
	Symbol       string
	SymbolWidth  float32
	SymbolHeight float32
	Repeat       bool
	Margin       float32
	Clip         bool
	ItemColors   []Color
}

func NewPictorialBarSeries(name string, values []float32) *PictorialBarSeries {
	return &PictorialBarSeries{seriesBase: newSeriesBase(name), Values: values, Symbol: "circle"}
}

func (s *PictorialBarSeries) DataBounds() SeriesBounds {
	b := SeriesBounds{Xmin: 0, Xmax: float32(len(s.Values)), Ymin: 0, Ymax: 0, HasData: len(s.Values) > 0}
	for _, value := range s.Values {
		b.Ymin = minF32(b.Ymin, value)
		b.Ymax = maxF32(b.Ymax, value)
	}
	return b
}

func (s *PictorialBarSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	ca, ok := x.(*CategoryAxis)
	if !ok {
		return
	}
	bucket := ca.BucketWidth(plot.W)
	if bucket <= 0 {
		return
	}
	width, height := s.SymbolWidth, s.SymbolHeight
	if width <= 0 {
		width = bucket * .6
	}
	if height <= 0 {
		height = width
	}
	margin := s.Margin
	if margin < 0 {
		margin = 0
	}
	yZero := plot.Y + y.ValueToPixel(0, plot.H)
	for i, value := range s.Values {
		cx := plot.X + ca.ValueToPixel(float32(i), plot.W)
		valueY := plot.Y + y.ValueToPixel(value, plot.H)
		color := s.color
		if i < len(s.ItemColors) && !isZeroColor(s.ItemColors[i]) {
			color = s.ItemColors[i]
		}
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .12)
		}
		if !s.Repeat {
			centerY := valueY
			if value >= 0 {
				centerY = valueY - height/2
			} else {
				centerY = valueY + height/2
			}
			drawPictorialSymbol(canvas, s.Symbol, Point{X: cx, Y: centerY}, width, height, color)
			continue
		}
		dir := float32(-1)
		if value < 0 {
			dir = 1
		}
		stride := height + 2*margin
		for step := 0; ; step++ {
			centerY := yZero + dir*(height/2+float32(step)*stride)
			if (dir < 0 && centerY+height/2 < valueY) || (dir > 0 && centerY-height/2 > valueY) {
				break
			}
			symbolHeight := height
			if s.Clip {
				if dir < 0 && centerY-height/2 < valueY {
					symbolHeight = maxF32(0, centerY+height/2-valueY)
					centerY = valueY + symbolHeight/2
				} else if dir > 0 && centerY+height/2 > valueY {
					symbolHeight = maxF32(0, valueY-(centerY-height/2))
					centerY = valueY - symbolHeight/2
				}
				if symbolHeight <= 0 {
					break
				}
			}
			drawPictorialSymbol(canvas, s.Symbol, Point{X: cx, Y: centerY}, width, symbolHeight, color)
		}
	}
}

func drawPictorialSymbol(canvas Canvas, symbol string, center Point, width, height float32, color Color) {
	switch symbol {
	case "rect":
		canvas.FillRect(Rect{X: center.X - width/2, Y: center.Y - height/2, W: width, H: height}, color)
	case "roundRect":
		radius := minF32(width, height) * .25
		canvas.FillRoundedRect(Rect{X: center.X - width/2, Y: center.Y - height/2, W: width, H: height}, radius, color)
	case "diamond":
		path := NewPath().MoveTo(center.X, center.Y-height/2).LineTo(center.X+width/2, center.Y).LineTo(center.X, center.Y+height/2).LineTo(center.X-width/2, center.Y).Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
	case "triangle":
		path := NewPath().MoveTo(center.X, center.Y-height/2).LineTo(center.X+width/2, center.Y+height/2).LineTo(center.X-width/2, center.Y+height/2).Close()
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
	default:
		radius := minF32(width, height) * .25
		canvas.FillRoundedRect(Rect{X: center.X - width/2, Y: center.Y - height/2, W: width, H: height}, radius, color)
	}
}

func (s *PictorialBarSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	ca, ok := x.(*CategoryAxis)
	if !ok {
		return -1
	}
	bucket := ca.BucketWidth(plot.W)
	for i, value := range s.Values {
		cx := plot.X + ca.ValueToPixel(float32(i), plot.W)
		y0 := plot.Y + y.ValueToPixel(0, plot.H)
		y1 := plot.Y + y.ValueToPixel(value, plot.H)
		rect := Rect{X: cx - bucket*.3, Y: minF32(y0, y1), W: bucket * .6, H: float32(math.Abs(float64(y1 - y0)))}
		if rect.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *PictorialBarSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.color)
}
