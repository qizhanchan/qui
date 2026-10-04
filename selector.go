package qui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Selector grammar — CSS-attribute style, picked because LLMs already
// know it and parse it more reliably than ad-hoc colon syntax.
//
// Supported forms (all may be combined in a single selector string;
// criteria AND together):
//
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
	if w == nil {
		return nil, nil
	}
	sel, err := parseSelector(selector)
	if err != nil {
		return nil, err
	}
	tree := w.AccessibilityTree()
	nodes := sel.matchNodes(tree)
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
func (w *Window) FindNodes(selector string) ([]*AXNode, error) {
	if w == nil {
		return nil, nil
	}
	sel, err := parseSelector(selector)
	if err != nil {
		return nil, err
	}
	return sel.matchNodes(w.AccessibilityTree()), nil
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
