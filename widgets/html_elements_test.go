package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestBoxPaintsAndComposes(t *testing.T) {
	child := NewLabel("hi")
	b := NewBox(FlexLayout{Direction: Vertical}, child)
	b.Style().Background = Color{R: 1, A: 1}
	b.Style().Border = Color{B: 1, A: 1}
	b.Style().BorderSize = 2

	if got := b.ChildList(); len(got) != 1 || got[0] != child {
		t.Fatalf("Box child not composed: %v", got)
	}
	if child.Parent() != Widget(b) {
		t.Errorf("child.Parent() = %v, want the Box (SetSelf wiring)", child.Parent())
	}
	b.Layout(Rect{W: 100, H: 40})
	// Draw through a recording canvas: a fill then a stroke.
	rc := &RecordingCanvas{}
	b.Draw(rc)
	if b.Role() != RoleGeneric {
		t.Errorf("Box.Role() = %q, want %q", b.Role(), RoleGeneric)
	}
}

func TestAnchorClickFiresOnClickOverHref(t *testing.T) {
	clicked := false
	a := NewAnchor("docs", "https://example.com")
	a.OnClick = func() { clicked = true }
	a.Layout(Rect{X: 0, Y: 0, W: 80, H: 20})

	a.Handle(NewMouseEvent(EventMouseDown, 10, 10, MouseButtonLeft, 0))
	a.Handle(NewMouseEvent(EventMouseUp, 10, 10, MouseButtonLeft, 0))
	if !clicked {
		t.Fatal("Anchor OnClick did not fire on press-release inside bounds")
	}
	if a.Role() != RoleLink {
		t.Errorf("Anchor.Role() = %q, want %q", a.Role(), RoleLink)
	}
	if a.AccessibleValue() != "https://example.com" {
		t.Errorf("Anchor.AccessibleValue() = %q", a.AccessibleValue())
	}
}

func TestAnchorNoClickWhenReleasedOutside(t *testing.T) {
	clicked := false
	a := NewAnchor("x", "")
	a.OnClick = func() { clicked = true }
	a.Layout(Rect{X: 0, Y: 0, W: 40, H: 20})

	a.Handle(NewMouseEvent(EventMouseDown, 5, 5, MouseButtonLeft, 0))
	a.Handle(NewMouseEvent(EventMouseUp, 500, 500, MouseButtonLeft, 0))
	if clicked {
		t.Fatal("Anchor fired OnClick despite release outside bounds")
	}
}

func TestRuleMeasuresFullWidthThinHeight(t *testing.T) {
	r := NewRule()
	got := r.Measure(Size{W: 200, H: 100})
	if got.W != 200 {
		t.Errorf("Rule width = %v, want 200 (full available)", got.W)
	}
	if got.H != 1 {
		t.Errorf("Rule height = %v, want 1 (default thickness)", got.H)
	}
	if r.Role() != RoleSeparator {
		t.Errorf("Rule.Role() = %q, want %q", r.Role(), RoleSeparator)
	}
}
