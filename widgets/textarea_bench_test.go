package widgets

import (
	"strings"
	"testing"

	. "github.com/qizhanchan/qui"
)

// A 2000-line document in a scrolling TextArea: the paint, drag-select and
// typing paths should cost what the visible lines cost, not the document.
func newBenchTextArea(wrap bool) *TextArea {
	ta := NewTextArea("")
	ta.AutoWrap = wrap
	ta.SetText(strings.Repeat("The quick brown fox jumps over the lazy dog, again and again.\n", 2000))
	ta.Layout(Rect{W: 400, H: 300})
	ta.SetFocused(true)
	ta.SetCursorPos(1000)
	return ta
}

func benchTextArea(b *testing.B, wrap bool, run func(ta *TextArea, i int)) {
	ta := newBenchTextArea(wrap)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		run(ta, i)
	}
}

func BenchmarkTextAreaDraw(b *testing.B) {
	for _, wrap := range []bool{false, true} {
		b.Run(map[bool]string{false: "nowrap", true: "wrap"}[wrap], func(b *testing.B) {
			benchTextArea(b, wrap, func(ta *TextArea, _ int) { ta.Draw(&RecordingCanvas{}) })
		})
	}
}

func BenchmarkTextAreaDragSelect(b *testing.B) {
	for _, wrap := range []bool{false, true} {
		b.Run(map[bool]string{false: "nowrap", true: "wrap"}[wrap], func(b *testing.B) {
			benchTextArea(b, wrap, func(ta *TextArea, i int) {
				ta.ed.selecting = true
				ta.Handle(NewMouseEvent(EventMouseMove, float32(20+i%200), 150, MouseButtonLeft, 0))
			})
		})
	}
}

func BenchmarkTextAreaType(b *testing.B) {
	benchTextArea(b, true, func(ta *TextArea, _ int) {
		ta.Handle(CharEvent{Rune: 'x'})
	})
}

// Paint and drag-select of a long document must not do per-document work:
// before the layout caches, one Draw allocated ~24k times (a rune copy of the
// whole text plus a re-measure of every line).
func TestTextAreaPaintCostIsNotPerDocument(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		ta := newBenchTextArea(wrap)
		ta.Draw(&RecordingCanvas{}) // warm
		if n := testing.AllocsPerRun(5, func() { ta.Draw(&RecordingCanvas{}) }); n > 1000 {
			t.Errorf("wrap=%v: Draw allocated %.0f times per call", wrap, n)
		}
		ta.ed.selecting = true
		if n := testing.AllocsPerRun(5, func() {
			ta.Handle(NewMouseEvent(EventMouseMove, 60, 150, MouseButtonLeft, 0))
		}); n > 1000 {
			t.Errorf("wrap=%v: a drag-select move allocated %.0f times", wrap, n)
		}
	}
}
