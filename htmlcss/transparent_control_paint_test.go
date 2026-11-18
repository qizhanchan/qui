package htmlcss

import (
	"github.com/qizhanchan/qui"
	"image"
	"testing"
)

func TestTransparentControlPreservesParentBorder(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><div id="search"><input id="query" value="query"></div></body>`, `body{margin:0}#search{position:relative;width:300px;height:42px;border:1px solid #999;background:white}input{position:relative;top:-1px;width:200px;height:44px;background:none;border:none;outline:none;padding:0}`, Options{})
	res.Root.Layout(qui.Rect{W: 400, H: 100})
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(100, 0); c.R != 153 {
		t.Fatalf("parent border covered: %v", c)
	}
}
