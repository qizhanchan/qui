package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func newMouseEventForHandle(evType EventType, x, y float32) MouseEvent {
	return NewMouseEvent(evType, x, y, MouseButtonLeft, 0)
}

// ---- CheckBox ----

func TestCheckBoxClickToggles(t *testing.T) {
	changes := 0
	var lastState bool
	cb := NewCheckBox("foo", func(checked bool) {
		changes++
		lastState = checked
	})
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})

	cb.Handle(newMouseEventForHandle(EventMouseDown, 10, 10))
	if !cb.Checked || !lastState {
		t.Errorf("after click: Checked=%v lastState=%v, want true/true", cb.Checked, lastState)
	}
	cb.Handle(newMouseEventForHandle(EventMouseDown, 10, 10))
	if cb.Checked || lastState {
		t.Errorf("after 2nd click: Checked=%v lastState=%v, want false/false", cb.Checked, lastState)
	}
	if changes != 2 {
		t.Errorf("OnChange fired %d times, want 2", changes)
	}
}

func TestCheckBoxSpaceKeyToggles(t *testing.T) {
	cb := NewCheckBox("foo", nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	cb.SetFocused(true)

	cb.Handle(NewKeyEvent(EventKeyDown, KeySpace, 0))
	if !cb.Checked {
		t.Error("Space did not toggle CheckBox")
	}
}

func TestCheckBoxDisabledIgnoresInput(t *testing.T) {
	cb := NewCheckBox("foo", nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	cb.SetEnabled(false)

	cb.Handle(newMouseEventForHandle(EventMouseDown, 10, 10))
	if cb.Checked {
		t.Error("disabled CheckBox should not toggle on click")
	}
}

// ---- Switch ----

func TestSwitchClickToggles(t *testing.T) {
	var called bool
	s := NewSwitch("lbl", func(on bool) { called = on })
	s.Layout(Rect{X: 0, Y: 0, W: 100, H: 40})

	// Click = MouseDown + MouseUp inside bounds; commit-on-release
	// supports the standard "drag off to cancel" gesture.
	s.Handle(newMouseEventForHandle(EventMouseDown, 20, 20))
	s.Handle(newMouseEventForHandle(EventMouseUp, 20, 20))
	if !s.On || !called {
		t.Errorf("Switch: On=%v cb=%v", s.On, called)
	}
}

// ---- Progress ----

func TestProgressFractionClamps(t *testing.T) {
	p := NewProgress(0, 100)
	p.Value = -10
	if p.fraction() != 0 {
		t.Errorf("negative below Min should clamp to 0, got %v", p.fraction())
	}
	p.Value = 500
	if p.fraction() != 1 {
		t.Errorf("value above Max should clamp to 1, got %v", p.fraction())
	}
	p.Value = 25
	if p.fraction() != 0.25 {
		t.Errorf("fraction wrong: got %v want 0.25", p.fraction())
	}
}

func TestProgressIndeterminateFullFraction(t *testing.T) {
	p := NewProgress(0, 100)
	p.Value = 10
	p.Indeterminate = true
	if p.fraction() != 1 {
		t.Errorf("indeterminate fraction should be 1, got %v", p.fraction())
	}
}

// ---- RadioGroup ----

func TestRadioGroupSelectsAtMostOne(t *testing.T) {
	group := NewRadioGroup()
	a := NewRadioButton(group, "a")
	b := NewRadioButton(group, "b")
	c := NewRadioButton(group, "c")

	a.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	b.Layout(Rect{X: 0, Y: 25, W: 100, H: 20})
	c.Layout(Rect{X: 0, Y: 50, W: 100, H: 20})

	b.Handle(newMouseEventForHandle(EventMouseDown, 10, 30))
	if !b.Checked || a.Checked || c.Checked {
		t.Errorf("after selecting b: a=%v b=%v c=%v, want f/t/f", a.Checked, b.Checked, c.Checked)
	}
	if group.Selected() != b {
		t.Errorf("group.Selected() = %v, want b", group.Selected())
	}

	c.Handle(newMouseEventForHandle(EventMouseDown, 10, 55))
	if !c.Checked || b.Checked {
		t.Errorf("after selecting c: b should be unchecked, c checked; got b=%v c=%v", b.Checked, c.Checked)
	}
}

// TestRadioGroupClearsManuallyCheckedPeer regresses a bug where pre-selecting
// a button by setting Checked=true directly (bypassing group.Select) left
// the group's internal `selected` pointer nil — so the first click on any
// other button found nothing to uncheck and produced two simultaneously
// checked radios. Select() now sweeps every registered button to clear
// stale Checked state.
func TestRadioGroupClearsManuallyCheckedPeer(t *testing.T) {
	group := NewRadioGroup()
	a := NewRadioButton(group, "a")
	b := NewRadioButton(group, "b")
	b.Checked = true // direct field write, group.selected stays nil

	a.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	b.Layout(Rect{X: 0, Y: 25, W: 100, H: 20})

	a.Handle(newMouseEventForHandle(EventMouseDown, 10, 10))
	if !a.Checked || b.Checked {
		t.Errorf("after click on a: want a=true b=false; got a=%v b=%v", a.Checked, b.Checked)
	}
	if group.Selected() != a {
		t.Errorf("group.Selected() = %v, want a", group.Selected())
	}
}

func TestRadioGroupOnChangeFires(t *testing.T) {
	group := NewRadioGroup()
	a := NewRadioButton(group, "a")
	b := NewRadioButton(group, "b")
	a.Layout(Rect{X: 0, Y: 0, W: 100, H: 20})
	b.Layout(Rect{X: 0, Y: 25, W: 100, H: 20})

	var received *RadioButton
	group.OnChange = func(r *RadioButton) { received = r }

	group.Select(a)
	if received != a {
		t.Errorf("OnChange didn't receive a; got %v", received)
	}
	group.Select(b)
	if received != b {
		t.Errorf("OnChange didn't receive b; got %v", received)
	}
	// Re-selecting same does not fire.
	received = nil
	group.Select(b)
	if received != nil {
		t.Errorf("re-Selecting same button should not fire OnChange; got %v", received)
	}
}

func TestRadioButtonPanicsOnNilGroup(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NewRadioButton(nil, ...) should panic")
		}
	}()
	NewRadioButton(nil, "foo")
}

// ---- Slider ----

func TestSliderClickSetsValue(t *testing.T) {
	var got float32
	s := NewSlider(0, 100, 0, func(v float32) { got = v })
	s.Layout(Rect{X: 0, Y: 0, W: 100, H: 24})

	// Click at x=50 (midpoint) → value 50.
	s.Handle(newMouseEventForHandle(EventMouseDown, 50, 12))
	if s.Value != 50 {
		t.Errorf("click at midpoint: Value=%v, want 50", s.Value)
	}
	if got != 50 {
		t.Errorf("OnChange got %v, want 50", got)
	}
}

func TestSliderDragUpdatesValue(t *testing.T) {
	s := NewSlider(0, 100, 0, nil)
	// W = 2*track-padding (18) + 100 = 136 → visible track 100 px wide,
	// so trackX+v maps 1:1 to value v.
	s.Layout(Rect{X: 0, Y: 0, W: 136, H: 40})

	s.Handle(newMouseEventForHandle(EventMouseDown, 28, 20)) // trackX + 10
	if s.Value != 10 {
		t.Fatalf("down: Value=%v, want 10", s.Value)
	}
	// Mouse move while dragging.
	s.Handle(newMouseEventForHandle(EventMouseMove, 98, 20)) // trackX + 80
	if s.Value != 80 {
		t.Errorf("drag: Value=%v, want 80", s.Value)
	}
	// MouseMove without prior down should not affect value.
	s.Handle(newMouseEventForHandle(EventMouseUp, 98, 20))
	s.Handle(newMouseEventForHandle(EventMouseMove, 38, 20))
	if s.Value != 80 {
		t.Errorf("move after up: Value=%v, want 80 (unchanged)", s.Value)
	}
}

func TestSliderArrowKeysStep(t *testing.T) {
	s := NewSlider(0, 100, 50, nil)
	s.Step = 5
	s.Layout(Rect{X: 0, Y: 0, W: 100, H: 24})
	s.SetFocused(true)

	key := func(k Key) KeyEvent {
		return NewKeyEvent(EventKeyDown, k, 0)
	}

	s.Handle(key(KeyRight))
	if s.Value != 55 {
		t.Errorf("Right: got %v want 55", s.Value)
	}
	s.Handle(key(KeyLeft))
	s.Handle(key(KeyLeft))
	if s.Value != 45 {
		t.Errorf("Left x2: got %v want 45", s.Value)
	}
}

func TestSliderClampsValueOnConstruction(t *testing.T) {
	s := NewSlider(0, 10, 100, nil)
	if s.Value != 10 {
		t.Errorf("initial above Max should clamp to Max; got %v", s.Value)
	}
	s = NewSlider(0, 10, -5, nil)
	if s.Value != 0 {
		t.Errorf("initial below Min should clamp to Min; got %v", s.Value)
	}
}

// ---- Button focusable ----

func TestButtonFocusableAndSpaceTriggersClick(t *testing.T) {
	var clicked bool
	b := NewButton("Go", func() { clicked = true })
	b.Layout(Rect{X: 0, Y: 0, W: 50, H: 24})
	if !b.Focusable() {
		t.Error("enabled Button should be Focusable")
	}
	b.SetFocused(true)

	b.Handle(NewKeyEvent(EventKeyDown, KeySpace, 0))
	if !clicked {
		t.Error("Space on focused Button should trigger OnClick")
	}
}

func TestButtonHoverInvalidatesOwnBoundsThroughDispatch(t *testing.T) {
	w := NewTestWindow(Size{W: 100, H: 100})
	b := NewButton("Go", nil)
	w.SetRoot(b)
	b.Layout(Rect{X: 10, Y: 10, W: 50, H: 24})
	b.ClearLayoutDirty()
	w.ClearDirtyRegion()
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 200, 200, MouseButtonLeft, 0))
	if !w.DirtyRegion().IsEmpty() {
		t.Fatalf("mouse move outside button should not invalidate; dirty=%v", w.DirtyRegion())
	}
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 15, 15, MouseButtonLeft, 0))

	if !w.DirtyRegion().Intersects(b.Bounds()) {
		t.Fatalf("button hover should invalidate its bounds; dirty=%v bounds=%v", w.DirtyRegion(), b.Bounds())
	}
}
