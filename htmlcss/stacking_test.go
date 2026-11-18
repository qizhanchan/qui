package htmlcss

import (
	"image"
	"image/color"
	"testing"

	"github.com/qizhanchan/qui"
)

// renderWeb renders a web-compat document into a test window and returns
// the root and the elements by id.
func renderWeb(t *testing.T, html string) (*qui.Window, RenderResult) {
	t.Helper()
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	res := RenderDoc(html, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 400})
	win.SetRoot(res.Root)
	res.Root.Measure(qui.Size{W: 400, H: 300})
	res.Root.Layout(qui.Rect{W: 400, H: 300})
	return win, res
}

func pixelAt(t *testing.T, root qui.Widget, x, y int) color.RGBA {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	root.Draw(qui.NewImageCanvas(img))
	return img.RGBAAt(x, y)
}

// A z-indexed dropdown inside a positioned header paints over a LATER
// positioned sibling of the header, and takes the clicks there — CSS
// stacking, not tree order inside the header.
func TestStackingDropdownOverLaterSibling(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<header style="position:relative;height:40px;background:#00f">
  <div id=menu style="position:absolute;top:30px;left:10px;width:100px;height:60px;background:#f00;z-index:10"></div>
</header>
<main id=main style="position:relative;height:200px;background:#0f0"></main>
</body></html>`)
	menu := res.ByID["menu"]
	if c := pixelAt(t, res.Root, 50, 70); c.R < 200 || c.G > 50 {
		t.Fatalf("pixel under the menu = %v, want the menu's red", c)
	}
	if got := res.Root.HitTest(qui.Point{X: 50, Y: 70}); got != menu {
		t.Fatalf("hit at the menu = %T %v, want the menu", got, got)
	}
	if got := res.Root.HitTest(qui.Point{X: 200, Y: 70}); got != res.ByID["main"] {
		t.Fatalf("hit beside the menu = %v, want main", got)
	}
}

// Positioned z-index:auto boxes paint after in-flow blocks even when they
// come first in the tree; a negative z-index paints under in-flow content.
func TestStackingOrderPositionedAfterInFlow(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<div style="position:relative;height:0"><div style="position:absolute;top:0;left:0;width:100px;height:100px;background:#f00"></div></div>
<div style="height:100px;width:200px;background:#00f;margin-top:0"></div>
<div style="position:relative;height:0"><div style="position:absolute;top:0;left:0;width:100px;height:50px;background:#0f0;z-index:-1"></div></div>
</body></html>`)
	if c := pixelAt(t, res.Root, 50, 50); c.R < 200 || c.B > 50 {
		t.Fatalf("positioned box under an in-flow block later in the tree: pixel %v, want red on top", c)
	}
}

// Fixed elements form stacking contexts; overflow:hidden keeps a positioned
// descendant inside its clip.
func TestStackingClipKeepsLayersInside(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<div style="overflow:hidden;height:50px;width:300px;background:#00f">
  <div style="position:relative"><div id=pop style="position:absolute;top:30px;width:100px;height:100px;background:#f00"></div></div>
</div>
</body></html>`)
	if c := pixelAt(t, res.Root, 50, 80); c.R > 200 && c.G < 50 && c.B < 50 {
		t.Fatalf("a positioned box escaped its overflow:hidden ancestor (pixel %v)", c)
	}
	if got := res.Root.HitTest(qui.Point{X: 50, Y: 80}); got == res.ByID["pop"] {
		t.Fatal("the clipped part of a positioned box takes clicks")
	}
}
