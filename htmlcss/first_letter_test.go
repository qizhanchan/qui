package htmlcss

import (
	"github.com/qizhanchan/qui"
	"image"
	"testing"
)

func TestFirstLetterColorPaint(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><style>body{margin:0}h2{font:40px Arial;margin:0;color:blue}h2::first-letter{color:red}</style><h2>Versatile</h2>`)
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	res.Root.Draw(qui.NewImageCanvas(img))
	red, blue := false, false
	for y := 0; y < 60; y++ {
		for x := 0; x < 200; x++ {
			c := img.RGBAAt(x, y)
			if x < 30 && c.R > 200 && c.B < 50 {
				red = true
			}
			if x > 30 && c.B > 200 && c.R < 50 {
				blue = true
			}
		}
	}
	if !red || !blue {
		t.Fatalf("first-letter red=%v rest blue=%v", red, blue)
	}
}
