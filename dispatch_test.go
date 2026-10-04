package qui

import (
	"testing"
	"time"
)

// phaseSpy records (current-target-name, phase) for every Handle call.
type phaseSpy struct {
	BaseWidget
	name string
	log  *[]phaseLogEntry
	// stopOn, when non-zero, requests StopPropagation on Handle when the
	// current phase matches.
	stopOn EventPhase
}

type phaseLogEntry struct {
	name   string
	phase  EventPhase
	evType EventType
}

func newPhaseSpy(name string, r Rect, log *[]phaseLogEntry) *phaseSpy {
	s := &phaseSpy{BaseWidget: NewBaseWidget(), name: name, log: log}
	s.rect = r
	return s
}

func (s *phaseSpy) Handle(e Event) bool {
	*s.log = append(*s.log, phaseLogEntry{name: s.name, phase: e.Phase()})
	if s.stopOn != 0 && e.Phase() == s.stopOn {
		e.StopPropagation()
	}
	return false
}

func (s *phaseSpy) HitTest(p Point) Widget {
	if s.rect.Contains(p) {
		return s
	}
	return nil
}

// Measure reports the spy's pre-set rect size so widgets that Layout
// us (Popup / Dialog / ScrollView) see a non-zero natural size.
func (s *phaseSpy) Measure(available Size) Size {
	return Size{W: s.rect.W, H: s.rect.H}
}

func newPhaseEvent(evType EventType, x, y float32) MouseEvent {
	return MouseEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: evType,
		When:      time.Now(),
		X:         x,
		Y:         y,
		Button:    MouseButtonLeft,
	}
}

func TestDispatchTargetFiresOnce(t *testing.T) {
	// Plain Containers have no-op Handle, so only leaf logs.
	log := []phaseLogEntry{}
	leaf := newPhaseSpy("leaf", Rect{X: 10, Y: 10, W: 50, H: 50}, &log)
	midC := NewContainer(nil, leaf)
	midC.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	rootC := NewContainer(nil, midC)
	rootC.rect = Rect{X: 0, Y: 0, W: 100, H: 100}

	w := &Window{root: rootC, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseDown, 20, 20))

	want := []phaseLogEntry{{name: "leaf", phase: PhaseTarget}}
	if len(log) != len(want) || log[0] != want[0] {
		t.Fatalf("phase log = %+v, want %+v", log, want)
	}
}

// phaseSpyContainer is a Container whose Handle logs phases (to verify
// capture and bubble run on ancestors).
type phaseSpyContainer struct {
	Container
	name string
	log  *[]phaseLogEntry
}

func newPhaseSpyContainer(name string, r Rect, log *[]phaseLogEntry, children ...Widget) *phaseSpyContainer {
	c := &phaseSpyContainer{name: name, log: log}
	c.BaseWidget = NewBaseWidget()
	c.rect = r
	// Register self so AddChild records this subclass as the parent,
	// HitTest returns this subclass, and the framework calls the
	// overridden Handle on the path.
	c.SetSelf(c)
	for _, child := range children {
		c.AddChild(child)
	}
	return c
}

func (c *phaseSpyContainer) Handle(e Event) bool {
	*c.log = append(*c.log, phaseLogEntry{name: c.name, phase: e.Phase()})
	return false
}

func TestDispatchCapturePhaseBeforeTargetBeforeBubble(t *testing.T) {
	log := []phaseLogEntry{}
	leaf := newPhaseSpy("leaf", Rect{X: 10, Y: 10, W: 50, H: 50}, &log)
	mid := newPhaseSpyContainer("mid", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, leaf)
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, mid)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseDown, 20, 20))

	want := []phaseLogEntry{
		{name: "root", phase: PhaseCapture},
		{name: "mid", phase: PhaseCapture},
		{name: "leaf", phase: PhaseTarget},
		{name: "mid", phase: PhaseBubble},
		{name: "root", phase: PhaseBubble},
	}
	if len(log) != len(want) {
		t.Fatalf("log length = %d, want %d; log=%+v", len(log), len(want), log)
	}
	for i, w := range want {
		if log[i] != w {
			t.Errorf("step %d: got %+v want %+v", i, log[i], w)
		}
	}
}

func TestDispatchStopPropagationHaltsBubble(t *testing.T) {
	log := []phaseLogEntry{}
	leaf := newPhaseSpy("leaf", Rect{X: 10, Y: 10, W: 50, H: 50}, &log)
	leaf.stopOn = PhaseTarget // leaf stops after handling
	mid := newPhaseSpyContainer("mid", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, leaf)
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, mid)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseDown, 20, 20))

	want := []phaseLogEntry{
		{name: "root", phase: PhaseCapture},
		{name: "mid", phase: PhaseCapture},
		{name: "leaf", phase: PhaseTarget},
	}
	if len(log) != len(want) {
		t.Fatalf("log length = %d, want %d; log=%+v", len(log), len(want), log)
	}
	for i, w := range want {
		if log[i] != w {
			t.Errorf("step %d: got %+v want %+v", i, log[i], w)
		}
	}
}

func TestDispatchStopPropagationInCaptureSkipsTarget(t *testing.T) {
	log := []phaseLogEntry{}
	leaf := newPhaseSpy("leaf", Rect{X: 10, Y: 10, W: 50, H: 50}, &log)
	mid := newPhaseSpyContainer("mid", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, leaf)
	stopRoot := newStopCaptureContainer(&log, mid)

	w := &Window{root: stopRoot, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseDown, 20, 20))

	// Expect only stopRoot's capture entry; no mid, no leaf, no bubble.
	if len(log) != 1 {
		t.Fatalf("expected single capture entry at stopRoot, got %+v", log)
	}
	if log[0].name != "stopRoot" || log[0].phase != PhaseCapture {
		t.Errorf("expected stopRoot/capture, got %+v", log[0])
	}
}

type stopCaptureContainer struct {
	phaseSpyContainer
}

func newStopCaptureContainer(log *[]phaseLogEntry, children ...Widget) *stopCaptureContainer {
	c := &stopCaptureContainer{}
	c.BaseWidget = NewBaseWidget()
	c.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	c.name = "stopRoot"
	c.log = log
	// Self MUST point at the outer (stopCaptureContainer), so path
	// walking and HitTest return this type — otherwise the overridden
	// Handle below is never reached.
	c.SetSelf(c)
	for _, ch := range children {
		c.AddChild(ch)
	}
	return c
}

func (c *stopCaptureContainer) Handle(e Event) bool {
	*c.log = append(*c.log, phaseLogEntry{name: "stopRoot", phase: e.Phase()})
	if e.Phase() == PhaseCapture {
		e.StopPropagation()
	}
	return false
}

func TestDispatchReturnTrueImpliesStopPropagation(t *testing.T) {
	// A widget returning true from Handle should halt propagation, for
	// backward compatibility with pre-phased handlers.
	log := []phaseLogEntry{}
	leaf := &returnTrueSpy{BaseWidget: NewBaseWidget(), name: "leaf", log: &log}
	leaf.rect = Rect{X: 10, Y: 10, W: 50, H: 50}
	mid := newPhaseSpyContainer("mid", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, leaf)
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, mid)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseDown, 20, 20))

	// Expect: root capture, mid capture, leaf target (returns true),
	// then NO bubble.
	want := []phaseLogEntry{
		{name: "root", phase: PhaseCapture},
		{name: "mid", phase: PhaseCapture},
		{name: "leaf", phase: PhaseTarget},
	}
	if len(log) != len(want) {
		t.Fatalf("log length = %d, want %d; log=%+v", len(log), len(want), log)
	}
}

type returnTrueSpy struct {
	BaseWidget
	name string
	log  *[]phaseLogEntry
}

func (s *returnTrueSpy) Handle(e Event) bool {
	*s.log = append(*s.log, phaseLogEntry{name: s.name, phase: e.Phase()})
	return true
}

func (s *returnTrueSpy) HitTest(p Point) Widget {
	if s.rect.Contains(p) {
		return s
	}
	return nil
}

func TestDispatchTargetIsHitWidget(t *testing.T) {
	log := []phaseLogEntry{}
	leaf := newPhaseSpy("leaf", Rect{X: 10, Y: 10, W: 50, H: 50}, &log)
	mid := newPhaseSpyContainer("mid", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, leaf)
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 100, H: 100}, &log, mid)

	w := &Window{root: root, lastSize: Size{W: 100, H: 100}}

	// Dispatch and capture state.target via Handle's event argument.
	ev := newPhaseEvent(EventMouseDown, 20, 20)
	w.dispatch(ev)

	if ev.state() == nil {
		t.Fatal("event state should not be nil")
	}
	if ev.state().target != leaf {
		t.Errorf("target = %v, want leaf", ev.state().target)
	}
}

func TestDispatchMouseMoveReachesOnlyHitPath(t *testing.T) {
	// Two leaves in disjoint positions. MouseMove is now path-based:
	// only widgets on the hit path (a + ancestors) should receive the
	// move. b, outside the cursor, should receive no MouseMove.
	log := []phaseLogEntry{}
	a := newPhaseSpy("a", Rect{X: 0, Y: 0, W: 50, H: 50}, &log)
	b := newPhaseSpy("b", Rect{X: 100, Y: 100, W: 50, H: 50}, &log)
	root := newPhaseSpyContainer("root", Rect{X: 0, Y: 0, W: 200, H: 200}, &log, a, b)

	w := &Window{root: root, lastSize: Size{W: 200, H: 200}}
	w.dispatch(newPhaseEvent(EventMouseMove, 20, 20))

	// b must never appear.
	for _, l := range log {
		if l.name == "b" {
			t.Errorf("MouseMove must not reach b (outside cursor); log=%+v", log)
		}
	}
	// a must appear (it's the target).
	found := false
	for _, l := range log {
		if l.name == "a" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("MouseMove should reach hit target a; log=%+v", log)
	}
}

func TestDispatchMouseEnterFiresOnHoverPath(t *testing.T) {
	// When cursor moves onto a widget for the first time, MouseEnter
	// fires on it and each ancestor. Outer enters first (root-first).
	log := []phaseLogEntry{}
	leaf := &eventTypeSpy{log: &log}
	leaf.BaseWidget = NewBaseWidget()
	leaf.rect = Rect{X: 10, Y: 10, W: 50, H: 50}
	leaf.name = "leaf"

	rootC := &eventTypeSpyContainer{log: &log}
	rootC.BaseWidget = NewBaseWidget()
	rootC.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	rootC.name = "root"
	rootC.SetSelf(rootC)
	rootC.AddChild(leaf)

	w := &Window{root: rootC, lastSize: Size{W: 100, H: 100}}
	w.dispatch(newPhaseEvent(EventMouseMove, 20, 20))

	// First two events should be MouseEnter (root, then leaf).
	if len(log) < 2 {
		t.Fatalf("expected at least 2 events, got %+v", log)
	}
	if log[0].name != "root" || log[0].evType != EventMouseEnter {
		t.Errorf("first event: got %+v want root/enter", log[0])
	}
	if log[1].name != "leaf" || log[1].evType != EventMouseEnter {
		t.Errorf("second event: got %+v want leaf/enter", log[1])
	}
}

func TestDispatchMouseLeaveFiresOnExit(t *testing.T) {
	// After hovering a leaf, moving the cursor to empty space fires
	// MouseLeave on the leaf (and any other widget no longer hovered).
	log := []phaseLogEntry{}
	leaf := &eventTypeSpy{log: &log}
	leaf.BaseWidget = NewBaseWidget()
	leaf.rect = Rect{X: 10, Y: 10, W: 40, H: 40}
	leaf.name = "leaf"

	rootC := &eventTypeSpyContainer{log: &log}
	rootC.BaseWidget = NewBaseWidget()
	rootC.rect = Rect{X: 0, Y: 0, W: 100, H: 100}
	rootC.name = "root"
	rootC.SetSelf(rootC)
	rootC.AddChild(leaf)

	w := &Window{root: rootC, lastSize: Size{W: 100, H: 100}}
	// First move into the leaf.
	w.dispatch(newPhaseEvent(EventMouseMove, 20, 20))
	log = log[:0] // clear
	// Now move outside everything.
	w.dispatch(newPhaseEvent(EventMouseMove, 500, 500))

	// Expect MouseLeave on leaf (leaf-first) then on root.
	if len(log) < 2 {
		t.Fatalf("expected 2 leave events, got %+v", log)
	}
	if log[0].name != "leaf" || log[0].evType != EventMouseLeave {
		t.Errorf("first leave: got %+v want leaf/leave", log[0])
	}
	if log[1].name != "root" || log[1].evType != EventMouseLeave {
		t.Errorf("second leave: got %+v want root/leave", log[1])
	}
}

// eventTypeSpy records (name, eventType) — used for hover tests where
// we care about what kind of event was received, not the phase.
type eventTypeSpy struct {
	BaseWidget
	name string
	log  *[]phaseLogEntry
}

func (s *eventTypeSpy) Handle(e Event) bool {
	*s.log = append(*s.log, phaseLogEntry{name: s.name, evType: e.Type()})
	return false
}

func (s *eventTypeSpy) HitTest(p Point) Widget {
	if s.rect.Contains(p) {
		return s
	}
	return nil
}

type eventTypeSpyContainer struct {
	Container
	name string
	log  *[]phaseLogEntry
}

func (c *eventTypeSpyContainer) Handle(e Event) bool {
	*c.log = append(*c.log, phaseLogEntry{name: c.name, evType: e.Type()})
	return false
}

func TestPathToRootWalksParents(t *testing.T) {
	a := newPhaseSpy("a", Rect{}, nil)
	b := newPhaseSpy("b", Rect{}, nil)
	c := newPhaseSpy("c", Rect{}, nil)
	a.SetParent(b)
	b.SetParent(c)

	path := pathToRoot(a)
	if len(path) != 3 || path[0] != c || path[1] != b || path[2] != a {
		t.Errorf("pathToRoot = %+v, want [c, b, a]", path)
	}
}

func TestContainerAddChildWiresParent(t *testing.T) {
	child := newPhaseSpy("child", Rect{}, nil)
	c := NewContainer(nil, child)
	if child.Parent() != c {
		t.Errorf("child.Parent() = %v, want container", child.Parent())
	}
}
