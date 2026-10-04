package qui

// Source-space caret and hit-test queries over an InlineLayout — the editing
// surface of the IFC. Editors address positions as SourcePos (item index +
// rune offset in that item's Text) and use these to place a caret, resolve a
// click, and navigate line-up/line-down.
//
// All coordinates here are layout-relative (0,0 at the layout's top-left);
// the hosting widget translates to/from window space with its own origin.
//
// Precision: with InlineLayoutOptions.PreserveWhitespace these queries are
// exact — every rune is addressable. In collapse mode they are best-effort:
// positions inside collapsed/dropped whitespace snap to the nearest
// surviving boundary, and text merged from a non-contiguous source resolves
// to its fragment's edge.

// LineForSource returns the index of the line containing pos: the first
// line whose source range ends at or after pos. A position in the gap
// between two lines (a dropped wrap space or a '\n' rune) belongs to the
// earlier line when it sits at that line's end, otherwise to the later
// line. Positions past the end of content clamp to the last line.
func (l InlineLayout) LineForSource(pos SourcePos) int {
	for li := range l.Lines {
		if pos.Cmp(l.Lines[li].SrcEnd) <= 0 {
			return li
		}
	}
	if len(l.Lines) == 0 {
		return 0
	}
	return len(l.Lines) - 1
}

// CaretRectForSource returns the caret rectangle (zero width, text-ink
// height anchored on the line baseline — NOT the leaded line box, which
// reads far too tall as a caret) for the insertion point pos. At a
// soft-wrap boundary — where line N's end equals line N+1's start — the
// caret resolves to the end of line N (Word behavior: the caret after a
// wrap-hanging space stays on the earlier line); use CaretRectInLine to
// force the other affinity.
func (l InlineLayout) CaretRectForSource(pos SourcePos) Rect {
	return l.CaretRectInLine(l.LineForSource(pos), pos)
}

// CaretRectInLine is CaretRectForSource pinned to a specific line: pos is
// clamped into the line's source range first. This is the affinity control
// for wrap boundaries (same pos, previous or next line).
func (l InlineLayout) CaretRectInLine(li int, pos SourcePos) Rect {
	if len(l.Lines) == 0 {
		return Rect{}
	}
	if li < 0 {
		li = 0
	}
	if li >= len(l.Lines) {
		li = len(l.Lines) - 1
	}
	line := &l.Lines[li]
	if pos.Cmp(line.SrcStart) < 0 {
		pos = line.SrcStart
	}
	if pos.Cmp(line.SrcEnd) > 0 {
		pos = line.SrcEnd
	}
	caret := func(x float32) Rect {
		return Rect{X: x, Y: line.Baseline - line.InkAscent, W: 0,
			H: line.InkAscent + line.InkDescent}
	}
	x := float32(0)
	if len(line.Frags) > 0 {
		x = line.Frags[0].Rect.X
	}
	for fi := range line.Frags {
		f := &line.Frags[fi]
		if f.SrcItem < 0 {
			continue // not an exact source slice; boundaries stay put
		}
		if pos.Cmp(SourcePos{Item: f.SrcItem, Off: f.SrcStart}) < 0 {
			break // pos sits in a source gap before this fragment
		}
		if pos.Cmp(SourcePos{Item: f.SrcItem, Off: f.SrcEnd}) >= 0 {
			x = f.Rect.X + f.srcWidth()
			continue
		}
		// Inside the fragment's addressable range.
		if f.Box {
			x = f.Rect.X // pos.Off == SrcStart ("before the box")
		} else {
			runes := []rune(f.Text)
			n := pos.Off - f.SrcStart
			if n > len(runes) {
				n = len(runes)
			}
			x = f.Rect.X + TextXForOffset(f.Text, f.Font, TextDirectionAuto, n)
		}
		return caret(x)
	}
	return caret(x)
}

// SourcePosAt maps a layout-relative point to the nearest caret position,
// returning the position and the line it landed on (feed that line index to
// CaretRectInLine to keep the caret on the clicked line at wrap
// boundaries). Points above the first line, below the last, or outside a
// line horizontally clamp to the nearest boundary.
func (l InlineLayout) SourcePosAt(pt Point) (SourcePos, int) {
	if len(l.Lines) == 0 {
		return SourcePos{}, 0
	}
	li := len(l.Lines) - 1
	for i := range l.Lines {
		if pt.Y < l.Lines[i].Y+l.Lines[i].Height {
			li = i
			break
		}
	}
	return l.SourcePosAtLineX(li, pt.X), li
}

// SourcePosAtLineX returns the caret position nearest to x on line li —
// the primitive behind vertical caret navigation: resolve the caret's
// current x, then query the line above/below at that same x.
func (l InlineLayout) SourcePosAtLineX(li int, x float32) SourcePos {
	if len(l.Lines) == 0 {
		return SourcePos{}
	}
	if li < 0 {
		li = 0
	}
	if li >= len(l.Lines) {
		li = len(l.Lines) - 1
	}
	line := &l.Lines[li]
	best := line.SrcStart
	prevX := float32(0)
	if len(line.Frags) > 0 {
		prevX = line.Frags[0].Rect.X
	}
	for fi := range line.Frags {
		f := &line.Frags[fi]
		if f.SrcItem < 0 {
			continue
		}
		left := f.Rect.X
		if x < left {
			// In the gap between the previous boundary and this fragment:
			// snap to the nearer edge.
			if x-prevX >= left-x {
				return SourcePos{Item: f.SrcItem, Off: f.SrcStart}
			}
			return best
		}
		if f.Box {
			if x < f.Rect.X+f.Rect.W/2 {
				return SourcePos{Item: f.SrcItem, Off: f.SrcStart}
			}
			best = SourcePos{Item: f.SrcItem, Off: f.SrcEnd}
			prevX = f.Rect.X + f.Rect.W
			continue
		}
		right := f.Rect.X + f.srcWidth()
		if f.Tab && x <= right {
			// One rune whose advance is the whole gap: the midpoint rule
			// has to use the RECT, not the tab glyph's own tiny width.
			if x < f.Rect.X+f.Rect.W/2 {
				return SourcePos{Item: f.SrcItem, Off: f.SrcStart}
			}
			return SourcePos{Item: f.SrcItem, Off: f.SrcEnd}
		}
		if x <= right {
			runes := []rune(f.Text)
			n := f.SrcEnd - f.SrcStart
			if n > len(runes) {
				n = len(runes)
			}
			local := TextOffsetAtX(string(runes[:n]), f.Font, TextDirectionAuto, x-left)
			return SourcePos{Item: f.SrcItem, Off: f.SrcStart + local}
		}
		best = SourcePos{Item: f.SrcItem, Off: f.SrcEnd}
		prevX = right
	}
	if len(line.Frags) == 0 {
		return best
	}
	return line.SrcEnd
}

// srcWidth is the advance of the fragment's addressable source prefix: the
// full rect width for exact fragments, the measured prefix for fragments a
// collapse-mode merge extended past their source range.
func (f *InlineFrag) srcWidth() float32 {
	if f.Box {
		return f.Rect.W
	}
	runes := []rune(f.Text)
	n := f.SrcEnd - f.SrcStart
	if n >= len(runes) {
		return f.Rect.W
	}
	if n < 0 {
		n = 0
	}
	return TextXForOffset(f.Text, f.Font, TextDirectionAuto, n)
}
