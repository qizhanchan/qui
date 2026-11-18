package htmlcss

import (
	"github.com/qizhanchan/qui"
	"testing"
)

func TestRegisteredPropertyInitialPaintAndInheritance(t *testing.T) {
	r := RenderDoc(`<body><div id="parent"><div id="child"></div><div id="inherit"></div></div></body>`, `
 @property --ring-size {syntax:"<length>";inherits:false;initial-value:1px}
 @property --tint {syntax:"<color>";inherits:true;initial-value:blue}
 #parent{--ring-size:4px;--tint:red}
 #child,#inherit{box-shadow:0 0 0 var(--ring-size) var(--tint)}
 #inherit{--ring-size:inherit}
 `, Options{})
	child := r.ByID["child"].(*El).lastCS
	inherited := r.ByID["inherit"].(*El).lastCS
	if !child.HasShadow || child.Shadow.Spread != 1 {
		t.Fatalf("initial ring missing: %+v", child.Shadow)
	}
	if inherited.Shadow.Spread != 4 {
		t.Fatalf("explicit inherit failed: %+v", inherited.Shadow)
	}
	if child.Shadow.Color.R != 1 {
		t.Fatal("inheriting registered property lost parent color")
	}
}

func TestInlineSVGDescendantStyles(t *testing.T) {
	r := RenderDoc(`<body><svg id="icon" width="20" height="20" viewBox="0 0 20 20" fill="none"><path id="path" class="shape" d="M0 0h20v20H0z"/></svg></body>`, `:root{--paint:lab(60% -10 -30)}.shape{fill:var(--paint)}`, Options{ScriptDOM: true})
	e := r.ByID["icon"].(*El)
	img := e.inlineSVG.Rasterize(20, 20, qui.Color{})
	_, _, _, a := img.At(10, 10).RGBA()
	if a < 65000 {
		t.Fatal("class-styled SVG descendant is transparent")
	}
	r.ByID["path"].(*El).SetAttr("style", "fill:red")
	r.Engine.FlushPendingStyles()
	img = e.inlineSVG.Rasterize(20, 20, qui.Color{})
	red, green, _, a := img.At(10, 10).RGBA()
	if a < 65000 || red < 65000 || green > 100 {
		t.Fatal("SVG descendant style mutation did not repaint")
	}
}

func TestRepeatedGradientTilePaint(t *testing.T) {
	r := RenderDoc(`<div id="x"></div>`, `#x{background-image:repeating-linear-gradient(90deg,red 0 1px,transparent 0 50%);background-size:8px 8px}`, Options{})
	e := r.ByID["x"].(*El)
	if e.Box.BackgroundShader == nil {
		t.Fatal("repeating gradient missing")
	}
	sh := e.Box.BackgroundShader(qui.Rect{W: 40, H: 20})
	for _, x := range []float32{.5, 4.5, 8.5, 12.5} {
		if sh.ColorAt(x, 2).A < .9 {
			t.Errorf("stripe missing at %v", x)
		}
	}
	for _, x := range []float32{2, 6, 10, 14} {
		if sh.ColorAt(x, 2).A > .1 {
			t.Errorf("transparent stripe painted at %v", x)
		}
	}
}
