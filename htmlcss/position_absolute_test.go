package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// position:absolute positions the element relative to its nearest positioned
// ancestor (position:relative), out of normal flow.
func TestPositionAbsoluteAnchorsToRelativeAncestor(t *testing.T) {
	res := RenderDoc(
		`<body><div id="rel"><div id="abs">x</div></div></body>`,
		`body { margin:0; padding:0; }`+
			`#rel { position:relative; width:200px; height:100px; }`+
			`#abs { position:absolute; top:10px; right:8px; width:40px; height:20px; }`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	rel := res.ByID["rel"].(*El)
	abs := res.ByID["abs"].(*El)
	if !abs.OutOfFlow() {
		t.Fatal("position:absolute element is not marked out-of-flow")
	}

	rb := rel.Bounds()
	ab := abs.Bounds()
	// top:10 → y = rel.Y + 10; right:8 + width:40 → x = rel.right - 8 - 40.
	wantX := rb.X + rb.W - 8 - 40
	wantY := rb.Y + 10
	if ab.X != wantX || ab.Y != wantY {
		t.Errorf("abs origin = (%v,%v), want (%v,%v) [rel=%+v]", ab.X, ab.Y, wantX, wantY, rb)
	}
	if ab.W != 40 || ab.H != 20 {
		t.Errorf("abs size = %vx%v, want 40x20", ab.W, ab.H)
	}
}

// An absolute element does not consume flow space: in-flow siblings lay out
// as if it weren't there.
func TestPositionAbsoluteOutOfFlow(t *testing.T) {
	res := RenderDoc(
		`<body><div id="wrap">`+
			`<div id="a">A</div>`+
			`<div id="mid" >M</div>`+
			`<div id="b">B</div>`+
			`</div></body>`,
		`body{margin:0;padding:0;} #wrap{position:relative;}`+
			`#a,#b{height:30px;} #mid{position:absolute;top:0;left:0;height:30px;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	a := res.ByID["a"].(*El)
	b := res.ByID["b"].(*El)
	// With #mid out of flow, #b sits directly below #a (a.bottom == b.top),
	// not pushed down by #mid's height.
	if got, want := b.Bounds().Y, a.Bounds().Y+a.Bounds().H; got != want {
		t.Errorf("#b top = %v, want %v (absolute #mid must not take flow space)", got, want)
	}
}

// A relative ancestor further up is the containing block even when the
// absolute element is nested inside a plain (static) container.
func TestPositionAbsoluteNestedContainingBlock(t *testing.T) {
	res := RenderDoc(
		`<body><div id="rel"><div id="inner"><div id="abs">x</div></div></div></body>`,
		`body{margin:0;padding:0;} #rel{position:relative;width:300px;height:150px;}`+
			`#inner{padding:20px;} #abs{position:absolute;left:5px;top:6px;width:10px;height:10px;}`,
		Options{},
	)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	rel := res.ByID["rel"].(*El)
	abs := res.ByID["abs"].(*El)
	// Anchored to #rel (not #inner, which is static): left:5/top:6 from rel.
	if ab, rb := abs.Bounds(), rel.Bounds(); ab.X != rb.X+5 || ab.Y != rb.Y+6 {
		t.Errorf("abs origin = (%v,%v), want (%v,%v)", ab.X, ab.Y, rb.X+5, rb.Y+6)
	}
}
