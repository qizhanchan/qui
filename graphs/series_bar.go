package graphs

import . "github.com/qizhanchan/qui"

// BarSeries renders vertical bars against a CategoryAxis on X and a
// ValueAxis on Y. Two composition modes:
//
//   - grouped (Stacked=false): bars from different series in the same
//     category are laid side-by-side, sharing the bucket width.
//   - stacked (Stacked=true): bars stack on top of each other, each
//     starting where the previous stacked series in the same category
//     ended.
//
// Stacking mode is per-series so a chart can mix grouped and stacked
// bars when it makes sense. GroupOffset and StackBase are populated
// by the Chart before Draw — not set by callers.
type BarSeries struct {
	seriesBase
	Values []float32 // one value per category slot

	Stacked bool
	// GroupOffset is the number of grouped (non-stacked) bar series
	// that come BEFORE this one in the chart. Used to compute X offset
	// within a bucket. Filled in by Chart during Draw.
	GroupOffset int
	// GroupTotal is the count of grouped series in the chart. Combined
	// with GroupOffset to partition a bucket into equal-width slots.
	GroupTotal int
	// StackBase, when Stacked==true, is the Y value the bar is drawn ON
	// TOP of. Computed per-category by Chart during Draw.
	StackBase []float32
}

// NewBarSeries returns a grouped bar series with sensible defaults.
func NewBarSeries(name string, values []float32) *BarSeries {
	return &BarSeries{
		seriesBase: newSeriesBase(name),
		Values:     values,
		GroupTotal: 1,
	}
}

// DataBounds reports category-space X (0..N) and Y range over values.
// Bars always include 0 in their Y range so the baseline is visible.
func (s *BarSeries) DataBounds() SeriesBounds {
	if len(s.Values) == 0 {
		return SeriesBounds{}
	}
	b := SeriesBounds{
		Xmin: 0, Xmax: float32(len(s.Values)),
		Ymin: 0, Ymax: 0, HasData: true,
	}
	for _, v := range s.Values {
		if v < b.Ymin {
			b.Ymin = v
		}
		if v > b.Ymax {
			b.Ymax = v
		}
	}
	return b
}

// Draw paints each bar. For grouped bars, the bucket width is split
// evenly across GroupTotal groups with a small gap between them.
// For stacked bars, the bar starts at StackBase[i] and extends by
// Values[i] along the Y axis.
func (s *BarSeries) Draw(canvas Canvas, plot Rect, x, y Axis, hot HotSpot) {
	if len(s.Values) == 0 {
		return
	}
	ca, ok := x.(*CategoryAxis)
	if !ok {
		return
	}
	bucket := ca.BucketWidth(plot.W)
	if bucket <= 0 {
		return
	}
	padding := ca.GroupPadding
	if padding < 0 {
		padding = 0
	}
	if padding > 0.9 {
		padding = 0.9
	}
	// Inside the bucket, reserve padding fraction as total gap, split
	// across GroupTotal-1 inter-group gaps for grouped mode.
	groupTotal := s.GroupTotal
	if groupTotal <= 0 {
		groupTotal = 1
	}
	innerW := bucket * (1 - padding)
	var barW float32
	if s.Stacked {
		barW = innerW
	} else {
		barW = innerW / float32(groupTotal)
	}

	yZero := plot.Y + y.ValueToPixel(0, plot.H)
	for i, v := range s.Values {
		bucketX := plot.X + ca.ValueToPixel(float32(i), plot.W) - bucket/2
		base := float32(0)
		if s.Stacked && i < len(s.StackBase) {
			base = s.StackBase[i]
		}
		top := base + v
		topPx := plot.Y + y.ValueToPixel(top, plot.H)
		basePx := plot.Y + y.ValueToPixel(base, plot.H)
		// Guard against Y axes where the base projects off-plot.
		if !s.Stacked {
			basePx = yZero
		}
		barX := bucketX + bucket*padding/2
		if !s.Stacked {
			barX += float32(s.GroupOffset) * barW
		}
		top2 := minF32(topPx, basePx)
		bot2 := maxF32(topPx, basePx)
		rect := Rect{X: barX, Y: top2, W: barW, H: bot2 - top2}
		fill := s.color
		if hot.PointIdx == i && hot.SeriesIdx >= 0 {
			// Lift alpha slightly for the hover target. Cheap visual cue.
			fill.A = clampf(fill.A*1.15, 0, 1)
		}
		canvas.FillRoundedRect(rect, 2, fill)
	}
}

// HitTest finds the index whose bar rectangle contains p.
func (s *BarSeries) HitTest(p Point, plot Rect, x, y Axis) int {
	ca, ok := x.(*CategoryAxis)
	if !ok {
		return -1
	}
	bucket := ca.BucketWidth(plot.W)
	if bucket <= 0 {
		return -1
	}
	padding := ca.GroupPadding
	if padding < 0 {
		padding = 0
	}
	if padding > 0.9 {
		padding = 0.9
	}
	groupTotal := s.GroupTotal
	if groupTotal <= 0 {
		groupTotal = 1
	}
	innerW := bucket * (1 - padding)
	var barW float32
	if s.Stacked {
		barW = innerW
	} else {
		barW = innerW / float32(groupTotal)
	}

	yZero := plot.Y + y.ValueToPixel(0, plot.H)
	for i, v := range s.Values {
		bucketX := plot.X + ca.ValueToPixel(float32(i), plot.W) - bucket/2
		base := float32(0)
		if s.Stacked && i < len(s.StackBase) {
			base = s.StackBase[i]
		}
		top := base + v
		topPx := plot.Y + y.ValueToPixel(top, plot.H)
		basePx := plot.Y + y.ValueToPixel(base, plot.H)
		if !s.Stacked {
			basePx = yZero
		}
		barX := bucketX + bucket*padding/2
		if !s.Stacked {
			barX += float32(s.GroupOffset) * barW
		}
		top2 := minF32(topPx, basePx)
		bot2 := maxF32(topPx, basePx)
		rect := Rect{X: barX, Y: top2, W: barW, H: bot2 - top2}
		if rect.Contains(p) {
			return i
		}
	}
	return -1
}

func (s *BarSeries) LegendMarker(canvas Canvas, swatch Rect) {
	canvas.FillRoundedRect(swatch, 2, s.color)
}
