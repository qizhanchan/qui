package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestAreaSeriesDataBoundsIncludesBaseline(t *testing.T) {
	s := NewAreaSeries("a", []Point2D{{X: 0, Y: 5}, {X: 1, Y: 7}})
	s.Baseline = 0
	b := s.DataBounds()
	if b.Ymin > 0 {
		t.Errorf("AreaSeries bounds should include baseline; got %+v", b)
	}
}

func TestAreaSeriesDrawEmitsFillStripsAndStroke(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	s := NewAreaSeries("a", []Point2D{{X: 0, Y: 1}, {X: 1, Y: 2}, {X: 2, Y: 1}})
	s.SetColor(qui.Color{R: 0.2, G: 0.4, B: 0.8, A: 1})
	s.Baseline = 0
	x := &ValueAxis{Min: 0, Max: 2}
	y := &ValueAxis{Min: 0, Max: 3}
	plot := qui.Rect{X: 0, Y: 0, W: 200, H: 100}

	s.Draw(rec, plot, x, y, NoHotSpot)

	if len(rec.Fills) == 0 {
		t.Errorf("expected fill strips, got none")
	}
	if len(rec.Polylines) != 1 {
		t.Errorf("expected 1 stroke polyline, got %d", len(rec.Polylines))
	}
}
