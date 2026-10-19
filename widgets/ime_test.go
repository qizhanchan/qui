package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

// These tests simulate IME events programmatically to exercise the
// widget-side (IMEClient) implementation without needing a real
// NSTextInputClient bridge. A real macOS backend would call the
// same Window.SetPreedit / CommitIME entry points with strings
// produced by NSTextInputClient delegate methods.

// ---- Input IMEClient ----

func focusedTFWithIME(s string) (*Window, *Input) {
	w := NewTestWindow(Size{W: 500, H: 500})
	tf := NewInput("placeholder")
	tf.Layout(Rect{X: 0, Y: 0, W: 200, H: 30})
	tf.Text = s
	tf.ed.cursor = len([]rune(s))
	w.SetRoot(tf)
	w.SetFocus(tf)
	tf.SetFocused(true)
	return w, tf
}

func TestInputSetPreeditStoresComposition(t *testing.T) {
	_, tf := focusedTFWithIME("ab")
	tf.SetPreedit("你", 1)
	if tf.ed.preedit != "你" {
		t.Errorf("preeditText = %q, want 你", tf.ed.preedit)
	}
	if tf.ed.preeditCursor != 1 {
		t.Errorf("preeditCursor = %d, want 1", tf.ed.preeditCursor)
	}
}

func TestInputCommitIMEInsertsAtCaret(t *testing.T) {
	_, tf := focusedTFWithIME("ab")
	tf.ed.cursor = 1
	tf.SetPreedit("你好", 2)
	tf.CommitIME("你好")

	if tf.Text != "a你好b" {
		t.Errorf("Text after commit = %q, want a你好b", tf.Text)
	}
	if tf.ed.cursor != 3 {
		t.Errorf("cursorPos after commit = %d, want 3", tf.ed.cursor)
	}
	if tf.ed.preedit != "" {
		t.Error("preeditText should clear after commit")
	}
}

func TestInputCommitIMEReplacesSelection(t *testing.T) {
	_, tf := focusedTFWithIME("abcde")
	tf.ed.selStart = 1
	tf.ed.selEnd = 4
	tf.ed.cursor = 4
	tf.CommitIME("X")
	if tf.Text != "aXe" {
		t.Errorf("Text = %q, want aXe (selection replaced)", tf.Text)
	}
}

func TestInputCommitEmptyIsNoop(t *testing.T) {
	_, tf := focusedTFWithIME("ab")
	original := tf.Text
	tf.SetPreedit("x", 1)
	tf.CommitIME("")
	if tf.Text != original {
		t.Errorf("empty commit mutated text; got %q", tf.Text)
	}
	// But preedit is cleared.
	if tf.ed.preedit != "" {
		t.Error("empty commit should still clear preedit")
	}
}

func TestInputCaretRectShiftsForPreedit(t *testing.T) {
	_, tf := focusedTFWithIME("")
	tf.ed.cursor = 0

	noPreedit := tf.CaretRect()
	tf.SetPreedit("你好", 2)
	withPreedit := tf.CaretRect()

	if withPreedit.X <= noPreedit.X {
		t.Errorf("preedit should push caret right; no=%v with=%v",
			noPreedit.X, withPreedit.X)
	}
}

func TestInputCommitFiresOnChange(t *testing.T) {
	gotText := ""
	_, tf := focusedTFWithIME("")
	tf.OnChange = func(s string) { gotText = s }
	tf.CommitIME("中")
	if gotText != "中" {
		t.Errorf("OnChange received %q, want 中", gotText)
	}
}

func TestInputTypicalIMESequence(t *testing.T) {
	// Simulate: user types "ni" → preedit "你",
	//          types "hao" → preedit "你好",
	//          selects from candidate → commit "你好".
	_, tf := focusedTFWithIME("hello ")
	tf.ed.cursor = 6

	tf.SetPreedit("你", 1)
	if tf.Text != "hello " { // Text not mutated during composition
		t.Errorf("Text changed during preedit; got %q", tf.Text)
	}

	tf.SetPreedit("你好", 2)
	if tf.Text != "hello " {
		t.Errorf("Text changed during preedit; got %q", tf.Text)
	}

	tf.CommitIME("你好")
	if tf.Text != "hello 你好" {
		t.Errorf("after commit: got %q, want hello 你好", tf.Text)
	}
	if tf.ed.preedit != "" {
		t.Error("preedit should clear after commit")
	}
}

// ---- TextArea IMEClient ----

func focusedTAWithIME(s string) (*Window, *TextArea) {
	w := NewTestWindow(Size{W: 500, H: 500})
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 300, H: 120})
	ta.Text = s
	ta.ed.cursor = len([]rune(s))
	w.SetRoot(ta)
	w.SetFocus(ta)
	ta.SetFocused(true)
	return w, ta
}

func TestTextAreaSetPreeditStores(t *testing.T) {
	_, ta := focusedTAWithIME("abc")
	ta.SetPreedit("日本語", 3)
	if ta.ed.preedit != "日本語" {
		t.Errorf("preedit = %q", ta.ed.preedit)
	}
	if ta.ed.preeditCursor != 3 {
		t.Errorf("preeditCursor = %d", ta.ed.preeditCursor)
	}
}

func TestTextAreaCommitInsertsMulti(t *testing.T) {
	_, ta := focusedTAWithIME("start ")
	ta.ed.cursor = 6
	ta.CommitIME("こんにちは")
	if ta.Text != "start こんにちは" {
		t.Errorf("after commit: %q", ta.Text)
	}
	// Cursor advanced by rune count, not byte count.
	if ta.ed.cursor != 6+5 {
		t.Errorf("cursorPos = %d, want 11", ta.ed.cursor)
	}
}

func TestTextAreaCommitReplacesSelection(t *testing.T) {
	_, ta := focusedTAWithIME("hello world")
	ta.ed.selStart = 6
	ta.ed.selEnd = 11
	ta.ed.cursor = 11
	ta.CommitIME("世界")
	if ta.Text != "hello 世界" {
		t.Errorf("selection commit: %q", ta.Text)
	}
}

func TestTextAreaCommitFiresOnChange(t *testing.T) {
	got := ""
	_, ta := focusedTAWithIME("")
	ta.OnChange = func(s string) { got = s }
	ta.CommitIME("テスト")
	if got != "テスト" {
		t.Errorf("OnChange = %q", got)
	}
}

// ---- Window-level routing ----

func TestWindowSetPreeditRoutesToFocused(t *testing.T) {
	w, tf := focusedTFWithIME("")
	w.SetPreedit("好", 1)
	if tf.ed.preedit != "好" {
		t.Errorf("Window.SetPreedit did not route; preeditText = %q", tf.ed.preedit)
	}
}

func TestWindowCommitIMERoutesToFocused(t *testing.T) {
	w, tf := focusedTFWithIME("ab")
	tf.ed.cursor = 2
	w.CommitIME("你")
	if tf.Text != "ab你" {
		t.Errorf("Window.CommitIME did not route; Text = %q", tf.Text)
	}
}

func TestWindowSetPreeditNoFocusIsSafe(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	// no focused widget — must not panic
	w.SetPreedit("x", 0)
	w.CommitIME("x")
}

func TestWindowCaretRectFromFocusedIMEClient(t *testing.T) {
	w, tf := focusedTFWithIME("abc")
	tf.ed.cursor = 3
	r := w.CaretRect()
	if r.W <= 0 || r.H <= 0 {
		t.Errorf("CaretRect should have non-zero W/H; got %+v", r)
	}
}

func TestWindowCaretRectNonIMEReturnsEmpty(t *testing.T) {
	w := NewTestWindow(Size{W: 500, H: 500})
	// Focus a non-IME widget (plain Label doesn't implement IMEClient).
	lbl := NewLabel("x")
	w.SetFocus(lbl)
	r := w.CaretRect()
	if !r.IsEmpty() {
		t.Errorf("non-IME focus should produce empty CaretRect; got %+v", r)
	}
}

// ---- Regression: preedit must clear on CharEvent commit ----
//
// Some IMEs (macOS 拼音 among them) commit via insertText without
// calling unmarkText afterwards. If we don't clear preedit on CharEvent
// ourselves, the stale marked text keeps rendering on top of the
// newly-committed characters.

func TestInputCharCommitClearsStalePreedit(t *testing.T) {
	_, tf := focusedTFWithIME("")
	// Simulate: IME mid-composition then commit via insertText.
	tf.SetPreedit("zhong'wen", 9)
	if tf.ed.preedit == "" {
		t.Fatal("precondition: preedit should be set")
	}
	// CharEvent (as GLFW's CharCallback would fire) on commit.
	tf.Handle(newCharEvent('z'))
	if tf.ed.preedit != "" {
		t.Errorf("preedit should clear on CharEvent; got %q", tf.ed.preedit)
	}
	if tf.Text != "z" {
		t.Errorf("Text after first char: %q, want z", tf.Text)
	}
}

func TestTextAreaCharCommitClearsStalePreedit(t *testing.T) {
	_, ta := focusedTAWithIME("")
	ta.SetPreedit("konnichiwa", 10)
	if ta.ed.preedit == "" {
		t.Fatal("precondition: preedit should be set")
	}
	// Feed the first commit char through Handle — TextArea dispatches
	// CharEvent via Handle switch statement.
	ta.Handle(newCharEvent('k'))
	if ta.ed.preedit != "" {
		t.Errorf("TextArea preedit should clear on CharEvent; got %q", ta.ed.preedit)
	}
}

// ---- Regression: KeyDown during IME composition must be swallowed ---
//
// GLFW fires our KeyCallback BEFORE handing the event to macOS
// interpretKeyEvents / input method. Without the preedit guard,
// Backspace would both shrink the preedit (via IME's subsequent
// setMarkedText) and delete a rune from Text — corrupting existing
// content the user didn't intend to change. Reproduces: user types
// "你好🫲" then starts composing "ni" then backspaces — the emoji
// 🫲 must stay intact.

func TestInputBackspaceDuringIMEDoesNotTouchText(t *testing.T) {
	_, tf := focusedTFWithIME("你好🫲")
	tf.ed.cursor = 3 // after 🫲
	tf.SetPreedit("ni", 2)

	// Simulate backspace while composing.
	tf.Handle(newKeyDown(KeyBackspace))

	// Text must stay "你好🫲". (IME will separately trim preedit via
	// setMarkedText; our widget doesn't see that in this unit test.)
	if tf.Text != "你好🫲" {
		t.Errorf("Text mutated during IME backspace: got %q, want 你好🫲", tf.Text)
	}
	if tf.ed.cursor != 3 {
		t.Errorf("cursorPos moved: got %d, want 3", tf.ed.cursor)
	}
}

func TestInputArrowDuringIMESwallowed(t *testing.T) {
	_, tf := focusedTFWithIME("abc")
	tf.ed.cursor = 3
	tf.SetPreedit("x", 1)

	tf.Handle(newKeyDown(KeyLeft))
	if tf.ed.cursor != 3 {
		t.Errorf("Left arrow during composition moved cursor; got %d", tf.ed.cursor)
	}
}

func TestInputEnterDuringIMEDoesNotSubmit(t *testing.T) {
	submitted := false
	_, tf := focusedTFWithIME("abc")
	tf.OnSubmit = func(string) { submitted = true }
	tf.SetPreedit("x", 1)

	tf.Handle(newKeyDown(KeyEnter))
	if submitted {
		t.Error("Enter during composition fired OnSubmit — IME should handle it")
	}
}

func TestTextAreaBackspaceDuringIMEDoesNotTouchText(t *testing.T) {
	_, ta := focusedTAWithIME("你好🫲")
	ta.ed.cursor = 3
	ta.SetPreedit("ni", 2)

	ta.Handle(newKeyDown(KeyBackspace))

	if ta.Text != "你好🫲" {
		t.Errorf("TextArea Text mutated during IME backspace: %q", ta.Text)
	}
}

func TestTextAreaEnterDuringIMEDoesNotInsertNewline(t *testing.T) {
	_, ta := focusedTAWithIME("a")
	ta.ed.cursor = 1
	ta.SetPreedit("ni", 2)

	ta.Handle(newKeyDown(KeyEnter))
	if ta.Text != "a" {
		t.Errorf("Enter during composition mutated Text: %q", ta.Text)
	}
}

// Once composition ends (preedit cleared), keys resume normal handling.
func TestInputKeysWorkAfterPreeditCleared(t *testing.T) {
	_, tf := focusedTFWithIME("abc")
	tf.ed.cursor = 3
	tf.SetPreedit("x", 1)
	// IME commits + unmarks → preedit cleared.
	tf.SetPreedit("", 0)

	// Now backspace should work normally.
	tf.Handle(newKeyDown(KeyBackspace))
	if tf.Text != "ab" {
		t.Errorf("post-composition backspace failed: %q want ab", tf.Text)
	}
}

// ---- IMEClient interface conformance (compile-time) ----

// Compile-time assertions that Input and TextArea satisfy IMEClient.
var (
	_ IMEClient = (*Input)(nil)
	_ IMEClient = (*TextArea)(nil)
)
