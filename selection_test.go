package qui

import (
	"testing"
	"time"
)

// stubSel is a minimal TextSelectable used to exercise the window-level
// selection controller without pulling in the widgets package.
type stubSel struct {
	BaseWidget
	text     string
	selStart int
	selEnd   int
}

func newStubSel(text string) *stubSel {
	s := &stubSel{text: text, selStart: -1}
	s.SetSelf(s)
	return s
}

func (s *stubSel) runes() int { return len([]rune(s.text)) }

// SelectionOffsetAt: left half → 0, right half → end. Enough to drive
// full-line selections in the controller tests.
func (s *stubSel) SelectionOffsetAt(p Point) int {
	b := s.Bounds()
	if p.X >= b.X+b.W*0.5 {
		return s.runes()
	}
	return 0
}

func (s *stubSel) SetSelectionRange(start, end int) {
	if start < 0 {
		s.ClearTextSelection()
		return
	}
	s.selStart, s.selEnd = start, end
}

func (s *stubSel) ClearTextSelection() { s.selStart, s.selEnd = -1, 0 }

func (s *stubSel) SelectedText() string {
	if s.selStart < 0 || s.selStart == s.selEnd {
		return ""
	}
	a, b := s.selStart, s.selEnd
	if a > b {
		a, b = b, a
	}
	r := []rune(s.text)
	return string(r[a:b])
}

func (s *stubSel) SelectableLength() int { return s.runes() }

func (s *stubSel) HitTest(p Point) Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

// stubScroll is a vertical AutoScrollable holding stacked stubSel rows.
type stubScroll struct {
	BaseWidget
	rows      []*stubSel
	rowH      float32
	offset    float32
	maxScroll float32
}

func newStubScroll(viewport Rect, rowH float32, rows ...*stubSel) *stubScroll {
	s := &stubScroll{rows: rows, rowH: rowH}
	s.SetSelf(s)
	s.Layout(viewport)
	for _, r := range rows {
		r.SetParent(s)
	}
	content := rowH * float32(len(rows))
	if m := content - viewport.H; m > 0 {
		s.maxScroll = m
	}
	s.reposition()
	return s
}

func (s *stubScroll) reposition() {
	vp := s.Bounds()
	for i, r := range s.rows {
		r.Layout(Rect{X: vp.X, Y: vp.Y + float32(i)*s.rowH - s.offset, W: vp.W, H: s.rowH})
	}
}

func (s *stubScroll) ChildList() []Widget {
	out := make([]Widget, len(s.rows))
	for i, r := range s.rows {
		out[i] = r
	}
	return out
}

func (s *stubScroll) ScrollOffset() float32 { return s.offset }
func (s *stubScroll) MaxScroll() float32    { return s.maxScroll }
func (s *stubScroll) ScrollTo(off float32) {
	if off < 0 {
		off = 0
	}
	if off > s.maxScroll {
		off = s.maxScroll
	}
	s.offset = off
	s.reposition()
}

func (s *stubScroll) HitTest(p Point) Widget {
	for _, r := range s.rows {
		if h := r.HitTest(p); h != nil {
			return h
		}
	}
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

func TestSelectionAutoScrollRevealsHiddenRows(t *testing.T) {
	fc := &fakeClip{}
	SetClipboardProvider(fc)
	t.Cleanup(func() { SetClipboardProvider(nil) })

	w := NewTestWindow(Size{W: 200, H: 100})
	// Five 40px rows in a 100px viewport → content 200, maxScroll 100.
	// Only rows 0..2 are partly visible at offset 0.
	rows := []*stubSel{
		newStubSel("row zero"), newStubSel("row one"), newStubSel("row two"),
		newStubSel("row three"), newStubSel("row four"),
	}
	sv := newStubScroll(Rect{X: 0, Y: 0, W: 200, H: 100}, 40, rows...)
	w.SetRoot(sv)

	// Press at the top of row 0, then drag to the bottom edge of the
	// viewport (within the auto-scroll band) on the right half so the
	// focus offset lands at end-of-row.
	w.beginTextSelectionDrag(rows[0], NewMouseEvent(EventMouseDown, 4, 4, MouseButtonLeft, 0))
	w.updateTextSelectionDrag(NewMouseEvent(EventMouseMove, 196, 98, MouseButtonLeft, 0))

	if rows[4].SelectedText() != "" {
		t.Fatalf("row four should still be hidden/unselected before auto-scroll")
	}

	// Pump frames: the registered auto-scroller scrolls toward the bottom
	// and re-extends the selection into newly revealed rows.
	now := time.UnixMilli(0)
	for i := 0; i < 200 && sv.ScrollOffset() < sv.MaxScroll(); i++ {
		now = now.Add(16 * time.Millisecond)
		w.tickAnimators(now)
	}

	if sv.ScrollOffset() != sv.MaxScroll() {
		t.Fatalf("auto-scroll did not reach bottom: off=%v max=%v", sv.ScrollOffset(), sv.MaxScroll())
	}
	if rows[4].SelectedText() != "row four" {
		t.Fatalf("last row not selected after auto-scroll: %q", rows[4].SelectedText())
	}

	// End the drag and confirm the auto-scroller self-prunes.
	w.endTextSelectionDrag()
	w.tickAnimators(now.Add(16 * time.Millisecond))
	if len(w.animators) != 0 {
		t.Fatalf("auto-scroller should be pruned after drag end, have %d", len(w.animators))
	}

	// Aggregated copy spans every row in document order.
	if !w.copyTextSelection() {
		t.Fatalf("copyTextSelection should handle a multi-row selection")
	}
	want := "row zero\nrow one\nrow two\nrow three\nrow four"
	if fc.buf != want {
		t.Fatalf("clipboard = %q, want %q", fc.buf, want)
	}
}

type fakeClip struct{ buf string }

func (f *fakeClip) Get() string     { return f.buf }
func (f *fakeClip) Set(text string) { f.buf = text }
