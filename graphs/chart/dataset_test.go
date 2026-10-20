package chart

import (
	"encoding/json"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestDatasetArrayRowsEncodeCartesianSeries(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"dataset": {
			"source": [["Mon", 120], ["Tue", 80], ["Wed", 95]],
			"dimensions": ["day", "orders"]
		},
		"xAxis": {"type": "category"},
		"series": [{
			"type": "bar",
			"name": "orders",
			"encode": {"x": "day", "y": "orders"}
		}]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Dataset) != 1 || len(opt.Dataset[0].Source) != 3 {
		t.Fatalf("dataset decode = %+v", opt.Dataset)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 500, H: 300})
	bar, ok := c.Series[0].(*BarSeries)
	if !ok {
		t.Fatalf("series = %T", c.Series[0])
	}
	if len(bar.Values) != 3 || bar.Values[0] != 120 || bar.Values[2] != 95 {
		t.Fatalf("bar values = %v", bar.Values)
	}
	x, _ := c.XAxis.(*CategoryAxis)
	if x == nil || len(x.Categories) != 3 || x.Categories[0] != "Mon" || x.Categories[2] != "Wed" {
		t.Fatalf("categories = %+v", x)
	}
}

func TestDatasetObjectRowsEncodePieAndSingleDatasetObjectJSON(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"dataset": {
			"source": [
				{"product": "Mobile", "sales": 55},
				{"product": "Web", "sales": 30},
				{"product": "API", "sales": 15}
			]
		},
		"series": [{
			"type": "pie",
			"encode": {"name": "product", "value": "sales"}
		}]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 500, H: 300})
	pie, ok := c.Series[0].(*PieSeries)
	if !ok {
		t.Fatalf("series = %T", c.Series[0])
	}
	if len(pie.Slices) != 3 || pie.Slices[0].Label != "Mobile" || pie.Slices[0].Value != 55 ||
		pie.Slices[2].Label != "API" || pie.Slices[2].Value != 15 {
		t.Fatalf("pie slices = %+v", pie.Slices)
	}
}

func TestDatasetTransformsRunInOrder(t *testing.T) {
	dataset := DatasetOption{
		Dimensions: []string{"name", "value"},
		Source: []DatasetRow{
			{Values: []any{"A", 2.0}},
			{Values: []any{"B", 9.0}},
			{Values: []any{"C", 5.0}},
		},
		Transform: []DatasetTransform{
			FilterDataset(func(row DatasetRow) bool {
				value, _ := row.Value("value", []string{"name", "value"})
				number, _ := datasetNumber(value)
				return number >= 5
			}),
			SortDataset("value", true),
		},
	}
	c := New(Option{
		Dataset: DatasetList{dataset},
		Series: []SeriesOption{{
			Type:   SeriesLine,
			Encode: &EncodeOption{Y: "value"},
		}},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	line := c.Series[0].(*LineSeries)
	if len(line.Points) != 2 || line.Points[0].Y != 9 || line.Points[1].Y != 5 {
		t.Fatalf("transformed line points = %+v", line.Points)
	}
}

func TestDatasetIndexSelectsSourceAndDataOverridesIt(t *testing.T) {
	c := New(Option{
		Dataset: DatasetList{
			{Source: []DatasetRow{{Values: []any{1.0}}, {Values: []any{2.0}}}},
			{Source: []DatasetRow{{Values: []any{9.0}}, {Values: []any{10.0}}}},
		},
		Series: []SeriesOption{
			{Type: SeriesBar, Name: "second", DatasetIndex: 1},
			{Type: SeriesBar, Name: "explicit", DatasetIndex: 1, Data: Numbers(4, 5)},
			{Type: SeriesBar, Name: "missing", DatasetIndex: 9},
		},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	second := c.Series[0].(*BarSeries)
	explicit := c.Series[1].(*BarSeries)
	missing := c.Series[2].(*BarSeries)
	if len(second.Values) != 2 || second.Values[0] != 9 || second.Values[1] != 10 {
		t.Fatalf("datasetIndex values = %v", second.Values)
	}
	if len(explicit.Values) != 2 || explicit.Values[0] != 4 || explicit.Values[1] != 5 {
		t.Fatalf("explicit data should win: %v", explicit.Values)
	}
	if len(missing.Values) != 0 {
		t.Fatalf("invalid dataset index produced data: %v", missing.Values)
	}
}

func TestMergeOptionCanReplaceDatasetAndEncode(t *testing.T) {
	c := New(Option{
		Dataset: DatasetList{{Source: []DatasetRow{{Values: []any{1.0}}, {Values: []any{2.0}}}}},
		Series:  []SeriesOption{{Type: SeriesBar, Name: "orders", Encode: &EncodeOption{Y: "y"}}},
	})
	c.MergeOption(OptionPatch{
		Dataset: &DatasetList{{Source: []DatasetRow{{Values: []any{7.0}}, {Values: []any{8.0}}}}},
		Series: []SeriesPatch{{
			Name:         valuePtr("orders"),
			DatasetIndex: valuePtr(0),
			Encode:       &EncodeOption{Y: "value"},
		}},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	bar := c.Series[0].(*BarSeries)
	if len(bar.Values) != 2 || bar.Values[0] != 7 || bar.Values[1] != 8 {
		t.Fatalf("merged dataset values = %v", bar.Values)
	}
	c.MergeOption(OptionPatch{Series: []SeriesPatch{{Name: valuePtr("orders"), ClearEncode: true}}})
	if c.lastOption.Series[0].Encode != nil {
		t.Fatalf("encode was not cleared: %+v", c.lastOption.Series[0].Encode)
	}
}

func TestDatasetMapTransformAndRowJSONMarshal(t *testing.T) {
	row := DatasetRow{Values: []any{1.0, "A"}}
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[1,"A"]` {
		t.Fatalf("row marshal = %s", data)
	}
	transform := MapDataset(func(row DatasetRow) DatasetRow {
		return DatasetRow{Fields: map[string]any{"name": row.Values[1], "value": row.Values[0]}}
	})
	out := transform.Apply([]DatasetRow{row})
	if len(out) != 1 || out[0].Fields["name"] != "A" || out[0].Fields["value"] != 1.0 {
		t.Fatalf("mapped rows = %+v", out)
	}
}

func TestDeclarativeDatasetFilterAndSortTransforms(t *testing.T) {
	var opt Option
	err := json.Unmarshal([]byte(`{
		"dataset": {
			"dimensions": ["name", "value"],
			"source": [["A", 2], ["B", 9], ["C", 5], ["D", 7]],
			"transform": [
				{"type": "filter", "config": {"dimension": "value", "gte": 5}},
				{"type": "sort", "config": {"dimension": 1, "order": "desc"}}
			]
		},
		"series": [{"type": "line", "encode": {"y": 1}}]
	}`), &opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Dataset) != 1 || len(opt.Dataset[0].Transforms) != 2 {
		t.Fatalf("declarative transforms = %+v", opt.Dataset)
	}
	c := New(opt)
	c.Layout(qui.Rect{W: 500, H: 300})
	line := c.Series[0].(*LineSeries)
	if len(line.Points) != 3 || line.Points[0].Y != 9 || line.Points[1].Y != 7 || line.Points[2].Y != 5 {
		t.Fatalf("declarative transformed points = %+v", line.Points)
	}
}
