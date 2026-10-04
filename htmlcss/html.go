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
func ParseHTML(src string) *Node {
	doc, err := xhtml.Parse(strings.NewReader(src))
	if err != nil {
		// x/net's Parse is extremely tolerant and effectively never errors
		// on string input; fall back to an empty document just in case.
		return &Node{Type: ElementNode, Tag: "#document", Attrs: map[string]string{}}
	}
	root := &Node{Type: ElementNode, Tag: "#document", Attrs: map[string]string{}}
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if n := adaptNode(c); n != nil {
			root.appendChild(n)
		}
	}
	return root
}

// adaptNode converts an x/net html.Node subtree into our Node, dropping
// comment / doctype nodes.
func adaptNode(x *xhtml.Node) *Node {
	switch x.Type {
	case xhtml.ElementNode:
		attrs := make(map[string]string, len(x.Attr))
		for _, a := range x.Attr {
			// x/net lowercases HTML attribute keys already; last wins.
			attrs[a.Key] = a.Val
		}
		n := &Node{Type: ElementNode, Tag: strings.ToLower(x.Data), Attrs: attrs}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			if child := adaptNode(c); child != nil {
				n.appendChild(child)
			}
		}
		return n
	case xhtml.TextNode:
		if x.Data == "" {
			return nil
		}
		return &Node{Type: TextNode, Text: x.Data}
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
