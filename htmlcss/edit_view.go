package htmlcss

import (
	"math"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Editing geometry: DOM positions ↔ the InlineBox runs that render them.
// Every text run, <br> and atomic box an element folds into an InlineBox
// is tagged with the node it came from (partTag), so a position maps to a
// (box, SourcePos) pair for the caret and a click maps back.

// partTag is the source of one InlineBox part.
type partTag struct {
	el   *El
	kind uint8
}

const (
	tagText  uint8 = iota // el is a text node, or an element's own text
	tagBreak              // el is a <br>
	tagBox                // el is an atomic inline box
)

// tagAdded tags the part ib gained since it had n0 parts.
func tagAdded(ib *widgets.InlineBox, n0 int, el *El, kind uint8) {
	if ib.PartCount() > n0 {
		ib.TagLastPart(partTag{el: el, kind: kind})
	}
}

// liveRuns lists e's inline runs currently in its arranged children.
func (e *El) liveRuns() []*widgets.InlineBox {
	if e.folded {
		return nil // its content rides in an ancestor's run
	}
	var out []*widgets.InlineBox
	for _, w := range e.flowKids {
		if ib, ok := w.(*widgets.InlineBox); ok && ib.Parent() != nil {
			out = append(out, ib)
		}
	}
	return out
}

// findPart finds the run part rendering node n (text node, own text, <br>
// or atomic box).
func findPart(n *El) (*widgets.InlineBox, int) {
	for a := n; a != nil; a = a.elParent {
		for _, ib := range a.liveRuns() {
			for i, c := 0, ib.PartCount(); i < c; i++ {
				if t, ok := ib.PartTag(i).(partTag); ok && t.el == n {
					return ib, i
				}
			}
		}
	}
	return nil, -1
}

func isWS(r rune) bool { return r < 0x80 && isSpace(byte(r)) }

// alignMap maps rune offsets between a node's DOM text and the text it was
// laid out as (whitespace collapsed): s2i[srcOff] = item offset and
// i2s[itemOff] = DOM offset.
func alignMap(src, item []rune) (s2i, i2s []int) {
	s2i = make([]int, len(src)+1)
	i2s = make([]int, len(item)+1)
	i, j := 0, 0
	for i < len(src) || j < len(item) {
		switch {
		case i < len(src) && j < len(item) && (src[i] == item[j] || !isWS(src[i]) && !isWS(item[j])):
			s2i[i], i2s[j] = j, i
			i++
			j++
		case i < len(src) && isWS(src[i]):
			s2i[i] = j
			i++
		case j < len(item):
			i2s[j] = i
			j++
		default:
			s2i[i] = j
			i++
		}
	}
	s2i[len(src)] = len(item)
	i2s[len(item)] = len(src)
	return s2i, i2s
}

// domToSource maps a position to the run and source position rendering it.
func domToSource(p Pos) (*widgets.InlineBox, qui.SourcePos, bool) {
	return domToSourceDepth(p, 0)
}

func domToSourceDepth(p Pos, depth int) (*widgets.InlineBox, qui.SourcePos, bool) {
	n := p.Node
	if n == nil || depth > 64 {
		return nil, qui.SourcePos{}, false
	}
	if n.isTextSeg() {
		if ib, item := findPart(n); ib != nil {
			s2i, _ := alignMap([]rune(n.text), []rune(ib.PartText(item)))
			off := min(max(p.Off, 0), len(s2i)-1)
			return ib, qui.SourcePos{Item: item, Off: s2i[off]}, true
		}
		// Not rendered (collapsed away): use a neighbour.
		par := n.elParent
		if par == nil {
			return nil, qui.SourcePos{}, false
		}
		i := childIndex(par, n)
		if c := kidEl(par, i+1); c != nil {
			if ib, sp, ok := posBefore(c, depth+1); ok {
				return ib, sp, true
			}
		}
		if c := kidEl(par, i-1); c != nil {
			return posAfter(c, depth+1)
		}
		return nil, qui.SourcePos{}, false
	}
	if c := kidEl(n, p.Off); c != nil {
		if ib, sp, ok := posBefore(c, depth+1); ok {
			return ib, sp, true
		}
	}
	if c := kidEl(n, p.Off-1); c != nil {
		if ib, sp, ok := posAfter(c, depth+1); ok {
			return ib, sp, true
		}
	}
	if n.text != "" {
		if ib, item := findPart(n); ib != nil {
			return ib, qui.SourcePos{Item: item, Off: 0}, true
		}
	}
	return nil, qui.SourcePos{}, false
}

func posBefore(c *El, depth int) (*widgets.InlineBox, qui.SourcePos, bool) {
	if c.isTextSeg() {
		return domToSourceDepth(Pos{Node: c, Off: 0}, depth)
	}
	if ib, item := findPart(c); ib != nil {
		return ib, qui.SourcePos{Item: item, Off: 0}, true
	}
	if isAtomic(c) || isHidden(c) {
		return nil, qui.SourcePos{}, false
	}
	return domToSourceDepth(Pos{Node: c, Off: 0}, depth)
}

func posAfter(c *El, depth int) (*widgets.InlineBox, qui.SourcePos, bool) {
	if c.isTextSeg() {
		return domToSourceDepth(Pos{Node: c, Off: runeLen(c.text)}, depth)
	}
	if ib, item := findPart(c); ib != nil {
		return ib, qui.SourcePos{Item: item, Off: 1}, true
	}
	if isAtomic(c) || isHidden(c) || isBlockEl(c) {
		return nil, qui.SourcePos{}, false
	}
	return domToSourceDepth(Pos{Node: c, Off: len(c.elementKids)}, depth)
}

// sourceToDOM maps a run position back to a DOM position.
func sourceToDOM(ib *widgets.InlineBox, sp qui.SourcePos) (Pos, bool) {
	if p, ok := tagPos(ib, sp.Item, sp.Off); ok {
		return p, true
	}
	n := ib.PartCount()
	for i := sp.Item + 1; i < n; i++ {
		if p, ok := tagPos(ib, i, 0); ok {
			return p, true
		}
	}
	for i := min(sp.Item-1, n-1); i >= 0; i-- {
		if t, ok := ib.PartTag(i).(partTag); ok {
			end := 1
			if t.kind == tagText {
				end = runeLen(ib.PartText(i))
			}
			if p, ok := tagPos(ib, i, end); ok {
				return p, true
			}
		}
	}
	return Pos{}, false
}

func tagPos(ib *widgets.InlineBox, item, off int) (Pos, bool) {
	t, ok := ib.PartTag(item).(partTag)
	if !ok || t.el == nil {
		return Pos{}, false
	}
	switch t.kind {
	case tagText:
		if !t.el.isTextSeg() {
			return Pos{Node: t.el, Off: 0}, true
		}
		_, i2s := alignMap([]rune(t.el.text), []rune(ib.PartText(item)))
		return Pos{Node: t.el, Off: i2s[min(max(off, 0), len(i2s)-1)]}, true
	default:
		p := t.el.elParent
		if p == nil {
			return Pos{}, false
		}
		i := childIndex(p, t.el)
		if off > 0 {
			i++
		}
		return Pos{Node: p, Off: i}, true
	}
}

// flushLayout brings styles and layout up to date before geometry is read.
func (st *editState) flushLayout(host *El) {
	st.eng.FlushPendingStyles()
	if w := host.Window(); w != nil {
		w.FlushLayout()
	}
}

// caretRectWin returns the caret rectangle for p in window coordinates.
func (st *editState) caretRectWin(p Pos) (qui.Rect, bool) {
	if ib, sp, ok := domToSource(p); ok {
		if r, ok := ib.CaretRectAt(sp, -1); ok {
			return qui.InteractionRectFor(ib, r), true
		}
	}
	// Nothing rendered there (an empty element): the content box's start.
	n := p.Node
	if n == nil {
		return qui.Rect{}, false
	}
	if n.isTextSeg() {
		n = n.elParent
	}
	if n == nil {
		return qui.Rect{}, false
	}
	b := n.Bounds()
	in := n.Style().ContentInsets()
	fs := float32(16)
	lh := float32(0)
	if n.lastCS != nil {
		fs = n.lastCS.FontSize
		lh = n.lastCS.LineHeight
	}
	h := fs * 1.15
	y := b.Y + in.Top
	if lh > 0 && lh*fs > h {
		y += (lh*fs - h) / 2
	}
	r := qui.Rect{X: b.X + in.Left, Y: y, H: h}
	return qui.InteractionRectFor(n, r), true
}

// runsIn lists every inline run inside host, in document order.
func runsIn(host *El) []*widgets.InlineBox {
	var out []*widgets.InlineBox
	var walk func(e *El)
	walk = func(e *El) {
		if e.isTextSeg() {
			return
		}
		out = append(out, e.liveRuns()...)
		for _, k := range e.elementKids {
			if ke, ok := k.(*El); ok && !ke.isTextSeg() {
				walk(ke)
			}
		}
	}
	walk(host)
	return out
}

// ownerOf returns the element whose run ib is.
func ownerOf(ib *widgets.InlineBox) *El {
	for w := ib.Parent(); w != nil; w = w.Parent() {
		if e, ok := w.(*El); ok {
			return e
		}
	}
	return nil
}

// hitTest maps a window point to the nearest caret position inside host.
func (st *editState) hitTest(host *El, pt qui.Point) Pos {
	var target *widgets.InlineBox
	if win := host.Window(); win != nil {
		for w := win.WidgetAt(pt); w != nil; w = w.Parent() {
			if ib, ok := w.(*widgets.InlineBox); ok {
				if o := ownerOf(ib); o != nil && elContains(host, o) {
					target = ib
				}
				break
			}
			if w == qui.Widget(host) {
				break
			}
		}
	}
	if target == nil {
		best := float32(math.MaxFloat32)
		for _, ib := range runsIn(host) {
			r := qui.InteractionBoundsOf(ib)
			if r.W <= 0 && r.H <= 0 {
				continue
			}
			dy := float32(0)
			if pt.Y < r.Y {
				dy = r.Y - pt.Y
			} else if pt.Y > r.Y+r.H {
				dy = pt.Y - (r.Y + r.H)
			}
			dx := float32(0)
			if pt.X < r.X {
				dx = r.X - pt.X
			} else if pt.X > r.X+r.W {
				dx = pt.X - (r.X + r.W)
			}
			if d := dy*1000 + dx; d < best {
				best, target = d, ib
			}
		}
		// An empty block nearer than any run (an empty paragraph).
		if eb := emptyBlockAt(host, pt); eb != nil {
			r := qui.InteractionBoundsOf(eb)
			if target == nil || pt.Y >= r.Y && pt.Y <= r.Y+r.H {
				return Pos{Node: eb, Off: 0}
			}
		}
	}
	if target == nil {
		return Pos{Node: host, Off: 0}
	}
	lp := qui.WindowPointToLocal(target, pt)
	sp, _, ok := target.SourcePosAtPoint(lp)
	if !ok {
		return Pos{Node: host, Off: 0}
	}
	if p, ok := sourceToDOM(target, sp); ok && elContains(host, p.Node) {
		return p
	}
	return Pos{Node: host, Off: 0}
}

// emptyBlockAt returns a block inside host with no rendered runs whose box
// contains pt vertically.
func emptyBlockAt(host *El, pt qui.Point) *El {
	var found *El
	var walk func(e *El)
	walk = func(e *El) {
		for _, k := range e.elementKids {
			c, ok := k.(*El)
			if !ok || c.isTextSeg() || isHidden(c) {
				continue
			}
			if isBlockEl(c) && len(runsIn(c)) == 0 {
				r := qui.InteractionBoundsOf(c)
				if pt.Y >= r.Y && pt.Y <= r.Y+r.H {
					found = c
				}
				continue
			}
			walk(c)
		}
	}
	walk(host)
	return found
}

// lineEdge returns the start (end=false) or end of the visual line holding
// p, or ok=false when p is not on a rendered line.
func (st *editState) lineEdge(p Pos, end bool) (Pos, bool) {
	ib, sp, ok := domToSource(p)
	if !ok {
		return Pos{}, false
	}
	l, ok := ib.EditLayout()
	if !ok {
		return Pos{}, false
	}
	li := l.LineForSource(sp)
	e, ok := ib.LineEdgePos(li, end)
	if !ok {
		return Pos{}, false
	}
	return sourceToDOM(ib, e)
}

// --- painting ---

// queueHighlight re-applies the selection highlight after the next layout
// (a restyle rebuilds the runs, dropping their highlight).
func (st *editState) queueHighlight() {
	if st.syncQueued {
		return
	}
	var win *qui.Window
	if st.eng.root != nil {
		win = st.eng.root.Window()
	}
	if win == nil {
		st.syncHighlight()
		return
	}
	st.syncQueued = true
	win.AfterLayout(func() {
		st.syncQueued = false
		st.syncHighlight()
	})
	win.Invalidate()
}

// syncHighlight paints the selection into the runs it covers.
func (st *editState) syncHighlight() {
	for _, ib := range st.highlighted {
		ib.ClearTextSelection()
	}
	st.highlighted = st.highlighted[:0]
	if !st.hasSel || st.collapsed() || !st.valid(st.anchor) || !st.valid(st.focus) {
		return
	}
	a, b := st.ordered()
	host := st.selHost()
	if host == nil {
		return
	}
	for _, ib := range runsIn(host) {
		var first, last qui.SourcePos
		found := false
		for i, n := 0, ib.PartCount(); i < n; i++ {
			t, ok := ib.PartTag(i).(partTag)
			if !ok || t.el == nil {
				continue
			}
			s, ok1 := tagPos(ib, i, 0)
			endOff := 1
			if t.kind == tagText {
				endOff = runeLen(ib.PartText(i))
			}
			e, ok2 := tagPos(ib, i, endOff)
			if !ok1 || !ok2 || comparePos(e, a) <= 0 || comparePos(s, b) >= 0 {
				continue
			}
			lo, hi := 0, endOff
			if t.kind == tagText && t.el.isTextSeg() {
				s2i, _ := alignMap([]rune(t.el.text), []rune(ib.PartText(i)))
				if a.Node == t.el {
					lo = s2i[min(a.Off, len(s2i)-1)]
				}
				if b.Node == t.el {
					hi = s2i[min(b.Off, len(s2i)-1)]
				}
			}
			if !found {
				first = qui.SourcePos{Item: i, Off: lo}
				found = true
			}
			last = qui.SourcePos{Item: i, Off: hi}
		}
		if found {
			ib.SetSourceSelection(first, last)
			st.highlighted = append(st.highlighted, ib)
		}
	}
}

// drawEditing paints the caret and the composition underline of the
// focused editing host e.
func (e *El) drawEditing(canvas qui.Canvas) {
	st := e.engine.edit
	if st == nil || !st.hasSel || !e.Focused() || st.selHost() != e {
		return
	}
	color := qui.Color{A: 1}
	if e.lastCS != nil {
		color = e.lastCS.Color
		if c, ok := e.lastCS.raw["caret-color"]; ok && c != "auto" && c != "" {
			if cc, ok := parseColor(c); ok {
				color = cc
			}
		}
	}
	toLocal := func(r qui.Rect) qui.Rect {
		p := qui.WindowPointToLocal(e, qui.Point{X: r.X, Y: r.Y})
		return qui.Rect{X: p.X, Y: p.Y, W: r.W, H: r.H}
	}
	if c := st.comp; c != nil && c.host == e && c.seg.elParent != nil {
		r1, ok1 := st.caretRectWin(Pos{Node: c.seg, Off: c.start})
		r2, ok2 := st.caretRectWin(Pos{Node: c.seg, Off: c.start + c.n})
		if ok1 && ok2 && c.n > 0 {
			a, b := toLocal(r1), toLocal(r2)
			if a.Y == b.Y {
				canvas.FillRect(qui.Rect{X: a.X, Y: a.Y + a.H, W: b.X - a.X, H: 1}, color)
			} else {
				bb := e.Bounds()
				canvas.FillRect(qui.Rect{X: a.X, Y: a.Y + a.H, W: bb.X + bb.W - a.X, H: 1}, color)
				canvas.FillRect(qui.Rect{X: bb.X, Y: b.Y + b.H, W: b.X - bb.X, H: 1}, color)
			}
		}
	}
	if !st.collapsed() {
		return
	}
	r, ok := st.caretRectWin(st.focus)
	if !ok {
		return
	}
	l := toLocal(r)
	canvas.FillRect(qui.Rect{X: l.X, Y: l.Y, W: 1, H: l.H}, color)
}
