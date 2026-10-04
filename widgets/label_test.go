package widgets

import (
	"testing"
	"time"

	. "github.com/qizhanchan/qui"
)

// Multi-click granularity (double = word, triple = line) is owned by the
// window's selection controller, so these tests dispatch through a test
// window instead of calling Label.Handle directly.

func dispatchClicks(w *Window, x, y float32, n int, base time.Time) {
	for i := 0; i < n; i++ {
		d := NewMouseEvent(EventMouseDown, x, y, MouseButtonLeft, 0)
		d.When = base.Add(time.Duration(i*90) * time.Millisecond)
		w.DispatchTestEvent(d)
		u := NewMouseEvent(EventMouseUp, x, y, MouseButtonLeft, 0)
		u.When = d.When.Add(10 * time.Millisecond)
		w.DispatchTestEvent(u)
	}
}

func TestLabelDoubleClickSelectsWordAndCopy(t *testing.T) {
	fc := withFakeClipboard(t)
	w := NewTestWindow(Size{W: 320, H: 80})
	l := NewLabel("hello world")
	w.SetRoot(l)
	l.Layout(Rect{X: 0, Y: 0, W: 240, H: 60})

	x := float32(8) + textWidthForRunes(l.Style().Font, []rune("hello w"))
	y := float32(12)
	dispatchClicks(w, x, y, 2, time.UnixMilli(1000))

	a, b := l.orderedSelection()
	got := string([]rune(l.contentText())[a:b])
	if got != "world" {
		t.Fatalf("double-click selected %q, want world", got)
	}

	w.DispatchTestEvent(newCmdKeyDown(KeyC))
	if fc.buf != "world" {
		t.Fatalf("clipboard = %q, want world", fc.buf)
	}
}

// Selecting a justified paragraph must not highlight past the column edge:
// each line's [start,end) range includes the trimmed wrap space, whose
// advance plus its justify share would otherwise overshoot by a per-line-
// varying amount.
func TestLabelJustifySelectionStaysInColumn(t *testing.T) {
	l := NewLabel("alpha beta gamma delta epsilon zeta eta theta iota kappa")
	l.Paragraph.Wrap = true
	l.Paragraph.Align = TextAlignJustify
	l.Selectable = true
	l.Style().Padding = Insets{}
	const colW = 160
	l.Layout(Rect{X: 0, Y: 0, W: colW, H: 200})
	l.SetSelectionRange(0, len([]rune(l.Text())))

	rc := &RecordingCanvas{}
	l.Draw(rc)
	if len(rc.Fills) < 2 {
		t.Fatalf("expected selection highlight fills per line, got %d", len(rc.Fills))
	}
	for _, r := range rc.Fills {
		if right := r.X + r.W; right > colW+0.5 {
			t.Errorf("selection fill reaches %v, must stay within column width %v", right, colW)
		}
	}
	// Sanity: justified lines fill the column, so at least one highlight
	// should reach (nearly) the full width.
	var maxRight float32
	for _, r := range rc.Fills {
		if right := r.X + r.W; right > maxRight {
			maxRight = right
		}
	}
	if maxRight < colW-1 {
		t.Errorf("widest selection fill right edge = %v, want ≈ %v (justified line fills column)", maxRight, colW)
	}
}

func TestLabelTripleClickSelectsLogicalLine(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 100})
	l := NewLabel("first line\nsecond line")
	w.SetRoot(l)
	l.Layout(Rect{X: 0, Y: 0, W: 320, H: 80})

	dispatchClicks(w, 12, 30, 3, time.UnixMilli(2000))

	a, b := l.orderedSelection()
	got := string([]rune(l.contentText())[a:b])
	if got != "second line" {
		t.Fatalf("triple-click selected %q, want second line", got)
	}
}

// A word selection anchored by double-click extends word-by-word when the
// mouse drags on (browser behavior) — previously impossible because the
// widget owned multi-click and the window only extended by character.
func TestLabelDoubleClickDragExtendsByWord(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 100})
	l := NewLabel("alpha beta gamma")
	w.SetRoot(l)
	l.Layout(Rect{X: 0, Y: 0, W: 320, H: 60})

	font := l.Style().Font
	xIn := func(prefix string) float32 {
		return float32(8) + textWidthForRunes(font, []rune(prefix))
	}
	base := time.UnixMilli(3000)
	// Double-click inside "beta".
	x := xIn("alpha be")
	d1 := NewMouseEvent(EventMouseDown, x, 12, MouseButtonLeft, 0)
	d1.When = base
	w.DispatchTestEvent(d1)
	u1 := NewMouseEvent(EventMouseUp, x, 12, MouseButtonLeft, 0)
	u1.When = base.Add(10 * time.Millisecond)
	w.DispatchTestEvent(u1)
	d2 := NewMouseEvent(EventMouseDown, x, 12, MouseButtonLeft, 0)
	d2.When = base.Add(90 * time.Millisecond)
	w.DispatchTestEvent(d2)
	// Drag (button still down) into "gamma".
	m := NewMouseEvent(EventMouseMove, xIn("alpha beta gam"), 12, MouseButtonLeft, 0)
	m.When = base.Add(150 * time.Millisecond)
	w.DispatchTestEvent(m)
	u2 := NewMouseEvent(EventMouseUp, xIn("alpha beta gam"), 12, MouseButtonLeft, 0)
	u2.When = base.Add(160 * time.Millisecond)
	w.DispatchTestEvent(u2)

	if got := l.SelectedText(); got != "beta gamma" {
		t.Fatalf("word-drag selected %q, want %q", got, "beta gamma")
	}
}

// Shift+click extends the prior selection from its original anchor.
func TestLabelShiftClickExtendsSelection(t *testing.T) {
	w := NewTestWindow(Size{W: 400, H: 100})
	l := NewLabel("alpha beta gamma")
	w.SetRoot(l)
	l.Layout(Rect{X: 0, Y: 0, W: 320, H: 60})

	font := l.Style().Font
	xIn := func(prefix string) float32 {
		return float32(8) + textWidthForRunes(font, []rune(prefix))
	}
	base := time.UnixMilli(4000)
	// Drag-select "alpha".
	d := NewMouseEvent(EventMouseDown, xIn(""), 12, MouseButtonLeft, 0)
	d.When = base
	w.DispatchTestEvent(d)
	m := NewMouseEvent(EventMouseMove, xIn("alpha")-1, 12, MouseButtonLeft, 0)
	m.When = base.Add(30 * time.Millisecond)
	w.DispatchTestEvent(m)
	u := NewMouseEvent(EventMouseUp, xIn("alpha")-1, 12, MouseButtonLeft, 0)
	u.When = base.Add(40 * time.Millisecond)
	w.DispatchTestEvent(u)
	if got := l.SelectedText(); got != "alpha" {
		t.Fatalf("initial drag selected %q, want alpha", got)
	}

	// Shift+click far to the right extends to "alpha beta gamma".
	sd := NewMouseEvent(EventMouseDown, xIn("alpha beta gamma"), 12, MouseButtonLeft, ModShift)
	sd.When = base.Add(2 * time.Second) // well past double-click window
	w.DispatchTestEvent(sd)
	su := NewMouseEvent(EventMouseUp, xIn("alpha beta gamma"), 12, MouseButtonLeft, ModShift)
	su.When = sd.When.Add(10 * time.Millisecond)
	w.DispatchTestEvent(su)

	if got := l.SelectedText(); got != "alpha beta gamma" {
		t.Fatalf("shift-click selected %q, want %q", got, "alpha beta gamma")
	}
}

func textWidthForRunes(font Font, runes []rune) float32 {
	w := float32(0)
	for _, r := range runes {
		w += RuneAdvance(r, font)
	}
	return w
}

func TestLabelMeasureWithSpansAndParagraph(t *testing.T) {
	l := NewLabel("")
	blue := Color{B: 1, A: 1}
	large := Font{Size: 20}
	l.SetSpans([]TextSpan{
		{Text: "Rich "},
		{Text: "Label", Color: &blue, Font: &large},
	})
	l.Paragraph = ParagraphStyle{
		Wrap:            true,
		LineHeightScale: 1.2,
	}
	size := l.Measure(Size{W: 80, H: 200})
	if size.W <= 0 || size.H <= 0 {
		t.Fatalf("unexpected measured size: %+v", size)
	}
}

func TestLabelSetTextInvalidatesMountedLayout(t *testing.T) {
	w := NewTestWindow(Size{W: 240, H: 80})
	l := NewLabel("old")
	l.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	w.SetRoot(l)
	l.ClearLayoutDirty()
	w.ClearDirtyRegion()

	l.SetText("new selected row")

	if !l.IsLayoutDirty() {
		t.Fatal("SetText should mark mounted label layout dirty")
	}
	if w.DirtyRegion().IsEmpty() {
		t.Fatal("SetText should invalidate paint on mounted label")
	}
}

// Selection highlighting and hit-testing must step by EXACTLY the pitch the
// painter uses — the layout's LineHeight — not a local re-derivation from
// face metrics, which drifted 11 px per line when the engine's
// LineHeightScale switched to CSS semantics (font size, not natural height).
func TestLabelSelectionPitchMatchesPaintedPitch(t *testing.T) {
	for _, scale := range []float32{0, 1, 1.4, 2} {
		l := NewLabel("one two three\nfour five six\nseven eight nine")
		l.Style().Font = Font{Size: 16}
		l.Paragraph.LineHeightScale = scale
		painted := BuildTextLayout(l.Text(), l.Style().Font, l.Paragraph.LayoutOptions(0)).LineHeight
		if got := l.lineHeight(); got != painted {
			t.Errorf("scale %v: selection/hit-test pitch = %v, painted pitch = %v",
				scale, got, painted)
		}
	}
}
