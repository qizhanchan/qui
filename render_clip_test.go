package qui

import "testing"

// State-stack clip tests. They push state on a RecordingCanvas and
// assert on the recorded effective rects.

func newRecorderWithClip(clip Rect) *RecordingCanvas {
	r := &RecordingCanvas{canvasState: newCanvasState(clip)}
	return r
}

func TestStateClipFillInside(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 10, Y: 10, W: 20, H: 20})

	rec.FillRect(Rect{X: 12, Y: 12, W: 5, H: 5}, Color{})
	if len(rec.Fills) != 1 || rec.Fills[0] != (Rect{X: 12, Y: 12, W: 5, H: 5}) {
		t.Fatalf("inside-clip FillRect: got %+v", rec.Fills)
	}
}

func TestStateClipFillOutside(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 10, Y: 10, W: 20, H: 20})

	rec.FillRect(Rect{X: 100, Y: 100, W: 5, H: 5}, Color{})
	if len(rec.Fills) != 0 {
		t.Errorf("outside-clip FillRect should be dropped, got %+v", rec.Fills)
	}
}

func TestStateClipFillPartial(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 10, Y: 10, W: 20, H: 20})

	rec.FillRect(Rect{X: 20, Y: 20, W: 100, H: 100}, Color{})
	want := Rect{X: 20, Y: 20, W: 10, H: 10}
	if len(rec.Fills) != 1 || rec.Fills[0] != want {
		t.Errorf("partial-clip FillRect: got %+v, want [%+v]", rec.Fills, want)
	}
}

func TestStateClipNested(t *testing.T) {
	// Outer clip {0..100,0..100}, inner clip {50..150,0..100} —
	// effective {50..100, 0..100}. A rect at X=60..160 should clip to
	// X=60..100.
	rec := newRecorderWithClip(Rect{X: 0, Y: 0, W: 100, H: 100})

	innerID := rec.Save()
	rec.ClipRect(Rect{X: 50, Y: 0, W: 100, H: 100})
	rec.FillRect(Rect{X: 60, Y: 20, W: 100, H: 10}, Color{})
	rec.RestoreTo(innerID)

	want := Rect{X: 60, Y: 20, W: 40, H: 10}
	if len(rec.Fills) != 1 || rec.Fills[0] != want {
		t.Fatalf("nested clip FillRect: got %+v, want [%+v]", rec.Fills, want)
	}

	// After RestoreTo, the outer clip is back — a fill at X=80..120
	// should clip to X=80..100 (not further to the inner clip).
	rec.FillRect(Rect{X: 80, Y: 30, W: 40, H: 10}, Color{})
	want2 := Rect{X: 80, Y: 30, W: 20, H: 10}
	if rec.Fills[1] != want2 {
		t.Errorf("post-restore FillRect: got %+v, want %+v", rec.Fills[1], want2)
	}
}

func TestStateClipBoundsExposed(t *testing.T) {
	clip := Rect{X: 1, Y: 2, W: 3, H: 4}
	rec := newRecorderWithClip(clip)

	var ca ClipAware = rec
	if got := ca.ClipBounds(); got != clip {
		t.Errorf("ClipBounds() = %+v, want %+v", got, clip)
	}
}

func TestStateClipTextDropped(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 10, Y: 10, W: 20, H: 20})

	rec.DrawText("hi", Rect{X: 200, Y: 200, W: 50, H: 20}, Color{}, Font{})
	if len(rec.Texts) != 0 {
		t.Errorf("DrawText outside clip should be dropped, got %+v", rec.Texts)
	}
}

func TestStateClipRoundedForwardsClipped(t *testing.T) {
	// A partially-out-of-clip rounded rect goes through
	// FillRoundedRect on the backend, which applies the state clip
	// directly. RecordingCanvas's FillRoundedRect records the eff rect.
	rec := newRecorderWithClip(Rect{X: 10, Y: 10, W: 20, H: 20})
	rec.FillRoundedRect(Rect{X: 0, Y: 0, W: 25, H: 25}, 6, Color{})
	if len(rec.Rounds) != 1 {
		t.Fatalf("partial rounded forward expected, got %+v", rec.Rounds)
	}
}

func TestStateScaleAppliesToFillRect(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 0, Y: 0, W: 1000, H: 1000})

	rec.Scale(2, 2)
	rec.FillRect(Rect{X: 10, Y: 10, W: 5, H: 5}, Color{})
	want := Rect{X: 20, Y: 20, W: 10, H: 10}
	if rec.Fills[0] != want {
		t.Errorf("scaled FillRect: got %+v, want %+v", rec.Fills[0], want)
	}
}

func TestStateSaveRestorePopsScale(t *testing.T) {
	rec := newRecorderWithClip(Rect{X: 0, Y: 0, W: 1000, H: 1000})

	id := rec.Save()
	rec.Scale(2, 2)
	rec.FillRect(Rect{X: 10, Y: 10, W: 5, H: 5}, Color{}) // scaled
	rec.RestoreTo(id)
	rec.FillRect(Rect{X: 10, Y: 10, W: 5, H: 5}, Color{}) // unscaled

	if rec.Fills[0] != (Rect{X: 20, Y: 20, W: 10, H: 10}) {
		t.Errorf("first fill should be scaled: %+v", rec.Fills[0])
	}
	if rec.Fills[1] != (Rect{X: 10, Y: 10, W: 5, H: 5}) {
		t.Errorf("second fill should be unscaled: %+v", rec.Fills[1])
	}
}
