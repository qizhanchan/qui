package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestChartHoverAtSetsTooltipState(t *testing.T) {
	c := NewChart()
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}, {X: 20, Y: 0}})
	c.AddSeries(s)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Middle data point maps to plot's X-center. Hover there.
	plot := c.PlotRect()
	cx := plot.X + c.XAxis.ValueToPixel(10, plot.W)
	cy := plot.Y + c.YAxis.ValueToPixel(10, plot.H)
	sIdx, pIdx := c.HoverAt(qui.Point{X: cx + 1, Y: cy + 1})

	if sIdx != 0 || pIdx != 1 {
		t.Errorf("hover returned series=%d index=%d; want 0,1", sIdx, pIdx)
	}
	if !c.tooltip.active {
		t.Errorf("tooltip not activated after hover")
	}
	if c.tooltip.text == "" {
		t.Errorf("tooltip text empty")
	}
}

func TestChartHoverOutsideClearsTooltip(t *testing.T) {
	c := NewChart()
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 0}})
	c.AddSeries(s)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Force a hover state first.
	c.setHover(0, 0, qui.Point{X: 100, Y: 100})
	if !c.tooltip.active {
		t.Fatalf("precondition: tooltip should be active")
	}

	sIdx, pIdx := c.HoverAt(qui.Point{X: 10000, Y: 10000})
	if sIdx != -1 || pIdx != -1 {
		t.Errorf("far hover returned series=%d index=%d; want -1,-1", sIdx, pIdx)
	}
	if c.tooltip.active {
		t.Errorf("tooltip should be cleared after hover outside")
	}
}

func TestChartTooltipBuilderOverride(t *testing.T) {
	c := NewChart()
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}})
	c.AddSeries(s)
	c.TooltipBuilder = func(_ Series, i int) string {
		return "custom"
	}
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	c.setHover(0, 0, qui.Point{X: 10, Y: 10})
	if c.tooltip.text != "custom" {
		t.Errorf("tooltip text = %q, want 'custom'", c.tooltip.text)
	}
}
