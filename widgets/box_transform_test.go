package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestBoxCSSTransformAffectsHitTestAndPaintBounds(t *testing.T) {
	box := NewBox(nil)
	box.Layout(Rect{X: 0, Y: 0, W: 50, H: 40})
	box.Transform = &BoxTransform{TX: 100, SX: 1, SY: 1}

	if hit := box.HitTest(Point{X: 110, Y: 10}); hit != box {
		t.Fatalf("transformed point hit %v, want box", hit)
	}
	if hit := box.HitTest(Point{X: 10, Y: 10}); hit != nil {
		t.Fatalf("original unpainted point hit %v, want nil", hit)
	}
	if got, want := box.PaintBounds(), (Rect{X: 100, Y: 0, W: 50, H: 40}); got != want {
		t.Fatalf("PaintBounds = %+v, want %+v", got, want)
	}
}

func TestBoxVisualTransformDoesNotMoveHitTarget(t *testing.T) {
	box := NewBox(nil)
	box.Layout(Rect{X: 0, Y: 0, W: 50, H: 40})
	box.VisualTransform = &BoxTransform{TX: 100, SX: 1, SY: 1}

	if hit := box.HitTest(Point{X: 10, Y: 10}); hit != box {
		t.Fatalf("original point hit %v, want box", hit)
	}
	if hit := box.HitTest(Point{X: 110, Y: 10}); hit != nil {
		t.Fatalf("visual-only transformed point hit %v, want nil", hit)
	}
	if got, want := box.PaintBounds(), (Rect{X: 100, Y: 0, W: 50, H: 40}); got != want {
		t.Fatalf("PaintBounds = %+v, want %+v", got, want)
	}
}

func TestBoxPaintBoundsIncludesTransformedChildOverflow(t *testing.T) {
	child := newPhaseSpy("child", Rect{X: 40, Y: 10, W: 30, H: 20}, nil)
	box := NewBox(nil, child)
	box.Layout(Rect{X: 0, Y: 0, W: 50, H: 40})
	// The test layout engine is nil, so restore the explicit overflowing
	// child rect after the parent records its own bounds.
	child.Layout(Rect{X: 40, Y: 10, W: 30, H: 20})
	box.Transform = &BoxTransform{TX: 100, SX: 1, SY: 1}

	if got, want := box.PaintBounds(), (Rect{X: 100, Y: 0, W: 70, H: 40}); got != want {
		t.Fatalf("PaintBounds = %+v, want %+v", got, want)
	}
}

func TestChildInteractionAndPaintBoundsInheritParentTransform(t *testing.T) {
	child := newPhaseSpy("child", Rect{X: 10, Y: 5, W: 20, H: 10}, nil)
	parent := NewBox(nil, child)
	parent.Layout(Rect{X: 0, Y: 0, W: 80, H: 40})
	child.Layout(Rect{X: 10, Y: 5, W: 20, H: 10})
	parent.Transform = &BoxTransform{TX: 100, SX: 1, SY: 1}

	want := Rect{X: 110, Y: 5, W: 20, H: 10}
	if got := InteractionBoundsOf(child); got != want {
		t.Fatalf("InteractionBoundsOf(child) = %+v, want %+v", got, want)
	}
	if got := PaintBoundsInWindow(child); got != want {
		t.Fatalf("PaintBoundsInWindow(child) = %+v, want %+v", got, want)
	}
}

func TestTransformedChildInvalidationUsesWindowPaintBounds(t *testing.T) {
	child := newPhaseSpy("child", Rect{X: 10, Y: 5, W: 20, H: 10}, nil)
	parent := NewBox(nil, child)
	parent.Layout(Rect{X: 0, Y: 0, W: 80, H: 40})
	child.Layout(Rect{X: 10, Y: 5, W: 20, H: 10})
	parent.Transform = &BoxTransform{TX: 100, SX: 1, SY: 1}
	w := NewTestWindow(Size{W: 300, H: 100})
	w.SetRoot(parent)
	w.ClearDirtyRegion()

	child.InvalidateLayout()

	want := Rect{X: 110, Y: 5, W: 20, H: 10}
	if got := w.IdleState().DirtyRegion; got != want {
		t.Fatalf("dirty region = %+v, want transformed bounds %+v", got, want)
	}
}

func TestTransformedOverlayInvalidatesPaintBounds(t *testing.T) {
	box := NewBox(nil)
	box.Layout(Rect{X: 0, Y: 0, W: 50, H: 40})
	box.Transform = &BoxTransform{TX: 100, SX: 1, SY: 1}
	w := NewTestWindow(Size{W: 300, H: 100})

	w.PushOverlay(box)
	want := Rect{X: 100, Y: 0, W: 50, H: 40}
	if got := w.IdleState().DirtyRegion; got != want {
		t.Fatalf("dirty region after push = %+v, want %+v", got, want)
	}

	w.ClearDirtyRegion()
	w.PopOverlay()
	if got := w.IdleState().DirtyRegion; got != want {
		t.Fatalf("dirty region after pop = %+v, want %+v", got, want)
	}
}
