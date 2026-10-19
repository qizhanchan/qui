package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// Input and TextArea share one editing model. These tests drive both through
// the same script and assert the same result, so the two controls cannot
// drift apart again. Where single-line and multi-line editing legitimately
// differ (newlines in a paste, Enter, Up/Down) the case says so explicitly
// with a per-widget expectation instead of being left out.

// parityEditor is the surface both text controls expose.
type parityEditor interface {
	Widget
	Handle(Event) bool
	SetText(string)
	SetFocused(bool)
	TextState() TextState
	SelectRange(start, end int)
	SetCursorPos(int)
	InsertText(string)
	CommitIME(string)
	SetPreedit(string, int)
	NotifyTextChanged()
}

type parityField struct {
	name    string
	ed      parityEditor
	changes int
	commits int
}

func newParityFields(t *testing.T) []*parityField {
	t.Helper()
	// Deterministic undo coalescing: every edit lands inside the window.
	orig := undoNow
	now := time.Unix(1_700_000_000, 0)
	undoNow = func() time.Time { return now }
	t.Cleanup(func() { undoNow = orig })

	in := NewInput("")
	ta := NewTextArea("")
	fields := []*parityField{{name: "Input", ed: in}, {name: "TextArea", ed: ta}}
	in.OnChange = func(string) { fields[0].changes++ }
	in.OnCommit = func(string) { fields[0].commits++ }
	ta.OnChange = func(string) { fields[1].changes++ }
	ta.OnCommit = func(string) { fields[1].commits++ }
	for _, f := range fields {
		f.ed.Layout(Rect{X: 0, Y: 0, W: 400, H: 120})
		f.ed.SetFocused(true)
	}
	return fields
}

func (f *parityField) key(k Key, mods Modifiers) bool {
	return f.ed.Handle(NewKeyEvent(EventKeyDown, k, mods))
}

func (f *parityField) typeText(s string) {
	for _, r := range s {
		f.ed.Handle(CharEvent{Rune: r})
	}
}

// seed sets text silently and parks the caret, without counting as a change.
func (f *parityField) seed(text string, caret int) {
	f.ed.SetText(text)
	f.ed.SetCursorPos(caret)
	f.changes = 0
}

func assertState(t *testing.T, f *parityField, want TextState) {
	t.Helper()
	got := f.ed.TextState()
	got.CanUndo, got.CanRedo = false, false
	if got != want {
		t.Errorf("%s: state = %+v, want %+v", f.name, got, want)
	}
}

func noSel(value string, caret int) TextState {
	return TextState{Value: value, Caret: caret, SelStart: -1, SelEnd: 0}
}

func TestParityTyping(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.typeText("ab")
		assertState(t, f, noSel("ab", 2))
		if f.changes != 2 {
			t.Errorf("%s: OnChange fired %d times for two characters", f.name, f.changes)
		}
	}
}

// Control characters never become text; DEL (0x7F) is a control character
// too, not a printable rune.
func TestParityControlCharsIgnored(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.typeText("a\x01\x7Fb")
		assertState(t, f, noSel("ab", 2))
	}
}

// A programmatic SetText is silent and parks the caret at the end, like
// assigning an HTML control's value.
func TestParitySetTextSilentCaretAtEnd(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.ed.SetText("hello")
		assertState(t, f, noSel("hello", 5))
		if f.changes != 0 {
			t.Errorf("%s: SetText fired OnChange %d times", f.name, f.changes)
		}
		f.ed.NotifyTextChanged()
		if f.changes != 1 {
			t.Errorf("%s: NotifyTextChanged fired OnChange %d times, want 1", f.name, f.changes)
		}
	}
}

func TestParityBackspaceAndDeleteGraphemes(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("a👌🏻b", 3) // caret after the emoji cluster (a + 2 runes)
		f.key(KeyBackspace, 0)
		assertState(t, f, noSel("ab", 1))
		f.key(KeyDelete, 0)
		assertState(t, f, noSel("a", 1))
		f.key(KeyDelete, 0) // at the end: no-op, still handled
		assertState(t, f, noSel("a", 1))
		if f.changes != 2 {
			t.Errorf("%s: OnChange fired %d times, want 2", f.name, f.changes)
		}
	}
}

func TestParitySelectAllTypeReplaces(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 0)
		f.key(KeyA, cmdMods())
		assertState(t, f, TextState{Value: "hello", Caret: 5, SelStart: 0, SelEnd: 5})
		f.typeText("x")
		assertState(t, f, noSel("x", 1))
	}
}

func TestParityShiftHomeEndSelect(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 2)
		f.key(KeyHome, ModShift)
		assertState(t, f, TextState{Value: "hello", Caret: 0, SelStart: 2, SelEnd: 0})
		f.key(KeyEnd, 0)
		assertState(t, f, noSel("hello", 5))
		f.key(KeyLeft, ModShift)
		assertState(t, f, TextState{Value: "hello", Caret: 4, SelStart: 5, SelEnd: 4})
		f.key(KeyRight, 0)
		assertState(t, f, noSel("hello", 5))
	}
}

func TestParityCutCopyPaste(t *testing.T) {
	for _, f := range newParityFields(t) {
		fc := withFakeClipboard(t)
		f.seed("hello world", 0)
		f.ed.SelectRange(0, 5)
		f.key(KeyC, cmdMods())
		if fc.buf != "hello" {
			t.Errorf("%s: copy put %q on the clipboard", f.name, fc.buf)
		}
		f.key(KeyX, cmdMods())
		assertState(t, f, noSel(" world", 0))
		f.key(KeyEnd, 0)
		f.key(KeyV, cmdMods())
		assertState(t, f, noSel(" worldhello", 11))
		if f.changes != 2 {
			t.Errorf("%s: OnChange fired %d times for cut+paste, want 2", f.name, f.changes)
		}
	}
}

// The one intended paste difference: a single-line field folds newlines to
// spaces; a multi-line field normalizes them to '\n'.
func TestParityPasteNewlinePolicy(t *testing.T) {
	want := map[string]string{"Input": "a b c", "TextArea": "a\nb\nc"}
	for _, f := range newParityFields(t) {
		fc := withFakeClipboard(t)
		fc.buf = "a\r\nb\rc"
		f.key(KeyV, cmdMods())
		if got := f.ed.TextState().Value; got != want[f.name] {
			t.Errorf("%s: pasted %q, want %q", f.name, got, want[f.name])
		}
	}
}

func TestParityUndoRedo(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.typeText("ab cd")
		f.key(KeyZ, cmdMods())
		assertState(t, f, noSel("ab ", 3))
		f.key(KeyZ, cmdMods())
		assertState(t, f, noSel("", 0))
		f.key(KeyZ, cmdMods()|ModShift)
		assertState(t, f, noSel("ab ", 3))
		if f.changes != 5+3 {
			t.Errorf("%s: OnChange fired %d times, want 8 (5 typed + 3 undo/redo)", f.name, f.changes)
		}
	}
}

// Escape drops a selection (handled); with none it is not the field's key.
func TestParityEscape(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 0)
		f.ed.SelectRange(1, 3)
		if !f.key(KeyEscape, 0) {
			t.Errorf("%s: Escape with a selection was not handled", f.name)
		}
		assertState(t, f, noSel("hello", 3))
		if f.key(KeyEscape, 0) {
			t.Errorf("%s: Escape without a selection was swallowed", f.name)
		}
	}
}

// Caret-only API calls leave the canonical "no selection" state.
func TestParityCaretAPIs(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 0)
		f.ed.SelectRange(1, 4)
		assertState(t, f, TextState{Value: "hello", Caret: 4, SelStart: 1, SelEnd: 4})
		f.ed.SetCursorPos(2)
		assertState(t, f, noSel("hello", 2))
		f.ed.SelectRange(3, 3)
		assertState(t, f, noSel("hello", 3))
		f.ed.SetCursorPos(99)
		assertState(t, f, noSel("hello", 5))
	}
}

func TestParityInsertTextAndIMEReplaceSelection(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 0)
		f.ed.SelectRange(1, 4)
		f.ed.InsertText("EY")
		assertState(t, f, noSel("hEYo", 3))
		f.ed.SelectRange(0, 1)
		f.ed.CommitIME("你")
		assertState(t, f, noSel("你EYo", 1))
		if f.changes != 2 {
			t.Errorf("%s: OnChange fired %d times, want 2", f.name, f.changes)
		}
	}
}

// While an IME composes, editing keys belong to the input method.
func TestParityPreeditSwallowsKeys(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("ab", 2)
		f.ed.SetPreedit("ni", 2)
		if !f.key(KeyBackspace, 0) {
			t.Errorf("%s: Backspace during composition was not swallowed", f.name)
		}
		st := f.ed.TextState()
		if st.Value != "ab" || st.Preedit != "ni" {
			t.Errorf("%s: state %+v after Backspace during composition", f.name, st)
		}
	}
}

// Blur keeps the selection (a browser restores it on refocus) and commits an
// edited value exactly once.
func TestParityBlurKeepsSelectionAndCommits(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.typeText("hello")
		f.ed.SelectRange(1, 3)
		f.ed.SetFocused(false)
		assertState(t, f, TextState{Value: "hello", Caret: 3, SelStart: 1, SelEnd: 3})
		if f.commits != 1 {
			t.Errorf("%s: OnCommit fired %d times on blur after an edit, want 1", f.name, f.commits)
		}
		f.ed.SetFocused(true)
		f.ed.SetFocused(false)
		if f.commits != 1 {
			t.Errorf("%s: blur without an edit committed again (%d)", f.name, f.commits)
		}
	}
}

// Double-click selects a word, triple-click the line (the whole value of a
// single-line field).
func TestParityMultiClickSelection(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello world", 0)
		// A point safely inside the first word for both controls.
		x, y := float32(20), float32(10)
		if in, ok := f.ed.(*Input); ok {
			c := in.inputBounds()
			x, y = c.X+6, c.Y+c.H/2
		} else if ta, ok := f.ed.(*TextArea); ok {
			c, _, _ := ta.visibleContentRect(ta.Bounds())
			x, y = c.X+6, c.Y+4
		}
		click := func() {
			f.ed.Handle(NewMouseEvent(EventMouseDown, x, y, MouseButtonLeft, 0))
			f.ed.Handle(NewMouseEvent(EventMouseUp, x, y, MouseButtonLeft, 0))
		}
		click()
		click()
		assertState(t, f, TextState{Value: "hello world", Caret: 5, SelStart: 0, SelEnd: 5})
		click()
		assertState(t, f, TextState{Value: "hello world", Caret: 11, SelStart: 0, SelEnd: 11})
	}
}

// A plain click leaves no selection, in the canonical form.
func TestParityClickCollapsesSelection(t *testing.T) {
	for _, f := range newParityFields(t) {
		f.seed("hello", 0)
		f.ed.SelectRange(0, 5)
		b := f.ed.Bounds()
		f.ed.Handle(NewMouseEvent(EventMouseDown, b.X+b.W-4, b.Y+b.H/2, MouseButtonLeft, 0))
		f.ed.Handle(NewMouseEvent(EventMouseUp, b.X+b.W-4, b.Y+b.H/2, MouseButtonLeft, 0))
		st := f.ed.TextState()
		if st.SelStart != -1 || st.SelEnd != 0 {
			t.Errorf("%s: click left selection (%d, %d), want (-1, 0)", f.name, st.SelStart, st.SelEnd)
		}
	}
}
