package htmlcss

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestTransparentBorderTriangle(t *testing.T) {
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(false) })
	res := RenderDoc(`<body><div id="arrow"></div></body>`, `body{margin:0}#arrow{margin:10px;width:0;height:0;border-top:10px solid #234;border-right:10px solid transparent;border-left:10px solid transparent}`, Options{})
	res.Root.Layout(qui.Rect{W: 80, H: 40})
	img := image.NewRGBA(image.Rect(0, 0, 80, 40))
	res.Root.Draw(qui.NewImageCanvas(img))
	if p := img.RGBAAt(20, 17); p.A < 240 {
		t.Fatalf("arrow center missing: %v", p)
	}
	for _, x := range []int{11, 28} {
		if p := img.RGBAAt(x, 17); p.A != 0 {
			t.Fatalf("arrow rendered as rectangle at x=%d: %v", x, p)
		}
	}
}
