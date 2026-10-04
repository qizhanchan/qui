package graphs

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestLineSeriesBoundsFromPoints(t *testing.T) {
	s := NewLineSeries("a", []Point2D{{X: 1, Y: 2}, {X: 5, Y: 0}, {X: 3, Y: 7}})
	b := s.DataBounds()
	if !b.HasData {
		t.Fatalf("expected HasData=true")
	}
	if b.Xmin != 1 || b.Xmax != 5 || b.Ymin != 0 || b.Ymax != 7 {
		t.Errorf("bounds = %+v, want Xmin=1 Xmax=5 Ymin=0 Ymax=7", b)
	}
}

func TestLineSeriesDrawEmitsPolyline(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}, {X: 2, Y: 2}})
	s.SetColor(qui.Color{R: 1, A: 1})
	x := &ValueAxis{Min: 0, Max: 2}
	y := &ValueAxis{Min: 0, Max: 2}
	plot := qui.Rect{X: 10, Y: 20, W: 100, H: 200}

	// Disable markers so the test asserts the polyline path cleanly.
	s.MarkerSize = 0
	s.Draw(rec, plot, x, y, NoHotSpot)

	if len(rec.Polylines) != 1 {
		t.Fatalf("expected 1 DrawPolyline call, got %d", len(rec.Polylines))
	}
	pts := rec.Polylines[0]
	if len(pts) != 3 {
		t.Errorf("expected 3 points, got %d: %+v", len(pts), pts)
	}
	// First point maps to plot origin; last to plot far corner.
	first := pts[0]
	last := pts[2]
	if first.X != plot.X || first.Y != plot.Y {
		t.Errorf("first point projection = %+v, want plot origin %+v", first, qui.Point{X: plot.X, Y: plot.Y})
	}
	if last.X != plot.X+plot.W || last.Y != plot.Y+plot.H {
		t.Errorf("last point projection = %+v, want far corner (%v,%v)", last, plot.X+plot.W, plot.Y+plot.H)
	}
}

func TestLineSeriesHitTestWithinRadius(t *testing.T) {
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 20, Y: 0}})
	x := &ValueAxis{Min: 0, Max: 20}
	y := &ValueAxis{Min: -1, Max: 1}
	plot := qui.Rect{X: 0, Y: 0, W: 200, H: 100}

	// The middle data point (index 1) projects to (100, 50).
	if got := s.HitTest(qui.Point{X: 102, Y: 51}, plot, x, y); got != 1 {
		t.Errorf("hit near middle point returned %d, want 1", got)
	}
	// Far away from any point.
	if got := s.HitTest(qui.Point{X: 300, Y: 300}, plot, x, y); got != -1 {
		t.Errorf("hit far away returned %d, want -1", got)
	}
}

func TestLineSeriesSmoothEmitsDensePolylineThroughEndpoints(t *testing.T) {
	rec := &qui.RecordingCanvas{}
	pts := []Point2D{{X: 0, Y: 0}, {X: 1, Y: 2}, {X: 2, Y: 1}, {X: 3, Y: 3}}
	s := NewLineSeries("smooth", pts)
	s.Smooth = true
	s.SmoothSteps = 8
	s.MarkerSize = 0
	x := &ValueAxis{Min: 0, Max: 3}
	y := &ValueAxis{Min: 0, Max: 3}
	plot := qui.Rect{X: 0, Y: 0, W: 300, H: 300}

	s.Draw(rec, plot, x, y, NoHotSpot)

	if len(rec.Polylines) != 1 {
		t.Fatalf("expected 1 polyline, got %d", len(rec.Polylines))
	}
	path := rec.Polylines[0]
	// (n-1) segments * steps + 1 start point.
	wantLen := (len(pts)-1)*8 + 1
	if len(path) != wantLen {
		t.Errorf("smooth path len = %d, want %d", len(path), wantLen)
	}
	// First and last samples must coincide with projected endpoints.
	firstWant := qui.Point{X: plot.X + x.ValueToPixel(pts[0].X, plot.W), Y: plot.Y + y.ValueToPixel(pts[0].Y, plot.H)}
	lastWant := qui.Point{X: plot.X + x.ValueToPixel(pts[len(pts)-1].X, plot.W), Y: plot.Y + y.ValueToPixel(pts[len(pts)-1].Y, plot.H)}
	if path[0] != firstWant {
		t.Errorf("smooth path[0] = %+v, want %+v", path[0], firstWant)
	}
	if path[len(path)-1] != lastWant {
		t.Errorf("smooth path[last] = %+v, want %+v", path[len(path)-1], lastWant)
	}
}

func TestLineSeriesHiddenNoBoundsContribution(t *testing.T) {
	s := NewLineSeries("a", []Point2D{{X: 0, Y: 0}, {X: 1, Y: 1}})
	s.SetVisible(false)
	if s.Visible() {
		t.Errorf("series should be hidden")
	}
	// Bounds themselves are still reported — the chart filters by Visible().
	b := s.DataBounds()
	if !b.HasData {
		t.Errorf("DataBounds should still contain data even when hidden")
	}
}
