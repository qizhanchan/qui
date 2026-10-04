package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func TestRelativeOffset(t *testing.T) {
	res := RenderDoc(
		`<body><div id="r">x</div></body>`,
		`#r { position: relative; left: 10px; top: 5px; width: 50px; height: 20px }`,
		Options{},
	)
	box := &res.ByID["r"].(*El).Box
	if box.PosOffset.X != 10 || box.PosOffset.Y != 5 {
		t.Errorf("PosOffset = %+v, want (10,5)", box.PosOffset)
	}
	// The offset moves the box (and subtree) from its normal position.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 50, H: 20})
	if b := box.Bounds(); b.X != 10 || b.Y != 5 {
		t.Errorf("bounds after relative offset = (%v,%v), want (10,5)", b.X, b.Y)
	}
}

func TestRelativeOffsetRightBottom(t *testing.T) {
	res := RenderDoc(
		`<body><div id="r">x</div></body>`,
		`#r { position: relative; right: 8px; bottom: 3px; width: 50px; height: 20px }`,
		Options{},
	)
	box := &res.ByID["r"].(*El).Box
	if box.PosOffset.X != -8 || box.PosOffset.Y != -3 {
		t.Errorf("PosOffset = %+v, want (-8,-3)", box.PosOffset)
	}
}

func TestOverflowHiddenClips(t *testing.T) {
	res := RenderDoc(
		`<body><div id="h">x</div></body>`,
		`#h { overflow: hidden; width: 40px; height: 20px }`,
		Options{},
	)
	box := &res.ByID["h"].(*El).Box
	if !box.ClipChildren {
		t.Error("overflow:hidden should set ClipChildren")
	}
	// Draw must not panic with the clip scope.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 40, H: 20})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
}

func TestOverflowScrollWrapsScrollView(t *testing.T) {
	res := RenderDoc(
		`<body><div id="s">`+
			`<p>line 1</p><p>line 2</p><p>line 3</p><p>line 4</p>`+
			`<p>line 5</p><p>line 6</p><p>line 7</p><p>line 8</p>`+
			`</div></body>`,
		`#s { overflow: auto; width: 200px; height: 60px }`,
		Options{},
	)
	outerEl, ok := res.ByID["s"].(*El)
	if !ok {
		t.Fatalf("#s is %T, want outer *widgets.Box", res.ByID["s"])
	}
	outer := &outerEl.Box
	if outer.Style().Height != 60 {
		t.Errorf("outer height = %v, want 60", outer.Style().Height)
	}
	kids := outer.ChildList()
	if len(kids) != 1 {
		t.Fatalf("outer has %d children, want 1 (ScrollView)", len(kids))
	}
	sv, ok := kids[0].(*widgets.ScrollView)
	if !ok {
		t.Fatalf("outer child is %T, want *widgets.ScrollView", kids[0])
	}
	// Lay out the fixed-height viewport; the tall content must overflow.
	outer.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 60})
	if sv.MaxScroll() <= 0 {
		t.Errorf("MaxScroll = %v, want > 0 (content should overflow 60px viewport)", sv.MaxScroll())
	}
}
