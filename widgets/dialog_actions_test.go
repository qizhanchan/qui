package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

func TestDialogCanCloseVetoesUserCloses(t *testing.T) {
	d := NewDialog("t", nil)
	var reasons []DialogCloseReason
	allow := false
	d.CanClose = func(r DialogCloseReason) bool { reasons = append(reasons, r); return allow }
	closed := DialogCloseReason(-1)
	d.OnClose = func(r DialogCloseReason) { closed = r }
	btn := d.AddButton("OK", nil)
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)

	btn.OnClick()
	w.DispatchTestEvent(newKeyDown(KeyEscape))
	if !d.IsShown() {
		t.Fatal("vetoed close still closed the dialog")
	}
	if len(reasons) != 2 || reasons[0] != DialogCloseAction || reasons[1] != DialogCloseEscape {
		t.Errorf("CanClose reasons = %v, want [action escape]", reasons)
	}

	allow = true
	w.DispatchTestEvent(newKeyDown(KeyEscape))
	if d.IsShown() || closed != DialogCloseEscape {
		t.Errorf("shown=%v closed=%v, want closed with escape", d.IsShown(), closed)
	}
}

func TestDialogCloseIsNotVetoed(t *testing.T) {
	d := NewDialog("t", nil)
	d.CanClose = func(DialogCloseReason) bool { return false }
	got := DialogCloseReason(-1)
	d.OnClose = func(r DialogCloseReason) { got = r }
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	d.Close()
	if d.IsShown() || got != DialogCloseProgrammatic {
		t.Errorf("Close: shown=%v reason=%v, want closed programmatic", d.IsShown(), got)
	}
}

func TestDialogAddActionDoesNotAutoClose(t *testing.T) {
	d := NewDialog("t", nil)
	clicked := false
	custom := NewButton("Delete", func() { clicked = true })
	d.AddAction(custom)
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	custom.OnClick()
	if !clicked || !d.IsShown() {
		t.Errorf("clicked=%v shown=%v, want clicked and still shown", clicked, d.IsShown())
	}
	if custom.Parent() != Widget(d) {
		t.Error("AddAction should parent the widget to the dialog")
	}
	// Any widget, not only *Button.
	d.AddAction(NewCheckBox("Don't ask again", nil))
	if len(d.Actions) != 2 {
		t.Fatalf("Actions = %d, want 2", len(d.Actions))
	}
}

func TestDialogEnterPressesDefaultAction(t *testing.T) {
	saved := 0
	d := NewDialog("t", nil)
	d.AddButton("Cancel", nil)
	d.DefaultAction = d.AddButton("Save", func() { saved++ })
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	w.SetFocus(nil)

	w.DispatchTestEvent(newKeyDown(KeyEnter))
	if saved != 1 || d.IsShown() {
		t.Errorf("saved=%d shown=%v, want 1 and closed", saved, d.IsShown())
	}
}

// keyEater consumes Enter / Escape as its own (a TextArea newline, an
// inline editor's cancel).
type keyEater struct {
	focusableSpy
	got int
}

func (k *keyEater) Handle(e Event) bool {
	if ke, ok := e.(KeyEvent); ok && ke.Phase() == PhaseTarget {
		k.got++
		return true
	}
	return false
}

func TestDialogFocusedContentGetsKeysFirst(t *testing.T) {
	eater := &keyEater{focusableSpy: focusableSpy{BaseWidget: NewBaseWidget()}}
	eater.SetSelf(eater)
	saved := 0
	d := NewDialog("t", eater)
	d.DefaultAction = d.AddButton("Save", func() { saved++ })
	d.InitialFocus = eater
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	if w.Focused() != Widget(eater) {
		t.Fatalf("InitialFocus not honored: focused=%v", w.Focused())
	}

	w.DispatchTestEvent(newKeyDown(KeyEnter))
	w.DispatchTestEvent(newKeyDown(KeyEscape))
	if eater.got != 2 {
		t.Errorf("focused content saw %d keys, want 2", eater.got)
	}
	if saved != 0 || !d.IsShown() {
		t.Errorf("saved=%d shown=%v: the dialog acted on keys its content consumed", saved, d.IsShown())
	}
}

func TestDialogDismissOnBackdrop(t *testing.T) {
	d := NewDialog("t", nil)
	d.DismissOnBackdrop = true
	got := DialogCloseReason(-1)
	d.OnClose = func(r DialogCloseReason) { got = r }
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	d.Handle(newMouseEventForHandle(EventMouseDown, 1, 1))
	if d.IsShown() || got != DialogCloseBackdrop {
		t.Errorf("shown=%v reason=%v, want closed by backdrop", d.IsShown(), got)
	}
}

// tickSpy counts ticks.
type tickSpy struct {
	BaseWidget
	n int
}

func (s *tickSpy) Tick(time.Time) Rect { s.n++; return Rect{} }

func TestDialogTicksThroughScrollView(t *testing.T) {
	leaf := &tickSpy{BaseWidget: NewBaseWidget()}
	sv := NewScrollView()
	sv.Content = leaf
	d := NewDialog("t", sv)
	d.Tick(time.Now())
	if leaf.n != 1 {
		t.Errorf("widget inside a ScrollView ticked %d times, want 1", leaf.n)
	}
}

func TestDialogActionsAlign(t *testing.T) {
	mk := func(align DialogActionsAlign) (*Dialog, *Button, *Button) {
		d := NewDialog("", nil)
		d.Width = 400
		d.ActionsAlign = align
		a := d.AddButton("A", nil)
		b := d.AddButton("B", nil)
		d.Layout(Rect{X: 0, Y: 0, W: 400, H: 200}) // embedded: rect is the box
		return d, a, b
	}
	m := DefaultDialogMetrics()
	left, right := m.ActionsPadX, 400-m.ActionsPadX

	_, a, b := mk(DialogActionsEnd)
	if got := b.Bounds().X + b.Bounds().W; got != right {
		t.Errorf("End: last action right edge = %v, want %v", got, right)
	}
	_, a, _ = mk(DialogActionsStart)
	if a.Bounds().X != left {
		t.Errorf("Start: first action x = %v, want %v", a.Bounds().X, left)
	}
	_, a, b = mk(DialogActionsSpaceBetween)
	if a.Bounds().X != left || b.Bounds().X+b.Bounds().W != right {
		t.Errorf("SpaceBetween: a.x=%v b.right=%v, want %v / %v", a.Bounds().X, b.Bounds().X+b.Bounds().W, left, right)
	}
	_, a, b = mk(DialogActionsStretch)
	if a.Bounds().W != b.Bounds().W || a.Bounds().X != left || b.Bounds().X+b.Bounds().W != right {
		t.Errorf("Stretch: a=%v b=%v, want equal widths spanning the row", a.Bounds(), b.Bounds())
	}
}

func TestDialogHeaderReplacesTitle(t *testing.T) {
	header := newPhaseSpy("header", Rect{W: 100, H: 48}, nil)
	d := NewDialog("ignored", nil)
	d.SetHeader(header)
	m := DefaultDialogMetrics()
	if got, want := d.Measure(Size{W: 800, H: 600}).H, m.HeadlinePadTop+48; got != want {
		t.Errorf("height = %v, want headline pad + header height %v", got, want)
	}
	found := false
	for _, c := range d.ChildList() {
		found = found || c == Widget(header)
	}
	if !found {
		t.Error("Header missing from ChildList")
	}
}

func TestDialogContentRelayoutsWhenItGrows(t *testing.T) {
	body := newPhaseSpy("body", Rect{W: 200, H: 40}, nil)
	d := NewDialog("t", body)
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)
	before := body.Bounds().H

	body.hitRect.H = 120 // content grew while shown
	d.RelayoutOverlay(Size{W: 800, H: 600})
	if got := body.Bounds().H; got <= before {
		t.Errorf("content height %v after growing, want > %v", got, before)
	}
}

func TestDialogEnterAndEscapeFromFocusedInput(t *testing.T) {
	in := NewInput("Name")
	saved := 0
	d := NewDialog("Rename", in)
	d.DefaultAction = d.AddButton("Rename", func() { saved++ })
	d.InitialFocus = in
	w := windowWithRoot(Size{W: 800, H: 600}, NewContainer(nil))
	d.Show(w)

	w.DispatchTestEvent(newKeyDown(KeyEnter))
	if saved != 1 {
		t.Fatalf("Enter in a focused Input pressed the default action %d times, want 1", saved)
	}

	d2 := NewDialog("Rename", NewInput("Name"))
	d2.InitialFocus = d2.Content
	d2.Show(w)
	w.DispatchTestEvent(newKeyDown(KeyEscape))
	if d2.IsShown() {
		t.Error("Escape in a focused Input (no selection) did not close the dialog")
	}
}
