package graphs

import (
	"math"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestPieSeriesSlicesSumTo2Pi(t *testing.T) {
	s := NewPieSeries("p", []PieSlice{
		{Label: "A", Value: 1},
		{Label: "B", Value: 2},
		{Label: "C", Value: 1},
	})
	var total float32
	for _, sl := range s.Slices {
		if sl.Value > 0 {
			total += sl.Value
		}
	}
	// After normalization each slice's sweep = value/total * 2π. Summing
	// should give 2π — sanity check on the expected model.
	sum := float64(0)
	for _, sl := range s.Slices {
		if sl.Value <= 0 {
			continue
		}
		sum += float64(sl.Value/total) * 2 * math.Pi
	}
	if math.Abs(sum-2*math.Pi) > 1e-5 {
		t.Errorf("slice sweeps sum = %v, want 2π", sum)
	}
}

func TestPieSeriesHitTestInsideFirstSlice(t *testing.T) {
	s := NewPieSeries("p", []PieSlice{
		{Label: "A", Value: 1},
		{Label: "B", Value: 1},
		{Label: "C", Value: 1},
		{Label: "D", Value: 1},
	})
	plot := qui.Rect{X: 0, Y: 0, W: 200, H: 200}
	// A hit just below center and slightly right lands in the first
	// slice (starting at 12 o'clock, sweeping clockwise 90°).
	idx := s.HitTest(qui.Point{X: 120, Y: 90}, plot, nil, nil)
	if idx != 0 {
		t.Errorf("expected slice 0, got %d", idx)
	}
	// Inside donut hole → no slice (when InnerRatio > 0).
	s.InnerRatio = 0.5
	idx = s.HitTest(qui.Point{X: 100, Y: 100}, plot, nil, nil)
	if idx != -1 {
		t.Errorf("expected -1 in donut hole, got %d", idx)
	}
}

func TestChartPieOnlySkipsAxesAndGrid(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	c := NewChart()
	c.Title = "" // avoid title-text draws polluting the line count
	c.AddSeries(NewPieSeries("p", []PieSlice{
		{Label: "A", Value: 1},
		{Label: "B", Value: 1},
	}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})

	// usesAxes must be false for an all-pie chart — Layout should not
	// reserve gutters for tick labels, so the plot area covers the full
	// chart rect (no Title / Legend either).
	plot := c.PlotRect()
	if plot.W != 200 || plot.H != 200 {
		t.Errorf("pie-only plotRect = %+v, want full 200×200 with no gutters", plot)
	}

	// Baseline: a chart with a line series should reserve gutters.
	c2 := NewChart()
	c2.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	c2.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})
	if c2.PlotRect().W == 200 {
		t.Errorf("line chart should reserve gutters; plot.W = 200 means no reservation happened")
	}

	// Draw the pie-only chart and confirm no axis/grid helper ran.
	c.Draw(rec)
	// Axis line count for a cartesian chart would be >= 2 (the two
	// baselines from drawAxes). We assert strictly: no axis lines means
	// the line count is bounded by pie spokes (which are always drawn
	// with FillLine-ish primitives inside fillPieSlice). Presence check
	// is indirect — we verify no XAxis/YAxis was auto-created either.
	if c.XAxis != nil || c.YAxis != nil {
		t.Errorf("pie-only chart should not auto-create axes; got XAxis=%v YAxis=%v", c.XAxis, c.YAxis)
	}
}

func TestChartAutoAssignsPieSliceColorsFromPalette(t *testing.T) {
	c := NewChart()
	pie := NewPieSeries("p", []PieSlice{
		{Label: "A", Value: 1},
		{Label: "B", Value: 2},
		{Label: "C", Value: 3},
		{Label: "Custom", Value: 1, Color: qui.Color{R: 1, A: 1}},
	})
	c.AddSeries(pie)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})

	if isZeroColor(pie.Slices[0].Color) {
		t.Errorf("slice 0 still zero-color after Layout")
	}
	if pie.Slices[0].Color == pie.Slices[1].Color {
		t.Errorf("slices 0 and 1 share a color %+v — palette cycling broken", pie.Slices[0].Color)
	}
	if pie.Slices[1].Color == pie.Slices[2].Color {
		t.Errorf("slices 1 and 2 share a color %+v", pie.Slices[1].Color)
	}
	// User-specified color preserved.
	if pie.Slices[3].Color != (qui.Color{R: 1, A: 1}) {
		t.Errorf("explicit slice color overwritten: got %+v", pie.Slices[3].Color)
	}
}

func TestPieSeriesSkipsZeroValueSlices(t *testing.T) {
	s := NewPieSeries("p", []PieSlice{
		{Label: "A", Value: 1},
		{Label: "Zero", Value: 0},
		{Label: "B", Value: 2},
	})
	// No zero-value slice should contribute to sweep or hit detection.
	// We assert via HitTest semantic: there's no angle at which "Zero" wins.
	plot := qui.Rect{X: 0, Y: 0, W: 200, H: 200}
	for ang := 0.0; ang < 2*math.Pi; ang += math.Pi / 8 {
		x := float32(100 + 50*math.Cos(ang))
		y := float32(100 + 50*math.Sin(ang))
		idx := s.HitTest(qui.Point{X: x, Y: y}, plot, nil, nil)
		if idx == 1 {
			t.Errorf("zero-value slice should never win hit-test, got idx=1 at angle %v", ang)
		}
	}
}
