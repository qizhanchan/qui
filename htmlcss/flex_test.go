package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// CSS `order` reorders flex items independent of DOM order.
func TestFlexOrder(t *testing.T) {
	res := RenderDoc(`<body><div style="display:flex">
		<div id="a" style="order:2;width:50px">a</div>
		<div id="b" style="order:1;width:50px">b</div>
		<div id="c" style="order:3;width:50px">c</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 100})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	c := res.ByID["c"].(*El).Bounds()
	if !(b.X < a.X && a.X < c.X) {
		t.Errorf("order should place b<a<c, got X: a=%v b=%v c=%v", a.X, b.X, c.X)
	}
}

// align-content distributes wrap lines in the container's leftover cross
// space. Two 20px-tall lines in a 100px container: center puts line 1 at
// (100-40)/2 = 30; unset defaults to stretch (CSS initial) so lines grow.
func TestFlexAlignContent(t *testing.T) {
	render := func(alignContent string) (a, c qui.Rect) {
		extra := ""
		if alignContent != "" {
			extra = "align-content:" + alignContent
		}
		res := RenderDoc(`<body><div id="row" style="display:flex;flex-wrap:wrap;width:200px;height:100px;`+extra+`">
			<div id="a" style="width:80px;height:20px"></div>
			<div id="b" style="width:80px;height:20px"></div>
			<div id="c" style="width:80px;height:20px"></div>
		</div></body>`, ``, Options{})
		res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 200})
		// Report Y relative to the flex container (body has UA margin).
		row := res.ByID["row"].(*El).Bounds()
		a = res.ByID["a"].(*El).Bounds()
		c = res.ByID["c"].(*El).Bounds()
		a.Y -= row.Y
		c.Y -= row.Y
		return a, c
	}

	if a, c := render("center"); a.Y != 30 || c.Y != 50 {
		t.Errorf("center: a.Y=%v c.Y=%v, want 30/50", a.Y, c.Y)
	}
	if a, c := render("space-between"); a.Y != 0 || c.Y != 80 {
		t.Errorf("space-between: a.Y=%v c.Y=%v, want 0/80", a.Y, c.Y)
	}
	if a, c := render(""); a.Y != 0 || c.Y != 50 {
		t.Errorf("default stretch: a.Y=%v c.Y=%v, want 0/50 (lines grown by 30 each)", a.Y, c.Y)
	}
}

// align-self overrides the container's align-items for one item.
func TestFlexAlignSelf(t *testing.T) {
	res := RenderDoc(`<body><div style="display:flex;height:100px;align-items:flex-start">
		<div id="a">a</div>
		<div id="b" style="align-self:center">b</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 100})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	if b.Y <= a.Y {
		t.Errorf("align-self:center item Y=%v should sit below flex-start sibling Y=%v", b.Y, a.Y)
	}
}

// flex-basis sets the main-axis base size.
func TestFlexBasis(t *testing.T) {
	res := RenderDoc(`<body><div style="display:flex;width:400px">
		<div id="a" style="flex-basis:120px">a</div>
		<div id="b" style="flex-grow:1">b</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 100})

	if a := res.ByID["a"].(*El).Bounds(); a.W < 118 || a.W > 122 {
		t.Errorf("flex-basis:120px item W=%v, want ~120", a.W)
	}
}

// flex-shrink:0 keeps an item from shrinking when the line overflows.
func TestFlexShrinkZero(t *testing.T) {
	res := RenderDoc(`<body><div style="display:flex;width:100px">
		<div id="a" style="width:80px;flex-shrink:0">a</div>
		<div id="b" style="width:80px">b</div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 100})

	a := res.ByID["a"].(*El).Bounds()
	b := res.ByID["b"].(*El).Bounds()
	if a.W < 79 {
		t.Errorf("flex-shrink:0 item W=%v, want ~80 (no shrink)", a.W)
	}
	if b.W >= a.W {
		t.Errorf("shrinkable sibling W=%v should shrink below the non-shrinking item W=%v", b.W, a.W)
	}
}

// margin-left:auto on a flex item pushes it to the far end — the canonical
// "logo left, actions right" navbar idiom. Justify defaults to start.
func TestFlexAutoMarginPushRight(t *testing.T) {
	res := RenderDoc(`<body><div id="nav" style="display:flex;width:400px">
		<div id="logo" style="width:60px;height:20px"></div>
		<div id="cta" style="width:80px;height:20px;margin-left:auto"></div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 100})

	logo := res.ByID["logo"].(*El).Bounds()
	cta := res.ByID["cta"].(*El).Bounds()
	nav := res.ByID["nav"].(*El).Bounds()
	if diff := logo.X - nav.X; diff < -1 || diff > 1 {
		t.Errorf("logo should sit at the start of nav, got X=%v (nav.X=%v)", logo.X, nav.X)
	}
	// cta pushed to the right edge: nav right (nav.X+400) - cta width 80.
	wantX := nav.X + 400 - 80
	if diff := cta.X - wantX; diff < -1 || diff > 1 {
		t.Errorf("margin-left:auto CTA X=%v, want ~%v (pushed right)", cta.X, wantX)
	}
}

// margin:auto (horizontal) on a lone flex item centers it in the row.
func TestFlexAutoMarginCenter(t *testing.T) {
	res := RenderDoc(`<body><div id="row" style="display:flex;width:300px">
		<div id="mid" style="width:100px;height:20px;margin-left:auto;margin-right:auto"></div>
	</div></body>`, ``, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	row := res.ByID["row"].(*El).Bounds()
	mid := res.ByID["mid"].(*El).Bounds()
	wantX := row.X + (300-100)/2
	if diff := mid.X - wantX; diff < -1 || diff > 1 {
		t.Errorf("margin:0 auto centered X=%v, want ~%v", mid.X, wantX)
	}
}
