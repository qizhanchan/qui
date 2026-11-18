package htmlcss

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
)

type stubImages struct{ img image.Image }

func (s stubImages) Image(src string, ready func()) ImageResource {
	return ImageResource{Raster: s.img}
}

// <img> sizing: one specified dimension scales the other by the intrinsic
// ratio; max-width keeps the ratio when the height is auto.
func TestReplacedImageSizing(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	src := `<html><body style="margin:0;width:300px">
<img id="h" src="a.png" style="height:32px">
<img id="w" src="a.png" style="width:108px;display:block">
<img id="m" src="a.png" style="max-width:100%;display:block">
<img id="n" src="a.png" width="54" height="10" style="display:block">
<img id="b" src="a.png" style="display:block;height:20px">
</body></html>`
	win := qui.NewTestWindow(qui.Size{W: 600, H: 400})
	res := RenderDoc(src, "", Options{Window: win, ViewportWidth: 600, DocumentRoot: true,
		Resources: stubImages{image.NewRGBA(image.Rect(0, 0, 540, 557))}})
	win.SetRoot(res.Root)
	sz := res.Root.Measure(qui.Size{W: 600})
	res.Root.Layout(qui.Rect{W: 600, H: sz.H})
	for _, c := range []struct {
		id   string
		w, h float32
	}{
		{"h", 32 * 540.0 / 557, 32},
		{"w", 108, 108 * 557.0 / 540},
		{"m", 300, 300 * 557.0 / 540},
		{"n", 54, 10},
		{"b", 20 * 540.0 / 557, 20},
	} {
		b := res.ByID[c.id].Bounds()
		if absf(b.W-c.w) > 0.6 || absf(b.H-c.h) > 0.6 {
			t.Errorf("#%s: %.1f×%.1f, want %.1f×%.1f", c.id, b.W, b.H, c.w, c.h)
		}
	}
}
