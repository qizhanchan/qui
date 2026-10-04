package qui

import (
	"strings"
	"testing"
	"time"
)

func inlineText(t string, f Font) InlineItem { return InlineItem{Text: t, Font: f, Color: ColorBlack} }

// countBoxFrags returns every box fragment in a layout.
func boxFrags(l InlineLayout) []InlineFrag {
	var out []InlineFrag
	for _, ln := range l.Lines {
		for _, f := range ln.Frags {
			if f.Box {
				out = append(out, f)
			}
		}
	}
	return out
}

// A run's DecorationPaint (color/style/thickness) survives tokenize → place
// and lands on its output fragment; a differing paint is not merged away.
func TestInlineCarriesDecorationPaint(t *testing.T) {
	f := Font{Size: 16}
	red := DecorationPaint{Color: Color{R: 1, A: 1}, HasColor: true, Style: DecorationDashed, Thickness: 2}
	items := []InlineItem{
		{Text: "plain ", Font: f, Color: ColorBlack},
		{Text: "fancy", Font: f, Color: ColorBlack, Decoration: DecorationUnderline, DecorationPaint: red},
	}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	var got *InlineFrag
	for li := range l.Lines {
		for fi := range l.Lines[li].Frags {
			if l.Lines[li].Frags[fi].Text == "fancy" {
				got = &l.Lines[li].Frags[fi]
			}
		}
	}
	if got == nil {
		t.Fatal("no 'fancy' fragment produced")
	}
	if got.Decoration&DecorationUnderline == 0 {
		t.Error("underline flag lost")
	}
	if got.DecorationPaint != red {
		t.Errorf("decoration paint = %+v, want %+v", got.DecorationPaint, red)
	}
}

func TestInlineWrapsAtWidth(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText("alpha beta gamma delta epsilon", f)}
	narrow := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 60, Wrap: true, BaseFont: f})
	if len(narrow.Lines) < 2 {
		t.Fatalf("expected wrapping into multiple lines, got %d", len(narrow.Lines))
	}
	wide := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	if len(wide.Lines) != 1 {
		t.Fatalf("wide layout should be a single line, got %d", len(wide.Lines))
	}
}

// text-align: justify stretches each soft-wrapped inline line to the layout
// width: gaps widen positionally AND the frag fonts carry the extra as
// WordSpacing so drawing/selection advance identically. The final line stays
// natural.
func TestInlineJustifyStretchesSoftLines(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText("alpha beta gamma delta epsilon zeta eta theta", f)}
	maxW := float32(140)
	plain := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: maxW, Wrap: true, BaseFont: f})
	if len(plain.Lines) < 2 {
		t.Fatalf("fixture should wrap into multiple lines, got %d", len(plain.Lines))
	}
	just := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: maxW, Wrap: true, Align: TextAlignJustify, BaseFont: f})
	if len(just.Lines) != len(plain.Lines) {
		t.Fatalf("justify must not change line breaking: %d vs %d lines", len(just.Lines), len(plain.Lines))
	}

	first := just.Lines[0]
	if first.Width <= plain.Lines[0].Width {
		t.Errorf("justified line width %v should exceed natural %v", first.Width, plain.Lines[0].Width)
	}
	if diff := first.Width - maxW; diff < -0.5 || diff > 0.5 {
		t.Errorf("justified line width %v, want ≈ MaxWidth %v", first.Width, maxW)
	}
	// The stretch rides on the frag font's WordSpacing.
	if len(first.Frags) == 0 || first.Frags[0].Font.WordSpacing <= 0 {
		t.Errorf("justified frag should carry WordSpacing > 0, got %+v", first.Frags[0].Font.WordSpacing)
	}
	// Fragments start at the line origin (no alignment shift).
	if first.Frags[0].Rect.X != 0 {
		t.Errorf("justified line should start at X=0, got %v", first.Frags[0].Rect.X)
	}

	last := just.Lines[len(just.Lines)-1]
	if last.Width != plain.Lines[len(plain.Lines)-1].Width {
		t.Errorf("last line must stay natural: %v vs %v", last.Width, plain.Lines[len(plain.Lines)-1].Width)
	}
	for _, fr := range last.Frags {
		if fr.Font.WordSpacing != 0 {
			t.Errorf("last line frag should have no justify WordSpacing, got %v", fr.Font.WordSpacing)
		}
	}
}

// Lines ended by a forced '\n' are paragraph-final: justify leaves them alone.
func TestInlineJustifySkipsForcedBreakLines(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText("one two\nthree four five six seven eight nine ten", f)}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 500, Wrap: true, Align: TextAlignJustify, BaseFont: f})
	if len(l.Lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(l.Lines))
	}
	for _, fr := range l.Lines[0].Frags {
		if fr.Font.WordSpacing != 0 {
			t.Errorf("line before forced break should not stretch, frag WordSpacing = %v", fr.Font.WordSpacing)
		}
	}
}

func TestInlineForcedBreak(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText("one\ntwo", f)}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	if len(l.Lines) != 2 {
		t.Fatalf("forced break should yield 2 lines, got %d", len(l.Lines))
	}
}

func TestInlineBoxSharesLineAndBaseline(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{
		inlineText("icon ", f),
		{Box: true, Size: Size{W: 12, H: 12}, VAlign: InlineBaseline, BoxIndex: 0},
		inlineText(" here", f),
	}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	if len(l.Lines) != 1 {
		t.Fatalf("box + text should share one line, got %d lines", len(l.Lines))
	}
	boxes := boxFrags(l)
	if len(boxes) != 1 {
		t.Fatalf("expected 1 box frag, got %d", len(boxes))
	}
	// Baseline alignment: the box's bottom edge sits on the line baseline.
	bottom := boxes[0].Rect.Y + boxes[0].Rect.H
	if d := bottom - l.Lines[0].Baseline; d > 0.5 || d < -0.5 {
		t.Errorf("box bottom %.2f not on baseline %.2f", bottom, l.Lines[0].Baseline)
	}
}

// A box carrying a Baseline (a text-bearing atomic box, e.g. a styled link
// kept as its own box) aligns its OWN baseline with the line baseline, rather
// than resting its bottom edge on it.
func TestInlineBoxBaselineAlign(t *testing.T) {
	f := Font{Size: 16}
	const boxBaseline = 12 // ascent-ish within a 16px-tall box
	items := []InlineItem{
		inlineText("word ", f),
		{Box: true, Size: Size{W: 40, H: 16}, VAlign: InlineBaseline, Baseline: boxBaseline, BoxIndex: 0},
		inlineText(" tail", f),
	}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	box := boxFrags(l)[0]
	// The box's own baseline (top + Baseline) sits on the line baseline.
	got := box.Rect.Y + boxBaseline
	if d := got - l.Lines[0].Baseline; d > 0.5 || d < -0.5 {
		t.Errorf("box baseline %.2f not on line baseline %.2f (box top %.2f)",
			got, l.Lines[0].Baseline, box.Rect.Y)
	}
	// And it must NOT be bottom-aligned (that was the bug — box sitting high).
	bottom := box.Rect.Y + box.Rect.H
	if bottom <= l.Lines[0].Baseline+0.5 {
		t.Errorf("baseline-aligned text box should extend its descent below the baseline (bottom %.2f, baseline %.2f)",
			bottom, l.Lines[0].Baseline)
	}
}

func TestInlineTallBoxRaisesLineHeight(t *testing.T) {
	f := Font{Size: 16}
	textOnly := BuildInlineLayout([]InlineItem{inlineText("x", f)}, InlineLayoutOptions{BaseFont: f})
	withBox := BuildInlineLayout([]InlineItem{
		inlineText("x ", f),
		{Box: true, Size: Size{W: 40, H: 40}, BoxIndex: 0},
	}, InlineLayoutOptions{BaseFont: f})
	if withBox.Lines[0].Height < 40 {
		t.Errorf("line with a 40px box should be >=40 tall, got %.2f", withBox.Lines[0].Height)
	}
	if withBox.Lines[0].Height <= textOnly.Lines[0].Height {
		t.Errorf("tall box should raise line height above text-only (%.2f vs %.2f)", withBox.Lines[0].Height, textOnly.Lines[0].Height)
	}
}

func TestInlineMiddleAlign(t *testing.T) {
	f := Font{Size: 16}
	l := BuildInlineLayout([]InlineItem{
		inlineText("x ", f),
		{Box: true, Size: Size{W: 20, H: 20}, VAlign: InlineMiddle, BoxIndex: 0},
	}, InlineLayoutOptions{BaseFont: f})
	box := boxFrags(l)[0]
	center := box.Rect.Y + box.Rect.H/2
	// vertical-align:middle centers the box on the reference ink midline
	// (baseline minus (ascent-descent)/2), i.e. raised above the baseline so
	// it reads as optically centered with the surrounding text.
	want := l.Lines[0].Baseline - inlineMidRef(f)
	if d := center - want; d > 0.5 || d < -0.5 {
		t.Errorf("middle box center %.2f, want %.2f (baseline %.2f, mid %.2f)", center, want, l.Lines[0].Baseline, inlineMidRef(f))
	}
	if center >= l.Lines[0].Baseline {
		t.Errorf("middle box center %.2f should sit above the baseline %.2f", center, l.Lines[0].Baseline)
	}
}

func TestInlineLinkAt(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{
		inlineText("visit ", f),
		{Text: "site", Font: f, Color: ColorBlack, Href: "https://x.example"},
	}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 10000, Wrap: true, BaseFont: f})
	origin := Rect{X: 0, Y: 0}
	// Find the link fragment and probe its center.
	var link InlineFrag
	found := false
	for _, ln := range l.Lines {
		for _, fr := range ln.Frags {
			if fr.Href != "" {
				link, found = fr, true
			}
		}
	}
	if !found {
		t.Fatal("no link fragment produced")
	}
	pt := Point{X: link.Rect.X + link.Rect.W/2, Y: link.Rect.Y + link.Rect.H/2}
	if href, ok := l.LinkAt(origin, pt); !ok || href != "https://x.example" {
		t.Errorf("LinkAt = (%q, %v), want the link href", href, ok)
	}
	// A point far below any line hits no link.
	if _, ok := l.LinkAt(origin, Point{X: 0, Y: 9999}); ok {
		t.Error("LinkAt off the layout should miss")
	}
}

// BreakLongWords is the character-level fallback for a run with no break
// opportunity: without it a pasted URL or a space-free string is one token
// and simply overflows MaxWidth (CSS overflow-wrap:normal, the default).
func TestInlineBreakLongWords(t *testing.T) {
	f := Font{Size: 16}
	long := "1231212312123121231212312123121231212312123121231212312"
	items := []InlineItem{inlineText(long, f)}
	maxW := float32(120)

	overflow := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: maxW, Wrap: true, BaseFont: f})
	if len(overflow.Lines) != 1 {
		t.Fatalf("default (overflow-wrap:normal) should keep one overflowing line, got %d", len(overflow.Lines))
	}
	if overflow.Width <= maxW {
		t.Fatalf("expected the unbroken run to overflow, width=%v maxW=%v", overflow.Width, maxW)
	}

	broken := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: maxW, Wrap: true, BaseFont: f, BreakLongWords: true})
	if len(broken.Lines) < 2 {
		t.Fatalf("BreakLongWords should split the run across lines, got %d", len(broken.Lines))
	}
	for i, ln := range broken.Lines {
		if ln.Width > maxW+0.5 {
			t.Errorf("line %d width %v exceeds MaxWidth %v", i, ln.Width, maxW)
		}
	}
	// Every rune survives, in order, and the source ranges stay contiguous.
	var joined string
	for _, ln := range broken.Lines {
		for _, fr := range ln.Frags {
			joined += fr.Text
		}
	}
	if joined != long {
		t.Errorf("text after breaking = %q, want %q", joined, long)
	}
	if got := broken.Lines[0].SrcStart; got != (SourcePos{Item: 0, Off: 0}) {
		t.Errorf("first line SrcStart = %+v, want item 0 off 0", got)
	}
	for i := 1; i < len(broken.Lines); i++ {
		if prev, cur := broken.Lines[i-1].SrcEnd, broken.Lines[i].SrcStart; prev != cur {
			t.Errorf("line %d starts at %+v but line %d ended at %+v — source ranges must be contiguous", i, cur, i-1, prev)
		}
	}
}

// A long unbreakable run mixed with ordinary words: the words still wrap at
// spaces, only the oversized run is cut, and it fills the line it starts on
// (break as late as possible) rather than beginning on a fresh one.
func TestInlineBreakLongWordsFillsCurrentLine(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText("ab 123456789012345678901234567890123456789012345678901234567890 cd", f)}
	maxW := float32(140)
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: maxW, Wrap: true, BaseFont: f, BreakLongWords: true})
	if len(l.Lines) < 3 {
		t.Fatalf("expected several lines, got %d", len(l.Lines))
	}
	first := l.Lines[0]
	var firstText string
	for _, fr := range first.Frags {
		firstText += fr.Text
	}
	if firstText == "ab " || firstText == "ab" {
		t.Errorf("first line = %q: the oversized run should start filling the line it is on, not move down", firstText)
	}
	for i, ln := range l.Lines {
		if ln.Width > maxW+0.5 {
			t.Errorf("line %d width %v exceeds MaxWidth %v (text %q)", i, ln.Width, maxW, lineText(ln))
		}
	}
	// Collapsing mode drops a space that lands at a line start, so compare
	// the visible glyphs: nothing may be lost or reordered by the split.
	var joined string
	for _, ln := range l.Lines {
		joined += strings.ReplaceAll(lineText(ln), " ", "")
	}
	if want := "ab123456789012345678901234567890123456789012345678901234567890cd"; joined != want {
		t.Errorf("text after breaking = %q, want %q", joined, want)
	}
}

func lineText(ln InlineLine) string {
	var s string
	for _, fr := range ln.Frags {
		s += fr.Text
	}
	return s
}

// Pathological widths must terminate: a MaxWidth narrower than a single glyph
// leaves the character break with nothing to cut, so it has to give up and
// overflow rather than emit empty lines forever.
func TestInlineBreakLongWordsDegenerateWidth(t *testing.T) {
	f := Font{Size: 16}
	done := make(chan InlineLayout, 1)
	go func() {
		done <- BuildInlineLayout([]InlineItem{inlineText("wwwwwwww", f)},
			InlineLayoutOptions{MaxWidth: 1, Wrap: true, BaseFont: f, BreakLongWords: true})
	}()
	select {
	case l := <-done:
		if len(l.Lines) == 0 {
			t.Fatal("no lines produced")
		}
		if got := flatFragText(l); got != "wwwwwwww" {
			t.Errorf("text = %q, want the run intact", got)
		}
		if len(l.Lines) > 8 {
			t.Errorf("expected one line per glyph at most, got %d", len(l.Lines))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BuildInlineLayout did not terminate with MaxWidth below one glyph")
	}
}

// A first-line indent hangs off the side the paragraph STARTS at. Left to
// right that is the left edge; right to left it is the right one, so the
// indented line's right edge sits `FirstIndent` inside the column instead of
// the line being pushed away from the left margin.
func TestInlineFirstIndentMirrorsForRTL(t *testing.T) {
	const width, indent = 300, 40
	items := []InlineItem{{Text: "one two", Font: Font{Size: 14}}}
	opts := InlineLayoutOptions{
		MaxWidth: width, Wrap: true, BaseFont: Font{Size: 14},
		FirstIndent: indent, PreserveWhitespace: true,
	}
	rightEdge := func(l InlineLayout) float32 {
		var right float32
		for _, f := range l.Lines[0].Frags {
			if x := f.Rect.X + f.Rect.W; x > right {
				right = x
			}
		}
		return right
	}
	leftEdge := func(l InlineLayout) float32 {
		left := float32(width)
		for _, f := range l.Lines[0].Frags {
			if f.Rect.X < left {
				left = f.Rect.X
			}
		}
		return left
	}

	opts.Direction = TextDirectionLTR
	ltr := BuildInlineLayout(items, opts)
	if got := leftEdge(ltr); got < indent-0.5 || got > indent+0.5 {
		t.Errorf("left-to-right first line starts at %v, want the %v indent", got, indent)
	}

	opts.Direction = TextDirectionRTL
	rtl := BuildInlineLayout(items, opts)
	if got, want := rightEdge(rtl), float32(width-indent); got < want-0.5 || got > want+0.5 {
		t.Errorf("right-to-left first line ends at %v, want %v", got, want)
	}
}
