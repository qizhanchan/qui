package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestScatterSeriesHitTestWithinRadius(t *testing.T) {
	s := NewScatterSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 0}})
	s.Size = 10 // radius=5, hit-radius=7
	x := &ValueAxis{Min: 0, Max: 10}
	y := &ValueAxis{Min: -1, Max: 1}
	plot := qui.Rect{X: 0, Y: 0, W: 200, H: 100}

	// Point 0 projects to (0, 50). Click within 6px.
	if got := s.HitTest(qui.Point{X: 3, Y: 52}, plot, x, y); got != 0 {
		t.Errorf("near-hit on point 0 returned %d, want 0", got)
	}
	// Far from either point.
	if got := s.HitTest(qui.Point{X: 100, Y: 100}, plot, x, y); got != -1 {
		t.Errorf("far click returned %d, want -1", got)
	}
}

func TestScatterSeriesDrawCountsMarkers(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	s := NewScatterSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}})
	s.SetColor(qui.Color{R: 1, A: 1})
	s.Shape = MarkerCircle
	x := &ValueAxis{Min: 0, Max: 2}
	y := &ValueAxis{Min: 0, Max: 2}
	plot := qui.Rect{X: 0, Y: 0, W: 100, H: 100}
	s.Draw(rec, plot, x, y, NoHotSpot)
	// Circle markers use FillRoundedRect, one per point.
	if len(rec.Rounds) != 3 {
		t.Errorf("expected 3 rounded rects, got %d", len(rec.Rounds))
	}
}

func TestScatterSeriesSquareShapeUsesFillRect(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	s := NewScatterSeries("a", []Point2D{{X: 0, Y: 0}})
	s.Shape = MarkerSquare
	s.Draw(rec, qui.Rect{X: 0, Y: 0, W: 100, H: 100}, &ValueAxis{Min: 0, Max: 1}, &ValueAxis{Min: 0, Max: 1}, NoHotSpot)
	if len(rec.Fills) != 1 {
		t.Errorf("expected 1 square fill, got %d", len(rec.Fills))
	}
}
