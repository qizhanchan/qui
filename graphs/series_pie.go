package graphs

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// PieLabelMode controls where pie slice labels are drawn.
type PieLabelMode int

const (
	PieLabelsOutside PieLabelMode = iota // label to the right of the slice
	PieLabelsNone                        // no labels (legend does the work)
)

// PieSlice is one wedge of the pie.
type PieSlice struct {
	Label string
	Value float32
	Color Color
}

// PieSeries is a radial chart — a single "series" by convention, but
// each slice is individually hit-testable. InnerRatio > 0 renders the
// donut shape (slices don't reach all the way to the center).
type PieSeries struct {
	seriesBase
	Slices     []PieSlice
	InnerRatio float32 // 0..1 — 0 is a solid pie
	// OuterRatio sizes the pie relative to min(plot.W, plot.H)/2. 0 or
	// negative falls back to pieDefaultOuterRatio; 1 would touch the
	// plot edge. The default leaves room for outside labels without
	// clipping against the legend or title.
	OuterRatio float32
	LabelMode  PieLabelMode
}

// pieDefaultOuterRatio is the fraction of min(plot.W, plot.H)/2 the
// pie's outer edge occupies by default. Previously 1/2.2 (~0.45);
// shrunk to 0.32 so outside labels have more room without clipping.
const pieDefaultOuterRatio = 0.32

// NewPieSeries returns a pie with outside-labels by default.
func NewPieSeries(name string, slices []PieSlice) *PieSeries {
	return &PieSeries{
		seriesBase: newSeriesBase(name),
		Slices:     slices,
		LabelMode:  PieLabelsOutside,
	}
}

// DataBounds for PieSeries is intentionally HasData=false — a pie
// occupies the whole plot area independent of axis scales, so the
// chart shouldn't try to auto-range axes from it.
func (s *PieSeries) DataBounds() SeriesBounds { return SeriesBounds{} }

// Draw paints each slice as a fan of narrow triangles (approximated
// with thin FillRoundedRect wedges via filled polylines). Since the
// Canvas doesn't have a native fill-polygon primitive, we emit thin
// radial "spokes" stepping across each slice's angle — dense enough
// to look solid at typical chart sizes.
func (s *PieSeries) Draw(canvas Canvas, plot Rect, _, _ Axis, hot HotSpot) {
	if len(s.Slices) == 0 {
		return
	}
	total := float32(0)
	for _, sl := range s.Slices {
		if sl.Value > 0 {
			total += sl.Value
		}
	}
	if total <= 0 {
		return
	}

	cx := plot.X + plot.W/2
	cy := plot.Y + plot.H/2
	outer := s.outerRadius(plot)
	inner := outer * s.InnerRatio

	angle := -float32(math.Pi) / 2 // start at 12 o'clock
	for i, slice := range s.Slices {
		if slice.Value <= 0 {
			continue
		}
		sweep := slice.Value / total * 2 * float32(math.Pi)
		color := slice.Color
		if isZeroColor(color) {
			color = s.color
		}
		radius := outer
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			radius *= 1.06 // "pop out" the hovered slice
		}
		fillPieSlice(canvas, cx, cy, inner, radius, angle, angle+sweep, color)
		angle += sweep
	}

	if s.LabelMode == PieLabelsOutside {
		s.drawSliceLabels(canvas, cx, cy, outer, total)
	}
}

// outerRadius computes the pie's outer radius in pixels. Consulted by
// both Draw and HitTest so they stay in sync. The previous hard-coded
// min(W,H)/2.2 maps to OuterRatio≈0.4545; the new default (0.32)
// leaves ~30% more padding for outside labels and the legend.
func (s *PieSeries) outerRadius(plot Rect) float32 {
	ratio := s.OuterRatio
	if ratio <= 0 {
		ratio = pieDefaultOuterRatio
	}
	return minF32(plot.W, plot.H) * ratio
}

// fillPieSlice approximates a filled pie slice with a dense fan of
// thin strokes from inner to outer radius. Step size adapts to slice
// size so big slices stay solid and small ones don't over-draw.
func fillPieSlice(canvas Canvas, cx, cy, inner, outer, start, end float32, color Color) {
	span := end - start
	if span <= 0 {
		return
	}
	arcLen := span * outer
	// ~0.5px angular step on the outer edge.
	steps := int(arcLen / 0.5)
	if steps < 4 {
		steps = 4
	}
	if steps > 2048 {
		steps = 2048
	}
	prevOuter := Point{
		X: cx + outer*float32(math.Cos(float64(start))),
		Y: cy + outer*float32(math.Sin(float64(start))),
	}
	for i := 1; i <= steps; i++ {
		a := start + span*float32(i)/float32(steps)
		cosA := float32(math.Cos(float64(a)))
		sinA := float32(math.Sin(float64(a)))
		curOuter := Point{X: cx + outer*cosA, Y: cy + outer*sinA}
		curInner := Point{X: cx + inner*cosA, Y: cy + inner*sinA}
		// Radial spokes of width = chord length step so slices fill solid.
		chord := distBetween(prevOuter, curOuter) + 1
		canvas.DrawLine(prevOuter, curInner, color, chord)
		prevOuter = curOuter
	}
}

func distBetween(a, b Point) float32 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

// drawSliceLabels places small labels outside each slice.
func (s *PieSeries) drawSliceLabels(canvas Canvas, cx, cy, outer, total float32) {
	theme := CurrentTheme()
	font := Font{Size: theme.FontSmall}
	if font.Size <= 0 {
		font.Size = 12
	}
	angle := -float32(math.Pi) / 2
	for _, slice := range s.Slices {
		if slice.Value <= 0 {
			continue
		}
		sweep := slice.Value / total * 2 * float32(math.Pi)
		mid := angle + sweep/2
		lx := cx + (outer+10)*float32(math.Cos(float64(mid)))
		ly := cy + (outer+10)*float32(math.Sin(float64(mid)))
		w := float32(len(slice.Label)) * font.Size * 0.55
		canvas.DrawText(slice.Label,
			Rect{X: lx - w/2, Y: ly - font.Size/2, W: w, H: font.Size + 2},
			theme.Text, font,
		)
		angle += sweep
	}
}

// HitTest returns the index of the slice whose wedge contains the
// point, or -1 when outside the pie / in the donut hole.
func (s *PieSeries) HitTest(p Point, plot Rect, _, _ Axis) int {
	if len(s.Slices) == 0 {
		return -1
	}
	cx := plot.X + plot.W/2
	cy := plot.Y + plot.H/2
	outer := s.outerRadius(plot)
	inner := outer * s.InnerRatio
	dx := p.X - cx
	dy := p.Y - cy
	dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
	if dist > outer || dist < inner {
		return -1
	}
	// Angle of p relative to center, measured clockwise from 12 o'clock
	// (to match Draw's starting angle).
	a := float32(math.Atan2(float64(dy), float64(dx)))
	// Normalize to [-pi/2, 3pi/2) so we count sweeps from the top.
	a -= -float32(math.Pi) / 2
	if a < 0 {
		a += 2 * float32(math.Pi)
	}
	total := float32(0)
	for _, sl := range s.Slices {
		if sl.Value > 0 {
			total += sl.Value
		}
	}
	if total <= 0 {
		return -1
	}
	acc := float32(0)
	for i, sl := range s.Slices {
		if sl.Value <= 0 {
			continue
		}
		sweep := sl.Value / total * 2 * float32(math.Pi)
		if a >= acc && a < acc+sweep {
			return i
		}
		acc += sweep
	}
	return -1
}

// LegendMarker renders a small filled square in the first slice's
// color; pie legends typically identify the series as a whole.
func (s *PieSeries) LegendMarker(canvas Canvas, swatch Rect) {
	color := s.color
	if len(s.Slices) > 0 && !isZeroColor(s.Slices[0].Color) {
		color = s.Slices[0].Color
	}
	canvas.FillRoundedRect(swatch, 2, color)
}
