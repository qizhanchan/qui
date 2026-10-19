package qui

import "testing"

func TestScrollBarThumbBothAxes(t *testing.T) {
	v := ScrollBar{Track: Rect{X: 90, Y: 0, W: 10, H: 100}, Content: 400, Viewport: 100, Offset: 150}
	h := ScrollBar{Track: Rect{X: 0, Y: 90, W: 100, H: 10}, Horizontal: true, Content: 400, Viewport: 100, Offset: 150}
	vt, ht := v.Thumb(), h.Thumb()
	// 100*100/400 = 25 px thumb, travel 75, progress 150/300 = 0.5.
	if vt != (Rect{X: 90, Y: 37.5, W: 10, H: 25}) {
		t.Fatalf("vertical thumb = %+v", vt)
	}
	if ht != (Rect{X: 37.5, Y: 90, W: 25, H: 10}) {
		t.Fatalf("horizontal thumb = %+v", ht)
	}
}

func TestScrollBarMinThumbAndFit(t *testing.T) {
	b := ScrollBar{Track: Rect{W: 10, H: 100}, Content: 100000, Viewport: 100}
	if got := b.Thumb().H; got != vbarMinThumb {
		t.Fatalf("thumb floor = %v, want %v", got, vbarMinThumb)
	}
	fit := ScrollBar{Track: Rect{W: 10, H: 100}, Content: 50, Viewport: 100}
	if got := fit.Thumb(); got != fit.Track {
		t.Fatalf("fitting content thumb = %+v, want the track", got)
	}
}

func TestScrollBarPressPagesAndDrags(t *testing.T) {
	b := ScrollBar{Track: Rect{X: 90, W: 10, H: 100}, Content: 400, Viewport: 100, Offset: 150}
	var d ScrollBarDrag
	if off, hit := b.Press(Point{X: 95, Y: 95}, &d); !hit || off != 240 || d.Active {
		t.Fatalf("track below thumb: off=%v hit=%v drag=%v", off, hit, d.Active)
	}
	if off, hit := b.Press(Point{X: 95, Y: 2}, &d); !hit || off != 60 {
		t.Fatalf("track above thumb: off=%v hit=%v", off, hit)
	}
	if _, hit := b.Press(Point{X: 50, Y: 50}, &d); hit {
		t.Fatal("press off the track hit the bar")
	}
	if _, hit := b.Press(Point{X: 95, Y: 50}, &d); !hit || !d.Active {
		t.Fatal("thumb press did not start a drag")
	}
	// 1 px of travel = 300/75 = 4 px of scroll.
	if got := d.Offset(b, Point{X: 95, Y: 60}); got != 190 {
		t.Fatalf("drag offset = %v, want 190", got)
	}
	if got := d.Offset(b, Point{X: 95, Y: 1000}); got != 300 {
		t.Fatalf("drag past the end = %v, want clamped 300", got)
	}
	if !d.End() || d.Active || d.End() {
		t.Fatal("End should report the drag once")
	}
}
