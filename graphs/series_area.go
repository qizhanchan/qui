package graphs

import . "github.com/qizhanchan/qui"

// AreaSeries draws a filled polygon between the data line and a
// horizontal baseline (Y=Baseline). An optional stroke on the upper
// edge matches LineSeries for callers who want a "line + soft fill"
// look common in time-series dashboards.
type AreaSeries struct {
	seriesBase
	Points   []Point2D
	Baseline float32 // default 0 — the Y value the fill falls to
	Stroke   float32 // upper-edge stroke width; 0 = no stroke
	// FillAlpha (0..1) is applied to Color when producing the fill
	// color; 0 falls back to 0.35 (good default for overlays).
	FillAlpha float32
}

// NewAreaSeries returns a series filled to Y=0 with a 35%-alpha fill.
func NewAreaSeries(name string, points []Point2D) *AreaSeries {
	return &AreaSeries{
		seriesBase: newSeriesBase(name),
		Points:     points,
		Stroke:     2,
		FillAlpha:  0.35,
	}
}

func (s *AreaSeries) DataBounds() SeriesBounds {
	b := boundsFromPoints(s.Points)
	if !b.HasData {
		return b
	}
	// Ensure baseline is visible so the fill has something to fall to.
	if s.Baseline < b.Ymin {
		b.Ymin = s.Baseline
	}
	if s.Baseline > b.Ymax {
		b.Ymax = s.Baseline
	}
	return b
}

// Draw emits two passes: (1) a filled polygon approximating the area
// via stacked vertical 1px-wide FillRect strips between the line and
// baseline — this works without a polygon-fill primitive; (2) an
// optional stroke along the upper edge.
func (s *AreaSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	if len(s.Points) < 2 {
		return
	}
	fill := s.color
	alpha := s.FillAlpha
	if alpha <= 0 {
		alpha = 0.35
	}
	fill.A *= alpha

	// Project each data point, pairing with baseline Y pixel at the same X.
	baselineY := plot.Y + y.ValueToPixel(s.Baseline, plot.H)
	pts := make([]Point, len(s.Points))
	for i, p := range s.Points {
		pts[i] = Point{
			X: plot.X + x.ValueToPixel(p.X, plot.W),
			Y: plot.Y + y.ValueToPixel(p.Y, plot.H),
		}
	}

	// Paint fill as a run of 1px-wide rectangles between each segment's
	// interpolated Y and baselineY. This is O(pixelsWide × 1) per
	// segment, plenty fast for chart densities we expect in v1.
	for i := 1; i < len(pts); i++ {
		a := pts[i-1]
		b := pts[i]
		// Skip degenerate (same-X) segments — the fill column would be 0w.
		if a.X == b.X {
			continue
		}
		xStart := a.X
		xEnd := b.X
		if xEnd < xStart {
			xStart, xEnd = xEnd, xStart
		}
		dx := b.X - a.X
		for px := xStart; px <= xEnd; px++ {
			t := (px - a.X) / dx
			yOnLine := a.Y + t*(b.Y-a.Y)
			top := minF32(yOnLine, baselineY)
			bot := maxF32(yOnLine, baselineY)
			canvas.FillRect(Rect{X: px, Y: top, W: 1, H: bot - top}, fill)
		}
	}

	if s.Stroke > 0 {
		canvas.DrawPolyline(pts, s.color, s.Stroke)
	}
	// Reuse the hot marker halo pattern from LineSeries.
	if hot.SeriesIdx >= 0 && hot.PointIdx >= 0 && hot.PointIdx < len(pts) {
		p := pts[hot.PointIdx]
		r := s.Stroke + 3
		canvas.FillRoundedRect(Rect{X: p.X - r/2, Y: p.Y - r/2, W: r, H: r}, r/2, s.color)
	}
}

func (s *AreaSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	const hitRadius = 6.0
	best := -1
	bestD := float32(hitRadius * hitRadius)
	for i, pt := range s.Points {
		cx := plot.X + x.ValueToPixel(pt.X, plot.W)
		cy := plot.Y + y.ValueToPixel(pt.Y, plot.H)
		dx := p.X - cx
		dy := p.Y - cy
		d := dx*dx + dy*dy
		if d < bestD {
			bestD = d
			best = i
		}
	}
	return best
}

// LegendMarker paints a swatch: filled rectangle with stroke line on top.
func (s *AreaSeries) LegendMarker(canvas Canvas, swatch Rect) {
	fill := s.color
	alpha := s.FillAlpha
	if alpha <= 0 {
		alpha = 0.35
	}
	fill.A *= alpha
	canvas.FillRect(swatch, fill)
	if s.Stroke > 0 {
		mid := swatch.Y + swatch.H*0.35
		canvas.DrawLine(
			Point{X: swatch.X, Y: mid},
			Point{X: swatch.X + swatch.W, Y: mid},
			s.color, s.Stroke,
		)
	}
}
