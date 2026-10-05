package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestSelectDefaultNoSelection(t *testing.T) {
	cb := NewSelect(nil, []string{"A", "B"}, nil)
	if cb.SelectedIdx != -1 {
		t.Errorf("default SelectedIdx = %d, want -1", cb.SelectedIdx)
	}
	if cb.SelectedValue() != "" {
		t.Errorf("SelectedValue() = %q, want empty", cb.SelectedValue())
	}
}

func TestSelectSelectedValueReturnsItem(t *testing.T) {
	cb := NewSelect(nil, []string{"Alpha", "Beta"}, nil)
	cb.SelectedIdx = 1
	if cb.SelectedValue() != "Beta" {
		t.Errorf("SelectedValue() = %q, want Beta", cb.SelectedValue())
	}
}

func TestSelectClickOpensDropdown(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 10, Y: 10, W: 120, H: 30})

	cb.Handle(newMouseEventForHandle(EventMouseDown, 50, 20))
	if !cb.isOpen {
		t.Error("click should open dropdown")
	}
	if len(w.Overlays()) != 1 {
		t.Errorf("dropdown should push overlay; got %d", len(w.Overlays()))
	}
}

// A native (raw-HTML) select widens its dropdown to fit the widest option so
// long labels (font names, etc.) aren't clipped — never narrower than the
// trigger. A labelled select keeps the menu pinned to the anchor width.
func TestSelectDropdownWidth(t *testing.T) {
	w := NewTestWindow(Size{W: 800, H: 500})

	// Native: a long option on a narrow trigger must grow the dropdown.
	native := NewSelect(w, []string{"Times New Roman", "Arial"}, nil)
	native.Layout(Rect{X: 10, Y: 10, W: 72, H: 36})
	native.openDropdown()
	if native.popup == nil {
		t.Fatal("native dropdown should open")
	}
	if got := native.popup.Bounds().W; got <= native.Bounds().W {
		t.Errorf("native dropdown width = %v, want > anchor %v (should widen to fit options)", got, native.Bounds().W)
	}
	native.closeDropdown()

	// With a floating label the menu matches the anchor width exactly.
	labelled := NewSelect(w, []string{"0.5x", "0.75x", "1x"}, nil)
	labelled.Label = "Zoom"
	labelled.Layout(Rect{X: 10, Y: 10, W: 200, H: 56})
	labelled.openDropdown()
	if labelled.popup == nil {
		t.Fatal("labelled dropdown should open")
	}
	if got := labelled.popup.Bounds().W; got != labelled.Bounds().W {
		t.Errorf("labelled dropdown width = %v, want anchor width %v", got, labelled.Bounds().W)
	}
}

func TestSelectSecondClickClosesDropdown(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 10, Y: 10, W: 120, H: 30})

	cb.Handle(newMouseEventForHandle(EventMouseDown, 50, 20))
	cb.Handle(newMouseEventForHandle(EventMouseDown, 50, 20))
	if cb.isOpen {
		t.Error("second click should close dropdown")
	}
}

func TestSelectItemSelectionFiresOnChange(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	var gotIdx int = -1
	var gotVal string
	cb := NewSelect(w, []string{"X", "Y", "Z"}, func(i int, v string) {
		gotIdx = i
		gotVal = v
	})
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})

	cb.openDropdown()
	// Find the second item's button in the dropdown popup and click it.
	// The popup's Content is a menuListView wrapping the real item container.
	content := cb.popup.Content.(*menuListView).content
	secondItem := content.ChildAt(1).(MenuActivatable)
	secondItem.ActivateMenuRow()

	if gotIdx != 1 || gotVal != "Y" {
		t.Errorf("OnChange got (%d, %q), want (1, Y)", gotIdx, gotVal)
	}
	if cb.SelectedIdx != 1 {
		t.Errorf("SelectedIdx = %d, want 1", cb.SelectedIdx)
	}
}

func TestSelectItemSelectionClosesDropdown(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})

	cb.openDropdown()
	content := cb.popup.Content.(*menuListView).content
	content.ChildAt(0).(MenuActivatable).ActivateMenuRow()

	if cb.isOpen {
		t.Error("selecting item should close dropdown")
	}
	if cb.popup != nil {
		t.Error("popup ref should be cleared after selection")
	}
	if len(w.Overlays()) != 0 {
		t.Errorf("overlays should be empty; got %d", len(w.Overlays()))
	}
}

func TestSelectArrowKeysQuickSelect(t *testing.T) {
	called := 0
	cb := NewSelect(nil, []string{"A", "B", "C"}, func(_ int, _ string) {
		called++
	})
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})
	cb.SetFocused(true)

	cb.Handle(newKeyDown(KeyDown))
	if cb.SelectedIdx != 0 {
		t.Errorf("KeyDown from -1: SelectedIdx=%d, want 0", cb.SelectedIdx)
	}
	cb.Handle(newKeyDown(KeyDown))
	if cb.SelectedIdx != 1 {
		t.Errorf("KeyDown from 0: SelectedIdx=%d, want 1", cb.SelectedIdx)
	}
	cb.Handle(newKeyDown(KeyUp))
	if cb.SelectedIdx != 0 {
		t.Errorf("KeyUp from 1: SelectedIdx=%d, want 0", cb.SelectedIdx)
	}
	// Verify OnChange fired three times — once per state transition.
	if called != 3 {
		t.Errorf("OnChange fired %d times, want 3", called)
	}
}

func TestSelectSpaceOpensDropdown(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})
	cb.SetFocused(true)

	cb.Handle(newKeyDown(KeySpace))
	if !cb.isOpen {
		t.Error("Space should open dropdown when focused")
	}
}

func TestSelectDisabledIgnoresInput(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})
	cb.SetEnabled(false)

	cb.Handle(newMouseEventForHandle(EventMouseDown, 50, 15))
	if cb.isOpen {
		t.Error("disabled Select should not open on click")
	}
}

func TestSelectWithoutWindowNoCrash(t *testing.T) {
	// Constructed with nil window — toggleDropdown should be a no-op.
	cb := NewSelect(nil, []string{"A"}, nil)
	cb.Layout(Rect{X: 0, Y: 0, W: 100, H: 30})
	cb.Handle(newMouseEventForHandle(EventMouseDown, 50, 15))
	if cb.isOpen {
		t.Error("nil window should prevent dropdown from opening")
	}
}

func TestSelectMeasureFitsWidestItem(t *testing.T) {
	// String must be long enough that even the raw-HTML default font
	// (14 px) puts it past the 200-px minimum width.
	wideItem := "This is a comfortably long option string that overflows"
	cb := NewSelect(nil, []string{"A", wideItem}, nil)
	narrow := NewSelect(nil, []string{"A"}, nil)

	wide := cb.Measure(Size{W: 1000, H: 50})
	slim := narrow.Measure(Size{W: 1000, H: 50})

	if wide.W <= slim.W {
		t.Errorf("expected wider combobox to measure wider: wide=%v slim=%v", wide.W, slim.W)
	}
}

func TestSelectDenseMatchesButtonHeight(t *testing.T) {
	dense := NewSelect(nil, []string{"A", "B"}, nil)
	dense.Label = "Density"
	dense.Variant = SelectFilled
	dense.Dense = true
	// Dense Select aligns to the button height (40 px),
	// same invariant as Dense Input. Configure a Button with an
	// explicit matching Height to represent the reference.
	btn := NewButton("Ok", nil)
	btn.States.Base.Height = 40

	got := dense.Measure(Size{W: 1000, H: 200})
	want := btn.Measure(Size{W: 1000, H: 200})
	if got.H != want.H {
		t.Errorf("dense combo height = %v, want button height %v", got.H, want.H)
	}

	// Dense suppresses the floating label even with Label set.
	dense.Layout(Rect{X: 0, Y: 0, W: 200, H: got.H})
	var canvas RecordingCanvas
	dense.Draw(&canvas)
	// SelectedIdx = -1 + no Placeholder + Dense suppresses Label → 0 texts.
	if len(canvas.Texts) != 0 {
		t.Errorf("dense combo drew %d texts, want 0 (label suppressed)", len(canvas.Texts))
	}
}

func TestSelectDenseInputHasRoomForBodyLargeFont(t *testing.T) {
	// Regression: Select.inputBounds used the tall 16+16 padding even in
	// Dense mode, leaving 8 px of content inside a 40-dp field. Body-
	// large ink is ~19 px, so DrawText's extraY clamped to 0 and the
	// baseline parked at the very top of the slot — visible as "value
	// text biased upward" in the controls row. The fix mirrors
	// Input.Dense: 8+8 padding, 24 px of input height.
	cb := NewSelect(nil, []string{"1x"}, nil)
	cb.Dense = true
	h := cb.Measure(Size{W: 1000, H: 200}).H
	cb.Layout(Rect{X: 0, Y: 0, W: 200, H: h})

	font := cb.Style().Font
	_, glyphH := TextMetrics("Mg", font)
	input := cb.inputBounds()
	if input.H < glyphH {
		t.Errorf("dense inputBounds H = %v, too small for body-large height %v (baseline will clip to top)",
			input.H, glyphH)
	}
}

func TestSelectDrawsTextInsidePadding(t *testing.T) {
	cb := NewSelect(nil, []string{"1x"}, nil)
	cb.SelectedIdx = 0
	cb.Layout(Rect{X: 10, Y: 20, W: 220, H: 64})

	var canvas RecordingCanvas
	cb.Draw(&canvas)
	if len(canvas.Texts) != 1 {
		t.Fatalf("DrawText calls = %d, want 1", len(canvas.Texts))
	}
	text := canvas.Texts[0]
	// The selected-value text sits at the input area's leading edge — the
	// field's leading inset from the widget's left (8 px compact for this
	// plain select; 16 px once a floating Label opts into the tall geometry).
	wantX := cb.inputBounds().X
	if text.X != wantX {
		t.Errorf("text X = %v, want inputBounds left %v", text.X, wantX)
	}
	// Text Y is the inputBounds top — DrawText handles vertical
	// centering internally via its extraY math.
	want := cb.inputBounds()
	if text.Y != want.Y {
		t.Errorf("text Y = %v, want inputBounds top %v", text.Y, want.Y)
	}
}

func TestSelectDropdownTextAlignsWithTriggerText(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"0.5x", "1x"}, nil)
	cb.SelectedIdx = 1
	cb.Layout(Rect{X: 10, Y: 20, W: 220, H: 64})

	var triggerCanvas RecordingCanvas
	cb.Draw(&triggerCanvas)
	if len(triggerCanvas.Texts) != 1 {
		t.Fatalf("trigger DrawText calls = %d, want 1", len(triggerCanvas.Texts))
	}

	cb.openDropdown()
	if cb.popup == nil {
		t.Fatal("dropdown should open")
	}
	content := cb.popup.Content.(*menuListView).content
	row := content.ChildAt(0).(*menuItemView)
	var rowCanvas RecordingCanvas
	row.Draw(&rowCanvas)
	if len(rowCanvas.Texts) != 1 {
		t.Fatalf("row DrawText calls = %d, want 1", len(rowCanvas.Texts))
	}
	if rowCanvas.Texts[0].X != triggerCanvas.Texts[0].X {
		t.Errorf("dropdown text X = %v, want trigger text X %v", rowCanvas.Texts[0].X, triggerCanvas.Texts[0].X)
	}
}

// --- agent-debugging surface -------------------------------------------

func TestSelectRoleIsCombobox(t *testing.T) {
	cb := NewSelect(nil, []string{"A"}, nil)
	if got := WidgetRole(cb); got != RoleCombobox {
		t.Errorf("Role() = %q, want %q", got, RoleCombobox)
	}
}

func TestSelectAccessibleOptions(t *testing.T) {
	cb := NewSelect(nil, []string{"A", "B", "C"}, nil)
	cb.SelectedIdx = 1
	opts := cb.AccessibleOptions()
	if len(opts) != 3 {
		t.Fatalf("options len = %d, want 3", len(opts))
	}
	if opts[1].Label != "B" || !opts[1].Selected {
		t.Errorf("opts[1] = %+v, want {B selected}", opts[1])
	}
	if opts[0].Selected || opts[2].Selected {
		t.Error("only the selected index should be flagged Selected")
	}
	// Surfaces through the generic accessor too.
	if len(WidgetOptions(cb)) != 3 {
		t.Error("WidgetOptions should return the option set")
	}
}

func TestSelectExpandedState(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 10, Y: 10, W: 120, H: 30})
	if cb.AccessibleState()&AXStateExpanded != 0 {
		t.Error("closed select should not report expanded")
	}
	cb.openDropdown()
	if cb.AccessibleState()&AXStateExpanded == 0 {
		t.Error("open select should report expanded")
	}
}

func TestSelectSetTextSelectsByLabel(t *testing.T) {
	var gotIdx int
	var gotVal string
	cb := NewSelect(nil, []string{"Alpha", "Beta", "Gamma"}, func(i int, v string) {
		gotIdx, gotVal = i, v
	})
	cb.SetText("Beta") // exact
	if cb.SelectedIdx != 1 || gotIdx != 1 || gotVal != "Beta" {
		t.Fatalf("exact match: idx=%d val=%q (OnChange idx=%d val=%q)", cb.SelectedIdx, cb.SelectedValue(), gotIdx, gotVal)
	}
}

func TestSelectSetTextTrimsIndent(t *testing.T) {
	// Indented tree labels (like api-saw's Save-to folder picker) must be
	// reachable by typing the bare name.
	cb := NewSelect(nil, []string{"root", "    child", "        deep"}, nil)
	cb.SetText("deep")
	if cb.SelectedValue() != "        deep" {
		t.Errorf("trimmed match: value = %q, want the indented 'deep'", cb.SelectedValue())
	}
	cb.SetText("CHILD") // case-insensitive trimmed
	if cb.SelectedValue() != "    child" {
		t.Errorf("ci match: value = %q, want the indented 'child'", cb.SelectedValue())
	}
}

func TestSelectSetTextUnknownIsNoop(t *testing.T) {
	cb := NewSelect(nil, []string{"A", "B"}, nil)
	cb.SelectedIdx = 0
	cb.SetText("nope")
	if cb.SelectedIdx != 0 {
		t.Errorf("unknown label should not change selection; got idx %d", cb.SelectedIdx)
	}
}

func TestSelectSetTextClosesDropdown(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	cb := NewSelect(w, []string{"A", "B"}, nil)
	cb.Layout(Rect{X: 10, Y: 10, W: 120, H: 30})
	cb.openDropdown()
	cb.SetText("B")
	if cb.isOpen {
		t.Error("selecting via SetText should close the dropdown")
	}
	if cb.SelectedValue() != "B" {
		t.Errorf("value = %q, want B", cb.SelectedValue())
	}
}
