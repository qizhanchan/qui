package qui

import "testing"

func TestRectIsEmpty(t *testing.T) {
	cases := []struct {
		name string
		r    Rect
		want bool
	}{
		{"zero", Rect{}, true},
		{"zero-width", Rect{X: 10, Y: 10, W: 0, H: 20}, true},
		{"zero-height", Rect{X: 10, Y: 10, W: 20, H: 0}, true},
		{"negative-width", Rect{X: 10, Y: 10, W: -5, H: 20}, true},
		{"normal", Rect{X: 0, Y: 0, W: 100, H: 100}, false},
	}
	for _, c := range cases {
		if got := c.r.IsEmpty(); got != c.want {
			t.Errorf("%s: IsEmpty()=%v want %v", c.name, got, c.want)
		}
	}
}

func TestRectUnion(t *testing.T) {
	// Empty + non-empty = non-empty.
	got := Rect{}.Union(Rect{X: 10, Y: 10, W: 5, H: 5})
	want := Rect{X: 10, Y: 10, W: 5, H: 5}
	if got != want {
		t.Errorf("empty.Union: got %+v want %+v", got, want)
	}

	// Overlapping rects.
	got = Rect{X: 0, Y: 0, W: 10, H: 10}.Union(Rect{X: 5, Y: 5, W: 10, H: 10})
	want = Rect{X: 0, Y: 0, W: 15, H: 15}
	if got != want {
		t.Errorf("overlap.Union: got %+v want %+v", got, want)
	}

	// Disjoint rects — bounding box.
	got = Rect{X: 0, Y: 0, W: 10, H: 10}.Union(Rect{X: 100, Y: 100, W: 10, H: 10})
	want = Rect{X: 0, Y: 0, W: 110, H: 110}
	if got != want {
		t.Errorf("disjoint.Union: got %+v want %+v", got, want)
	}
}

func TestRectIntersect(t *testing.T) {
	// Overlapping.
	got := Rect{X: 0, Y: 0, W: 10, H: 10}.Intersect(Rect{X: 5, Y: 5, W: 10, H: 10})
	want := Rect{X: 5, Y: 5, W: 5, H: 5}
	if got != want {
		t.Errorf("overlap: got %+v want %+v", got, want)
	}

	// Disjoint → empty.
	got = Rect{X: 0, Y: 0, W: 10, H: 10}.Intersect(Rect{X: 100, Y: 100, W: 10, H: 10})
	if !got.IsEmpty() {
		t.Errorf("disjoint: got %+v want empty", got)
	}

	// Edge-touching → empty (no overlap at boundary).
	got = Rect{X: 0, Y: 0, W: 10, H: 10}.Intersect(Rect{X: 10, Y: 0, W: 10, H: 10})
	if !got.IsEmpty() {
		t.Errorf("edge-touch: got %+v want empty", got)
	}
}

func TestRectIntersects(t *testing.T) {
	if !(Rect{X: 0, Y: 0, W: 10, H: 10}).Intersects(Rect{X: 5, Y: 5, W: 10, H: 10}) {
		t.Error("overlapping rects should intersect")
	}
	if (Rect{X: 0, Y: 0, W: 10, H: 10}).Intersects(Rect{X: 100, Y: 100, W: 10, H: 10}) {
		t.Error("disjoint rects should not intersect")
	}
}
