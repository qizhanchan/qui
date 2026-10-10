package html_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
)

// inkBox returns the first and last ink row and the first ink column of
// the dark pixels inside r, ignoring a 3 px margin for the border.
func inkBox(img *image.RGBA, r image.Rectangle) (top, bot, left int) {
	top, bot, left = -1, -1, -1
	for y := r.Min.Y + 3; y < r.Max.Y-3; y++ {
		for x := r.Min.X + 3; x < r.Max.X-3; x++ {
			if c := img.RGBAAt(x, y); c.R < 120 && c.G < 120 && c.B < 120 {
				if top < 0 {
					top = y
				}
				bot = y
				if left < 0 || x < left {
					left = x
				}
			}
		}
	}
	return
}

// CSS height / min-height / padding decide an <input>'s and <select>'s box,
// with the value text centred in it and inset by border + padding.
func TestControlBoxFollowsCSS(t *testing.T) {
	const border = `border: 1px solid #747775; font-size: 14px; line-height: 20px;`
	cases := []struct {
		name, css string
		wantH     float32
	}{
		{"height + box-sizing", border + `height: 40px; padding: 0 12px; box-sizing: border-box;`, 40},
		{"min-height", border + `min-height: 40px; padding: 0 12px; box-sizing: border-box;`, 40},
		{"padding only", border + `padding: 9px 11px;`, 40},
		{"content-box height", border + `height: 40px; padding: 0 12px;`, 42},
		{"taller than the text", border + `height: 60px; padding: 0 12px; box-sizing: border-box;`, 60},
	}
	for _, tag := range []string{"input", "select"} {
		for _, c := range cases {
			win := qui.NewTestWindow(qui.Size{W: 400, H: 200})
			rt := h.Mount(win, tag+" { "+c.css+" }", func() h.Node {
				if tag == "input" {
					return h.Div(h.Input().Value("Hxg"))
				}
				return h.Div(h.Select("Hxg", "b").Selected(0))
			})
			root := rt.Root().(*htmlcss.El)
			root.Layout(qui.Rect{W: 400, H: 200})
			b := findTag(root, tag).Bounds()
			if b.H != c.wantH {
				t.Errorf("<%s> %s: height %v, want %v", tag, c.name, b.H, c.wantH)
			}
			img := image.NewRGBA(image.Rect(0, 0, 400, 200))
			cv := qui.NewImageCanvas(img)
			cv.Clear(qui.Color{R: 1, G: 1, B: 1, A: 1})
			root.Draw(cv)
			top, bot, left := inkBox(img, image.Rect(int(b.X), int(b.Y), int(b.X+b.W), int(b.Y+b.H)))
			if top < 0 {
				t.Errorf("<%s> %s: no text drawn", tag, c.name)
				continue
			}
			if mid := float32(top+bot)/2 - b.Y; mid < b.H/2-3 || mid > b.H/2+3 {
				t.Errorf("<%s> %s: text centred at y=%v in a %v px box", tag, c.name, mid, b.H)
			}
			if in := float32(left) - b.X; in < 12 || in > 15 {
				t.Errorf("<%s> %s: text starts %v px from the edge, want ~13 (border + padding)", tag, c.name, in)
			}
		}
	}
}
