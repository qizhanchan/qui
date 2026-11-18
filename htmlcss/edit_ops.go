package htmlcss

import (
	"strings"
	"unicode/utf8"
)

// The editing operations. Each runs inside a user edit (userEdit), which
// fires beforeinput / input around it and records its DOM mutations as
// one undo step. They work on DOM positions and leave the caret in the
// selection.

// userEdit runs one edit of host: beforeinput (cancelable), the mutation,
// input. Reports whether it ran.
func (st *editState) userEdit(host *El, inputType, data, undoKind string, cancelable bool, fn func()) bool {
	if host == nil {
		return false
	}
	if st.hooks.BeforeInput != nil && !st.hooks.BeforeInput(host, inputType, data) && cancelable {
		return false
	}
	st.prepare(host)
	st.begin(undoKind)
	fn()
	st.commit()
	st.afterEdit(host)
	if st.hooks.Input != nil {
		st.hooks.Input(host, inputType, data)
	}
	return true
}

// prepare readies host for an edit: own texts become text nodes and the
// selection lies inside host.
func (st *editState) prepare(host *El) {
	st.materialize(host)
	if !st.hasSel || !st.valid(st.anchor) || !st.valid(st.focus) ||
		!elContains(host, st.anchor.Node) || !elContains(host, st.focus.Node) {
		fd := st.flatten(host)
		st.collapse(fd.pos[len(fd.pos)-1])
		return
	}
	st.anchor, st.focus = st.fixPos(st.anchor), st.fixPos(st.focus)
}

// fixPos moves an element position that sits next to a text node into
// that text node (own text materialized away may have shifted indices).
func (st *editState) fixPos(p Pos) Pos {
	p = clampPos(p)
	return p
}

func (st *editState) afterEdit(host *El) {
	host.Invalidate()
	st.haveGoal = false
	st.queueHighlight()
	if st.hooks.SelectionChange != nil {
		st.hooks.SelectionChange()
	}
}

// deleteSelection removes the selected content (no-op when collapsed).
func (st *editState) deleteSelection(host *El) {
	if st.collapsed() {
		return
	}
	a, b := st.ordered()
	st.deleteRange(host, a, b)
}

// deleteRange removes the content between a and b and leaves the caret
// at a. Units are removed back to front so earlier positions stay valid.
func (st *editState) deleteRange(host *El, a, b Pos) {
	fd := st.flatten(host)
	i, j := fd.index(a), fd.index(b)
	if comparePos(fd.pos[j], b) < 0 && j < len(fd.text) && fd.src[j].kind == flatText && fd.src[j].node == b.Node {
		// b lies inside a collapsed whitespace run: the run goes too.
		j++
	}
	if i >= j {
		st.collapse(a)
		return
	}
	caret := fd.pos[i]
	for k := j - 1; k >= i; k-- {
		s := fd.src[k]
		switch s.kind {
		case flatText:
			lo := k
			for lo > i && fd.src[lo-1].kind == flatText && fd.src[lo-1].node == s.node {
				lo--
			}
			start, end := fd.src[lo].start, s.end
			if lo == i && a.Node == s.node && a.Off > start && a.Off < end {
				start = a.Off
			}
			if k == j-1 && b.Node == s.node && b.Off < end && b.Off > start {
				end = b.Off
			}
			r := []rune(s.node.text)
			st.setText(s.node, string(r[:start])+string(r[min(end, len(r)):]))
			k = lo
		case flatBr, flatAtom:
			st.remove(s.node)
		case flatBlock:
			st.mergeBlocks(host, fd.pos[k], fd.pos[k+1])
		}
	}
	if !st.valid(caret) {
		caret = a
		if !st.valid(caret) {
			caret = Pos{Node: host, Off: 0}
		}
	}
	caret = clampPos(caret)
	st.collapse(caret)
	st.tidy(host)
}

// mergeBlocks joins the line ending at left with the one starting at
// right (deleting the line break between two blocks).
func (st *editState) mergeBlocks(host *El, left, right Pos) {
	A := blockOf(left.Node, host)
	B := blockOf(right.Node, host)
	if A == B || B == nil {
		return
	}
	if elContains(A, B) {
		// B sits inside A: unwrap it (an empty line's placeholder goes).
		par := B.elParent
		if len(B.elementKids) == 1 {
			if br := kidEl(B, 0); br != nil && br.tag == "br" {
				st.remove(B)
				return
			}
		}
		for _, k := range B.Kids() {
			if c, ok := k.(*El); ok {
				st.insert(par, c, B)
			}
		}
		st.remove(B)
		return
	}
	// The inline run of B starting at right moves to A's end.
	var start *El
	if right.Node == B {
		start = kidEl(B, right.Off)
	} else {
		start = right.Node
		for start != nil && start.elParent != B {
			start = start.elParent
		}
	}
	var run []*El
	for i := childIndex(B, start); i >= 0 && i < len(B.elementKids); i++ {
		c := kidEl(B, i)
		if c == nil {
			continue
		}
		if isBlockEl(c) {
			break
		}
		run = append(run, c)
	}
	if last := kidEl(A, len(A.elementKids)-1); last != nil && last.tag == "br" {
		st.remove(last)
	}
	if len(run) == 1 && run[0].tag == "br" {
		st.remove(run[0]) // the empty line's placeholder
	} else {
		for _, c := range run {
			st.insert(A, c, nil)
		}
	}
	if !elContains(B, A) && B != host && !hasContent(B) {
		st.remove(B)
	}
}

// hasContent reports whether e holds text, a <br> or an atom.
func hasContent(e *El) bool {
	for _, k := range e.elementKids {
		c, ok := k.(*El)
		if !ok {
			return true
		}
		switch {
		case c.isTextSeg():
			if c.text != "" {
				return true
			}
		case c.tag == "#comment":
		case c.tag == "br" || isAtomic(c):
			return true
		case hasContent(c):
			return true
		}
	}
	return e.text != ""
}

// tidy drops empty text nodes and empty inline elements left by an edit
// (keeping the caret's node and its ancestors) and gives a block left
// empty its <br> placeholder.
func (st *editState) tidy(host *El) {
	keep := map[*El]bool{}
	for x := st.focus.Node; x != nil; x = x.elParent {
		keep[x] = true
	}
	for x := st.anchor.Node; x != nil; x = x.elParent {
		keep[x] = true
	}
	var walk func(e *El)
	walk = func(e *El) {
		for _, k := range e.Kids() {
			c, ok := k.(*El)
			if !ok {
				continue
			}
			switch {
			case c.isTextSeg():
				if c.text == "" && !keep[c] {
					st.remove(c)
				}
			case c.tag == "#comment" || c.tag == "br" || isAtomic(c) || isHidden(c):
			case isBlockEl(c):
				walk(c)
			default:
				walk(c)
				if !keep[c] && len(c.elementKids) == 0 && c.text == "" {
					st.remove(c)
				}
			}
		}
	}
	walk(host)
	if b := blockOf(st.focus.Node, host); b != host && !hasContent(b) {
		br := st.eng.NewEl("br")
		st.insert(b, br, nil)
		st.collapse(Pos{Node: b, Off: childIndex(b, br)})
	}
}

// preserveWS reports whether whitespace is kept verbatim at n.
func preserveWS(n *El) bool {
	for x := n; x != nil; x = x.elParent {
		if x.lastCS != nil {
			return x.lastCS.PreserveWS
		}
	}
	return false
}

// textAt returns a text node at the caret to insert into, creating one
// when the caret sits between elements: (seg, offset).
func (st *editState) textAt(p Pos) (*El, int) {
	if p.Node.isTextSeg() {
		return p.Node, p.Off
	}
	n := p.Node
	// A block holding only its placeholder <br>: the text replaces it.
	if len(n.elementKids) == 1 && p.Off <= 1 {
		if br := kidEl(n, 0); br != nil && br.tag == "br" {
			st.remove(br)
			p.Off = 0
		}
	}
	// A trailing placeholder <br> right after the caret is not needed
	// once the line has text.
	if next := kidEl(n, p.Off); next != nil && next.tag == "br" && !contentAfter(next, blockOf(next, nil)) {
		if prev := kidEl(n, p.Off-1); prev == nil || prev.tag == "br" || prev.isTextSeg() {
			st.remove(next)
		}
	}
	if prev := kidEl(n, p.Off-1); prev != nil && prev.isTextSeg() {
		return prev, runeLen(prev.text)
	}
	if next := kidEl(n, p.Off); next != nil && next.isTextSeg() {
		return next, 0
	}
	seg := st.eng.NewTextEl("")
	st.insert(n, seg, kidEl(n, p.Off))
	return seg, 0
}

// insertText inserts s at the caret (replacing the selection).
func (st *editState) insertText(host *El, s string) {
	st.deleteSelection(host)
	p := st.focus
	if len(st.typingStyle) > 0 && st.typingAt == p {
		tags := st.typingStyle
		st.typingStyle = nil
		par, idx := st.splitText(p)
		var outer, inner *El
		for _, t := range tags {
			w := st.eng.NewEl(t)
			if inner == nil {
				outer = w
			} else {
				st.dom().Insert(inner, w, nil)
			}
			inner = w
		}
		st.insert(par, outer, kidEl(par, idx))
		p = Pos{Node: inner, Off: 0}
	}
	seg, off := st.textAt(p)
	r := []rune(seg.text)
	text := string(r[:off]) + s + string(r[off:])
	end := off + runeLen(s)
	if !preserveWS(seg.elParent) {
		text = fixSpaces(text, off, end, neighborRune(seg, -1), neighborRune(seg, 1))
	}
	st.setText(seg, text)
	st.collapse(Pos{Node: seg, Off: end})
}

// fixSpaces makes the spaces of the run around [from, to) visible the way
// Chrome does: a space at a text node's edge or next to another space is
// a no-break space, the others plain spaces.
func fixSpaces(s string, from, to int, before, after rune) string {
	r := []rune(s)
	isSp := func(c rune) bool { return c == ' ' || c == ' ' }
	lo, hi := from, to
	for lo > 0 && isSp(r[lo-1]) {
		lo--
	}
	for hi < len(r) && isSp(r[hi]) {
		hi++
	}
	for k := lo; k < hi; k++ {
		if !isSp(r[k]) {
			continue
		}
		prevSp := k == 0 && (before == 0 || isSp(before) || before == '\n') || k > 0 && isSp(r[k-1])
		atEnd := k == len(r)-1 && (after == 0 || isSp(after) || after == '\n')
		if prevSp || atEnd {
			r[k] = ' '
		} else {
			r[k] = ' '
		}
	}
	return string(r)
}

// neighborRune returns the character rendered just before (dir < 0) or
// after seg on its line: 0 at a block edge, '\n' at a <br>.
func neighborRune(seg *El, dir int) rune {
	for x := seg; x != nil && x.elParent != nil; x = x.elParent {
		par := x.elParent
		for i := childIndex(par, x) + dir; i >= 0 && i < len(par.elementKids); i += dir {
			c := kidEl(par, i)
			if c == nil || c.tag == "#comment" || isHidden(c) {
				continue
			}
			if r, ok := edgeRune(c, dir); ok {
				return r
			}
		}
		if isBlockEl(par) {
			return 0
		}
	}
	return 0
}

// edgeRune is the first (dir > 0) or last character e renders; ok is
// false when it renders nothing.
func edgeRune(e *El, dir int) (rune, bool) {
	switch {
	case e.isTextSeg():
		r := []rune(e.text)
		if len(r) == 0 {
			return 0, false
		}
		if dir > 0 {
			return r[0], true
		}
		return r[len(r)-1], true
	case e.tag == "br":
		return '\n', true
	case isBlockEl(e):
		return 0, true
	case isAtomic(e):
		return '\uFFFC', true
	}
	n := len(e.elementKids)
	for k := 0; k < n; k++ {
		i := k
		if dir < 0 {
			i = n - 1 - k
		}
		if c := kidEl(e, i); c != nil {
			if r, ok := edgeRune(c, dir); ok {
				return r, true
			}
		}
	}
	return 0, false
}

// splitText splits the text node at p (when p is inside one) and returns
// the element boundary there: (parent, index).
func (st *editState) splitText(p Pos) (*El, int) {
	if !p.Node.isTextSeg() {
		return p.Node, p.Off
	}
	seg := p.Node
	par := seg.elParent
	i := childIndex(par, seg)
	r := []rune(seg.text)
	switch {
	case p.Off <= 0:
		return par, i
	case p.Off >= len(r):
		return par, i + 1
	}
	rest := st.eng.NewTextEl(string(r[p.Off:]))
	st.setText(seg, string(r[:p.Off]))
	st.insert(par, rest, kidEl(par, i+1))
	return par, i + 1
}

// splitTo splits the inline ancestors of p up to top, returning the index
// in top where the second half starts.
func (st *editState) splitTo(p Pos, top *El) int {
	n, i := st.splitText(p)
	for n != top && n != nil && n.elParent != nil {
		par := n.elParent
		ni := childIndex(par, n)
		switch {
		case i <= 0:
			n, i = par, ni
		case i >= len(n.elementKids):
			n, i = par, ni+1
		default:
			c := st.cloneShallow(n)
			for _, k := range n.Kids()[i:] {
				if ke, ok := k.(*El); ok {
					st.insert(c, ke, nil)
				}
			}
			st.insert(par, c, kidEl(par, ni+1))
			n, i = par, ni+1
		}
	}
	return i
}

// cloneShallow copies an element's tag and attributes (not its id).
func (st *editState) cloneShallow(e *El) *El {
	c := st.eng.NewEl(e.tag)
	for _, k := range e.AttrNames() {
		if k == "id" {
			continue
		}
		v, _ := e.Attr(k)
		c.SetAttr(k, v)
	}
	return c
}

// firstPos is the first caret position inside e.
func firstPos(e *El) Pos {
	for {
		c := kidEl(e, 0)
		if c == nil || c.tag == "br" || isAtomic(c) || c.tag == "#comment" {
			return Pos{Node: e, Off: 0}
		}
		if c.isTextSeg() {
			return Pos{Node: c, Off: 0}
		}
		e = c
	}
}

var paragraphTags = map[string]bool{"div": true, "p": true, "li": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "blockquote": true, "pre": true, "dd": true, "dt": true}

// insertParagraph splits the block at the caret (Enter).
func (st *editState) insertParagraph(host *El) {
	st.deleteSelection(host)
	p := st.focus
	B := blockOf(p.Node, host)
	if B != host && !paragraphTags[B.tag] {
		B = host
	}
	if B == host {
		// Inline content directly in the host: the part after the caret
		// moves into a new <div> (Chrome's default paragraph separator).
		k := st.splitTo(p, host)
		// The run before the caret: back to the previous block.
		runStart := k
		for runStart > 0 {
			c := kidEl(host, runStart-1)
			if c != nil && isBlockEl(c) {
				break
			}
			runStart--
		}
		div := st.eng.NewEl("div")
		var move []*El
		for i := k; i < len(host.elementKids); i++ {
			c := kidEl(host, i)
			if c == nil {
				continue
			}
			if isBlockEl(c) {
				break
			}
			move = append(move, c)
		}
		ref := kidEl(host, k+len(move))
		before := false
		for i := runStart; i < k; i++ {
			if c := kidEl(host, i); c != nil && (!c.isTextSeg() || strings.TrimSpace(c.text) != "") {
				before = true
			}
		}
		if !before {
			// Nothing before the caret on this line: it becomes an empty
			// paragraph of its own.
			empty := st.eng.NewEl("div")
			st.insert(host, empty, kidEl(host, k))
			st.insert(empty, st.eng.NewEl("br"), nil)
		}
		st.insert(host, div, ref)
		for _, c := range move {
			st.insert(div, c, nil)
		}
		if !hasContent(div) {
			st.insert(div, st.eng.NewEl("br"), nil)
		}
		st.collapse(firstPos(div))
		st.tidy(host)
		return
	}
	k := st.splitTo(p, B)
	tag := B.tag
	if strings.HasPrefix(tag, "h") && len(tag) == 2 && k >= len(B.elementKids) {
		tag = "div"
	}
	nb := st.eng.NewEl(tag)
	if tag == B.tag {
		nb = st.cloneShallow(B)
	}
	var move []*El
	for _, kd := range B.Kids()[k:] {
		if c, ok := kd.(*El); ok {
			move = append(move, c)
		}
	}
	st.insert(B.elParent, nb, kidEl(B.elParent, childIndex(B.elParent, B)+1))
	for _, c := range move {
		st.insert(nb, c, nil)
	}
	if !hasContent(B) {
		st.insert(B, st.eng.NewEl("br"), nil)
	}
	if !hasContent(nb) {
		st.insert(nb, st.eng.NewEl("br"), nil)
	}
	st.collapse(firstPos(nb))
	st.tidy(host)
}

// insertLineBreak inserts a <br> at the caret (Shift+Enter).
func (st *editState) insertLineBreak(host *El) {
	st.deleteSelection(host)
	par, idx := st.splitText(st.focus)
	br := st.eng.NewEl("br")
	st.insert(par, br, kidEl(par, idx))
	after := Pos{Node: par, Off: childIndex(par, br) + 1}
	if !contentAfter(br, blockOf(br, host)) {
		// The new line is empty: it needs a placeholder to show.
		st.insert(par, st.eng.NewEl("br"), nil)
	}
	if next := kidEl(par, after.Off); next != nil && next.isTextSeg() {
		after = Pos{Node: next, Off: 0}
	}
	st.collapse(after)
}

// contentAfter reports whether anything renders after n inside block.
func contentAfter(n, block *El) bool {
	for x := n; x != nil && x != block; x = x.elParent {
		par := x.elParent
		if par == nil {
			return false
		}
		for i := childIndex(par, x) + 1; i < len(par.elementKids); i++ {
			c := kidEl(par, i)
			if c == nil {
				return true
			}
			if c.isTextSeg() && strings.TrimSpace(c.text) != "" || !c.isTextSeg() && (c.tag == "br" || isAtomic(c) || hasContent(c)) {
				return true
			}
		}
	}
	return false
}

// insertPlain inserts text that may hold newlines (paste): each newline
// starts a new paragraph.
func (st *editState) insertPlain(host *El, s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if i > 0 {
			if host.plaintextOnly() || preserveWS(host) {
				st.insertLineBreak(host)
			} else {
				st.insertParagraph(host)
			}
		}
		if ln != "" {
			st.insertText(host, ln)
		} else {
			st.deleteSelection(host)
		}
	}
}

// insertHTML inserts parsed markup at the caret.
func (st *editState) insertHTML(host *El, html string) {
	st.deleteSelection(host)
	nodes := st.eng.ParseFragment(html)
	par, idx := st.splitText(st.focus)
	ref := kidEl(par, idx)
	var last *El
	for _, w := range nodes {
		if e, ok := w.(*El); ok {
			st.insert(par, e, ref)
			last = e
		}
	}
	st.materialize(host)
	if last != nil {
		if last.isTextSeg() {
			st.collapse(Pos{Node: last, Off: runeLen(last.text)})
		} else {
			st.collapse(Pos{Node: par, Off: childIndex(par, last) + 1})
		}
	}
}

// formatTags lists the elements that carry each format.
var formatTags = map[string][]string{
	"b": {"b", "strong"}, "i": {"i", "em"}, "u": {"u", "ins"}, "s": {"s", "strike", "del"},
}

func hasFormat(n, host *El, tag string) bool {
	for x := n; x != nil && x != host.elParent; x = x.elParent {
		for _, t := range formatTags[tag] {
			if x.tag == t {
				return true
			}
		}
	}
	return false
}

// formatState reports whether the selection start carries the format.
func (st *editState) formatState(host *El, tag string) bool {
	if !st.hasSel {
		return false
	}
	for _, t := range st.typingStyle {
		if t == tag {
			return true
		}
	}
	a, _ := st.ordered()
	return hasFormat(a.Node, host, tag)
}

// segsIn lists the text nodes the range [a, b) covers, in order.
func segsIn(host *El, a, b Pos) []*El {
	var out []*El
	var walk func(e *El)
	walk = func(e *El) {
		for _, k := range e.elementKids {
			c, ok := k.(*El)
			if !ok {
				continue
			}
			if c.isTextSeg() {
				if c.text != "" && comparePos(Pos{Node: c, Off: runeLen(c.text)}, a) > 0 && comparePos(Pos{Node: c, Off: 0}, b) < 0 {
					out = append(out, c)
				}
				continue
			}
			walk(c)
		}
	}
	walk(host)
	return out
}

// toggleFormat applies or removes a format (b / i / u / s) on the
// selection.
func (st *editState) toggleFormat(host *El, tag string) {
	on := st.formatState(host, tag)
	if st.collapsed() {
		if on {
			st.typingStyle = nil
		} else {
			st.typingStyle = append(st.typingStyle, tag)
		}
		st.typingAt = st.focus
		return
	}
	a, b := st.ordered()
	segs := segsIn(host, a, b)
	var mids []*El
	// Isolate the selected part of each text node.
	for _, seg := range segs {
		lo, hi := 0, runeLen(seg.text)
		if seg == a.Node {
			lo = a.Off
		}
		if seg == b.Node {
			hi = b.Off
		}
		if lo >= hi {
			continue
		}
		mid := seg
		if hi < runeLen(seg.text) {
			st.splitText(Pos{Node: seg, Off: hi})
		}
		if lo > 0 {
			par, idx := st.splitText(Pos{Node: seg, Off: lo})
			mid = kidEl(par, idx)
		}
		mids = append(mids, mid)
	}
	for _, mid := range mids {
		if on {
			st.unformat(host, mid, tag)
		} else if !hasFormat(mid, host, tag) {
			w := st.eng.NewEl(tag)
			par := mid.elParent
			st.insert(par, w, mid)
			st.insert(w, mid, nil)
		}
	}
	if len(mids) > 0 {
		last := mids[len(mids)-1]
		st.setSel(Pos{Node: mids[0], Off: 0}, Pos{Node: last, Off: runeLen(last.text)})
	}
}

// unformat removes the format from text node mid: its formatting
// ancestor is split around it and the part holding it unwrapped.
func (st *editState) unformat(host, mid *El, tag string) {
	for {
		var f *El
		for x := mid.elParent; x != nil && x != host; x = x.elParent {
			for _, t := range formatTags[tag] {
				if x.tag == t {
					f = x
				}
			}
			if f != nil {
				break
			}
		}
		if f == nil {
			return
		}
		top := f.elParent
		k2 := st.splitTo(Pos{Node: mid, Off: runeLen(mid.text)}, top)
		k1 := st.splitTo(Pos{Node: mid, Off: 0}, top)
		_ = k2
		for i := k1; i < len(top.elementKids); i++ {
			c := kidEl(top, i)
			if c == nil || !elContains(c, mid) {
				continue
			}
			isF := false
			for _, t := range formatTags[tag] {
				if c.tag == t {
					isF = true
				}
			}
			if isF {
				for _, k := range c.Kids() {
					if ke, ok := k.(*El); ok {
						st.insert(top, ke, c)
					}
				}
				st.remove(c)
			}
			break
		}
	}
}

// selectAll selects the host's whole content.
func (st *editState) selectAll(host *El) {
	st.materialize(host)
	fd := st.flatten(host)
	st.setSel(fd.pos[0], fd.pos[len(fd.pos)-1])
}

// undoStep / redoStep replay the history.
func (st *editState) undoStep(host *El) bool {
	if len(st.undo) == 0 {
		return false
	}
	t := st.undo[len(st.undo)-1]
	ok := false
	st.userEdit(host, "historyUndo", "", "", true, func() {
		st.txn = nil
		st.undo = st.undo[:len(st.undo)-1]
		if st.replay(t, true) {
			st.redo = append(st.redo, t)
			ok = true
		} else {
			st.undo, st.redo = nil, nil
		}
	})
	return ok
}

func (st *editState) redoStep(host *El) bool {
	if len(st.redo) == 0 {
		return false
	}
	t := st.redo[len(st.redo)-1]
	ok := false
	st.userEdit(host, "historyRedo", "", "", true, func() {
		st.txn = nil
		st.redo = st.redo[:len(st.redo)-1]
		if st.replay(t, false) {
			st.undo = append(st.undo, t)
			ok = true
		} else {
			st.undo, st.redo = nil, nil
		}
	})
	return ok
}

// --- IME composition ---

func (st *editState) setPreedit(host *El, text string, cursor int) {
	c := st.comp
	if c != nil && (c.seg.elParent == nil || c.host != host) {
		st.comp, c = nil, nil
	}
	if text == "" {
		if c == nil {
			return
		}
		st.replaceComposition("")
		st.comp = nil
		st.collapse(Pos{Node: c.seg, Off: c.start})
		if st.hooks.Composition != nil {
			st.hooks.Composition(host, "compositionend", "")
		}
		host.Invalidate()
		return
	}
	if c == nil {
		st.prepare(host)
		st.begin("delete")
		st.deleteSelection(host)
		seg, off := st.textAt(st.focus)
		st.commit()
		st.comp = &composition{host: host, seg: seg, start: off, before: seg.text}
		if st.hooks.Composition != nil {
			st.hooks.Composition(host, "compositionstart", "")
		}
	}
	if st.hooks.BeforeInput != nil {
		st.hooks.BeforeInput(host, "insertCompositionText", text) // not cancelable
	}
	if st.comp == nil {
		return
	}
	st.replaceComposition(text)
	cur := min(max(cursor, 0), runeLen(text))
	st.collapse(Pos{Node: st.comp.seg, Off: st.comp.start + cur})
	if st.hooks.Composition != nil {
		st.hooks.Composition(host, "compositionupdate", text)
	}
	host.Invalidate()
	if st.hooks.Input != nil {
		st.hooks.Input(host, "insertCompositionText", text)
	}
}

// replaceComposition swaps the composing text for text (not recorded:
// the commit records the whole composition as one step).
func (st *editState) replaceComposition(text string) {
	c := st.comp
	r := []rune(c.seg.text)
	s := min(c.start, len(r))
	e := min(c.start+c.n, len(r))
	st.dom().SetText(c.seg, string(r[:s])+text+string(r[e:]))
	c.n = runeLen(text)
}

func (st *editState) commitIME(host *El, text string) {
	c := st.comp
	if c == nil || c.seg.elParent == nil || c.host != host {
		st.comp = nil
		st.userEdit(host, "insertText", text, "type", true, func() { st.insertText(host, text) })
		return
	}
	if st.hooks.BeforeInput != nil {
		st.hooks.BeforeInput(host, "insertCompositionText", text)
	}
	if st.comp == nil {
		return
	}
	selBefore := Pos{Node: c.seg, Off: c.start}
	st.replaceComposition(text)
	st.comp = nil
	after := c.seg.text
	if after != c.before {
		st.redo = nil
		st.undo = append(st.undo, &editTxn{kind: "ime",
			ops:       []editOp{{kind: opText, node: c.seg, old: c.before, new: after}},
			selBefore: [2]Pos{selBefore, selBefore},
			selAfter:  [2]Pos{{Node: c.seg, Off: c.start + c.n}, {Node: c.seg, Off: c.start + c.n}}})
	}
	st.collapse(Pos{Node: c.seg, Off: c.start + c.n})
	if st.hooks.Composition != nil {
		st.hooks.Composition(host, "compositionupdate", text)
	}
	st.afterEdit(host)
	if st.hooks.Input != nil {
		st.hooks.Input(host, "insertCompositionText", text)
	}
	if st.hooks.Composition != nil {
		st.hooks.Composition(host, "compositionend", text)
	}
}

var _ = utf8.RuneLen
