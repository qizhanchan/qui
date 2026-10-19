package chart

import . "github.com/qizhanchan/qui"

// CandlestickPoint is one OHLC sample.
type CandlestickPoint struct {
	Open, Close, Low, High float32
}

// CandlestickSeries renders financial OHLC bodies and wicks against a
// CategoryAxis. ECharts' default red-up / green-down palette is used
// unless explicitly overridden.
type CandlestickSeries struct {
	seriesBase
	Points             []CandlestickPoint
	WidthRatio         float32
	UpColor, DownColor Color
}

func NewCandlestickSeries(name string, points []CandlestickPoint) *CandlestickSeries {
	return &CandlestickSeries{
		seriesBase: newSeriesBase(name),
		Points:     points,
		WidthRatio: 0.5,
		UpColor:    Color{R: 0xeb / 255.0, G: 0x54 / 255.0, B: 0x54 / 255.0, A: 1},
		DownColor:  Color{R: 0x47 / 255.0, G: 0xb2 / 255.0, B: 0x62 / 255.0, A: 1},
	}
}

func (s *CandlestickSeries) DataBounds() SeriesBounds {
	if len(s.Points) == 0 {
		return SeriesBounds{}
	}
	b := SeriesBounds{Xmin: 0, Xmax: float32(len(s.Points)), Ymin: s.Points[0].Low, Ymax: s.Points[0].High, HasData: true}
	for _, p := range s.Points[1:] {
		if p.Low < b.Ymin {
			b.Ymin = p.Low
		}
		if p.High > b.Ymax {
			b.Ymax = p.High
		}
	}
	return b
}

func (s *CandlestickSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	ca, ok := x.(*CategoryAxis)
	if !ok || len(s.Points) == 0 {
		return
	}
	bucket := ca.BucketWidth(plot.W)
	ratio := s.WidthRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 0.7
	}
	bodyW := bucket * ratio
	for i, p := range s.Points {
		cx := plot.X + x.ValueToPixel(float32(i), plot.W)
		high := plot.Y + y.ValueToPixel(p.High, plot.H)
		low := plot.Y + y.ValueToPixel(p.Low, plot.H)
		open := plot.Y + y.ValueToPixel(p.Open, plot.H)
		close := plot.Y + y.ValueToPixel(p.Close, plot.H)
		up := p.Close >= p.Open
		color := s.DownColor
		if up {
			color = s.UpColor
		}
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color.A = clampf(color.A*1.12, 0, 1)
		}
		canvas.DrawLine(Point{X: cx, Y: high}, Point{X: cx, Y: low}, color, 1)
		top, bottom := minF32(open, close), maxF32(open, close)
		if bottom-top < 1 {
			bottom = top + 1
		}
		body := Rect{X: cx - bodyW/2, Y: top, W: bodyW, H: bottom - top}
		path := NewPath().AddRect(body)
		canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, AntiAlias: true})
	}
}

func (s *CandlestickSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	ca, ok := x.(*CategoryAxis)
	if !ok || len(s.Points) == 0 {
		return -1
	}
	bucket := ca.BucketWidth(plot.W)
	for i, c := range s.Points {
		cx := plot.X + x.ValueToPixel(float32(i), plot.W)
		high := plot.Y + y.ValueToPixel(c.High, plot.H)
		low := plot.Y + y.ValueToPixel(c.Low, plot.H)
		if p.X >= cx-bucket/2 && p.X <= cx+bucket/2 && p.Y >= high-2 && p.Y <= low+2 {
			return i
		}
	}
	return -1
}

func (s *CandlestickSeries) LegendMarker(canvas Canvas, swatch Rect) {
	half := swatch.W / 2
	canvas.FillRoundedRect(Rect{X: swatch.X, Y: swatch.Y, W: half - 1, H: swatch.H}, 1, s.UpColor)
	canvas.FillRoundedRect(Rect{X: swatch.X + half + 1, Y: swatch.Y, W: half - 1, H: swatch.H}, 1, s.DownColor)
}
