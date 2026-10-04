package htmlcss

import "testing"

// computeFor parses html+css, resolves styles, and returns the computed
// style of the node the locator picks.
func computeFor(html, css string, locate func(*Node) *Node) *ComputedStyle {
	dom := ParseHTML(html)
	sheet := ParseCSS(css)
	styles := resolveStyles(dom, sheet)
	return styles[locate(dom)]
}

func TestVarResolvesFromRoot(t *testing.T) {
	cs := computeFor(
		`<html><body><div class="x">hi</div></body></html>`,
		`:root { --brand: #2f6fed } .x { color: var(--brand) }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 0) },
	)
	want, _ := parseColor("#2f6fed")
	if cs.Color != want {
		t.Errorf("color = %+v, want brand %+v (var(--brand) unresolved?)", cs.Color, want)
	}
}

func TestVarFallback(t *testing.T) {
	cs := computeFor(
		`<body><div class="x">hi</div></body>`,
		`.x { color: var(--missing, #ff0000) }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 0) },
	)
	want, _ := parseColor("#ff0000")
	if cs.Color != want {
		t.Errorf("color = %+v, want fallback red %+v", cs.Color, want)
	}
}

func TestVarInherits(t *testing.T) {
	// --pad defined on the ancestor, consumed by a descendant.
	cs := computeFor(
		`<body><div class="outer" style="--pad: 12px"><div class="inner" style="padding: var(--pad)">hi</div></div></body>`,
		``,
		func(r *Node) *Node { return nthOfTag(r, "div", 1) },
	)
	if cs.Padding.Top != 12 || cs.Padding.Left != 12 {
		t.Errorf("padding = %+v, want 12 all sides from var(--pad)", cs.Padding)
	}
}

func TestVarNestedReference(t *testing.T) {
	// --a references --b.
	cs := computeFor(
		`<body><div class="x">hi</div></body>`,
		`:root { --b: #00aa00; --a: var(--b) } .x { color: var(--a) }`,
		func(r *Node) *Node { return nthOfTag(r, "div", 0) },
	)
	want, _ := parseColor("#00aa00")
	if cs.Color != want {
		t.Errorf("color = %+v, want green via --a→--b %+v", cs.Color, want)
	}
}

func TestRootPseudoMatchesHTML(t *testing.T) {
	dom := ParseHTML(`<html><body><div>x</div></body></html>`)
	sel := firstSelector(t, `:root { color: red }`)
	html := nthOfTag(dom, "html", 0)
	if html == nil || !sel.matches(html, selectorState{}) {
		t.Error(":root should match the <html> element")
	}
	if div := nthOfTag(dom, "div", 0); sel.matches(div, selectorState{}) {
		t.Error(":root must not match a non-root element")
	}
}
