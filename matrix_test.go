package qui

import "testing"

func TestMatrixTransformPoint(t *testing.T) {
	m := IdentityMatrix()
	if p := m.TransformPoint(Point{X: 3, Y: 4}); p.X != 3 || p.Y != 4 {
		t.Errorf("identity: got %v", p)
	}
	m = TranslateMatrix(10, 20)
	if p := m.TransformPoint(Point{X: 3, Y: 4}); p.X != 13 || p.Y != 24 {
		t.Errorf("translate: got %v", p)
	}
	m = ScaleMatrix(2, 3)
	if p := m.TransformPoint(Point{X: 3, Y: 4}); p.X != 6 || p.Y != 12 {
		t.Errorf("scale: got %v", p)
	}
	// Translate then Scale, composed via Concat — drawing order matches
	// canvas.Translate then canvas.Scale.
	m = TranslateMatrix(10, 0).Concat(ScaleMatrix(2, 1))
	if p := m.TransformPoint(Point{X: 5, Y: 0}); p.X != 20 || p.Y != 0 {
		t.Errorf("translate*scale: got %v", p)
	}
}

func TestMatrixInvert(t *testing.T) {
	m := TranslateMatrix(10, 20).Concat(ScaleMatrix(2, 4))
	inv, ok := m.Invert()
	if !ok {
		t.Fatal("invert failed for non-singular matrix")
	}
	p := Point{X: 3, Y: 5}
	q := m.TransformPoint(p)
	r := inv.TransformPoint(q)
	if absF(r.X-p.X) > 1e-4 || absF(r.Y-p.Y) > 1e-4 {
		t.Errorf("invert round-trip: %v -> %v -> %v", p, q, r)
	}
}

func TestCanvasStateTranslate(t *testing.T) {
	s := newCanvasState(Rect{W: 1000, H: 1000})
	s.Translate(10, 20)
	s.Scale(2, 3)
	m := s.CurrentMatrix()
	// The state stack should have composed translate then scale.
	if got := m.TransformPoint(Point{X: 5, Y: 7}); got.X != 20 || got.Y != 41 {
		t.Errorf("translate*scale composition: %v", got)
	}
}
