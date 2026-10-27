package qui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// Selector grammar — CSS-attribute style, picked because LLMs already
// know it and parse it more reliably than ad-hoc colon syntax.
//
// Supported forms (all may be combined in a single selector string;
// criteria AND together):
//
//	@123                      — match by NodeID (process-wide identity;
//	                            see inspect_nodeid.go). A reference that matches
//	                            nothing is stale: actions report
//	                            ErrStaleNode
//	#id                       — match by Window-scoped ID
//	[role=button]             — match by Role string (exact)
//	[name="Save"]             — match by accessible name (exact)
//	[name*="Save"]            — match by accessible name (substring)
//	[name^="Save"]            — match by accessible name (prefix)
//	[name$="Save"]            — match by accessible name (suffix)
//	[text=...] / [text*=...]  — alias for name (text content)
//	[key=qui.save]            — match by message key (locale-independent)
//	[value=...] / [value*=...]— match by AccessibleValue
//	[id=...]                  — same as #...; redundant but valid
//	:focused                  — currently-focused widget only
//	:visible                  — Bounds non-empty
//	:hidden                   — Bounds empty (collapsed Tabs etc.)
//	:nth(n)                   — Nth match (0-indexed) among current
//	                            candidates AFTER all other filters
//	:layer(root|modal|topmost)— restrict to a single AX subtree
//	                              root    → main tree
//	                              modal   → topmost modal overlay
//	                              topmost → topmost overlay (any kind)
//	A, B                      — selector list: the union, in tree order;
//	                            each alternative is resolved on its own
//	                            (widget grammar, else CSS)
//
// Attribute values support double-quoted strings with backslash
// escapes (\" and \\); unquoted values are read up to the next ']'.
// Whitespace between criteria is permitted and ignored.
//
// Examples:
//
//	#submit
//	[role=button][name="Save"]
//	[role=button][key=qui.save]
//	[role=textbox]:nth(1)
//	[role=button]:visible
//	:layer(modal) [role=button][key=qui.ok]
//	:focused
//
// Localized apps: [name=] matches the rendered caption, which changes
// with the language. Scripts meant to survive a locale switch should
// target #id or [key=], both of which are language-independent.
//
// Find returns the first match in tree order (depth-first, main tree
// then overlays bottom-to-top). FindAll returns all matches. Both
// return nil / empty slice when nothing matches; explicit errors are
// reserved for malformed selectors — including the EMPTY selector,
// which would otherwise match every node and hand callers the root.

// ErrInvalidSelector is returned when the selector string fails to
// parse. The error message includes the offending position.
var ErrInvalidSelector = errors.New("qui: invalid selector")

// Find returns the first widget matching the selector. Returns nil
// if nothing matches. Returns nil and an ErrInvalidSelector-wrapped
// error if the selector is malformed.
func (w *Window) Find(selector string) (Widget, error) {
	matches, err := w.FindAll(selector)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return matches[0], nil
}

// FindAll returns every widget matching the selector in tree order.
// Returns an empty slice if nothing matches.
func (w *Window) FindAll(selector string) ([]Widget, error) {
	nodes, err := w.FindNodes(selector)
	if err != nil {
		return nil, err
	}
	out := make([]Widget, 0, len(nodes))
	for _, n := range nodes {
		if n.widget != nil {
			out = append(out, n.widget)
		}
	}
	return out, nil
}

// FindNodes is like FindAll but returns the matched AXNodes
// directly. Useful for the agent layer where path/role/name are
// already serialized.
//
// A selector the widget grammar cannot parse — a tag, a .class, a
// combinator — goes to the registered SelectorEngine (htmlcss registers its
// CSS engine), so one selector string addresses both surfaces. The rule is
// deterministic: whatever the widget grammar accepts means what it means
// there (#id is the widget ID, [name=] the accessible name).
func (w *Window) FindNodes(selector string) ([]*AXNode, error) {
	if w == nil {
		return nil, nil
	}
	return w.findNodesIn(w.AccessibilityTree(), selector)
}

func (w *Window) findNodesIn(tree *AccessibilityTree, selector string) ([]*AXNode, error) {
	// A selector list resolves alternative by alternative, each under the
	// rule above, so "[role=button][name=A], [role=button][name=B]" keeps
	// [name=] the accessible name instead of the whole string falling
	// through to CSS, where [name=] is the HTML attribute.
	if parts := splitSelectorList(selector); len(parts) > 1 {
		var lists [][]*AXNode
		for _, part := range parts {
			nodes, err := w.findNodesIn(tree, part)
			if err != nil {
				return nil, err
			}
			lists = append(lists, nodes)
		}
		return unionInTreeOrder(tree, lists), nil
	}
	sel, err := parseSelector(selector)
	if err == nil {
		return sel.matchNodes(tree), nil
	}
	if !errors.Is(err, ErrInvalidSelector) || strings.TrimSpace(selector) == "" {
		return nil, err
	}
	engine := selectorEngine.Load()
	if engine == nil {
		return nil, err
	}
	nodes, cssErr := (*engine)(w, tree, selector)
	if cssErr != nil {
		return nil, fmt.Errorf("%w: not a widget selector (%v) nor a CSS selector (%v)", ErrInvalidSelector, err, cssErr)
	}
	return nodes, nil
}

// findNodesCSSIn resolves selector as CSS only — the contract of the CDP
// domains (DOM.querySelector, Overlay.highlightNode), where [name=] is the
// HTML attribute and a client such as Puppeteer expects exactly that. The
// widget grammar answers only the forms that mean the same in CSS (#id,
// [id=…]: the DOM projection exports the widget ID as the id attribute), so
// native widgets the CSS engine does not project stay addressable by id;
// everything else goes to the CSS engine.
func (w *Window) findNodesCSSIn(tree *AccessibilityTree, selector string) ([]*AXNode, error) {
	if parts := splitSelectorList(selector); len(parts) > 1 {
		var lists [][]*AXNode
		for _, part := range parts {
			nodes, err := w.findNodesCSSIn(tree, part)
			if err != nil {
				return nil, err
			}
			lists = append(lists, nodes)
		}
		return unionInTreeOrder(tree, lists), nil
	}
	sel, err := parseSelector(selector)
	if err == nil && sel.cssEquivalent() {
		return sel.matchNodes(tree), nil
	}
	if strings.TrimSpace(selector) == "" {
		return nil, fmt.Errorf("%w: empty selector", ErrInvalidSelector)
	}
	engine := selectorEngine.Load()
	if engine == nil {
		return nil, fmt.Errorf("%w: %q: this window has no CSS engine; for the widget grammar ([role=…][name=…], @N) use Agent.query", ErrInvalidSelector, selector)
	}
	nodes, cssErr := (*engine)(w, tree, selector)
	if cssErr != nil {
		if err == nil {
			return nil, fmt.Errorf("%w: %q is qui's widget grammar, not CSS; use Agent.query (%v)", ErrInvalidSelector, selector, cssErr)
		}
		return nil, cssErr
	}
	return nodes, nil
}

// cssEquivalent reports whether the selector uses only forms that mean
// the same in the widget grammar and in CSS: #id and [id=…].
func (s *compiledSelector) cssEquivalent() bool {
	if s.nodeID != 0 || s.wantFocused || s.wantVisible || s.wantHidden || s.layer != "" || s.hasNth {
		return false
	}
	for _, a := range s.attrs {
		if a.field != "id" {
			return false
		}
	}
	return s.idEquals != "" || len(s.attrs) > 0
}

// splitSelectorList splits a selector at its top-level commas — outside
// quotes, [...] and (...). A selector without one comes back as nil.
func splitSelectorList(s string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '(':
			depth++
		case c == ']' || c == ')':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if parts == nil {
		return nil
	}
	return append(parts, strings.TrimSpace(s[start:]))
}

// unionInTreeOrder merges match lists without duplicates, in tree order
// (main tree, then overlays bottom-to-top), as one CSS selector list does.
func unionInTreeOrder(tree *AccessibilityTree, lists [][]*AXNode) []*AXNode {
	want := map[*AXNode]bool{}
	for _, l := range lists {
		for _, n := range l {
			want[n] = true
		}
	}
	out := make([]*AXNode, 0, len(want))
	if tree != nil {
		for _, n := range tree.flatten() {
			if want[n] {
				out = append(out, n)
				delete(want, n)
			}
		}
	}
	// A CSS engine may hand back nodes the flattened tree does not hold;
	// keep them, in the order they were matched.
	for _, l := range lists {
		for _, n := range l {
			if want[n] {
				out = append(out, n)
				delete(want, n)
			}
		}
	}
	return out
}

// FindNodesSynced is FindNodes for callers off the UI goroutine (the
// agent's protocol handlers): capture and matching both run on the UI
// thread, since a CSS selector engine reads live elements.
func (w *Window) FindNodesSynced(selector string) ([]*AXNode, error) {
	if w == nil {
		return nil, nil
	}
	var nodes []*AXNode
	err := w.synchronously(func() error {
		var err error
		nodes, err = w.FindNodes(selector)
		return err
	})
	return nodes, err
}

// SelectorEngine resolves a selector the widget grammar rejects against the
// window's already-captured AX tree, returning the matched nodes in document
// order. It runs on the UI goroutine. Return an ErrInvalidSelector-wrapped
// error when the selector is not valid in this engine's grammar either.
type SelectorEngine func(w *Window, tree *AccessibilityTree, selector string) ([]*AXNode, error)

var selectorEngine atomic.Pointer[SelectorEngine]

// RegisterSelectorEngine installs the fallback engine FindNodes (and every
// selector-addressed action) consults for selectors outside the widget
// grammar. htmlcss registers its CSS engine from init; the last registration
// wins.
func RegisterSelectorEngine(fn SelectorEngine) {
	if fn == nil {
		selectorEngine.Store(nil)
		return
	}
	selectorEngine.Store(&fn)
}

// noMatchFor is the error for a selector that matched nothing: a pure node
// reference ("@123") that misses is a stale handle, which an agent must
// re-resolve rather than retry.
func noMatchFor(selector string) error {
	if _, ok := ParseNodeRef(strings.TrimSpace(selector)); ok {
		return fmt.Errorf("%w: %s", ErrStaleNode, strings.TrimSpace(selector))
	}
	return ErrNoMatch
}

// -------------------------------------------------------------------
// Selector AST + matching

type matchOp int

const (
	opEqual matchOp = iota
	opContains
	opPrefix
	opSuffix
)

type attrPredicate struct {
	field string // "id" | "role" | "name" | "value"
	op    matchOp
	value string
}

type compiledSelector struct {
	nodeID      NodeID // from @123; 0 = no reference
	idEquals    string // from #foo or [id=foo]
	attrs       []attrPredicate
	wantFocused bool
	wantVisible bool
	wantHidden  bool
	layer       string // "" | "root" | "modal" | "topmost"
	nth         int    // -1 if no :nth(n) filter
	hasNth      bool
}

func (s *compiledSelector) matchNodes(tree *AccessibilityTree) []*AXNode {
	if tree == nil {
		return nil
	}
	// Choose the candidate node set based on :layer(...)
	var candidates []*AXNode
	switch s.layer {
	case "root":
		candidates = flattenAX(tree.Root)
	case "modal":
		layerName := tree.topmostModalLayer()
		if layerName == "" {
			return nil
		}
		for _, ov := range tree.Overlays {
			if ov.Layer == layerName {
				candidates = flattenAX(ov)
				break
			}
		}
	case "topmost":
		if len(tree.Overlays) == 0 {
			candidates = flattenAX(tree.Root)
		} else {
			candidates = flattenAX(tree.Overlays[len(tree.Overlays)-1])
		}
	default:
		candidates = tree.flatten()
	}

	out := make([]*AXNode, 0, 8)
	for _, n := range candidates {
		if !s.matchOne(n) {
			continue
		}
		out = append(out, n)
	}
	if s.hasNth {
		if s.nth < 0 || s.nth >= len(out) {
			return nil
		}
		return []*AXNode{out[s.nth]}
	}
	return out
}

func (s *compiledSelector) matchOne(n *AXNode) bool {
	if n == nil {
		return false
	}
	if s.nodeID != 0 && n.NodeID != s.nodeID {
		return false
	}
	if s.idEquals != "" && n.ID != s.idEquals {
		return false
	}
	for _, a := range s.attrs {
		var got string
		switch a.field {
		case "id":
			got = n.ID
		case "role":
			got = n.Role
		case "name", "text":
			got = n.Name
		case "key":
			got = n.NameKey
		case "value":
			got = n.Value
		default:
			return false
		}
		switch a.op {
		case opEqual:
			if got != a.value {
				return false
			}
		case opContains:
			if !strings.Contains(got, a.value) {
				return false
			}
		case opPrefix:
			if !strings.HasPrefix(got, a.value) {
				return false
			}
		case opSuffix:
			if !strings.HasSuffix(got, a.value) {
				return false
			}
		}
	}
	if s.wantFocused && !strings.Contains(n.State, "focused") {
		return false
	}
	if s.wantVisible && !n.Visible {
		return false
	}
	if s.wantHidden && n.Visible {
		return false
	}
	return true
}

// -------------------------------------------------------------------
// Parser

func parseSelector(s string) (*compiledSelector, error) {
	// A selector with no criteria would match EVERY node, so callers that
	// pass an empty string — an agent request that misnames the `target`
	// field, a UI that forwards an empty search box — would silently act
	// on the root widget. There is no "match everything" use case worth
	// that failure mode; ask for it with `:visible` / `:layer(root)`.
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("%w: empty selector", ErrInvalidSelector)
	}
	p := &selectorParser{src: s, pos: 0}
	out := &compiledSelector{nth: -1}
	for {
		p.skipWS()
		if p.eof() {
			break
		}
		c := p.peek()
		switch c {
		case '@':
			start := p.pos
			p.pos++
			n, err := p.readInt()
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("%w: node reference at pos %d must be @<positive id>", ErrInvalidSelector, start)
			}
			if out.nodeID != 0 && out.nodeID != NodeID(n) {
				return nil, fmt.Errorf("%w: conflicting node references", ErrInvalidSelector)
			}
			out.nodeID = NodeID(n)
		case '#':
			p.pos++
			id, err := p.readIdent()
			if err != nil {
				return nil, err
			}
			if out.idEquals != "" && out.idEquals != id {
				return nil, fmt.Errorf("%w: conflicting ID predicates", ErrInvalidSelector)
			}
			out.idEquals = id
		case '[':
			pred, err := p.readAttr()
			if err != nil {
				return nil, err
			}
			if pred.field == "id" && pred.op == opEqual {
				if out.idEquals != "" && out.idEquals != pred.value {
					return nil, fmt.Errorf("%w: conflicting ID predicates", ErrInvalidSelector)
				}
				out.idEquals = pred.value
			} else {
				out.attrs = append(out.attrs, pred)
			}
		case ':':
			p.pos++
			name, err := p.readIdent()
			if err != nil {
				return nil, err
			}
			switch name {
			case "focused":
				out.wantFocused = true
			case "visible":
				out.wantVisible = true
			case "hidden":
				out.wantHidden = true
			case "nth":
				if !p.consume('(') {
					return nil, fmt.Errorf("%w: :nth requires (N)", ErrInvalidSelector)
				}
				idx, err := p.readInt()
				if err != nil {
					return nil, err
				}
				if !p.consume(')') {
					return nil, fmt.Errorf("%w: :nth missing closing paren", ErrInvalidSelector)
				}
				out.nth = idx
				out.hasNth = true
			case "layer":
				if !p.consume('(') {
					return nil, fmt.Errorf("%w: :layer requires (root|modal|topmost)", ErrInvalidSelector)
				}
				val, err := p.readIdent()
				if err != nil {
					return nil, err
				}
				switch val {
				case "root", "modal", "topmost":
					out.layer = val
				default:
					return nil, fmt.Errorf("%w: :layer must be root|modal|topmost, got %q", ErrInvalidSelector, val)
				}
				if !p.consume(')') {
					return nil, fmt.Errorf("%w: :layer missing closing paren", ErrInvalidSelector)
				}
			default:
				return nil, fmt.Errorf("%w: unknown pseudo :%s", ErrInvalidSelector, name)
			}
		default:
			return nil, fmt.Errorf("%w: unexpected %q at pos %d", ErrInvalidSelector, c, p.pos)
		}
	}
	return out, nil
}

type selectorParser struct {
	src string
	pos int
}

func (p *selectorParser) eof() bool  { return p.pos >= len(p.src) }
func (p *selectorParser) peek() byte { return p.src[p.pos] }
func (p *selectorParser) consume(c byte) bool {
	if p.eof() || p.src[p.pos] != c {
		return false
	}
	p.pos++
	return true
}
func (p *selectorParser) skipWS() {
	for !p.eof() {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *selectorParser) readIdent() (string, error) {
	start := p.pos
	for !p.eof() {
		c := p.src[p.pos]
		isAlpha := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
		isDigit := c >= '0' && c <= '9'
		if !(isAlpha || isDigit || c == '-' || c == '_') {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("%w: expected identifier at pos %d", ErrInvalidSelector, p.pos)
	}
	return p.src[start:p.pos], nil
}

func (p *selectorParser) readInt() (int, error) {
	start := p.pos
	if !p.eof() && (p.src[p.pos] == '-' || p.src[p.pos] == '+') {
		p.pos++
	}
	for !p.eof() {
		c := p.src[p.pos]
		if c < '0' || c > '9' {
			break
		}
		p.pos++
	}
	if p.pos == start {
		return 0, fmt.Errorf("%w: expected integer at pos %d", ErrInvalidSelector, p.pos)
	}
	return strconv.Atoi(p.src[start:p.pos])
}

// readAttr parses one [field op value] predicate. Called with p.pos
// at the opening '['.
func (p *selectorParser) readAttr() (attrPredicate, error) {
	if !p.consume('[') {
		return attrPredicate{}, fmt.Errorf("%w: expected '[' at pos %d", ErrInvalidSelector, p.pos)
	}
	p.skipWS()
	field, err := p.readIdent()
	if err != nil {
		return attrPredicate{}, err
	}
	p.skipWS()
	op := opEqual
	switch {
	case p.eof():
		return attrPredicate{}, fmt.Errorf("%w: unexpected end of selector inside attribute", ErrInvalidSelector)
	case p.src[p.pos] == '=':
		p.pos++
	case p.src[p.pos] == '*' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
		op = opContains
		p.pos += 2
	case p.src[p.pos] == '^' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
		op = opPrefix
		p.pos += 2
	case p.src[p.pos] == '$' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '=':
		op = opSuffix
		p.pos += 2
	default:
		return attrPredicate{}, fmt.Errorf("%w: expected =, *=, ^=, or $= inside attribute at pos %d", ErrInvalidSelector, p.pos)
	}
	p.skipWS()
	value, err := p.readAttrValue()
	if err != nil {
		return attrPredicate{}, err
	}
	p.skipWS()
	if !p.consume(']') {
		return attrPredicate{}, fmt.Errorf("%w: expected ']' at pos %d", ErrInvalidSelector, p.pos)
	}
	return attrPredicate{field: strings.ToLower(field), op: op, value: value}, nil
}

func (p *selectorParser) readAttrValue() (string, error) {
	if p.eof() {
		return "", fmt.Errorf("%w: missing attribute value", ErrInvalidSelector)
	}
	if p.src[p.pos] == '"' {
		p.pos++
		var sb strings.Builder
		for !p.eof() {
			c := p.src[p.pos]
			if c == '\\' && p.pos+1 < len(p.src) {
				sb.WriteByte(p.src[p.pos+1])
				p.pos += 2
				continue
			}
			if c == '"' {
				p.pos++
				return sb.String(), nil
			}
			sb.WriteByte(c)
			p.pos++
		}
		return "", fmt.Errorf("%w: unterminated quoted attribute value", ErrInvalidSelector)
	}
	start := p.pos
	for !p.eof() {
		c := p.src[p.pos]
		if c == ']' || c == ' ' || c == '\t' {
			break
		}
		p.pos++
	}
	return p.src[start:p.pos], nil
}
