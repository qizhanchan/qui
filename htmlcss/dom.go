// Package htmlcss is a lightweight HTML5 + CSS engine for qui: it parses
// real HTML markup and CSS text, resolves computed styles (cascade +
// inheritance), and builds a retained qui widget tree that lays out and
// paints through the framework's normal Measure/Layout/Draw path.
//
// Scope: a practical subset aimed at rendering real-looking
// pages, not full spec compliance. Layout maps CSS `display` onto qui's
// engines — block → FlowLayout (normal flow), flex → FlexLayout, inline
// runs of text are folded into a single rich-text Label. See the package
// files for the exact property + selector coverage.
//
// Import direction: depends on root qui + widgets only.
package htmlcss

import "strings"

// NodeType distinguishes element nodes from text nodes.
type NodeType uint8

const (
	ElementNode NodeType = iota
	TextNode
)

// Node is a single DOM node. Elements carry a Tag, Attrs, and Children;
// text nodes carry Text. The tree is built by ParseHTML.
type Node struct {
	Type     NodeType
	Tag      string // lowercase tag name for elements ("" for text)
	Attrs    map[string]string
	Text     string // text content for TextNode
	Children []*Node
	Parent   *Node
}

// Attr returns the attribute value and whether it was present.
func (n *Node) Attr(name string) (string, bool) {
	if n.Attrs == nil {
		return "", false
	}
	v, ok := n.Attrs[name]
	return v, ok
}

// AttrOr returns the attribute value or a default.
func (n *Node) AttrOr(name, def string) string {
	if v, ok := n.Attr(name); ok {
		return v
	}
	return def
}

// Classes returns the whitespace-split class list.
func (n *Node) Classes() []string {
	c, ok := n.Attr("class")
	if !ok {
		return nil
	}
	return strings.Fields(c)
}

// ID returns the element id attribute.
func (n *Node) ID() string { return n.AttrOr("id", "") }

// hasClass reports whether the node carries the given class.
func (n *Node) hasClass(name string) bool {
	for _, c := range n.Classes() {
		if c == name {
			return true
		}
	}
	return false
}

// isElement reports whether n is an element with the given tag.
func (n *Node) isElement(tag string) bool {
	return n.Type == ElementNode && n.Tag == tag
}

// appendChild links a child and sets its parent pointer.
func (n *Node) appendChild(c *Node) {
	c.Parent = n
	n.Children = append(n.Children, c)
}

// prevElementSibling returns the nearest preceding sibling that is an
// element (text nodes are skipped, matching CSS sibling combinators
// `+`/`~`). Returns nil when n has no parent or no element precedes it.
func prevElementSibling(n *Node) *Node {
	if n == nil || n.Parent == nil {
		return nil
	}
	sibs := n.Parent.Children
	idx := -1
	for i, c := range sibs {
		if c == n {
			idx = i
			break
		}
	}
	for i := idx - 1; i >= 0; i-- {
		if sibs[i].Type == ElementNode {
			return sibs[i]
		}
	}
	return nil
}

// elementIndex returns n's 0-based position among its element siblings
// (text nodes don't count), or 0 when it has no parent. Used by
// :first-child / :nth-child evaluation.
func elementIndex(n *Node) int {
	if n == nil || n.Parent == nil {
		return 0
	}
	idx := 0
	for _, c := range n.Parent.Children {
		if c.Type != ElementNode {
			continue
		}
		if c == n {
			return idx
		}
		idx++
	}
	return idx
}

// elementIndexOfType returns n's 0-based position among its element siblings
// that share its tag (text nodes and other tags don't count), or 0 when it
// has no parent. Used by :nth-of-type evaluation.
func elementIndexOfType(n *Node) int {
	if n == nil || n.Parent == nil {
		return 0
	}
	idx := 0
	for _, c := range n.Parent.Children {
		if c.Type != ElementNode || c.Tag != n.Tag {
			continue
		}
		if c == n {
			return idx
		}
		idx++
	}
	return idx
}

// elementIndexFromEnd returns n's 0-based position among its element siblings
// counting from the last (used by :nth-last-child).
func elementIndexFromEnd(n *Node) int {
	if n == nil || n.Parent == nil {
		return 0
	}
	idx := 0
	sibs := n.Parent.Children
	for i := len(sibs) - 1; i >= 0; i-- {
		if sibs[i].Type != ElementNode {
			continue
		}
		if sibs[i] == n {
			return idx
		}
		idx++
	}
	return idx
}

// isFormControlNode reports whether n is a form control — the element set
// that :enabled / :disabled / :required / :optional apply to. <button> and
// <fieldset> can be disabled too, so they count; a <div> never does.
func isFormControlNode(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	switch n.Tag {
	case "input", "select", "textarea", "button", "fieldset", "optgroup", "option":
		return true
	}
	return false
}

// isEditableNode reports whether the user can alter n's value — the CSS
// :read-write condition. That means a text-entry control that is neither
// `readonly` nor `disabled`, or any element with `contenteditable`.
//
// Note the asymmetry with :read-only, which per spec matches everything
// ELSE, ordinary elements included: a <p> is read-only, not read-write.
func isEditableNode(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	if ce, ok := n.Attr("contenteditable"); ok && strings.ToLower(strings.TrimSpace(ce)) != "false" {
		return true
	}
	switch n.Tag {
	case "textarea":
	case "input":
		// Only text-entry types are read-write; a checkbox or a button is
		// altered by activation, not by typing.
		switch strings.ToLower(n.AttrOr("type", "text")) {
		case "", "text", "password", "email", "search", "tel", "url", "number",
			"date", "time", "datetime-local", "month", "week":
		default:
			return false
		}
	default:
		return false
	}
	if _, ro := n.Attr("readonly"); ro {
		return false
	}
	if _, dis := n.Attr("disabled"); dis {
		return false
	}
	return true
}

// isEmptyElement reports whether n has no element children and no non-whitespace
// text (matches CSS :empty).
func isEmptyElement(n *Node) bool {
	if n == nil {
		return false
	}
	for _, c := range n.Children {
		if c.Type == ElementNode {
			return false
		}
		if c.Type == TextNode && strings.TrimSpace(c.Text) != "" {
			return false
		}
	}
	return true
}

// hasMatchingDescendant reports whether any descendant element of n matches the
// simple selector (CSS :has, restricted to a single simple descendant selector).
func hasMatchingDescendant(n *Node, sel *simpleSelector, st selectorState) bool {
	var walk func(*Node) bool
	walk = func(p *Node) bool {
		for _, c := range p.Children {
			if c.Type != ElementNode {
				continue
			}
			if sel.matches(c, st) {
				return true
			}
			if walk(c) {
				return true
			}
		}
		return false
	}
	return walk(n)
}

// isRootElement reports whether n is the document root element (matches
// the CSS `:root` pseudo-class — normally <html>). True when the node has
// no element parent (its parent is nil or the synthetic #document node).
func isRootElement(n *Node) bool {
	if n == nil || n.Type != ElementNode {
		return false
	}
	return n.Parent == nil || n.Parent.Type != ElementNode || n.Parent.Tag == "#document"
}

// isLastElementChild reports whether n is the final element child of its
// parent (trailing text/whitespace nodes don't count). True for an
// orphan node (no parent).
func isLastElementChild(n *Node) bool {
	if n == nil || n.Parent == nil {
		return true
	}
	sibs := n.Parent.Children
	for i := len(sibs) - 1; i >= 0; i-- {
		if sibs[i].Type == ElementNode {
			return sibs[i] == n
		}
	}
	return false
}
