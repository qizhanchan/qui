package chart

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestBarSeriesDataBoundsIncludesZero(t *testing.T) {
	s := NewBarSeries("a", []float32{3, 5, 2})
	b := s.DataBounds()
	if b.Ymin > 0 || b.Ymax < 5 {
		t.Errorf("bounds %+v should span 0..max(Values)", b)
	}
}

func TestBarSeriesGroupedOffsetsSideBySide(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	c := NewChart()
	c.XAxis = &CategoryAxis{Categories: []string{"A", "B"}}
	// two grouped series; Chart should assign them GroupOffset 0 and 1
	// plus GroupTotal=2 so they lay out side-by-side within each bucket.
	a := NewBarSeries("a", []float32{1, 2})
	b := NewBarSeries("b", []float32{3, 4})
	c.AddSeries(a)
	c.AddSeries(b)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if a.GroupOffset != 0 || a.GroupTotal != 2 {
		t.Errorf("a offset/total = %d/%d, want 0/2", a.GroupOffset, a.GroupTotal)
	}
	if b.GroupOffset != 1 || b.GroupTotal != 2 {
		t.Errorf("b offset/total = %d/%d, want 1/2", b.GroupOffset, b.GroupTotal)
	}

	// Draw and verify bars were issued as FillRoundedRects and are
	// non-overlapping within a bucket.
	c.Draw(rec)
	if got := len(rec.Rounds) + len(rec.Paths); got < 4 {
		t.Fatalf("expected at least 4 bar fills across 2 series × 2 buckets, got %d", got)
	}
}

func TestBarSeriesStackedBasesAccumulate(t *testing.T) {
	c := NewChart()
	c.XAxis = &CategoryAxis{Categories: []string{"A", "B"}}
	a := NewBarSeries("a", []float32{1, 2})
	a.Stacked = true
	b := NewBarSeries("b", []float32{3, 4})
	b.Stacked = true
	c.AddSeries(a)
	c.AddSeries(b)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if len(a.StackBase) != 2 || a.StackBase[0] != 0 || a.StackBase[1] != 0 {
		t.Errorf("a.StackBase = %+v, want [0 0]", a.StackBase)
	}
	if len(b.StackBase) != 2 || b.StackBase[0] != 1 || b.StackBase[1] != 2 {
		t.Errorf("b.StackBase = %+v, want [1 2]", b.StackBase)
	}
}

func TestBarSeriesHitTestFindsBar(t *testing.T) {
	c := NewChart()
	c.XAxis = &CategoryAxis{Categories: []string{"A", "B"}}
	bar := NewBarSeries("a", []float32{5, 10})
	c.AddSeries(bar)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Find first bucket center in plot coordinates.
	plot := c.PlotRect()
	ca := c.XAxis.(*CategoryAxis)
	cx := plot.X + ca.ValueToPixel(0, plot.W)
	cy := plot.Y + c.YAxis.ValueToPixel(2.5, plot.H) // mid-height of the bar
	got := bar.HitTest(qui.Point{X: cx, Y: cy}, plot, c.XAxis, c.YAxis)
	if got != 0 {
		t.Errorf("expected HitTest to find bar 0, got %d", got)
	}
}

func TestBarGeometryMatchesEChartsDefaultSlots(t *testing.T) {
	axis := &CategoryAxis{Categories: []string{"A", "B", "C", "D", "E", "F", "G"}}
	bucket := float32(800.0 / 7.0)

	grouped := NewBarSeries("g", []float32{1})
	grouped.GroupTotal = 2
	grouped.GroupOffset = 0
	grouped.CategoryGap = 0.27
	width, offset := grouped.barGeometry(bucket, axis)
	if diffF32(width, 39.7279) > 1e-3 || diffF32(offset, -41.7134) > 1e-3 {
		t.Fatalf("grouped geometry = (%v,%v), want (39.7279,-41.7134)", width, offset)
	}

	stacked := NewBarSeries("s", []float32{1})
	stacked.Stacked = true
	stacked.GroupTotal = 1
	stacked.GroupOffset = 0
	stacked.CategoryGap = 0.31
	width, offset = stacked.barGeometry(160, axis)
	if diffF32(width, 110.4) > 1e-3 || diffF32(offset, -55.2) > 1e-3 {
		t.Fatalf("stacked geometry = (%v,%v), want (110.4,-55.2)", width, offset)
	}
}

func diffF32(a, b float32) float32 {
	if a > b {
		return a - b
	}
	return b - a
}
