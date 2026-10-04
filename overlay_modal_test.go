package qui

import (
	"testing"
	"time"
)

// keySpyModal is a full-window modal overlay that records the key events
// it receives as their target.
type keySpyModal struct {
	*Container
	keys int
}

func newKeySpyModal() *keySpyModal {
	m := &keySpyModal{Container: NewContainer(nil)}
	m.SetSelf(m)
	return m
}

func (m *keySpyModal) Modal() bool { return true }

func (m *keySpyModal) Handle(e Event) bool {
	if _, ok := e.(KeyEvent); ok && e.Phase() == PhaseTarget {
		m.keys++
		return true
	}
	return false
}

// keySpy is a focusable main-tree widget that counts the keys it sees.
type keySpy struct {
	*Container
	keys int
}

func newKeySpy() *keySpy {
	s := &keySpy{Container: NewContainer(nil)}
	s.SetSelf(s)
	return s
}

func (s *keySpy) Focusable() bool { return true }
func (s *keySpy) SetFocused(bool) {}
func (s *keySpy) Handle(e Event) bool {
	if _, ok := e.(KeyEvent); ok {
		s.keys++
	}
	return false
}

func TestModalBlocksAcceleratorsAndRootKeys(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	reg := NewAcceleratorRegistry()
	fired := 0
	_ = reg.Register("Cmd+S", func() { fired++ })
	win.SetAcceleratorRegistry(reg)
	root := newKeySpy()
	win.SetRoot(root)

	modal := newKeySpyModal()
	modal.Layout(Rect{W: 200, H: 200})
	win.PushOverlay(modal)
	win.SetFocus(nil)

	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyS, ModSuper))
	if fired != 0 {
		t.Errorf("accelerator fired %d times behind a modal, want 0", fired)
	}
	if root.keys != 0 {
		t.Errorf("main root saw %d key events behind a modal, want 0", root.keys)
	}
	if modal.keys != 1 {
		t.Errorf("modal received %d key events, want 1", modal.keys)
	}

	win.RemoveOverlay(modal)
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyS, ModSuper))
	if fired != 1 {
		t.Errorf("accelerator fired %d times after the modal closed, want 1", fired)
	}
}

func TestModalRetargetsKeysFocusedBehindIt(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	root := newKeySpy()
	win.SetRoot(root)
	modal := newKeySpyModal()
	modal.Layout(Rect{W: 200, H: 200})
	win.PushOverlay(modal)
	// Programmatic focus into the main tree while the modal is up.
	win.SetFocus(root)

	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyA, 0))
	if root.keys != 0 {
		t.Errorf("widget behind the modal saw %d keys, want 0", root.keys)
	}
	if modal.keys != 1 {
		t.Errorf("modal received %d keys, want 1", modal.keys)
	}
}

// relayoutSpy is an overlay implementing OverlayLayouter.
type relayoutSpy struct {
	*Container
	calls int
	size  Size
}

func (r *relayoutSpy) RelayoutOverlay(s Size) { r.calls++; r.size = s }

func TestLayoutDirtyOverlayIsRelaidOut(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 200})
	win.SetRoot(NewContainer(nil))
	ov := &relayoutSpy{Container: NewContainer(nil)}
	ov.SetSelf(ov)
	child := NewContainer(nil)
	ov.AddChild(child)
	ov.Layout(Rect{W: 300, H: 200})
	win.PushOverlay(ov)
	ov.ClearLayoutDirty()

	win.relayoutDirtyOverlays()
	if ov.calls != 0 {
		t.Fatalf("clean overlay relaid out %d times, want 0", ov.calls)
	}

	child.InvalidateLayout() // content changed while shown
	win.relayoutDirtyOverlays()
	if ov.calls != 1 || ov.size != (Size{W: 300, H: 200}) {
		t.Errorf("calls=%d size=%v, want 1 call with the window size", ov.calls, ov.size)
	}
	if ov.IsLayoutDirty() {
		t.Error("overlay still layout-dirty after relayout")
	}

	// A window-wide invalidation (theme / locale switch) reaches overlays.
	win.InvalidateLayout()
	win.relayoutDirtyOverlays()
	if ov.calls != 2 {
		t.Errorf("Window.InvalidateLayout did not relayout the overlay (calls=%d)", ov.calls)
	}
}

// tickCounter counts ticks.
type tickCounter struct {
	BaseWidget
	n int
}

func (c *tickCounter) Tick(time.Time) Rect { c.n++; return Rect{} }

func TestTickWidgetRecursesThroughNonTickableContainers(t *testing.T) {
	leaf := &tickCounter{BaseWidget: NewBaseWidget()}
	// A plain parent that is NOT Tickable but lists children (a
	// ScrollView-like wrapper).
	wrapper := &childListWrapper{BaseWidget: NewBaseWidget(), kids: []Widget{leaf}}
	TickWidget(wrapper, time.Now())
	if leaf.n != 1 {
		t.Errorf("leaf ticked %d times through a non-Tickable wrapper, want 1", leaf.n)
	}
}

type childListWrapper struct {
	BaseWidget
	kids []Widget
}

func (c *childListWrapper) ChildList() []Widget { return c.kids }
