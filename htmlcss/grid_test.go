package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestParseGridTracks(t *testing.T) {
	tracks := parseGridTracks("1fr 2fr", 16)
	if len(tracks) != 2 || tracks[0].Kind != qui.GridFraction || tracks[1].Size != 2 {
		t.Errorf("1fr 2fr → %+v", tracks)
	}
	rep := parseGridTracks("repeat(3, 1fr)", 16)
	if len(rep) != 3 {
		t.Errorf("repeat(3,1fr) → %d tracks, want 3", len(rep))
	}
	mixed := parseGridTracks("100px auto 1fr", 16)
	if len(mixed) != 3 || mixed[0].Kind != qui.GridFixed || mixed[0].Size != 100 ||
		mixed[1].Kind != qui.GridAuto || mixed[2].Kind != qui.GridFraction {
		t.Errorf("100px auto 1fr → %+v", mixed)
	}
}

func TestGridLayoutPlacesItems(t *testing.T) {
	res := RenderDoc(
		`<body><div id="g"><div>1</div><div>2</div><div>3</div><div>4</div></div></body>`,
		`#g { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; width: 200px }`,
		Options{},
	)
	el, ok := res.ByID["g"].(*El)
	if !ok {
		t.Fatalf("#g is %T, want *htmlcss.El", res.ByID["g"])
	}
	box := &el.Box
	kids := box.ChildList()
	if len(kids) != 4 {
		t.Fatalf("grid has %d children, want 4", len(kids))
	}
	box.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 200})

	b0, b1, b2 := kids[0].Bounds(), kids[1].Bounds(), kids[2].Bounds()
	// Item 1 is in the second column → further right than item 0.
	if b1.X <= b0.X {
		t.Errorf("item1.X (%v) should be right of item0.X (%v)", b1.X, b0.X)
	}
	// Item 2 wraps to the second row → below item 0.
	if b2.Y <= b0.Y {
		t.Errorf("item2.Y (%v) should be below item0.Y (%v)", b2.Y, b0.Y)
	}
	// Two 1fr columns in 200px with a 10px gap → ~95px each.
	if b0.W < 90 || b0.W > 100 {
		t.Errorf("column width = %v, want ~95", b0.W)
	}
}

func TestGridExplicitRows(t *testing.T) {
	res := RenderDoc(
		`<body><div id="g"><div>1</div><div>2</div></div></body>`,
		`#g { display: grid; grid-template-columns: repeat(2, 1fr); grid-template-rows: 40px }`,
		Options{},
	)
	box := &res.ByID["g"].(*El).Box
	kids := box.ChildList()
	if len(kids) != 2 {
		t.Fatalf("want 2 items, got %d", len(kids))
	}
	box.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 100})
	// Both items on row 0 (same Y), different columns.
	if kids[0].Bounds().Y != kids[1].Bounds().Y {
		t.Error("both items should share row 0")
	}
	if kids[1].Bounds().X <= kids[0].Bounds().X {
		t.Error("item1 should be in the second column")
	}
}

// TestGridMeasuresHeightSoSiblingsReachable guards against the grid
// under-reporting its height: content laid out after a grid (here a footer)
// must fall within the parent's measured height, or it would be clipped
// out of a ScrollView and become unreachable when scrolling to the bottom.
func TestGridMeasuresHeightSoSiblingsReachable(t *testing.T) {
	root := Render(
		`<body>`+
			`<div class="g"><div class="c">1</div><div class="c">2</div><div class="c">3</div>`+
			`<div class="c">4</div><div class="c">5</div><div class="c">6</div></div>`+
			`<p id="foot">FOOTER</p></body>`,
		`.g { display:grid; grid-template-columns: repeat(3, 1fr); gap: 10px }
		 .c { background:#48f; color:#fff; padding:16px }`,
		Options{},
	)
	sz := root.Measure(qui.Size{W: 600})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 600, H: sz.H})
	var footBottom float32
	walkWidgets(root, func(w qui.Widget) {
		if hasText(w, "FOOTER") {
			if b := w.Bounds(); b.Y+b.H > footBottom {
				footBottom = b.Y + b.H
			}
		}
	})
	if footBottom == 0 {
		t.Fatal("footer not found")
	}
	if footBottom > sz.H+0.5 {
		t.Errorf("footer bottom %.1f exceeds measured height %.1f — would be clipped in a ScrollView", footBottom, sz.H)
	}
}

// grid-column / grid-row place items into explicit cells with spans.
func TestGridExplicitPlacement(t *testing.T) {
	res := RenderDoc(`<body><div style="display:grid;grid-template-columns:repeat(3,100px);grid-template-rows:repeat(2,40px)">
		<div id="a" style="grid-column:1 / span 2;grid-row:1">a</div>
		<div id="b" style="grid-column:3;grid-row:1">b</div>
		<div id="c" style="grid-column:1;grid-row:2">c</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	c := res.ByID["c"].(*El).Bounds()
	if a.W < 195 || a.W > 205 {
		t.Errorf("a should span cols 1-2 (W~200), got W=%v", a.W)
	}
	if d := b.X - a.X; d < 195 || d > 205 {
		t.Errorf("b should start at col 3 (200px right of a), got Δ=%v", d)
	}
	if c.Y <= a.Y || c.X != a.X {
		t.Errorf("c should be on row 2, col 1 (below a, same X): c=%v a=%v", c, a)
	}
}

// grid-template-areas maps named areas to placements.
func TestGridTemplateAreas(t *testing.T) {
	res := RenderDoc(`<body><div style="display:grid;grid-template-columns:repeat(2,100px);grid-template-rows:repeat(2,40px);grid-template-areas:'head head' 'side main'">
		<div id="head" style="grid-area:head">head</div>
		<div id="side" style="grid-area:side">side</div>
		<div id="main" style="grid-area:main">main</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	head := res.ByID["head"].(*El).Bounds()
	side := res.ByID["side"].(*El).Bounds()
	main := res.ByID["main"].(*El).Bounds()
	if head.W < 195 {
		t.Errorf("head area should span both columns (W~200), got W=%v", head.W)
	}
	if side.Y <= head.Y || main.Y <= head.Y {
		t.Errorf("side/main should be on row 2 below head")
	}
	if main.X <= side.X {
		t.Errorf("main (col 2) should be right of side (col 1): side.X=%v main.X=%v", side.X, main.X)
	}
}
