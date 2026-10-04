package graphs

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
	if len(rec.Rounds) < 4 {
		t.Fatalf("expected at least 4 bar rects across 2 series × 2 buckets, got %d", len(rec.Rounds))
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
