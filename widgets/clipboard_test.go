package widgets

import (
	"runtime"
	"testing"

	. "github.com/qizhanchan/qui"
)

// fakeClipboard is a swappable, in-memory Clipboard used so these tests
// don't need a real GLFW window. Installed via SetClipboardProvider
// and torn down with SetClipboardProvider(nil) in t.Cleanup.
type fakeClipboard struct{ buf string }

func (f *fakeClipboard) Get() string     { return f.buf }
func (f *fakeClipboard) Set(text string) { f.buf = text }

// cmdMods returns the platform's "command" modifier bit — mirrors
// isCommandMod in clipboard.go. Tests need the same branch so Cmd+C
// on mac and Ctrl+C on Linux both hit the same code path.
func cmdMods() Modifiers {
	if runtime.GOOS == "darwin" {
		return ModSuper
	}
	return ModControl
}

func newCmdKeyDown(k Key) KeyEvent {
	return NewKeyEvent(EventKeyDown, k, cmdMods())
}

func withFakeClipboard(t *testing.T) *fakeClipboard {
	t.Helper()
	fc := &fakeClipboard{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })
	return fc
}

// ---- Input ----

func TestInputCopyPutsSelectionOnClipboard(t *testing.T) {
	fc := withFakeClipboard(t)
	tf := focusedInput("hello world")
	tf.selStart, tf.selEnd, tf.cursorPos = 0, 5, 5

	tf.Handle(newCmdKeyDown(KeyC))

	if fc.buf != "hello" {
		t.Errorf("clipboard = %q, want hello", fc.buf)
	}
	// Copy must not mutate text or clear selection.
	if tf.Text != "hello world" {
		t.Errorf("Text mutated by copy: %q", tf.Text)
	}
	if !tf.hasSelection() {
		t.Error("copy cleared the selection; it should preserve it")
	}
}

func TestInputCopyNoSelectionIsNoop(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "stale"
	tf := focusedInput("hello")

	tf.Handle(newCmdKeyDown(KeyC))

	if fc.buf != "stale" {
		t.Errorf("clipboard clobbered with no-selection copy: %q", fc.buf)
	}
}

func TestInputCutPutsAndDeletes(t *testing.T) {
	fc := withFakeClipboard(t)
	tf := focusedInput("hello world")
	tf.selStart, tf.selEnd, tf.cursorPos = 6, 11, 11

	tf.Handle(newCmdKeyDown(KeyX))

	if fc.buf != "world" {
		t.Errorf("clipboard = %q, want world", fc.buf)
	}
	if tf.Text != "hello " {
		t.Errorf("Text after cut = %q, want 'hello '", tf.Text)
	}
	if tf.hasSelection() {
		t.Error("cut should clear the selection")
	}
	if tf.cursorPos != 6 {
		t.Errorf("cursor after cut = %d, want 6", tf.cursorPos)
	}
}

func TestInputPasteInsertsAtCaret(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "XYZ"
	tf := focusedInput("ab")
	tf.cursorPos = 1

	tf.Handle(newCmdKeyDown(KeyV))

	if tf.Text != "aXYZb" {
		t.Errorf("Text after paste = %q, want aXYZb", tf.Text)
	}
	if tf.cursorPos != 4 {
		t.Errorf("cursor after paste = %d, want 4", tf.cursorPos)
	}
}

func TestInputPasteReplacesSelection(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "Z"
	tf := focusedInput("abcd")
	tf.selStart, tf.selEnd, tf.cursorPos = 1, 3, 3

	tf.Handle(newCmdKeyDown(KeyV))

	if tf.Text != "aZd" {
		t.Errorf("Text = %q, want aZd", tf.Text)
	}
	if tf.hasSelection() {
		t.Error("paste should clear the selection")
	}
}

func TestInputPasteStripsNewlines(t *testing.T) {
	fc := withFakeClipboard(t)
	// Rich source: CRLF, LF, CR — Input is single-line, so each
	// must collapse to a space.
	fc.buf = "a\r\nb\nc\rd"
	tf := focusedInput("")

	tf.Handle(newCmdKeyDown(KeyV))

	want := "a b c d"
	if tf.Text != want {
		t.Errorf("Text = %q, want %q", tf.Text, want)
	}
}

func TestInputSelectAllSelectsEverything(t *testing.T) {
	withFakeClipboard(t)
	tf := focusedInput("hello")
	tf.cursorPos = 2

	tf.Handle(newCmdKeyDown(KeyA))

	if !tf.hasSelection() {
		t.Fatal("Cmd+A should establish a selection")
	}
	a, b := tf.orderedSelection()
	if a != 0 || b != 5 {
		t.Errorf("selection = [%d, %d), want [0, 5)", a, b)
	}
}

func TestInputCopyWithCJK(t *testing.T) {
	fc := withFakeClipboard(t)
	tf := focusedInput("你好世界")
	tf.selStart, tf.selEnd, tf.cursorPos = 0, 2, 2

	tf.Handle(newCmdKeyDown(KeyC))

	if fc.buf != "你好" {
		t.Errorf("clipboard = %q, want 你好", fc.buf)
	}
}

// ---- TextArea ----

func focusedTextArea(text string) *TextArea {
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 300, H: 120})
	ta.Text = text
	ta.cursorPos = len([]rune(text))
	ta.SetFocused(true)
	return ta
}

func TestTextAreaCopyPutsSelectionOnClipboard(t *testing.T) {
	fc := withFakeClipboard(t)
	ta := focusedTextArea("line one\nline two")
	ta.selStart, ta.selEnd, ta.cursorPos = 0, 8, 8

	ta.Handle(newCmdKeyDown(KeyC))

	if fc.buf != "line one" {
		t.Errorf("clipboard = %q, want line one", fc.buf)
	}
}

func TestTextAreaCutDeletesSelection(t *testing.T) {
	fc := withFakeClipboard(t)
	ta := focusedTextArea("abcdef")
	ta.selStart, ta.selEnd, ta.cursorPos = 2, 4, 4

	ta.Handle(newCmdKeyDown(KeyX))

	if fc.buf != "cd" {
		t.Errorf("clipboard = %q, want cd", fc.buf)
	}
	if ta.Text != "abef" {
		t.Errorf("Text after cut = %q, want abef", ta.Text)
	}
	if ta.cursorPos != 2 {
		t.Errorf("cursor after cut = %d, want 2", ta.cursorPos)
	}
}

func TestTextAreaPasteKeepsNewlines(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "X\nY"
	ta := focusedTextArea("ab")
	ta.cursorPos = 1

	ta.Handle(newCmdKeyDown(KeyV))

	if ta.Text != "aX\nYb" {
		t.Errorf("Text = %q, want aX\\nYb", ta.Text)
	}
	// cursor should land after the pasted 3 runes ('X', '\n', 'Y').
	if ta.cursorPos != 4 {
		t.Errorf("cursor = %d, want 4", ta.cursorPos)
	}
}

func TestTextAreaPasteReplacesSelection(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "ZZ"
	ta := focusedTextArea("abcde")
	ta.selStart, ta.selEnd, ta.cursorPos = 1, 4, 4

	ta.Handle(newCmdKeyDown(KeyV))

	if ta.Text != "aZZe" {
		t.Errorf("Text = %q, want aZZe", ta.Text)
	}
}

func TestTextAreaSelectAllCoversAllRunes(t *testing.T) {
	withFakeClipboard(t)
	ta := focusedTextArea("你好\n世界")

	ta.Handle(newCmdKeyDown(KeyA))

	if ta.selStart != 0 {
		t.Errorf("selStart = %d, want 0", ta.selStart)
	}
	runes := []rune(ta.Text)
	if ta.selEnd != len(runes) {
		t.Errorf("selEnd = %d, want %d", ta.selEnd, len(runes))
	}
}

// ---- IME interaction: clipboard shortcuts must be swallowed by IME ----

func TestInputClipboardDuringIMEIsSwallowed(t *testing.T) {
	fc := withFakeClipboard(t)
	fc.buf = "stale"
	tf := focusedInput("ab")
	tf.SetPreedit("你", 1)

	tf.Handle(newCmdKeyDown(KeyC))
	tf.Handle(newCmdKeyDown(KeyV))

	if fc.buf != "stale" {
		t.Errorf("IME swallow broken; clipboard changed: %q", fc.buf)
	}
	if tf.Text != "ab" {
		t.Errorf("Text mutated during IME: %q", tf.Text)
	}
}

// ---- Provider plumbing ----

func TestSetClipboardProviderNilReverts(t *testing.T) {
	fc := &fakeClipboard{buf: "x"}
	SetClipboardProvider(fc)
	if GetClipboardText() != "x" {
		t.Fatal("provider install didn't take effect")
	}
	SetClipboardProvider(nil)
	t.Cleanup(func() { SetClipboardProvider(nil) })
	if GetClipboardText() != "" {
		t.Error("nil reset should return empty clipboard")
	}
	// Writes against noop provider must not panic and must not leak
	// back into the previous provider.
	SetClipboardText("y")
	if fc.buf != "x" {
		t.Errorf("fake clipboard mutated after detach: %q", fc.buf)
	}
}
