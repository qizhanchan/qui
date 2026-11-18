package htmlcss

import (
	"github.com/qizhanchan/qui"
	"image"
	"testing"
)

func TestInlineBackgroundStatesKeepLineGeometry(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	res := RenderDoc(`<body><div id="line"><a id="link" href="#">tag</a></div></body>`, `body{margin:0;font:16px/24px sans-serif}a{background:red;padding:0 4px 1px;border:1px solid black}a:hover{background:blue}a:focus{background:lime}`, Options{Window: win})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{W: 300, H: 100})
	link := res.ByID["link"].(*El)
	if !link.folded {
		t.Fatal("background state must remain an inline span")
	}
	if h := res.ByID["line"].Bounds().H; h != 24 {
		t.Fatalf("decorations enlarged line: %v", h)
	}
	count := func(r, g, b uint8) int {
		img := image.NewRGBA(image.Rect(0, 0, 300, 100))
		res.Root.Draw(qui.NewImageCanvas(img))
		n := 0
		for y := 0; y < 100; y++ {
			for x := 0; x < 300; x++ {
				c := img.RGBAAt(x, y)
				if c.R == r && c.G == g && c.B == b && c.A == 255 {
					n++
				}
			}
		}
		return n
	}
	if count(255, 0, 0) == 0 {
		t.Fatal("resting background missing")
	}
	// The span begins at the line origin; hover through the actual pointer path.
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, 10, 10, 0, 0))
	if count(0, 0, 255) == 0 {
		t.Fatal("hover background missing")
	}
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseMove, 290, 90, 0, 0))
	link.SetFocused(true)
	if count(0, 255, 0) == 0 {
		t.Fatal("focus background missing")
	}
	link.SetFocused(false)
	if count(255, 0, 0) == 0 {
		t.Fatal("resting background not restored")
	}
}

func TestInlineRoundedBorderUsesRoundedStroke(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	res := RenderDoc(`<body><a id="tag">tag</a></body>`, `body{margin:10px;font:16px/24px sans-serif}a{font-size:10px;border:1px solid red;border-radius:5px;padding:0 4px 1px}`, Options{})
	res.Root.Layout(qui.Rect{W: 200, H: 60})
	var rec qui.RecordingCanvas
	res.Root.Draw(&rec)
	if len(rec.Strokes) != 1 {
		t.Fatalf("inline rounded border must be one curved stroke, got %d", len(rec.Strokes))
	}
	if h := rec.Strokes[0].H; h >= 24 {
		t.Fatalf("border follows line box instead of font box: height %v", h)
	}
}
