package chart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestParityFixturesRenderEverySeries(t *testing.T) {
	paths, err := filepath.Glob("testdata/parity/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no parity fixtures found")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Width  int             `json:"width"`
				Height int             `json:"height"`
				Option json.RawMessage `json:"option"`
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			var option Option
			if err := json.Unmarshal(fixture.Option, &option); err != nil {
				t.Fatal(err)
			}
			animation := false
			option.Animation = &animation
			chart := New(option)
			chart.Layout(qui.Rect{W: float32(fixture.Width), H: float32(fixture.Height)})
			if len(chart.Series) == 0 {
				t.Fatal("fixture produced no series")
			}
			for i, series := range chart.Series {
				rect, xAxis, yAxis := chart.seriesAxesAt(i)
				if chartUsesFullBounds(series) {
					rect = chart.Bounds()
				}
				rec := &qui.RecordingCanvas{}
				series.Draw(rec, rect, xAxis, yAxis, NoHotSpot)
				calls := len(rec.Fills) + len(rec.Rounds) + len(rec.Strokes) + len(rec.Texts) +
					len(rec.Lines) + len(rec.Polylines) + len(rec.Paths) + len(rec.Images) +
					len(rec.Vectors) + len(rec.Shadows)
				if calls == 0 {
					t.Fatalf("series %d %T emitted no draw calls", i, series)
				}
			}
		})
	}
}
