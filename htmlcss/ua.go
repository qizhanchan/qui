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
// over the tag defaults. Interactive-state pseudos on the subject
// (:hover, :focus, :focus-visible, :active) work too: a node a framework
// state rule can match gets its state variants computed even when the
// author sheet has no state rules of its own (see frameworkStateWants).
var frameworkCSS struct {
	mu    sync.RWMutex
	rules []Rule
	// stateSels are the framework selectors gated on a subject state;
	// hover / focus / focusVisible / active summarize them.
	stateSels                          []Selector
	hover, focus, focusVisible, active bool
	// has / siblingComb / structural mirror Stylesheet.HasHas,
	// HasSiblingCombinator and HasStructural for the framework rules.
	has, siblingComb, structural bool
	structKeys                   structKeys
}

// RegisterFrameworkCSS appends css to the framework stylesheet. Call it
// from a package init: rules registered after an engine has styled its
// tree only take effect on the next restyle.
func RegisterFrameworkCSS(css string) {
	sheet := ParseCSS(css)
	frameworkCSS.mu.Lock()
	frameworkCSS.rules = append(frameworkCSS.rules, sheet.Rules...)
	frameworkCSS.has = frameworkCSS.has || sheet.HasHas
	frameworkCSS.siblingComb = frameworkCSS.siblingComb || sheet.HasSiblingCombinator
	frameworkCSS.structural = frameworkCSS.structural || sheet.HasStructural
	frameworkCSS.structKeys.merge(sheet.structKeys)
	for _, rule := range sheet.Rules {
		for _, sel := range rule.Selectors {
			h, f, a := sel.stateGates()
			fv := sel.usesFocusVisible()
			if h || f || a || fv {
				frameworkCSS.stateSels = append(frameworkCSS.stateSels, sel)
				frameworkCSS.hover = frameworkCSS.hover || h
				frameworkCSS.focus = frameworkCSS.focus || f
				frameworkCSS.focusVisible = frameworkCSS.focusVisible || fv
				frameworkCSS.active = frameworkCSS.active || a
			}
		}
	}
	frameworkCSS.mu.Unlock()
}

// frameworkStateWants reports which state variants n needs for the
// framework's own state rules: for each state, whether some framework
// state selector matches n in that state. Cheap — only the (few) state
// selectors are tried — so nodes no framework state rule targets pay
// nothing extra.
func frameworkStateWants(n *Node) (hover, focus, focusVisible, active bool) {
	frameworkCSS.mu.RLock()
	defer frameworkCSS.mu.RUnlock()
	if len(frameworkCSS.stateSels) == 0 {
		return
	}
	try := func(want bool, st selectorState) bool {
		if !want {
			return false
		}
		for _, sel := range frameworkCSS.stateSels {
			if sel.matches(n, st) {
				return true
			}
		}
		return false
	}
	hover = try(frameworkCSS.hover, selectorState{hover: true})
	focus = try(frameworkCSS.focus, selectorState{focus: true})
	focusVisible = try(frameworkCSS.focusVisible, selectorState{focus: true, focusVisible: true})
	active = try(frameworkCSS.active, selectorState{hover: true, active: true})
	return
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
	"input": true, "button": true, "select": true, "textarea": true, "br": true,
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
		if tag == "img" || tag == "input" || tag == "button" || tag == "select" || tag == "textarea" || tag == "canvas" {
			return "inline-block"
		}
		return "inline"
	}
	return "block"
}

// uaTagFor is the UA-sheet key for a node: its tag, except that push-button
// input types borrow <button>'s chrome and checkbox / radio inputs get
// their own rule.
func uaTagFor(n *Node) string {
	if n.Tag != "input" {
		return n.Tag
	}
	switch n.AttrOr("type", "") {
	case "submit", "reset", "button", "file", "color":
		return "button"
	case "checkbox", "radio":
		return "input-checkable"
	}
	return n.Tag
}

// uaDeclarations returns the default declarations for a tag.
func uaDeclarations(tag string) []Declaration {
	switch tag {
	// Headings, paragraphs and lists follow Chrome's UA stylesheet
	// (html.css): font sizes in em of the parent, margins in em of the
	// element's own size.
	case "h1":
		return decls("font-size", "2em", "font-weight", "bold", "margin", "0.67em 0")
	case "h2":
		return decls("font-size", "1.5em", "font-weight", "bold", "margin", "0.83em 0")
	case "h3":
		return decls("font-size", "1.17em", "font-weight", "bold", "margin", "1em 0")
	case "h4":
		return decls("font-weight", "bold", "margin", "1.33em 0")
	case "h5":
		return decls("font-size", "0.83em", "font-weight", "bold", "margin", "1.67em 0")
	case "h6":
		return decls("font-size", "0.67em", "font-weight", "bold", "margin", "2.33em 0")
	case "p":
		return decls("margin", "1em 0")
	case "strong", "b":
		return decls("font-weight", "bold")
	case "table":
		// A table's width is its border box, as in Chrome's UA sheet.
		return decls("border-spacing", "2px", "box-sizing", "border-box")
	case "td":
		return decls("padding", "1px")
	case "th":
		// Header cells: bold + centered by default (browser UA).
		return decls("font-weight", "bold", "text-align", "center", "padding", "1px")
	case "caption":
		// Table caption: centered with a little breathing room (browser UA).
		return decls("text-align", "center", "padding", "6px 0")
	case "em", "i":
		return decls("font-style", "italic")
	case "small":
		return decls("font-size", "0.8333em") // Chrome's `smaller`: 1/1.2
	case "a":
		// `cursor: pointer` matches the UA sheet. (<button> deliberately does
		// NOT get it — browsers leave buttons on `cursor: default`.)
		return decls("color", "#0000ee", "text-decoration", "underline",
			"cursor", "pointer")
	case "s", "del", "strike":
		return decls("text-decoration", "line-through")
	case "ins", "u":
		return decls("text-decoration", "underline")
	case "code":
		return decls("font-family", "monospace")
	case "pre":
		return decls("font-family", "monospace", "margin", "1em 0", "white-space", "pre")
	case "ul", "ol":
		return decls("margin", "1em 0", "padding-left", "40px")
	case "dl":
		return decls("margin", "1em 0")
	case "dd":
		return decls("margin-left", "40px")
	case "figure":
		return decls("margin", "1em 40px")
	case "fieldset":
		// Chrome: margin 0 2px, padding .35em .75em .625em, a 2px groove
		// border (painted solid here — qui has no groove style).
		return decls("border", "2px solid #c0c0c0",
			"padding", "0.35em 0.75em 0.625em", "margin", "0 2px")
	case "legend":
		return decls("padding", "0 2px")
	case "blockquote":
		return decls("margin", "1em 40px")
	case "hr":
		return decls("margin", "0.5em 0")
	case "button":
		// Chrome's push button: its UA values (padding 1px 6px, the #efefef
		// face, centered text, border-box, the 13.33px control font), with
		// the border drawn the way its native theme paints it — a 1px
		// #767676 hairline with slightly rounded corners — rather than the
		// 2px outset border it reports. Hover / press darken the face
		// (defaultButtonStates).
		// The 1px hairline sits in Chrome's 2px border box, so the padding
		// takes the other pixel per side: same outer size as Chrome's.
		return decls("padding", "2px 7px", "background-color", "#efefef",
			"border", "1px solid #767676", "border-radius", "3px",
			"text-align", "center", "box-sizing", "border-box",
			"font-size", "13.3333px", "line-height", "normal")
	case "input", "textarea":
		// Form controls use the small control font (Chrome's
		// `font: -webkit-small-control`, which also resets line-height).
		return decls("font-size", "13.3333px", "line-height", "normal")
	case "select":
		return decls("font-size", "13.3333px", "line-height", "normal", "box-sizing", "border-box")
	case "input-checkable":
		// <input type=checkbox|radio> (see uaTagFor).
		return decls("font-size", "13.3333px", "line-height", "normal",
			"margin", "3px 3px 3px 4px", "box-sizing", "border-box")
	case "body":
		return decls("margin", "8px")
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
