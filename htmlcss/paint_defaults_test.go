package htmlcss

import (
	"image"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestOutlineRequiresStyle(t *testing.T) {
	for _, tc := range []struct {
		css     string
		visible bool
		width   float32
	}{
		{"outline-color:red", false, 0},
		{"outline-width:2px;outline-color:red", false, 0},
		{"outline:2px solid red", true, 2},
		{"outline-style:solid;outline-color:red", true, 3},
		{"outline:2px solid red;outline-style:none", false, 0},
		{"outline:2px solid red;outline-width:0", false, 0},
	} {
		res := RenderDoc(`<body><div id=x></div></body>`, `#x{`+tc.css+`}`, Options{})
		cs := res.ByID["x"].(*El).lastCS
		if cs.HasOutline != tc.visible || cs.OutlineWidth != tc.width {
			t.Errorf("%s: outline=%v width=%v", tc.css, cs.HasOutline, cs.OutlineWidth)
		}
	}
}

func TestInlineSVGCurrentColorPaint(t *testing.T) {
	res := RenderDoc(`<body><svg id=s width="20" height="20" viewBox="0 0 20 20" fill="none"><rect width="10" height="20" fill="currentColor"/><path d="M15 0V20" stroke="currentColor" stroke-width="4"/></svg></body>`, `svg{color:#087ea4}`, Options{ScriptDOM: true})
	s := res.ByID["s"].(*El)
	assertColor := func(want qui.Color) {
		t.Helper()
		img := s.inlineSVG.Rasterize(20, 20, qui.Color{})
		for _, x := range []int{5, 15} {
			r, g, b, a := img.At(x, 10).RGBA()
			if a < 65000 || absf(float32(r)/65535-want.R) > .01 || absf(float32(g)/65535-want.G) > .01 || absf(float32(b)/65535-want.B) > .01 {
				t.Fatalf("pixel %d: %v, want %v", x, img.At(x, 10), want)
			}
		}
	}
	assertColor(qui.Color{R: 8.0 / 255, G: 126.0 / 255, B: 164.0 / 255, A: 1})
	s.SetAttr("style", "color:red")
	res.Engine.FlushPendingStyles()
	assertColor(qui.Color{R: 1, A: 1})
}

func TestConicGradientPositionAndAngularStops(t *testing.T) {
	g, ok := parseConicGradient("conic-gradient(from 90deg at -10% 100%,#bcc1cd 0deg,#bcc1cd 90deg,#fff 1turn)")
	if !ok {
		t.Fatal("gradient did not parse")
	}
	sh := g.shaderFor(qui.Rect{X: 10, Y: 20, W: 100, H: 200}).(qui.ConicGradient)
	if sh.Center != (qui.Point{X: 0, Y: 220}) || len(sh.Stops) != 3 || sh.Stops[1].Offset != .25 || sh.Stops[2].Offset != 1 {
		t.Fatalf("gradient: %+v", sh)
	}
	// Above/right of the off-box origin falls late in the sweep, near white.
	if c := sh.ColorAt(50, 40); c.R < .9 {
		t.Fatalf("gradient too dark: %v", c)
	}
}

func TestOutsetShadowLeavesTransparentBoxInterior(t *testing.T) {
	res := RenderDoc(`<body><div id=x></div></body>`, `body{margin:0}#x{width:40px;height:20px;margin:10px;border-radius:10px;box-shadow:0 0 0 2px #999}`, Options{})
	res.Root.Layout(qui.Rect{W: 80, H: 50})
	img := image.NewRGBA(image.Rect(0, 0, 80, 50))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(30, 20); c.A != 0 {
		t.Fatalf("shadow filled transparent center: %v", c)
	}
	if c := img.RGBAAt(8, 20); c.A == 0 {
		t.Fatal("outset shadow ring missing")
	}
}

func TestEscapedFunctionalClassGradient(t *testing.T) {
	css := `.bg-\[conic-gradient\(at_top_right\2c _var\(--stops\)\)\]{background-image:conic-gradient(at top right,var(--stops))} .from{--position: ;--stops:red var(--position),blue var(--position)}`
	res := RenderDoc(`<body><div id=x class="bg-[conic-gradient(at_top_right,_var(--stops))] from"></div></body>`, css, Options{})
	e := res.ByID["x"].(*El)
	if e.lastCS.Gradient == nil {
		t.Fatalf("functional class gradient did not match: %+v", mergedDecls(e.node, e.engine.sheet, selectorState{}))
	}
}

func TestInsetShadowPaint(t *testing.T) {
	res := RenderDoc(`<body><div id=x></div></body>`, `body{margin:0}#x{width:40px;height:20px;margin:10px;border-radius:6px;overflow:auto;background:white;box-shadow:inset 0 0 0 2px #999}`, Options{})
	res.Root.Layout(qui.Rect{W: 80, H: 50})
	img := image.NewRGBA(image.Rect(0, 0, 80, 50))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(30, 20); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Fatalf("inset shadow filled center: %v", c)
	}
	if c := img.RGBAAt(11, 20); c.R < 145 || c.R > 160 {
		t.Fatalf("inset shadow edge missing: %v", c)
	}
	if c := img.RGBAAt(9, 20); c.A != 0 {
		t.Fatalf("inset shadow escaped box: %v", c)
	}
}

func TestRoundedScrollViewportClipsChildren(t *testing.T) {
	res := RenderDoc(`<body><div id=x><div></div></div></body>`, `body{margin:0}#x{width:40px;height:20px;margin:10px;border-radius:8px;overflow:auto;background:white}#x>div{height:40px;background:black}`, Options{})
	res.Root.Layout(qui.Rect{W: 80, H: 50})
	img := image.NewRGBA(image.Rect(0, 0, 80, 50))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(10, 10); c.A != 0 {
		t.Fatalf("child overran rounded viewport: %v", c)
	}
	if c := img.RGBAAt(30, 20); c.A != 255 || c.R != 0 {
		t.Fatalf("scroll content missing: %v", c)
	}
}

func TestBackgroundImageSizePositionAndNoRepeat(t *testing.T) {
	res := RenderDoc(`<body><div id=x></div></body>`, `body{margin:0}#x{width:40px;height:20px;background-color:red;background-image:url(`+tinyPNGDataURI(t)+`);background-size:10px 100%;background-position:right center;background-repeat:no-repeat}`, Options{})
	res.Root.Layout(qui.Rect{W: 80, H: 50})
	e := res.ByID["x"].(*El)
	sh := e.Box.BackgroundShader(e.Bounds())
	if c := sh.ColorAt(5, 10); c.A != 0 {
		t.Fatalf("non-repeated image covered empty area: %v", c)
	}
	if c := sh.ColorAt(35, 10); c.A == 0 {
		t.Fatal("image missing at right-aligned tile")
	}
	img := image.NewRGBA(image.Rect(0, 0, 80, 50))
	res.Root.Draw(qui.NewImageCanvas(img))
	if c := img.RGBAAt(5, 10); c.R != 255 || c.G != 0 || c.B != 0 {
		t.Fatalf("background-color missing outside image: %v", c)
	}
}

func TestDropShadowNestedColorArguments(t *testing.T) {
	f, ok := parseFilter("drop-shadow(0 20px 13px rgba(0,0,0,.03)) drop-shadow(0 8px 5px rgba(0,0,0,.08))", 16)
	if !ok {
		t.Fatal("filter did not parse")
	}
	chain := f.(cssFilterChain)
	if len(chain) != 2 {
		t.Fatalf("filter chain: %v", chain)
	}
	for i, want := range []float32{.03, .08} {
		sh := chain[i].image.(qui.DropShadowImageFilter)
		if absf(sh.Color.A-want) > .0001 {
			t.Fatalf("shadow %d opacity=%v, want %v", i, sh.Color.A, want)
		}
	}
}

func TestSquareSectionAtViewportEdge(t *testing.T) {
	res := RenderDoc(`<body><div></div><section id=s></section></body>`, `body{margin:0}div{height:60px}section{height:200px;background:linear-gradient(to right,#eee,#fff);border-top:1px solid #ddd}`, Options{})
	res.Root.Layout(qui.Rect{W: 160, H: 100})
	img := image.NewRGBA(image.Rect(0, 0, 160, 100))
	c := qui.NewImageCanvas(img)
	c.ClipRect(qui.Rect{W: 160, H: 100})
	res.Root.Draw(c)
	for _, p := range []image.Point{{0, 61}, {0, 99}} {
		if img.RGBAAt(p.X, p.Y).A != 255 {
			t.Fatalf("square section corner clipped at %v", p)
		}
	}
}

func TestUnderlineOffsetInheritedByMonospaceLink(t *testing.T) {
	res := RenderDoc(`<body><p><a href="#"><code id=c>__import__()</code></a></p></body>`, `p{text-underline-offset:4px}code{font:15.44px Menlo}`, Options{})
	cs := res.ByID["c"].(*El).lastCS
	paint := cs.decorationPaint()
	if !paint.HasUnderlineOffset || paint.UnderlineOffset != 4 {
		t.Fatal("offset lost in text paint")
	}
}

func TestInlineSVGRootCSSPaint(t *testing.T) {
	res := RenderDoc(`<body><span style="color:#747474"><svg id=s width=20 height=20 viewBox="0 0 20 20"><rect width=20 height=20 /></svg></span></body>`, `svg{fill:currentColor}`, Options{ScriptDOM: true})
	s := res.ByID["s"].(*El)
	check := func(want float32) {
		t.Helper()
		r, _, _, _ := s.inlineSVG.Rasterize(20, 20, qui.Color{}).At(10, 10).RGBA()
		if absf(float32(r)/65535-want) > .01 {
			t.Fatalf("CSS SVG paint got %v want %v", r, want)
		}
	}
	check(116.0 / 255)
	s.SetAttr("style", "fill:red")
	res.Engine.FlushPendingStyles()
	check(1)
	s.RemoveAttr("style")
	res.Engine.FlushPendingStyles()
	check(116.0 / 255)
}
func TestAddressDefaultsToItalic(t *testing.T) {
	res := RenderDoc(`<body><address id="address">Attribution <a id="link" href="#">operator</a></address></body>`, "", Options{})
	for _, id := range []string{"address", "link"} {
		if !res.ByID[id].(*El).lastCS.Italic {
			t.Errorf("#%s must inherit the address UA italic style", id)
		}
	}
	res.ByID["address"].(*El).SetAttr("style", "font-style:normal")
	res.Engine.FlushPendingStyles()
	if res.ByID["link"].(*El).lastCS.Italic {
		t.Fatal("authored normal style must override the UA default")
	}
}

func TestWebNowrapEllipsisUsesTruncatedText(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><body style="margin:0"><h1 id=title style="font:24px/30px Arial;margin:0;width:44px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis">http</h1></body>`)
	e := res.ByID["title"].(*El)
	if e.textLabel == nil {
		t.Fatal("nowrap ellipsis did not reach a text layout")
	}
	p := e.textLabel.Paragraph
	layout := qui.BuildTextLayout("http", e.textLabel.Style().Font, p.LayoutOptions(44))
	if len(layout.Lines) != 1 || !strings.HasSuffix(layout.Lines[0].Text, "…") || layout.Lines[0].Width > 44 {
		t.Fatalf("overflowing title was not ellipsized: %+v", layout.Lines)
	}
}
