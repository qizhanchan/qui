package htmlcss

import (
	"strconv"
	"testing"
)

func TestColorParsingRegressions(t *testing.T) {
	if _, ok := parseHexColor("#zzz"); ok {
		t.Error("#zzz must be rejected")
	}
	if c, ok := parseHexColor("#fff"); !ok || c.R != 1 {
		t.Errorf("#fff = %v, %v", c, ok)
	}
	if c, ok := parseRGBColor("rgb(0 0 0 / 50%)"); !ok || c.A < 0.49 || c.A > 0.51 {
		t.Errorf("rgb(0 0 0 / 50%%) alpha = %v, want 0.5", c.A)
	}
	if c, ok := parseRGBColor("rgba(0,0,0,5)"); !ok || c.A != 1 {
		t.Errorf("rgba alpha 5 = %v, want clamped to 1", c.A)
	}
}

func TestPaddingShorthandWithCalc(t *testing.T) {
	ins := parseBox(map[string]string{"padding": "calc(2px + 4px) 8px"}, "padding", 16)
	if ins.Top != 6 || ins.Bottom != 6 || ins.Left != 8 || ins.Right != 8 {
		t.Errorf("padding = %+v, want 6/8/6/8", ins)
	}
}

func TestAtRulesDoNotSwallowFollowingRule(t *testing.T) {
	for _, src := range []string{
		`@charset "utf-8"; .btn{color:red}`,
		`@import url(x.css); .btn{color:red}`,
		`@keyframes s{from{opacity:0}to{opacity:1}} .btn{color:red}`,
		`@supports (display:grid){.x{color:blue}} .btn{color:red}`,
	} {
		sheet := ParseCSS(src)
		if len(sheet.Rules) != 1 {
			t.Errorf("%q: %d rules, want just .btn", src, len(sheet.Rules))
			continue
		}
		if d := sheet.Rules[0].Declarations; len(d) != 1 || d[0].Property != "color" || d[0].Value != "red" {
			got := d
			t.Errorf("%q: surviving rule %+v, want color:red", src, got)
		}
	}
}

// A descendant step must try farther ancestors when a nearer match fails a
// later child step.
func TestSelectorBacktracksThroughDescendant(t *testing.T) {
	doc := ParseHTML(`<ul class="menu"><li><ul><li><a id="x">x</a></li></ul></li></ul>`)
	a := findByID(doc, "x")
	if a == nil {
		t.Fatal("no #x")
	}
	sel := parseSelectorList(".menu > li a")[0]
	if !sel.matches(a, selectorState{}) {
		t.Error(".menu > li a should match via the outer li")
	}
	if parseSelectorList(".nope > li a")[0].matches(a, selectorState{}) {
		t.Error(".nope > li a must not match")
	}
}

func findByID(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if v, ok := n.Attrs["id"]; ok && v == id {
		return n
	}
	for _, c := range n.Children {
		if f := findByID(c, id); f != nil {
			return f
		}
	}
	return nil
}

func TestNotListAndRelativeHas(t *testing.T) {
	doc := ParseHTML(`<div id="card"><p class="a" id="pa"></p><p class="b" id="pb"></p><section><span class="x" id="deep"></span></section></div><div id="flat"><span class="x"></span></div>`)
	pa, pb := findByID(doc, "pa"), findByID(doc, "pb")
	not := parseSelectorList("p:not(.a, .b)")[0]
	if not.matches(pa, selectorState{}) || not.matches(pb, selectorState{}) {
		t.Error("p:not(.a, .b) must exclude both .a and .b")
	}
	card, flat := findByID(doc, "card"), findByID(doc, "flat")
	child := parseSelectorList("div:has(> .x)")[0]
	if child.matches(card, selectorState{}) {
		t.Error("div:has(> .x) must not match a grandchild .x")
	}
	if !child.matches(flat, selectorState{}) {
		t.Error("div:has(> .x) should match a direct child .x")
	}
	if !parseSelectorList("div:has(section .x)")[0].matches(card, selectorState{}) {
		t.Error("div:has(section .x) should match")
	}
	if !parseSelectorList("p:has(+ .b)")[0].matches(pa, selectorState{}) {
		t.Error("p:has(+ .b) should match .a")
	}
}

func TestVarCyclesAreInvalid(t *testing.T) {
	props := buildCustomProps(nil, map[string]string{
		"--a": "var(--b)", "--b": "var(--a)", "--ok": "var(--base)", "--base": "4px",
	})
	if _, ok := props["--a"]; ok {
		t.Error("--a is in a cycle and must be invalid")
	}
	if props["--ok"] != "4px" {
		t.Errorf("--ok = %q, want 4px", props["--ok"])
	}
	out := resolveMapVars(map[string]string{"color": "var(--a, red)", "width": "var(--a)"}, props)
	if out["color"] != "red" {
		t.Errorf("color = %q, want the fallback red", out["color"])
	}
	if _, ok := out["width"]; ok {
		t.Errorf("width = %q, want dropped (invalid at computed-value time)", out["width"])
	}
	// A doubling chain stays bounded instead of building megabytes.
	m := map[string]string{"--v0": "xxxxxxxxxxxxxxxx"}
	for i := 1; i <= 30; i++ {
		m["--v"+strconv.Itoa(i)] = "var(--v" + strconv.Itoa(i-1) + ") var(--v" + strconv.Itoa(i-1) + ")"
	}
	if v, ok := buildCustomProps(nil, m)["--v30"]; ok {
		t.Errorf("--v30 resolved to %d bytes, want invalid", len(v))
	}
}

func TestCalcSignsAndNestedFunctions(t *testing.T) {
	for in, want := range map[string]float32{
		"calc(-1 * 8px)":             -8,
		"calc(100% - -4px)":          104,
		"calc(-(2px + 3px))":         -5,
		"calc(min(10px, 2em) + 4px)": 14,
		"calc(max(1px, 3px) * 2)":    6,
		"min(calc(1px + 2px), 10px)": 3,
		"calc(10px - 4px)":           6,
	} {
		got, ok := parseLength(in, 16, 100)
		if !ok || got != want {
			t.Errorf("%s = %v (ok=%v), want %v", in, got, ok, want)
		}
	}
}

func TestFlexShorthandForms(t *testing.T) {
	for in, want := range map[string][3]string{
		"1":         {"1", "1", "0"},
		"2 200px":   {"2", "1", "200px"},
		"2 3":       {"2", "3", "0"},
		"200px":     {"1", "1", "200px"},
		"auto":      {"1", "1", "auto"},
		"none":      {"0", "0", "auto"},
		"0 0 100px": {"0", "0", "100px"},
	} {
		m := map[string]string{}
		expandFlex(in, m)
		got := [3]string{m["flex-grow"], m["flex-shrink"], m["flex-basis"]}
		if got != want {
			t.Errorf("flex: %s → %v, want %v", in, got, want)
		}
	}
}

func TestMediaQueryLists(t *testing.T) {
	for q, want := range map[string]bool{
		"only screen and (max-width: 600px)": true,
		"screen, print":                      true,
		"print, (min-width: 1000px)":         false,
		"not print":                          true,
		"not screen and (max-width: 600px)":  false,
		"(min-width: 100px)":                 true,
	} {
		if got := mediaMatches(q, 500); got != want {
			t.Errorf("@media %s at 500px = %v, want %v", q, got, want)
		}
	}
}

func TestStylesheetStringsAreOpaque(t *testing.T) {
	sheet := ParseCSS(`.q::before{content:"}"} .a{content:"/*"} .next{color:red} /* real */ .last{color:blue}`)
	got := map[string]bool{}
	for _, r := range sheet.Rules {
		for _, d := range r.Declarations {
			got[d.Property+":"+d.Value] = true
		}
	}
	for _, want := range []string{`content:"}"`, `content:"/*"`, "color:red", "color:blue"} {
		if !got[want] {
			t.Errorf("missing declaration %s (have %v)", want, got)
		}
	}
}

// Shorthand vs longhand follows cascade order, whichever is which.
func TestShorthandLonghandCascadeOrder(t *testing.T) {
	sheet := ParseCSS(`
.btn { background: #ffffff; padding-top: 40px; border-left: 2px solid red }
.btn.primary { background-color: #0000ff; padding: 4px; border: none }
.box { margin: 10px }
.box.tight { margin-left: 1px }`)
	doc := ParseHTML(`<div><a id="b" class="btn primary"></a><p id="x" class="box tight"></p></div>`)
	b := findByID(doc, "b")
	cs := interpret(b, mergedDecls(b, sheet, selectorState{}), nil)
	if cs.Background.B != 1 || cs.Background.R != 0 {
		t.Errorf("background = %+v, want the later background-color blue", cs.Background)
	}
	if cs.Padding.Top != 4 {
		t.Errorf("padding-top = %v, want 4 from the later padding shorthand", cs.Padding.Top)
	}
	if cs.SideWidths.Left != 0 {
		t.Errorf("border-left width = %v, want 0 after the later border: none", cs.SideWidths.Left)
	}
	x := findByID(doc, "x")
	cs = interpret(x, mergedDecls(x, sheet, selectorState{}), nil)
	if cs.Margin.Left != 1 || cs.Margin.Top != 10 {
		t.Errorf("margin = %+v, want left 1 (later longhand) and top 10", cs.Margin)
	}
}

// The rule index keeps source order and follows a sheet that grows.
func TestRuleIndexOrderAndGrowth(t *testing.T) {
	sheet := ParseCSS(`.a { color: red } span { color: green } * { padding: 1px } #x.a { color: blue }`)
	doc := ParseHTML(`<span id="x" class="a b"></span>`)
	n := findByID(doc, "x")
	if got := mergedDecls(n, sheet, selectorState{})["color"]; got != "blue" {
		t.Fatalf("color = %q, want blue (highest specificity)", got)
	}
	extra := ParseCSS(`.b { margin: 3px }`)
	sheet.Rules = append(sheet.Rules, extra.Rules...)
	if got := mergedDecls(n, sheet, selectorState{})["margin"]; got != "3px" {
		t.Fatalf("margin = %q after appending a rule, want 3px", got)
	}
}
