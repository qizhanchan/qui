package widgets

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// Editing support for InlineBox: a host that builds the box from a model
// (htmlcss's contenteditable) tags each appended part with its source and
// then asks source-space questions — where the caret for a position is,
// which position a point lands on — in the box's own coordinate space
// (the space of Bounds). Item i of the layout is part i (buildItems emits
// exactly one item per part), so a SourcePos's Item indexes the tags.

// PartCount returns the number of appended parts (text runs, boxes,
// breaks and span edges) — the index the next part will get.
func (b *InlineBox) PartCount() int { return len(b.parts) }

// TagLastPart attaches tag to the most recently appended part.
func (b *InlineBox) TagLastPart(tag any) {
	n := len(b.parts)
	if n == 0 {
		return
	}
	for len(b.partTags) < n {
		b.partTags = append(b.partTags, nil)
	}
	b.partTags[n-1] = tag
}

// PartTag returns the tag of part i (nil when untagged).
func (b *InlineBox) PartTag(i int) any {
	if i < 0 || i >= len(b.partTags) || i >= len(b.parts) {
		return nil
	}
	return b.partTags[i]
}

// PartText returns the text of part i as laid out ("" for a box).
func (b *InlineBox) PartText(i int) string {
	if i < 0 || i >= len(b.parts) || b.parts[i].isBox {
		return ""
	}
	return b.parts[i].text
}

// PartIsBox reports whether part i is an atomic box.
func (b *InlineBox) PartIsBox(i int) bool {
	return i >= 0 && i < len(b.parts) && b.parts[i].isBox
}

// EditLayout returns the current layout, building it for the box's width
// when content changed since the last layout pass. ok is false before the
// box was ever laid out.
func (b *InlineBox) EditLayout() (InlineLayout, bool) {
	w := b.Bounds().W
	if !b.haveLay {
		if w <= 0 {
			return InlineLayout{}, false
		}
		b.buildLayout(w)
	}
	return b.layout, true
}

// CaretRectAt returns the caret rectangle for pos in the box's own
// coordinate space. line >= 0 pins the caret to that line (wrap affinity).
func (b *InlineBox) CaretRectAt(pos SourcePos, line int) (Rect, bool) {
	l, ok := b.EditLayout()
	if !ok || len(l.Lines) == 0 {
		return Rect{}, false
	}
	var r Rect
	if line >= 0 {
		r = l.CaretRectInLine(line, pos)
	} else {
		r = l.CaretRectForSource(pos)
	}
	o := b.Bounds()
	r.X += o.X
	r.Y += o.Y
	return r, true
}

// SourcePosAtPoint maps a point in the box's own coordinate space to the
// nearest caret position and the line it falls on.
func (b *InlineBox) SourcePosAtPoint(p Point) (SourcePos, int, bool) {
	l, ok := b.EditLayout()
	if !ok || len(l.Lines) == 0 {
		return SourcePos{}, 0, false
	}
	o := b.Bounds()
	pos, li := l.SourcePosAt(Point{X: p.X - o.X, Y: p.Y - o.Y})
	return pos, li, true
}

// LineEdgePos returns the first (end=false) or last caret position of
// line li.
func (b *InlineBox) LineEdgePos(li int, end bool) (SourcePos, bool) {
	l, ok := b.EditLayout()
	if !ok || len(l.Lines) == 0 {
		return SourcePos{}, false
	}
	x := float32(-math.MaxFloat32 / 4)
	if end {
		x = math.MaxFloat32 / 4
	}
	return l.SourcePosAtLineX(li, x), true
}

// selectionOffsetFor maps a source position into the box's selection
// offset space (see walkSelFrags).
func (b *InlineBox) selectionOffsetFor(pos SourcePos) int {
	result := -1
	total := b.walkSelFrags(func(sf selFrag) {
		if result >= 0 {
			return
		}
		f := sf.frag
		if f.SrcItem < 0 {
			return
		}
		start := SourcePos{Item: f.SrcItem, Off: f.SrcStart}
		end := SourcePos{Item: f.SrcItem, Off: f.SrcEnd}
		switch {
		case pos.Cmp(start) <= 0:
			result = sf.start
		case pos.Cmp(end) <= 0:
			if sf.isBox {
				result = sf.end
			} else {
				result = minInt(sf.start+pos.Off-f.SrcStart, sf.end)
			}
		}
	})
	if result < 0 {
		return total
	}
	return result
}

// SetSourceSelection highlights the source range [a, c) (in source order;
// a == c clears).
func (b *InlineBox) SetSourceSelection(a, c SourcePos) {
	if _, ok := b.EditLayout(); !ok || a.Cmp(c) >= 0 {
		b.ClearTextSelection()
		return
	}
	b.SetSelectionRange(b.selectionOffsetFor(a), b.selectionOffsetFor(c))
}
