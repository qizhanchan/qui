package graphs

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// LineSeries draws an antialiased polyline connecting successive points
// in Points. When ShowMarkers is true, a small filled circle is drawn
// at each data point — useful when the data is sparse enough that the
// line alone doesn't show individual samples.
type LineSeries struct {
	seriesBase
	Points      []Point2D
	Stroke      float32 // line width in logical pixels; default 2
	ShowMarkers bool
	MarkerSize  float32 // marker diameter; default 5
	// Smooth renders a Catmull-Rom spline (converted to cubic Béziers)
	// through the data points instead of straight segments. HitTest and
	// markers still operate on the raw data points — only the stroke path
	// is interpolated.
	Smooth bool
	// SmoothSteps controls the samples per segment when Smooth is true;
	// 0 or negative falls back to 16, which is smooth enough for a
	// few-hundred-point series without pathological overdraw.
	SmoothSteps int
}

// NewLineSeries returns a series with sensible defaults.
func NewLineSeries(name string, points []Point2D) *LineSeries {
	return &LineSeries{
		seriesBase: newSeriesBase(name),
		Points:     points,
		Stroke:     2,
		MarkerSize: 5,
	}
}

func (s *LineSeries) DataBounds() SeriesBounds { return boundsFromPoints(s.Points) }

// Draw projects each data point to the plot rectangle and emits a
// single polyline. Marker highlight: if the hot index matches one of
// our points, that marker is painted with a larger halo.
func (s *LineSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	if len(s.Points) == 0 {
		return
	}
	pts := s.projectPoints(plot, x, y)
	width := s.Stroke
	if width <= 0 {
		width = 2
	}
	path := pts
	if s.Smooth && len(pts) >= 2 {
		steps := s.SmoothSteps
		if steps <= 0 {
			steps = 16
		}
		path = catmullRomPolyline(pts, steps)
	}
	canvas.DrawPolyline(path, s.color, width)
	if s.ShowMarkers || s.MarkerSize > 0 {
		size := s.MarkerSize
		if size <= 0 {
			size = 5
		}
		for i, p := range pts {
			r := size
			if hot.PointIdx == i && hot.SeriesIdx >= 0 {
				r = size + 3 // halo for hover highlight
			}
			canvas.FillRoundedRect(Rect{X: p.X - r/2, Y: p.Y - r/2, W: r, H: r}, r/2, s.color)
		}
	}
}

// HitTest finds the nearest data point within a modest pixel radius.
// Returns -1 when nothing is close enough.
func (s *LineSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	if len(s.Points) == 0 {
		return -1
	}
	const hitRadius = 6.0
	pts := s.projectPoints(plot, x, y)
	bestIdx := -1
	bestDistSq := float32(hitRadius * hitRadius)
	for i, pt := range pts {
		dx := p.X - pt.X
		dy := p.Y - pt.Y
		d := dx*dx + dy*dy
		if d < bestDistSq {
			bestDistSq = d
			bestIdx = i
		}
	}
	return bestIdx
}

// LegendMarker draws a short stroke of the series color so the legend
// swatch visually matches the line style.
func (s *LineSeries) LegendMarker(canvas Canvas, swatch Rect) {
	mid := swatch.Y + swatch.H/2
	width := s.Stroke
	if width <= 0 {
		width = 2
	}
	canvas.DrawLine(
		Point{X: swatch.X, Y: mid},
		Point{X: swatch.X + swatch.W, Y: mid},
		s.color, width,
	)
}

// projectPoints converts data points to pixel coordinates inside plot.
// Points outside the visible range are still projected (they'll be
// clipped by whatever ClipCanvas wraps the plot area).
func (s *LineSeries) projectPoints(plot Rect, x, y Axis) []Point {
	out := make([]Point, len(s.Points))
	for i, p := range s.Points {
		out[i] = Point{
			X: plot.X + x.ValueToPixel(p.X, plot.W),
			Y: plot.Y + y.ValueToPixel(p.Y, plot.H),
		}
	}
	return out
}

// catmullRomPolyline tessellates a uniform Catmull-Rom spline passing
// through every input point into a dense polyline. Endpoint tangents
// clamp to the neighboring points (duplicated virtual control points),
// which gives a visually-natural curve that still touches pts[0] and
// pts[len-1] exactly. steps is samples per segment (>= 1).
func catmullRomPolyline(pts []Point, steps int) []Point {
	n := len(pts)
	if n < 3 {
		return pts
	}
	if steps < 1 {
		steps = 1
	}
	out := make([]Point, 0, (n-1)*steps+1)
	out = append(out, pts[0])
	for i := 0; i < n-1; i++ {
		p0 := pts[max(i-1, 0)]
		p1 := pts[i]
		p2 := pts[i+1]
		p3 := pts[min(i+2, n-1)]
		// Catmull-Rom → cubic Bézier controls (tension 0.5 / tau=1).
		c1 := Point{X: p1.X + (p2.X-p0.X)/6, Y: p1.Y + (p2.Y-p0.Y)/6}
		c2 := Point{X: p2.X - (p3.X-p1.X)/6, Y: p2.Y - (p3.Y-p1.Y)/6}
		for k := 1; k <= steps; k++ {
			t := float32(k) / float32(steps)
			out = append(out, cubicBezier(p1, c1, c2, p2, t))
		}
	}
	return out
}

func cubicBezier(p0, p1, p2, p3 Point, t float32) Point {
	u := 1 - t
	b0 := u * u * u
	b1 := 3 * u * u * t
	b2 := 3 * u * t * t
	b3 := t * t * t
	return Point{
		X: b0*p0.X + b1*p1.X + b2*p2.X + b3*p3.X,
		Y: b0*p0.Y + b1*p1.Y + b2*p2.Y + b3*p3.Y,
	}
}

// clampf is a small helper used by other series files — defined here
// to avoid a separate utils file.
func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// minF32 / maxF32 are float32 min/max; Go's stdlib min/max generics
// need 1.21+ and the repo uses float32 helpers elsewhere (minF/maxF in
// geometry.go are package-private to qui).
func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// absF32 is unused in this file but referenced by future series; keep
// a small surface of helpers co-located here so wave-2 additions don't
// introduce new files.
func absF32(v float32) float32 {
	return float32(math.Abs(float64(v)))
}
