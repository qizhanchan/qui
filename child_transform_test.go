package qui

import "testing"

// translatingHost is a minimal stand-in for a scroll container: it holds one
// child, keeps the child's bounds untouched, and publishes the offset through
// the child-transform contracts. Root can't import widgets, so the ScrollView
// behavior is modeled here.
type translatingHost struct {
	BaseWidget
	child  Widget
	offset Point
	clip   Rect
}

func newTranslatingHost(rect Rect, child Widget, offset Point) *translatingHost {
	h := &translatingHost{BaseWidget: NewBaseWidget(), child: child, offset: offset, clip: rect}
	h.SetSelf(h)
	h.rect = rect
	child.SetParent(h)
	return h
}

func (h *translatingHost) ChildList() []Widget { return []Widget{h.child} }

func (h *translatingHost) ChildInteractionTransform() Matrix {
	return TranslateMatrix(h.offset.X, h.offset.Y)
}

func (h *translatingHost) ChildPaintTransform() Matrix {
	return TranslateMatrix(h.offset.X, h.offset.Y)
}

func (h *translatingHost) PaintClip() Rect { return h.clip }

func (h *translatingHost) HitTest(p Point) Widget {
	if !h.rect.Contains(p) {
		return nil
	}
	local := Point{X: p.X - h.offset.X, Y: p.Y - h.offset.Y}
	if hit := h.child.HitTest(local); hit != nil {
		return hit
	}
	return h
}

// coordSpy records the coordinates every mouse event arrives with.
type coordSpy struct {
	BaseWidget
	seen []Point
}

func newCoordSpy(rect Rect) *coordSpy {
	s := &coordSpy{BaseWidget: NewBaseWidget()}
	s.SetSelf(s)
	s.rect = rect
	return s
}

func (s *coordSpy) Handle(e Event) bool {
	if me, ok := e.(MouseEvent); ok && me.Type() == EventMouseDown {
		s.seen = append(s.seen, Point{X: me.X, Y: me.Y})
	}
	return false
}

func (s *coordSpy) HitTest(p Point) Widget {
	if s.rect.Contains(p) {
		return s
	}
	return nil
}

// A child transform maps retained bounds to screen without moving them, and
// window↔local point mapping round-trips through it.
func TestChildInteractionTransformMapsGeometry(t *testing.T) {
	child := newCoordSpy(Rect{X: 0, Y: 100, W: 200, H: 20})
	host := newTranslatingHost(Rect{W: 200, H: 200}, child, Point{Y: -60})

	if got := child.Bounds().Y; got != 100 {
		t.Fatalf("retained bounds must not move, got Y=%v", got)
	}
	if got := InteractionBoundsOf(child).Y; got != 40 {
		t.Errorf("on-screen Y = %v, want 40", got)
	}
	if got := WindowPointToLocal(child, Point{X: 5, Y: 40}); got.Y != 100 {
		t.Errorf("window→local Y = %v, want 100", got.Y)
	}
	if got := LocalPointToWindow(child, Point{X: 5, Y: 100}); got.Y != 40 {
		t.Errorf("local→window Y = %v, want 40", got.Y)
	}
	_ = host
}

// Dispatch hands each widget the event in its own coordinate space, so a
// widget under a child transform compares against its own bounds unchanged.
func TestDispatchLocalizesEventCoordinates(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	child := newCoordSpy(Rect{X: 0, Y: 100, W: 200, H: 20})
	host := newTranslatingHost(Rect{W: 200, H: 200}, child, Point{Y: -60})
	win.SetRoot(host)

	// Screen Y=45 is inside the child's translated box (40..60).
	win.dispatch(newPhaseEvent(EventMouseDown, 10, 45))
	if len(child.seen) != 1 {
		t.Fatalf("child saw %d events, want 1", len(child.seen))
	}
	if got := child.seen[0].Y; got != 105 {
		t.Errorf("localized Y = %v, want 105 (screen 45 + 60 offset)", got)
	}
	if got := child.seen[0].X; got != 10 {
		t.Errorf("X should be unchanged on a Y-only transform, got %v", got)
	}
}

// A descendant's invalidation maps through the child paint transform and is
// clipped by the host's viewport, so scrolled-out content dirties nothing.
func TestPaintBoundsInWindowClipsAtPaintClipper(t *testing.T) {
	child := newCoordSpy(Rect{X: 0, Y: 100, W: 200, H: 20})
	host := newTranslatingHost(Rect{W: 200, H: 200}, child, Point{Y: -60})

	got := PaintBoundsInWindow(child)
	want := Rect{X: 0, Y: 40, W: 200, H: 20}
	if got != want {
		t.Errorf("dirty rect = %+v, want %+v", got, want)
	}

	// Scroll the child far past the viewport: nothing of it is on screen.
	host.offset = Point{Y: -500}
	if got := PaintBoundsInWindow(child); !got.IsEmpty() {
		t.Errorf("scrolled-out child dirtied %+v, want empty", got)
	}
}
