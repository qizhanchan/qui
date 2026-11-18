package htmlcss

import (
	"strconv"
	"strings"
)

// CSS counters (CSS Lists 3 § 4) and list markers: counter-reset /
// counter-set / counter-increment, counter() and counters() in `content`,
// the implicit `list-item` counter of display: list-item elements, and the
// HTML list attributes (<ol start reversed>, <li value>).
//
// Counter values are computed on demand by walking the element tree in
// document order up to the element asking. The restyle walk styles an
// element after everything before it in document order, so the walk only
// reads styles that are already current. Lists whose numbering involves no
// author counter rules take a shortcut over the list's own children.

// counterOp is one counter-reset / counter-set / counter-increment entry.
type counterOp struct {
	kind  byte // 'r' reset, 's' set, 'i' increment
	name  string
	value int
}

// isListItemDisplay reports a display value that generates a list item.
func isListItemDisplay(d string) bool {
	if !strings.Contains(d, "list-item") {
		return false
	}
	for _, f := range strings.Fields(d) {
		switch f {
		case "list-item", "block", "flow", "flow-root":
		default:
			return false // inline list-item etc.: not supported, plain box
		}
	}
	return true
}

// parseListAndCounters reads list-style-type (and the list-style
// shorthand) and the counter properties into cs.
func parseListAndCounters(n *Node, m map[string]string, cs *ComputedStyle) {
	// UA defaults (HTML § 15.3.8): ol is decimal; ul / menu are disc, then
	// circle and square as they nest inside other lists. The type
	// attribute is a presentational hint under any author rule.
	if n != nil {
		switch n.Tag {
		case "ol":
			cs.ListStyleType = "decimal"
		case "ul", "menu", "dir":
			depth := 0
			for p := n.Parent; p != nil; p = p.Parent {
				if p.Tag == "ul" || p.Tag == "ol" || p.Tag == "menu" || p.Tag == "dir" {
					depth++
				}
			}
			cs.ListStyleType = [...]string{"disc", "circle", "square"}[min(depth, 2)]
		}
		if t, ok := n.Attrs["type"]; ok && (n.Tag == "ol" || n.Tag == "li" || n.Tag == "ul") {
			switch t {
			case "1":
				cs.ListStyleType = "decimal"
			case "a":
				cs.ListStyleType = "lower-alpha"
			case "A":
				cs.ListStyleType = "upper-alpha"
			case "i":
				cs.ListStyleType = "lower-roman"
			case "I":
				cs.ListStyleType = "upper-roman"
			case "disc", "circle", "square", "none":
				cs.ListStyleType = t
			}
		}
	}
	if v, ok := m["list-style"]; ok {
		if t := listStyleFromShorthand(v); t != "" {
			cs.ListStyleType = t
		}
	}
	if v, ok := m["list-style-type"]; ok {
		if t := normalizeListStyleType(v); t != "" {
			cs.ListStyleType = t
		}
	}
	cs.Counters = nil
	for _, p := range [...]struct {
		prop string
		kind byte
		def  int
	}{{"counter-reset", 'r', 0}, {"counter-increment", 'i', 1}, {"counter-set", 's', 0}} {
		v, ok := m[p.prop]
		if !ok {
			continue
		}
		f := strings.Fields(strings.TrimSpace(v))
		if len(f) == 1 && strings.EqualFold(f[0], "none") {
			continue
		}
		for i := 0; i < len(f); i++ {
			name := f[i]
			if strings.HasPrefix(name, "reversed(") { // counter-reset: reversed(x)
				name = strings.TrimSuffix(strings.TrimPrefix(name, "reversed("), ")")
			}
			val := p.def
			if i+1 < len(f) {
				if n, err := strconv.Atoi(f[i+1]); err == nil {
					val = n
					i++
				}
			}
			cs.Counters = append(cs.Counters, counterOp{kind: p.kind, name: name, value: val})
		}
	}
}

// normalizeListStyleType validates a list-style-type value: a keyword
// (lowercased) or a quoted string (kept with its quotes). "" = invalid.
func normalizeListStyleType(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v
	}
	v = strings.ToLower(v)
	if v == "" || strings.ContainsAny(v, " (") {
		return ""
	}
	return v
}

// listStyleFromShorthand extracts the list-style-type keyword (or string)
// from a `list-style` shorthand value, ignoring position and image parts.
func listStyleFromShorthand(v string) string {
	for _, f := range fieldsTopLevel(strings.TrimSpace(v)) {
		lf := strings.ToLower(f)
		switch {
		case lf == "inside" || lf == "outside" || strings.Contains(lf, "("):
			continue
		case lf == "none":
			return "none"
		}
		if t := normalizeListStyleType(f); t != "" {
			return t
		}
	}
	return ""
}

// formatCounter renders n in a list-style-type / counter style.
func formatCounter(n int, style string) string {
	switch style {
	case "none":
		return ""
	case "disc":
		return "•"
	case "circle":
		return "◦"
	case "square":
		return "▪"
	case "decimal-leading-zero":
		if n >= 0 && n < 10 {
			return "0" + strconv.Itoa(n)
		}
	case "lower-alpha", "lower-latin":
		if s := alphabetic(n, 'a', 26); s != "" {
			return s
		}
	case "upper-alpha", "upper-latin":
		if s := alphabetic(n, 'A', 26); s != "" {
			return s
		}
	case "lower-greek":
		if s := alphabeticRunes(n, []rune("αβγδεζηθικλμνξοπρστυφχψω")); s != "" {
			return s
		}
	case "lower-roman":
		if s := roman(n); s != "" {
			return strings.ToLower(s)
		}
	case "upper-roman":
		if s := roman(n); s != "" {
			return s
		}
	}
	if len(style) >= 2 && (style[0] == '"' || style[0] == '\'') {
		return UnescapeCSS(style[1 : len(style)-1])
	}
	return strconv.Itoa(n) // decimal, and the fallback of every style
}

func alphabetic(n int, first rune, size int) string {
	r := make([]rune, size)
	for i := range r {
		r[i] = first + rune(i)
	}
	return alphabeticRunes(n, r)
}

// alphabeticRunes is the CSS `alphabetic` system: a, b, …, z, aa, ab, …
func alphabeticRunes(n int, digits []rune) string {
	if n < 1 {
		return ""
	}
	var out []rune
	for n > 0 {
		n--
		out = append([]rune{digits[n%len(digits)]}, out...)
		n /= len(digits)
	}
	return string(out)
}

func roman(n int) string {
	if n < 1 || n > 3999 {
		return ""
	}
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"M", "CM", "D", "CD", "C", "XC", "L", "XL", "X", "IX", "V", "IV", "I"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// isBulletStyle reports the list-style-types drawn as a shape.
func isBulletStyle(t string) bool { return t == "" || t == "disc" || t == "circle" || t == "square" }

// --- content parsing helpers ---

// closingParen returns the index of the ')' matching the '(' at open,
// skipping quoted strings; -1 when unbalanced.
func closingParen(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitFunctionsQuoted parses one `name(args)` whose arguments may be
// quoted strings containing commas (counters(x, ", ")). Quotes are kept.
func splitFunctionsQuoted(s string) []cssFunction {
	open := strings.IndexByte(s, '(')
	end := closingParen(s, open)
	if open < 0 || end < 0 {
		return nil
	}
	fn := cssFunction{name: strings.ToLower(strings.TrimSpace(s[:open]))}
	inner := s[open+1 : end]
	start := 0
	for i := 0; i <= len(inner); i++ {
		if i < len(inner) && (inner[i] == '"' || inner[i] == '\'') {
			q := inner[i]
			for i++; i < len(inner) && inner[i] != q; i++ {
				if inner[i] == '\\' {
					i++
				}
			}
			continue
		}
		if i == len(inner) || inner[i] == ',' {
			if a := strings.TrimSpace(inner[start:min(i, len(inner))]); a != "" {
				fn.args = append(fn.args, a)
			}
			start = i + 1
		}
	}
	return []cssFunction{fn}
}

// --- counter evaluation ---

type counterInst struct {
	name string
	val  int
	step int // list-item: +1, or -1 inside <ol reversed>
}

// counterWalk evaluates counters in document order up to a target.
type counterWalk struct {
	target *El
	which  string         // "marker", "before", "after"
	pcs    *ComputedStyle // the target pseudo-element's style (its own ops)
	stack  []counterInst
	done   bool
}

func (w *counterWalk) find(name string) int {
	for i := len(w.stack) - 1; i >= 0; i-- {
		if w.stack[i].name == name {
			return i
		}
	}
	return -1
}

// apply runs one element's (or pseudo-element's) counter operations in
// the CSS Lists 3 order: resets, then increments, then sets.
func (w *counterWalk) apply(ops []counterOp, e *El, listItem bool) {
	explicitLI := false
	for _, op := range ops {
		if op.kind == 'r' {
			if op.name == "list-item" {
				explicitLI = true
			}
			w.stack = append(w.stack, counterInst{name: op.name, val: op.value, step: 1})
		}
	}
	// HTML lists reset list-item implicitly (the UA's ol, ul, menu rule),
	// carrying the numbering direction of <ol reversed> and start.
	if e != nil && !explicitLI && (e.tag == "ol" || e.tag == "ul" || e.tag == "menu") {
		w.stack = append(w.stack, listReset(e))
	}
	incLI := false
	for _, op := range ops {
		if op.kind == 'i' {
			if op.name == "list-item" {
				incLI = true
			}
			w.increment(op.name, op.value)
		}
	}
	if listItem && !incLI {
		i := w.find("list-item")
		step := 1
		if i >= 0 {
			step = w.stack[i].step
		}
		w.increment("list-item", step)
	}
	if listItem && e != nil {
		if v, ok := e.attrs["value"]; ok && e.tag == "li" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				w.set("list-item", n)
			}
		}
	}
	for _, op := range ops {
		if op.kind == 's' {
			w.set(op.name, op.value)
		}
	}
}

func (w *counterWalk) increment(name string, by int) {
	i := w.find(name)
	if i < 0 {
		w.stack = append(w.stack, counterInst{name: name, step: 1})
		i = len(w.stack) - 1
	}
	w.stack[i].val += by
}

func (w *counterWalk) set(name string, v int) {
	i := w.find(name)
	if i < 0 {
		w.stack = append(w.stack, counterInst{name: name, step: 1})
		i = len(w.stack) - 1
	}
	w.stack[i].val = v
}

// listReset is the implicit list-item reset of an <ol>/<ul>/<menu>.
func listReset(list *El) counterInst {
	if list.tag != "ol" {
		return counterInst{name: "list-item", step: 1}
	}
	_, reversed := list.attrs["reversed"]
	start, hasStart := 1, false
	if v, ok := list.attrs["start"]; ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			start, hasStart = n, true
		}
	}
	if reversed {
		if !hasStart {
			start = countListItems(list)
		}
		return counterInst{name: "list-item", val: start + 1, step: -1}
	}
	return counterInst{name: "list-item", val: start - 1, step: 1}
}

// countListItems counts the list items among a list's children.
func countListItems(list *El) int {
	n := 0
	for _, k := range list.elementKids {
		if ke, ok := k.(*El); ok && ke.lastCS != nil && ke.lastCS.ListItem && ke.lastCS.Display != "none" {
			n++
		} else if ok && ke.lastCS == nil && ke.tag == "li" {
			n++ // not styled yet (first restyle): an <li> is a list item
		}
	}
	return n
}

// visit walks e's subtree; it stops (w.done) at the target.
func (w *counterWalk) visit(e *El) {
	cs := e.lastCS
	if e == w.target {
		cs = w.targetCS(e)
	}
	if cs == nil || cs.Display == "none" || e.isTextSeg() {
		return
	}
	w.apply(cs.Counters, e, cs.ListItem)
	if e == w.target && w.which == "marker" {
		w.done = true
		return
	}
	if e == w.target && w.which == "before" {
		if w.pcs != nil {
			w.apply(w.pcs.Counters, nil, false)
		}
		w.done = true
		return
	}
	if e != w.target && e.pbCS != nil {
		w.apply(e.pbCS.Counters, nil, false)
	}
	base := len(w.stack)
	for _, k := range e.elementKids {
		if ke, ok := k.(*El); ok {
			w.visit(ke)
			if w.done {
				return
			}
		}
	}
	// Counters created by the children go out of scope with their parent.
	w.stack = w.stack[:base]
	if e == w.target && w.which == "after" {
		if w.pcs != nil {
			w.apply(w.pcs.Counters, nil, false)
		}
		w.done = true
		return
	}
	if e.paCS != nil {
		w.apply(e.paCS.Counters, nil, false)
	}
}

// targetCS is the target's style: the one being applied right now.
func (w *counterWalk) targetCS(e *El) *ComputedStyle { return e.counterCS }

// counterValues returns the values of every in-scope counter name, outermost
// first, at e's `which` pseudo-element ("marker", "before", "after"). cs is
// e's style being applied, pcs the pseudo-element's own style (or nil).
func (e *El) counterValues(name, which string, cs, pcs *ComputedStyle) []int {
	root := e
	for root.elParent != nil {
		root = root.elParent
	}
	e.counterCS = cs
	defer func() { e.counterCS = nil }()
	w := &counterWalk{target: e, which: which, pcs: pcs}
	w.visit(root)
	var out []int
	for _, c := range w.stack {
		if c.name == name {
			out = append(out, c.val)
		}
	}
	if len(out) == 0 {
		out = []int{0} // a counter() of a counter not in scope is 0
	}
	return out
}

// counterContent resolves a counter() / counters() function for e.
func (e *El) counterContent(fn cssFunction, which string, cs, pcs *ComputedStyle) string {
	if len(fn.args) == 0 {
		return ""
	}
	name := strings.TrimSpace(fn.args[0])
	vals := e.counterValues(name, which, cs, pcs)
	unq := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') {
			return UnescapeCSS(s[1 : len(s)-1])
		}
		return s
	}
	style := "decimal"
	if fn.name == "counter" {
		if len(fn.args) > 1 {
			style = strings.ToLower(strings.TrimSpace(fn.args[1]))
		}
		return formatCounter(vals[len(vals)-1], style)
	}
	sep := ""
	if len(fn.args) > 1 {
		sep = unq(fn.args[1])
	}
	if len(fn.args) > 2 {
		style = strings.ToLower(strings.TrimSpace(fn.args[2]))
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = formatCounter(v, style)
	}
	return strings.Join(parts, sep)
}

// listItemOrdinal is the list-item counter at e's marker. Plain HTML lists
// (no author counter rules) count the list's own children; anything else
// walks the document.
func (e *El) listItemOrdinal(cs *ComputedStyle) int {
	p := e.elParent
	if p != nil && (p.tag == "ol" || p.tag == "ul" || p.tag == "menu") &&
		!sheetUsesCounters(e.engineSheet()) && len(cs.Counters) == 0 {
		inst := listReset(p)
		val := inst.val
		for _, k := range p.elementKids {
			ke, ok := k.(*El)
			if !ok {
				continue
			}
			kcs := ke.lastCS
			if ke == e {
				kcs = cs
			}
			if kcs == nil || kcs.Display == "none" || !kcs.ListItem {
				if ke == e {
					break
				}
				continue
			}
			val += inst.step
			if v, ok := ke.attrs["value"]; ok && ke.tag == "li" {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					val = n
				}
			}
			if ke == e {
				return val
			}
		}
		return val
	}
	v := e.counterValues("list-item", "marker", cs, nil)
	return v[len(v)-1]
}

func (e *El) engineSheet() *Stylesheet {
	if e.engine == nil {
		return nil
	}
	return e.engine.sheet
}

// sheetUsesCounters reports whether any rule in sheet declares a counter
// property or uses counter()/counters() — the cue that list numbering
// needs the full document walk. Memoized per sheet and rule count.
func sheetUsesCounters(sheet *Stylesheet) bool {
	if sheet == nil {
		return false
	}
	sheet.idxMu.Lock()
	defer sheet.idxMu.Unlock()
	if c := sheet.counterUse; c.known && c.rules == len(sheet.Rules) {
		return c.uses
	}
	uses := false
	for i := range sheet.Rules {
		for _, d := range sheet.Rules[i].Declarations {
			if strings.HasPrefix(d.Property, "counter-") {
				uses = true
			}
		}
	}
	sheet.counterUse = counterUseEntry{known: true, rules: len(sheet.Rules), uses: uses}
	return uses
}

type counterUseEntry struct {
	known bool
	rules int
	uses  bool
}
