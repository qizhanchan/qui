package graphs

import . "github.com/qizhanchan/qui"

// MarkerShape selects the scatter marker glyph. Circle is default;
// square and diamond are cheap variants that distinguish overlapping
// series without needing per-point color variation.
type MarkerShape int

const (
	MarkerCircle MarkerShape = iota
	MarkerSquare
	MarkerDiamond
)

// ScatterSeries draws one marker per data point. Unlike LineSeries,
// markers are opaque and do not connect — ideal for correlation /
// cluster plots.
type ScatterSeries struct {
	seriesBase
	Points []Point2D
	Size   float32     // marker diameter in logical pixels; default 6
	Shape  MarkerShape // default MarkerCircle
}

// NewScatterSeries constructs a ScatterSeries with sensible defaults.
func NewScatterSeries(name string, points []Point2D) *ScatterSeries {
	return &ScatterSeries{
		seriesBase: newSeriesBase(name),
		Points:     points,
		Size:       6,
		Shape:      MarkerCircle,
	}
}

func (s *ScatterSeries) DataBounds() SeriesBounds { return boundsFromPoints(s.Points) }

// Draw paints each point with the configured marker shape. Hot point
// gets a small halo ring to make it pop.
func (s *ScatterSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	if len(s.Points) == 0 {
		return
	}
	size := s.Size
	if size <= 0 {
		size = 6
	}
	for i, p := range s.Points {
		cx := plot.X + x.ValueToPixel(p.X, plot.W)
		cy := plot.Y + y.ValueToPixel(p.Y, plot.H)
		r := size
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			r = size + 4
		}
		drawMarker(canvas, s.Shape, Point{X: cx, Y: cy}, r, s.color)
	}
}

// HitTest picks the closest marker within its own radius.
func (s *ScatterSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	size := s.Size
	if size <= 0 {
		size = 6
	}
	radius := size/2 + 2
	radiusSq := radius * radius
	best := -1
	bestDistSq := radiusSq
	for i, pt := range s.Points {
		cx := plot.X + x.ValueToPixel(pt.X, plot.W)
		cy := plot.Y + y.ValueToPixel(pt.Y, plot.H)
		dx := p.X - cx
		dy := p.Y - cy
		d := dx*dx + dy*dy
		if d < bestDistSq {
			bestDistSq = d
			best = i
		}
	}
	return best
}

func (s *ScatterSeries) LegendMarker(canvas Canvas, swatch Rect) {
	cx := swatch.X + swatch.W/2
	cy := swatch.Y + swatch.H/2
	drawMarker(canvas, s.Shape, Point{X: cx, Y: cy}, minF32(swatch.W, swatch.H)*0.8, s.color)
}

// drawMarker emits the shape primitive. Circles use FillRoundedRect
// with full-radius, squares use FillRect, diamonds use a 2-segment
// polygon approximation (4 triangles — cheap-and-dirty).
func drawMarker(canvas Canvas, shape MarkerShape, center Point, size float32, color Color) {
	half := size / 2
	switch shape {
	case MarkerSquare:
		canvas.FillRect(Rect{X: center.X - half, Y: center.Y - half, W: size, H: size}, color)
	case MarkerDiamond:
		// Approximate with a 4-wide filled polyline: thick stroke whose
		// endpoints describe the diamond outline. The polyline fills the
		// diamond because consecutive edges overlap (diamond fits in the
		// width of the stroke). Not exact, but cheap and visually ok.
		pts := []Point{
			{X: center.X, Y: center.Y - half},
			{X: center.X + half, Y: center.Y},
			{X: center.X, Y: center.Y + half},
			{X: center.X - half, Y: center.Y},
			{X: center.X, Y: center.Y - half},
		}
		canvas.DrawPolyline(pts, color, size)
	default:
		canvas.FillRoundedRect(Rect{X: center.X - half, Y: center.Y - half, W: size, H: size}, half, color)
	}
}
