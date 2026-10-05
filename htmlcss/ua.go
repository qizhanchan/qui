package htmlcss

// User-agent (default) stylesheet. Kept intentionally small — just the
// defaults that make unstyled HTML look like HTML. Author CSS overrides
// all of these via the normal cascade (UA is tier 0).

import "sync"

// Framework stylesheet: class-keyed defaults for components a framework
// layer assembles from plain elements (reactive/html's Dialog shell). It
// joins the UA origin (tier 0), so ANY author rule overrides it whatever
// its specificity — a bare `h2 { … }` in the app beats the shell's
// `.q-dialog-title` — while among themselves its rules cascade normally
// over the tag defaults. Interactive-state pseudos (:hover …) are not
// supported here: whether state variants are computed at all is decided
// by the author sheet.
var frameworkCSS struct {
	mu    sync.RWMutex
	rules []Rule
}

// RegisterFrameworkCSS appends css to the framework stylesheet. Call it
// from a package init: rules registered after an engine has styled its
// tree only take effect on the next restyle.
func RegisterFrameworkCSS(css string) {
	sheet := ParseCSS(css)
	frameworkCSS.mu.Lock()
	frameworkCSS.rules = append(frameworkCSS.rules, sheet.Rules...)
	frameworkCSS.mu.Unlock()
}

// frameworkDecls returns the framework rules' declarations matching n,
// with their specificity, for the tier-0 cascade.
func frameworkDecls(n *Node, st selectorState) []matchedDecl {
	frameworkCSS.mu.RLock()
	defer frameworkCSS.mu.RUnlock()
	var out []matchedDecl
	for _, rule := range frameworkCSS.rules {
		best := -1
		var ba, bb, bc int
		for _, sel := range rule.Selectors {
			if sel.matches(n, st) {
				a, b, c := sel.specificity()
				if score := a*10000 + b*100 + c; score > best {
					best, ba, bb, bc = score, a, b, c
				}
			}
		}
		if best >= 0 {
			for _, d := range rule.Declarations {
				out = append(out, matchedDecl{decl: d, a: ba, b: bb, c: bc, tier: 0})
			}
		}
	}
	return out
}

// inlineTags are the elements whose default `display` is inline.
var inlineTags = map[string]bool{
	"a": true, "span": true, "strong": true, "b": true, "em": true,
	"i": true, "small": true, "code": true, "label": true, "u": true,
	"abbr": true, "mark": true, "sub": true, "sup": true, "img": true,
	"input": true, "button": true, "select": true, "br": true,
	"s": true, "del": true, "strike": true, "ins": true,
	"canvas": true,
}

// defaultDisplay returns the initial CSS display for a tag.
func defaultDisplay(tag string) string {
	switch tag {
	case "none", "head", "title", "meta", "link", "script", "style":
		return "none"
	// <datalist> is a data holder, not a box: its options feed the linked
	// input's autocomplete (see htmlcss/datalist.go).
	case "datalist":
		return "none"
	// Table box types (see table.go). colgroup/col render no box of their own
	// — their width hints are read directly by buildTable; caption falls back
	// to block and is placed as a full-width spanning grid row.
	case "table":
		return "table"
	case "thead", "tbody", "tfoot":
		return "table-row-group"
	case "tr":
		return "table-row"
	case "td", "th":
		return "table-cell"
	case "colgroup", "col":
		return "none"
	}
	if inlineTags[tag] {
		if tag == "img" || tag == "input" || tag == "button" || tag == "select" || tag == "canvas" {
			return "inline-block"
		}
		return "inline"
	}
	return "block"
}

// uaDeclarations returns the default declarations for a tag.
func uaDeclarations(tag string) []Declaration {
	switch tag {
	case "h1":
		return decls("font-size", "32px", "font-weight", "bold", "margin", "16px 0")
	case "h2":
		return decls("font-size", "26px", "font-weight", "bold", "margin", "14px 0")
	case "h3":
		return decls("font-size", "21px", "font-weight", "bold", "margin", "12px 0")
	case "h4":
		return decls("font-size", "17px", "font-weight", "bold", "margin", "10px 0")
	case "h5":
		return decls("font-size", "15px", "font-weight", "bold", "margin", "10px 0")
	case "h6":
		return decls("font-size", "13px", "font-weight", "bold", "margin", "10px 0")
	case "p":
		return decls("margin", "12px 0")
	case "strong", "b":
		return decls("font-weight", "bold")
	case "th":
		// Header cells: bold + centered by default (browser UA).
		return decls("font-weight", "bold", "text-align", "center")
	case "caption":
		// Table caption: centered with a little breathing room (browser UA).
		return decls("text-align", "center", "padding", "6px 0")
	case "em", "i":
		return decls("font-style", "italic")
	case "small":
		return decls("font-size", "13px")
	case "a":
		// `cursor: pointer` matches the UA sheet. (<button> deliberately does
		// NOT get it — browsers leave buttons on `cursor: default`.)
		return decls("color", "#1a66e6", "text-decoration", "underline",
			"cursor", "pointer")
	case "s", "del", "strike":
		return decls("text-decoration", "line-through")
	case "ins", "u":
		return decls("text-decoration", "underline")
	case "code", "pre":
		return decls("font-family", "monospace")
	case "ul", "ol":
		return decls("margin", "10px 0", "padding-left", "24px")
	case "li":
		return decls("margin", "4px 0")
	case "dl":
		return decls("margin", "12px 0")
	case "dd":
		return decls("margin", "4px 0 4px 40px")
	case "dt":
		return decls("font-weight", "bold", "margin", "4px 0 0 0")
	case "figure":
		return decls("margin", "12px 40px")
	case "figcaption":
		return decls("font-size", "13px", "color", "#666")
	case "fieldset":
		return decls("border", "1px solid #c0c0c8", "border-radius", "4px",
			"padding", "8px 12px", "margin", "8px 0")
	case "legend":
		return decls("font-weight", "bold", "padding", "0 4px")
	case "blockquote":
		return decls("margin", "12px 0", "padding-left", "16px",
			"border-left-width", "4px", "border-left-color", "#ddd", "color", "#555")
	case "hr":
		return decls("margin", "12px 0")
	case "button":
		// Minimal, borderless default: a bare <button> is a flat, padded,
		// rounded hit target with NO box (border/fill) of its own — the box
		// chrome a browser draws reads as an unwanted extra layer in this
		// engine's minimalist UIs and reserves layout space even when made
		// transparent. Authors add their own background via CSS; hover
		// feedback is synthesized in el.go (defaultButtonStates), and the
		// border-radius keeps that hover/author fill rounded.
		return decls("padding", "6px 14px", "border-radius", "6px")
	case "body":
		return decls("margin", "0", "padding", "16px")
	default:
		return nil
	}
}

// decls builds a declaration slice from alternating prop, value strings.
func decls(pairs ...string) []Declaration {
	var out []Declaration
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Declaration{Property: pairs[i], Value: pairs[i+1]})
	}
	return out
}
