package qui_test

import (
	"errors"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Phase 3 tests cover the actuation API. We use NewTestWindow so the
// PostJob path runs inline (no GLFW). The test buttons / fields
// drive the same dispatch pipeline production input uses.

func TestClick_FiresOnClick(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicked := 0
	btn := widgets.NewButton("Submit", func() { clicked++ })
	btn.SetID("submit")
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 30})

	if err := w.Click("#submit", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if clicked != 1 {
		t.Errorf("OnClick fired %d times, want 1", clicked)
	}
}

func TestClick_NoMatch(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 100, H: 100})
	if err := w.Click("#missing", qui.ClickOptions{}); !errors.Is(err, qui.ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

func TestClick_ModalBlocks(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicked := 0
	btn := widgets.NewButton("Submit", func() { clicked++ })
	btn.SetID("submit")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 30})

	// Push a modal dialog that swallows clicks outside its bounds.
	d := widgets.NewDialog("Confirm", widgets.NewLabel("body"))
	d.SetSelf(d)
	w.PushOverlay(d)
	d.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	err := w.Click("#submit", qui.ClickOptions{})
	var mb *qui.ErrModalBlocked
	if !errors.As(err, &mb) {
		t.Fatalf("expected ErrModalBlocked, got %v", err)
	}
	if clicked != 0 {
		t.Errorf("OnClick fired despite modal: %d", clicked)
	}
}

// A widget that PROJECTS children for introspection — a canvas that draws
// its own items and exposes them through ChildList — must still be clickable
// by selector. Hit-testing the projected child's bounds resolves to the
// OWNING widget (the projected child was never a dispatch target), and that
// counts as reachable: the owner is what handles the click.
func TestClick_ProjectedChildReachesThroughItsOwner(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	owner := newProjectingWidget()
	w.SetRoot(owner)
	owner.Layout(qui.Rect{W: 400, H: 300})

	if err := w.Click("#item", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click on a projected child: %v", err)
	}
	if owner.clicks != 1 {
		t.Errorf("the owner received %d clicks, want 1", owner.clicks)
	}
}

// projectingWidget draws its items itself and exposes one AX proxy for them.
type projectingWidget struct {
	qui.BaseWidget
	proxy  *projectedProxy
	clicks int
}

func newProjectingWidget() *projectingWidget {
	p := &projectingWidget{}
	p.BaseWidget = qui.NewBaseWidget()
	p.SetSelf(p)
	p.proxy = &projectedProxy{}
	p.proxy.BaseWidget = qui.NewBaseWidget()
	p.proxy.SetSelf(p.proxy)
	p.proxy.SetID("item")
	p.proxy.SetParent(p)
	p.proxy.Layout(qui.Rect{X: 100, Y: 100, W: 60, H: 40})
	return p
}

func (p *projectingWidget) ChildList() []qui.Widget { return []qui.Widget{p.proxy} }

func (p *projectingWidget) Handle(event qui.Event) bool {
	if me, ok := event.(qui.MouseEvent); ok && me.Type() == qui.EventMouseDown {
		p.clicks++
		return true
	}
	return false
}

type projectedProxy struct{ qui.BaseWidget }

func TestType_UsesTextSinkFastPath(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	tf := widgets.NewInput("name")
	tf.SetID("name")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})
	tf.Layout(qui.Rect{X: 10, Y: 10, W: 200, H: 30})

	if err := w.Type("#name", "hello world", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if got := tf.Text; got != "hello world" {
		t.Errorf("Text = %q, want hello world", got)
	}
}

func TestFocus_TargetReceivesFocus(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	tf := widgets.NewInput("name")
	tf.SetID("name")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	tf.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 30})

	if err := w.Focus("#name"); err != nil {
		t.Fatalf("Focus: %v", err)
	}
	if w.Focused() != tf {
		t.Errorf("focused = %v, want %v", w.Focused(), tf)
	}
}

func TestScrollIntoView_ScrollsContainer(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	row := widgets.NewButton("Bottom", nil)
	row.SetID("bottom")
	sv := widgets.NewScrollView()
	inner := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	inner.SetSelf(inner)
	for i := 0; i < 20; i++ {
		filler := widgets.NewLabel("filler")
		inner.AddChild(filler)
	}
	inner.AddChild(row)
	sv.SetContent(inner, qui.Size{W: 200, H: 1000})
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(sv)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	// Force layout numbers that put `bottom` below the viewport.
	sv.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 100})
	sv.ContentSize = qui.Size{W: 200, H: 1000}
	row.Layout(qui.Rect{X: 0, Y: 900, W: 200, H: 30})

	before := sv.ScrollY()
	if err := w.Click("#bottom", qui.ClickOptions{ScrollIntoView: true}); err != nil {
		// Click may still fail on reachability if Layout wasn't full
		// (we don't fully simulate ScrollView's child layout). We
		// just want to confirm ScrollChildIntoView fired.
		_ = err
	}
	if sv.ScrollY() == before {
		t.Errorf("ScrollView did not scroll on agent click; ScrollY=%v", sv.ScrollY())
	}
}

// An agent click on a target below the fold must both scroll it into view and
// LAND on it. The aim point therefore has to be computed after the scroll —
// aiming with the pre-scroll AX snapshot misses by the scroll distance.
func TestScrollIntoView_ClickReachesOffscreenTarget(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	clicked := false
	rows := make([]qui.Widget, 40)
	for i := range rows {
		l := widgets.NewLabel("filler")
		l.SetPreferredSize(0, 25)
		rows[i] = l
	}
	target := widgets.NewButton("Bottom", func() { clicked = true })
	target.SetID("bottom")
	target.SetPreferredSize(0, 25)
	rows[39] = target

	inner := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, rows...)
	inner.SetSelf(inner)
	sv := widgets.NewScrollView()
	sv.SetContent(inner, qui.Size{W: 200, H: 1000})
	w.SetRoot(sv)
	sv.Layout(qui.Rect{W: 200, H: 100})

	if qui.InteractionBoundsOf(target).Y < 100 {
		t.Fatalf("target should start below the viewport, got %+v",
			qui.InteractionBoundsOf(target))
	}
	if err := w.Click("#bottom", qui.ClickOptions{ScrollIntoView: true}); err != nil {
		t.Fatalf("Click: %v (scroll=%v, target on screen %+v)",
			err, sv.ScrollY(), qui.InteractionBoundsOf(target))
	}
	if sv.ScrollY() == 0 {
		t.Error("ScrollView did not scroll the target into view")
	}
	if !clicked {
		t.Error("the click never reached the target")
	}
}

func TestSendChord_RoutesToFocused(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	tf := widgets.NewInput("x")
	tf.Text = "abc"
	tf.SetID("tf")
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 200, H: 100})
	tf.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 30})
	_ = w.Focus("#tf")

	// Cmd-A selects all in the text field.
	if err := w.SendChord("", "Cmd+A"); err != nil {
		t.Fatalf("SendChord: %v", err)
	}
	// We can't directly inspect Input.selStart from outside the
	// package, so just verify no panic and the focused widget
	// remains the textfield.
	if w.Focused() != tf {
		t.Errorf("focus changed unexpectedly")
	}
}

func TestIdleState_PendingJobsCounted(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	if s := w.IdleState(); !s.IsIdle() {
		t.Fatalf("fresh test window not idle: %+v", s)
	}
	w.PostJob(func() {})
	w.PostJob(func() {})
	if s := w.IdleState(); s.PendingJobs != 2 || s.IsIdle() {
		t.Fatalf("after 2 PostJob: %+v", s)
	}
	w.DrainJobsForTest()
	if s := w.IdleState(); s.PendingJobs != 0 {
		t.Errorf("after drain: PendingJobs=%d, want 0", s.PendingJobs)
	}
}

// -------------------------------------------------------------------
// Widget-targeted action variants (used by the htmlcss DOM agent layer).

func TestClickWidget_FiresOnClick(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicked := 0
	btn := widgets.NewButton("Submit", func() { clicked++ })
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 30})

	if err := w.ClickWidget(btn, qui.ClickOptions{}); err != nil {
		t.Fatalf("ClickWidget: %v", err)
	}
	if clicked != 1 {
		t.Errorf("OnClick fired %d times, want 1", clicked)
	}
}

func TestClickWidget_NilNoMatch(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 100, H: 100})
	root := qui.NewContainer(qui.FlexLayout{})
	root.SetSelf(root)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 100, H: 100})
	if err := w.ClickWidget(nil, qui.ClickOptions{}); !errors.Is(err, qui.ErrNoMatch) {
		t.Errorf("err = %v, want ErrNoMatch", err)
	}
}

func TestTypeWidget_SetsText(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	tf := widgets.NewInput("name")
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(tf)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})
	tf.Layout(qui.Rect{X: 10, Y: 10, W: 200, H: 28})

	if err := w.TypeWidget(tf, "hello", qui.TypeOptions{}); err != nil {
		t.Fatalf("TypeWidget: %v", err)
	}
	if tf.GetText() != "hello" {
		t.Errorf("text = %q, want hello", tf.GetText())
	}
	if w.Focused() != tf {
		t.Errorf("TypeWidget did not focus the field")
	}
}

// charWidget is a focusable widget that owns its own text entry — the shape
// of a sheet grid or canvas editor that starts editing on the first
// character rather than exposing a text field.
type charWidget struct {
	qui.BaseWidget
	typed string
}

func newCharWidget() *charWidget {
	c := &charWidget{BaseWidget: qui.NewBaseWidget()}
	c.SetSelf(c)
	return c
}

func (c *charWidget) Measure(qui.Size) qui.Size { return qui.Size{W: 100, H: 40} }
func (c *charWidget) Draw(qui.Canvas)           {}
func (c *charWidget) Focusable() bool           { return c.Enabled() }
func (c *charWidget) SetFocused(bool)           {}

func (c *charWidget) Handle(event qui.Event) bool {
	if ce, ok := event.(qui.CharEvent); ok {
		c.typed += string(ce.Rune)
		return true
	}
	return false
}

func typeFixture(t *testing.T) (*qui.Window, *qui.Container, *charWidget, *widgets.Label) {
	t.Helper()
	grid := newCharWidget()
	grid.SetID("grid")
	lbl := widgets.NewLabel("caption")
	lbl.SetID("caption")
	lbl.Selectable = true // selectable ⇒ focusable, but still not a text field
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(grid)
	root.AddChild(lbl)
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})
	return w, root, grid, lbl
}

// A widget that handles its own character input gets the characters
// dispatched to it — the documented alternative to a TextSink.
func TestType_NonSinkFocusableGetsChars(t *testing.T) {
	w, _, grid, _ := typeFixture(t)

	if err := w.Type("#grid", "41", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#grid): %v", err)
	}
	if grid.typed != "41" {
		t.Errorf("target received %q, want %q", grid.typed, "41")
	}
	if w.Focused() != grid {
		t.Errorf("focus = %v, want the target", w.Focused())
	}
}

// SetText is too common a method name to trust as a text-input marker: a
// Label has one, and typing into a caption must not rewrite it.
func TestType_LabelIsNotATextSink(t *testing.T) {
	w, _, _, lbl := typeFixture(t)

	err := w.Type("#caption", "hijacked", qui.TypeOptions{})
	if !errors.Is(err, qui.ErrNotTextTarget) {
		t.Fatalf("err = %v, want ErrNotTextTarget", err)
	}
	if lbl.Text() != "caption" {
		t.Errorf("label text = %q, want unchanged", lbl.Text())
	}
}

// A non-focusable, non-sink target can't be typed into at all: the
// characters would otherwise land on whatever was focused before.
func TestType_NonFocusableFails(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	tf := widgets.NewInput("name")
	tf.SetID("name")
	box := qui.NewContainer(qui.FlexLayout{})
	box.SetSelf(box)
	box.SetID("box")
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(tf)
	root.AddChild(box)
	w.SetRoot(root)
	root.Layout(qui.Rect{W: 400, H: 300})

	if err := w.Type("#name", "typed", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#name): %v", err)
	}
	err := w.Type("#box", "stray", qui.TypeOptions{})
	if !errors.Is(err, qui.ErrNotTextTarget) {
		t.Fatalf("err = %v, want ErrNotTextTarget", err)
	}
	if tf.GetText() != "typed" {
		t.Errorf("field text = %q — the stray characters leaked to the focused field", tf.GetText())
	}
}

// An empty selector used to match every node, so an action with a missing
// target silently operated on the root widget.
func TestActions_EmptyTargetIsInvalid(t *testing.T) {
	w, _, grid, _ := typeFixture(t)

	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"type", func() error { return w.Type("", "41", qui.TypeOptions{}) }},
		{"click", func() error { return w.Click("", qui.ClickOptions{}) }},
		{"focus", func() error { return w.Focus("") }},
		{"scroll", func() error { return w.Scroll("", 0, 10) }},
	} {
		if err := tc.run(); !errors.Is(err, qui.ErrInvalidSelector) {
			t.Errorf("%s with empty target: err = %v, want ErrInvalidSelector", tc.name, err)
		}
	}
	if grid.typed != "" {
		t.Errorf("root-targeted type reached a widget: %q", grid.typed)
	}
}

// Raw mode bypasses the sink but still refuses a target that cannot hold
// focus — the characters have nowhere to go.
func TestType_RawRequiresFocusableTarget(t *testing.T) {
	w, _, grid, lbl := typeFixture(t)

	if err := w.Type("#grid", "ab", qui.TypeOptions{Raw: true}); err != nil {
		t.Fatalf("raw Type(#grid): %v", err)
	}
	if grid.typed != "ab" {
		t.Errorf("target received %q, want ab", grid.typed)
	}
	lbl.Selectable = false // no longer focusable
	if err := w.Type("#caption", "ab", qui.TypeOptions{Raw: true}); !errors.Is(err, qui.ErrNotTextTarget) {
		t.Errorf("err = %v, want ErrNotTextTarget", err)
	}
}

// Clicking a disabled control has to FAIL, not silently succeed.
//
// The widget ignores the click either way; the difference is whether the
// caller finds out. A script that "clicked" a greyed-out menu item and got
// ok back carries on believing the command ran — the same silent-failure
// shape as a selector that matched nothing, which this layer already
// refuses. Found by a parity scenario that clicked a disabled "Restart
// numbering" and reported success.
func TestClick_DisabledTargetFails(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicked := 0
	btn := widgets.NewButton("Submit", func() { clicked++ })
	btn.SetID("submit")
	btn.SetEnabled(false)
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 30})

	var disabled *qui.ErrDisabled
	if err := w.Click("#submit", qui.ClickOptions{}); !errors.As(err, &disabled) {
		t.Fatalf("Click on a disabled button: err = %v, want ErrDisabled", err)
	}
	if clicked != 0 {
		t.Errorf("the disabled button's OnClick ran %d times", clicked)
	}
	// Re-enabling makes the same call work, so the guard is about state and
	// not about the selector.
	btn.SetEnabled(true)
	if err := w.Click("#submit", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click after enabling: %v", err)
	}
	if clicked != 1 {
		t.Errorf("OnClick fired %d times after enabling, want 1", clicked)
	}
}

// An explicit coordinate click is the caller saying "hit this pixel"; there
// is no node whose state to consult, so it still goes through.
func TestClickAt_UnaffectedByDisabledState(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	btn := widgets.NewButton("Submit", func() {})
	btn.SetID("submit")
	btn.SetEnabled(false)
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(btn)
	w.SetRoot(root)
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	btn.Layout(qui.Rect{X: 10, Y: 10, W: 80, H: 30})
	if err := w.ClickAt(50, 25); err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
}

// modWidget records the modifiers of the mouse events it receives.
type modWidget struct {
	qui.BaseWidget
	mods qui.Modifiers
	hits int
}

func newModWidget() *modWidget {
	m := &modWidget{BaseWidget: qui.NewBaseWidget()}
	m.SetSelf(m)
	return m
}

func (m *modWidget) Measure(qui.Size) qui.Size { return qui.Size{W: 100, H: 40} }
func (m *modWidget) Draw(qui.Canvas)           {}

func (m *modWidget) Handle(event qui.Event) bool {
	if me, ok := event.(qui.MouseEvent); ok && me.Type() == qui.EventMouseDown {
		m.mods = me.Mods
		m.hits++
		return true
	}
	return false
}

// A point click has to be able to carry modifiers. Custom-drawn surfaces are
// exactly what the coordinate escape hatch exists for, and they are also where
// modifier-clicks live — Cmd+click to follow a link inside a document canvas,
// Shift+click to extend a selection. Without this those gestures were
// undrivable from outside: the modifiers were silently dropped and the click
// arrived plain, which looks like "the feature does not work".
func TestClickAtWith_CarriesModifiers(t *testing.T) {
	w := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	target := newModWidget()
	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical})
	root.SetSelf(root)
	root.AddChild(target)
	w.SetRoot(root)
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if err := w.ClickAtWith(50, 25, qui.ClickOptions{Modifiers: qui.ModSuper | qui.ModShift}); err != nil {
		t.Fatalf("ClickAtWith: %v", err)
	}
	if target.hits != 1 {
		t.Fatalf("%d mouse-downs, want 1", target.hits)
	}
	if target.mods != qui.ModSuper|qui.ModShift {
		t.Errorf("mods = %v, want Super|Shift", target.mods)
	}
	// Plain ClickAt still means no modifiers.
	if err := w.ClickAt(50, 25); err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
	if target.mods != 0 {
		t.Errorf("mods = %v after a plain ClickAt, want none", target.mods)
	}
}
