package widgets

import (
	"math"
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

func TestTextAreaDefaultAutoWrapOff(t *testing.T) {
	ta := NewTextArea("")
	if ta.AutoWrap {
		t.Fatalf("AutoWrap default = true, want false")
	}
}

func TestTextAreaVisualLinesNoWrapByDefault(t *testing.T) {
	ta := NewTextArea("")
	runes := []rune("abcdefghij")
	lines := ta.visualLines(runes, 10) // very narrow width
	if len(lines) != 1 {
		t.Fatalf("no-wrap lines = %d, want 1", len(lines))
	}
	if lines[0].start != 0 || lines[0].end != len(runes) {
		t.Fatalf("line bounds = [%d,%d), want [0,%d)", lines[0].start, lines[0].end, len(runes))
	}
}

func TestTextAreaVisualLinesWrapWhenEnabled(t *testing.T) {
	ta := NewTextArea("")
	ta.AutoWrap = true
	runes := []rune("abcdefghij")
	lines := ta.visualLines(runes, 10) // narrow width should force wrap
	if len(lines) <= 1 {
		t.Fatalf("auto-wrap lines = %d, want > 1", len(lines))
	}
}

func TestTextAreaNoWrapScrollsHorizontallyToCursor(t *testing.T) {
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 120, H: 80})
	ta.Style().Padding = Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}
	ta.Text = "111111111111111111111111111111"
	ta.cursorPos = len([]rune(ta.Text))

	ta.scrollToCursor()

	if ta.scrollX <= 0 {
		t.Fatalf("scrollX = %v, want > 0", ta.scrollX)
	}
}

func TestTextAreaNoWrapKeepsCaretRightMargin(t *testing.T) {
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 120, H: 80})
	ta.Style().Padding = Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}
	ta.Text = "111111111111111111111111111111"
	ta.cursorPos = len([]rune(ta.Text))

	content := ta.Bounds().Inset(ta.Style().Padding)
	cx, _ := ta.getCursorPosition(content)
	ta.scrollToCursor()

	visibleCaretX := cx - ta.scrollX
	maxCaretX := content.X + content.W - 4
	if visibleCaretX > maxCaretX+0.001 {
		t.Fatalf("visible caret x = %v, want <= %v", visibleCaretX, maxCaretX)
	}
}

func TestTextAreaScrollBarVisibilityFlags(t *testing.T) {
	ta := NewTextArea("")
	ta.AutoWrap = false
	// Height chosen so 3 lines overflow at the native default font/padding.
	ta.Layout(Rect{X: 0, Y: 0, W: 120, H: 40})
	ta.Text = "111111111111111111111111111111\n2222222222222222222222\n3333333333333333333333"

	_, showX, showY := ta.visibleContentRect(ta.Bounds())
	if !showX {
		t.Fatalf("showX = false, want true for long no-wrap content")
	}
	if !showY {
		t.Fatalf("showY = false, want true for multi-line overflow")
	}

	ta.ShowScrollBarX = false
	ta.ShowScrollBarY = false
	_, showX, showY = ta.visibleContentRect(ta.Bounds())
	if showX || showY {
		t.Fatalf("showX/showY = %v/%v, want false/false when disabled", showX, showY)
	}
}

func TestTextAreaAutoSizeMeasureFollowsContent(t *testing.T) {
	ta := NewTextArea("")
	ta.AutoSizeWidth = true
	ta.AutoSizeHeight = true
	ta.MinWidth = 0
	ta.MinHeight = 0
	ta.Text = "12345\n12"

	size := ta.Measure(Size{})
	wantW := ta.naturalContentWidth() + ta.Style().Padding.Horizontal()
	wantH := float32(ta.logicalLineCount())*ta.lineHeight() + ta.Style().Padding.Vertical()

	if math.Abs(float64(size.W-wantW)) > 0.51 {
		t.Fatalf("auto width = %v, want ~%v", size.W, wantW)
	}
	if math.Abs(float64(size.H-wantH)) > 0.51 {
		t.Fatalf("auto height = %v, want ~%v", size.H, wantH)
	}
}

func TestTextAreaAutoSizeNoOverflowDoesNotScrollHorizontally(t *testing.T) {
	ta := NewTextArea("")
	ta.AutoSizeWidth = true
	ta.MinWidth = 0
	ta.Style().Padding = Insets{Top: 0, Right: 4, Bottom: 0, Left: 4}
	ta.Text = "123123123123"
	ta.cursorPos = len([]rune(ta.Text))

	w := ta.naturalContentWidth() + ta.Style().Padding.Horizontal()
	ta.Layout(Rect{X: 0, Y: 0, W: w, H: ta.lineHeight()})
	ta.scrollToCursor()

	if ta.scrollX != 0 {
		t.Fatalf("scrollX = %v, want 0 when content exactly fits", ta.scrollX)
	}
}

func TestTextAreaEnterAddsNewlineCanBeCustomized(t *testing.T) {
	ta := focusedTextArea("ab")
	ta.cursorPos = 1
	ta.EnterAddsNewline = func(e KeyEvent) bool {
		return e.Mods&ModAlt != 0
	}

	// Plain Enter should not insert newline when custom matcher rejects it.
	handled := ta.Handle(NewKeyEvent(EventKeyDown, KeyEnter, 0))
	if handled {
		t.Fatalf("plain Enter should not be handled as newline")
	}
	if ta.Text != "ab" {
		t.Fatalf("plain Enter mutated text: %q", ta.Text)
	}

	// Alt+Enter should insert newline.
	handled = ta.Handle(NewKeyEvent(EventKeyDown, KeyEnter, ModAlt))
	if !handled {
		t.Fatalf("Alt+Enter should be handled")
	}
	if ta.Text != "a\nb" {
		t.Fatalf("Alt+Enter text = %q, want a\\nb", ta.Text)
	}
}

func TestTextAreaAutoSizeInvalidatesLayoutOnSetText(t *testing.T) {
	ta := NewTextArea("")
	ta.AutoSizeWidth = true
	ta.AutoSizeHeight = true
	ta.SetText("hello")
	if !ta.IsLayoutDirty() {
		t.Fatalf("expected layout dirty after SetText with autosize enabled")
	}
}

func TestTextAreaVerticalScrollbarDrag(t *testing.T) {
	ta := NewTextArea("")
	ta.Layout(Rect{X: 0, Y: 0, W: 200, H: 120})
	ta.Text = "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14"

	content, _, showY := ta.visibleContentRect(ta.Bounds())
	if !showY {
		t.Fatalf("expected vertical scrollbar to be visible")
	}
	thumb := ta.verticalBarThumb(content)
	mx := thumb.X + thumb.W/2
	my := thumb.Y + thumb.H/2

	ta.Handle(newMouseEventForHandle(EventMouseDown, mx, my))
	if !ta.barDraggingY {
		t.Fatalf("mouse down on vertical thumb should start drag")
	}
	ta.Handle(newMouseEventForHandle(EventMouseMove, mx, my+30))
	if ta.scrollY <= 0 {
		t.Fatalf("dragging vertical thumb should move scrollY, got %v", ta.scrollY)
	}
	ta.Handle(newMouseEventForHandle(EventMouseUp, mx, my+30))
	if ta.barDraggingY {
		t.Fatalf("mouse up should stop vertical thumb dragging")
	}
}

func TestTextAreaHomeEndMovesWithinCurrentLine(t *testing.T) {
	ta := focusedTextArea("abc\ndef")
	ta.cursorPos = 5 // on second line after 'd'

	ta.Handle(newKeyDown(KeyHome))
	if ta.cursorPos != 4 {
		t.Fatalf("KeyHome cursorPos=%d, want 4", ta.cursorPos)
	}

	ta.Handle(newKeyDown(KeyEnd))
	if ta.cursorPos != 7 {
		t.Fatalf("KeyEnd cursorPos=%d, want 7", ta.cursorPos)
	}
}

func TestTextAreaShiftHomeEndExtendsSelection(t *testing.T) {
	ta := focusedTextArea("abc\ndef")
	ta.cursorPos = 5

	ta.Handle(NewKeyEvent(EventKeyDown, KeyEnd, ModShift))
	if ta.selStart != 5 || ta.selEnd != 7 || ta.cursorPos != 7 {
		t.Fatalf("Shift+End selStart=%d selEnd=%d cursor=%d, want 5/7/7", ta.selStart, ta.selEnd, ta.cursorPos)
	}

	ta.Handle(NewKeyEvent(EventKeyDown, KeyHome, ModShift))
	if ta.selStart != 5 || ta.selEnd != 4 || ta.cursorPos != 4 {
		t.Fatalf("Shift+Home selStart=%d selEnd=%d cursor=%d, want 5/4/4", ta.selStart, ta.selEnd, ta.cursorPos)
	}
}

func TestTextAreaDoubleClickSelectsWord(t *testing.T) {
	ta := focusedTextArea("hello world\nnext")
	e1 := MouseEvent{When: time.Now(), X: 24, Y: 10, Button: MouseButtonLeft}
	e2 := MouseEvent{When: e1.When.Add(100 * time.Millisecond), X: 24, Y: 10, Button: MouseButtonLeft}

	ta.handleMouseDown(e1)
	ta.handleMouseUp()
	ta.handleMouseDown(e2)

	start, end := ta.selStart, ta.selEnd
	if start > end {
		start, end = end, start
	}
	if start != 0 || end != 5 {
		t.Fatalf("double click selection = [%d,%d), want [0,5)", start, end)
	}
}

func TestTextAreaTripleClickSelectsLogicalLine(t *testing.T) {
	ta := focusedTextArea("hello world\nnext")
	e1 := MouseEvent{When: time.Now(), X: 24, Y: 10, Button: MouseButtonLeft}
	e2 := MouseEvent{When: e1.When.Add(80 * time.Millisecond), X: 24, Y: 10, Button: MouseButtonLeft}
	e3 := MouseEvent{When: e1.When.Add(160 * time.Millisecond), X: 24, Y: 10, Button: MouseButtonLeft}

	ta.handleMouseDown(e1)
	ta.handleMouseUp()
	ta.handleMouseDown(e2)
	ta.handleMouseUp()
	ta.handleMouseDown(e3)

	start, end := ta.selStart, ta.selEnd
	if start > end {
		start, end = end, start
	}
	if start != 0 || end != 11 {
		t.Fatalf("triple click selection = [%d,%d), want [0,11)", start, end)
	}
}

func TestTextAreaDragOutsideAutoScrolls(t *testing.T) {
	ta := focusedTextArea("1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n16")
	ta.Layout(Rect{X: 0, Y: 0, W: 240, H: 120})
	start := MouseEvent{When: time.Now(), X: 12, Y: 12, Button: MouseButtonLeft}
	ta.handleMouseDown(start)

	ta.handleMouseDrag(MouseEvent{When: time.Now(), X: 12, Y: 240, Button: MouseButtonLeft})
	if ta.scrollY <= 0 {
		t.Fatalf("drag outside should auto-scroll, scrollY=%v", ta.scrollY)
	}
}

func TestTextAreaBeforeInputAndCallbacks(t *testing.T) {
	ta := focusedTextArea("abc")
	cursor := -1
	selectionCalls := 0
	ta.BeforeInput = func(ev BeforeInputEvent) bool { return ev.Kind != "type" }
	ta.OnCursorChange = func(pos int) { cursor = pos }
	ta.OnSelectionChange = func(start, end int) { selectionCalls++; _, _ = start, end }

	ta.Handle(newCharEvent('X'))
	if ta.Text != "abc" {
		t.Fatalf("BeforeInput should block typing, got %q", ta.Text)
	}
	ta.Handle(newKeyDown(KeyLeft))
	if cursor != 2 {
		t.Fatalf("OnCursorChange got %d, want 2", cursor)
	}
	ta.Handle(NewKeyEvent(EventKeyDown, KeyHome, ModShift))
	if selectionCalls == 0 {
		t.Fatalf("OnSelectionChange should fire for shift selection")
	}
}
