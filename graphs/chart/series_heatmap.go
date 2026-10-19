package chart

import . "github.com/qizhanchan/qui"

// HeatmapPoint is one category-by-category cell.
type HeatmapPoint struct {
	X, Y  int
	Value float32
}

// HeatmapSeries renders a value matrix on two category axes. Values are
// mapped through an optional visualMap color ramp; without one the
// series color is used uniformly.
type HeatmapSeries struct {
	seriesBase
	Points []HeatmapPoint
	Min    float32
	Max    float32
	Colors []Color
}

func NewHeatmapSeries(name string, points []HeatmapPoint) *HeatmapSeries {
	return &HeatmapSeries{seriesBase: newSeriesBase(name), Points: points}
}

func (s *HeatmapSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

func (s *HeatmapSeries) valueExtent() (float32, float32) {
	lo, hi := s.Min, s.Max
	if hi > lo {
		return lo, hi
	}
	if len(s.Points) == 0 {
		return 0, 0
	}
	lo, hi = s.Points[0].Value, s.Points[0].Value
	for _, point := range s.Points[1:] {
		if point.Value < lo {
			lo = point.Value
		}
		if point.Value > hi {
			hi = point.Value
		}
	}
	return lo, hi
}

func (s *HeatmapSeries) colorFor(value float32) Color {
	lo, hi := s.valueExtent()
	t := float32(0)
	if hi > lo {
		t = clampf((value-lo)/(hi-lo), 0, 1)
	}
	if len(s.Colors) == 0 {
		if isZeroColor(s.color) {
			return Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}
		}
		return s.color
	}
	if len(s.Colors) == 1 {
		return s.Colors[0]
	}
	pos := t * float32(len(s.Colors)-1)
	i := int(pos)
	if i >= len(s.Colors)-1 {
		return s.Colors[len(s.Colors)-1]
	}
	return mixColor(s.Colors[i], s.Colors[i+1], pos-float32(i))
}

func (s *HeatmapSeries) cellRects(plot Rect, x, y Axis) []Rect {
	xa, okX := x.(*CategoryAxis)
	ya, okY := y.(*CategoryAxis)
	if !okX || !okY {
		return nil
	}
	width := xa.BucketWidth(plot.W) + .5
	height := ya.BucketWidth(plot.H) + .5
	if width <= 0 || height <= 0 {
		return nil
	}
	rects := make([]Rect, len(s.Points))
	for i, point := range s.Points {
		cx := plot.X + xa.ValueToPixel(float32(point.X), plot.W)
		cy := plot.Y + ya.ValueToPixel(float32(point.Y), plot.H)
		rects[i] = Rect{X: cx - width/2, Y: cy - height/2, W: width, H: height}
	}
	return rects
}

func (s *HeatmapSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	rects := s.cellRects(plot, x, y)
	for i, rect := range rects {
		color := s.colorFor(s.Points[i].Value)
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			color = mixColor(color, Color{R: 1, G: 1, B: 1, A: 1}, .15)
		}
		canvas.FillRect(rect, color)
	}
}

func (s *HeatmapSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	for i, rect := range s.cellRects(plot, x, y) {
		if rect.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *HeatmapSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.colorFor(s.valueExtentMiddle()))
}

func (s *HeatmapSeries) valueExtentMiddle() float32 {
	lo, hi := s.valueExtent()
	return lo + (hi-lo)/2
}
