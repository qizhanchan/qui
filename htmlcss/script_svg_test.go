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

func TestShadowScopedSVGDescendantPaint(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	defer SetWebCompat(prev)
	res := RenderDoc(`<body style="margin:0"><div><template shadowrootmode="open"><style>:host{display:block}svg{display:block;width:40px;height:20px} .mark{fill:#0666dd}</style><svg viewBox="0 0 40 20"><path class="mark" d="M0 0h40v20H0z"/></svg></template></div></body>`, `.mark{fill:red}`, Options{DocumentRoot: true, ScriptDOM: true})
	res.Root.Layout(qui.Rect{W: 100, H: 100})
	img := paintWhite(res.Root)
	if c := img.RGBAAt(10, 10); !near(c, 6, 102, 221, 2) {
		t.Fatalf("scoped SVG paint: %v", c)
	}
}

func TestFoldedAbsolutePseudoPaint(t *testing.T) {
	prev := webCompat
	SetWebCompat(true)
	defer SetWebCompat(prev)
	res := RenderDoc(`<body style="margin:0"><p><a id="anchor">text</a> next</p></body>`, `p{margin:0;line-height:28px}a{padding-right:20px;position:relative}a:after{content:"";position:absolute;width:16px;height:16px;margin-left:2px;background:red}`, Options{DocumentRoot: true})
	res.Root.Layout(qui.Rect{W: 200, H: 100})
	img := paintWhite(res.Root)
	red := 0
	for y := 0; y < 28; y++ {
		for x := 25; x < 70; x++ {
			c := img.RGBAAt(x, y)
			if c.R > 240 && c.G < 10 {
				red++
			}
		}
	}
	if red < 200 {
		t.Fatalf("absolute generated content painted only %d pixels", red)
	}
}
