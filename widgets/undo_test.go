package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// Pure history-struct tests — no widget involvement. Swap undoNow so
// coalescing decisions are deterministic.

func withFixedTime(t *testing.T, initial time.Time) *time.Time {
	t.Helper()
	orig := undoNow
	now := initial
	undoNow = func() time.Time { return now }
	t.Cleanup(func() { undoNow = orig })
	return &now
}

func mkEntry(text string) undoEntry {
	return undoEntry{text: text}
}

func TestHistoryPushSkipsWhenTextUnchanged(t *testing.T) {
	h := &undoHistory{}
	h.push("type", mkEntry("a"), mkEntry("a"))
	if len(h.undo) != 0 {
		t.Errorf("unchanged text should not push; stack=%d", len(h.undo))
	}
}

func TestHistoryCoalescesRapidTyping(t *testing.T) {
	now := withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}

	h.push("type", mkEntry(""), mkEntry("h"))
	*now = now.Add(100 * time.Millisecond)
	h.push("type", mkEntry("h"), mkEntry("he"))
	*now = now.Add(100 * time.Millisecond)
	h.push("type", mkEntry("he"), mkEntry("hel"))

	if len(h.undo) != 1 {
		t.Fatalf("expected 1 coalesced entry, got %d", len(h.undo))
	}
	if h.undo[0].text != "" {
		t.Errorf("coalesced entry's before-state should be \"\", got %q", h.undo[0].text)
	}
}

func TestHistoryBreaksCoalesceAfterWindow(t *testing.T) {
	now := withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}

	h.push("type", mkEntry(""), mkEntry("a"))
	*now = now.Add(600 * time.Millisecond) // past 500ms window
	h.push("type", mkEntry("a"), mkEntry("ab"))

	if len(h.undo) != 2 {
		t.Errorf("expected 2 entries across the window, got %d", len(h.undo))
	}
}

func TestHistoryDoesNotCoalesceAcrossKinds(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}

	h.push("type", mkEntry(""), mkEntry("a"))
	h.push("delete", mkEntry("a"), mkEntry(""))
	h.push("type", mkEntry(""), mkEntry("b"))

	if len(h.undo) != 3 {
		t.Errorf("type/delete/type should not merge; got %d entries", len(h.undo))
	}
}

func TestHistoryBreakCoalesceSealsGroup(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}

	h.push("type", mkEntry(""), mkEntry("a"))
	h.breakCoalesce()
	h.push("type", mkEntry("a"), mkEntry("ab"))

	if len(h.undo) != 2 {
		t.Errorf("breakCoalesce should force new entry; got %d", len(h.undo))
	}
}

func TestHistoryPushClearsRedo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}
	h.undo = append(h.undo, undoEntry{text: "a", kind: "type", when: time.Unix(0, 0)})
	h.redo = append(h.redo, undoEntry{text: "b"})

	h.push("paste", mkEntry("a"), mkEntry("ax"))
	if len(h.redo) != 0 {
		t.Errorf("new push should clear redo stack; len=%d", len(h.redo))
	}
}

func TestHistoryCapsAtMaxDepth(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}
	for i := 0; i < maxUndoDepth+50; i++ {
		h.push("paste", mkEntry(""), mkEntry("x")) // "paste" never coalesces
	}
	if len(h.undo) > maxUndoDepth {
		t.Errorf("stack exceeded cap: %d > %d", len(h.undo), maxUndoDepth)
	}
}

func TestHistoryPopUndoMovesCurrentToRedo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	h := &undoHistory{}
	h.push("set", mkEntry("old"), mkEntry("new"))

	prev := h.popUndo(undoEntry{text: "new"})
	if prev == nil || prev.text != "old" {
		t.Fatalf("popUndo returned %+v, want text=old", prev)
	}
	if len(h.redo) != 1 || h.redo[0].text != "new" {
		t.Errorf("current state not preserved on redo stack: %+v", h.redo)
	}
}

// ---- Input end-to-end ----

func TestInputUndoReversesType(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('h'))
	tf.Handle(newCharEvent('i'))

	tf.undo()

	// All typing within the coalesce window collapses to one entry —
	// one undo unwinds the entire "hi".
	if tf.Text != "" {
		t.Errorf("undo should restore empty text; got %q", tf.Text)
	}
}

func TestInputUndoReversesBackspace(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("hello")
	tf.cursorPos = 5
	tf.Handle(newKeyDown(KeyBackspace)) // deletes 'o'
	if tf.Text != "hell" {
		t.Fatalf("precondition: expected hell, got %q", tf.Text)
	}

	tf.undo()

	if tf.Text != "hello" {
		t.Errorf("undo after backspace: got %q, want hello", tf.Text)
	}
}

func TestInputRedoReappliesUndo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('a'))

	tf.undo()
	if tf.Text != "" {
		t.Fatal("undo failed")
	}
	tf.redo()
	if tf.Text != "a" {
		t.Errorf("redo should restore %q, got %q", "a", tf.Text)
	}
}

func TestInputNewEditClearsRedo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('a'))
	tf.undo()
	// Fresh input should invalidate redo history.
	tf.Handle(newCharEvent('b'))
	if !tf.redo() {
		// redo should return false — stack was cleared by the 'b' push.
		// Text should stay as-is.
	} else {
		t.Error("redo after new input should have been a no-op")
	}
}

func TestInputUndoReversesPaste(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	fc := withFakeClipboard(t)
	fc.buf = "XYZ"
	tf := focusedInput("ab")
	tf.cursorPos = 2
	tf.Handle(newCmdKeyDown(KeyV))
	if tf.Text != "abXYZ" {
		t.Fatalf("precondition: expected abXYZ, got %q", tf.Text)
	}

	tf.undo()

	if tf.Text != "ab" {
		t.Errorf("undo after paste: got %q, want ab", tf.Text)
	}
}

func TestInputUndoReversesCut(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	withFakeClipboard(t)
	tf := focusedInput("hello")
	tf.selStart, tf.selEnd, tf.cursorPos = 0, 5, 5
	tf.Handle(newCmdKeyDown(KeyX))
	if tf.Text != "" {
		t.Fatalf("precondition: cut should empty; got %q", tf.Text)
	}

	tf.undo()

	if tf.Text != "hello" {
		t.Errorf("undo after cut: got %q, want hello", tf.Text)
	}
}

func TestInputTypingBreaksOnSpace(t *testing.T) {
	now := withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('h'))
	tf.Handle(newCharEvent('i'))
	*now = now.Add(50 * time.Millisecond)
	tf.Handle(newCharEvent(' '))
	*now = now.Add(50 * time.Millisecond)
	tf.Handle(newCharEvent('y'))
	tf.Handle(newCharEvent('o'))

	// Two groups: "hi " and "yo" — one undo drops "yo".
	tf.undo()
	if tf.Text != "hi " {
		t.Errorf("undo after word boundary: got %q, want 'hi '", tf.Text)
	}
	tf.undo()
	if tf.Text != "" {
		t.Errorf("second undo: got %q, want empty", tf.Text)
	}
}

func TestInputCmdZInvokesUndo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('x'))
	tf.Handle(newCmdKeyDown(KeyZ))
	if tf.Text != "" {
		t.Errorf("Cmd+Z should undo; Text=%q", tf.Text)
	}
}

func TestInputCmdShiftZInvokesRedo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('x'))
	tf.Handle(newCmdKeyDown(KeyZ)) // undo

	shiftZ := newCmdKeyDown(KeyZ)
	shiftZ.Mods |= ModShift
	tf.Handle(shiftZ) // redo

	if tf.Text != "x" {
		t.Errorf("Cmd+Shift+Z should redo; Text=%q", tf.Text)
	}
}

func TestInputCmdYInvokesRedo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('x'))
	tf.Handle(newCmdKeyDown(KeyZ)) // undo
	tf.Handle(newCmdKeyDown(KeyY)) // redo (Windows alias)
	if tf.Text != "x" {
		t.Errorf("Cmd+Y should redo; Text=%q", tf.Text)
	}
}

func TestInputCursorMoveBreaksCoalesce(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.Handle(newCharEvent('a'))
	tf.Handle(newKeyDown(KeyLeft))
	tf.Handle(newCharEvent('b'))

	// Two undo groups separated by arrow move.
	tf.undo()
	if tf.Text != "a" {
		t.Errorf("after first undo: %q, want a", tf.Text)
	}
}

func TestInputSetTextIsUndoable(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("hello")
	tf.SetText("world")
	tf.undo()
	if tf.Text != "hello" {
		t.Errorf("SetText should be undoable; got %q", tf.Text)
	}
}

func TestInputIMECommitIsUndoable(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("")
	tf.SetPreedit("你", 1)
	tf.CommitIME("你好")
	if tf.Text != "你好" {
		t.Fatalf("precondition: CommitIME should insert")
	}
	tf.undo()
	if tf.Text != "" {
		t.Errorf("undo after IME commit: got %q, want empty", tf.Text)
	}
}

func TestInputUndoClearsStalePreedit(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	tf := focusedInput("a")
	tf.Handle(newCharEvent('b'))
	tf.preeditText = "??"
	tf.preeditCursor = 1

	tf.undo()

	if tf.preeditText != "" || tf.preeditCursor != 0 {
		t.Errorf("undo should drop preedit; got %q/%d", tf.preeditText, tf.preeditCursor)
	}
}

// ---- TextArea end-to-end ----

func TestTextAreaUndoReversesEnter(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	ta := focusedTextArea("abc")
	ta.cursorPos = 2
	ta.Handle(newKeyDown(KeyEnter))
	if ta.Text != "ab\nc" {
		t.Fatalf("precondition: expected ab\\nc, got %q", ta.Text)
	}
	ta.undo()
	if ta.Text != "abc" {
		t.Errorf("undo after enter: got %q, want abc", ta.Text)
	}
}

func TestTextAreaUndoAfterMultilinePaste(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	fc := withFakeClipboard(t)
	fc.buf = "X\nY"
	ta := focusedTextArea("ab")
	ta.cursorPos = 1
	ta.Handle(newCmdKeyDown(KeyV))
	ta.undo()
	if ta.Text != "ab" {
		t.Errorf("undo multi-line paste: got %q, want ab", ta.Text)
	}
}

func TestTextAreaRedoAfterUndo(t *testing.T) {
	withFixedTime(t, time.Unix(0, 0))
	ta := focusedTextArea("")
	ta.Handle(NewCharEvent('q', 0))
	ta.undo()
	ta.redo()
	if ta.Text != "q" {
		t.Errorf("redo after undo: got %q, want q", ta.Text)
	}
}
