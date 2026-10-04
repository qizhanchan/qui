package qui

import (
	"testing"
	"time"
)

// focusableSpy is a minimal widget that participates in focus.
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

func newKeyEvent(key Key, mods Modifiers) KeyEvent {
	return KeyEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: EventKeyDown,
		When:      time.Now(),
		Key:       key,
		Mods:      mods,
	}
}

func TestCollectFocusablesDFSOrder(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	c := newFocusableSpy("c")
	inner := NewContainer(nil, b, c)
	root := NewContainer(nil, a, inner)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	list := w.CollectFocusables()

	if len(list) != 3 {
		t.Fatalf("expected 3 focusables, got %d", len(list))
	}
	if list[0] != a || list[1] != b || list[2] != c {
		t.Errorf("DFS order wrong: got [%v, %v, %v]", list[0], list[1], list[2])
	}
}

func TestCollectFocusablesSkipsDisabled(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	b.SetEnabled(false)
	root := NewContainer(nil, a, b)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	list := w.CollectFocusables()

	if len(list) != 1 || list[0] != a {
		t.Errorf("disabled widget should not appear: got %+v", list)
	}
}

type hiddenFocusableSpy struct {
	focusableSpy
	hidden bool
}

func newHiddenFocusableSpy(name string) *hiddenFocusableSpy {
	s := &hiddenFocusableSpy{focusableSpy: *newFocusableSpy(name)}
	s.SetSelf(s)
	return s
}

func (s *hiddenFocusableSpy) VisibilityHidden() bool { return s.hidden }

func TestHiddenSubtreeIsNotHitOrFocusable(t *testing.T) {
	hidden := newHiddenFocusableSpy("hidden")
	hidden.hidden = true
	hidden.Layout(Rect{X: 0, Y: 0, W: 50, H: 50})
	root := NewContainer(nil, hidden)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}

	if got := root.HitTest(Point{X: 10, Y: 10}); got == hidden {
		t.Fatal("visibility-hidden child remained hit-testable")
	}
	if got := w.CollectFocusables(); len(got) != 0 {
		t.Fatalf("visibility-hidden child remained focusable: %v", got)
	}
	w.SetFocus(hidden)
	if w.Focused() != nil {
		t.Fatal("SetFocus accepted a visibility-hidden target")
	}
}

func TestRemoveFocusedSubtreeClearsWindowInteractionState(t *testing.T) {
	child := newFocusableSpy("child")
	child.Layout(Rect{X: 0, Y: 0, W: 50, H: 50})
	root := NewContainer(nil, child)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)
	w.SetFocus(child)
	w.mouseCaptured = child
	w.gestureTarget = child
	w.dragCandidate = child
	w.dragging = true
	w.hoverPath = []Widget{root, child}

	root.RemoveChild(child)

	if w.Focused() != nil || child.focused {
		t.Fatalf("detached focus survived: window=%v child=%v", w.Focused(), child.focused)
	}
	if w.mouseCaptured != nil || w.gestureTarget != nil || w.dragCandidate != nil || w.dragging {
		t.Fatalf("detached input state survived: mouse=%v gesture=%v drag=%v dragging=%v",
			w.mouseCaptured, w.gestureTarget, w.dragCandidate, w.dragging)
	}
	if len(w.hoverPath) != 1 || w.hoverPath[0] != root {
		t.Fatalf("hover path after detach = %v, want root only", w.hoverPath)
	}
}

func TestCollapseFocusedParentClearsFocus(t *testing.T) {
	child := newFocusableSpy("child")
	parent := NewContainer(nil, child)
	parent.SetSelf(parent)
	root := NewContainer(nil, parent)
	root.SetSelf(root)
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)
	w.SetFocus(child)

	parent.SetCollapsed(true)

	if w.Focused() != nil || child.focused {
		t.Fatalf("focus survived collapsed ancestor: window=%v child=%v", w.Focused(), child.focused)
	}
	if got := w.CollectFocusables(); len(got) != 0 {
		t.Fatalf("collapsed subtree remained in tab order: %v", got)
	}
}

func TestFocusNextAdvancesAndWraps(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	c := newFocusableSpy("c")
	root := NewContainer(nil, a, b, c)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}

	w.FocusNext() // nil -> a
	if w.focused != a || !a.focused {
		t.Errorf("first Next: expected a focused, got %v", w.focused)
	}
	w.FocusNext() // a -> b
	if w.focused != b || !b.focused || a.focused {
		t.Errorf("second Next: expected b focused, a unfocused; got a=%v b=%v", a.focused, b.focused)
	}
	w.FocusNext() // b -> c
	w.FocusNext() // c -> a (wrap)
	if w.focused != a {
		t.Errorf("wrap failed, expected a, got %v", w.focused)
	}
}

func TestFocusPrevFromNilStartsAtLast(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	c := newFocusableSpy("c")
	root := NewContainer(nil, a, b, c)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.FocusPrev() // nil -> c (last)

	if w.focused != c {
		t.Errorf("FocusPrev from nil should start at last; got %v", w.focused)
	}
}

func TestTabKeyNavigatesFocus(t *testing.T) {
	a := newFocusableSpy("a")
	b := newFocusableSpy("b")
	root := NewContainer(nil, a, b)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}

	w.dispatch(newKeyEvent(KeyTab, 0))
	if w.focused != a {
		t.Errorf("Tab from nil should focus a; got %v", w.focused)
	}
	w.dispatch(newKeyEvent(KeyTab, 0))
	if w.focused != b {
		t.Errorf("second Tab should focus b; got %v", w.focused)
	}
	w.dispatch(newKeyEvent(KeyTab, ModShift))
	if w.focused != a {
		t.Errorf("Shift+Tab should go back to a; got %v", w.focused)
	}
}

func TestTabKeyConsumedNotDispatchedToWidget(t *testing.T) {
	// A focused widget should NOT see Tab in its Handle — the window
	// intercepts. Otherwise TextArea would try to insert a literal \t.
	spy := &focusableKeySpy{focusableSpy: *newFocusableSpy("spy")}
	spy.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	root := NewContainer(nil, spy)
	root.rect = Rect{X: 0, Y: 0, W: 100, H: 100}

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}, focused: spy}
	spy.SetFocused(true)

	w.dispatch(newKeyEvent(KeyTab, 0))

	if spy.sawTab {
		t.Error("focused widget received Tab; window should intercept")
	}
}

type focusableKeySpy struct {
	focusableSpy
	sawTab bool
}

func (s *focusableKeySpy) Handle(e Event) bool {
	if ke, ok := e.(KeyEvent); ok && ke.Key == KeyTab {
		s.sawTab = true
	}
	return false
}

func TestClearFocusBlursCurrentWidget(t *testing.T) {
	a := newFocusableSpy("a")
	root := NewContainer(nil, a)
	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}

	w.FocusNext()
	if w.focused != a || !a.focused {
		t.Fatalf("setup: a should be focused")
	}
	w.ClearFocus()
	if w.focused != nil {
		t.Errorf("ClearFocus: focused should be nil, got %v", w.focused)
	}
	if a.focused {
		t.Errorf("ClearFocus: a.focused should be false, got true")
	}
}

func TestNextFocusIndexWrap(t *testing.T) {
	list := []Widget{newFocusableSpy("0"), newFocusableSpy("1"), newFocusableSpy("2")}
	// From last, forward wraps to 0.
	if got := nextFocusIndex(list, list[2], 1); got != 0 {
		t.Errorf("forward wrap: got %d want 0", got)
	}
	// From 0, backward wraps to last.
	if got := nextFocusIndex(list, list[0], -1); got != 2 {
		t.Errorf("backward wrap: got %d want 2", got)
	}
	// From nil, forward -> 0.
	if got := nextFocusIndex(list, nil, 1); got != 0 {
		t.Errorf("nil forward: got %d want 0", got)
	}
	// From nil, backward -> last.
	if got := nextFocusIndex(list, nil, -1); got != 2 {
		t.Errorf("nil backward: got %d want 2", got)
	}
}
