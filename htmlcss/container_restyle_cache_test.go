package htmlcss

import "testing"

// Layout probes a query container at several widths and may alternate
// between them (GitHub's pricing table: 1280 ↔ 528, 25 times, 400ms each).
// The results stay right, a switch that changed nothing is skipped the
// next time, and any other style change drops what was remembered.
func TestContainerWidthToggleCache(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><div id="c" style="container-type:inline-size"><p id="p">a</p><span id="s">b</span></div></body>`,
		`@container (min-width: 600px) { #p { color: #ff0000 } }
		 @container (min-width: 900px) { #q { color: #0000ff } }`,
		Options{DocumentRoot: true, ViewportWidth: 1280, ViewportHeight: 800})
	c, p := res.ByID["c"].(*El), res.ByID["p"].(*El)
	eng := res.Engine
	red := func() bool { return p.lastCS.Color.R > 0.9 }

	c.updateContainerWidth(1000)
	if !red() {
		t.Fatal("wide: rule not applied")
	}
	c.updateContainerWidth(500)
	if red() {
		t.Fatal("narrow: rule kept")
	}
	c.updateContainerWidth(1000)
	if !red() {
		t.Fatal("wide again (cached): rule not applied")
	}
	// 1000 ↔ 700 flips only the #q condition, which styles nothing here.
	c.updateContainerWidth(700)
	before := eng.styled
	c.updateContainerWidth(1000)
	c.updateContainerWidth(700)
	if got := eng.styled - before; got != 0 {
		t.Fatalf("no-op switch walked %d elements again", got)
	}
	if !red() {
		t.Fatal("700: rule lost")
	}
	// A DOM-driven restyle starts a new generation: nothing is reused.
	p.SetClass("x")
	before = eng.styled
	c.updateContainerWidth(1000)
	if eng.styled == before {
		t.Fatal("stale equivalence survived a style change")
	}
}
