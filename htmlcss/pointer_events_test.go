package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func transparentOf(t *testing.T, w qui.Widget) bool {
	t.Helper()
	pt, ok := w.(qui.PointerTransparency)
	if !ok {
		t.Fatalf("widget %T does not expose PointerTransparent", w)
	}
	return pt.PointerTransparent()
}

// `pointer-events: none` flags the element and (by inheritance) its subtree;
// a descendant reopens itself with `auto`.
func TestPointerEventsNoneInheritsAndReopens(t *testing.T) {
	res := RenderDoc(
		`<body><div id="scrim"><div id="deco">x</div><div id="live">click</div></div>
		 <div id="normal">n</div></body>`,
		`#scrim { pointer-events: none; } #live { pointer-events: auto; }`,
		Options{},
	)
	if !transparentOf(t, res.ByID["scrim"]) {
		t.Error("#scrim should be pointer-transparent")
	}
	if !transparentOf(t, res.ByID["deco"]) {
		t.Error("#deco should inherit pointer-events:none")
	}
	if transparentOf(t, res.ByID["live"]) {
		t.Error("#live should reopen itself with pointer-events:auto")
	}
	if transparentOf(t, res.ByID["normal"]) {
		t.Error("#normal should be hittable")
	}
}

// The pointer must reach the button UNDER a full-bleed transparent scrim,
// and the scrim must never become the target itself.
func TestPointerEventsNoneFallsThroughToWidgetBehind(t *testing.T) {
	res := RenderDoc(
		`<body><div id="stack">
		   <div id="btn">Save</div>
		   <div id="scrim"></div>
		 </div></body>`,
		`#stack { position: relative; width: 200px; height: 100px; }
		 #btn { width: 200px; height: 100px; }
		 #scrim { position: absolute; top: 0; left: 0; width: 200px; height: 100px;
		          background: rgba(0,0,0,0.2); pointer-events: none; }`,
		Options{},
	)
	win := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 300, H: 200})

	scrim := res.ByID["scrim"].(*El)
	b := scrim.Bounds()
	hit := win.HitTestForTest(qui.Point{X: b.X + b.W/2, Y: b.Y + b.H/2})
	if hit == nil {
		t.Fatal("nothing was hit under the scrim")
	}
	if hit == qui.Widget(scrim) {
		t.Fatal("the pointer-events:none scrim became the hit target")
	}
	// The target must be the button (or a widget inside it), not the scrim.
	found := false
	for cur := hit; cur != nil; cur = cur.Parent() {
		if cur == res.ByID["btn"] {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hit %T is not the button or a descendant of it", hit)
	}
}

// Restyling out of a `none` scope has to make the element hittable again.
func TestPointerEventsRestoredOnRestyle(t *testing.T) {
	res := RenderDoc(
		`<body><div id="d" class="ghost">x</div></body>`,
		`.ghost { pointer-events: none; } .solid { color: #333; }`,
		Options{},
	)
	d := res.ByID["d"].(*El)
	if !transparentOf(t, d) {
		t.Fatal("initial state should be transparent")
	}
	d.SetClass("solid")
	res.Engine.Restyle()
	if transparentOf(t, d) {
		t.Error("element stayed pointer-transparent after the rule stopped matching")
	}
}
