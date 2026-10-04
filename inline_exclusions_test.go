package qui

import (
	"strings"
	"testing"
)

// Exclusions are the IFC half of "text wraps around a floating image": the
// engine narrows each line to the band its own vertical extent leaves free.
// The tests assert geometry (no fragment may overlap the excluded rectangle),
// not line counts, so they survive font-metric drift.

// lineBoxes returns each line's [left, right] ink span.
func lineSpans(l InlineLayout) [][2]float32 {
	out := make([][2]float32, 0, len(l.Lines))
	for _, ln := range l.Lines {
		if len(ln.Frags) == 0 {
			out = append(out, [2]float32{0, 0})
			continue
		}
		left, right := ln.Frags[0].Rect.X, float32(0)
		for _, f := range ln.Frags {
			if f.Rect.X < left {
				left = f.Rect.X
			}
			if r := f.Rect.X + f.Rect.W; r > right {
				right = r
			}
		}
		out = append(out, [2]float32{left, right})
	}
	return out
}

// overlaps finds a fragment whose INK runs into the exclusion. Trailing
// whitespace does not count: a space at a wrap point hangs past the edge it
// broke at (Word/pre-wrap semantics, and the same at a float's edge), and it
// draws nothing.
func overlaps(l InlineLayout, ex Rect) *InlineFrag {
	for li := range l.Lines {
		for fi := range l.Lines[li].Frags {
			f := l.Lines[li].Frags[fi]
			right := f.Rect.X + f.Rect.W
			if trimmed := strings.TrimRight(f.Text, " \t"); trimmed != f.Text {
				w, _ := TextMetrics(f.Text[len(trimmed):], f.Font)
				right -= w
			}
			if f.Rect.X < ex.X+ex.W && right > ex.X &&
				f.Rect.Y < ex.Y+ex.H && f.Rect.Y+f.Rect.H > ex.Y {
				return &l.Lines[li].Frags[fi]
			}
		}
	}
	return nil
}

const exclusionText = "one two three four five six seven eight nine ten eleven twelve " +
	"thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty"

// A float on the RIGHT keeps the lines beside it short; the lines below it
// run the full width again.
func TestInlineExclusionOnTheRight(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 200, Y: 0, W: 100, H: 40}
	opts := InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f, Exclusions: []Rect{ex}}
	l := BuildInlineLayout([]InlineItem{inlineText(exclusionText, f)}, opts)

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the exclusion %+v", bad.Text, bad.Rect, ex)
	}
	spans := lineSpans(l)
	if len(spans) < 3 {
		t.Fatalf("expected several lines, got %d", len(spans))
	}
	// A line clear of the float must be free to use the width the beside-it
	// lines could not.
	var beside, below float32
	for i, ln := range l.Lines {
		if ln.Y+ln.Height <= ex.Y+ex.H {
			if spans[i][1] > beside {
				beside = spans[i][1]
			}
		} else if spans[i][1] > below {
			below = spans[i][1]
		}
	}
	if beside > ex.X {
		t.Errorf("a line beside the float reaches x=%v, past the float's left edge %v", beside, ex.X)
	}
	if below <= beside {
		t.Errorf("lines below the float (%v) are no wider than the ones beside it (%v)", below, beside)
	}
}

// A float on the LEFT indents the lines beside it instead of shortening them.
func TestInlineExclusionOnTheLeft(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 0, Y: 0, W: 120, H: 40}
	l := BuildInlineLayout([]InlineItem{inlineText(exclusionText, f)},
		InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f, Exclusions: []Rect{ex}})

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the exclusion %+v", bad.Text, bad.Rect, ex)
	}
	spans := lineSpans(l)
	for i, ln := range l.Lines {
		if ln.Y >= ex.Y+ex.H {
			if spans[i][0] > 1 {
				t.Errorf("line %d below the float starts at x=%v, want the left margin", i, spans[i][0])
			}
			continue
		}
		if spans[i][0] < ex.W {
			t.Errorf("line %d beside the float starts at x=%v, want ≥ %v", i, spans[i][0], ex.W)
		}
	}
}

// A float spanning the whole column has no room beside it, so the text starts
// below it — Word's "break text" wrapping, and what CSS does with a float too
// wide for a line.
func TestInlineExclusionPushesTextBelow(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 0, Y: 0, W: 300, H: 80}
	l := BuildInlineLayout([]InlineItem{inlineText(exclusionText, f)},
		InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f, Exclusions: []Rect{ex}})

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the full-width exclusion", bad.Text, bad.Rect)
	}
	if got := l.Lines[0].Y; got < ex.H {
		t.Errorf("first line at y=%v, want it pushed to %v or below", got, ex.H)
	}
	if l.Height <= ex.H {
		t.Errorf("layout height %v does not account for the space the float took", l.Height)
	}
}

// A float that clears both edges leaves two bands and a line can only use
// one. It takes the LEFT band — text reads and is typed from there, and a
// float slightly left of centre must not fling the whole paragraph to the
// other side of the page (which is exactly what "wider side wins" did).
func TestInlineExclusionInTheMiddlePrefersTheLeftBand(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 148, Y: 0, W: 404, H: 300} // 148px of column left, 208 right
	l := BuildInlineLayout([]InlineItem{inlineText("1231", f)},
		InlineLayoutOptions{MaxWidth: 760, Wrap: true, BaseFont: f, PreserveWhitespace: true,
			Exclusions: []Rect{ex}})

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the exclusion", bad.Text, bad.Rect)
	}
	if got := lineSpans(l)[0][0]; got > 1 {
		t.Errorf("line starts at x=%v, want the left margin — the left band fits it", got)
	}
}

// A line does not STOP at the float: it fills the slot on its left and
// continues on its right, which is what "wrap text / both sides" means. Text
// piling up in a narrow left band while the whole right of the page sits
// empty is the bug this guards.
func TestInlineExclusionFillsBothSidesOfAFloat(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 150, Y: 0, W: 300, H: 200}
	l := BuildInlineLayout([]InlineItem{inlineText(exclusionText, f)},
		InlineLayoutOptions{MaxWidth: 800, Wrap: true, BaseFont: f, PreserveWhitespace: true,
			Exclusions: []Rect{ex}})

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the exclusion", bad.Text, bad.Rect)
	}
	// The first line beside the float has to use BOTH sides of it.
	line := l.Lines[0]
	var left, right bool
	for _, fr := range line.Frags {
		if fr.Rect.X < ex.X-0.5 {
			left = true
		}
		if fr.Rect.X >= ex.X+ex.W-0.5 {
			right = true
		}
	}
	if !left || !right {
		t.Errorf("line 0 uses left=%v right=%v of the float; want both", left, right)
	}
	// …and it starts at the left margin, where the caret is.
	if got := lineSpans(l)[0][0]; got > 1 {
		t.Errorf("line starts at x=%v, want the left margin", got)
	}
}

// …unless the left slot is too narrow for what goes on the line, in which
// case the line starts on the roomier side rather than overflowing into the
// float.
func TestInlineExclusionStartsInTheRightSlotWhenLeftIsTooNarrow(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 12, Y: 0, W: 140, H: 40} // barely any column to its left
	l := BuildInlineLayout([]InlineItem{inlineText("wrapping", f)},
		InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f, PreserveWhitespace: true,
			Exclusions: []Rect{ex}})

	if bad := overlaps(l, ex); bad != nil {
		t.Fatalf("fragment %q at %+v runs into the exclusion", bad.Text, bad.Rect)
	}
	if got := lineSpans(l)[0][0]; got < ex.X+ex.W {
		t.Errorf("line starts at %v, want it past the float at %v", got, ex.X+ex.W)
	}
}

// An empty line — the one the caret sits on before anything is typed — keeps
// the left band too, so the caret is where the first character will land.
func TestInlineExclusionEmptyLineKeepsTheCaretSide(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 148, Y: 0, W: 404, H: 300}
	l := BuildInlineLayout(nil, InlineLayoutOptions{MaxWidth: 760, Wrap: true, BaseFont: f,
		PreserveWhitespace: true, Exclusions: []Rect{ex}})
	if len(l.Lines) != 1 {
		t.Fatalf("%d lines for an empty layout, want 1", len(l.Lines))
	}
	typed := BuildInlineLayout([]InlineItem{inlineText("1231", f)},
		InlineLayoutOptions{MaxWidth: 760, Wrap: true, BaseFont: f, PreserveWhitespace: true,
			Exclusions: []Rect{ex}})
	if got := lineSpans(typed)[0][0]; got > 1 {
		t.Errorf("typing moved the line to x=%v; the caret was at the left margin", got)
	}
}

// Centring happens inside the line's OWN band: a centred line beside a float
// centres in what is left of the column.
func TestInlineExclusionCentresInTheBand(t *testing.T) {
	f := Font{Size: 16}
	ex := Rect{X: 200, Y: 0, W: 100, H: 40}
	l := BuildInlineLayout([]InlineItem{inlineText("one two", f)},
		InlineLayoutOptions{MaxWidth: 300, Wrap: true, Align: TextAlignCenter, BaseFont: f,
			Exclusions: []Rect{ex}})

	span := lineSpans(l)[0]
	mid := (span[0] + span[1]) / 2
	if want := ex.X / 2; mid < want-6 || mid > want+6 {
		t.Errorf("line centred at %v, want ≈%v (the middle of the free band)", mid, want)
	}
	if bad := overlaps(l, ex); bad != nil {
		t.Errorf("centred fragment %q runs into the exclusion", bad.Text)
	}
}

// A taller line — one carrying an inline image — is measured against the band
// its whole height sees, not the band a text-sized probe would have found.
func TestInlineExclusionSeesTallLines(t *testing.T) {
	f := Font{Size: 16}
	// The float only starts 30px down: a text-sized probe at the top clears
	// it, so a 60px-tall line measured that way would be broken against the
	// full column and drive its 200px-wide box straight through the float.
	ex := Rect{X: 150, Y: 30, W: 150, H: 100}
	items := []InlineItem{
		{Box: true, Size: Size{W: 200, H: 60}, BoxIndex: 0},
		inlineText(" tail", f),
	}
	l := BuildInlineLayout(items, InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f,
		Exclusions: []Rect{ex}})
	if bad := overlaps(l, ex); bad != nil {
		t.Errorf("fragment %q at %+v ignored the float a tall line runs into", bad.Text, bad.Rect)
	}
}

// Without exclusions nothing changes — the band is the whole column.
func TestInlineNoExclusionsIsUnchanged(t *testing.T) {
	f := Font{Size: 16}
	items := []InlineItem{inlineText(exclusionText, f)}
	opts := InlineLayoutOptions{MaxWidth: 300, Wrap: true, BaseFont: f}
	plain := BuildInlineLayout(items, opts)
	opts.Exclusions = []Rect{}
	empty := BuildInlineLayout(items, opts)
	if len(plain.Lines) != len(empty.Lines) || plain.Height != empty.Height || plain.Width != empty.Width {
		t.Fatalf("an empty exclusion list changed the layout: %d lines/%v/%v vs %d/%v/%v",
			len(plain.Lines), plain.Height, plain.Width, len(empty.Lines), empty.Height, empty.Width)
	}
	for li := range plain.Lines {
		if plain.Lines[li].Y != empty.Lines[li].Y || plain.Lines[li].Width != empty.Lines[li].Width {
			t.Fatalf("line %d moved: %+v vs %+v", li, plain.Lines[li], empty.Lines[li])
		}
	}
}
