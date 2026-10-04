package qui

import "testing"

// ---- Style.Margin consumed by FlexLayout (margin-box semantics) ----

func TestFlexMarginMainAxis(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(30, 20)
	b.Style().Margin = Insets{Left: 16, Right: 4}
	c := newSized(40, 20)
	layout := FlexLayout{Direction: Horizontal, Gap: 10}
	layout.Apply([]Widget{a, b, c}, Rect{X: 0, Y: 0, W: 500, H: 40})

	// a at 0..50. b's slot starts at 60 (gap), content inset by Left=16.
	if b.Bounds().X != 76 {
		t.Errorf("b.X = %v, want 76 (60 + marginLeft 16)", b.Bounds().X)
	}
	if b.Bounds().W != 30 {
		t.Errorf("b.W = %v, want 30 (margin outside, content keeps natural)", b.Bounds().W)
	}
	// c starts after b's margin-box (60 + 16+30+4) + gap 10 = 120.
	if c.Bounds().X != 120 {
		t.Errorf("c.X = %v, want 120", c.Bounds().X)
	}
}

func TestFlexMarginCrossAxisAndVertical(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	b.Style().Margin = Insets{Top: 8, Left: 12}
	layout := FlexLayout{Direction: Vertical, Gap: 0, AlignItems: AlignStart}
	layout.Apply([]Widget{a, b}, Rect{X: 0, Y: 0, W: 200, H: 200})

	if b.Bounds().Y != 28 {
		t.Errorf("b.Y = %v, want 28 (a.H 20 + marginTop 8)", b.Bounds().Y)
	}
	if b.Bounds().X != 12 {
		t.Errorf("b.X = %v, want 12 (marginLeft indents on cross axis)", b.Bounds().X)
	}
}

func TestFlexMarginStretchShrinksContent(t *testing.T) {
	// AlignStretch fills the cross axis; margin insets the stretched box.
	a := newSized(50, 20)
	a.Style().Margin = Insets{Left: 10, Right: 10}
	layout := FlexLayout{Direction: Vertical, AlignItems: AlignStretch}
	layout.Apply([]Widget{a}, Rect{X: 0, Y: 0, W: 200, H: 100})

	if a.Bounds().X != 10 || a.Bounds().W != 180 {
		t.Errorf("a rect = (%v, w=%v), want (10, w=180)", a.Bounds().X, a.Bounds().W)
	}
}

func TestContainerMeasureIncludesMargin(t *testing.T) {
	a := newSized(50, 20)
	b := newSized(50, 20)
	b.Style().Margin = Insets{Top: 8, Left: 30}
	c := NewContainer(FlexLayout{Direction: Vertical}, a, b)
	c.Style().Padding = Insets{}

	size := c.Measure(Size{W: 500, H: 500})
	if size.H != 48 {
		t.Errorf("measured H = %v, want 48 (20 + 20 + marginTop 8)", size.H)
	}
	if size.W != 80 {
		t.Errorf("measured W = %v, want 80 (max(50, 50+marginLeft 30))", size.W)
	}
}
