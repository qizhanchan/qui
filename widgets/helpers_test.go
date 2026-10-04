package widgets

import (
	. "github.com/qizhanchan/qui"
)

// phaseSpy is a stand-in for newPhaseSpy from the root package's
// dispatch_test.go — inline here so widgets tests don't need root's
// unexported test helpers. Tracks dispatch phases if the caller wires
// a log pointer, otherwise just answers HitTest based on its rect.
type phaseSpy struct {
	BaseWidget
	name    string
	stopOn  EventPhase
	hitRect Rect
}

func newPhaseSpy(name string, r Rect, _ *[]int) *phaseSpy {
	s := &phaseSpy{BaseWidget: NewBaseWidget(), name: name, hitRect: r}
	s.SetSelf(s)
	s.Layout(r)
	return s
}

// Measure returns the spy's configured hit rect as its intrinsic size so
// Popup.ShowAt / Dialog.Show compute a non-zero content box from it.
// (Root's dispatch_test.go sets b.rect directly via its package-private
// access; from widgets we reach the same end state by honoring the rect
// through Measure + Layout.)
func (s *phaseSpy) Measure(Size) Size { return Size{W: s.hitRect.W, H: s.hitRect.H} }

// HitTest consults the widget's CURRENT bounds (set by Layout), not the
// rect frozen at construction — so tests that re-layout the spy see the
// updated hit region.
func (s *phaseSpy) HitTest(p Point) Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

func (s *phaseSpy) Name() string { return s.name }

// focusableSpy is a widget that reports itself as focusable, used by
// tests that exercise focus collection / cycling inside overlays.
type focusableSpy struct {
	BaseWidget
	name    string
	focused bool
}

func newFocusableSpy(name string) *focusableSpy {
	s := &focusableSpy{BaseWidget: NewBaseWidget(), name: name}
	s.SetSelf(s)
	return s
}

func (s *focusableSpy) Focusable() bool   { return true }
func (s *focusableSpy) SetFocused(f bool) { s.focused = f }

// windowWithRoot is the common "test window with a tree attached" pattern.
// Returns a Window sized to fit the tests' layout expectations.
func windowWithRoot(size Size, root Widget) *Window {
	w := NewTestWindow(size)
	w.SetRoot(root)
	return w
}
