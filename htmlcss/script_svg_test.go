package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestScriptCreatedSVGPaintsAfterMutation(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	t.Cleanup(func() { SetWebCompat(prev) })
	res := RenderDoc(`<body style="margin:0"></body>`, "", Options{ScriptDOM: true})
	icon := res.Engine.NewEl("svg")
	icon.SetAttr("viewbox", "0 0 80 48")
	icon.SetAttr("style", "width:50px;height:30px;display:block;color:#b80000;fill:currentColor")
	path := res.Engine.NewEl("path")
	path.SetAttr("d", "M0 0h80v48H0z")
	icon.SetElementChildren([]qui.Widget{path})
	res.Root.(*El).SetElementChildren([]qui.Widget{icon})
	icon.RefreshInlineSVG()
	res.Engine.Restyle()
	res.Root.Layout(qui.Rect{W: 400, H: 300})
	img := paintWhite(res.Root)
	if got := img.RGBAAt(25, 15); !near(got, 184, 0, 0, 2) {
		t.Fatalf("script-created SVG painted %v, want red", got)
	}
	path.SetAttr("d", "M0 0h40v48H0z")
	icon.RefreshInlineSVG()
	res.Root.Layout(qui.Rect{W: 400, H: 300})
	img = paintWhite(res.Root)
	if got := img.RGBAAt(40, 15); !near(got, 255, 255, 255, 2) {
		t.Fatalf("SVG path mutation left old pixels: %v", got)
	}
}
