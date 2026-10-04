package htmlcss

import (
	"strings"

	"github.com/qizhanchan/qui"
)

// Static compilation: the one-shot Render path builds a live *El tree
// from a parsed DOM and restyles it once through the same StyleEngine
// the reactive path uses. There is ONE widget-assembly implementation —
// El.applyComputed / El.buildFlow — for both paths; this file only maps
// DOM structure onto El structure (attributes, text segments, form
// content) and skips statically-dead nodes.

// nonRenderedTags never produce a widget.
var nonRenderedTags = map[string]bool{
	"script": true, "style": true, "head": true, "meta": true,
	"link": true, "title": true,
}

// buildStaticEl compiles element node n (and its subtree) into an El
// tree. styles is the resolved style map for the ORIGINAL DOM — used
// only to skip display:none subtrees at compile time (the Els carry
// their own node mirrors for live restyling afterwards).
func (eng *StyleEngine) buildStaticEl(n *Node, styles map[*Node]*ComputedStyle) *El {
	if n.Type != ElementNode || nonRenderedTags[n.Tag] {
		return nil
	}
	// display:none subtrees are skipped at compile time — except <colgroup>/
	// <col>, whose UA display is none but whose width hints buildTable reads
	// off the DOM node mirror (they build as childless, non-rendering Els so
	// their nodes land in the table's node.Children).
	if cs := styles[n]; cs != nil && cs.Display == "none" && n.Tag != "colgroup" && n.Tag != "col" {
		return nil
	}

	e := newEl(n.Tag, eng)
	for k, v := range n.Attrs {
		e.storeAttr(k, v)
		e.node.Attrs[k] = v
	}
	if id := n.ID(); id != "" {
		e.SetID(id)
	}

	// Form content maps onto the El control model.
	switch n.Tag {
	case "textarea":
		if txt := strings.TrimSpace(textContent(n)); txt != "" {
			e.storeAttr("value", txt)
		}
		return e
	case "select":
		e.readOptions(n)
		return e
	case "input":
		if _, ok := n.Attr("checked"); ok {
			e.checked = true
		}
		// Autocomplete list. Resolved from the ORIGINAL parsed DOM (not the
		// El's node mirror): the <datalist> is display:none, so compile-time
		// pruning keeps it out of the El tree entirely.
		e.suggestions = datalistSuggestions(n)
		return e
	case "img", "br", "hr":
		return e
	}

	// Pure-text container → the cheap Label leaf path.
	elemKids := 0
	for _, c := range n.Children {
		if c.Type == ElementNode && !nonRenderedTags[c.Tag] {
			elemKids++
		}
	}
	if elemKids == 0 {
		e.text = collapseFor(textContent(n), styles[n])
		return e
	}

	// Mixed content: text nodes become anonymous text segments so they
	// keep their position among the element children (El.buildFlow folds
	// them into anonymous inline runs). Whitespace-only text nodes are
	// kept only when the element has inline-ish content at all — they
	// are the word gaps between adjacent inline pieces; between blocks
	// they'd only leak empty labels.
	hasInlineish := false
	for _, c := range n.Children {
		if c.Type == TextNode && collapseText(c.Text) != "" {
			hasInlineish = true
			break
		}
		if c.Type == ElementNode && textLikeInline[c.Tag] {
			hasInlineish = true
			break
		}
	}
	var kids []qui.Widget
	for _, c := range n.Children {
		switch c.Type {
		case TextNode:
			if !hasInlineish && collapseText(c.Text) == "" {
				continue
			}
			kids = append(kids, newTextEl(c.Text, eng))
		case ElementNode:
			if k := eng.buildStaticEl(c, styles); k != nil {
				kids = append(kids, k)
			}
		}
	}
	e.SetElementChildren(kids)
	return e
}

// collectStaticIndexes walks the ELEMENT tree filling the id / class
// lookup maps (first-in-document-order wins for duplicate ids). Every
// element appears — including inline pieces that fold into a text run;
// those are addressable El objects even though they don't materialize
// as standalone boxes.
func collectStaticIndexes(e *El, byID map[string]qui.Widget, byClass map[string][]qui.Widget) {
	if e == nil || e.isTextSeg() {
		return
	}
	if id := e.attrs["id"]; id != "" {
		if _, dup := byID[id]; !dup {
			byID[id] = e
		}
	}
	for _, c := range strings.Fields(e.attrs["class"]) {
		byClass[c] = append(byClass[c], e)
	}
	for _, k := range e.elementKids {
		if ke, ok := k.(*El); ok {
			collectStaticIndexes(ke, byID, byClass)
		}
	}
}

// --- structural predicates, shared with other builders ---
//
// The reactive/html template compiler maps markup onto El trees the same
// way buildStaticEl does. These three predicates are the rules it has to
// agree on; exporting them keeps ONE table per rule rather than a copy
// that silently drifts.

// CollapseText collapses runs of whitespace to single spaces and trims,
// the CSS `white-space: normal` rule. A text node is insignificant
// exactly when this returns "".
func CollapseText(s string) string { return collapseText(s) }

// IsInlineTag reports whether tag is one of the text-like inline elements
// (span, b, em, a, …) that share a line and a baseline with surrounding
// text rather than stacking as blocks.
func IsInlineTag(tag string) bool { return textLikeInline[tag] }

// IsNonRenderedTag reports whether tag never produces a widget (script,
// style, head, meta, link, title).
func IsNonRenderedTag(tag string) bool { return nonRenderedTags[tag] }
