package chart

import (
	"encoding/json"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestFunnelSeriesLayoutAndHitTest(t *testing.T) {
	s := NewFunnelSeries("funnel", []PieSlice{
		{Label: "A", Value: 40},
		{Label: "B", Value: 30},
		{Label: "C", Value: 20},
		{Label: "D", Value: 10},
	})
	plot := qui.Rect{X: 0, Y: 0, W: 900, H: 460}
	segments := s.segments(plot)
	if len(segments) != 4 {
		t.Fatalf("segments = %d, want 4", len(segments))
	}
	if got := s.HitTest(qui.Point{X: 450, Y: 70}, plot, nil, nil); got != 0 {
		t.Fatalf("top hit = %d, want data index 0", got)
	}
	if got := s.HitTest(qui.Point{X: 450, Y: 430}, plot, nil, nil); got != -1 {
		t.Fatalf("outside hit = %d, want -1", got)
	}
	if got := s.HitTest(qui.Point{X: 10, Y: 70}, plot, nil, nil); got != -1 {
		t.Fatalf("left hit = %d, want -1", got)
	}
}

func TestFunnelSeriesDrawAndSort(t *testing.T) {
	s := NewFunnelSeries("funnel", []PieSlice{
		{Label: "small", Value: 1},
		{Label: "big", Value: 3},
	})
	s.SetColor(qui.Color{R: 0.2, G: 0.4, B: 0.8, A: 1})
	plot := qui.Rect{W: 400, H: 300}
	rec := &qui.RecordingCanvas{}
	s.Draw(rec, plot, nil, nil, HotSpot{SeriesIdx: 0, PointIdx: 0})
	if len(rec.Paths) != 4 {
		t.Fatalf("draw paths = %d, want two paths per segment", len(rec.Paths))
	}
	if len(rec.Texts) != 2 {
		t.Fatalf("outside labels = %d, want 2", len(rec.Texts))
	}
	if s.segments(plot)[0].index != 1 {
		t.Fatalf("descending sort should place the largest item first")
	}
	s.Sort = FunnelSortAscending
	if s.segments(plot)[0].index != 0 {
		t.Fatalf("ascending sort should place the smallest item first")
	}
}

func TestFunnelOptionMapping(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"color": ["#112233", "#445566"],
		"series": [{
			"type": "funnel",
			"name": "Sales",
			"sort": "none",
			"maxSize": "90%",
			"minSize": "10%",
			"data": [
				{"name": "A", "value": 12},
				{"name": "B", "value": 8}
			]
		}]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 500, H: 320})
	if len(c.Series) != 1 {
		t.Fatalf("series count = %d", len(c.Series))
	}
	s := c.Series[0].(*FunnelSeries)
	if s.Sort != FunnelSortNone || s.MaxSize != 0.9 || s.MinSize != 0.1 {
		t.Fatalf("mapped funnel = %+v", s)
	}
	if s.Slices[0].Color == s.Slices[1].Color || isZeroColor(s.Slices[0].Color) {
		t.Fatalf("data-item palette colors not assigned: %+v", s.Slices)
	}
}

func TestFunnelDataItemColorOverridesPalette(t *testing.T) {
	var point DataPoint
	if err := json.Unmarshal([]byte(`{"name":"A","value":1,"itemStyle":{"color":"#ff0000"}}`), &point); err != nil {
		t.Fatal(err)
	}
	if point.Color != CSSColor("#ff0000") {
		t.Fatalf("item color = %q", point.Color)
	}
}
