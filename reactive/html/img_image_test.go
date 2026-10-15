package html_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/widgets"
)

func findRaster(w qui.Widget) *widgets.Image {
	if iw, ok := w.(*widgets.Image); ok {
		return iw
	}
	if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, ch := range c.ChildList() {
			if iw := findRaster(ch); iw != nil {
				return iw
			}
		}
	}
	return nil
}

// h.Img().Image(img) shows an in-memory bitmap, and a new bitmap on a
// re-render replaces it (the host reloads a changed file this way).
func TestImgShowsInMemoryImage(t *testing.T) {
	a := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	b := image.NewNRGBA(image.Rect(0, 0, 5, 4))
	a.Set(0, 0, color.White)
	var set func(image.Image)
	win, root := mount(t, func() h.Node {
		cur, setCur := reactive.UseState[image.Image](a)
		set = setCur
		return h.Div(h.Img().Image(cur))
	})
	win.LayoutForTest()
	iw := findRaster(root)
	if iw == nil || iw.Image != a {
		t.Fatalf("raster widget = %v, want the in-memory image", iw)
	}
	set(b)
	win.LayoutForTest()
	if iw = findRaster(root); iw == nil || iw.Image != b {
		t.Fatalf("after re-render raster = %v, want the new image", iw)
	}
}

// A drawn <canvas> stretched by flex-grow paints over its whole box, not
// just its 300×150 intrinsic size.
func TestCanvasFillsStretchedBox(t *testing.T) {
	var el *htmlcss.El
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rt := h.Mount(win, `.col { display: flex; flex-direction: column; height: 300px; }
.cv { flex-grow: 1; }`, func() h.Node {
		return h.Div(h.Canvas(func(qui.Canvas, htmlcss.CanvasContext) {}).Class("cv").Ref(func(e *htmlcss.El) { el = e })).Class("col")
	})
	win.SetRoot(rt.Root())
	win.LayoutForTest()
	if el == nil {
		t.Fatal("no canvas element")
	}
	kids := el.ChildList()
	if len(kids) != 1 {
		t.Fatalf("canvas has %d children, want its paint leaf", len(kids))
	}
	if eb, kb := el.Bounds(), kids[0].Bounds(); eb.H < 290 || kb.H != eb.H || kb.W != eb.W {
		t.Errorf("canvas box %v, paint leaf %v: want the leaf to fill a ~300px-high box", eb, kb)
	}
}
