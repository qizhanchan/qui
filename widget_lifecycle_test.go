package qui

import "testing"

type lifecycleSpy struct {
	BaseWidget
	focused  bool
	canceled bool
	dragEnds int
	preedit  string
	leaves   int
}

func newLifecycleSpy() *lifecycleSpy {
	spy := &lifecycleSpy{BaseWidget: NewBaseWidget()}
	spy.SetSelf(spy)
	return spy
}

func (s *lifecycleSpy) Focusable() bool               { return true }
func (s *lifecycleSpy) SetFocused(value bool)         { s.focused = value }
func (s *lifecycleSpy) CancelInteraction()            { s.canceled = true }
func (s *lifecycleSpy) Draggable() bool               { return true }
func (s *lifecycleSpy) SetPreedit(text string, _ int) { s.preedit = text }
func (s *lifecycleSpy) CommitIME(string)              {}
func (s *lifecycleSpy) CaretRect() Rect               { return Rect{} }
func (s *lifecycleSpy) Handle(event Event) bool {
	if event.Type() == EventDragEnd {
		s.dragEnds++
	}
	if event.Type() == EventMouseLeave {
		s.leaves++
	}
	return false
}

func TestSameWindowReparentPreservesActiveInteraction(t *testing.T) {
	child := newLifecycleSpy()
	left := NewContainer(nil, child)
	right := NewContainer(nil)
	root := NewContainer(nil, left, right)
	w := NewTestWindow(Size{W: 200, H: 100})
	w.SetRoot(root)
	w.SetFocus(child)
	w.mouseCaptured = child
	w.dragCandidate = child
	w.dragging = true
	w.hoverPath = []Widget{root, left, child}

	right.AddChild(child)

	if left.ChildCount() != 0 || right.ChildCount() != 1 || child.Parent() != right {
		t.Fatalf("reparent failed: left=%d right=%d parent=%v", left.ChildCount(), right.ChildCount(), child.Parent())
	}
	if w.Focused() != child || !child.focused || w.mouseCaptured != child || w.dragCandidate != child || !w.dragging {
		t.Fatalf("same-window interaction was lost: focus=%v capture=%v drag=%v/%v", w.Focused(), w.mouseCaptured, w.dragCandidate, w.dragging)
	}
	if child.Window() != w || len(w.hoverPath) != 0 || child.leaves == 0 {
		t.Fatalf("move reconciliation failed: window=%v hover=%v leaves=%d", child.Window(), w.hoverPath, child.leaves)
	}
}

func TestCrossWindowSetRootTransfersOwnership(t *testing.T) {
	child := newLifecycleSpy()
	root := NewContainer(nil, child)
	one := NewTestWindow(Size{W: 100, H: 100})
	two := NewTestWindow(Size{W: 100, H: 100})
	one.SetRoot(root)
	one.SetFocus(child)
	one.mouseCaptured = child

	two.SetRoot(root)

	if one.Root() != nil || two.Root() != root || root.Window() != two || child.Window() != two {
		t.Fatalf("ownership transfer failed: oldRoot=%v newRoot=%v rootWindow=%v childWindow=%v", one.Root(), two.Root(), root.Window(), child.Window())
	}
	if one.Focused() != nil || one.mouseCaptured != nil || child.focused || !child.canceled {
		t.Fatalf("old-window interaction survived: focus=%v capture=%v childFocused=%v canceled=%v", one.Focused(), one.mouseCaptured, child.focused, child.canceled)
	}
}

func TestDetachCancelsIMEAndDrag(t *testing.T) {
	child := newLifecycleSpy()
	root := NewContainer(nil, child)
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(root)
	w.SetFocus(child)
	w.SetPreedit("active", 1)
	w.dragCandidate = child
	w.dragging = true

	root.RemoveChild(child)

	if child.preedit != "" || child.dragEnds != 1 || !child.canceled {
		t.Fatalf("detach cleanup: preedit=%q dragEnds=%d canceled=%v", child.preedit, child.dragEnds, child.canceled)
	}
}

func TestSetRootPromotesDescendantWithoutDroppingFocus(t *testing.T) {
	child := newLifecycleSpy()
	oldRoot := NewContainer(nil, child)
	w := NewTestWindow(Size{W: 100, H: 100})
	w.SetRoot(oldRoot)
	w.SetFocus(child)

	w.SetRoot(child)

	if w.Root() != child || child.Parent() != nil || child.Window() != w || w.Focused() != child || !child.focused {
		t.Fatalf("descendant promotion failed: root=%v parent=%v window=%v focus=%v", w.Root(), child.Parent(), child.Window(), w.Focused())
	}
	if oldRoot.Window() != nil {
		t.Fatalf("old root remained attached to %v", oldRoot.Window())
	}
}
