package htmlcss

import (
	"strings"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// contenteditable: rich-text editing of the live element tree.
//
// The DOM is the model. An element with `contenteditable` (true, "" or
// plaintext-only) — or every element of a document in design mode — is
// editable; the outermost editable element is its editing host, which takes
// focus, draws the caret and turns keys, characters and IME composition
// into DOM mutations: text node data, inserted <div> / <br> / <b> elements.
// Positions are DOM boundary points (Pos). Every mutation goes through the
// engine's EditDOM so an embedder (q-browser's script DOM) records it for
// mutation observers, and every user edit is wrapped in beforeinput /
// input notifications (EditHooks).
//
// Files: edit.go (model, state, hooks, linear text view), edit_ops.go (the
// editing operations + undo), edit_view.go (geometry: caret, hit testing,
// highlight), edit_input.go (keys, mouse, IME, execCommand).

// Pos is a DOM boundary point. Node is a text node (a #text segment; Off
// is a rune offset into its text) or an element (Off is an index into its
// Kids()).
type Pos struct {
	Node *El
	Off  int
}

// EditDOM performs the DOM mutations editing makes. The default works on
// the El tree directly; a script engine substitutes its own so the
// changes reach mutation observers and its node bookkeeping.
type EditDOM interface {
	// SetText replaces a text node's data.
	SetText(seg *El, text string)
	// Insert puts child into parent before ref (nil: at the end),
	// removing it from any previous parent first.
	Insert(parent, child, ref *El)
	// Remove detaches child from its parent.
	Remove(child *El)
	// OwnText turns an element's own text (an element whose only content
	// is text keeps it in a field, not a child) into a leading text node
	// and returns that node; nil when the element has no own text. Not a
	// DOM change — the node existed for scripts all along.
	OwnText(e *El) *El
}

// EditHooks connects an embedder to editing.
type EditHooks struct {
	// DOM performs the mutations (nil: the El tree directly).
	DOM EditDOM
	// BeforeInput runs before a user edit; false cancels it (the DOM's
	// beforeinput preventDefault). inputType uses the Input Events names
	// (insertText, deleteContentBackward, insertParagraph, …).
	BeforeInput func(host *El, inputType, data string) bool
	// Input runs after an edit changed the DOM.
	Input func(host *El, inputType, data string)
	// Composition reports IME composition: typ is compositionstart,
	// compositionupdate or compositionend.
	Composition func(host *El, typ, data string)
	// SelectionChange runs after the selection moved.
	SelectionChange func()
}

// SetEditHooks installs the engine's editing hooks.
func (eng *StyleEngine) SetEditHooks(h EditHooks) { eng.editing().hooks = h }

// SetDesignMode turns document.designMode on or off: every element of the
// document becomes editable, with <body> (or the root) as the host.
func (eng *StyleEngine) SetDesignMode(on bool) {
	st := eng.editing()
	if st.designMode == on {
		return
	}
	st.designMode = on
	if eng.root != nil {
		eng.root.node.designMode = on
		eng.Restyle()
	}
}

// DesignMode reports whether design mode is on.
func (eng *StyleEngine) DesignMode() bool { return eng.edit != nil && eng.edit.designMode }

// editState is one document's editing state: the selection, an IME
// composition, typing style and the undo history.
type editState struct {
	eng        *StyleEngine
	hooks      EditHooks
	designMode bool

	anchor, focus Pos
	hasSel        bool

	comp *composition
	// typingStyle: formatting tags (b / i / u) execCommand toggled on a
	// collapsed caret, applied to the next inserted text.
	typingStyle []string
	typingAt    Pos

	undo, redo []*editTxn
	txn        *editTxn // the transaction being recorded

	// goalX keeps the caret's column across vertical moves.
	goalX    float32
	haveGoal bool

	dragging   bool
	dragUnit   int // 1 char, 2 word, 3 paragraph
	dragOrigin [2]Pos

	highlighted []*widgets.InlineBox
	syncQueued  bool

	// commitBuf collects the characters an input method commits while a
	// composition is open (finished in one job).
	commitBuf    string
	commitQueued bool

	// Click counting for double / triple click.
	lastDown   time.Time
	lastDownPt qui.Point
	clickCount int
	// pressCanceled: the page prevented the coming mousedown's default.
	pressCanceled bool
}

// composition is an IME composition in progress: its text is live in the
// DOM between start and start+n of seg (Chrome's model).
type composition struct {
	host   *El
	seg    *El
	start  int
	n      int
	before string // seg's text when composition started
}

func (eng *StyleEngine) editing() *editState {
	if eng.edit == nil {
		eng.edit = &editState{eng: eng}
	}
	return eng.edit
}

func (st *editState) dom() EditDOM {
	if st.hooks.DOM != nil {
		return st.hooks.DOM
	}
	return rawDOM{}
}

// rawDOM edits the El tree directly.
type rawDOM struct{}

func (rawDOM) SetText(seg *El, s string) { seg.SetTextContent(s) }

func (rawDOM) Insert(parent, child, ref *El) {
	if child.elParent != nil {
		rawDOM{}.Remove(child)
	}
	kids := parent.Kids()
	out := make([]qui.Widget, 0, len(kids)+1)
	placed := false
	for _, k := range kids {
		if ref != nil && k == qui.Widget(ref) {
			out = append(out, child)
			placed = true
		}
		out = append(out, k)
	}
	if !placed {
		out = append(out, child)
	}
	parent.SetElementChildren(out)
}

func (rawDOM) Remove(child *El) {
	p := child.elParent
	if p == nil {
		return
	}
	kids := p.Kids()
	out := kids[:0]
	for _, k := range kids {
		if k != qui.Widget(child) {
			out = append(out, k)
		}
	}
	p.SetElementChildren(out)
	child.elParent = nil
}

func (rawDOM) OwnText(e *El) *El {
	if e.text == "" || e.isTextSeg() {
		return nil
	}
	seg := newTextEl(e.TextContent(), e.engine)
	e.SetTextContent("")
	e.SetElementChildren(append([]qui.Widget{seg}, e.Kids()...))
	return seg
}

// --- editability ---

// ceState reads an element's contenteditable attribute: 1 true, 2
// plaintext-only, -1 false, 0 absent (inherit).
func ceState(e *El) int {
	if e == nil || e.isTextSeg() {
		return 0
	}
	v, ok := e.readAttr("contenteditable")
	if !ok {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "true":
		return 1
	case "plaintext-only":
		return 2
	}
	return -1 // "false" and invalid values (invalid = inherit in the spec; false is safer)
}

// editableState returns 1 / 2 when e is editable (2: plaintext-only), 0
// otherwise.
func (e *El) editableState() int {
	for a := e; a != nil; a = a.elParent {
		switch s := ceState(a); s {
		case 1, 2:
			return s
		case -1:
			return 0
		}
		if a.elParent == nil && a.engine != nil && a.engine.DesignMode() && a == a.engine.root {
			return 1
		}
	}
	return 0
}

// IsContentEditable reports whether the element (or text node) is
// editable — the DOM's isContentEditable.
func (e *El) IsContentEditable() bool { return e != nil && e.isControlFree() && e.editableState() != 0 }

func (e *El) isControlFree() bool { return e.backing == nil }

// EditingHost returns the editing host of e: the outermost editable
// ancestor-or-self (in design mode, <body>). Nil when e is not editable.
func (e *El) EditingHost() *El {
	if e == nil || e.editableState() == 0 {
		return nil
	}
	var host *El
	for a := e; a != nil; a = a.elParent {
		if a.isTextSeg() {
			continue
		}
		if ceState(a) == -1 {
			break
		}
		if a.editableState() == 0 {
			break
		}
		host = a
	}
	if host != nil && host.elParent == nil && host.engine != nil && host.engine.DesignMode() {
		for _, k := range host.elementKids {
			if b, ok := k.(*El); ok && b.tag == "body" {
				return b
			}
		}
	}
	return host
}

// isEditingHost reports whether e is an editing host.
func (e *El) isEditingHost() bool {
	if e == nil || e.isTextSeg() || e.tag == "#comment" || e.backing != nil {
		return false
	}
	if s := ceState(e); s == 1 || s == 2 {
		p := e.elParent
		return p == nil || p.editableState() == 0
	}
	return e.engine != nil && e.engine.DesignMode() && e.EditingHost() == e
}

// plaintextOnly reports a plaintext-only host (no formatting).
func (e *El) plaintextOnly() bool { return e.editableState() == 2 }

// --- tree helpers ---

func childIndex(p, c *El) int {
	if p == nil {
		return -1
	}
	for i, k := range p.elementKids {
		if k == qui.Widget(c) {
			return i
		}
	}
	return -1
}

func kidEl(p *El, i int) *El {
	if p == nil || i < 0 || i >= len(p.elementKids) {
		return nil
	}
	e, _ := p.elementKids[i].(*El)
	return e
}

func runeLen(s string) int { return len([]rune(s)) }

// nodeLen is the DOM length of a node: runes of a text node, children of
// an element.
func nodeLen(n *El) int {
	if n.isTextSeg() {
		return runeLen(n.text)
	}
	return len(n.elementKids)
}

// contains reports whether a contains b (or is b).
func elContains(a, b *El) bool {
	for x := b; x != nil; x = x.elParent {
		if x == a {
			return true
		}
	}
	return false
}

// comparePos orders two boundary points in document order.
func comparePos(a, b Pos) int {
	if a.Node == b.Node {
		switch {
		case a.Off < b.Off:
			return -1
		case a.Off > b.Off:
			return 1
		}
		return 0
	}
	// a's node an ancestor of b's?
	if c := childOnPath(a.Node, b.Node); c != nil {
		if a.Off <= childIndex(a.Node, c) {
			return -1
		}
		return 1
	}
	if c := childOnPath(b.Node, a.Node); c != nil {
		if b.Off <= childIndex(b.Node, c) {
			return 1
		}
		return -1
	}
	// Common ancestor: compare the branches.
	depth := map[*El]*El{} // ancestor → its child on a's path
	prev := a.Node
	for x := a.Node.elParent; x != nil; x = x.elParent {
		depth[x] = prev
		prev = x
	}
	prev = b.Node
	for x := b.Node.elParent; x != nil; x = x.elParent {
		if ca, ok := depth[x]; ok {
			if childIndex(x, ca) < childIndex(x, prev) {
				return -1
			}
			return 1
		}
		prev = x
	}
	return 0
}

// childOnPath returns the child of anc on the path down to n (nil when
// anc is not a proper ancestor of n).
func childOnPath(anc, n *El) *El {
	for x := n; x != nil; x = x.elParent {
		if x.elParent == anc {
			return x
		}
	}
	return nil
}

func minPos(a, b Pos) Pos {
	if comparePos(a, b) <= 0 {
		return a
	}
	return b
}

func maxPos(a, b Pos) Pos {
	if comparePos(a, b) >= 0 {
		return a
	}
	return b
}

// isBlockEl reports a block-level element (it starts a new line).
func isBlockEl(e *El) bool {
	if e == nil || e.isTextSeg() {
		return false
	}
	if cs := e.lastCS; cs != nil && cs.Display != "" {
		d := cs.Display
		return !strings.HasPrefix(d, "inline") && d != "contents" && d != "none"
	}
	return blockTags[e.tag] || e.tag == "table" || e.tag == "tr"
}

func isHidden(e *El) bool {
	if e.tag == "#comment" || e.tag == "script" || e.tag == "style" || e.tag == "template" || e.tag == "head" {
		return true
	}
	return e.displayNone || (e.lastCS != nil && e.lastCS.Display == "none")
}

// isAtomic: an element that is one unit of content (a picture, a control,
// a non-editable island).
func isAtomic(e *El) bool {
	switch e.tag {
	case "img", "input", "button", "select", "textarea", "video", "audio", "canvas", "iframe", "svg", "hr", "object", "embed":
		return true
	}
	return ceState(e) == -1
}

// blockOf returns the nearest block-level ancestor-or-self of n within
// host (host itself at worst).
func blockOf(n, host *El) *El {
	for x := n; x != nil; x = x.elParent {
		if x == host {
			return host
		}
		if !x.isTextSeg() && isBlockEl(x) {
			return x
		}
	}
	return host
}

// materialize turns every own text inside host into a text node, so
// positions only ever address text nodes.
func (st *editState) materialize(host *El) {
	var walk func(e *El)
	walk = func(e *El) {
		if e.isTextSeg() || e.tag == "#comment" || e.backing != nil {
			return
		}
		if e.text != "" {
			st.dom().OwnText(e)
		}
		for _, k := range e.Kids() {
			if ke, ok := k.(*El); ok {
				walk(ke)
			}
		}
	}
	walk(host)
}

// --- the linear view ---

// flatDoc is a host's content as a string of characters with the DOM
// position of every boundary between them: rendered text (whitespace
// collapsed), '\n' between blocks and for each <br>, U+FFFC for an atomic
// element. Left / right / word movement and deletion run on it.
type flatDoc struct {
	host *El
	text []rune
	pos  []Pos // len(text)+1
	// src[i] says where character i came from: a text node (seg) or an
	// element (a <br>, an atom), or a block boundary (nil node).
	src   []flatSrc
	bound []int // grapheme boundaries
}

type flatSrc struct {
	node *El
	// for text: rune range in node.text
	start, end int
	kind       uint8 // flatText, flatBr, flatAtom, flatBlock
	// flatBlock: the boundary leaves block a and enters b (b may be a
	// run of inline content when nil).
}

const (
	flatText = iota
	flatBr
	flatAtom
	flatBlock
)

func (st *editState) flatten(host *El) *flatDoc {
	fd := &flatDoc{host: host}
	fd.pos = []Pos{{Node: host, Off: 0}}
	weak := true
	breakPending := false
	addChar := func(r rune, src flatSrc, after Pos, afterWeak bool) {
		fd.text = append(fd.text, r)
		fd.src = append(fd.src, src)
		fd.pos = append(fd.pos, after)
		weak = afterWeak
	}
	// lineBreak emits the '\n' a block boundary makes (once), the new line
	// starting at at.
	lineBreak := func(at Pos) {
		if len(fd.text) > 0 && fd.text[len(fd.text)-1] != '\n' || breakPending && len(fd.text) > 0 {
			addChar('\n', flatSrc{kind: flatBlock}, at, true)
		}
		breakPending = false
	}
	lastSpace := func() bool {
		return len(fd.text) == 0 || fd.text[len(fd.text)-1] == ' ' || fd.text[len(fd.text)-1] == '\n'
	}
	var walk func(e *El, preserve bool)
	walk = func(e *El, preserve bool) {
		for i, k := range e.elementKids {
			c, ok := k.(*El)
			if !ok {
				continue
			}
			if c.isTextSeg() {
				if c.text == "" {
					continue
				}
				src := []rune(c.text)
				if breakPending {
					lineBreak(Pos{Node: c, Off: 0})
				}
				for j := 0; j < len(src); j++ {
					r := src[j]
					if !preserve && r < 0x80 && isSpace(byte(r)) {
						// A whitespace run → one space, dropped after a
						// space or at a line start.
						end := j
						for end < len(src) && src[end] < 0x80 && isSpace(byte(src[end])) {
							end++
						}
						if !lastSpace() {
							if weak {
								fd.pos[len(fd.pos)-1] = Pos{Node: c, Off: j}
							}
							addChar(' ', flatSrc{node: c, start: j, end: end, kind: flatText}, Pos{Node: c, Off: end}, false)
						}
						j = end - 1
						continue
					}
					if weak {
						fd.pos[len(fd.pos)-1] = Pos{Node: c, Off: j}
					}
					addChar(r, flatSrc{node: c, start: j, end: j + 1, kind: flatText}, Pos{Node: c, Off: j + 1}, false)
				}
				continue
			}
			if isHidden(c) {
				continue
			}
			if c.tag == "br" {
				if breakPending {
					lineBreak(Pos{Node: e, Off: i})
				}
				if weak {
					fd.pos[len(fd.pos)-1] = Pos{Node: e, Off: i}
				}
				addChar('\n', flatSrc{node: c, kind: flatBr}, Pos{Node: e, Off: i + 1}, true)
				continue
			}
			if isAtomic(c) {
				if breakPending {
					lineBreak(Pos{Node: e, Off: i})
				}
				if weak {
					fd.pos[len(fd.pos)-1] = Pos{Node: e, Off: i}
				}
				addChar('￼', flatSrc{node: c, kind: flatAtom}, Pos{Node: e, Off: i + 1}, true)
				continue
			}
			pre := preserve
			if c.lastCS != nil {
				pre = c.lastCS.PreserveWS
			}
			if isBlockEl(c) {
				lineBreak(Pos{Node: c, Off: 0})
				if len(fd.text) == 0 {
					fd.pos[0] = Pos{Node: c, Off: 0}
				} else {
					fd.pos[len(fd.pos)-1] = Pos{Node: c, Off: 0}
				}
				weak = true
				startLen := len(fd.text)
				walk(c, pre)
				// A trailing <br> ends its block's last line: it makes
				// no line of its own (an empty block's <br> is its
				// placeholder).
				if n := len(fd.text); n > startLen && fd.src[n-1].kind == flatBr && blockOf(fd.src[n-1].node, host) == c {
					fd.text = fd.text[:n-1]
					fd.src = fd.src[:n-1]
					fd.pos = fd.pos[:n]
				}
				// Trailing collapsible space at a block end is invisible.
				if n := len(fd.text); !pre && n > startLen && fd.text[n-1] == ' ' && fd.src[n-1].kind == flatText {
					fd.text = fd.text[:n-1]
					fd.src = fd.src[:n-1]
					fd.pos = fd.pos[:n]
				}
				breakPending = true
				weak = true
				continue
			}
			walk(c, pre)
		}
	}
	pre := false
	if host.lastCS != nil {
		pre = host.lastCS.PreserveWS
	}
	walk(host, pre)
	if n := len(fd.text); n > 0 && fd.src[n-1].kind == flatBr {
		fd.text = fd.text[:n-1]
		fd.src = fd.src[:n-1]
		fd.pos = fd.pos[:n]
	}
	fd.bound = qui.GraphemeBoundaries(fd.text)
	return fd
}

// index returns the boundary at or before p (the largest i with
// pos[i] <= p).
func (fd *flatDoc) index(p Pos) int {
	lo, hi := 0, len(fd.pos)-1
	if comparePos(fd.pos[0], p) > 0 {
		return 0
	}
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if comparePos(fd.pos[mid], p) <= 0 {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func (fd *flatDoc) prevGrapheme(i int) int {
	for j := len(fd.bound) - 1; j >= 0; j-- {
		if fd.bound[j] < i {
			return fd.bound[j]
		}
	}
	return 0
}

func (fd *flatDoc) nextGrapheme(i int) int {
	for _, b := range fd.bound {
		if b > i {
			return b
		}
	}
	return len(fd.text)
}

func (fd *flatDoc) prevWord(i int) int {
	for i > 0 && !qui.IsWordRune(fd.text[i-1]) {
		i--
		if fd.text[i] == '\n' {
			return i
		}
	}
	for i > 0 && qui.IsWordRune(fd.text[i-1]) {
		i--
	}
	return i
}

func (fd *flatDoc) nextWord(i int) int {
	n := len(fd.text)
	for i < n && !qui.IsWordRune(fd.text[i]) {
		if fd.text[i] == '\n' {
			return i + 1
		}
		i++
	}
	for i < n && qui.IsWordRune(fd.text[i]) {
		i++
	}
	return i
}

// hardLine returns the [start, end) of the hard line containing i.
func (fd *flatDoc) hardLine(i int) (int, int) {
	s := i
	for s > 0 && fd.text[s-1] != '\n' {
		s--
	}
	e := i
	for e < len(fd.text) && fd.text[e] != '\n' {
		e++
	}
	return s, e
}

// --- selection ---

// Selection returns the document selection (anchor, focus); ok is false
// when there is none.
func (eng *StyleEngine) Selection() (anchor, focus Pos, ok bool) {
	st := eng.edit
	if st == nil || !st.hasSel {
		return Pos{}, Pos{}, false
	}
	if !st.valid(st.anchor) || !st.valid(st.focus) {
		st.hasSel = false
		return Pos{}, Pos{}, false
	}
	return st.anchor, st.focus, true
}

// SetSelection sets the document selection (a.Node == nil clears it).
// A selection inside an editing host moves its caret.
func (eng *StyleEngine) SetSelection(anchor, focus Pos) {
	st := eng.editing()
	if anchor.Node == nil {
		st.clearSel()
		return
	}
	if focus.Node == nil {
		focus = anchor
	}
	st.setSel(clampPos(anchor), clampPos(focus))
}

func clampPos(p Pos) Pos {
	if p.Off < 0 {
		p.Off = 0
	}
	if n := nodeLen(p.Node); p.Off > n {
		p.Off = n
	}
	return p
}

// valid reports whether p still addresses a node in the document.
func (st *editState) valid(p Pos) bool {
	if p.Node == nil {
		return false
	}
	root := p.Node
	for root.elParent != nil {
		if !isKid(root.elParent, root) {
			return false // removed (a stale parent link)
		}
		root = root.elParent
	}
	if st.eng.roots[root] || root == st.eng.root {
		return p.Off <= nodeLen(p.Node)
	}
	return false
}

func (st *editState) clearSel() {
	if !st.hasSel {
		return
	}
	st.hasSel = false
	st.anchor, st.focus = Pos{}, Pos{}
	st.afterSelChange()
}

func (st *editState) setSel(a, f Pos) {
	if st.hasSel && st.anchor == a && st.focus == f {
		return
	}
	st.hasSel = true
	st.anchor, st.focus = a, f
	if st.typingStyle != nil && st.typingAt != f {
		st.typingStyle = nil
	}
	st.afterSelChange()
}

func (st *editState) collapse(p Pos) { st.setSel(p, p) }

func (st *editState) afterSelChange() {
	if h := st.selHost(); h != nil {
		h.Invalidate()
	}
	for _, ib := range st.highlighted {
		ib.Invalidate()
	}
	st.queueHighlight()
	if st.hooks.SelectionChange != nil {
		st.hooks.SelectionChange()
	}
}

// ordered returns the selection's start and end in document order.
func (st *editState) ordered() (Pos, Pos) {
	if comparePos(st.anchor, st.focus) <= 0 {
		return st.anchor, st.focus
	}
	return st.focus, st.anchor
}

func (st *editState) collapsed() bool { return st.anchor == st.focus }

// selHost is the editing host the selection's focus is in (nil when it is
// not in editable content).
func (st *editState) selHost() *El {
	if !st.hasSel || st.focus.Node == nil {
		return nil
	}
	n := st.focus.Node
	if n.isTextSeg() {
		n = n.elParent
	}
	return n.EditingHost()
}

// SelectionText returns the selected text (Selection.toString).
func (eng *StyleEngine) SelectionText() string {
	st := eng.edit
	if st == nil || !st.hasSel || st.collapsed() {
		return ""
	}
	a, b := st.ordered()
	host := commonHost(a, b)
	fd := st.flatten(host)
	i, j := fd.index(a), fd.index(b)
	return strings.ReplaceAll(string(fd.text[i:j]), "￼", "")
}

// commonHost returns the element to linearize for a range: the editing
// host, else the nearest common element ancestor.
func commonHost(a, b Pos) *El {
	n := a.Node
	if n.isTextSeg() && n.elParent != nil {
		n = n.elParent
	}
	if h := n.EditingHost(); h != nil && elContains(h, b.Node) {
		return h
	}
	for x := n; x != nil; x = x.elParent {
		if elContains(x, b.Node) {
			return x
		}
	}
	return n
}

// --- undo ---

type editOp struct {
	kind     uint8 // opText, opInsert, opRemove
	node     *El
	parent   *El
	ref      *El // insert: before; remove: the next sibling it had
	old, new string
}

const (
	opText = iota
	opInsert
	opRemove
)

type editTxn struct {
	kind      string
	ops       []editOp
	selBefore [2]Pos
	selAfter  [2]Pos
	when      time.Time
}

func (st *editState) setText(seg *El, s string) {
	if seg.text == s {
		return
	}
	if st.txn != nil {
		st.txn.ops = append(st.txn.ops, editOp{kind: opText, node: seg, old: seg.text, new: s})
	}
	st.dom().SetText(seg, s)
}

func (st *editState) insert(parent, child, ref *El) {
	// A move is a removal then an insertion, so undo puts it back.
	if p := child.elParent; p != nil && isKid(p, child) {
		st.remove(child)
	}
	if st.txn != nil {
		st.txn.ops = append(st.txn.ops, editOp{kind: opInsert, node: child, parent: parent, ref: ref})
	}
	st.dom().Insert(parent, child, ref)
}

func (st *editState) remove(child *El) {
	p := child.elParent
	if p == nil {
		return
	}
	if st.txn != nil {
		st.txn.ops = append(st.txn.ops, editOp{kind: opRemove, node: child, parent: p, ref: kidEl(p, childIndex(p, child)+1)})
	}
	domRemove(st.dom(), child)
}

// domRemove removes child and drops its parent link (an embedder's
// removal may leave it set).
func domRemove(d EditDOM, child *El) {
	p := child.elParent
	d.Remove(child)
	if child.elParent == p && !isKid(p, child) {
		child.elParent = nil
	}
}

// begin starts recording an undoable edit.
func (st *editState) begin(kind string) {
	st.txn = &editTxn{kind: kind, selBefore: [2]Pos{st.anchor, st.focus}}
}

// commit ends the recording, coalescing consecutive typing.
func (st *editState) commit() {
	t := st.txn
	st.txn = nil
	if t == nil || len(t.ops) == 0 {
		return
	}
	t.selAfter = [2]Pos{st.anchor, st.focus}
	t.when = time.Now()
	st.redo = nil
	if n := len(st.undo); n > 0 && (t.kind == "type" || t.kind == "delete") {
		last := st.undo[n-1]
		if last.kind == t.kind && t.when.Sub(last.when) < time.Second && last.selAfter == t.selBefore {
			last.ops = append(last.ops, t.ops...)
			last.selAfter, last.when = t.selAfter, t.when
			return
		}
	}
	st.undo = append(st.undo, t)
	if len(st.undo) > 200 {
		st.undo = st.undo[1:]
	}
}

func isKid(p, c *El) bool { return childIndex(p, c) >= 0 }

// replay applies a transaction backwards (undo) or forwards (redo).
func (st *editState) replay(t *editTxn, back bool) bool {
	d := st.dom()
	n := len(t.ops)
	for k := 0; k < n; k++ {
		i := k
		if back {
			i = n - 1 - k
		}
		op := t.ops[i]
		kind := op.kind
		if back {
			switch kind {
			case opInsert:
				kind = opRemove
			case opRemove:
				kind = opInsert
			}
		}
		switch kind {
		case opText:
			s := op.new
			if back {
				s = op.old
			}
			d.SetText(op.node, s)
		case opInsert:
			if op.ref != nil && !isKid(op.parent, op.ref) {
				return false
			}
			d.Insert(op.parent, op.node, op.ref)
		case opRemove:
			if !isKid(op.parent, op.node) {
				return false
			}
			domRemove(d, op.node)
		}
	}
	sel := t.selAfter
	if back {
		sel = t.selBefore
	}
	if st.valid(sel[0]) && st.valid(sel[1]) {
		st.setSel(sel[0], sel[1])
	}
	return true
}
