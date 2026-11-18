package htmlcss

import (
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// paint draws root into a white 400×300 image.
func paintWhite(root qui.Widget) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	root.Draw(qui.NewImageCanvas(img))
	return img
}

func near(c color.RGBA, r, g, b uint8, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, r) <= tol && d(c.G, g) <= tol && d(c.B, b) <= tol
}

// --- text-shadow ---

func TestParseTextShadows(t *testing.T) {
	cur := qui.Color{R: 1, A: 1}
	got := parseTextShadows("1px 2px 3px #00f, -1px 0 currentColor", 16, cur)
	if len(got) != 2 {
		t.Fatalf("got %d layers, want 2", len(got))
	}
	if got[0].X != 1 || got[0].Y != 2 || got[0].Blur != 3 || got[0].Color.B != 1 {
		t.Errorf("layer 0 = %+v", got[0])
	}
	if got[1].X != -1 || got[1].Color != cur {
		t.Errorf("layer 1 = %+v, want currentColor", got[1])
	}
	if parseTextShadows("none", 16, cur) != nil || parseTextShadows("1px", 16, cur) != nil {
		t.Error("none / a single length must not parse")
	}
}

// text-shadow inherits, reaches both a bare label and folded inline runs,
// and paints the shadow color offset from the text.
func TestTextShadowPaints(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0;font-size:40px">
<div id=a style="text-shadow:30px 0 #f00;color:#fff;height:60px">WWW</div>
<p id=b style="text-shadow:30px 0 #f00;color:#fff;margin:0">MM <b>MM</b></p>
</body></html>`)
	img := paintWhite(res.Root)
	red := func(y0, y1 int) bool {
		for y := y0; y < y1; y++ {
			for x := 0; x < 400; x++ {
				if near(img.RGBAAt(x, y), 255, 0, 0, 40) {
					return true
				}
			}
		}
		return false
	}
	if !red(0, 60) {
		t.Error("text-shadow on a block's text painted no red shadow")
	}
	if !red(60, 120) {
		t.Error("text-shadow on inline runs painted no red shadow")
	}
	var lbl *widgets.Label
	walkWidgets(res.ByID["a"].(*El), func(w qui.Widget) {
		if l, ok := w.(*widgets.Label); ok && lbl == nil {
			lbl = l
		}
	})
	if lbl != nil && len(lbl.TextShadows) != 1 {
		t.Errorf("label TextShadows = %v, want one layer", lbl.TextShadows)
	}
}

// --- clip-path ---

func TestParseClipPath(t *testing.T) {
	for _, v := range []string{
		"inset(10px 20% round 4px)", "circle(50%)", "circle(closest-side at 20px 30px)",
		"ellipse(40% 30% at center)", "polygon(50% 0, 100% 100%, 0 100%)",
		`path("M0 0 L10 0 A5 5 0 0 1 10 10 Z")`, "circle() content-box", "padding-box",
	} {
		if _, ok := parseClipPath(v, 16); !ok {
			t.Errorf("clip-path %q should parse", v)
		}
	}
	for _, v := range []string{"none", "url(#c)", "polygon(1px)", "bogus(1px)"} {
		if _, ok := parseClipPath(v, 16); ok {
			t.Errorf("clip-path %q should not parse", v)
		}
	}
}

// A circle clip hides the corners of the box (paint) and makes them miss
// (hit testing); the center still paints and hits.
func TestClipPathCirclePaintAndHit(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<div id=c style="width:100px;height:100px;background:#00f;clip-path:circle(50%)"></div>
<div id=t style="width:100px;height:100px;background:#0f0;clip-path:polygon(0 0,100% 0,0 100%)"></div>
</body></html>`)
	img := paintWhite(res.Root)
	if c := img.RGBAAt(50, 50); !near(c, 0, 0, 255, 10) {
		t.Errorf("center = %v, want blue", c)
	}
	if c := img.RGBAAt(3, 3); !near(c, 255, 255, 255, 10) {
		t.Errorf("clipped corner = %v, want white", c)
	}
	if c := img.RGBAAt(10, 110); !near(c, 0, 255, 0, 10) {
		t.Errorf("triangle inside = %v, want green", c)
	}
	if c := img.RGBAAt(90, 190); !near(c, 255, 255, 255, 10) {
		t.Errorf("triangle outside = %v, want white", c)
	}
	el := res.ByID["c"].(*El)
	if hit := res.Root.HitTest(qui.Point{X: 3, Y: 3}); hit == qui.Widget(el) {
		t.Error("a point clipped away by clip-path must not hit the element")
	}
	if hit := res.Root.HitTest(qui.Point{X: 50, Y: 50}); hit != qui.Widget(el) {
		t.Errorf("center hit = %T, want the clipped element", hit)
	}
}

// --- mask-image ---

func TestParseMask(t *testing.T) {
	ms, ok := parseMask(map[string]string{"-webkit-mask": `url(i.svg) no-repeat center / contain`}, 16)
	if !ok || ms.url != "i.svg" || ms.repeatX || ms.size != "contain" || ms.posX.pct != 0.5 {
		t.Fatalf("-webkit-mask shorthand = %+v ok=%v", ms, ok)
	}
	ms, ok = parseMask(map[string]string{"mask-image": "linear-gradient(#000, transparent)", "mask-size": "10px 20px", "mask-repeat": "repeat-x"}, 16)
	if !ok || ms.grad == nil || ms.sizeW == nil || ms.sizeW.px != 10 || !ms.repeatX || ms.repeatY {
		t.Fatalf("mask longhands = %+v ok=%v", ms, ok)
	}
	if _, ok := parseMask(map[string]string{"mask-image": "none"}, 16); ok {
		t.Error("mask-image:none should not parse")
	}
}

// The icon idiom: background-color: currentColor shaped by an SVG mask.
func TestMaskImageSVGIcon(t *testing.T) {
	svgSrc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect x="0" y="0" width="5" height="10" fill="#000"/></svg>`
	uri := "data:image/svg+xml," + url.PathEscape(svgSrc)
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<i id=i style="display:block;width:40px;height:40px;color:#f00;background-color:currentColor;-webkit-mask:url('`+uri+`') no-repeat center / contain;mask:url('`+uri+`') no-repeat center / contain"></i>
<div id=g style="width:100px;height:40px;background:#00f;mask-image:linear-gradient(to right, #000, transparent)"></div>
</body></html>`)
	img := paintWhite(res.Root)
	if c := img.RGBAAt(10, 20); !near(c, 255, 0, 0, 20) {
		t.Errorf("masked-in half = %v, want red", c)
	}
	if c := img.RGBAAt(30, 20); !near(c, 255, 255, 255, 20) {
		t.Errorf("masked-out half = %v, want white", c)
	}
	l, r := img.RGBAAt(5, 60), img.RGBAAt(95, 60)
	if !(l.B > 200 && l.R < 60) || r.R < 200 {
		t.Errorf("gradient mask left=%v right=%v, want opaque blue fading to white", l, r)
	}
}

// --- filter ---

func TestParseFilterChain(t *testing.T) {
	f, ok := parseFilter("grayscale(1) blur(2px) brightness(50%)", 16)
	if !ok {
		t.Fatal("chain should parse")
	}
	ch, isChain := f.(cssFilterChain)
	if !isChain || len(ch) != 3 {
		t.Fatalf("got %T %v, want a 3-step chain", f, f)
	}
	for _, v := range []string{"contrast(2)", "sepia()", "invert(100%)", "saturate(0)", "hue-rotate(90deg)", "opacity(.5)"} {
		if _, ok := parseFilter(v, 16); !ok {
			t.Errorf("%s should parse", v)
		}
	}
}

func TestFilterColorFunctionsPaint(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<div style="width:40px;height:20px;background:#f00;filter:grayscale(1)"></div>
<div style="width:40px;height:20px;background:#f00;filter:invert(1)"></div>
<div style="width:40px;height:20px;background:#f00;filter:hue-rotate(120deg)"></div>
<div style="width:40px;height:20px;background:#f00;filter:opacity(0)"></div>
</body></html>`)
	img := paintWhite(res.Root)
	if c := img.RGBAAt(20, 10); c.R != c.G || c.G != c.B || c.R > 120 {
		t.Errorf("grayscale(1) of red = %v, want a dark gray", c)
	}
	if c := img.RGBAAt(20, 30); !near(c, 0, 255, 255, 10) {
		t.Errorf("invert(1) of red = %v, want cyan", c)
	}
	if c := img.RGBAAt(20, 50); !(c.G > c.R && c.G > c.B) {
		t.Errorf("hue-rotate(120deg) of red = %v, want green dominant", c)
	}
	if c := img.RGBAAt(20, 70); !near(c, 255, 255, 255, 5) {
		t.Errorf("opacity(0) = %v, want invisible", c)
	}
}

// --- list markers and counters ---

func markers(t *testing.T, html string) map[string]string {
	t.Helper()
	_, res := renderWeb(t, html)
	out := map[string]string{}
	for id, w := range res.ByID {
		if e, ok := w.(*El); ok {
			out[id] = e.marker
		}
	}
	return out
}

func TestListMarkerTypesAndAttributes(t *testing.T) {
	m := markers(t, `<!doctype html><html><body>
<ol start=3><li id=a>x</li><li id=b value=10>x</li><li id=c>x</li></ol>
<ol reversed><li id=r1>x</li><li id=r2>x</li><li id=r3>x</li></ol>
<ol style="list-style-type:lower-roman"><li>x</li><li>x</li><li>x</li><li id=iv>x</li></ol>
<ol type=A><li>x</li><li id=B>x</li></ol>
<ul><li id=disc>x<ul><li id=circle>x<ul><li id=square>x</li></ul></li></ul></li></ul>
<ul style="list-style:none"><li id=none>x</li></ul>
<ul><li id=flex style="display:flex">x</li><li id=ib style="display:inline-block">x</li></ul>
<div><div id=li style="display:list-item;list-style-type:upper-alpha">x</div></div>
</body></html>`)
	want := map[string]string{
		"a": "3.", "b": "10.", "c": "11.",
		"r1": "3.", "r2": "2.", "r3": "1.",
		"iv": "iv.", "B": "B.",
		"disc": "•", "circle": "◦", "square": "▪",
		"none": "", "flex": "", "ib": "", "li": "A.",
	}
	for id, w := range want {
		if m[id] != w {
			t.Errorf("#%s marker = %q, want %q", id, m[id], w)
		}
	}
}

func TestMarkerPseudoAndCounters(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><head><style>
#cm li::marker { content: "[" counter(list-item) "] "; color: #f00 }
body { counter-reset: sec }
h2 { counter-increment: sec; counter-reset: sub }
h2::before { content: counter(sec) ". " }
h3 { counter-increment: sub }
h3::before { content: counter(sec) "." counter(sub, lower-alpha) " " }
.toc { counter-reset: item }
.toc li { display: block; counter-increment: item }
.toc li::before { content: counters(item, ".") " " }
</style></head><body>
<ol id=cm><li id=m1>x</li><li id=m2>x</li></ol>
<h2 id=h1>A</h2><h3 id=h11>a</h3><h3 id=h12>b</h3><h2 id=h2>B</h2><h3 id=h21>c</h3>
<ol class=toc><li id=t1>x<ol class=toc><li id=t11>y</li><li id=t12>z</li></ol></li><li id=t2>w</li></ol>
</body></html>`)
	el := func(id string) *El { return res.ByID[id].(*El) }
	if got := el("m2").marker; got != "[2] " {
		t.Errorf("::marker content = %q, want %q", got, "[2] ")
	}
	if lbl := el("m2").markerLabel; lbl == nil || lbl.Style().Foreground != (qui.Color{R: 1, A: 1}) {
		t.Error("::marker color should paint the marker red")
	}
	for id, want := range map[string]string{
		"h1": "1. ", "h11": "1.a ", "h12": "1.b ", "h2": "2. ", "h21": "2.a ",
		"t1": "1 ", "t11": "1.1 ", "t12": "1.2 ", "t2": "2 ",
	} {
		if got := el(id).pbText; got != want {
			t.Errorf("#%s ::before = %q, want %q", id, got, want)
		}
	}
	if el("t1").marker != "" {
		t.Errorf("display:block li got marker %q", el("t1").marker)
	}
}

func TestFormatCounter(t *testing.T) {
	for _, c := range []struct {
		n     int
		style string
		want  string
	}{
		{1, "decimal", "1"}, {27, "lower-alpha", "aa"}, {4, "upper-roman", "IV"},
		{1994, "lower-roman", "mcmxciv"}, {7, "decimal-leading-zero", "07"}, {0, "lower-alpha", "0"},
		{2, "lower-greek", "β"}, {3, `"-"`, "-"},
	} {
		if got := formatCounter(c.n, c.style); got != c.want {
			t.Errorf("formatCounter(%d, %s) = %q, want %q", c.n, c.style, got, c.want)
		}
	}
}

// TestSnapshotEffects writes the effects page to a PNG when
// QUI_HTMLCSS_SNAPSHOT=1, for visual review.
func TestSnapshotEffects(t *testing.T) {
	if os.Getenv("QUI_HTMLCSS_SNAPSHOT") != "1" {
		t.Skip("set QUI_HTMLCSS_SNAPSHOT=1 to write /tmp/qui-htmlcss/effects.png")
	}
	icon := "data:image/svg+xml," + url.PathEscape(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 2l3 7h7l-5.5 4.5 2 7.5L12 17l-6.5 4 2-7.5L2 9h7z"/></svg>`)
	html := `<!doctype html><html><body style="margin:16px;font-family:sans-serif">
<h1 style="text-shadow:2px 2px 4px rgba(0,0,0,.5), 0 0 1px #f0f;color:#36c">Text shadow</h1>
<div style="display:flex;gap:12px">
 <div style="width:80px;height:80px;background:linear-gradient(#f80,#c0f);clip-path:circle(40%)"></div>
 <div style="width:80px;height:80px;background:#3a3;clip-path:polygon(50% 0,100% 50%,50% 100%,0 50%)"></div>
 <div style="width:80px;height:80px;background:#39f;clip-path:inset(10px round 16px)"></div>
 <div style="width:80px;height:80px;background:#e33;clip-path:path('M40 5 A35 35 0 1 1 39.9 5 Z')"></div>
</div>
<div style="display:flex;gap:12px;margin-top:12px;color:#c33">
 <i style="display:block;width:48px;height:48px;background:currentColor;mask:url('` + icon + `') no-repeat center/contain"></i>
 <i style="display:block;width:48px;height:48px;background:#36c;-webkit-mask-image:url('` + icon + `');-webkit-mask-size:16px"></i>
 <div style="width:120px;height:48px;background:#333;mask-image:linear-gradient(to right,#000,transparent)"></div>
</div>
<div style="display:flex;gap:8px;margin-top:12px">
 <div style="width:60px;height:40px;background:#f60;filter:grayscale(1)"></div>
 <div style="width:60px;height:40px;background:#f60;filter:sepia(1)"></div>
 <div style="width:60px;height:40px;background:#f60;filter:hue-rotate(180deg)"></div>
 <div style="width:60px;height:40px;background:#f60;filter:brightness(.5) contrast(2)"></div>
 <div style="width:60px;height:40px;background:#f60;filter:invert(1) blur(2px)"></div>
 <div style="width:60px;height:40px;background:#f60;filter:drop-shadow(4px 4px 2px #000)"></div>
</div>
<ol start=4 style="list-style-type:upper-roman"><li>four</li><li>five</li></ol>
<ul><li>disc<ul><li>circle<ul><li>square</li></ul></li></ul></li></ul>
<style>.m li::marker{color:#e33;content:"→ "}</style><ul class=m><li>marker content</li></ul>
</body></html>`
	win := qui.NewTestWindow(qui.Size{W: 600, H: 600})
	prev := webCompat
	SetWebCompat(true)
	defer SetWebCompat(prev)
	res := RenderDoc(html, "", Options{Window: win, DocumentRoot: true, ViewportWidth: 600})
	win.SetRoot(res.Root)
	res.Root.Measure(qui.Size{W: 600, H: 600})
	res.Root.Layout(qui.Rect{W: 600, H: 600})
	img := image.NewRGBA(image.Rect(0, 0, 600, 600))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	res.Root.Draw(qui.NewImageCanvas(img))
	_ = os.MkdirAll("/tmp/qui-htmlcss", 0o755)
	f, err := os.Create("/tmp/qui-htmlcss/effects.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// backdrop-filter blurs what is painted behind the box: a hard red/white
// edge under it turns into a gradient; outside the box it stays hard.
func TestBackdropFilterBlur(t *testing.T) {
	_, res := renderWeb(t, `<!doctype html><html><body style="margin:0">
<div style="position:relative;height:100px;background:linear-gradient(to right,#f00 50%,#fff 50%)">
 <div style="position:absolute;left:100px;top:0;width:200px;height:50px;backdrop-filter:blur(8px)"></div>
</div></body></html>`)
	img := paintWhite(res.Root)
	under := img.RGBAAt(198, 25) // just left of the edge, under the box
	if under.G < 40 {
		t.Errorf("pixel under the backdrop at the edge = %v, want blurred toward white", under)
	}
	if out := img.RGBAAt(198, 75); out.G > 10 {
		t.Errorf("pixel outside the backdrop = %v, want pure red", out)
	}
}
