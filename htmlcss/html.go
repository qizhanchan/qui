package htmlcss

import (
	"strings"

	xhtml "golang.org/x/net/html"
)

// ParseHTML parses an HTML document or fragment into our DOM tree using
// the spec-compliant golang.org/x/net/html tokenizer + tree builder,
// then adapts its nodes into the lightweight *Node this package works
// with. x/net handles the messy parts for us — implicit <html>/<head>/
// <body> insertion, optional-tag closing, entity decoding, malformed
// markup recovery — so the rest of the engine sees a clean tree.
func ParseHTML(src string) *Node { return parseHTML(src, false) }

// parseHTML is ParseHTML, optionally keeping comments (as CommentNode).
func parseHTML(src string, comments bool) *Node {
	doc, err := xhtml.Parse(strings.NewReader(src))
	if err != nil {
		// x/net's Parse is extremely tolerant and effectively never errors
		// on string input; fall back to an empty document just in case.
		return &Node{Type: ElementNode, Tag: "#document", Attrs: map[string]string{}}
	}
	root := &Node{Type: ElementNode, Tag: "#document", Attrs: map[string]string{}}
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if n := adaptNode(c, comments); n != nil {
			root.appendChild(n)
		}
	}
	return root
}

// adaptNode converts an x/net html.Node subtree into our Node, dropping
// comment / doctype nodes.
func adaptNode(x *xhtml.Node, comments bool) *Node {
	switch x.Type {
	case xhtml.ElementNode:
		attrs := make(map[string]string, len(x.Attr))
		order := make([]string, 0, len(x.Attr))
		for _, a := range x.Attr {
			// x/net lowercases HTML attribute keys already; a repeated
			// attribute is dropped (the first one wins, as in a browser).
			if _, dup := attrs[a.Key]; dup {
				continue
			}
			attrs[a.Key] = a.Val
			order = append(order, a.Key)
		}
		n := &Node{Type: ElementNode, Tag: strings.ToLower(x.Data), Attrs: attrs, AttrOrder: order}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if child := adaptNode(c, comments); child != nil {
				n.appendChild(child)
			}
		}
		return n
	case xhtml.TextNode:
		if x.Data == "" {
			return nil
		}
		return &Node{Type: TextNode, Text: x.Data}
	case xhtml.CommentNode:
		if comments {
			return &Node{Type: CommentNode, Text: x.Data}
		}
		return nil
	default:
		// DoctypeNode, CommentNode, DocumentNode-as-child: skip.
		return nil
	}
}

// isSpace reports whether b is HTML whitespace. Shared with the CSS
// parser (css.go).
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}
