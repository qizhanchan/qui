package html_test

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/svg"
)

// An untinted icon keeps its own colours: a red logo stays red under a
// dark text colour, while a plain Icon collapses to the text colour.
func TestIconUntintedKeepsColours(t *testing.T) {
	logo, err := svg.ParseBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="#ff0000"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	win := qui.NewTestWindow(qui.Size{W: 200, H: 100})
	rt := h.Mount(win, `span { color: #1f1f1f; width: 20px; height: 20px; }`, func() h.Node {
		return h.Div(h.Span().IconUntinted(logo), h.Span().Icon(logo))
	})
	root := rt.Root().(*htmlcss.El)
	root.Layout(qui.Rect{W: 200, H: 100})
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	cv := qui.NewImageCanvas(img)
	root.Draw(cv)
	kids := root.ChildList()
	at := func(i int) (r, g, b uint8) {
		bb := kids[i].Bounds()
		c := img.RGBAAt(int(bb.X+bb.W/2), int(bb.Y+bb.H/2))
		return c.R, c.G, c.B
	}
	if r, g, b := at(0); r < 200 || g > 60 || b > 60 {
		t.Errorf("untinted logo pixel = %d,%d,%d, want red", r, g, b)
	}
	if r, g, b := at(1); r > 80 || g > 80 || b > 80 {
		t.Errorf("tinted icon pixel = %d,%d,%d, want the dark text colour", r, g, b)
	}
}
