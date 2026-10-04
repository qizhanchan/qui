package qui

import (
	"strings"
	"testing"
)

// --- F0.2: PreserveWhitespace tokenization / wrapping ---

// flatFragText concatenates every text fragment in layout order.
func flatFragText(l InlineLayout) string {
	var sb strings.Builder
	for _, ln := range l.Lines {
		for _, f := range ln.Frags {
			if !f.Box {
				sb.WriteString(f.Text)
			}
		}
	}
	return sb.String()
}

func preserveOpts(maxW float32) InlineLayoutOptions {
	return InlineLayoutOptions{MaxWidth: maxW, Wrap: maxW > 0, BaseFont: Font{Size: 16}, PreserveWhitespace: true}
}

// Preserve mode keeps every space rune verbatim — the laid-out text is the
// exact source text, unlike collapse mode.
func TestPreserveWhitespaceKeepsRuns(t *testing.T) {
	f := Font{Size: 16}
	src := "a  b   c "
	l := BuildInlineLayout([]InlineItem{inlineText(src, f)}, preserveOpts(10000))
	if got := flatFragText(l); got != src {
		t.Errorf("preserve mode text = %q, want %q", got, src)
	}
	// Collapse mode, for contrast, folds the runs.
	c := BuildInlineLayout([]InlineItem{inlineText(src, f)},
		InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	if got := flatFragText(c); strings.Contains(got, "  ") {
		t.Errorf("collapse mode kept a space run: %q", got)
	}
}

// Every fragment in preserve mode is an exact source slice.
func TestPreserveFragmentsAreExactSourceSlices(t *testing.T) {
	f := Font{Size: 16}
	b := Font{Size: 16, Weight: FontWeightBold}
	items := []InlineItem{
		{Text: "hello  wide ", Font: f, Color: ColorBlack},
		{Text: "bold  world", Font: b, Color: ColorBlack},
	}
	l := BuildInlineLayout(items, preserveOpts(72))
	for li, ln := range l.Lines {
		for _, fr := range ln.Frags {
			if fr.Box {
				continue
			}
			if fr.SrcItem < 0 {
				t.Fatalf("line %d: preserve-mode fragment %q lost its source", li, fr.Text)
			}
			src := []rune(items[fr.SrcItem].Text)
			want := string(src[fr.SrcStart:fr.SrcEnd])
			if fr.Text != want {
				t.Errorf("line %d: frag text %q != source slice %q", li, fr.Text, want)
			}
		}
	}
}

// A run of spaces at a soft wrap hangs at the end of the line (Word
// semantics) instead of being dropped or wrapped by itself: the next line
// starts with the following word, and no source rune is lost.
func TestPreserveWrapHangsSpaces(t *testing.T) {
	f := Font{Size: 16}
	src := "alpha    beta"
	wordW := measureRunes([]rune("alpha"), f, nil)
	l := BuildInlineLayout([]InlineItem{inlineText(src, f)}, preserveOpts(wordW+2))
	if len(l.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(l.Lines))
	}
	if got := flatFragText(l); got != src {
		t.Errorf("wrap lost runes: %q, want %q", got, src)
	}
	// Line 0 carries "alpha    " (spaces hanging past MaxWidth), line 1 "beta".
	if l.Lines[0].SrcEnd.Off != 9 { // after the 4 spaces
		t.Errorf("line 0 SrcEnd = %+v, want Off 9", l.Lines[0].SrcEnd)
	}
	if l.Lines[1].SrcStart.Off != 9 {
		t.Errorf("line 1 SrcStart = %+v, want Off 9", l.Lines[1].SrcStart)
	}
	if l.Lines[0].Width <= wordW {
		t.Errorf("hanging spaces should extend line 0 width past the word (%v <= %v)", l.Lines[0].Width, wordW)
	}
}

// A trailing '\n' in preserve mode produces a final empty line — the caret
// needs a line to sit on. Collapse mode keeps its historical behavior.
func TestPreserveTrailingNewlineEmptyLine(t *testing.T) {
	f := Font{Size: 16}
	p := BuildInlineLayout([]InlineItem{inlineText("a\n", f)}, preserveOpts(10000))
	if len(p.Lines) != 2 {
		t.Fatalf("preserve: expected 2 lines (text + empty), got %d", len(p.Lines))
	}
	if got := p.Lines[1].SrcStart; got != (SourcePos{Item: 0, Off: 2}) || p.Lines[1].SrcEnd != got {
		t.Errorf("empty line src = [%+v,%+v], want both {0 2}", p.Lines[1].SrcStart, p.Lines[1].SrcEnd)
	}
	c := BuildInlineLayout([]InlineItem{inlineText("a\n", f)},
		InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	if len(c.Lines) != 1 {
		t.Fatalf("collapse: trailing-newline behavior changed, got %d lines", len(c.Lines))
	}
}

// --- F0.1/F0.3: caret rects and hit-testing in source space ---

// Caret x positions are strictly monotonic across every source offset of a
// single-line layout, and each position round-trips through SourcePosAt.
func TestCaretRoundTripSingleLine(t *testing.T) {
	f := Font{Size: 16}
	src := "ab  cd"
	l := BuildInlineLayout([]InlineItem{inlineText(src, f)}, preserveOpts(10000))
	if len(l.Lines) != 1 {
		t.Fatalf("expected single line, got %d", len(l.Lines))
	}
	prevX := float32(-1)
	for off := 0; off <= len([]rune(src)); off++ {
		pos := SourcePos{Item: 0, Off: off}
		r := l.CaretRectForSource(pos)
		if r.X <= prevX {
			t.Errorf("caret x not monotonic at off %d: %v <= %v", off, r.X, prevX)
		}
		prevX = r.X
		got, _ := l.SourcePosAt(Point{X: r.X + 0.5, Y: r.Y + r.H/2})
		if got != pos {
			t.Errorf("round trip at off %d: got %+v", off, got)
		}
	}
}

// A position right after a '\n' lands at the start of the next line; the
// position right before it is the end of the previous line.
func TestCaretAcrossHardBreak(t *testing.T) {
	f := Font{Size: 16}
	l := BuildInlineLayout([]InlineItem{inlineText("ab\ncd", f)}, preserveOpts(10000))
	if len(l.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(l.Lines))
	}
	before := l.CaretRectForSource(SourcePos{Item: 0, Off: 2})
	after := l.CaretRectForSource(SourcePos{Item: 0, Off: 3})
	if !caretOnLine(before, l.Lines[0]) {
		t.Errorf("caret before \\n on wrong line: y=%v", before.Y)
	}
	if !caretOnLine(after, l.Lines[1]) || after.X != l.Lines[1].Frags[0].Rect.X {
		t.Errorf("caret after \\n should sit at start of line 1, got (%v,%v)", after.X, after.Y)
	}
}

// caretOnLine reports whether the caret rect sits inside line's box and
// spans exactly the line's text ink (baseline-anchored, leading excluded).
func caretOnLine(r Rect, line InlineLine) bool {
	return r.Y >= line.Y && r.Y+r.H <= line.Y+line.Height+0.5 &&
		r.Y == line.Baseline-line.InkAscent &&
		r.H == line.InkAscent+line.InkDescent
}

// At a soft-wrap boundary the shared position defaults to the end of the
// earlier line; CaretRectInLine forces the other affinity.
func TestCaretWrapAffinity(t *testing.T) {
	f := Font{Size: 16}
	wordW := measureRunes([]rune("alpha"), f, nil)
	l := BuildInlineLayout([]InlineItem{inlineText("alpha beta", f)}, preserveOpts(wordW+2))
	if len(l.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(l.Lines))
	}
	pos := l.Lines[0].SrcEnd // == Lines[1].SrcStart
	if pos != l.Lines[1].SrcStart {
		t.Fatalf("wrap boundary not shared: %+v vs %+v", pos, l.Lines[1].SrcStart)
	}
	if r := l.CaretRectForSource(pos); !caretOnLine(r, l.Lines[0]) {
		t.Errorf("default affinity should keep the caret on line 0, got y=%v", r.Y)
	}
	if r := l.CaretRectInLine(1, pos); !caretOnLine(r, l.Lines[1]) {
		t.Errorf("CaretRectInLine(1) should pin to line 1, got y=%v", r.Y)
	}
}

// SourcePosAtLineX drives up/down navigation: querying the next line at the
// caret's current x returns a sensible position on that line.
func TestSourcePosAtLineXNavigation(t *testing.T) {
	f := Font{Size: 16}
	l := BuildInlineLayout([]InlineItem{inlineText("aaaa bbbb\ncc dddd", f)}, preserveOpts(10000))
	if len(l.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(l.Lines))
	}
	// Caret after "aaaa" (off 4) → down → should land inside/near "cc dddd".
	r := l.CaretRectForSource(SourcePos{Item: 0, Off: 4})
	down := l.SourcePosAtLineX(1, r.X)
	if down.Item != 0 || down.Off < 10 || down.Off > 17 {
		t.Errorf("down-navigation landed at %+v", down)
	}
	// x far past the end of line 1 clamps to the line end.
	end := l.SourcePosAtLineX(1, 1e6)
	if end != l.Lines[1].SrcEnd {
		t.Errorf("past-end click = %+v, want %+v", end, l.Lines[1].SrcEnd)
	}
	// x before the start of a line clamps to the line start.
	start := l.SourcePosAtLineX(1, -5)
	if start != l.Lines[1].SrcStart {
		t.Errorf("before-start click = %+v, want %+v", start, l.Lines[1].SrcStart)
	}
}

// Atomic boxes occupy the synthetic source range [0,1]: caret before/after,
// and clicks resolve by the box midpoint.
func TestBoxSourcePositions(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{
		inlineText("ab ", f),
		{Box: true, Size: Size{W: 40, H: 20}, BoxIndex: 7},
		inlineText(" cd", f),
	}
	l := BuildInlineLayout(items, preserveOpts(10000))
	var box *InlineFrag
	for i := range l.Lines[0].Frags {
		if l.Lines[0].Frags[i].Box {
			box = &l.Lines[0].Frags[i]
		}
	}
	if box == nil {
		t.Fatal("no box fragment")
	}
	if box.SrcItem != 1 || box.SrcStart != 0 || box.SrcEnd != 1 {
		t.Fatalf("box src = %d [%d,%d], want 1 [0,1]", box.SrcItem, box.SrcStart, box.SrcEnd)
	}
	if r := l.CaretRectForSource(SourcePos{Item: 1, Off: 0}); r.X != box.Rect.X {
		t.Errorf("caret before box at %v, want %v", r.X, box.Rect.X)
	}
	if r := l.CaretRectForSource(SourcePos{Item: 1, Off: 1}); r.X != box.Rect.X+box.Rect.W {
		t.Errorf("caret after box at %v, want %v", r.X, box.Rect.X+box.Rect.W)
	}
	if got, _ := l.SourcePosAt(Point{X: box.Rect.X + 1, Y: l.Lines[0].Y + 1}); got != (SourcePos{Item: 1, Off: 0}) {
		t.Errorf("click left of box midpoint = %+v", got)
	}
	if got, _ := l.SourcePosAt(Point{X: box.Rect.X + box.Rect.W - 1, Y: l.Lines[0].Y + 1}); got != (SourcePos{Item: 1, Off: 1}) {
		t.Errorf("click right of box midpoint = %+v", got)
	}
}

// An empty layout still answers caret queries: one empty line, caret at the
// origin.
func TestCaretOnEmptyLayout(t *testing.T) {
	l := BuildInlineLayout(nil, preserveOpts(100))
	if len(l.Lines) != 1 {
		t.Fatalf("expected 1 empty line, got %d", len(l.Lines))
	}
	r := l.CaretRectForSource(SourcePos{})
	if r.X != 0 || r.H <= 0 || !caretOnLine(r, l.Lines[0]) {
		t.Errorf("empty-layout caret = %+v", r)
	}
	// The caret spans the ink box, not the leaded line box.
	if r.H >= l.Lines[0].Height && l.Lines[0].Height > l.Lines[0].InkAscent+l.Lines[0].InkDescent {
		t.Errorf("caret height %v should be smaller than leaded line height %v", r.H, l.Lines[0].Height)
	}
}

// Regression: with a generous line-height (1.4, the document default) the
// caret must span the glyph ink box anchored on the baseline — not the
// leaded line box, which reads far too tall next to the text.
func TestCaretHeightIsInkNotLineBox(t *testing.T) {
	f := Font{Size: 16}
	opts := preserveOpts(10000)
	opts.LineHeightScale = 1.4
	l := BuildInlineLayout([]InlineItem{inlineText("123", f)}, opts)
	line := l.Lines[0]
	if line.Height <= line.InkAscent+line.InkDescent {
		t.Skip("no leading at this font; nothing to distinguish")
	}
	r := l.CaretRectForSource(SourcePos{Item: 0, Off: 3})
	if r.H != line.InkAscent+line.InkDescent {
		t.Errorf("caret height %v, want ink height %v", r.H, line.InkAscent+line.InkDescent)
	}
	if r.H >= line.Height {
		t.Errorf("caret height %v should be < leaded line height %v", r.H, line.Height)
	}
	if r.Y != line.Baseline-line.InkAscent {
		t.Errorf("caret should anchor on the baseline: y=%v baseline=%v inkAsc=%v", r.Y, line.Baseline, line.InkAscent)
	}
}

// Collapse mode: source tracking is best-effort but word fragments still
// carry exact ranges, and dropped whitespace snaps to the surviving
// boundaries instead of crashing or drifting.
func TestCollapseModeSourceBestEffort(t *testing.T) {
	f := Font{Size: 16}
	src := "foo   bar"
	l := BuildInlineLayout([]InlineItem{inlineText(src, f)},
		InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	// "foo" is addressable exactly.
	r3 := l.CaretRectForSource(SourcePos{Item: 0, Off: 3})
	r0 := l.CaretRectForSource(SourcePos{Item: 0, Off: 0})
	if !(r3.X > r0.X) {
		t.Errorf("collapse-mode caret not advancing: off3=%v off0=%v", r3.X, r0.X)
	}
	// A position inside the collapsed run (off 4/5) stays between the
	// surviving boundaries.
	r4 := l.CaretRectForSource(SourcePos{Item: 0, Off: 4})
	r9 := l.CaretRectForSource(SourcePos{Item: 0, Off: 9})
	if r4.X < r3.X || r4.X > r9.X {
		t.Errorf("collapsed-space caret out of range: %v not in [%v,%v]", r4.X, r3.X, r9.X)
	}
}

// FirstIndent shifts only the first visual line and reduces its wrap
// width; caret/hit-test APIs stay exact in the shifted space.
func TestFirstIndentShiftsFirstLine(t *testing.T) {
	f := Font{Size: 16}
	opts := preserveOpts(200)
	opts.FirstIndent = 40
	l := BuildInlineLayout([]InlineItem{inlineText("aaa bbb ccc ddd eee fff ggg hhh", f)}, opts)
	if len(l.Lines) < 2 {
		t.Fatal("expected wrapping")
	}
	if x := l.Lines[0].Frags[0].Rect.X; x != 40 {
		t.Errorf("first line X = %v, want 40", x)
	}
	if x := l.Lines[1].Frags[0].Rect.X; x != 0 {
		t.Errorf("second line X = %v, want 0", x)
	}
	// Caret at offset 0 sits at the indent.
	if r := l.CaretRectForSource(SourcePos{}); r.X != 40 {
		t.Errorf("caret at 0 = %v, want 40", r.X)
	}
	// Round trip through the shifted x.
	pos, _ := l.SourcePosAt(Point{X: 41, Y: 1})
	if pos != (SourcePos{Item: 0, Off: 0}) {
		t.Errorf("hit at indent start = %+v", pos)
	}
}

// The character-level break must keep preserve mode's exact-source-slice
// contract: an editor's caret still round-trips through a run that was cut
// mid-token, which is what makes the break usable in a text editor (q-word
// pastes long unbroken strings this way).
func TestPreserveBreakLongWordsCaretRoundTrip(t *testing.T) {
	f := Font{Size: 16}
	src := "12312123121231212312123121231212312"
	opts := preserveOpts(90)
	opts.BreakLongWords = true
	l := BuildInlineLayout([]InlineItem{inlineText(src, f)}, opts)
	if len(l.Lines) < 3 {
		t.Fatalf("expected the run to break across several lines, got %d", len(l.Lines))
	}
	if got := flatFragText(l); got != src {
		t.Errorf("text after breaking = %q, want %q", got, src)
	}
	for i, ln := range l.Lines {
		if ln.Width > 90.5 {
			t.Errorf("line %d width %v exceeds MaxWidth 90", i, ln.Width)
		}
	}
	// Every caret offset resolves to a rect inside some line and hit-tests
	// back to itself. Offsets at a wrap boundary are ambiguous by design
	// (end-of-line vs start-of-next), so those accept either neighbour.
	runes := []rune(src)
	boundary := map[int]bool{}
	for _, ln := range l.Lines {
		boundary[ln.SrcEnd.Off] = true
		boundary[ln.SrcStart.Off] = true
	}
	for off := 0; off <= len(runes); off++ {
		pos := SourcePos{Item: 0, Off: off}
		r := l.CaretRectForSource(pos)
		if r.H <= 0 {
			t.Errorf("off %d: empty caret rect %v", off, r)
			continue
		}
		got, _ := l.SourcePosAt(Point{X: r.X + 0.5, Y: r.Y + r.H/2})
		if got != pos && !boundary[off] {
			t.Errorf("caret round trip at off %d: got %+v", off, got)
		}
	}
}
