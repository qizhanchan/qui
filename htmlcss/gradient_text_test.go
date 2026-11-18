package htmlcss

import (
	"github.com/qizhanchan/qui"
	"image"
	"testing"
)

func TestTextBackgroundClipGradient(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><style>body{margin:0}div{font:60px Arial;width:180px;background:-webkit-linear-gradient(0deg,red,blue);background-clip:text;-webkit-text-fill-color:transparent}</style><div id=x>MMMM</div>`)
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	res.Root.Draw(qui.NewImageCanvas(img))
	red, blue, clear := false, false, false
	for y := 0; y < 75; y++ {
		for x := 0; x < 180; x++ {
			c := img.RGBAAt(x, y)
			if c.A == 0 {
				clear = true
			}
			if c.R > c.B+30 && c.A > 200 {
				red = true
			}
			if c.B > c.R+30 && c.A > 200 {
				blue = true
			}
		}
	}
	if !red || !blue || !clear {
		t.Fatalf("glyph gradient missing: red=%v blue=%v transparent=%v", red, blue, clear)
	}
}
func TestLayeredGradientBackgroundBoxes(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><style>body{margin:0}#x{width:100px;height:40px;border:4px solid #0000;border-radius:8px;background:linear-gradient(white,white) padding-box,linear-gradient(90deg,red,blue) border-box}</style><div id=x></div>`)
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	res.Root.Draw(qui.NewImageCanvas(img))
	for _, p := range []struct {
		x, y int
		kind string
	}{{2, 20, "red"}, {105, 20, "blue"}, {50, 20, "white"}, {0, 0, "clear"}} {
		c := img.RGBAAt(p.x, p.y)
		ok := false
		switch p.kind {
		case "red":
			ok = c.R > 200 && c.B < 50
		case "blue":
			ok = c.B > 200 && c.R < 50
		case "white":
			ok = c.R == 255 && c.G == 255 && c.B == 255
		case "clear":
			ok = c.A < 100
		}
		if !ok {
			t.Errorf("%s pixel=%v", p.kind, c)
		}
	}
}
func TestShortHexAlpha(t *testing.T) {
	c, ok := parseColor("#1234")
	if !ok || absf(c.R-1.0/15) > .001 || absf(c.A-4.0/15) > .001 {
		t.Fatalf("#rgba=%v %v", c, ok)
	}
}
