package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

func newCharEvent(r rune) CharEvent {
	return NewCharEvent(r, 0)
}

func focusedInput(text string) *Input {
	tf := NewInput("placeholder")
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: 30})
	tf.Text = text
	tf.cursorPos = len([]rune(text))
	tf.SetFocused(true)
	return tf
}

// ---- Text entry ----

func TestInputCharInputInserts(t *testing.T) {
	tf := focusedInput("")
	tf.Handle(newCharEvent('H'))
	tf.Handle(newCharEvent('i'))
	if tf.Text != "Hi" {
		t.Errorf("Text = %q, want Hi", tf.Text)
	}
	if tf.cursorPos != 2 {
		t.Errorf("cursorPos = %d, want 2", tf.cursorPos)
	}
}

func TestInputInsertMidText(t *testing.T) {
	tf := focusedInput("abcd")
	tf.cursorPos = 2
	tf.Handle(newCharEvent('X'))
	if tf.Text != "abXcd" {
		t.Errorf("Text = %q, want abXcd", tf.Text)
	}
}

func TestInputChangeFiresOnChange(t *testing.T) {
	got := ""
	tf := focusedInput("")
	tf.OnChange = func(s string) { got = s }
	tf.Handle(newCharEvent('a'))
	if got != "a" {
		t.Errorf("OnChange got %q, want a", got)
	}
}

// ---- Backspace / Delete ----

func TestInputBackspaceDeletesChar(t *testing.T) {
	tf := focusedInput("abc")
	tf.Handle(newKeyDown(KeyBackspace))
	if tf.Text != "ab" {
		t.Errorf("after backspace: %q, want ab", tf.Text)
	}
	if tf.cursorPos != 2 {
		t.Errorf("cursorPos = %d, want 2", tf.cursorPos)
	}
}

func TestInputBackspaceAtStartNoop(t *testing.T) {
	tf := focusedInput("abc")
	tf.cursorPos = 0
	tf.Handle(newKeyDown(KeyBackspace))
	if tf.Text != "abc" {
		t.Errorf("backspace at start should be no-op; got %q", tf.Text)
	}
}

// ---- Navigation ----

func TestInputArrowNavigation(t *testing.T) {
	tf := focusedInput("abc")
	tf.cursorPos = 3

	tf.Handle(newKeyDown(KeyLeft))
	if tf.cursorPos != 2 {
		t.Errorf("KeyLeft: cursorPos = %d, want 2", tf.cursorPos)
	}
	tf.Handle(newKeyDown(KeyRight))
	if tf.cursorPos != 3 {
		t.Errorf("KeyRight: cursorPos = %d, want 3", tf.cursorPos)
	}
}

func TestInputArrowBoundaryClamps(t *testing.T) {
	tf := focusedInput("x")
	tf.cursorPos = 0
	tf.Handle(newKeyDown(KeyLeft))
	if tf.cursorPos != 0 {
		t.Error("KeyLeft at start should not move below 0")
	}
	tf.cursorPos = 1
	tf.Handle(newKeyDown(KeyRight))
	if tf.cursorPos != 1 {
		t.Error("KeyRight at end should not move beyond len")
	}
}

func TestInputHomeEndNavigation(t *testing.T) {
	tf := focusedInput("hello")
	tf.cursorPos = 3

	tf.Handle(newKeyDown(KeyHome))
	if tf.cursorPos != 0 {
		t.Fatalf("KeyHome: cursorPos = %d, want 0", tf.cursorPos)
	}
	tf.Handle(newKeyDown(KeyEnd))
	if tf.cursorPos != 5 {
		t.Fatalf("KeyEnd: cursorPos = %d, want 5", tf.cursorPos)
	}
}

func TestInputShiftHomeEndExtendsSelection(t *testing.T) {
	tf := focusedInput("hello")
	tf.cursorPos = 2

	tf.Handle(NewKeyEvent(EventKeyDown, KeyEnd, ModShift))
	if tf.selStart != 2 || tf.selEnd != 5 || tf.cursorPos != 5 {
		t.Fatalf("Shift+End: selStart=%d selEnd=%d cursor=%d, want 2/5/5", tf.selStart, tf.selEnd, tf.cursorPos)
	}

	tf.Handle(NewKeyEvent(EventKeyDown, KeyHome, ModShift))
	if tf.selStart != 2 || tf.selEnd != 0 || tf.cursorPos != 0 {
		t.Fatalf("Shift+Home: selStart=%d selEnd=%d cursor=%d, want 2/0/0", tf.selStart, tf.selEnd, tf.cursorPos)
	}
}

// ---- Selection ----

func TestInputShiftArrowExtendsSelection(t *testing.T) {
	tf := focusedInput("hello")
	tf.cursorPos = 2
	tf.Handle(NewKeyEvent(EventKeyDown, KeyRight, ModShift))
	if !tf.hasSelection() {
		t.Fatal("Shift+Right should start selection")
	}
	a, b := tf.orderedSelection()
	if a != 2 || b != 3 {
		t.Errorf("selection = [%d, %d), want [2, 3)", a, b)
	}
}

func TestInputDoubleClickSelectsWord(t *testing.T) {
	tf := focusedInput("hello world")
	tf.Handle(newMouseEventForHandle(EventMouseDown, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseUp, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseDown, 24, 10))

	a, b := tf.orderedSelection()
	if a != 0 || b != 5 {
		t.Fatalf("double click selection = [%d,%d), want [0,5)", a, b)
	}
}

func TestInputTripleClickSelectsLine(t *testing.T) {
	tf := focusedInput("hello world")
	tf.Handle(newMouseEventForHandle(EventMouseDown, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseUp, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseDown, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseUp, 24, 10))
	tf.Handle(newMouseEventForHandle(EventMouseDown, 24, 10))

	a, b := tf.orderedSelection()
	if a != 0 || b != len([]rune(tf.Text)) {
		t.Fatalf("triple click selection = [%d,%d), want full line", a, b)
	}
}

func TestInputBeforeInputCanBlock(t *testing.T) {
	tf := focusedInput("abc")
	tf.BeforeInput = func(ev BeforeInputEvent) bool {
		return ev.Kind != "type"
	}
	tf.Handle(newCharEvent('X'))
	if tf.Text != "abc" {
		t.Fatalf("BeforeInput should block type, got %q", tf.Text)
	}
}

func TestInputCursorAndSelectionCallbacks(t *testing.T) {
	tf := focusedInput("abc")
	cursor := -1
	selectionCalls := 0
	tf.OnCursorChange = func(pos int) { cursor = pos }
	tf.OnSelectionChange = func(start, end int) { selectionCalls++; _, _ = start, end }

	tf.Handle(newKeyDown(KeyLeft))
	if cursor != 2 {
		t.Fatalf("OnCursorChange got %d, want 2", cursor)
	}
	tf.Handle(NewKeyEvent(EventKeyDown, KeyLeft, ModShift))
	if selectionCalls == 0 {
		t.Fatalf("OnSelectionChange should fire on shift selection")
	}
}

func TestInputBackspaceRemovesSelection(t *testing.T) {
	tf := focusedInput("hello")
	tf.cursorPos = 5
	tf.selStart = 1
	tf.selEnd = 4
	tf.Handle(newKeyDown(KeyBackspace))
	if tf.Text != "ho" {
		t.Errorf("deleting selection: got %q, want ho", tf.Text)
	}
	if tf.cursorPos != 1 {
		t.Errorf("cursor after selection delete: %d, want 1", tf.cursorPos)
	}
}

func TestInputCharReplacesSelection(t *testing.T) {
	tf := focusedInput("abcde")
	tf.selStart = 1
	tf.selEnd = 4
	tf.cursorPos = 4
	tf.Handle(newCharEvent('Z'))
	if tf.Text != "aZe" {
		t.Errorf("char with selection: got %q, want aZe", tf.Text)
	}
}

// ---- Enter / Escape ----

func TestInputEnterFiresOnSubmit(t *testing.T) {
	got := ""
	tf := focusedInput("hello")
	tf.OnSubmit = func(s string) { got = s }
	tf.Handle(newKeyDown(KeyEnter))
	if got != "hello" {
		t.Errorf("OnSubmit got %q, want hello", got)
	}
}

func TestInputEscapeClearsSelection(t *testing.T) {
	tf := focusedInput("abcde")
	tf.selStart = 0
	tf.selEnd = 3
	tf.Handle(newKeyDown(KeyEscape))
	if tf.hasSelection() {
		t.Error("Esc should clear selection")
	}
}

// ---- SetText ----

func TestInputSetTextResetsCursor(t *testing.T) {
	tf := focusedInput("old")
	tf.cursorPos = 2
	tf.SetText("replacement")
	if tf.Text != "replacement" {
		t.Errorf("Text = %q", tf.Text)
	}
	if tf.cursorPos != len([]rune("replacement")) {
		t.Errorf("cursor after SetText: %d, want %d", tf.cursorPos, len([]rune("replacement")))
	}
}

// ---- Tick (cursor blink) ----

func TestInputTickTogglesWhenFocused(t *testing.T) {
	tf := focusedInput("hi")
	// Fast-forward 600ms past the last blink timestamp.
	future := time.Now().Add(600 * time.Millisecond)
	dirty := tf.Tick(future)
	if dirty.IsEmpty() {
		t.Error("Tick after blink interval should return dirty rect")
	}
}

func TestInputTickNoopWhenNotFocused(t *testing.T) {
	tf := NewInput("")
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: 30})
	future := time.Now().Add(1 * time.Second)
	if !tf.Tick(future).IsEmpty() {
		t.Error("unfocused field should not blink")
	}
}

// ---- Disabled ----

func TestInputDisabledIgnoresInput(t *testing.T) {
	tf := focusedInput("x")
	tf.SetEnabled(false)
	tf.Handle(newCharEvent('a'))
	if tf.Text != "x" {
		t.Errorf("disabled field accepted input; got %q", tf.Text)
	}
}

// ---- Focusable ----

func TestInputFocusable(t *testing.T) {
	tf := NewInput("")
	if !tf.Focusable() {
		t.Error("Input should be Focusable by default")
	}
	tf.SetEnabled(false)
	if tf.Focusable() {
		t.Error("disabled Input should not be Focusable")
	}
}

// ---- Dense mode ----

func TestInputDenseMatchesButtonHeight(t *testing.T) {
	tf := NewInput("")
	tf.Label = "Name"
	tf.Dense = true
	// Dense Input aligns to the M3 button pill height (40 dp).
	// Use a Button configured with the same Height to represent that
	// reference — mirrors the M3 40 dp button pill.
	btn := NewButton("Ok", nil)
	btn.States.Base.Height = 40

	got := tf.Measure(Size{W: 1000, H: 200})
	want := btn.Measure(Size{W: 1000, H: 200})
	if got.H != want.H {
		t.Errorf("dense field height = %v, want button height %v", got.H, want.H)
	}
}

func TestInputDenseSuppressesFloatingLabel(t *testing.T) {
	tf := NewInput("")
	tf.Label = "Name"
	tf.Dense = true
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: tf.Measure(Size{W: 1000, H: 200}).H})

	var canvas RecordingCanvas
	tf.Draw(&canvas)
	// Dense + empty Text + no Placeholder + Label suppressed → 0 text draws.
	if len(canvas.Texts) != 0 {
		t.Errorf("dense Input drew %d texts, want 0 (label suppressed)", len(canvas.Texts))
	}
}

func TestInputDenseInputHasRoomForBodyLargeFont(t *testing.T) {
	// Regression: M3's 16+16 vertical padding leaves only 8 px of input
	// height in a 40-dp dense field; TextBodyLarge is ~22 px tall and
	// ClipCanvas crops the descenders. inputBounds must reserve enough
	// vertical space for the body-large face in Dense mode.
	tf := NewInput("")
	tf.Dense = true
	h := tf.Measure(Size{W: 1000, H: 200}).H
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: h})

	font := tf.Style().Font
	_, glyphH := TextMetrics("Mg", font) // includes descender
	input := tf.inputBounds()
	if input.H < glyphH {
		t.Errorf("dense inputBounds H = %v, too small for body-large height %v (descenders will clip)",
			input.H, glyphH)
	}
}

func TestInputDensePlaceholderShowsWithoutFocus(t *testing.T) {
	// With Label suppressed, placeholder should behave as if Label was
	// empty — visible whenever Text is empty, regardless of focus. Count
	// of 1 confirms placeholder draws AND that the suppressed label
	// didn't sneak through (otherwise we'd see 2 text draws).
	tf := NewInput("")
	tf.Label = "Name"
	tf.Placeholder = "type here"
	tf.Dense = true
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: tf.Measure(Size{W: 1000, H: 200}).H})

	var canvas RecordingCanvas
	tf.Draw(&canvas)
	if len(canvas.Texts) != 1 {
		t.Errorf("dense + placeholder + no focus: got %d texts, want 1 (just placeholder)", len(canvas.Texts))
	}
}

// OnCommit is the "value finished" hook: Enter, or focus leaving the field
// after an edit. Without it a numeric field either applies every intermediate
// rune (OnChange) or silently drops a typed value when the user clicks away
// instead of pressing Enter (OnSubmit).
func TestInputOnCommitFiresOnEnterAndBlur(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	in := NewInput("")
	in.SetID("field")
	win.SetRoot(in)
	in.Layout(Rect{X: 0, Y: 0, W: 300, H: 40})

	var commits []string
	in.OnCommit = func(s string) { commits = append(commits, s) }

	// Enter commits.
	win.SetFocus(in)
	in.SetText("12")
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	if len(commits) != 1 || commits[0] != "12" {
		t.Fatalf("commits after Enter = %v", commits)
	}
	// Enter again without an edit does NOT re-commit.
	win.DispatchTestEvent(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	if len(commits) != 1 {
		t.Errorf("unchanged value committed twice: %v", commits)
	}
	// An edit followed by blur commits.
	in.SetText("34")
	win.SetFocus(nil)
	if len(commits) != 2 || commits[1] != "34" {
		t.Fatalf("commits after blur = %v", commits)
	}
	// Focusing and blurring without editing commits nothing.
	win.SetFocus(in)
	win.SetFocus(nil)
	if len(commits) != 2 {
		t.Errorf("blur without an edit committed: %v", commits)
	}
}

// The window's loop sleeps when nothing changes, so a focused field must
// ask to be woken for its next caret flip — otherwise the caret freezes
// until the next input event.
func TestInputCaretBlinkRequestsItsNextTick(t *testing.T) {
	win := NewTestWindow(Size{W: 300, H: 100})
	in := NewInput("")
	win.SetRoot(in)
	in.Layout(Rect{X: 0, Y: 0, W: 300, H: 40})

	now := time.Now()
	in.Tick(now)
	if got := win.TickRequestForTest(); !got.IsZero() {
		t.Fatalf("unfocused field requested a tick at %v", got)
	}

	win.SetFocus(in)
	in.Tick(now)
	got := win.TickRequestForTest()
	if got.IsZero() {
		t.Fatal("focused field did not request its next caret flip")
	}
	if d := got.Sub(now); d <= 0 || d > 510*time.Millisecond {
		t.Fatalf("next flip requested %v from now, want within the 500ms blink", d)
	}
}
