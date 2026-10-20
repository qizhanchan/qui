package chart

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestDataZoomInitialWindowAndSliderLayout(t *testing.T) {
	animation := false
	c := New(Option{
		Animation: &animation,
		XAxis:     AxisOption{Type: AxisValue, Min: valuePtr(0.0), Max: valuePtr(100.0)},
		YAxis:     AxisOption{Type: AxisValue},
		DataZoom:  []DataZoomOption{{Start: valuePtr(20.0), End: valuePtr(80.0)}},
		Series:    []SeriesOption{{Type: SeriesLine, Data: []DataPoint{XYPoint(0, 0), XYPoint(100, 10)}}},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	lo, hi := c.XAxis.Range()
	if lo != 20 || hi != 80 {
		t.Fatalf("dataZoom range = %v..%v, want 20..80", lo, hi)
	}
	if c.DataZoomRect().IsEmpty() {
		t.Fatal("slider rect is empty")
	}
	rec := &qui.RecordingCanvas{}
	c.Draw(rec)
	found := false
	for _, round := range rec.Rounds {
		if round.Rect == c.DataZoomRect() {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("slider track %+v not drawn: %+v", c.DataZoomRect(), rec.Rounds)
	}
}

func TestDataZoomSliderBrushUpdatesRange(t *testing.T) {
	animation := false
	c := New(Option{
		Animation: &animation,
		XAxis:     AxisOption{Type: AxisValue, Min: valuePtr(0.0), Max: valuePtr(100.0)},
		YAxis:     AxisOption{Type: AxisValue},
		DataZoom:  []DataZoomOption{{}},
		Series:    []SeriesOption{{Type: SeriesLine, Data: []DataPoint{XYPoint(0, 0), XYPoint(100, 10)}}},
	})
	c.Layout(qui.Rect{W: 500, H: 300})
	rect := c.DataZoomRect()
	y := rect.Y + rect.H/2
	x0 := rect.X + rect.W*.25
	x1 := rect.X + rect.W*.75
	if !c.Handle(qui.NewMouseEvent(qui.EventMouseDown, x0, y, qui.MouseButtonLeft, 0)) {
		t.Fatal("slider mouse down was not consumed")
	}
	if !c.Handle(qui.NewMouseEvent(qui.EventMouseMove, x1, y, qui.MouseButtonLeft, 0)) {
		t.Fatal("slider mouse move was not consumed")
	}
	if !c.Handle(qui.NewMouseEvent(qui.EventMouseUp, x1, y, qui.MouseButtonLeft, 0)) {
		t.Fatal("slider mouse up was not consumed")
	}
	lo, hi := c.XAxis.Range()
	if lo < 24.9 || lo > 25.1 || hi < 74.9 || hi > 75.1 {
		t.Fatalf("brushed range = %v..%v, want ~25..75", lo, hi)
	}
}

func TestInsideDataZoomEnablesWheelAndPan(t *testing.T) {
	animation := false
	c := New(Option{
		Animation: &animation,
		XAxis:     AxisOption{Type: AxisValue},
		YAxis:     AxisOption{Type: AxisValue},
		DataZoom:  []DataZoomOption{{Type: "inside"}},
		Series:    []SeriesOption{{Type: SeriesLine, Data: []DataPoint{XYPoint(0, 0), XYPoint(10, 10)}}},
	})
	if !c.EnableZoom || !c.EnablePan {
		t.Fatalf("inside dataZoom did not enable navigation: zoom=%v pan=%v", c.EnableZoom, c.EnablePan)
	}
}
