package chart

import (
	"encoding/json"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestOptionJSONBuildsCategoryLineChart(t *testing.T) {
	const source = `{
		"title": {"text": "Revenue", "subtext": "weekly"},
		"tooltip": {"show": true},
		"legend": {"show": true, "position": "bottom"},
		"xAxis": {"type": "category", "data": ["Mon", "Tue", "Wed"]},
		"yAxis": {"type": "value", "name": "USD", "min": 0},
		"color": ["#5470c6", "#91cc75"],
		"series": [
			{"type": "line", "name": "Online", "data": [120, 200, 150], "smooth": true},
			{"type": "bar", "name": "Retail", "data": [80, 90, 100]}
		]
	}`
	var opt Option
	if err := json.Unmarshal([]byte(source), &opt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	c := New(opt)
	if c.Title != "Revenue" || c.SubTitle != "weekly" {
		t.Fatalf("title = %q/%q", c.Title, c.SubTitle)
	}
	c.Layout(qui.Rect{X: 0, Y: 0, W: 500, H: 320})
	if len(c.Series) != 2 {
		t.Fatalf("series count = %d, want 2", len(c.Series))
	}
	line, ok := c.Series[0].(*LineSeries)
	if !ok {
		t.Fatalf("series[0] = %T, want *LineSeries", c.Series[0])
	}
	if !line.Smooth || len(line.Points) != 3 {
		t.Fatalf("line = %+v, want 3 smooth points", line)
	}
	if line.Points[1] != (Point2D{X: 1, Y: 200}) {
		t.Fatalf("line point[1] = %+v", line.Points[1])
	}
	axis, ok := c.XAxis.(*CategoryAxis)
	if !ok {
		t.Fatalf("XAxis = %T, want *CategoryAxis", c.XAxis)
	}
	if len(axis.Categories) != 3 || axis.Categories[2] != "Wed" {
		t.Fatalf("categories = %#v", axis.Categories)
	}
	if c.Legend == nil || c.Legend.Position != LegendBottom {
		t.Fatalf("legend = %+v", c.Legend)
	}
	if c.Palette[0] != (qui.Color{R: 0x54 / 255.0, G: 0x70 / 255.0, B: 0xc6 / 255.0, A: 1}) {
		t.Fatalf("palette[0] = %+v", c.Palette[0])
	}
}

func TestOptionDerivesCategoriesForBarSeries(t *testing.T) {
	c := New(Option{
		XAxis: AxisOption{Type: AxisCategory},
		Series: []SeriesOption{{
			Type: SeriesBar,
			Name: "Orders",
			Data: []DataPoint{NamedPoint("Mon", 3), NamedPoint("Tue", 5)},
		}},
	})
	axis := c.XAxis.(*CategoryAxis)
	if len(axis.Categories) != 2 || axis.Categories[0] != "Mon" || axis.Categories[1] != "Tue" {
		t.Fatalf("derived categories = %#v", axis.Categories)
	}
	bar := c.Series[0].(*BarSeries)
	if len(bar.Values) != 2 || bar.Values[1] != 5 {
		t.Fatalf("bar values = %#v", bar.Values)
	}
}

func TestSetOptionReplacesSeriesAndLegend(t *testing.T) {
	c := New(Option{
		Legend: &LegendOption{},
		Series: []SeriesOption{{Type: SeriesLine, Name: "old", Data: Numbers(1, 2)}},
	})
	oldLegend := c.Legend
	c.SetOption(Option{
		Legend: &LegendOption{Show: boolPtr(false)},
		Series: []SeriesOption{{Type: SeriesBar, Name: "new", Data: Numbers(4, 5)}},
	})
	if len(c.Series) != 1 || c.Series[0].Name() != "new" {
		t.Fatalf("series were not replaced: %+v", c.Series)
	}
	if c.Legend != nil {
		t.Fatalf("legend should be removed, got %+v", c.Legend)
	}
	if oldLegend != nil && c.ChildCount() != 0 {
		t.Fatalf("old legend remains attached; child count=%d", c.ChildCount())
	}
}

func TestOptionGridOverridesPlotGutters(t *testing.T) {
	c := New(Option{
		Title:  &TitleOption{Text: "Fixed grid"},
		Legend: &LegendOption{},
		Grid:   &GridOption{Left: 80, Right: 20, Top: 30, Bottom: 50},
		XAxis:  AxisOption{Type: AxisValue},
		Series: []SeriesOption{{
			Type: SeriesLine,
			Data: []DataPoint{XYPoint(0, 0), XYPoint(10, 10)},
		}},
	})
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	plot := c.PlotRect()
	if plot.X != 80 || plot.Y != 30 || plot.W != 300 || plot.H != 220 {
		t.Fatalf("plot rect = %+v, want {X:80 Y:30 W:300 H:220}", plot)
	}
}

func TestOptionTooltipCanBeDisabled(t *testing.T) {
	c := New(Option{
		Tooltip: &TooltipOption{Show: boolPtr(false)},
		Series: []SeriesOption{{
			Type: SeriesScatter,
			Data: []DataPoint{XYPoint(1, 2)},
		}},
	})
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	if c.ShowTooltip {
		t.Fatal("ShowTooltip should be false")
	}
	c.HoverAt(qui.Point{X: c.PlotRect().X + c.PlotRect().W/2, Y: c.PlotRect().Y + c.PlotRect().H/2})
	if c.tooltip.active {
		t.Fatalf("tooltip should stay inactive: %+v", c.tooltip)
	}
}

func TestDataPointJSONShapes(t *testing.T) {
	const source = `[12, [3, 4], ["Mon", 7], {"name":"Tue","value":9}, null]`
	var data []DataPoint
	if err := json.Unmarshal([]byte(source), &data); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if data[0].Y != 12 || data[1].X != 3 || data[1].Y != 4 {
		t.Fatalf("cartesian shapes = %#v", data[:2])
	}
	if data[2].Name != "Mon" || data[2].Y != 7 || data[3].Name != "Tue" || data[3].Y != 9 {
		t.Fatalf("named shapes = %#v", data[2:4])
	}
	if !data[4].Missing {
		t.Fatalf("null should decode as Missing: %#v", data[4])
	}
}

func boolPtr(value bool) *bool { return &value }

func TestOptionDrawsDeclarativeSeries(t *testing.T) {
	c := New(Option{
		Title: &TitleOption{Text: "Mixed"},
		XAxis: AxisOption{Type: AxisCategory, Data: []string{"A", "B", "C"}},
		Series: []SeriesOption{
			{Type: SeriesLine, Name: "line", Data: Numbers(2, 5, 3)},
			{Type: SeriesBar, Name: "bar", Data: Numbers(1, 4, 2)},
		},
	})
	c.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	if len(rec.Polylines) == 0 {
		t.Fatal("declarative line series did not emit a polyline")
	}
	if len(rec.Fills) == 0 && len(rec.Rounds) == 0 {
		t.Fatal("declarative bar series did not emit a rectangle")
	}
}

func TestMergeOptionUpdatesSeriesByNameAndKeepsUnsetFields(t *testing.T) {
	c := New(Option{
		Title: &TitleOption{Text: "Revenue"},
		YAxis: AxisOption{Type: AxisValue, Name: "USD"},
		Series: []SeriesOption{
			{Type: SeriesLine, Name: "actual", Data: Numbers(1, 2)},
			{Type: SeriesBar, Name: "target", Data: Numbers(3, 4)},
		},
	})
	c.MergeOption(Option{
		Title: &TitleOption{SubText: "updated"},
		YAxis: AxisOption{Name: "Revenue"},
		Series: []SeriesOption{
			{Name: "target", Data: Numbers(8, 9)},
		},
	})
	if c.Title != "Revenue" || c.SubTitle != "updated" {
		t.Fatalf("title = %q/%q", c.Title, c.SubTitle)
	}
	if c.YAxis.Title() != "Revenue" {
		t.Fatalf("YAxis title = %q", c.YAxis.Title())
	}
	if len(c.Series) != 2 {
		t.Fatalf("series count = %d", len(c.Series))
	}
	bar := c.Series[1].(*BarSeries)
	if bar.Name() != "target" || len(bar.Values) != 2 || bar.Values[0] != 8 {
		t.Fatalf("merged bar = %+v", bar)
	}
	line := c.Series[0].(*LineSeries)
	if len(line.Points) != 2 || line.Points[0].Y != 1 {
		t.Fatalf("line should be unchanged: %+v", line.Points)
	}
}
