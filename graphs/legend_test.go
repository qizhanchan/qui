package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestLegendAttachedAsChild(t *testing.T) {
	c := NewChart()
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	c.SetLegend(NewLegend(LegendBottom))

	if c.Legend == nil {
		t.Fatalf("SetLegend did not attach a legend")
	}
	if c.Legend.chart != c {
		t.Errorf("legend.chart back-pointer not wired")
	}
}

func TestLegendClickTogglesSeriesVisibility(t *testing.T) {
	c := NewChart()
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}})
	c.AddSeries(s)
	c.SetLegend(NewLegend(LegendBottom))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Click inside the first legend row.
	if len(c.Legend.rowRects) == 0 {
		t.Fatalf("legend has no row rects after layout")
	}
	row := c.Legend.rowRects[0]
	click := qui.NewMouseEvent(qui.EventMouseDown, row.X+row.W/2, row.Y+row.H/2, qui.MouseButtonLeft, 0)

	if !c.Legend.Handle(click) {
		t.Errorf("legend click was not consumed")
	}
	if s.Visible() {
		t.Errorf("series should be hidden after legend click")
	}
	// Second click re-shows.
	c.Legend.Handle(click)
	if !s.Visible() {
		t.Errorf("series should be visible after second legend click")
	}
}

func TestLegendPositionBottomShrinksPlotHeight(t *testing.T) {
	base := NewChart()
	base.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	base.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	baseline := base.PlotRect().H

	withLegend := NewChart()
	withLegend.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	withLegend.SetLegend(NewLegend(LegendBottom))
	withLegend.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	shrunk := withLegend.PlotRect().H

	if shrunk >= baseline {
		t.Errorf("legend at bottom should shrink plot height: baseline=%v shrunk=%v", baseline, shrunk)
	}
}

func TestLegendReplacesPreviousInstance(t *testing.T) {
	c := NewChart()
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	first := NewLegend(LegendBottom)
	second := NewLegend(LegendRight)
	c.SetLegend(first)
	c.SetLegend(second)
	if c.Legend != second {
		t.Errorf("SetLegend should replace the existing legend")
	}
	// First legend should no longer sit in the chart child list.
	for _, ch := range c.Container.Children() {
		if ch == qui.Widget(first) {
			t.Errorf("previous legend was not detached from children")
		}
	}
}
