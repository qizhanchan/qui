package qui

import (
	"testing"
	"time"
)

// masonryLayout is a custom engine: two columns, items stacked into the
// shorter one. It implements LayoutMeasurer.
type masonryLayout struct{ gap float32 }

func (m masonryLayout) Apply(children []Widget, b Rect) {
	colW := (b.W - m.gap) / 2
	heights := [2]float32{}
	for _, c := range children {
		col := 0
		if heights[1] < heights[0] {
			col = 1
		}
		s := c.Measure(Size{W: colW})
		c.Layout(Rect{X: b.X + float32(col)*(colW+m.gap), Y: b.Y + heights[col], W: colW, H: s.H})
		heights[col] += s.H + m.gap
	}
}

func (m masonryLayout) Measure(children []Widget, avail Size) Size {
	heights := [2]float32{}
	for _, c := range children {
		col := 0
		if heights[1] < heights[0] {
			col = 1
		}
		heights[col] += c.Measure(Size{W: avail.W / 2}).H + m.gap
	}
	h := max(heights[0], heights[1]) - m.gap
	return Size{W: avail.W, H: h}
}

func TestCustomLayoutMeasurer(t *testing.T) {
	c := NewContainer(masonryLayout{gap: 10}, newSized(10, 30), newSized(10, 50), newSized(10, 40))
	c.Style().Padding = Insets{Top: 5, Bottom: 5}
	got := c.Measure(Size{W: 200, H: 1000})
	// col0: 30 + 10 + 40, col1: 50 → 80, plus 10px padding.
	if got.H != 90 {
		t.Fatalf("custom measurer height = %v, want 90", got.H)
	}
}

func TestPointerFlexLayoutMeasures(t *testing.T) {
	c := NewContainer(&FlexLayout{Direction: Vertical, Gap: 4}, newSized(10, 20), newSized(10, 20))
	got := c.Measure(Size{W: 300, H: 1000})
	v := NewContainer(FlexLayout{Direction: Vertical, Gap: 4}, newSized(10, 20), newSized(10, 20))
	if want := v.Measure(Size{W: 300, H: 1000}); got != want || got.H >= 1000 {
		t.Fatalf("&FlexLayout{} measured %v, want %v like the value form", got, want)
	}
}

func TestEventFilterConsumesBeforeDispatch(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	root := newKeySpy()
	win.SetRoot(root)
	win.SetFocus(root)
	var seen []EventType
	remove := win.AddEventFilter(func(e Event) bool {
		seen = append(seen, e.Type())
		ke, ok := e.(KeyEvent)
		return ok && ke.Key == KeyP
	})
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyP, 0))
	if root.keys != 0 {
		t.Fatalf("filtered key reached the widget")
	}
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyQ, 0))
	if root.keys != 1 {
		t.Fatalf("unfiltered key didn't reach the widget: %d", root.keys)
	}
	remove()
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyP, 0))
	if root.keys != 2 || len(seen) != 2 {
		t.Fatalf("after remove: keys=%d seen=%d", root.keys, len(seen))
	}
}

type customSpy struct {
	*Container
	got   []string
	stop  bool
	phase []EventPhase
}

func newCustomSpy() *customSpy {
	s := &customSpy{Container: NewContainer(nil)}
	s.SetSelf(s)
	return s
}

func (s *customSpy) Handle(e Event) bool {
	if ce, ok := e.(CustomEvent); ok {
		s.got = append(s.got, ce.Name)
		s.phase = append(s.phase, e.Phase())
		return s.stop
	}
	return false
}

func TestCustomEventBubbles(t *testing.T) {
	outer, inner := newCustomSpy(), newCustomSpy()
	leaf := newCustomSpy()
	outer.AddChild(inner)
	inner.AddChild(leaf)
	if DispatchCustomEvent(leaf, "row.renamed", 3) {
		t.Fatal("nobody consumed it, but DispatchCustomEvent reported true")
	}
	if len(outer.got) != 2 || outer.phase[0] != PhaseCapture || outer.phase[1] != PhaseBubble {
		t.Fatalf("outer saw %v %v, want capture + bubble", outer.got, outer.phase)
	}
	inner.stop = true
	outer.got, outer.phase = nil, nil
	if !DispatchCustomEvent(leaf, "row.renamed", 3) {
		t.Fatal("inner consumed it, want true")
	}
	if len(outer.got) != 1 {
		t.Fatalf("outer should only see the capture phase after inner stops: %v", outer.phase)
	}
}

func TestAcceleratorPrecedence(t *testing.T) {
	reg := NewAcceleratorRegistry()
	var log []string
	_ = reg.Register("Cmd+K", func() { log = append(log, "app") })
	removePanel, _ := reg.Bind("Cmd+K", func() { log = append(log, "panel") })
	reg.Match(NewKeyEvent(EventKeyDown, KeyK, ModSuper))
	removePanel()
	reg.Match(NewKeyEvent(EventKeyDown, KeyK, ModSuper))
	if len(log) != 2 || log[0] != "panel" || log[1] != "app" {
		t.Fatalf("newest-wins / remove: %v", log)
	}

	scope := NewContainer(nil)
	inside := newSized(1, 1)
	scope.AddChild(inside)
	outside := newSized(1, 1)
	_, _ = reg.RegisterScoped("Cmd+K", scope, func() { log = append(log, "scoped") })
	reg.MatchFor(NewKeyEvent(EventKeyDown, KeyK, ModSuper), inside)
	reg.MatchFor(NewKeyEvent(EventKeyDown, KeyK, ModSuper), outside)
	if log[2] != "scoped" || log[3] != "app" {
		t.Fatalf("scope precedence: %v", log)
	}
	if n := reg.Unregister("Command+K"); n != 2 {
		t.Fatalf("Unregister removed %d, want 2", n)
	}
	if reg.Match(NewKeyEvent(EventKeyDown, KeyK, ModSuper)) {
		t.Fatal("Cmd+K still bound after Unregister")
	}
}

func TestCmdOrCtrlToken(t *testing.T) {
	_, mods, err := ParseShortcut("CmdOrCtrl+Shift+S")
	if err != nil || mods != CommandMod()|ModShift {
		t.Fatalf("CmdOrCtrl parsed to %v (%v)", mods, err)
	}
}

func TestScopedAcceleratorFiresInsideModal(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	win.SetRoot(newKeySpy())
	modal := newKeySpyModal()
	modal.Layout(Rect{W: 200, H: 200})
	win.PushOverlay(modal)
	global, scoped := 0, 0
	reg := win.Accelerators()
	_ = reg.Register("Cmd+Enter", func() { global++ })
	_, _ = reg.RegisterScoped("Cmd+Enter", modal, func() { scoped++ })
	// keySpyModal consumes every key itself, so drive the accelerator
	// pass dispatch runs for an unconsumed key behind / inside a modal.
	reg.match(NewKeyEvent(EventKeyDown, KeyEnter, ModSuper), modal, true)
	if scoped != 1 || global != 0 {
		t.Fatalf("inside modal: scoped=%d global=%d", scoped, global)
	}
}

func TestCloseRequestVeto(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	allow := false
	w.OnCloseRequest(func() bool { return allow })
	w.RequestClose()
	if !w.ShouldClose() || !w.closeVetoed() {
		t.Fatal("handler returning false should veto")
	}
	if w.ShouldClose() {
		t.Fatal("veto must clear the close flag")
	}
	w.Close()
	if w.closeVetoed() {
		t.Fatal("Close is not vetoable")
	}
	w.life.forceClose = false
	allow = true
	w.RequestClose()
	if w.closeVetoed() {
		t.Fatal("handler returning true must allow")
	}
}

func TestOwnedWindowClosesWithOwner(t *testing.T) {
	parent := newWindowOnFake(newFakePlatformWindow())
	child := newWindowOnFake(newFakePlatformWindow())
	_ = child.SetOwner(parent) // fake has no native window: unsupported, but ownership holds
	if child.Owner() != parent {
		t.Fatal("Owner not recorded")
	}
	child.OnCloseRequest(func() bool { return false })
	parent.Destroy()
	if !child.ShouldClose() || child.closeVetoed() {
		t.Fatal("owner closing must force-close the owned window")
	}
}

func TestMoveAndMinimizeCallbacks(t *testing.T) {
	f := newFakePlatformWindow()
	w := newWindowOnFake(f)
	var moved []int
	var minimized []bool
	w.OnMove(func(x, y int) { moved = append(moved, x, y) })
	w.OnMinimize(func(m bool) { minimized = append(minimized, m) })
	w.pollLifecycle()
	f.posX, f.posY = 50, 60
	f.iconified = true
	w.pollLifecycle()
	if len(moved) != 2 || moved[0] != 50 || moved[1] != 60 {
		t.Fatalf("OnMove got %v", moved)
	}
	if len(minimized) != 1 || !minimized[0] {
		t.Fatalf("OnMinimize got %v", minimized)
	}
	var active []bool
	w.OnActivate(func(a bool) { active = append(active, a) })
	w.onFocus(false)
	if len(active) != 1 || active[0] {
		t.Fatalf("OnActivate got %v", active)
	}
}

// dndWidget is a draggable + droppable leaf that logs drag events.
type dndWidget struct {
	BaseWidget
	log    []EventType
	accept func(*DragData) bool
	data   *DragData
	ended  DragEvent
}

func newDnd(r Rect) *dndWidget {
	d := &dndWidget{BaseWidget: NewBaseWidget()}
	d.SetSelf(d)
	d.Layout(r)
	return d
}

func (d *dndWidget) Draggable() bool { return d.data != nil }
func (d *dndWidget) Droppable() bool { return d.accept != nil }
func (d *dndWidget) DragData() *DragData {
	return d.data
}
func (d *dndWidget) AcceptsDrop(data *DragData) bool { return d.accept(data) }
func (d *dndWidget) Handle(e Event) bool {
	de, ok := e.(DragEvent)
	if !ok {
		return false
	}
	d.log = append(d.log, de.Type())
	if de.Type() == EventDragEnd {
		d.ended = de
	}
	return de.Type() == EventDrop
}

func TestDragPayloadEnterLeaveAccept(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	src := newDnd(Rect{X: 0, Y: 0, W: 50, H: 50})
	data := &DragData{}
	data.Set("application/x-row", 7)
	src.data = data
	picky := newDnd(Rect{X: 100, Y: 0, W: 50, H: 50})
	picky.accept = func(d *DragData) bool { return d.Has("text/plain") }
	target := newDnd(Rect{X: 200, Y: 0, W: 50, H: 50})
	target.accept = func(d *DragData) bool { return d.Has("application/x-row") }
	root := NewContainer(AbsoluteLayout{}, src, picky, target)
	win.SetRoot(root)
	root.Layout(Rect{W: 300, H: 100})
	src.Layout(Rect{X: 0, Y: 0, W: 50, H: 50})
	picky.Layout(Rect{X: 100, Y: 0, W: 50, H: 50})
	target.Layout(Rect{X: 200, Y: 0, W: 50, H: 50})

	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, 10, 10, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseMove, 120, 10, 0, 0))
	if len(picky.log) != 0 {
		t.Fatalf("a rejecting target got %v", picky.log)
	}
	win.DispatchTestEvent(NewMouseEvent(EventMouseMove, 220, 10, 0, 0))
	if len(target.log) < 2 || target.log[0] != EventDragEnter || target.log[1] != EventDragOver {
		t.Fatalf("target log %v, want Enter, Over", target.log)
	}
	if d, ok := win.DragInProgress(); !ok || d != data {
		t.Fatal("DragInProgress should expose the payload")
	}
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, 220, 10, MouseButtonLeft, 0))
	last := target.log[len(target.log)-2:]
	if last[0] != EventDrop || last[1] != EventDragLeave {
		t.Fatalf("target tail %v, want Drop, Leave", last)
	}
	if !src.ended.Accepted || src.ended.Data != data {
		t.Fatalf("DragEnd Accepted=%v, want true with the payload", src.ended.Accepted)
	}
}

func TestFileDropRoutesToDroppable(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	zone := newDnd(Rect{X: 0, Y: 0, W: 100, H: 100})
	zone.accept = func(d *DragData) bool { return d.Has("Files") }
	win.SetRoot(zone)
	zone.Layout(Rect{X: 0, Y: 0, W: 100, H: 100})
	windowGot := 0
	win.SetOnFileDrop(func([]string, float32, float32) { windowGot++ })
	win.onFileDrop([]string{"/tmp/a.png"}, 10, 10)
	if windowGot != 0 || len(zone.log) != 1 || zone.log[0] != EventDrop {
		t.Fatalf("zone log %v, window callback %d", zone.log, windowGot)
	}
	win.onFileDrop([]string{"/tmp/a.png"}, 200, 10)
	if windowGot != 1 {
		t.Fatal("a drop outside every zone should reach the window callback")
	}
}

func TestOverlayLayersKeepToastsOnTop(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	win.SetRoot(newSized(10, 10))
	toast := newSized(10, 10)
	win.PushOverlayLayer(toast, OverlayLayerNotification)
	modal := newKeySpyModal()
	win.PushOverlay(modal)
	ovs := win.Overlays()
	if len(ovs) != 2 || ovs[0] != modal || ovs[1] != toast {
		t.Fatalf("overlay order %v, want modal then toast", ovs)
	}
	win.PushOverlay(toast) // re-push keeps its layer
	if win.Overlays()[1] != toast {
		t.Fatal("re-push lost the toast's layer")
	}
}

type exitingOverlay struct {
	BaseWidget
	done func()
}

func (e *exitingOverlay) BeginOverlayExit(done func()) bool {
	e.done = done
	return true
}

func TestOverlayExitAnimation(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	win.SetRoot(newSized(10, 10))
	ov := &exitingOverlay{BaseWidget: NewBaseWidget()}
	ov.SetSelf(ov)
	win.PushOverlay(ov)
	win.RemoveOverlay(ov)
	if len(win.Overlays()) != 0 || !win.OverlayExiting(ov) || ov.Window() != win {
		t.Fatal("exiting overlay should leave the stack but stay attached")
	}
	ov.done()
	if win.OverlayExiting(ov) || ov.Window() != nil {
		t.Fatal("done should detach the overlay")
	}
}

func TestMouseEventClickCount(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	var clicks []int
	spy := &clickSpy{BaseWidget: NewBaseWidget(), clicks: &clicks}
	spy.SetSelf(spy)
	win.SetRoot(spy)
	spy.Layout(Rect{W: 200, H: 200})
	now := time.Now()
	for i := 0; i < 2; i++ {
		down := NewMouseEvent(EventMouseDown, 10, 10, MouseButtonLeft, 0)
		down.When = now.Add(time.Duration(i) * 100 * time.Millisecond)
		win.DispatchTestEvent(down)
		up := NewMouseEvent(EventMouseUp, 10, 10, MouseButtonLeft, 0)
		up.When = down.When
		win.DispatchTestEvent(up)
	}
	late := NewMouseEvent(EventMouseDown, 10, 10, MouseButtonLeft, 0)
	late.When = now.Add(2 * time.Second)
	win.DispatchTestEvent(late)
	want := []int{1, 1, 2, 2, 1}
	if len(clicks) != len(want) {
		t.Fatalf("clicks %v, want %v", clicks, want)
	}
	for i := range want {
		if clicks[i] != want[i] {
			t.Fatalf("clicks %v, want %v", clicks, want)
		}
	}
}

type clickSpy struct {
	BaseWidget
	clicks *[]int
}

func (c *clickSpy) Handle(e Event) bool {
	if me, ok := e.(MouseEvent); ok && (me.Type() == EventMouseDown || me.Type() == EventMouseUp) {
		*c.clicks = append(*c.clicks, me.Clicks)
	}
	return false
}

func TestThemeGenerationAndUnsubscribe(t *testing.T) {
	saved := *CurrentTheme()
	defer SetTheme(saved)
	gen := ThemeGeneration()
	calls := 0
	unsub := SubscribeTheme(func() { calls++ })
	before := len(themeSubscribers)
	SetTheme(saved)
	unsub()
	SetTheme(saved)
	if calls != 1 || ThemeGeneration() != gen+2 || len(themeSubscribers) != before-1 {
		t.Fatalf("calls=%d gen+%d subscribers %d→%d", calls, ThemeGeneration()-gen, before, len(themeSubscribers))
	}
}

func TestFocusListenerRemoval(t *testing.T) {
	win := NewTestWindow(Size{W: 100, H: 100})
	a := newKeySpy()
	win.SetRoot(a)
	n := 0
	remove := win.AddFocusChangeListener(func() { n++ })
	win.SetFocus(a)
	remove()
	win.SetFocus(nil)
	if n != 1 {
		t.Fatalf("listener ran %d times, want 1", n)
	}
}

func TestTooltipDelay(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 300})
	target := newSized(50, 20)
	target.SetTooltip("hi")
	win.SetRoot(target)
	target.Layout(Rect{W: 50, H: 20})
	win.SetTooltipStyle(TooltipStyle{Delay: 500 * time.Millisecond})
	win.SetHoverPath([]Widget{target})
	win.updateTooltipFromHover(5, 5)
	if win.tooltipView != nil {
		t.Fatal("tooltip shown before its delay")
	}
	win.tickTooltip(time.Now().Add(time.Second))
	if win.tooltipView == nil {
		t.Fatal("tooltip not shown after its delay")
	}
}
