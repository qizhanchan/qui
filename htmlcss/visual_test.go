package htmlcss

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

func tinyPNGDataURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestRadialGradientParse(t *testing.T) {
	g, ok := parseGradient("radial-gradient(circle at center, #ff0000, #0000ff)")
	if !ok {
		t.Fatal("radial-gradient should parse")
	}
	if !g.radial {
		t.Error("gradient should be radial")
	}
	if len(g.stops) != 2 {
		t.Errorf("radial gradient stops = %d, want 2", len(g.stops))
	}
}

func TestDataURIImageDecodes(t *testing.T) {
	img := loadRasterImage(tinyPNGDataURI(t), "")
	if img == nil {
		t.Fatal("data: URI image should decode")
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Errorf("decoded image bounds = %v, want 2x2", img.Bounds())
	}
}

func TestBackgroundImageURLSetsShader(t *testing.T) {
	css := `#hero { background-image: url(` + tinyPNGDataURI(t) + `); }`
	res := RenderDoc(`<body><div id="hero">x</div></body>`, css, Options{})
	el := res.ByID["hero"].(*El)
	if el.bgImg == nil {
		t.Fatal("background-image url() should load an image")
	}
	if el.Box.BackgroundShader == nil {
		t.Error("background-image should install a BackgroundShader")
	}
}

func TestParseTransformSkewMatrixOrigin(t *testing.T) {
	if ts, ok := parseTransform("skewX(15deg)", 16); !ok || ts.kx == 0 {
		t.Errorf("skewX should set kx, got %+v ok=%v", ts, ok)
	}
	if ts, ok := parseTransform("skew(10deg, 20deg)", 16); !ok || ts.kx == 0 || ts.ky == 0 {
		t.Errorf("skew should set kx+ky, got %+v", ts)
	}
	if ts, ok := parseTransform("matrix(1,0,0,1,30,40)", 16); !ok || ts.matrix == nil || ts.matrix.TX != 30 || ts.matrix.TY != 40 {
		t.Errorf("matrix() should set the affine matrix, got %+v", ts)
	}
	ox, oy, ok := parseTransformOrigin("left bottom")
	if !ok || ox != 0 || oy != 1 {
		t.Errorf("transform-origin left bottom = (%v,%v), want (0,1)", ox, oy)
	}
	if ox, oy, _ := parseTransformOrigin("25% 75%"); ox != 0.25 || oy != 0.75 {
		t.Errorf("transform-origin 25%% 75%% = (%v,%v)", ox, oy)
	}
}

func TestParseBoxShadowMultiLayer(t *testing.T) {
	layers := parseBoxShadows("0 1px 2px #000, 0 4px 8px rgba(0,0,0,0.3)", 16)
	if len(layers) != 2 {
		t.Fatalf("multi-layer box-shadow = %d layers, want 2", len(layers))
	}
	if layers[1].Y != 4 || layers[1].Blur != 8 {
		t.Errorf("second layer = %+v, want Y=4 Blur=8", layers[1])
	}
	// inset layers are dropped.
	if got := parseBoxShadows("inset 0 0 4px #000", 16); len(got) != 0 {
		t.Errorf("inset shadow should be dropped, got %d", len(got))
	}
}

func TestZIndexPaintOrder(t *testing.T) {
	res := RenderDoc(`<body>
		<div id="a" style="position:absolute;z-index:1">a</div>
		<div id="b" style="position:absolute;z-index:5">b</div>
		<div id="c" style="position:absolute;z-index:3">c</div>
	</body>`, ``, Options{})
	if z := res.ByID["b"].(*El).ZIndex(); z != 5 {
		t.Errorf("b z-index = %d, want 5", z)
	}
	// Paint order sorts ascending: a(1), c(3), b(5).
	kids := []qui.Widget{res.ByID["a"], res.ByID["c"], res.ByID["b"]}
	shuffled := []qui.Widget{res.ByID["b"], res.ByID["a"], res.ByID["c"]}
	ordered := qui.ChildrenInPaintOrder(shuffled)
	for i := range ordered {
		if ordered[i] != kids[i] {
			t.Errorf("paint order[%d] wrong: got %v", i, ordered[i].(*El).attrs["id"])
		}
	}
}

func TestMediaQuery(t *testing.T) {
	css := `.box{color:red} @media (min-width: 600px){ .box{color:green} } @media (max-width: 500px){ .box{color:blue} }`
	// Wide viewport: min-width:600 matches → green wins (later source order).
	wide := ParseCSSViewport(css, 800)
	got := computeNode(&Node{Type: ElementNode, Tag: "div", Attrs: map[string]string{"class": "box"}}, wide, nil)
	if got.Color.G < 0.4 || got.Color.R > 0.1 {
		t.Errorf("wide viewport color = %+v, want green (@media min-width:600 applies)", got.Color)
	}
	// Narrow viewport: only max-width:500 matches → blue.
	narrow := ParseCSSViewport(css, 400)
	got2 := computeNode(&Node{Type: ElementNode, Tag: "div", Attrs: map[string]string{"class": "box"}}, narrow, nil)
	if got2.Color.B < 0.4 {
		t.Errorf("narrow viewport color = %+v, want blue (@media max-width:500 applies)", got2.Color)
	}
	// Mid viewport: neither → base red.
	mid := ParseCSSViewport(css, 550)
	got3 := computeNode(&Node{Type: ElementNode, Tag: "div", Attrs: map[string]string{"class": "box"}}, mid, nil)
	if got3.Color.R < 0.9 {
		t.Errorf("mid viewport color = %+v, want red (no @media applies)", got3.Color)
	}
}

func TestBeforeAfterContent(t *testing.T) {
	res := RenderDoc(`<body><span id="s">core</span></body>`,
		`#s::before{content:"[";} #s::after{content:"]";}`, Options{})
	el := res.ByID["s"].(*El)
	if el.pbText != "[" || el.paText != "]" {
		t.Fatalf("pseudo content = before %q after %q, want [ and ]", el.pbText, el.paText)
	}
	// The generated content folds into a single inline run with the text.
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	var ib *widgets.InlineBox
	for _, k := range el.flowKids {
		if box, ok := k.(*widgets.InlineBox); ok {
			ib = box
		}
	}
	if ib == nil {
		t.Fatal("::before/::after leaf should produce an InlineBox flow kid")
	}
	if txt := ib.Text(); txt != "[core]" {
		t.Errorf("inline run text = %q, want [core]", txt)
	}
}

func TestBeforeAfterAttrContent(t *testing.T) {
	res := RenderDoc(`<body><a id="s" href="https://x" data-label="Docs">core</a></body>`,
		`#s::before{content:attr(data-label) ": ";} #s::after{content:" (" attr(href) ")";}`, Options{})
	el := res.ByID["s"].(*El)
	if el.pbText != "Docs: " {
		t.Errorf("::before attr content = %q, want %q", el.pbText, "Docs: ")
	}
	if el.paText != " (https://x)" {
		t.Errorf("::after attr content = %q, want %q", el.paText, " (https://x)")
	}
}

func TestUnquoteContent(t *testing.T) {
	n := &Node{Type: ElementNode, Tag: "span", Attrs: map[string]string{"data-x": "42", "title": "hi"}}
	cases := []struct{ in, want string }{
		{`"foo"`, "foo"},
		{`'bar'`, "bar"},
		{`none`, ""},
		{`normal`, ""},
		{`attr(data-x)`, "42"},
		{`attr(missing)`, ""},
		{`"[" attr(data-x) "]"`, "[42]"},
		{`attr(title, "def")`, "hi"},       // type/fallback syntax → use name
		{`counter(list)`, "counter(list)"}, // unsupported → verbatim
	}
	for _, c := range cases {
		if got := unquoteContent(c.in, n); got != c.want {
			t.Errorf("unquoteContent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestConicGradientParse(t *testing.T) {
	g, ok := parseGradient("conic-gradient(from 90deg, red, blue, green)")
	if !ok || !g.conic {
		t.Fatalf("conic-gradient should parse as conic, got ok=%v %+v", ok, g)
	}
	if len(g.stops) != 3 {
		t.Errorf("conic stops = %d, want 3", len(g.stops))
	}
	if g.angleDeg < 89 || g.angleDeg > 91 {
		t.Errorf("conic start angle = %v, want ~90", g.angleDeg)
	}
}

func TestParseFilter(t *testing.T) {
	if f, ok := parseFilter("blur(4px)", 16); !ok {
		t.Error("blur(4px) should parse")
	} else if _, isBlur := f.(qui.BlurImageFilter); !isBlur {
		t.Errorf("blur() → %T, want BlurImageFilter", f)
	}
	if f, ok := parseFilter("drop-shadow(2px 3px 5px #000)", 16); !ok {
		t.Error("drop-shadow should parse")
	} else if ds, isDS := f.(qui.DropShadowImageFilter); !isDS || ds.Offset.X != 2 || ds.Blur != 5 {
		t.Errorf("drop-shadow → %+v, want offset(2,3) blur5", f)
	}
	if _, ok := parseFilter("none", 16); ok {
		t.Error("filter:none should not parse to a filter")
	}
}
