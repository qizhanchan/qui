package chart

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestMultipleGridsAndAxesRouteSeriesIndependently(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"animation": false,
		"grid": [
			{"left": 60, "right": 40, "top": 30, "bottom": 320},
			{"left": 60, "right": 40, "top": 320, "bottom": 40}
		],
		"xAxis": [
			{"type": "category", "data": ["A", "B"]},
			{"type": "category", "data": ["C", "D"]}
		],
		"yAxis": [
			{"type": "value", "name": "top"},
			{"type": "value", "name": "bottom"}
		],
		"series": [
			{"type": "line", "name": "one", "xAxisIndex": 0, "yAxisIndex": 0, "gridIndex": 0, "data": [1, 2]},
			{"type": "bar", "name": "two", "xAxisIndex": 1, "yAxisIndex": 1, "gridIndex": 1, "data": [3, 4]}
		]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Grids) != 2 || len(opt.XAxes) != 2 || len(opt.YAxes) != 2 {
		t.Fatalf("decoded collections = grids %d x %d y %d", len(opt.Grids), len(opt.XAxes), len(opt.YAxes))
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 600, H: 700})
	if len(c.GridRects) != 2 || len(c.XAxes) != 2 || len(c.YAxes) != 2 {
		t.Fatalf("laid out collections = grids %d x %d y %d", len(c.GridRects), len(c.XAxes), len(c.YAxes))
	}
	if c.GridRects[0].Y != 30 || c.GridRects[1].Y != 320 {
		t.Fatalf("grid rects = %+v", c.GridRects)
	}
	if c.XAxis != c.XAxes[0] || c.YAxis != c.YAxes[0] || c.PlotRect() != c.GridRects[0] {
		t.Fatal("primary aliases do not point at index zero")
	}
	rect, xAxis, yAxis := c.seriesAxesAt(1)
	if rect != c.GridRects[1] || xAxis != c.XAxes[1] || yAxis != c.YAxes[1] {
		t.Fatalf("series 1 mapping = rect %+v x %v y %v", rect, xAxis, yAxis)
	}
	bar := c.Series[1].(*BarSeries)
	category := xAxis.(*CategoryAxis)
	x := rect.X + category.ValueToPixel(0, rect.W)
	y := rect.Y + yAxis.ValueToPixel(bar.Values[0], rect.H)
	if got := bar.HitTest(qui.Point{X: x, Y: y}, rect, xAxis, yAxis); got != 0 {
		t.Fatalf("second-grid bar hit = %d, want 0", got)
	}
	if got := bar.HitTest(qui.Point{X: x, Y: y}, c.GridRects[0], c.XAxes[0], c.YAxes[0]); got >= 0 {
		t.Fatalf("bar unexpectedly hit using first grid axes: %d", got)
	}
}

func TestSecondaryYAxisSharesGridAndRangesFromItsSeries(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"animation": false,
		"xAxis": {"type": "category", "data": ["A", "B"]},
		"yAxis": [
			{"type": "value", "name": "left", "min": 0, "max": 10},
			{"type": "value", "name": "right", "min": 0, "max": 100}
		],
		"series": [
			{"type": "line", "name": "small", "yAxisIndex": 0, "data": [2, 4]},
			{"type": "line", "name": "large", "yAxisIndex": 1, "data": [40, 80]}
		]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 600, H: 400})
	if len(c.GridRects) != 1 || len(c.YAxes) != 2 {
		t.Fatalf("grids/axes = %d/%d", len(c.GridRects), len(c.YAxes))
	}
	leftLo, leftHi := c.YAxes[0].Range()
	rightLo, rightHi := c.YAxes[1].Range()
	if leftLo != 0 || leftHi != 10 || rightLo != 0 || rightHi != 100 {
		t.Fatalf("secondary ranges = left %v..%v right %v..%v", leftLo, leftHi, rightLo, rightHi)
	}
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	if len(rec.Texts) < 8 {
		t.Fatalf("secondary axis labels were not drawn: %d texts", len(rec.Texts))
	}
}

func TestAxisArraysSurviveJSONRoundTrip(t *testing.T) {
	input := Option{
		Grids: []GridOption{{Left: 10}, {Left: 20}},
		XAxes: []AxisOption{{Type: AxisCategory, Data: []string{"A"}}, {Type: AxisValue}},
		YAxes: []AxisOption{{Type: AxisValue}, {Type: AxisValue, Name: "right"}},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output Option
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Grids) != 2 || len(output.XAxes) != 2 || len(output.YAxes) != 2 || output.YAxes[1].Name != "right" {
		t.Fatalf("round trip = %+v", output)
	}
}

func TestMergeOptionPatchesPluralAxesAndGrids(t *testing.T) {
	c := New(Option{
		Grids: []GridOption{{Left: 20}, {Left: 30}},
		XAxes: []AxisOption{{Type: AxisValue, Name: "one"}, {Type: AxisValue, Name: "two"}},
		YAxes: []AxisOption{{Type: AxisValue}, {Type: AxisValue}},
	})
	c.MergeOption(OptionPatch{
		Grids: &[]GridPatch{{Left: valuePtr(float32(80))}},
		XAxes: &[]AxisPatch{{Name: valuePtr("renamed")}, {Name: valuePtr("second")}},
		YAxis: &AxisPatch{Name: valuePtr("left-y")},
	})
	if len(c.lastOption.Grids) != 1 || c.lastOption.Grids[0].Left != 80 {
		t.Fatalf("grid patch = %+v", c.lastOption.Grids)
	}
	if len(c.lastOption.XAxes) != 2 || c.lastOption.XAxes[0].Name != "renamed" || c.lastOption.XAxes[1].Name != "second" {
		t.Fatalf("x axes patch = %+v", c.lastOption.XAxes)
	}
	if c.lastOption.YAxes[0].Name != "left-y" || c.lastOption.YAxis.Name != "left-y" {
		t.Fatalf("singular y patch did not update plural first axis: %+v / %+v", c.lastOption.YAxis, c.lastOption.YAxes)
	}
}

func TestAxisTooltipOnlyAggregatesSeriesOnHoveredAxes(t *testing.T) {
	c := New(Option{
		Animation: boolPointer(false),
		XAxes: []AxisOption{
			{Type: AxisCategory, Data: []string{"A", "B"}},
			{Type: AxisCategory, Data: []string{"A", "B"}},
		},
		YAxes: []AxisOption{{Type: AxisValue}, {Type: AxisValue}},
		Series: []SeriesOption{
			{Type: SeriesLine, Name: "top", XAxisIndex: 0, YAxisIndex: 0, Data: Numbers(1, 2)},
			{Type: SeriesLine, Name: "bottom", XAxisIndex: 1, YAxisIndex: 1, Data: Numbers(10, 20)},
		},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	c.hoverSeries = 1
	c.hoverIndex = 0
	text := DefaultAxisTooltipText(c, 0)
	if !strings.Contains(text, "bottom: 10") || strings.Contains(text, "top: 1") {
		t.Fatalf("axis tooltip leaked across axes: %q", text)
	}
}
