package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func phasedScroll(deltaY, x, y float32, phase GesturePhase) MouseEvent {
	e := NewScrollEvent(x, y, 0, deltaY, 0)
	e.ScrollPhase = phase
	return e
}

// A view that cannot move — its content fits — leaves the wheel to an
// enclosing scroller (Chrome's scroll chaining). It used to report every
// wheel as consumed, so a full-height list inside a scrolling page ate the
// wheel and the page never scrolled.
func TestScrollViewWheelChainsWhenContentFits(t *testing.T) {
	sv := NewScrollView()
	sv.SetContent(newPhaseSpy("c", Rect{}, nil), Size{W: 100, H: 100})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	if sv.Handle(newScrollEvent(-1, 100, 100)) {
		t.Fatal("a view with nothing to scroll consumed the wheel")
	}
}

// At an edge, a wheel toward that edge chains; away from it, it scrolls.
func TestScrollViewWheelChainsAtEdge(t *testing.T) {
	sv := NewScrollView()
	sv.SetContent(newPhaseSpy("c", Rect{}, nil), Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	if sv.Handle(newScrollEvent(1, 100, 100)) { // up, already at the top
		t.Fatal("wheel up at the top was consumed")
	}
	if !sv.Handle(newScrollEvent(-1, 100, 100)) || sv.ScrollY() == 0 {
		t.Fatal("wheel down did not scroll")
	}
	sv.ScrollTo(sv.MaxScroll())
	if sv.Handle(newScrollEvent(-1, 100, 100)) {
		t.Fatal("wheel down at the bottom was consumed")
	}
}

// A trackpad gesture that started by scrolling this view stays with it at
// the edge (momentum included); one that began elsewhere passes through.
func TestScrollViewGestureLatches(t *testing.T) {
	sv := NewScrollView()
	sv.SetContent(newPhaseSpy("c", Rect{}, nil), Size{W: 100, H: 1000})
	sv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	sv.ScrollTo(sv.MaxScroll() - 1)

	if !sv.Handle(phasedScroll(-5, 100, 100, GesturePhaseBegan)) {
		t.Fatal("gesture start that scrolled was not consumed")
	}
	if sv.ScrollY() != sv.MaxScroll() {
		t.Fatalf("scrollY = %v, want the bottom", sv.ScrollY())
	}
	for _, ph := range []GesturePhase{GesturePhaseChanged, GesturePhaseMomentum} {
		if !sv.Handle(phasedScroll(-5, 100, 100, ph)) {
			t.Fatalf("latched gesture (%v) leaked past the view at its edge", ph)
		}
	}
	sv.Handle(phasedScroll(0, 100, 100, GesturePhaseEnded))

	// A new gesture at the bottom, scrolling further down, cannot move it.
	if sv.Handle(phasedScroll(-5, 100, 100, GesturePhaseBegan)) {
		t.Fatal("new gesture at the edge was consumed")
	}
	if sv.Handle(phasedScroll(-5, 100, 100, GesturePhaseChanged)) {
		t.Fatal("unlatched gesture was consumed")
	}
}
