package htmlcss

import (
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
	"image"
	"strings"
	"testing"
)

type svgFitLoader struct{ doc *svg.Document }

func (l svgFitLoader) Image(string, func()) ImageResource { return ImageResource{Vector: l.doc} }

func TestSVGObjectFitCoverPosition(t *testing.T) {
	doc, err := svg.Parse(strings.NewReader(`<svg width="100" height="300" viewBox="0 0 100 300"><rect width="100" height="100" fill="red"/><rect y="100" width="100" height="200" fill="blue"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	prev := webCompat
	webCompat = true
	defer func() { webCompat = prev }()
	res := RenderDoc(`<body><img id=x src="image.svg"></body>`, `body{margin:0}img{display:block;width:100px;height:150px;margin:10px;object-fit:cover;object-position:100% 0}`, Options{Resources: svgFitLoader{doc}})
	res.Root.Layout(qui.Rect{W: 130, H: 180})
	img := image.NewRGBA(image.Rect(0, 0, 130, 180))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(60, 60); c.R < 250 || c.B != 0 {
		t.Fatalf("top of covered SVG: %v", c)
	}
	if c := img.RGBAAt(60, 130); c.B < 250 {
		t.Fatalf("lower SVG: %v", c)
	}
	if c := img.RGBAAt(60, 165); c.A != 0 {
		t.Fatalf("SVG escaped content box: %v", c)
	}
}
