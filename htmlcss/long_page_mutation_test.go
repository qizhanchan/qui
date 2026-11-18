package htmlcss

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

// A script changing one element's text on a long page must restyle that
// element alone and re-measure only its ancestor chain: the rest of the
// document answers from its caches.

const longPageCSS = `body { margin: 8px; font: 16px sans-serif }
main { display: flex; flex-direction: column; gap: 4px }
section p { line-height: 1.5 } .note { color: #555 } a:hover { color: red }
section > h2 + p { margin-top: 0 }`

func longPage(sections int) string {
	var sb strings.Builder
	sb.WriteString("<!doctype html><html><body><main>")
	for i := 0; i < sections; i++ {
		fmt.Fprintf(&sb, `<section id="s%d"><h2>Section %d</h2><p id="p%d" class="note">Paragraph %d with <a href="#x">a link</a>, <b>bold</b> and <code>code</code> words that wrap across several lines at the page width, which is what most of a long article is made of.</p><ul><li>one</li><li>two</li></ul></section>`, i, i, i, i)
	}
	sb.WriteString("</main></body></html>")
	return sb.String()
}

type longPageFixture struct {
	win    *qui.Window
	rd     RenderResult
	target *El
	w      float32
}

func newLongPage(t testing.TB, sections int) *longPageFixture {
	const w = 1000
	win := qui.NewTestWindow(qui.Size{W: w, H: 800})
	rd := RenderDoc(longPage(sections), longPageCSS, Options{Window: win, ViewportWidth: w, DocumentRoot: true, ScriptDOM: true})
	win.SetRoot(rd.Root)
	f := &longPageFixture{win: win, rd: rd, w: w}
	f.target, _ = rd.ByID[fmt.Sprintf("p%d", sections/2)].(*El)
	if f.target == nil {
		t.Fatal("no target paragraph")
	}
	f.layout()
	return f
}

func (f *longPageFixture) layout() qui.Size {
	sz := f.rd.Root.Measure(qui.Size{W: f.w})
	f.rd.Root.Layout(qui.Rect{W: f.w, H: sz.H})
	return sz
}

// setText replaces the target's content the way textContent does.
func (f *longPageFixture) setText(s string) {
	f.target.SetElementChildren(nil)
	f.target.SetTextContent(s)
	f.rd.Engine.FlushPendingStyles()
}

func TestLongPageTextMutationIsScoped(t *testing.T) {
	f := newLongPage(t, 400)
	eng := f.rd.Engine
	before := eng.styled
	f.setText("A much shorter paragraph.")
	// The `h2 + p` rule makes the paragraph's siblings depend on it: its
	// section (a handful of elements) is restyled, nothing else.
	if n := eng.styled - before; n > 10 {
		t.Fatalf("changing one paragraph's text restyled %d elements, want its section alone", n)
	}
	h0 := f.target.Bounds().H
	f.layout()
	if h1 := f.target.Bounds().H; h1 >= h0 {
		t.Fatalf("paragraph height %v -> %v: relayout did not pick up the shorter text", h0, h1)
	}
	// The scoped relayout lands where a fresh layout of the same document
	// does.
	next, _ := f.rd.ByID[fmt.Sprintf("s%d", 400/2+1)].(*El)
	got := qui.LocalPointToWindow(next, qui.Point{X: next.Bounds().X, Y: next.Bounds().Y})
	g := newLongPage(t, 400)
	g.setText("A much shorter paragraph.")
	g.win.InvalidateLayout()
	g.layout()
	ref, _ := g.rd.ByID[fmt.Sprintf("s%d", 400/2+1)].(*El)
	want := qui.LocalPointToWindow(ref, qui.Point{X: ref.Bounds().X, Y: ref.Bounds().Y})
	if got != want {
		t.Fatalf("next section at %v after the scoped relayout, %v after a fresh one", got, want)
	}
}

// The relayout after a one-paragraph change must cost a small fraction of
// laying the page out from scratch.
func TestLongPageTextMutationRelayoutIsCheap(t *testing.T) {
	if testing.Short() {
		t.Skip("timing")
	}
	f := newLongPage(t, 1500)
	full := time.Duration(1<<63 - 1)
	for i := 0; i < 3; i++ {
		f.win.InvalidateLayout()
		t0 := time.Now()
		f.layout()
		full = min(full, time.Since(t0))
	}
	inc := time.Duration(1<<63 - 1)
	for i := 0; i < 5; i++ {
		t0 := time.Now()
		f.setText(fmt.Sprintf("Changed text %d, long enough to wrap onto a second line maybe, maybe not, depending on the width.", i))
		f.layout()
		inc = min(inc, time.Since(t0))
	}
	t.Logf("full layout %v, text change + relayout %v", full, inc)
	if inc*5 > full {
		t.Fatalf("a one-paragraph change took %v, the full layout %v: the relayout is not scoped", inc, full)
	}
}

func BenchmarkLongPageTextMutation(b *testing.B) {
	f := newLongPage(b, 1500)
	eng := f.rd.Engine
	b.ReportAllocs()
	b.ResetTimer()
	before := eng.styled
	for i := 0; i < b.N; i++ {
		f.setText(fmt.Sprintf("Changed text %d", i))
		f.layout()
	}
	reportRestyled(b, eng, before)
}

// The baseline: laying the same page out from scratch.
func BenchmarkLongPageFullLayout(b *testing.B) {
	f := newLongPage(b, 1500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.win.InvalidateLayout()
		f.layout()
	}
}
