package chart

import . "github.com/qizhanchan/qui"

// BoxplotPoint is a five-number summary: [min, Q1, median, Q3, max].
type BoxplotPoint struct {
	Min, Q1, Median, Q3, Max float32
}

// BoxplotSeries renders one box per category on a category/value
// cartesian pair. It follows ECharts defaults: a white body outlined in
// the series color, a median line, and min/max whisker caps.
type BoxplotSeries struct {
	seriesBase
	Points []BoxplotPoint
	// GroupOffset/GroupTotal align multiple boxplot series within one
	// category. Chart populates them during layout.
	GroupOffset int
	GroupTotal  int
}

func NewBoxplotSeries(name string, points []BoxplotPoint) *BoxplotSeries {
	return &BoxplotSeries{seriesBase: newSeriesBase(name), Points: points, GroupTotal: 1}
}

func (s *BoxplotSeries) DataBounds() SeriesBounds {
	if len(s.Points) == 0 {
		return SeriesBounds{}
	}
	b := SeriesBounds{Xmin: 0, Xmax: float32(len(s.Points)), Ymin: s.Points[0].Min, Ymax: s.Points[0].Max, HasData: true}
	for i, point := range s.Points[1:] {
		_ = i
		if point.Min < b.Ymin {
			b.Ymin = point.Min
		}
		if point.Max > b.Ymax {
			b.Ymax = point.Max
		}
	}
	return b
}

func (s *BoxplotSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	ca, ok := x.(*CategoryAxis)
	if !ok || len(s.Points) == 0 {
		return
	}
	band := ca.BucketWidth(plot.W)
	if band <= 0 {
		return
	}
	width, offset := s.boxGeometry(band)
	color := s.color
	if isZeroColor(color) {
		color = Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}
	}
	fill := Color{R: 1, G: 1, B: 1, A: 1}
	for i, point := range s.Points {
		cx := plot.X + ca.ValueToPixel(float32(i), plot.W) + offset
		half := width / 2
		yMin := plot.Y + y.ValueToPixel(point.Min, plot.H)
		yQ1 := plot.Y + y.ValueToPixel(point.Q1, plot.H)
		yMed := plot.Y + y.ValueToPixel(point.Median, plot.H)
		yQ3 := plot.Y + y.ValueToPixel(point.Q3, plot.H)
		yMax := plot.Y + y.ValueToPixel(point.Max, plot.H)
		stroke := color
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			stroke = mixColor(stroke, Color{R: 0, G: 0, B: 0, A: 1}, .18)
		}
		box := Rect{X: cx - half, Y: minF32(yQ1, yQ3), W: width, H: absF32(yQ3 - yQ1)}
		canvas.DrawShape(ShapePath{Path: NewPath().AddRect(box)}, Paint{Color: fill, AntiAlias: true})
		canvas.DrawShape(ShapePath{Path: NewPath().AddRect(box)}, Paint{Color: stroke, Style: PaintStroke, StrokeWidth: 1, AntiAlias: true})
		canvas.DrawLine(Point{X: cx, Y: yMax}, Point{X: cx, Y: yQ3}, stroke, 1)
		canvas.DrawLine(Point{X: cx, Y: yQ1}, Point{X: cx, Y: yMin}, stroke, 1)
		canvas.DrawLine(Point{X: cx - half, Y: yMed}, Point{X: cx + half, Y: yMed}, stroke, 1)
		canvas.DrawLine(Point{X: cx - half, Y: yMax}, Point{X: cx + half, Y: yMax}, stroke, 1)
		canvas.DrawLine(Point{X: cx - half, Y: yMin}, Point{X: cx + half, Y: yMin}, stroke, 1)
	}
}

func (s *BoxplotSeries) boxGeometry(band float32) (float32, float32) {
	total := s.GroupTotal
	if total <= 0 {
		total = 1
	}
	available := band*.8 - 2
	if available < 0 {
		available = 0
	}
	gap := available / float32(total) * .3
	width := (available - gap*float32(total-1)) / float32(total)
	offset := width/2 - available/2 + float32(s.GroupOffset)*(width+gap)
	// ECharts clamps box width to [7px, 50% of the category band].
	maxWidth := band * .52
	if width > maxWidth {
		width = maxWidth
	}
	if width < 7 && band >= 7 {
		width = 7
	}
	return width, offset
}

func (s *BoxplotSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	ca, ok := x.(*CategoryAxis)
	if !ok {
		return -1
	}
	band := ca.BucketWidth(plot.W)
	width, offset := s.boxGeometry(band)
	for i, point := range s.Points {
		cx := plot.X + ca.ValueToPixel(float32(i), plot.W) + offset
		y0 := plot.Y + y.ValueToPixel(point.Max, plot.H)
		y1 := plot.Y + y.ValueToPixel(point.Min, plot.H)
		rect := Rect{X: cx - width/2, Y: minF32(y0, y1), W: width, H: absF32(y1 - y0)}
		if rect.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *BoxplotSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.color)
}
