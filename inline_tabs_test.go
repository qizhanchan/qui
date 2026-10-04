package qui

import "testing"

// Tab stops in the IFC. Without them a '\t' measured as its glyph, so a
// word processor could only fake tabs with runs of spaces — which then
// wrap, don't line up between paragraphs, and can't carry dot leaders.

func tabLayout(text string, opts InlineLayoutOptions) InlineLayout {
	opts.PreserveWhitespace = true
	opts.Wrap = true
	if opts.BaseFont.Size == 0 {
		opts.BaseFont = Font{Size: 14}
	}
	items := []InlineItem{{Text: text, Font: opts.BaseFont}}
	return BuildInlineLayout(items, opts)
}

// tabFrag returns the nth tab fragment on line 0.
func tabFrag(t *testing.T, l InlineLayout, n int) InlineFrag {
	t.Helper()
	seen := 0
	for _, f := range l.Lines[0].Frags {
		if f.Tab {
			if seen == n {
				return f
			}
			seen++
		}
	}
	t.Fatalf("line 0 has no tab fragment #%d", n)
	return InlineFrag{}
}

func TestTabAdvancesToInterval(t *testing.T) {
	l := tabLayout("a\tb", InlineLayoutOptions{MaxWidth: 400, TabInterval: 96})
	f := tabFrag(t, l, 0)
	if got := f.Rect.X + f.Rect.W; !approxEq(got, 96) {
		t.Fatalf("tab ends at %.2f, want the 96px stop", got)
	}
	// The next tab goes to the NEXT multiple, not 96 further on.
	l = tabLayout("a\tb\tc", InlineLayoutOptions{MaxWidth: 400, TabInterval: 96})
	f = tabFrag(t, l, 1)
	if got := f.Rect.X + f.Rect.W; !approxEq(got, 192) {
		t.Fatalf("second tab ends at %.2f, want 192", got)
	}
}

func TestTabWithoutStopsKeepsGlyphWidth(t *testing.T) {
	// The default must stay what it was: UI text has no tab stops, and a
	// tab there is just a character.
	l := tabLayout("a\tb", InlineLayoutOptions{MaxWidth: 400})
	f := tabFrag(t, l, 0)
	glyph, _ := TextMetrics("\t", Font{Size: 14})
	if !approxEq(f.Rect.W, glyph) {
		t.Fatalf("tab width %.2f with no stops, want the glyph advance %.2f", f.Rect.W, glyph)
	}
}

func TestExplicitStopsBeatTheInterval(t *testing.T) {
	l := tabLayout("a\tb\tc", InlineLayoutOptions{
		MaxWidth:    400,
		TabStops:    []TabStop{{Pos: 50}, {Pos: 130}},
		TabInterval: 96,
	})
	if got := tabFrag(t, l, 0); !approxEq(got.Rect.X+got.Rect.W, 50) {
		t.Fatalf("first tab ended at %.2f, want the explicit 50px stop", got.Rect.X+got.Rect.W)
	}
	if got := tabFrag(t, l, 1); !approxEq(got.Rect.X+got.Rect.W, 130) {
		t.Fatalf("second tab ended at %.2f, want the explicit 130px stop", got.Rect.X+got.Rect.W)
	}
}

func TestRightAlignedTabEndsRunAtTheStop(t *testing.T) {
	// The table-of-contents shape: heading, tab, page number flush right.
	const stop = 300
	l := tabLayout("Chapter one\t12", InlineLayoutOptions{
		MaxWidth: stop,
		TabStops: []TabStop{{Pos: stop, Align: TabRight, Leader: '.'}},
	})
	if len(l.Lines) != 1 {
		t.Fatalf("line count %d, want 1 — a right tab must not push the run past MaxWidth", len(l.Lines))
	}
	last := l.Lines[0].Frags[len(l.Lines[0].Frags)-1]
	if got := last.Rect.X + last.Rect.W; !approxEq(got, stop) {
		t.Fatalf("the run after the tab ends at %.2f, want %d", got, stop)
	}
	if f := tabFrag(t, l, 0); f.Leader != '.' {
		t.Fatalf("tab fragment leader = %q, want '.'", f.Leader)
	}
}

func TestCentreAlignedTabCentresTheRun(t *testing.T) {
	const stop = 200
	l := tabLayout("a\tmiddle", InlineLayoutOptions{
		MaxWidth: 400,
		TabStops: []TabStop{{Pos: stop, Align: TabCenter}},
	})
	f := tabFrag(t, l, 0)
	last := l.Lines[0].Frags[len(l.Lines[0].Frags)-1]
	mid := (f.Rect.X + f.Rect.W + last.Rect.X + last.Rect.W) / 2
	if !approxEq(mid, stop) {
		t.Fatalf("run centred at %.2f, want %d", mid, stop)
	}
}

func TestTabPastEveryStopDoesNotMoveBackwards(t *testing.T) {
	// Text that already overran the only stop: the tab collapses instead of
	// dragging the following word back over what is already drawn.
	l := tabLayout("aaaaaaaaaaaaaaaaaaaaaaaa\tb", InlineLayoutOptions{
		MaxWidth: 800,
		TabStops: []TabStop{{Pos: 10}},
	})
	f := tabFrag(t, l, 0)
	if f.Rect.W < 0 {
		t.Fatalf("tab width %.2f is negative", f.Rect.W)
	}
	if f.Rect.X+f.Rect.W < f.Rect.X {
		t.Fatal("tab moved the pen backwards")
	}
}

func TestTabIsAddressableAsOneRune(t *testing.T) {
	// The caret must be able to sit on both sides of a tab, and a click in
	// the gap must resolve by the GAP's midpoint, not the glyph's.
	l := tabLayout("ab\tcd", InlineLayoutOptions{MaxWidth: 400, TabInterval: 120})
	f := tabFrag(t, l, 0)
	before := l.SourcePosAtLineX(0, f.Rect.X+1)
	after := l.SourcePosAtLineX(0, f.Rect.X+f.Rect.W-1)
	if before.Off != 2 {
		t.Fatalf("click at the tab's left edge resolved to off %d, want 2", before.Off)
	}
	if after.Off != 3 {
		t.Fatalf("click at the tab's right edge resolved to off %d, want 3", after.Off)
	}
	// Caret rects on both sides bracket the gap.
	l0 := l.CaretRectForSource(SourcePos{Item: 0, Off: 2})
	l1 := l.CaretRectForSource(SourcePos{Item: 0, Off: 3})
	if !approxEq(l0.X, f.Rect.X) || !approxEq(l1.X, f.Rect.X+f.Rect.W) {
		t.Fatalf("caret rects %.2f / %.2f do not bracket the tab [%.2f, %.2f]",
			l0.X, l1.X, f.Rect.X, f.Rect.X+f.Rect.W)
	}
}

func TestTabNeverMergesIntoNeighbouringText(t *testing.T) {
	// A merged tab would make the fragment's width stop matching the sum of
	// its runes' advances, which every source-mapping helper relies on.
	l := tabLayout("ab\tcd", InlineLayoutOptions{MaxWidth: 400, TabInterval: 120})
	for _, f := range l.Lines[0].Frags {
		if f.Tab && f.Text != "\t" {
			t.Fatalf("tab fragment absorbed neighbours: %q", f.Text)
		}
		if !f.Tab {
			for _, r := range f.Text {
				if r == '\t' {
					t.Fatalf("fragment %q swallowed a tab", f.Text)
				}
			}
		}
	}
}

func approxEq(a, b float32) bool {
	d := a - b
	return d < 0.5 && d > -0.5
}

// A tab stop past the end of the line is clamped to the line's right edge.
// Google Docs writes a table of contents with a right stop at 12000 twips
// (8.33in) regardless of the page, so on A4 with 1in margins the stop sits
// beyond the 6.27in text column. Honouring it literally pushed every page
// number onto a line of its own; Docs (and a browser) put it at the margin.
func TestTabStopBeyondTheLineIsClampedToItsEdge(t *testing.T) {
	const width = 300
	l := tabLayout("Chapter one\t12", InlineLayoutOptions{
		MaxWidth: width,
		TabStops: []TabStop{{Pos: 800, Align: TabRight, Leader: '.'}},
	})
	if len(l.Lines) != 1 {
		t.Fatalf("line count %d, want 1 — an over-wide stop must not wrap the line", len(l.Lines))
	}
	last := l.Lines[0].Frags[len(l.Lines[0].Frags)-1]
	if got := last.Rect.X + last.Rect.W; !approxEq(got, width) {
		t.Errorf("the run after the tab ends at %.2f, want the right edge %d", got, width)
	}
}

// A word processor measures its default tab stops from the text COLUMN's left
// edge, not from each paragraph's indent: an indented paragraph's first tab
// lands on the next grid line past its indent, not one full interval past it.
// A host that lays such a paragraph out in its own space says how far that
// space sits right of the column with TabOrigin.
func TestTabOriginAnchorsTheGridToTheColumn(t *testing.T) {
	// Layout left edge 60px into the column, 96px grid: the column's grid
	// lines are at 96 and 192, i.e. 36 and 132 in the layout's own space.
	l := tabLayout("a\tb\tc", InlineLayoutOptions{
		MaxWidth: 400, TabInterval: 96, TabOrigin: 60,
	})
	if got := tabFrag(t, l, 0); !approxEq(got.Rect.X+got.Rect.W, 36) {
		t.Fatalf("first tab ended at %.2f, want 36 (column stop 96 − 60 indent)",
			got.Rect.X+got.Rect.W)
	}
	if got := tabFrag(t, l, 1); !approxEq(got.Rect.X+got.Rect.W, 132) {
		t.Fatalf("second tab ended at %.2f, want 132 (column stop 192 − 60 indent)",
			got.Rect.X+got.Rect.W)
	}
}

// A grid line the indent has already passed is not a stop: text starting past
// the column's 96px line advances to the 192px one.
func TestTabOriginSkipsGridLinesLeftOfTheIndent(t *testing.T) {
	l := tabLayout("a\tb", InlineLayoutOptions{
		MaxWidth: 400, TabInterval: 96, TabOrigin: 100,
	})
	if got := tabFrag(t, l, 0); !approxEq(got.Rect.X+got.Rect.W, 92) {
		t.Fatalf("tab ended at %.2f, want 92 (column stop 192 − 100 indent)",
			got.Rect.X+got.Rect.W)
	}
}

// Explicit stops are already in the layout's own space, so TabOrigin must not
// shift them — it only re-anchors the DEFAULT grid.
func TestTabOriginLeavesExplicitStopsAlone(t *testing.T) {
	l := tabLayout("a\tb", InlineLayoutOptions{
		MaxWidth: 400, TabStops: []TabStop{{Pos: 50}}, TabInterval: 96, TabOrigin: 60,
	})
	if got := tabFrag(t, l, 0); !approxEq(got.Rect.X+got.Rect.W, 50) {
		t.Fatalf("tab ended at %.2f, want the explicit 50px stop", got.Rect.X+got.Rect.W)
	}
}
