package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestChartAutoRangesAxesFromSeries(t *testing.T) {
	c := NewChart()
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 5}}))
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	xmin, xmax := c.XAxis.Range()
	if xmin >= 0 || xmax <= 10 {
		t.Errorf("X axis auto-range should pad outward, got [%v..%v]", xmin, xmax)
	}
	ymin, ymax := c.YAxis.Range()
	if ymin >= 0 || ymax <= 5 {
		t.Errorf("Y axis auto-range should pad outward, got [%v..%v]", ymin, ymax)
	}
	if !c.YAxis.Inverted() {
		t.Errorf("default Y axis should be inverted so 'up on screen' = larger value")
	}
}

func TestChartAddSeriesAutoAssignsPaletteColor(t *testing.T) {
	c := NewChart()
	a := NewLineSeries("a", []Point2D{{X: 0, Y: 0}})
	b := NewLineSeries("b", []Point2D{{X: 0, Y: 0}})
	c.AddSeries(a)
	c.AddSeries(b)
	if isZeroColor(a.Color()) {
		t.Errorf("series A did not receive a palette color")
	}
	if isZeroColor(b.Color()) {
		t.Errorf("series B did not receive a palette color")
	}
	if a.Color() == b.Color() {
		t.Errorf("adjacent series should get distinct palette colors")
	}
}

func TestChartKeepsExplicitSeriesColor(t *testing.T) {
	c := NewChart()
	a := NewLineSeries("a", []Point2D{{X: 0, Y: 0}})
	want := qui.Color{R: 0.2, G: 0.3, B: 0.4, A: 1}
	a.SetColor(want)
	c.AddSeries(a)
	if a.Color() != want {
		t.Errorf("explicit color overwritten: got %+v, want %+v", a.Color(), want)
	}
}

func TestChartPlotRectReservesGutters(t *testing.T) {
	c := NewChart()
	c.AddSeries(NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}}))
	total := qui.Rect{X: 0, Y: 0, W: 400, H: 300}
	c.Layout(total)

	p := c.PlotRect()
	if p.W >= total.W || p.H >= total.H {
		t.Errorf("plotRect %+v should be strictly smaller than %+v", p, total)
	}
	if p.X <= total.X || p.Y < total.Y {
		t.Errorf("plotRect origin %+v should be offset from total origin %+v", p, total)
	}
}

func TestChartDrawIssuesPolylineForLineSeries(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	c := NewChart()
	c.Title = ""
	line := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 0}})
	line.MarkerSize = 0
	c.AddSeries(line)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	c.Draw(rec)
	// At least one DrawPolyline call should have reached the base canvas
	// via the ClipCanvas used internally.
	found := false
	for _, pts := range rec.Polylines {
		if len(pts) == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a polyline with 3 points from LineSeries; got %+v", rec.Polylines)
	}
}

func TestChartHiddenSeriesDoesNotAffectRange(t *testing.T) {
	c := NewChart()
	visible := NewLineSeries("v", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 10}})
	hidden := NewLineSeries("h", []Point2D{{X: 100, Y: 100}})
	hidden.SetVisible(false)
	c.AddSeries(visible)
	c.AddSeries(hidden)
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	_, xmax := c.XAxis.Range()
	if xmax > 20 {
		t.Errorf("hidden series should not affect auto-range; got Xmax=%v", xmax)
	}
}
