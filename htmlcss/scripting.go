package htmlcss

import (
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
)

// The scripting surface: what a script engine (q-browser's DOM bindings)
// needs to treat the live El tree as a DOM — tree navigation, attribute
// listing, selector matching, fragment parsing. Mutations go through the
// existing setters (SetAttr, SetElementChildren, SetTextContent), which
// keep the node mirrors and the styling in step.

// ParentEl returns the element's parent element (nil at a root).
func (e *El) ParentEl() *El { return e.elParent }

// Kids returns a copy of the element's children (elements and text
// segments) in document order.
func (e *El) Kids() []qui.Widget { return append([]qui.Widget(nil), e.elementKids...) }

// IsText reports whether e is an anonymous text segment (a DOM text node).
func (e *El) IsText() bool { return e.isTextSeg() }

// AttrNames lists the element's attribute names in DOM order (source
// order for parsed markup, then insertion order).
func (e *El) AttrNames() []string {
	e.attrMu.RLock()
	defer e.attrMu.RUnlock()
	if len(e.attrOrder) == len(e.attrs) {
		return append([]string(nil), e.attrOrder...)
	}
	out := make([]string, 0, len(e.attrs))
	for k := range e.attrs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Engine returns the style engine the element belongs to.
func (e *El) Engine() *StyleEngine { return e.engine }

// CompileSelectors parses a selector list for Matches; ok is false when it
// does not parse (DOM APIs then throw a SyntaxError).
func CompileSelectors(sel string) (SelectorList, bool) {
	list := parseSelectorList(sel)
	if len(list) == 0 && strings.TrimSpace(sel) != "" {
		return nil, false
	}
	return list, len(list) > 0
}

// SelectorList is a compiled selector list.
type SelectorList []Selector

// Matches reports whether e (an element, not a text segment) matches any
// selector of the list, with its current interactive state.
func (l SelectorList) Matches(e *El) bool {
	if e == nil || e.isTextSeg() || e.tag == "#comment" || e.node == nil {
		return false
	}
	st := selectorState{hover: e.Hovering(), focus: e.Focused(), active: e.Pressed()}
	for _, s := range l {
		if s.matches(e.node, st) {
			return true
		}
	}
	return false
}

// QueryAll returns the descendants of root (not root itself) that match,
// in document order; limit > 0 stops after that many.
func (l SelectorList) QueryAll(root *El, limit int) []*El {
	var out []*El
	var walk func(e *El) bool
	walk = func(e *El) bool {
		for _, k := range e.elementKids {
			ke, ok := k.(*El)
			if !ok || ke.isTextSeg() {
				continue
			}
			if l.Matches(ke) {
				out = append(out, ke)
				if limit > 0 && len(out) >= limit {
					return false
				}
			}
			if !walk(ke) {
				return false
			}
		}
		return true
	}
	if root != nil {
		walk(root)
	}
	return out
}

// ParseFragment builds the nodes of an HTML fragment (innerHTML) as
// elements and text segments of this engine, not yet attached anywhere.
// Scripts are not built (a script inserted through innerHTML never runs).
func (eng *StyleEngine) ParseFragment(src string) []qui.Widget {
	doc := parseHTML("<body>"+src+"</body>", eng.opts.ScriptDOM)
	body := findTag(doc, "body")
	if body == nil {
		return nil
	}
	var out []qui.Widget
	for _, c := range body.Children {
		switch c.Type {
		case TextNode:
			out = append(out, newTextEl(c.Text, eng))
		case ElementNode:
			if k := eng.buildStaticEl(c, nil); k != nil {
				out = append(out, k)
			}
		case CommentNode:
			out = append(out, newCommentEl(c.Text, eng))
		}
	}
	return out
}

// NewCommentEl creates a detached comment node (document.createComment).
func (eng *StyleEngine) NewCommentEl(text string) *El { return newCommentEl(text, eng) }

// IsComment reports whether e is a comment node.
func (e *El) IsComment() bool { return e.tag == "#comment" }

// MediaMatches evaluates a media query list against the current viewport
// (window.matchMedia).
func MediaMatches(query string, viewportW float32) bool {
	for _, q := range strings.Split(query, ",") {
		if mediaQueryMatches(strings.TrimSpace(strings.ToLower(q)), viewportW) {
			return true
		}
	}
	return false
}
