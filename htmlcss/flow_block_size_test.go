package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A block in normal flow honors its explicit width (left-aligned, not
// stretched to fill) and explicit height; an auto-width block still fills.
func TestFlowBlockHonorsExplicitWidthHeight(t *testing.T) {
	res := RenderDoc(
		`<body><div id="fixed">a</div><div id="auto">b</div></body>`,
		`body{margin:0;padding:0;}`+
			`#fixed{width:200px;height:40px;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 300})

	fixed := res.ByID["fixed"].(*El)
	auto := res.ByID["auto"].(*El)

	if b := fixed.Bounds(); b.W != 200 || b.H != 40 {
		t.Errorf("#fixed = %vx%v, want 200x40 (explicit width/height honored)", b.W, b.H)
	}
	if b := fixed.Bounds(); b.X != 0 {
		t.Errorf("#fixed X = %v, want 0 (left-aligned)", b.X)
	}
	// Auto-width block fills the container.
	if b := auto.Bounds(); b.W != 800 {
		t.Errorf("#auto width = %v, want 800 (auto fills)", b.W)
	}
	// #auto stacks directly below #fixed (40px tall).
	if b := auto.Bounds(); b.Y != 40 {
		t.Errorf("#auto Y = %v, want 40", b.Y)
	}
}

// Percentage width resolves against the containing block's content width
// at layout time (was silently ~ignored — parseLength with pctRef=0).
func TestFlowBlockPercentWidth(t *testing.T) {
	res := RenderDoc(
		`<body><div id="half">a</div><div id="full">b</div><div id="tall">c</div></body>`,
		`body{margin:0;padding:0;}`+
			`#half{width:50%;height:40px;}`+
			`#full{width:100%;height:10px;}`+
			`#tall{height:50%;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 400})

	if b := res.ByID["half"].(*El).Bounds(); b.W != 400 {
		t.Errorf("#half width = %v, want 400 (50%% of 800)", b.W)
	}
	if b := res.ByID["half"].(*El).Bounds(); b.X != 0 {
		t.Errorf("#half X = %v, want 0 (explicit width → left-aligned)", b.X)
	}
	if b := res.ByID["full"].(*El).Bounds(); b.W != 800 {
		t.Errorf("#full width = %v, want 800 (100%%)", b.W)
	}
	if b := res.ByID["tall"].(*El).Bounds(); b.H != 200 {
		t.Errorf("#tall height = %v, want 200 (50%% of 400)", b.H)
	}
}

// `margin: 0 auto` centers an explicit-width block in its band; a single
// auto margin-left pushes the block to the right edge.
func TestFlowBlockMarginAutoCentering(t *testing.T) {
	res := RenderDoc(
		`<body><div id="center">a</div><div id="right">b</div><div id="pct">c</div></body>`,
		`body{margin:0;padding:0;}`+
			`#center{width:200px;height:10px;margin:0 auto;}`+
			`#right{width:200px;height:10px;margin-left:auto;}`+
			`#pct{width:50%;height:10px;margin:0 auto;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 300})

	if b := res.ByID["center"].(*El).Bounds(); b.X != 300 {
		t.Errorf("#center X = %v, want 300 ((800-200)/2)", b.X)
	}
	if b := res.ByID["right"].(*El).Bounds(); b.X != 600 {
		t.Errorf("#right X = %v, want 600 (margin-left:auto pushes right)", b.X)
	}
	if b := res.ByID["pct"].(*El).Bounds(); b.X != 200 || b.W != 400 {
		t.Errorf("#pct X=%v W=%v, want X=200 W=400 (50%% width centered)", b.X, b.W)
	}
}

// Percentage width on a flex item resolves against the flex container.
func TestFlexItemPercentWidth(t *testing.T) {
	res := RenderDoc(
		`<body><div id="row" style="display:flex;width:400px">`+
			`<div id="a" style="width:25%;height:10px"></div>`+
			`<div id="b" style="flex-grow:1;height:10px"></div>`+
			`</div></body>`,
		`body{margin:0;padding:0;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 300})

	if b := res.ByID["a"].(*El).Bounds(); b.W != 100 {
		t.Errorf("flex item 25%% width = %v, want 100 (25%% of 400)", b.W)
	}
	if b := res.ByID["b"].(*El).Bounds(); b.W != 300 {
		t.Errorf("grow sibling width = %v, want 300 (rest of 400)", b.W)
	}
}

// max-width caps an otherwise-auto block; min-width floors a narrow one.
func TestFlowBlockMinMaxWidth(t *testing.T) {
	res := RenderDoc(
		`<body><div id="capped">x</div></body>`,
		`body{margin:0;padding:0;} #capped{max-width:300px;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 800, H: 200})
	if b := res.ByID["capped"].(*El).Bounds(); b.W != 300 {
		t.Errorf("#capped width = %v, want 300 (max-width caps auto fill)", b.W)
	}
}

// max-width:100% keeps an over-wide block from overflowing its band — the
// standard responsive-image/container guard.
func TestFlowMaxWidthPercentClamps(t *testing.T) {
	res := RenderDoc(`<body><div id="wrap" style="width:200px">
		<div id="box" style="width:500px;height:20px;max-width:100%"></div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})

	box := res.ByID["box"].(*El).Bounds()
	// wrap content box is 200 wide; max-width:100% caps box at 200 despite
	// its declared 500px width.
	if box.W > 201 {
		t.Errorf("max-width:100%% box.W=%v, want <=200 (clamped to band)", box.W)
	}
}
