package chart

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestVisualMapLegendReservesSpaceAndDrawsGradient(t *testing.T) {
	animation := false
	c := New(Option{
		Animation: &animation,
		XAxis:     AxisOption{Type: AxisCategory, Data: []string{"A", "B"}},
		YAxis:     AxisOption{Type: AxisCategory, Data: []string{"X", "Y"}},
		VisualMap: &VisualMapOption{
			Min:     valuePtr(0.0),
			Max:     valuePtr(10.0),
			InRange: &VisualMapInRange{Color: []CSSColor{"#0000ff", "#ff0000"}},
		},
		Series: []SeriesOption{{
			Type: SeriesHeatmap,
			Data: []DataPoint{
				{Values: []float64{0, 0, 2}},
				{Values: []float64{1, 1, 9}},
			},
		}},
	})
	c.Layout(qui.Rect{W: 600, H: 400})
	legend := c.VisualMapRect()
	if legend.IsEmpty() {
		t.Fatal("visualMap legend rect is empty")
	}
	if legend.X < c.PlotRect().X+c.PlotRect().W+8 {
		t.Fatalf("legend overlaps plot: plot=%+v legend=%+v", c.PlotRect(), legend)
	}
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	foundGradient := false
	for _, rect := range rec.Fills {
		if rect == legend {
			foundGradient = true
			break
		}
	}
	if !foundGradient {
		t.Fatalf("gradient rect %+v not drawn; fills=%+v", legend, rec.Fills)
	}
}

func TestAxisPointerDrawsAtCategoryCenter(t *testing.T) {
	animation := false
	c := New(Option{
		Animation:   &animation,
		XAxis:       AxisOption{Type: AxisCategory, Data: []string{"A", "B", "C"}},
		YAxis:       AxisOption{Type: AxisValue},
		Tooltip:     &TooltipOption{Trigger: "axis"},
		AxisPointer: &AxisPointerOption{Color: "#ff0000"},
		Series: []SeriesOption{{
			Type: SeriesLine,
			Name: "line",
			Data: Numbers(1, 5, 2),
		}},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	line := c.Series[0].(*LineSeries)
	category := c.XAxis.(*CategoryAxis)
	x := c.PlotRect().X + category.ValueToPixel(1, c.PlotRect().W)
	y := c.PlotRect().Y + c.YAxis.ValueToPixel(line.Points[1].Y, c.PlotRect().H)
	if series, point := c.HoverAt(qui.Point{X: x, Y: y}); series != 0 || point != 1 {
		t.Fatalf("hover = %d/%d, want 0/1", series, point)
	}
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	for _, line := range rec.Lines {
		if line.P1.X == x && line.P2.X == x && line.P1.Y == c.PlotRect().Y && line.P2.Y == c.PlotRect().Y+c.PlotRect().H {
			return
		}
	}
	t.Fatalf("axis pointer vertical line at x=%v not found: %+v", x, rec.Lines)
}

func TestAxisPointerCanBeDisabled(t *testing.T) {
	disabled := false
	animation := false
	c := New(Option{
		Animation:   &animation,
		XAxis:       AxisOption{Type: AxisCategory, Data: []string{"A", "B"}},
		Tooltip:     &TooltipOption{Trigger: "axis"},
		AxisPointer: &AxisPointerOption{Show: &disabled},
		Series:      []SeriesOption{{Type: SeriesBar, Data: Numbers(1, 2)}},
	})
	if c.ShowAxisPointer {
		t.Fatal("axis pointer should be disabled")
	}
}
