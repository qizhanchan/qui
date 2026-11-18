package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestDocumentViewportUnitsRemainIsolated(t *testing.T) {
	old := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(old) })
	parent := RenderDoc(`<body style="margin:0"><div id=box style="width:100vw;height:50vh"></div></body>`, "", Options{DocumentRoot: true, ViewportWidth: 640, ViewportHeight: 480})
	child := RenderDoc(`<body style="margin:0"><div id=box style="width:100vw;height:50vh"></div></body>`, "", Options{DocumentRoot: true, ViewportWidth: 300, ViewportHeight: 150})
	parent.Engine.SetCSS(`body{margin:0}#box{max-width:100vw}`)
	parent.Root.Layout(qui.Rect{W: 640, H: 480})
	if b := parent.ByID["box"].Bounds(); b.W != 640 || b.H != 240 {
		t.Fatalf("parent after frame CSS: %v", b)
	}
	parent.ByID["box"].(*El).SetAttr("style", "width:50vw;height:25vh")
	parent.Engine.FlushPendingStyles()
	parent.Root.Layout(qui.Rect{W: 640, H: 480})
	if b := parent.ByID["box"].Bounds(); b.W != 320 || b.H != 120 {
		t.Fatalf("parent inline restyle: %v", b)
	}
	child.Engine.SetCSS(`body{margin:0}#box{max-width:100vw}`)
	child.Root.Layout(qui.Rect{W: 300, H: 150})
	if b := child.ByID["box"].Bounds(); b.W != 300 || b.H != 75 {
		t.Fatalf("frame after parent CSS: %v", b)
	}
	parent.Engine.SetViewport(640, 600)
	parent.Root.Layout(qui.Rect{W: 640, H: 600})
	if b := parent.ByID["box"].Bounds(); b.H != 150 {
		t.Fatalf("height-only resize: %v", b)
	}
}
