package htmlcss

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

type cardImageLoader struct{ img image.Image }

func (l cardImageLoader) Image(string, func()) ImageResource { return ImageResource{Raster: l.img} }

// TestCardRecordCost times recording vs rasterizing one react.dev
// marquee card (rotated, rounded, object-fit: cover photo).
func TestCardRecordCost(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	SetWebCompat(true)
	defer SetWebCompat(false)
	src := image.NewNRGBA(image.Rect(0, 0, 1200, 900))
	for i := range src.Pix {
		src.Pix[i] = uint8(i * 7)
	}
	_ = color.RGBA{}
	win := qui.NewTestWindow(qui.Size{W: 600, H: 400})
	r := RenderDoc(`<html><body><div id=card style="display:flex;justify-content:center;padding:0 20px;width:400px">
<div style="border-radius:16px;transform:rotate(2deg)"><div style="overflow:clip;position:relative">
<img src="a.webp" style="aspect-ratio:4/3;height:100%;width:100%;display:flex;object-fit:cover;border-radius:16px;background:#eee"></div></div></div></body></html>`,
		"body{margin:0}", Options{Window: win, DocumentRoot: true, Resources: cardImageLoader{src}, ViewportWidth: 600, ViewportHeight: 400})
	win.SetRoot(r.Root)
	win.LayoutForTest()
	card := r.ByID["card"].(*El)
	const scale = 2
	b := card.Bounds()
	rect := image.Rect(0, 0, int(b.W*scale)+4, int(b.H*scale)+4)
	var pic *qui.Picture
	n := 20
	start := time.Now()
	for i := 0; i < n; i++ {
		c, rec := qui.NewPictureCanvas(rect)
		c.Scale(scale, scale)
		c.Translate(-b.X, -b.Y)
		card.Draw(c)
		pic = rec.Finish()
	}
	recT := time.Since(start) / time.Duration(n)
	start = time.Now()
	for i := 0; i < n; i++ {
		pic.Rasterize(rect)
	}
	rasT := time.Since(start) / time.Duration(n)
	t.Logf("card %v: record %v, raster %v, ops %d, bytes %d", rect, recT, rasT, pic.Len(), pic.Bytes())
}
