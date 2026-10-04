package qui

import "testing"

func TestAbsoluteBothAnchorsStretch(t *testing.T) {
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorLeft | AnchorRight | AnchorTop | AnchorBottom,
		Left:   10, Right: 20, Top: 5, Bottom: 15,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 100})

	if a.Bounds().X != 10 {
		t.Errorf("X = %v, want 10", a.Bounds().X)
	}
	if a.Bounds().W != 170 { // 200 - 10 - 20
		t.Errorf("W = %v, want 170", a.Bounds().W)
	}
	if a.Bounds().Y != 5 {
		t.Errorf("Y = %v, want 5", a.Bounds().Y)
	}
	if a.Bounds().H != 80 { // 100 - 5 - 15
		t.Errorf("H = %v, want 80", a.Bounds().H)
	}
}

func TestAbsoluteLeftOnlyUsesWidth(t *testing.T) {
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorLeft | AnchorTop,
		Left:   20, Top: 10, Width: 100, Height: 50,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 500, H: 500})

	if a.Bounds().X != 20 || a.Bounds().W != 100 {
		t.Errorf("Left+Width: X=%v W=%v, want 20/100", a.Bounds().X, a.Bounds().W)
	}
	if a.Bounds().Y != 10 || a.Bounds().H != 50 {
		t.Errorf("Top+Height: Y=%v H=%v, want 10/50", a.Bounds().Y, a.Bounds().H)
	}
}

func TestAbsoluteRightOnlyPinsToEnd(t *testing.T) {
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorRight | AnchorTop,
		Right:  10, Top: 5, Width: 80, Height: 30,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 100})

	// Pinned to right edge: X = 200 - 10 - 80 = 110.
	if a.Bounds().X != 110 {
		t.Errorf("Right-anchored X = %v, want 110", a.Bounds().X)
	}
	if a.Bounds().W != 80 {
		t.Errorf("Right-anchored W = %v, want 80", a.Bounds().W)
	}
}

func TestAbsoluteNoAnchorFallsBackToMeasure(t *testing.T) {
	a := newSized(60, 40)
	// No explicit Anchor set → natural size at origin.
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 7, Y: 13, W: 200, H: 100})
	if a.Bounds().X != 7 || a.Bounds().Y != 13 {
		t.Errorf("no anchor: X=%v Y=%v, want 7/13", a.Bounds().X, a.Bounds().Y)
	}
	if a.Bounds().W != 60 || a.Bounds().H != 40 {
		t.Errorf("no anchor fell back to Measure: W=%v H=%v, want 60/40", a.Bounds().W, a.Bounds().H)
	}
}

func TestAbsoluteSingleAnchorUsesMeasureWhenWidthZero(t *testing.T) {
	a := newSized(25, 15)
	a.SetAbsolutePosition(AbsolutePosition{Anchor: AnchorLeft | AnchorTop, Left: 4, Top: 3})
	// Width/Height unset → Measure.
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 100, H: 100})
	if a.Bounds().W != 25 || a.Bounds().H != 15 {
		t.Errorf("measure fallback on single-anchor: W=%v H=%v, want 25/15", a.Bounds().W, a.Bounds().H)
	}
}

func TestAbsoluteTopRightCorner(t *testing.T) {
	// Classic tooltip/badge use — anchor to top-right.
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorTop | AnchorRight,
		Top:    8, Right: 8, Width: 40, Height: 20,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 300, H: 200})
	if a.Bounds().X != 252 { // 300 - 8 - 40
		t.Errorf("top-right X = %v, want 252", a.Bounds().X)
	}
	if a.Bounds().Y != 8 {
		t.Errorf("top-right Y = %v, want 8", a.Bounds().Y)
	}
}

func TestAbsoluteStretchedSidebar(t *testing.T) {
	// Sidebar pinned to left, top, bottom — stretches vertically.
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorLeft | AnchorTop | AnchorBottom,
		Width:  240,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 1000, H: 800})
	if a.Bounds().W != 240 {
		t.Errorf("sidebar W = %v, want 240", a.Bounds().W)
	}
	if a.Bounds().H != 800 {
		t.Errorf("sidebar stretched H = %v, want 800", a.Bounds().H)
	}
}

func TestAbsoluteOverConstrainedClamped(t *testing.T) {
	// Left + Right combined exceed parent width → W clamps to 0.
	a := newSized(0, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorLeft | AnchorRight,
		Left:   120, Right: 120,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 100, H: 50})
	if a.Bounds().W != 0 {
		t.Errorf("over-constrained W = %v, want 0 (clamped)", a.Bounds().W)
	}
}

func TestAbsoluteRightAnchorHonorsMinWidth(t *testing.T) {
	a := newSized(0, 0)
	a.SetMinSize(90, 0)
	a.SetAbsolutePosition(AbsolutePosition{
		Anchor: AnchorRight | AnchorTop,
		Right:  10,
		Width:  40,
	})
	AbsoluteLayout{}.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 100})

	if a.Bounds().W != 90 {
		t.Errorf("min width should override explicit width, got %v", a.Bounds().W)
	}
	if a.Bounds().X != 100 { // 200 - 10 - 90
		t.Errorf("right anchor should remain pinned after min-size clamp, X=%v want 100", a.Bounds().X)
	}
}
