package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestChartPanShiftsXAxisRange(t *testing.T) {
	c := NewChart()
	c.EnablePan = true
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	plot := c.PlotRect()
	xmin0, xmax0 := c.XAxis.Range()
	span := xmax0 - xmin0

	// Mouse down at plot center.
	down := qui.NewMouseEvent(qui.EventMouseDown, plot.X+plot.W/2, plot.Y+plot.H/2, qui.MouseButtonLeft, 0)
	if !c.Handle(down) {
		t.Fatalf("MouseDown inside plot should be consumed")
	}
	// Drag right by 40px.
	move := qui.NewMouseEvent(qui.EventMouseMove, plot.X+plot.W/2+40, plot.Y+plot.H/2, qui.MouseButtonLeft, 0)
	c.Handle(move)

	xmin1, xmax1 := c.XAxis.Range()
	// Expected shift: -40/plot.W * span (drag right = show earlier values).
	wantShift := -40 / plot.W * span
	gotShift := xmin1 - xmin0
	diff := gotShift - wantShift
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-3 {
		t.Errorf("pan shift = %v, want %v (xmin: %v -> %v)", gotShift, wantShift, xmin0, xmin1)
	}
	_ = xmax1
}

func TestChartZoomAroundCursorPreservesAnchorValue(t *testing.T) {
	c := NewChart()
	c.EnableZoom = true
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	plot := c.PlotRect()
	cursorX := plot.X + plot.W*0.25
	// Anchor data value at this cursor.
	anchor := c.XAxis.PixelToValue(cursorX-plot.X, plot.W)

	// Zoom in (deltaY < 0).
	scroll := qui.NewScrollEvent(cursorX, plot.Y+plot.H/2, 0, -1, 0)
	if !c.Handle(scroll) {
		t.Fatalf("scroll inside plot should be consumed")
	}
	// Anchor value should still project to the same pixel column (within tolerance).
	plot2 := c.PlotRect()
	backPx := plot2.X + c.XAxis.ValueToPixel(anchor, plot2.W)
	diff := backPx - cursorX
	if diff < 0 {
		diff = -diff
	}
	if diff > 1 {
		t.Errorf("anchor value drifted: was at %v, now at %v", cursorX, backPx)
	}
}

func TestChartOnSelectFiresOnClickWithoutDrag(t *testing.T) {
	c := NewChart()
	c.EnableSelect = true
	line := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}})
	c.AddSeries(line)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	var selected Series
	var selectedIdx int
	c.OnSelect = func(s Series, i int) { selected = s; selectedIdx = i }

	plot := c.PlotRect()
	// Put hover onto the first data point.
	dx := plot.X + c.XAxis.ValueToPixel(0, plot.W)
	dy := plot.Y + c.YAxis.ValueToPixel(0, plot.H)
	c.Handle(qui.NewMouseEvent(qui.EventMouseMove, dx+1, dy+1, qui.MouseButtonLeft, 0))
	c.Handle(qui.NewMouseEvent(qui.EventMouseDown, dx+1, dy+1, qui.MouseButtonLeft, 0))
	c.Handle(qui.NewMouseEvent(qui.EventMouseUp, dx+1, dy+1, qui.MouseButtonLeft, 0))

	if selected != Series(line) {
		t.Errorf("OnSelect not fired with line series; selected=%v", selected)
	}
	if selectedIdx != 0 {
		t.Errorf("OnSelect index = %d, want 0", selectedIdx)
	}
}

func TestChartResetAxesRestoresAutoRange(t *testing.T) {
	c := NewChart()
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	origXmin, origXmax := c.XAxis.Range()

	// Distort ranges.
	c.XAxis.SetRange(100, 200)
	c.ResetAxes()
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	xmin2, xmax2 := c.XAxis.Range()
	if xmin2 != origXmin || xmax2 != origXmax {
		t.Errorf("ResetAxes did not restore auto-range: got [%v..%v], want [%v..%v]",
			xmin2, xmax2, origXmin, origXmax)
	}
}

func TestChartShiftScrollZoomsYAxis(t *testing.T) {
	c := NewChart()
	c.EnableZoom = true
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	plot := c.PlotRect()
	ymin0, ymax0 := c.YAxis.Range()

	scroll := qui.NewScrollEvent(plot.X+plot.W/2, plot.Y+plot.H/2, 0, -1, qui.ModShift)
	c.Handle(scroll)
	ymin1, ymax1 := c.YAxis.Range()
	if ymin1 == ymin0 && ymax1 == ymax0 {
		t.Errorf("Shift+scroll should have zoomed Y axis; range unchanged")
	}
}
