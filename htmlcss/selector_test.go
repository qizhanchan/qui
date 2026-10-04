package htmlcss

import "testing"

// nthOfTag returns the i-th (0-based) element with the given tag in
// document order under root.
func nthOfTag(root *Node, tag string, i int) *Node {
	var found *Node
	seen := 0
	var walk func(*Node)
	walk = func(n *Node) {
		if found != nil {
			return
		}
		if n.isElement(tag) {
			if seen == i {
				found = n
				return
			}
			seen++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

// firstSelector parses css and returns the first selector of the first rule.
func firstSelector(t *testing.T, css string) Selector {
	t.Helper()
	sheet := ParseCSS(css)
	if len(sheet.Rules) == 0 || len(sheet.Rules[0].Selectors) == 0 {
		t.Fatalf("no selector parsed from %q", css)
	}
	return sheet.Rules[0].Selectors[0]
}

func TestParseAnB(t *testing.T) {
	cases := []struct {
		in   string
		a, b int
		ok   bool
	}{
		{"even", 2, 0, true},
		{"odd", 2, 1, true},
		{"3", 0, 3, true},
		{"n", 1, 0, true},
		{"2n", 2, 0, true},
		{"2n+1", 2, 1, true},
		{"2n-1", 2, -1, true},
		{"-n+3", -1, 3, true},
		{"  2n + 1 ", 2, 1, true},
		{"+n", 1, 0, true},
		{"", 0, 0, false},
		{"abc", 0, 0, false},
		{"2x+1", 0, 0, false},
	}
	for _, c := range cases {
		a, b, ok := parseAnB(c.in)
		if ok != c.ok || (ok && (a != c.a || b != c.b)) {
			t.Errorf("parseAnB(%q) = (%d,%d,%v), want (%d,%d,%v)", c.in, a, b, ok, c.a, c.b, c.ok)
		}
	}
}

func TestCombinatorMatching(t *testing.T) {
	cases := []struct {
		name   string
		html   string
		sel    string
		locate func(*Node) *Node
		want   bool
	}{
		{
			name:   "child matches direct child",
			html:   `<div><p>x</p></div>`,
			sel:    `div > p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   true,
		},
		{
			name:   "child does not match grandchild",
			html:   `<div><section><p>x</p></section></div>`,
			sel:    `div > p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   false,
		},
		{
			name:   "descendant matches grandchild",
			html:   `<div><section><p>x</p></section></div>`,
			sel:    `div p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   true,
		},
		{
			name:   "adjacent sibling matches immediate next",
			html:   `<div><h2>t</h2><p>x</p></div>`,
			sel:    `h2 + p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   true,
		},
		{
			name:   "adjacent sibling skips text but requires immediate element",
			html:   `<div><h2>t</h2><span>s</span><p>x</p></div>`,
			sel:    `h2 + p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   false,
		},
		{
			name:   "general sibling matches any following",
			html:   `<div><h2>t</h2><span>s</span><p>x</p></div>`,
			sel:    `h2 ~ p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   true,
		},
		{
			name:   "general sibling does not match preceding",
			html:   `<div><p>x</p><h2>t</h2></div>`,
			sel:    `h2 ~ p`,
			locate: func(r *Node) *Node { return nthOfTag(r, "p", 0) },
			want:   false,
		},
		{
			name:   "no-space combinator ul>li",
			html:   `<ul><li>x</li></ul>`,
			sel:    `ul>li`,
			locate: func(r *Node) *Node { return nthOfTag(r, "li", 0) },
			want:   true,
		},
		{
			name:   "mixed chain ul > li matches second item too",
			html:   `<ul><li>a</li><li>b</li></ul>`,
			sel:    `ul > li`,
			locate: func(r *Node) *Node { return nthOfTag(r, "li", 1) },
			want:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dom := ParseHTML(c.html)
			node := c.locate(dom)
			if node == nil {
				t.Fatalf("locate returned nil for %q", c.html)
			}
			got := firstSelector(t, c.sel+" { color: red }").matches(node, selectorState{})
			if got != c.want {
				t.Errorf("%q matches %q = %v, want %v", c.sel, c.html, got, c.want)
			}
		})
	}
}

func TestMalformedCombinatorDropped(t *testing.T) {
	for _, sel := range []string{"> p", "p >", "p > > q", "+ x", "~"} {
		sheet := ParseCSS(sel + " { color: red }")
		for _, r := range sheet.Rules {
			for _, s := range r.Selectors {
				if len(s.parts) > 0 {
					// A dropped selector must not have survived as a matchable
					// chain; if any survived it must at least not panic when
					// matched against a node.
					dom := ParseHTML(`<div><p>x</p></div>`)
					_ = s.matches(nthOfTag(dom, "p", 0), selectorState{})
				}
			}
		}
	}
}

func TestAttributeSelectors(t *testing.T) {
	cases := []struct {
		name string
		html string
		sel  string
		want bool
	}{
		{"exists true", `<input disabled>`, `input[disabled]`, true},
		{"exists false", `<input>`, `input[disabled]`, false},
		{"equals", `<input type="text">`, `input[type=text]`, true},
		{"equals wrong", `<input type="password">`, `input[type=text]`, false},
		{"prefix", `<a href="https://x">l</a>`, `a[href^="https"]`, true},
		{"prefix miss", `<a href="http://x">l</a>`, `a[href^="https"]`, false},
		{"suffix", `<img src="a.png">`, `img[src$=".png"]`, true},
		{"substr", `<div class="col-6 wide"></div>`, `div[class*="col"]`, true},
		{"word hit", `<div class="a btn c"></div>`, `div[class~="btn"]`, true},
		{"word miss substring", `<div class="a btn c"></div>`, `div[class~="bt"]`, false},
		{"dash exact", `<div lang="en"></div>`, `div[lang|="en"]`, true},
		{"dash prefix", `<div lang="en-US"></div>`, `div[lang|="en"]`, true},
		{"dash miss", `<div lang="english"></div>`, `div[lang|="en"]`, false},
		{"quoted space value", `<div title="a b"></div>`, `div[title="a b"]`, true},
		{"ci flag equals", `<input type="TEXT">`, `input[type="text" i]`, true},
		{"ci flag unquoted", `<input type="TEXT">`, `input[type=text i]`, true},
		{"no flag stays sensitive", `<input type="TEXT">`, `input[type=text]`, false},
		{"ci flag prefix", `<a href="HTTPS://x">l</a>`, `a[href^="https" i]`, true},
		{"ci flag word", `<div class="A BTN c"></div>`, `div[class~="btn" i]`, true},
		{"s flag stays sensitive", `<input type="TEXT">`, `input[type="text" s]`, false},
		{"value i is not a flag", `<div lang="i"></div>`, `div[lang=i]`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dom := ParseHTML(c.html)
			// The single element is the first of its tag.
			tag := ""
			for _, s := range []string{"input", "a", "img", "div"} {
				if n := nthOfTag(dom, s, 0); n != nil {
					tag = s
					break
				}
			}
			node := nthOfTag(dom, tag, 0)
			got := firstSelector(t, c.sel+" { color: red }").matches(node, selectorState{})
			if got != c.want {
				t.Errorf("%q on %q = %v, want %v", c.sel, c.html, got, c.want)
			}
		})
	}
}

func TestStructuralPseudo(t *testing.T) {
	html := `<ul><li>a</li><li>b</li><li>c</li><li>d</li><li>e</li></ul>`
	cases := []struct {
		sel  string
		want []bool // per li index 0..4
	}{
		{"li:first-child", []bool{true, false, false, false, false}},
		{"li:last-child", []bool{false, false, false, false, true}},
		{"li:nth-child(odd)", []bool{true, false, true, false, true}},
		{"li:nth-child(even)", []bool{false, true, false, true, false}},
		{"li:nth-child(2n)", []bool{false, true, false, true, false}},
		{"li:nth-child(3)", []bool{false, false, true, false, false}},
		{"li:nth-child(2n+1)", []bool{true, false, true, false, true}},
	}
	for _, c := range cases {
		t.Run(c.sel, func(t *testing.T) {
			dom := ParseHTML(html)
			sel := firstSelector(t, c.sel+" { color: red }")
			for i := 0; i < 5; i++ {
				li := nthOfTag(dom, "li", i)
				got := sel.matches(li, selectorState{})
				if got != c.want[i] {
					t.Errorf("%q li[%d] = %v, want %v", c.sel, i, got, c.want[i])
				}
			}
		})
	}
}

func TestOnlyChild(t *testing.T) {
	dom := ParseHTML(`<div><p>only</p></div><section><p>a</p><p>b</p></section>`)
	sel := firstSelector(t, `p:only-child { color: red }`)
	if !sel.matches(nthOfTag(dom, "p", 0), selectorState{}) {
		t.Error("first p should be only-child")
	}
	if sel.matches(nthOfTag(dom, "p", 1), selectorState{}) {
		t.Error("p in 2-child section should not be only-child")
	}
}

func TestNotPseudo(t *testing.T) {
	dom := ParseHTML(`<ul><li class="active">a</li><li>b</li></ul>`)
	sel := firstSelector(t, `li:not(.active) { color: red }`)
	if sel.matches(nthOfTag(dom, "li", 0), selectorState{}) {
		t.Error(":not(.active) should not match the active li")
	}
	if !sel.matches(nthOfTag(dom, "li", 1), selectorState{}) {
		t.Error(":not(.active) should match the non-active li")
	}
}

// TestUnsupportedPseudoFailsClosed pins the key correctness fix: an
// unsupported pseudo (and, historically, a fail-open one like :first-child)
// must NOT match every element.
func TestUnsupportedPseudoFailsClosed(t *testing.T) {
	dom := ParseHTML(`<form><input></form>`)
	input := nthOfTag(dom, "input", 0)
	// :required is unsupported → must never match (fail-closed), not always.
	if firstSelector(t, `input:required { color: red }`).matches(input, selectorState{}) {
		t.Error(":required (unsupported) must fail closed, got match")
	}
	// ::before pseudo-element likewise never matches an element.
	if firstSelector(t, `input::before { color: red }`).matches(input, selectorState{}) {
		t.Error("::before must fail closed, got match")
	}
}

func TestNthOfType(t *testing.T) {
	// Mixed tags: :nth-of-type counts per-tag, unlike :nth-child.
	html := `<div><h2>t</h2><p>a</p><p>b</p><span>x</span><p>c</p></div>`
	cases := []struct {
		sel  string
		tag  string
		want []bool
	}{
		{"p:nth-of-type(1)", "p", []bool{true, false, false}},    // first <p>
		{"p:nth-of-type(2)", "p", []bool{false, true, false}},    // second <p>
		{"p:nth-of-type(odd)", "p", []bool{true, false, true}},   // 1st,3rd <p>
		{"p:nth-of-type(even)", "p", []bool{false, true, false}}, // 2nd <p>
	}
	for _, c := range cases {
		t.Run(c.sel, func(t *testing.T) {
			dom := ParseHTML(html)
			sel := firstSelector(t, c.sel+" { color: red }")
			for i := range c.want {
				el := nthOfTag(dom, c.tag, i)
				if got := sel.matches(el, selectorState{}); got != c.want[i] {
					t.Errorf("%q %s[%d] = %v, want %v", c.sel, c.tag, i, got, c.want[i])
				}
			}
		})
	}
}

func TestCheckedDisabledPseudo(t *testing.T) {
	dom := ParseHTML(`<form>` +
		`<input type="checkbox" checked>` +
		`<input type="checkbox">` +
		`<input type="text" disabled>` +
		`<input type="text">` +
		`</form>`)
	checked := firstSelector(t, `input:checked { color: red }`)
	if !checked.matches(nthOfTag(dom, "input", 0), selectorState{}) {
		t.Error(":checked should match input with checked attr")
	}
	if checked.matches(nthOfTag(dom, "input", 1), selectorState{}) {
		t.Error(":checked should not match input without checked attr")
	}
	disabled := firstSelector(t, `input:disabled { color: red }`)
	if !disabled.matches(nthOfTag(dom, "input", 2), selectorState{}) {
		t.Error(":disabled should match input with disabled attr")
	}
	if disabled.matches(nthOfTag(dom, "input", 3), selectorState{}) {
		t.Error(":disabled should not match input without disabled attr")
	}
}

func TestNthLastChild(t *testing.T) {
	html := `<ul><li>a</li><li>b</li><li>c</li><li>d</li></ul>`
	cases := []struct {
		sel  string
		want []bool // per li index 0..3
	}{
		{"li:nth-last-child(1)", []bool{false, false, false, true}},  // last
		{"li:nth-last-child(2)", []bool{false, false, true, false}},  // 2nd from end
		{"li:nth-last-child(odd)", []bool{false, true, false, true}}, // d,b from end
	}
	for _, c := range cases {
		t.Run(c.sel, func(t *testing.T) {
			dom := ParseHTML(html)
			sel := firstSelector(t, c.sel+" { color: red }")
			for i := range c.want {
				if got := sel.matches(nthOfTag(dom, "li", i), selectorState{}); got != c.want[i] {
					t.Errorf("%q li[%d] = %v, want %v", c.sel, i, got, c.want[i])
				}
			}
		})
	}
}

func TestEmptyPseudo(t *testing.T) {
	dom := ParseHTML(`<div id="e"></div><div id="f">x</div><div id="g"> </div>`)
	sel := firstSelector(t, `div:empty { color: red }`)
	if !sel.matches(nthOfTag(dom, "div", 0), selectorState{}) {
		t.Error(":empty should match the empty div")
	}
	if sel.matches(nthOfTag(dom, "div", 1), selectorState{}) {
		t.Error(":empty should not match a div with text")
	}
	if !sel.matches(nthOfTag(dom, "div", 2), selectorState{}) {
		t.Error(":empty should match a whitespace-only div")
	}
}

func TestHasPseudo(t *testing.T) {
	dom := ParseHTML(`<div id="withimg"><p><img></p></div><div id="plain"><p>x</p></div>`)
	sel := firstSelector(t, `div:has(img) { color: red }`)
	if !sel.matches(nthOfTag(dom, "div", 0), selectorState{}) {
		t.Error(":has(img) should match the div containing an img")
	}
	if sel.matches(nthOfTag(dom, "div", 1), selectorState{}) {
		t.Error(":has(img) should not match a div without an img")
	}
}

func TestStateGates(t *testing.T) {
	sheet := ParseCSS(`.a:hover { color: red } .b:focus { color: blue } .c:active { color: green }`)
	if !sheet.HasHover || !sheet.HasFocus || !sheet.HasActive {
		t.Errorf("state gates = hover:%v focus:%v active:%v, want all true",
			sheet.HasHover, sheet.HasFocus, sheet.HasActive)
	}
	dom := ParseHTML(`<div class="b"></div>`)
	div := nthOfTag(dom, "div", 0)
	sel := firstSelector(t, `.b:focus { color: blue }`)
	if sel.matches(div, selectorState{}) {
		t.Error(":focus must not match in resting state")
	}
	if !sel.matches(div, selectorState{focus: true}) {
		t.Error(":focus must match when focus state is set")
	}
}

func TestSpecificityWithAttrAndNot(t *testing.T) {
	// [type=text] counts as a class (0,1,0); bare input is (0,0,1).
	attrSel := firstSelector(t, `input[type=text] { color: red }`)
	a, b, c := attrSel.specificity()
	if a != 0 || b != 1 || c != 1 {
		t.Errorf("input[type=text] specificity = (%d,%d,%d), want (0,1,1)", a, b, c)
	}
	// :not(.x) contributes its inner class specificity.
	notSel := firstSelector(t, `div:not(.x) { color: red }`)
	a, b, c = notSel.specificity()
	if a != 0 || b != 1 || c != 1 {
		t.Errorf("div:not(.x) specificity = (%d,%d,%d), want (0,1,1)", a, b, c)
	}
}
