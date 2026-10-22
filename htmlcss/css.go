package htmlcss

import (
	"strconv"
	"strings"
)

// Declaration is a single CSS property/value pair, plus its !important flag.
type Declaration struct {
	Property  string
	Value     string
	Important bool
}

// combinator links one compound selector to the compound on its left.
type combinator uint8

const (
	combDescendant combinator = iota // "a b"   (whitespace)
	combChild                        // "a > b"
	combAdjacent                     // "a + b"
	combSibling                      // "a ~ b"
)

// attrMatch is the operator of an attribute selector.
type attrMatch uint8

const (
	attrExists   attrMatch = iota // [attr]
	attrEquals                    // [attr=val]
	attrPrefix                    // [attr^=val]
	attrSuffix                    // [attr$=val]
	attrSubstr                    // [attr*=val]
	attrWord                      // [attr~=val]  (space-separated word list)
	attrDashHyph                  // [attr|=val]  (val or val-...)
)

// attrSelector is one `[name op val]` clause.
type attrSelector struct {
	name string
	op   attrMatch
	val  string
	ci   bool // trailing `i` flag — match values case-insensitively
}

// pseudoKind classifies a parsed pseudo-class.
type pseudoKind uint8

const (
	pseudoHover        pseudoKind = iota // :hover        (state variant)
	pseudoFocus                          // :focus        (state variant)
	pseudoActive                         // :active       (state variant)
	pseudoFocusVisible                   // :focus-visible (keyboard focus only)
	pseudoRoot                           // :root (document root element)
	pseudoFirstChild                     // :first-child
	pseudoLastChild                      // :last-child
	pseudoOnlyChild                      // :only-child
	pseudoNthChild                       // :nth-child(An+B) — uses a, b
	pseudoNthOfType                      // :nth-of-type(An+B) — uses a, b (per-tag)
	pseudoChecked                        // :checked  (attribute-driven)
	pseudoDisabled                       // :disabled (attribute-driven)
	pseudoEnabled                        // :enabled   (a control without `disabled`)
	pseudoRequired                       // :required  (control with `required`)
	pseudoOptional                       // :optional  (control without `required`)
	pseudoReadOnly                       // :read-only (not user-alterable)
	pseudoReadWrite                      // :read-write (editable control)
	pseudoNthLastChild                   // :nth-last-child(An+B) — from the end
	pseudoEmpty                          // :empty (no element/text children)
	pseudoHas                            // :has(rel, …) — relative selectors in args
	pseudoNot                            // :not(sel, …)  — uses args
	pseudoUnsupported                    // parsed but not evaluable → never matches
)

// pseudoSelector is a parsed pseudo-class. Structural pseudos evaluate at
// match time; state pseudos (hover/focus/active) participate only when the
// corresponding state flag is set during resolution.
type pseudoSelector struct {
	kind pseudoKind
	a, b int // :nth-child An+B coefficients
	// args is the selector list of :not() / :has(). For :has each entry is
	// a RELATIVE selector: parts[0].comb is its relation to the anchor
	// element (descendant unless written `> x`, `+ x`, `~ x`).
	args []Selector
	raw  string // original name, for diagnostics
}

// simpleSelector is one compound selector part (e.g. "div.card#main[type=x]:hover"):
// an optional tag plus any number of classes, ids, attribute clauses and
// pseudo-classes.
type simpleSelector struct {
	tag        string // "" or "*" means any
	id         string
	classes    []string
	attrs      []attrSelector
	pseudos    []pseudoSelector
	pseudoElem string // "before" / "after" (empty = none); targets a pseudo-element
}

// compound is one compound selector plus the combinator joining it to the
// PREVIOUS compound in the chain. The first compound's comb is
// combDescendant and is unused by matching.
type compound struct {
	comb combinator
	sel  simpleSelector
}

// Selector is a combinator chain of compound parts. "nav > ul li" parses to
// three compounds; matching walks right-to-left honoring each combinator.
type Selector struct {
	parts []compound
}

// Rule is a group of selectors sharing a declaration block.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration
}

// selectorState is the interactive state passed into matching so state
// pseudo-classes (:hover/:focus/:active) select the right variant. The
// zero value is the resting state (no interactive pseudo matches).
//
// Subject vs non-subject: hover/focus/active describe the SUBJECT's own
// state. Non-subject (ancestor / preceding-sibling) compounds are gated
// separately — see matchesElement's compState:
//   - the subject's state propagates to ANCESTOR compounds only where CSS
//     implies it (hovering/pressing an element hovers/activates its
//     ancestors; :focus is normalized from :focus-within, so a focused
//     subject counts as focus-within every ancestor) — never to siblings;
//   - ancestorHover/Focus/Active turn the pseudo on for ALL non-subject
//     compounds at once (the union variants used for sensitivity checks);
//   - hoverNodes/focusNodes/activeNodes scope the pseudo to SPECIFIC nodes,
//     answering "does exactly this set of hovered/focused/pressed elements
//     activate the rule?" — the per-trigger variants (El.ancestorStateVariant).
type selectorState struct {
	hover  bool
	focus  bool
	active bool
	// focusVisible narrows focus to keyboard focus (:focus-visible).
	focusVisible bool

	ancestorHover  bool
	ancestorFocus  bool
	ancestorActive bool

	hoverNodes  []*Node
	focusNodes  []*Node
	activeNodes []*Node
}

func nodeIn(list []*Node, n *Node) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

// stateSel indexes one selector that carries an interactive-state pseudo on
// a NON-subject compound (`.row:hover .del`), with which states it gates on.
// Trigger discovery (Stylesheet.stateTriggersFor) scans only these.
type stateSel struct {
	sel     Selector
	h, f, a bool
}

// Stylesheet is an ordered list of rules (source order is preserved and
// used as the final cascade tie-breaker).
type Stylesheet struct {
	Rules []Rule
	// HasHas is true when any selector uses :has(). Unlike ordinary supported
	// selectors, :has makes an element's match depend on descendants, so a
	// mutation may invalidate every ancestor up to its top-level style root.
	HasHas bool
	// HasSiblingCombinator is true when any selector uses "+" or "~": an
	// element's attributes then decide how its following siblings match.
	HasSiblingCombinator bool
	// HasStructural is true when any selector depends on an element's
	// position among its siblings or on its children existing
	// (:first-child, :last-child, :only-child, :nth-*, :empty) or uses a
	// sibling combinator: changing a children list may then restyle the
	// siblings that stay.
	HasStructural bool
	// structKeys keys the elements those selectors can concern (see
	// StructuralRelevant).
	structKeys structKeys
	// HasHover / HasFocus / HasActive are true when any rule is gated on the
	// matching interactive pseudo, so style resolution knows to compute the
	// corresponding per-element state variant.
	HasHover  bool
	HasFocus  bool
	HasActive bool
	// HasFocusVisible is true when a rule's subject is gated on
	// :focus-visible — gates the FocusVisible variant.
	HasFocusVisible bool
	// HasAncestorHover / Focus / Active are true when some selector carries
	// the state pseudo on a NON-subject compound (`.row:hover .del`) — gates
	// computing the per-element AncestorHover/Focus/Active union variants.
	HasAncestorHover  bool
	HasAncestorFocus  bool
	HasAncestorActive bool
	// stateSels are the selectors behind HasAncestor*; hasSiblingStateSel is
	// true when one of them reaches its state compound through a sibling
	// combinator (`.a:hover ~ .b`), so trigger discovery must also consider
	// preceding siblings, not just ancestors.
	stateSels          []stateSel
	hasSiblingStateSel bool
}

// stateTriggersFor reports which of cand's interactive states activate a
// non-subject state rule on n — i.e. whether some `cand…:hover … n` (resp.
// :focus/:active) rule matches n exactly because cand is in that state.
// Lets an ancestor-state reveal be scoped to the element(s) the selector
// names, not any hovered/focused ancestor on the hit path. want* skip the
// states the caller doesn't care about. Limitation: a rule needing TWO
// state compounds at once (`.a:hover .b:hover .c`) never reports a trigger
// (each candidate is tested alone).
func (s *Stylesheet) stateTriggersFor(n, cand *Node, wantH, wantF, wantA bool) (h, f, a bool) {
	if n == nil || cand == nil {
		return
	}
	nodes := []*Node{cand}
	for _, ss := range s.stateSels {
		if ss.h && wantH && !h && ss.sel.matchesElement(n, selectorState{hoverNodes: nodes}) {
			h = true
		}
		if ss.f && wantF && !f && ss.sel.matchesElement(n, selectorState{focusNodes: nodes}) {
			f = true
		}
		if ss.a && wantA && !a && ss.sel.matchesElement(n, selectorState{activeNodes: nodes}) {
			a = true
		}
		if (!wantH || h) && (!wantF || f) && (!wantA || a) {
			break
		}
	}
	return
}

// defaultViewportWidth sizes @media evaluation when no viewport is supplied
// (a typical desktop width). Render/Mount pass the real window width.
const defaultViewportWidth = 1280

// ParseCSS parses a stylesheet, evaluating @media blocks against a default
// desktop viewport width. Other at-rules (@font-face/@keyframes/…) are skipped.
func ParseCSS(src string) *Stylesheet {
	return ParseCSSViewport(src, defaultViewportWidth)
}

// ParseCSSViewport parses a stylesheet, evaluating @media (min-width/max-width)
// against viewportW. Matching @media blocks are flattened into the sheet after
// the base rules (so they win same-specificity ties, matching CSS source order).
// Non-@media at-rules are skipped. Evaluation is one-shot at parse time (not
// reactive to window resize).
func ParseCSSViewport(src string, viewportW float32) *Stylesheet {
	if viewportW <= 0 {
		viewportW = defaultViewportWidth
	}
	src = stripComments(src)
	sheet := &Stylesheet{}
	p := &cssParser{src: src}
	for !p.eof() {
		p.skipSpaces()
		if p.eof() {
			break
		}
		// A statement at-rule (@charset, @import, `@layer a, b;`) ends at
		// its ';' and has no block. Without this, readSelector would run
		// on to the NEXT rule's '{' and swallow that rule's selector.
		if p.src[p.pos] == '@' {
			rest := p.src[p.pos:]
			if semi := strings.IndexByte(rest, ';'); semi >= 0 {
				if brace := strings.IndexByte(rest, '{'); brace < 0 || semi < brace {
					p.pos += semi + 1
					continue
				}
			}
		}
		selText := strings.TrimSpace(p.readSelector())
		if p.eof() {
			break
		}
		p.pos++ // consume '{'
		if strings.HasPrefix(selText, "@media") {
			block := p.readBalancedBlock() // inner rules, braces balanced
			cond := strings.TrimSpace(strings.TrimPrefix(selText, "@media"))
			if mediaMatches(cond, viewportW) {
				inner := ParseCSSViewport(block, viewportW)
				sheet.Rules = append(sheet.Rules, inner.Rules...)
				sheet.HasHover = sheet.HasHover || inner.HasHover
				sheet.HasFocus = sheet.HasFocus || inner.HasFocus
				sheet.HasFocusVisible = sheet.HasFocusVisible || inner.HasFocusVisible
				sheet.HasActive = sheet.HasActive || inner.HasActive
				sheet.HasAncestorHover = sheet.HasAncestorHover || inner.HasAncestorHover
				sheet.HasAncestorFocus = sheet.HasAncestorFocus || inner.HasAncestorFocus
				sheet.HasAncestorActive = sheet.HasAncestorActive || inner.HasAncestorActive
				sheet.HasHas = sheet.HasHas || inner.HasHas
				sheet.HasSiblingCombinator = sheet.HasSiblingCombinator || inner.HasSiblingCombinator
				sheet.HasStructural = sheet.HasStructural || inner.HasStructural
				sheet.structKeys.merge(inner.structKeys)
				sheet.stateSels = append(sheet.stateSels, inner.stateSels...)
				sheet.hasSiblingStateSel = sheet.hasSiblingStateSel || inner.hasSiblingStateSel
			}
			continue
		}
		if strings.HasPrefix(selText, "@") {
			// Other block at-rules (@keyframes, @supports, @font-face)
			// are skipped whole; their blocks may nest, so a plain
			// readUntil('}') would stop at the first inner rule's end.
			p.readBalancedBlock()
			continue
		}
		body := p.readUntil('}')
		if !p.eof() {
			p.pos++ // consume '}'
		}
		if selText == "" || strings.HasPrefix(selText, "@") {
			continue // skip other at-rules and empties
		}
		rule := Rule{
			Selectors:    parseSelectorList(selText),
			Declarations: parseDeclarations(body),
		}
		if len(rule.Selectors) > 0 && len(rule.Declarations) > 0 {
			sheet.Rules = append(sheet.Rules, rule)
			for _, sel := range rule.Selectors {
				sheet.HasHas = sheet.HasHas || sel.hasPseudo(pseudoHas)
				sib := sel.hasSiblingCombinator()
				sheet.HasSiblingCombinator = sheet.HasSiblingCombinator || sib
				if sib || sel.hasStructuralPseudo() {
					sheet.HasStructural = true
					sel.addStructKeys(&sheet.structKeys)
				}
				h, f, a := sel.stateGates()
				sheet.HasHover = sheet.HasHover || h
				sheet.HasFocus = sheet.HasFocus || f
				sheet.HasFocusVisible = sheet.HasFocusVisible || sel.usesFocusVisible()
				sheet.HasActive = sheet.HasActive || a
				nh, nf, na, viaSibling := sel.nonSubjectStateGates()
				if nh || nf || na {
					sheet.HasAncestorHover = sheet.HasAncestorHover || nh
					sheet.HasAncestorFocus = sheet.HasAncestorFocus || nf
					sheet.HasAncestorActive = sheet.HasAncestorActive || na
					sheet.stateSels = append(sheet.stateSels, stateSel{sel: sel, h: nh, f: nf, a: na})
					sheet.hasSiblingStateSel = sheet.hasSiblingStateSel || viaSibling
				}
			}
		}
	}
	return sheet
}

// hasPseudo reports whether any compound (including the argument of :not)
// carries kind. Keeping this on the parsed selector avoids error-prone text
// scanning and makes nested supported simple selectors safe too.
// structKeys is a conservative key set for the elements structural
// selectors concern: an element carrying none of these classes, ids or
// tags can neither be matched nor un-matched by a change of its siblings'
// positions or attributes. any = some such compound has no key at all
// (":first-child", "* + *"): every element counts.
type structKeys struct {
	any                bool
	classes, ids, tags map[string]bool
}

func (k *structKeys) add(ss simpleSelector) {
	if len(ss.classes) == 0 && ss.id == "" && (ss.tag == "" || ss.tag == "*") {
		k.any = true
		return
	}
	put := func(m *map[string]bool, v string) {
		if *m == nil {
			*m = map[string]bool{}
		}
		(*m)[v] = true
	}
	for _, c := range ss.classes {
		put(&k.classes, c)
	}
	if ss.id != "" {
		put(&k.ids, ss.id)
	}
	if len(ss.classes) == 0 && ss.id == "" {
		put(&k.tags, ss.tag)
	}
}

func (k *structKeys) merge(o structKeys) {
	k.any = k.any || o.any
	for _, m := range []struct {
		dst *map[string]bool
		src map[string]bool
	}{{&k.classes, o.classes}, {&k.ids, o.ids}, {&k.tags, o.tags}} {
		for v := range m.src {
			if *m.dst == nil {
				*m.dst = map[string]bool{}
			}
			(*m.dst)[v] = true
		}
	}
}

// relevant reports whether n carries one of the keys.
func (k *structKeys) relevant(n *Node) bool {
	if k.any {
		return true
	}
	if n == nil || n.Type != ElementNode {
		return false
	}
	if k.tags[n.Tag] {
		return true
	}
	if id, ok := n.Attr("id"); ok && k.ids[id] {
		return true
	}
	if len(k.classes) > 0 {
		for _, c := range n.Classes() {
			if k.classes[c] {
				return true
			}
		}
	}
	return false
}

// StructuralRelevant reports whether a structural or sibling-combinator
// selector of the sheet can concern n.
func (s *Stylesheet) StructuralRelevant(n *Node) bool {
	return s != nil && s.HasStructural && s.structKeys.relevant(n)
}

// addStructKeys records the compounds of s a structural change can reach:
// those with a structural pseudo, and both sides of each "+" / "~".
func (s Selector) addStructKeys(k *structKeys) {
	for i, part := range s.parts {
		structural := false
		for _, ps := range part.sel.pseudos {
			switch ps.kind {
			case pseudoFirstChild, pseudoLastChild, pseudoOnlyChild, pseudoNthChild,
				pseudoNthOfType, pseudoNthLastChild, pseudoEmpty:
				structural = true
			}
			for _, a := range ps.args {
				if a.hasStructuralPseudo() {
					structural = true
				}
			}
		}
		if structural {
			k.add(part.sel)
		}
		if i > 0 && (part.comb == combAdjacent || part.comb == combSibling) {
			k.add(part.sel)
			k.add(s.parts[i-1].sel)
		}
	}
}

// hasSiblingCombinator reports whether s joins compounds with "+" or "~".
func (s Selector) hasSiblingCombinator() bool {
	for i, part := range s.parts {
		if i > 0 && (part.comb == combAdjacent || part.comb == combSibling) {
			return true
		}
	}
	return false
}

// hasStructuralPseudo reports whether s uses a pseudo-class that depends on
// the element's siblings or children.
func (s Selector) hasStructuralPseudo() bool {
	for _, k := range []pseudoKind{pseudoFirstChild, pseudoLastChild, pseudoOnlyChild,
		pseudoNthChild, pseudoNthOfType, pseudoNthLastChild, pseudoEmpty} {
		if s.hasPseudo(k) {
			return true
		}
	}
	return false
}

func (s Selector) hasPseudo(kind pseudoKind) bool {
	var hasInSimple func(simpleSelector) bool
	hasInSimple = func(simple simpleSelector) bool {
		for _, ps := range simple.pseudos {
			if ps.kind == kind {
				return true
			}
			for _, a := range ps.args {
				for _, part := range a.parts {
					if hasInSimple(part.sel) {
						return true
					}
				}
			}
		}
		return false
	}
	for _, part := range s.parts {
		if hasInSimple(part.sel) {
			return true
		}
	}
	return false
}

// readBalancedBlock reads from just after a '{' up to its matching '}',
// returning the inner text and consuming the closing brace.
func (p *cssParser) readBalancedBlock() string {
	start := p.pos
	depth := 1
	for p.pos < len(p.src) {
		if p.skipString() {
			continue
		}
		switch p.src[p.pos] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				inner := p.src[start:p.pos]
				p.pos++ // consume matching '}'
				return inner
			}
		}
		p.pos++
	}
	return p.src[start:p.pos]
}

// mediaMatches evaluates a media query list against viewportW: it matches
// when any comma-separated query does. Each query may start with `only`
// (ignored, as browsers do) or `not` (negates the whole query).
func mediaMatches(cond string, viewportW float32) bool {
	cond = strings.ToLower(strings.TrimSpace(cond))
	if cond == "" {
		return true
	}
	for _, q := range splitTopLevel(cond, ',') {
		q = strings.TrimSpace(q)
		negate := false
		if rest, ok := strings.CutPrefix(q, "not "); ok {
			negate, q = true, strings.TrimSpace(rest)
		} else if rest, ok := strings.CutPrefix(q, "only "); ok {
			q = strings.TrimSpace(rest)
		}
		if mediaQueryMatches(q, viewportW) != negate {
			return true
		}
	}
	return false
}

// mediaQueryMatches evaluates one media query. Supports `screen`/`all`
// (match), `print` (no), and `(min-width: N)` / `(max-width: N)` joined by
// `and`. Unknown features fail the term (conservative).
func mediaQueryMatches(cond string, viewportW float32) bool {
	if cond == "" {
		return true
	}
	for _, term := range strings.Split(cond, " and ") {
		term = strings.TrimSpace(term)
		switch term {
		case "screen", "all", "":
			continue
		case "print":
			return false
		}
		if !strings.HasPrefix(term, "(") || !strings.HasSuffix(term, ")") {
			return false
		}
		inner := term[1 : len(term)-1]
		colon := strings.IndexByte(inner, ':')
		if colon < 0 {
			return false
		}
		feature := strings.TrimSpace(inner[:colon])
		val, ok := parseLength(strings.TrimSpace(inner[colon+1:]), 16, 0)
		if !ok {
			return false
		}
		switch feature {
		case "min-width":
			if viewportW < val {
				return false
			}
		case "max-width":
			if viewportW > val {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// parseRelativeSelectorList parses :has()'s argument: comma-separated
// selectors that may each start with a combinator (`> .x`, `+ li`). The
// leading combinator (descendant when absent) lands in parts[0].comb.
func parseRelativeSelectorList(s string) []Selector {
	var out []Selector
	for _, group := range splitTopLevel(s, ',') {
		group = strings.TrimSpace(group)
		lead := combDescendant
		if group != "" {
			if c, ok := combinatorFor(group[:1]); ok {
				lead = c
				group = strings.TrimSpace(group[1:])
			}
		}
		if group == "" {
			continue
		}
		if sel, ok := parseComplexSelector(group); ok {
			sel.parts[0].comb = lead
			out = append(out, sel)
		}
	}
	return out
}

// parseSelectorList splits a comma-separated selector list, then parses
// each group into a combinator chain of compounds. The tokenizer is
// bracket/paren aware so `[href="a b"]` and `:not(a b)` are not split on
// their inner spaces.
func parseSelectorList(s string) []Selector {
	var out []Selector
	for _, group := range splitTopLevel(s, ',') {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if sel, ok := parseComplexSelector(group); ok {
			out = append(out, sel)
		}
	}
	return out
}

// parseComplexSelector parses one comma-free group ("nav > ul li") into a
// Selector. Malformed groups (leading/trailing/double combinator) are
// dropped (ok=false) rather than reinterpreted — conservative failure.
func parseComplexSelector(group string) (Selector, bool) {
	toks := tokenizeSelector(group)
	if len(toks) == 0 {
		return Selector{}, false
	}
	var parts []compound
	pending := combDescendant
	expectCompound := true
	for _, tok := range toks {
		if c, isComb := combinatorFor(tok); isComb {
			if expectCompound {
				return Selector{}, false // leading or doubled combinator
			}
			pending = c
			expectCompound = true
			continue
		}
		parts = append(parts, compound{comb: pending, sel: parseSimple(tok)})
		pending = combDescendant
		expectCompound = false
	}
	if expectCompound {
		return Selector{}, false // trailing combinator
	}
	return Selector{parts: parts}, true
}

func combinatorFor(tok string) (combinator, bool) {
	switch tok {
	case ">":
		return combChild, true
	case "+":
		return combAdjacent, true
	case "~":
		return combSibling, true
	}
	return combDescendant, false
}

// tokenizeSelector splits a compound group into tokens: compound selectors
// and standalone combinator symbols. Whitespace separates tokens except
// inside `[...]` / `(...)`. Combinator glyphs `>`,`+`,`~` are emitted as
// their own tokens even when not surrounded by spaces (e.g. "ul>li").
func tokenizeSelector(s string) []string {
	var toks []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '[' || c == '(':
			depth++
			cur.WriteByte(c)
		case c == ']' || c == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case depth == 0 && isSpace(c):
			flush()
		case depth == 0 && (c == '>' || c == '+' || c == '~'):
			flush()
			toks = append(toks, string(c))
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return toks
}

// splitTopLevel splits s on sep, ignoring sep inside `[...]` / `(...)`.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '[' || c == '(':
			depth++
			cur.WriteByte(c)
		case c == ']' || c == ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case depth == 0 && c == sep:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}

// parseSimple parses "div.card#main[type=text]:hover:not(.x)" into a
// simpleSelector. Bracketed attribute clauses and parenthesized pseudo
// arguments are read as whole units.
func parseSimple(tok string) simpleSelector {
	var ss simpleSelector
	i := 0
	// Leading tag / universal (up to the first .#:[ marker).
	for i < len(tok) && !isSimpleBoundary(tok[i]) {
		i++
	}
	ss.tag = strings.ToLower(tok[:i])
	for i < len(tok) {
		marker := tok[i]
		i++
		switch marker {
		case '.':
			name, next := readName(tok, i)
			if name != "" {
				ss.classes = append(ss.classes, name)
			}
			i = next
		case '#':
			name, next := readName(tok, i)
			if name != "" {
				ss.id = name
			}
			i = next
		case '[':
			clause, next := readBracket(tok, i)
			if a, ok := parseAttrSelector(clause); ok {
				ss.attrs = append(ss.attrs, a)
			}
			i = next
		case ':':
			// A second colon marks a pseudo-ELEMENT ("::before"); a single
			// colon before a known pseudo-element name (legacy ":before") too.
			if i < len(tok) && tok[i] == ':' {
				i++
			}
			name, arg, next := readPseudo(tok, i)
			if isPseudoElement(name) {
				ss.pseudoElem = strings.ToLower(name)
			} else {
				ss.pseudos = append(ss.pseudos, classifyPseudo(name, arg))
			}
			i = next
		default:
			i++ // skip stray byte
		}
	}
	return ss
}

func isSimpleBoundary(b byte) bool {
	return b == '.' || b == '#' || b == ':' || b == '['
}

// isPseudoElement reports whether name is a CSS pseudo-element (as opposed to a
// pseudo-class). Only ::before/::after carry generated content in this engine.
func isPseudoElement(name string) bool {
	switch strings.ToLower(name) {
	case "before", "after", "first-line", "first-letter", "placeholder", "marker", "selection", "backdrop":
		return true
	}
	return false
}

// pseudoElement returns the pseudo-element the selector's subject targets
// ("before"/"after"/…), or "" when it targets a real element.
func (s Selector) pseudoElement() string {
	if len(s.parts) == 0 {
		return ""
	}
	return s.parts[len(s.parts)-1].sel.pseudoElem
}

// readName reads a class/id name up to the next simple-selector boundary.
func readName(tok string, i int) (string, int) {
	start := i
	for i < len(tok) && !isSimpleBoundary(tok[i]) {
		i++
	}
	return tok[start:i], i
}

// readBracket reads the contents of an attribute clause after the '[' (at
// index i), returning the inner text and the index just past the ']'.
func readBracket(tok string, i int) (string, int) {
	start := i
	for i < len(tok) && tok[i] != ']' {
		i++
	}
	inner := tok[start:i]
	if i < len(tok) {
		i++ // consume ']'
	}
	return inner, i
}

// readPseudo reads a pseudo name and its optional (arg) after the ':'.
func readPseudo(tok string, i int) (name, arg string, next int) {
	start := i
	for i < len(tok) && !isSimpleBoundary(tok[i]) && tok[i] != '(' {
		i++
	}
	name = strings.ToLower(tok[start:i])
	if i < len(tok) && tok[i] == '(' {
		i++ // consume '('
		argStart := i
		depth := 1
		for i < len(tok) && depth > 0 {
			if tok[i] == '(' {
				depth++
			} else if tok[i] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
			i++
		}
		arg = tok[argStart:i]
		if i < len(tok) {
			i++ // consume ')'
		}
	}
	return name, arg, i
}

// parseAttrSelector parses the inside of `[...]` into an attrSelector.
func parseAttrSelector(clause string) (attrSelector, bool) {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return attrSelector{}, false
	}
	ops := []struct {
		sym string
		op  attrMatch
	}{
		{"^=", attrPrefix}, {"$=", attrSuffix}, {"*=", attrSubstr},
		{"~=", attrWord}, {"|=", attrDashHyph}, {"=", attrEquals},
	}
	for _, o := range ops {
		if idx := strings.Index(clause, o.sym); idx > 0 {
			name := strings.TrimSpace(clause[:idx])
			val := strings.TrimSpace(clause[idx+len(o.sym):])
			// A trailing flag token (`"val" i` / `val i`) must be stripped
			// BEFORE unquoting, or the closing quote is no longer terminal.
			// `s` (force-sensitive) is the default, so it just gets dropped.
			ci := false
			if fl := strings.ToLower(val); strings.HasSuffix(fl, " i") || strings.HasSuffix(fl, " s") {
				ci = strings.HasSuffix(fl, " i")
				val = strings.TrimSpace(val[:len(val)-2])
			}
			val = unquote(val)
			if ci {
				val = strings.ToLower(val)
			}
			return attrSelector{name: strings.ToLower(name), op: o.op, val: val, ci: ci}, name != ""
		}
	}
	return attrSelector{name: strings.ToLower(clause), op: attrExists}, true
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// classifyPseudo maps a pseudo name (+ optional arg) to a pseudoSelector.
func classifyPseudo(name, arg string) pseudoSelector {
	ps := pseudoSelector{raw: name}
	switch name {
	case "hover":
		ps.kind = pseudoHover
	case "focus", "focus-within":
		ps.kind = pseudoFocus
	case "focus-visible":
		ps.kind = pseudoFocusVisible
	case "active":
		ps.kind = pseudoActive
	case "root":
		ps.kind = pseudoRoot
	case "first-child":
		ps.kind = pseudoFirstChild
	case "last-child":
		ps.kind = pseudoLastChild
	case "only-child":
		ps.kind = pseudoOnlyChild
	case "nth-child":
		if a, b, ok := parseAnB(arg); ok {
			ps.kind, ps.a, ps.b = pseudoNthChild, a, b
		} else {
			ps.kind = pseudoUnsupported
		}
	case "nth-of-type":
		if a, b, ok := parseAnB(arg); ok {
			ps.kind, ps.a, ps.b = pseudoNthOfType, a, b
		} else {
			ps.kind = pseudoUnsupported
		}
	case "checked":
		ps.kind = pseudoChecked
	case "disabled":
		ps.kind = pseudoDisabled
	case "enabled":
		ps.kind = pseudoEnabled
	case "required":
		ps.kind = pseudoRequired
	case "optional":
		ps.kind = pseudoOptional
	case "read-only":
		ps.kind = pseudoReadOnly
	case "read-write":
		ps.kind = pseudoReadWrite
	case "nth-last-child":
		if a, b, ok := parseAnB(arg); ok {
			ps.kind, ps.a, ps.b = pseudoNthLastChild, a, b
		} else {
			ps.kind = pseudoUnsupported
		}
	case "empty":
		ps.kind = pseudoEmpty
	case "has":
		ps.kind, ps.args = pseudoHas, parseRelativeSelectorList(arg)
		if len(ps.args) == 0 {
			ps.kind = pseudoUnsupported
		}
	case "not":
		ps.kind, ps.args = pseudoNot, parseSelectorList(arg)
		if len(ps.args) == 0 {
			ps.kind = pseudoUnsupported
		}
	default:
		ps.kind = pseudoUnsupported
	}
	return ps
}

// parseAnB parses the :nth-child An+B microsyntax: "even", "odd", "3",
// "n", "2n", "2n+1", "-n+3", with arbitrary internal spaces.
func parseAnB(s string) (a, b int, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "")
	switch s {
	case "even":
		return 2, 0, true
	case "odd":
		return 2, 1, true
	case "":
		return 0, 0, false
	}
	if !strings.ContainsRune(s, 'n') {
		if v, err := strconv.Atoi(s); err == nil {
			return 0, v, true
		}
		return 0, 0, false
	}
	idx := strings.IndexByte(s, 'n')
	coef := s[:idx]
	rest := s[idx+1:]
	switch coef {
	case "", "+":
		a = 1
	case "-":
		a = -1
	default:
		v, err := strconv.Atoi(coef)
		if err != nil {
			return 0, 0, false
		}
		a = v
	}
	if rest == "" {
		return a, 0, true
	}
	v, err := strconv.Atoi(rest)
	if err != nil {
		return 0, 0, false
	}
	return a, v, true
}

func parseDeclarations(body string) []Declaration {
	var out []Declaration
	for _, chunk := range splitDeclChunks(body) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		colon := strings.IndexByte(chunk, ':')
		if colon < 0 {
			continue
		}
		prop := strings.TrimSpace(chunk[:colon])
		// Custom property names (--x) are case-sensitive; normal property
		// names are lowercased.
		if !strings.HasPrefix(prop, "--") {
			prop = strings.ToLower(prop)
		}
		val := strings.TrimSpace(chunk[colon+1:])
		important := false
		if idx := strings.LastIndex(strings.ToLower(val), "!important"); idx >= 0 {
			important = true
			val = strings.TrimSpace(val[:idx])
		}
		if prop != "" && val != "" {
			out = append(out, Declaration{Property: prop, Value: val, Important: important})
		}
	}
	return out
}

// splitDeclChunks splits a declaration body on ';' that are NOT inside
// parentheses or quotes — so a `;` within url(data:...;base64,…) or a quoted
// string doesn't prematurely end the declaration.
func splitDeclChunks(body string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ';' && depth == 0:
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	out = append(out, body[start:])
	return out
}

// specificity returns (ids, classes+attrs+pseudos, types) for cascade
// ordering. :not() contributes the specificity of its inner selector; a
// combinator contributes nothing (already handled by ignoring it).
func (s Selector) specificity() (int, int, int) {
	var ids, classes, types int
	for _, part := range s.parts {
		i, c, t := part.sel.specificity()
		ids += i
		classes += c
		types += t
	}
	return ids, classes, types
}

func (ss simpleSelector) specificity() (ids, classes, types int) {
	if ss.id != "" {
		ids++
	}
	classes += len(ss.classes) + len(ss.attrs)
	if ss.tag != "" && ss.tag != "*" {
		types++
	}
	for _, ps := range ss.pseudos {
		if (ps.kind == pseudoNot || ps.kind == pseudoHas) && len(ps.args) > 0 {
			// Selectors 4: the specificity of its most specific argument.
			var bi, bc, bt int
			for _, a := range ps.args {
				i, c, t := a.specificity()
				if i > bi || (i == bi && (c > bc || (c == bc && t > bt))) {
					bi, bc, bt = i, c, t
				}
			}
			ids += bi
			classes += bc
			types += bt
			continue
		}
		classes++ // structural / state pseudos count as a class
	}
	return ids, classes, types
}

// matches reports whether the selector matches node n under interactive
// state st. Matching is right-to-left: the last compound must match n,
// then each combinator constrains where the preceding compound may match.
func (s Selector) matches(n *Node, st selectorState) bool {
	// Pseudo-element selectors (::before/::after) don't style the host element
	// itself — only their generated content (resolved separately via
	// selectorMatchesIgnoringPseudoElem). Fail-closed here.
	if s.pseudoElement() != "" {
		return false
	}
	return s.matchesElement(n, st)
}

// selectorMatchesIgnoringPseudoElem matches s against n WITHOUT the
// pseudo-element fail-closed guard — used to resolve which element a
// ::before/::after generated-content rule attaches to.
func selectorMatchesIgnoringPseudoElem(s Selector, n *Node) bool {
	return s.matchesElement(n, selectorState{})
}

func (s Selector) matchesElement(n *Node, st selectorState) bool {
	return s.matchesAnchored(n, st, nil)
}

// matchesAnchored is matchesElement for a relative selector when anchor is
// non-nil (a :has() argument): the leftmost compound must also stand in
// parts[0].comb's relation to anchor.
func (s Selector) matchesAnchored(n *Node, st selectorState, anchor *Node) bool {
	if len(s.parts) == 0 {
		return false
	}
	last := len(s.parts) - 1
	// The subject compound (rightmost) matches n under the element's OWN
	// interactive state (st.hover/focus/active). Non-subject compounds are
	// gated per candidate node by compState:
	//   - node-scoped sets (hoverNodes/…) turn the pseudo on for exactly the
	//     listed nodes (per-trigger variants);
	//   - the union flags (ancestorHover/…) turn it on for every non-subject
	//     compound (sensitivity detection);
	//   - the subject's own state propagates to ANCESTOR candidates only:
	//     hovering/pressing an element hovers/activates its ancestors per
	//     CSS, and :focus is normalized from :focus-within so a focused
	//     subject counts as focus-within every ancestor. Sibling candidates
	//     never inherit the subject's state. Candidates reached through
	//     child/descendant steps are always ancestors of n (a sibling's
	//     parent is a shared ancestor), so isAncestor comes straight from
	//     the combinator — no tree walk needed.
	compState := func(base selectorState, cand *Node, isAncestor bool) selectorState {
		cs := base
		cs.hover = base.ancestorHover || nodeIn(base.hoverNodes, cand) || (isAncestor && base.hover)
		cs.focus = base.ancestorFocus || nodeIn(base.focusNodes, cand) || (isAncestor && base.focus)
		// On a non-subject compound :focus-visible acts as :focus (focus
		// within an ancestor carries no keyboard/pointer distinction).
		cs.focusVisible = cs.focus
		cs.active = base.ancestorActive || nodeIn(base.activeNodes, cand) || (isAncestor && base.active)
		return cs
	}
	if !s.parts[last].sel.matches(n, st) {
		return false
	}
	// matchLeft reports whether parts[:i] match given parts[i] matched at
	// cur. Descendant and general-sibling steps try EVERY candidate, not
	// just the nearest: in `.menu > li a` the nearest li may be a nested
	// one whose parent isn't .menu while an outer li's is. Child and
	// adjacent steps have one candidate each, so the search only branches
	// on ` ` and `~`, and real selectors are a handful of compounds deep.
	var matchLeft func(i int, cur *Node) bool
	matchLeft = func(i int, cur *Node) bool {
		if i == 0 {
			return anchor == nil || relatedTo(cur, anchor, s.parts[0].comb)
		}
		prev := s.parts[i-1].sel
		switch s.parts[i].comb { // combinator joining parts[i] to parts[i-1]
		case combChild:
			p := cur.Parent
			return p != nil && prev.matches(p, compState(st, p, true)) && matchLeft(i-1, p)
		case combAdjacent:
			sib := prevElementSibling(cur)
			return sib != nil && prev.matches(sib, compState(st, sib, false)) && matchLeft(i-1, sib)
		case combSibling:
			for sib := prevElementSibling(cur); sib != nil; sib = prevElementSibling(sib) {
				if prev.matches(sib, compState(st, sib, false)) && matchLeft(i-1, sib) {
					return true
				}
			}
			return false
		default: // combDescendant
			for p := cur.Parent; p != nil; p = p.Parent {
				if prev.matches(p, compState(st, p, true)) && matchLeft(i-1, p) {
					return true
				}
			}
			return false
		}
	}
	return matchLeft(last, n)
}

// nonSubjectStateGates reports which interactive-state pseudos appear on
// NON-subject compounds (everything left of the rightmost compound), and
// whether the selector uses a sibling combinator anywhere — such selectors
// can be triggered by a preceding sibling's state, so trigger discovery
// must look sideways, not just up.
func (s Selector) nonSubjectStateGates() (hover, focus, active, viaSibling bool) {
	for i, part := range s.parts {
		if i < len(s.parts)-1 {
			for _, ps := range part.sel.pseudos {
				switch ps.kind {
				case pseudoHover:
					hover = true
				case pseudoFocus, pseudoFocusVisible:
					focus = true
				case pseudoActive:
					active = true
				}
			}
		}
		if i > 0 && (part.comb == combAdjacent || part.comb == combSibling) {
			viaSibling = true
		}
	}
	return
}

// usesFocusVisible reports whether the selector's subject compound carries
// :focus-visible (which needs its own per-element variant).
func (s Selector) usesFocusVisible() bool {
	if len(s.parts) == 0 {
		return false
	}
	for _, ps := range s.parts[len(s.parts)-1].sel.pseudos {
		if ps.kind == pseudoFocusVisible {
			return true
		}
	}
	return false
}

// stateGates reports which interactive-state pseudos this selector uses,
// so the sheet knows which per-element variants to compute.
func (s Selector) stateGates() (hover, focus, active bool) {
	for _, part := range s.parts {
		for _, ps := range part.sel.pseudos {
			switch ps.kind {
			case pseudoHover:
				hover = true
			case pseudoFocus:
				focus = true
			case pseudoActive:
				active = true
			}
		}
	}
	return
}

// matches reports whether the compound matches n under interactive state
// st. Unsupported pseudos fail closed (never match) so their rule simply
// doesn't apply — instead of the old fail-open behavior that leaked every
// unrecognized pseudo's rule onto all elements.
func (ss simpleSelector) matches(n *Node, st selectorState) bool {
	if n.Type != ElementNode {
		return false
	}
	if ss.tag != "" && ss.tag != "*" && ss.tag != n.Tag {
		return false
	}
	if ss.id != "" && ss.id != n.ID() {
		return false
	}
	for _, c := range ss.classes {
		if !n.hasClass(c) {
			return false
		}
	}
	for _, a := range ss.attrs {
		if !a.matches(n) {
			return false
		}
	}
	for _, ps := range ss.pseudos {
		if !ps.matches(n, st) {
			return false
		}
	}
	return true
}

func (a attrSelector) matches(n *Node) bool {
	v, ok := n.Attr(a.name)
	if a.ci {
		v = strings.ToLower(v) // a.val was lowered at parse time
	}
	switch a.op {
	case attrExists:
		return ok
	case attrEquals:
		return ok && v == a.val
	case attrPrefix:
		return ok && a.val != "" && strings.HasPrefix(v, a.val)
	case attrSuffix:
		return ok && a.val != "" && strings.HasSuffix(v, a.val)
	case attrSubstr:
		return ok && a.val != "" && strings.Contains(v, a.val)
	case attrWord:
		if !ok || a.val == "" {
			return false
		}
		for _, w := range strings.Fields(v) {
			if w == a.val {
				return true
			}
		}
		return false
	case attrDashHyph:
		return ok && (v == a.val || strings.HasPrefix(v, a.val+"-"))
	}
	return false
}

func (ps pseudoSelector) matches(n *Node, st selectorState) bool {
	switch ps.kind {
	case pseudoHover:
		return st.hover
	case pseudoFocus:
		return st.focus
	case pseudoFocusVisible:
		return st.focus && st.focusVisible
	case pseudoActive:
		return st.active
	case pseudoRoot:
		return isRootElement(n)
	case pseudoFirstChild:
		return elementIndex(n) == 0
	case pseudoLastChild:
		return isLastElementChild(n)
	case pseudoOnlyChild:
		return elementIndex(n) == 0 && isLastElementChild(n)
	case pseudoNthChild:
		return nthMatches(elementIndex(n)+1, ps.a, ps.b) // 1-based position
	case pseudoNthOfType:
		return nthMatches(elementIndexOfType(n)+1, ps.a, ps.b) // 1-based, same-tag
	case pseudoChecked:
		_, ok := n.Attr("checked")
		return ok
	case pseudoDisabled:
		_, ok := n.Attr("disabled")
		return ok
	case pseudoEnabled:
		if !isFormControlNode(n) {
			return false // :enabled only applies to elements that CAN be disabled
		}
		_, disabled := n.Attr("disabled")
		return !disabled
	case pseudoRequired:
		if !isFormControlNode(n) {
			return false
		}
		_, ok := n.Attr("required")
		return ok
	case pseudoOptional:
		if !isFormControlNode(n) {
			return false
		}
		_, ok := n.Attr("required")
		return !ok
	case pseudoReadOnly:
		// Per spec :read-only matches everything that is NOT user-alterable —
		// including ordinary elements, not just locked fields.
		return !isEditableNode(n)
	case pseudoReadWrite:
		return isEditableNode(n)
	case pseudoNthLastChild:
		return nthMatches(elementIndexFromEnd(n)+1, ps.a, ps.b) // 1-based from end
	case pseudoEmpty:
		return isEmptyElement(n)
	case pseudoHas:
		for _, a := range ps.args {
			if hasRelativeMatch(n, a, st) {
				return true
			}
		}
		return false
	case pseudoNot:
		for _, a := range ps.args {
			if a.matchesElement(n, st) {
				return false
			}
		}
		return true
	default: // pseudoUnsupported
		return false
	}
}

// nthMatches reports whether 1-based position pos satisfies An+B.
func nthMatches(pos, a, b int) bool {
	if a == 0 {
		return pos == b
	}
	// pos = a*k + b for some integer k >= 0.
	diff := pos - b
	if diff == 0 {
		return true
	}
	if a > 0 {
		return diff > 0 && diff%a == 0
	}
	return diff < 0 && diff%a == 0
}

// stripComments removes /* ... */ blocks.
func stripComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	// One pass: `/*` inside a string (`content:"/*"`) is text, not a
	// comment — and rebuilding the string per comment was quadratic.
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == '"' || c == '\'' {
			end := stringEnd(s, i)
			b.WriteString(s[i:end])
			i = end
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += 2 + end + 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// stringEnd returns the index just past the CSS string literal opening at
// s[i] (a quote), honoring backslash escapes; len(s) if unterminated.
func stringEnd(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++ // skip the escaped character
		case q:
			return j + 1
		}
	}
	return len(s)
}

// skipString advances past a string literal at the cursor, reporting
// whether there was one — so block scanning never takes a quoted `}` or
// `{` (`content:"}"`) for structure.
func (p *cssParser) skipString() bool {
	if c := p.src[p.pos]; c == '"' || c == '\'' {
		p.pos = stringEnd(p.src, p.pos)
		return true
	}
	return false
}

type cssParser struct {
	src string
	pos int
}

func (p *cssParser) eof() bool { return p.pos >= len(p.src) }

func (p *cssParser) skipSpaces() {
	for !p.eof() && isSpace(p.src[p.pos]) {
		p.pos++
	}
}

func (p *cssParser) readUntil(delim byte) string {
	start := p.pos
	for !p.eof() && p.src[p.pos] != delim {
		if !p.skipString() {
			p.pos++
		}
	}
	return p.src[start:p.pos]
}

// readSelector reads a selector prelude up to the block-opening '{',
// ignoring '{' that appears inside `[...]` (attribute values may contain
// braces). Stops at EOF or the '{'.
func (p *cssParser) readSelector() string {
	start := p.pos
	depth := 0
	for !p.eof() {
		if p.skipString() {
			continue
		}
		c := p.src[p.pos]
		if c == '[' {
			depth++
		} else if c == ']' {
			if depth > 0 {
				depth--
			}
		} else if c == '{' && depth == 0 {
			break
		}
		p.pos++
	}
	return p.src[start:p.pos]
}
