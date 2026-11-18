package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

func TestScriptDOMBuildsNonRenderedElements(t *testing.T) {
	src := `<!doctype html><html><head><title>T</title><meta name="csrf" content="x">
<script type="application/json" id="data">{"a":  "b  c"}</script></head>
<body><p>one</p><script>var x = 1;</script><p>two</p></body></html>`
	layout := func(opts Options) (RenderResult, qui.Rect) {
		opts.ViewportWidth = 800
		opts.DocumentRoot = true
		res := RenderDoc(src, "", opts)
		root := res.Root
		root.Layout(qui.Rect{W: 800, H: 600})
		ps := mustSelectors(t, "p").QueryAll(root.(*El), 0)
		if len(ps) != 2 {
			t.Fatalf("got %d <p>", len(ps))
		}
		return res, ps[1].Bounds()
	}
	plain, want := layout(Options{})
	if mustSelectors(t, "head, script").QueryAll(plain.Root.(*El), 0) != nil {
		t.Fatal("non-rendered elements built without ScriptDOM")
	}
	res, got := layout(Options{ScriptDOM: true})
	if got != want {
		t.Fatalf("ScriptDOM moved the second <p>: %v, want %v", got, want)
	}
	root := res.Root.(*El)
	if n := len(mustSelectors(t, "head > meta[name=csrf], head > title, body > script").QueryAll(root, 0)); n != 3 {
		t.Fatalf("matched %d non-rendered elements, want 3", n)
	}
	data, _ := res.ByID["data"].(*El)
	if data == nil || data.TextContent() != `{"a":  "b  c"}` {
		t.Fatalf("data block text not verbatim: %+v", data)
	}
}

func mustSelectors(t *testing.T, s string) SelectorList {
	t.Helper()
	l, ok := CompileSelectors(s)
	if !ok {
		t.Fatalf("bad selector %q", s)
	}
	return l
}

func TestSetViewportReevaluatesMedia(t *testing.T) {
	src := `<html><body><div id=d>x</div></body></html>`
	css := `#d { width: 100px; } @media (max-width: 600px) { #d { width: 50px; } }`
	res := RenderDoc(src, css, Options{ViewportWidth: 800, DocumentRoot: true})
	d := res.ByID["d"].(*El)
	width := func() float32 {
		res.Root.Layout(qui.Rect{W: 800, H: 600})
		return d.Bounds().W
	}
	if w := width(); w != 100 {
		t.Fatalf("wide: %v", w)
	}
	res.Engine.SetViewport(500, 600)
	if w := width(); w != 50 {
		t.Fatalf("narrow after SetViewport: %v", w)
	}
	res.Engine.SetViewport(900, 600)
	if w := width(); w != 100 {
		t.Fatalf("wide again: %v", w)
	}
}

func TestScriptDOMCommentsKeepLayout(t *testing.T) {
	// A browser document (webCompat): hidden children never split a line.
	SetWebCompat(true)
	defer SetWebCompat(false)
	src := `<!doctype html><html><body><p id=p>Hello <!-- -->world<!-- x --> and <b>bold</b><!--$--> tail</p>
<ul id=u><!-- lead --><li id=first>a</li> <li>b</li>
</ul> <div id=after>z</div> <div id=btns><button>A</button> <button>B</button>
<div id=blk>block</div> </div><section id=sec>
  <div>one</div>
  <div>two</div>
</section></body></html>`
	css := `li:first-child { color: red; height: 30px; } #u > :nth-child(2) { height: 20px; }`
	measure := func(opts Options) (map[string]qui.Rect, *El) {
		opts.ViewportWidth = 800
		opts.DocumentRoot = true
		res := RenderDoc(src, css, opts)
		res.Root.Layout(qui.Rect{W: 800, H: 600})
		out := map[string]qui.Rect{}
		for _, id := range []string{"p", "u", "first", "after", "btns", "blk", "sec"} {
			out[id] = res.ByID[id].(*El).Bounds()
		}
		return out, res.ByID["p"].(*El)
	}
	want, _ := measure(Options{})
	got, p := measure(Options{ScriptDOM: true})
	near := func(a, b float32) bool { return a-b < 0.1 && b-a < 0.1 }
	for id, r := range want {
		g := got[id]
		if !near(g.X, r.X) || !near(g.Y, r.Y) || !near(g.W, r.W) || !near(g.H, r.H) {
			t.Errorf("%s: %v with comments and whitespace nodes, %v without", id, g, r)
		}
	}
	comments := 0
	for _, k := range p.Kids() {
		if c, ok := k.(*El); ok && c.IsComment() {
			comments++
		}
	}
	if comments != 3 {
		t.Errorf("<p> has %d comment children, want 3", comments)
	}
	if got := p.TextContent(); got != "" {
		// mixed content: the text lives in segments
		_ = got
	}
}

func TestScriptDOMInlineSVG(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	src := `<!doctype html><html><body><p>icon <svg id=s width="24" height="24" viewBox="0 0 24 24"><path id=pa d="M0 0h24v24H0z" fill="red"/></svg> text</p>
<svg id=s2 viewBox="0 0 10 5" style="width:40px"><circle cx=5 cy=2 r=2 /></svg></body></html>`
	res := RenderDoc(src, "", Options{ViewportWidth: 800, DocumentRoot: true, ScriptDOM: true})
	res.Root.Layout(qui.Rect{W: 800, H: 600})
	s := res.ByID["s"].(*El)
	if b := s.Bounds(); b.W != 24 || b.H != 24 {
		t.Errorf("inline svg box %v, want 24x24", b)
	}
	if b := res.ByID["s2"].(*El).Bounds(); b.W != 40 || b.H != 20 {
		t.Errorf("css-sized svg box %v, want 40x20", b)
	}
	pa := res.ByID["pa"].(*El)
	if pa.ParentEl() != s || pa.Tag() != "path" {
		t.Fatalf("svg subtree not in the DOM")
	}
	if s.inlineSVG == nil || s.inlineSVG.ViewBox.W != 24 {
		t.Fatalf("svg drawing not parsed: %+v", s.inlineSVG)
	}
	pa.SetAttr("fill", "blue")
	s.RefreshInlineSVG()
	if s.inlineSVG == nil {
		t.Fatal("refresh lost the drawing")
	}
}

func TestAdjacentTextIsOneFlexItem(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	width := func(src string) float32 {
		res := RenderDoc(src, "", Options{ViewportWidth: 800, DocumentRoot: true, ScriptDOM: true})
		res.Root.Layout(qui.Rect{W: 800, H: 600})
		return res.ByID["x"].(*El).Bounds().W
	}
	one := width(`<html><body><div style="display:flex"><a id=x style="display:flex;column-gap:20px">v19.3</a></div></body></html>`)
	split := width(`<html><body><div style="display:flex"><a id=x style="display:flex;column-gap:20px">v<!-- -->19.3</a></div></body></html>`)
	if one != split {
		t.Fatalf("v19.3 is %vpx wide as one text node, %vpx split by a comment", one, split)
	}
	// A script appending text nodes one by one makes the same run.
	res := RenderDoc(`<html><body><div style="display:flex"><a id=x style="display:flex;column-gap:20px">v</a></div></body></html>`, "", Options{ViewportWidth: 800, DocumentRoot: true, ScriptDOM: true})
	x := res.ByID["x"].(*El)
	seg := newTextEl("v", x.engine)
	x.SetTextContent("")
	x.SetElementChildren([]qui.Widget{seg, newCommentEl(" ", x.engine), newTextEl("19.3", x.engine)})
	res.Engine.FlushPendingStyles()
	res.Root.Layout(qui.Rect{W: 800, H: 600})
	if w := x.Bounds().W; w != one {
		t.Fatalf("script-built run is %vpx, want %v", w, one)
	}
}

func TestInlineSVGSizing(t *testing.T) {
	SetWebCompat(true)
	defer SetWebCompat(false)
	src := `<html><body style="font-size:16px"><svg id=a width="1.5em" height="1.5em" viewBox="0 0 24 24"></svg>
<svg id=b width="100%" height="100%" viewBox="0 0 20 10" style="width:40px"></svg><svg id=c viewBox="0 0 30 10"></svg>
<svg id=d width="28" height="28" viewBox="0 0 32 32"></svg></body></html>`
	res := RenderDoc(src, "", Options{ViewportWidth: 800, DocumentRoot: true, ScriptDOM: true})
	res.Root.Layout(qui.Rect{W: 800, H: 600})
	for id, want := range map[string][2]float32{"a": {24, 24}, "b": {40, 20}, "c": {300, 100}, "d": {28, 28}} {
		if b := res.ByID[id].(*El).Bounds(); b.W != want[0] || b.H != want[1] {
			t.Errorf("svg %s: %vx%v, want %vx%v", id, b.W, b.H, want[0], want[1])
		}
	}
}
