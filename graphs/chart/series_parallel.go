package chart

import (
	"math"

	. "github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs"
)

// ParallelSeries draws one polyline per data row across a set of vertical
// parallel axes. Axis extents auto-range from the visible rows unless
// explicit Min/Max values are supplied.
type ParallelSeries struct {
	seriesBase
	Items      [][]float32
	Axes       []AxisOption
	Smooth     bool
	FontFamily string
}

func NewParallelSeries(name string, items [][]float32) *ParallelSeries {
	return &ParallelSeries{seriesBase: newSeriesBase(name), Items: items}
}

func (s *ParallelSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

type parallelAxisLayout struct {
	name   string
	x      float32
	lo, hi float32
	ticks  []graphs.Tick
}

func (s *ParallelSeries) axisLayouts(bounds Rect) []parallelAxisLayout {
	area := Rect{X: bounds.X + 80, Y: bounds.Y + 60, W: bounds.W - 160, H: bounds.H - 120}
	if area.W <= 0 || area.H <= 0 || len(s.Axes) == 0 {
		return nil
	}
	axes := make([]parallelAxisLayout, len(s.Axes))
	for i, option := range s.Axes {
		x := area.X
		if len(s.Axes) > 1 {
			x += area.W * float32(i) / float32(len(s.Axes)-1)
		}
		lo, hi := float32(math.Inf(1)), float32(-math.Inf(1))
		for _, row := range s.Items {
			if i < len(row) {
				lo = minF32(lo, row[i])
				hi = maxF32(hi, row[i])
			}
		}
		if math.IsInf(float64(lo), 1) {
			lo, hi = 0, 1
		}
		if option.Min != nil {
			lo = float32(*option.Min)
		}
		if option.Max != nil {
			hi = float32(*option.Max)
		}
		if hi <= lo {
			if hi == lo {
				lo -= 1
				hi += 1
			} else {
				lo, hi = 0, 1
			}
		}
		ticks := graphs.NiceLinearTicks(float64(lo), float64(hi), 5)
		if len(ticks) > 1 {
			lo = float32(ticks[0].Value)
			hi = float32(ticks[len(ticks)-1].Value)
		}
		axes[i] = parallelAxisLayout{name: option.Name, x: x, lo: lo, hi: hi, ticks: ticks}
	}
	return axes
}

func (s *ParallelSeries) yFor(axis parallelAxisLayout, value float32, bounds Rect) float32 {
	areaTop, areaH := bounds.Y+60, bounds.H-120
	t := (value - axis.lo) / (axis.hi - axis.lo)
	return areaTop + (1-clampf(t, 0, 1))*areaH
}

func (s *ParallelSeries) Draw(canvas Canvas, bounds Rect, _, _ Axis, hot HotSpot) {
	axes := s.axisLayouts(bounds)
	if len(axes) == 0 {
		return
	}
	axisColor := Color{R: 0x6e / 255.0, G: 0x70 / 255.0, B: 0x79 / 255.0, A: 1}
	theme := CurrentTheme()
	font := Font{Family: s.FontFamily, Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	areaTop, areaBottom := bounds.Y+60, bounds.Y+bounds.H-60
	for _, axis := range axes {
		canvas.DrawLine(Point{X: axis.x, Y: areaTop}, Point{X: axis.x, Y: areaBottom}, axisColor, 1)
		w, _ := TextMetrics(axis.name, font)
		if w <= 0 {
			w = float32(len(axis.name)) * font.Size * .55
		}
		canvas.DrawText(axis.name, Rect{X: axis.x - w/2, Y: areaTop - font.Size - 16, W: w, H: font.Size + 2}, axisColor, font)
		for _, tick := range axis.ticks {
			y := s.yFor(axis, float32(tick.Value), bounds)
			canvas.DrawLine(Point{X: axis.x, Y: y}, Point{X: axis.x + 5, Y: y}, axisColor, 1)
			label := tick.Label
			lw, _ := TextMetrics(label, font)
			if lw <= 0 {
				lw = float32(len(label)) * font.Size * .55
			}
			canvas.DrawText(label, Rect{X: axis.x + 8, Y: y - font.Size/2 - 3, W: lw, H: font.Size + 2}, axisColor, font)
		}
	}
	lineColor := s.color
	if isZeroColor(lineColor) {
		lineColor = Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}
	}
	lineColor.A *= .45
	for rowIndex, row := range s.Items {
		points := make([]Point, 0, len(axes))
		for i, axis := range axes {
			if i >= len(row) {
				break
			}
			points = append(points, Point{X: axis.x, Y: s.yFor(axis, row[i], bounds)})
		}
		if len(points) < 2 {
			continue
		}
		color := lineColor
		if hot.PointIdx == rowIndex && hot.SeriesIdx >= 0 {
			color.A = .9
		}
		if s.Smooth {
			path := NewPath().MoveTo(points[0].X, points[0].Y)
			for i := 1; i < len(points); i++ {
				midX := (points[i-1].X + points[i].X) / 2
				path.CubicTo(midX, points[i-1].Y, midX, points[i].Y, points[i].X, points[i].Y)
			}
			canvas.DrawShape(ShapePath{Path: path}, Paint{Color: color, Style: PaintStroke, StrokeWidth: 1, AntiAlias: true})
		} else {
			canvas.DrawPolyline(points, color, 1)
		}
	}
}

func (s *ParallelSeries) HitTest(p Point, bounds Rect, _, _ Axis) int {
	axes := s.axisLayouts(bounds)
	if len(axes) == 0 {
		return -1
	}
	best, bestDist := -1, float32(16)
	for rowIndex, row := range s.Items {
		for i := 1; i < len(axes) && i < len(row); i++ {
			a := Point{X: axes[i-1].x, Y: s.yFor(axes[i-1], row[i-1], bounds)}
			b := Point{X: axes[i].x, Y: s.yFor(axes[i], row[i], bounds)}
			d := pointSegmentDistance(p, a, b)
			if d < bestDist {
				bestDist = d
				best = rowIndex
			}
		}
	}
	return best
}

func pointSegmentDistance(p, a, b Point) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	if dx == 0 && dy == 0 {
		ddx, ddy := p.X-a.X, p.Y-a.Y
		return float32(math.Sqrt(float64(ddx*ddx + ddy*ddy)))
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / (dx*dx + dy*dy)
	t = clampf(t, 0, 1)
	x, y := a.X+t*dx, a.Y+t*dy
	ddx, ddy := p.X-x, p.Y-y
	return float32(math.Sqrt(float64(ddx*ddx + ddy*ddy)))
}

func (s *ParallelSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.color)
}
