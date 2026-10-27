package gfx

import "testing"

func TestRecordingCanvasRecordsClearRect(t *testing.T) {
	var r RecordingCanvas
	r.Translate(10, 5)
	r.ClearRect(Rect{X: 1, Y: 2, W: 3, H: 4})
	if len(r.ClearRects) != 1 || r.ClearRects[0] != (Rect{X: 11, Y: 7, W: 3, H: 4}) {
		t.Fatalf("ClearRects = %+v", r.ClearRects)
	}
}
