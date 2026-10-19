package chart

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A pinned minimum stays while the maximum still follows the data; a top
// legend sits above the plot and takes room from its top.
func TestPinnedAxisMinAndTopLegend(t *testing.T) {
	c := NewChart()
	c.YAxis = &ValueAxis{InvertedAxis: true, Min: 0, PinMin: true}
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 40}, {X: 1, Y: 90}}))
	c.SetLegend(NewLegend(LegendTop))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	lo, hi := c.YAxis.Range()
	if lo != 0 || hi < 90 {
		t.Errorf("y range = %v..%v, want 0..≥90", lo, hi)
	}
	lb := c.Legend.Bounds()
	if lb.Y+lb.H > c.plotRect.Y {
		t.Errorf("top legend %+v overlaps the plot %+v", lb, c.plotRect)
	}
	if lb.Y < 0 {
		t.Errorf("top legend %+v outside the chart", lb)
	}
}
