package htmlcss

import (
	"math"
	"testing"

	"github.com/qizhanchan/qui"
)

func TestParseBoxShadow(t *testing.T) {
	cases := []struct {
		in                 string
		wantOK             bool
		x, y, blur, spread float32
		alpha              float32 // >0 → assert color alpha
	}{
		{"0 2px 8px rgba(0,0,0,0.2)", true, 0, 2, 8, 0, 0.2},
		{"1px 1px 0 #000", true, 1, 1, 0, 0, 1},
		{"2px 4px 12px 3px #333", true, 2, 4, 12, 3, 1},
		{"none", false, 0, 0, 0, 0, 0},
		{"inset 0 0 4px #000", false, 0, 0, 0, 0, 0},
		{"5px", false, 0, 0, 0, 0, 0}, // needs at least x+y
	}
	for _, c := range cases {
		sh, ok := parseBoxShadow(c.in, 16)
		if ok != c.wantOK {
			t.Errorf("parseBoxShadow(%q) ok=%v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if sh.X != c.x || sh.Y != c.y || sh.Blur != c.blur || sh.Spread != c.spread {
			t.Errorf("parseBoxShadow(%q) = x%v y%v blur%v spread%v, want %v %v %v %v",
				c.in, sh.X, sh.Y, sh.Blur, sh.Spread, c.x, c.y, c.blur, c.spread)
		}
		if c.alpha > 0 && absf(sh.Color.A-c.alpha) > 0.02 {
			t.Errorf("parseBoxShadow(%q) alpha = %v, want %v", c.in, sh.Color.A, c.alpha)
		}
	}
}

func TestParseOpacity(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float32
		ok   bool
	}{
		{"0.5", 0.5, true}, {"1", 1, true}, {"0", 0, true},
		{"1.5", 1, true}, {"-1", 0, true}, {"abc", 0, false},
	} {
		v, ok := parseOpacity(c.in)
		if ok != c.ok || (ok && v != c.want) {
			t.Errorf("parseOpacity(%q) = (%v,%v), want (%v,%v)", c.in, v, ok, c.want, c.ok)
		}
	}
}

func TestParseTransform(t *testing.T) {
	tf, ok := parseTransform("translate(10px, -4px) rotate(90deg) scale(2)", 16)
	if !ok {
		t.Fatal("parseTransform returned ok=false")
	}
	if tf.tx != 10 || tf.ty != -4 {
		t.Errorf("translate = (%v,%v), want (10,-4)", tf.tx, tf.ty)
	}
	if absf(tf.rotate-float32(math.Pi/2)) > 1e-4 {
		t.Errorf("rotate = %v, want pi/2", tf.rotate)
	}
	if tf.sx != 2 || tf.sy != 2 {
		t.Errorf("scale = (%v,%v), want (2,2)", tf.sx, tf.sy)
	}

	// scaleX/scaleY + translateY only.
	tf2, _ := parseTransform("scaleX(1.5) translateY(8px)", 16)
	if tf2.sx != 1.5 || tf2.sy != 1 || tf2.ty != 8 || tf2.tx != 0 {
		t.Errorf("partial transform = %+v", *tf2)
	}

	if _, ok := parseTransform("none", 16); ok {
		t.Error("transform:none should be ok=false")
	}
}

func TestParseLinearGradientAndShader(t *testing.T) {
	g, ok := parseLinearGradient("linear-gradient(90deg, red, blue)")
	if !ok {
		t.Fatal("parseLinearGradient ok=false")
	}
	if g.angleDeg != 90 {
		t.Errorf("angle = %v, want 90", g.angleDeg)
	}
	if len(g.stops) != 2 || g.stops[0].Offset != 0 || g.stops[1].Offset != 1 {
		t.Errorf("stops = %+v, want 2 evenly-spread", g.stops)
	}

	// "to right" == 90deg; explicit % offsets.
	g2, _ := parseLinearGradient("linear-gradient(to right, #fff 0%, #000 100%)")
	if g2.angleDeg != 90 {
		t.Errorf("to right angle = %v, want 90", g2.angleDeg)
	}

	// Default direction (to bottom = 180deg) when omitted.
	g3, _ := parseLinearGradient("linear-gradient(red, blue)")
	if g3.angleDeg != 180 {
		t.Errorf("default angle = %v, want 180", g3.angleDeg)
	}

	// shaderFor: 180deg (down) over a 100x50 box → vertical line through center.
	sh := (&gradientSpec{angleDeg: 180, stops: g3.stops}).shaderFor(qui.Rect{X: 0, Y: 0, W: 100, H: 50})
	lg, ok := sh.(qui.LinearGradient)
	if !ok {
		t.Fatalf("shaderFor returned %T, want LinearGradient", sh)
	}
	if lg.Start.X != 50 || lg.Start.Y != 0 || lg.End.X != 50 || lg.End.Y != 50 {
		t.Errorf("gradient line = %v→%v, want (50,0)→(50,50)", lg.Start, lg.End)
	}

	if _, ok := parseLinearGradient("radial-gradient(red, blue)"); ok {
		t.Error("radial-gradient should not parse as linear")
	}
}

// --- build integration -------------------------------------------------

func TestBuildBoxShadow(t *testing.T) {
	res := RenderDoc(
		`<body><div id="c">card</div></body>`,
		`#c { width: 120px; height: 40px; box-shadow: 0 3px 10px rgba(0,0,0,0.25) }`,
		Options{},
	)
	el, ok := res.ByID["c"].(*El)
	if !ok {
		t.Fatalf("#c is %T, want *htmlcss.El", res.ByID["c"])
	}
	box := &el.Box
	if box.Style().Shadow.IsZero() {
		t.Fatal("box-shadow not applied to Style.Shadow")
	}
	// Paint records a DrawShadow.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 120, H: 40})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
	if len(rec.Shadows) == 0 {
		t.Error("Draw did not emit a DrawShadow for box-shadow")
	} else if s := rec.Shadows[0]; absf(s.Spec.Y-3) > 0.5 || absf(s.Spec.Blur-10) > 0.5 {
		t.Errorf("shadow spec = %+v, want Y≈3 Blur≈10", s.Spec)
	}
}

func TestBuildGradientBackground(t *testing.T) {
	res := RenderDoc(
		`<body><div id="g">grad</div></body>`,
		`#g { width: 100px; height: 40px; background: linear-gradient(90deg, #ff0000, #0000ff) }`,
		Options{},
	)
	box := &res.ByID["g"].(*El).Box
	if box.BackgroundShader == nil {
		t.Fatal("gradient background not wired to Box.BackgroundShader")
	}
	sh := box.BackgroundShader(qui.Rect{X: 0, Y: 0, W: 100, H: 40})
	lg, ok := sh.(qui.LinearGradient)
	if !ok || len(lg.Stops) != 2 {
		t.Fatalf("BackgroundShader = %T (%d stops), want LinearGradient with 2", sh, len(lg.Stops))
	}
	// No solid background color set (gradient takes over).
	if box.Style().Background.A > 0 {
		t.Errorf("solid background should be unset when gradient present, got %+v", box.Style().Background)
	}
	// Paint records a rounded/flat fill for the gradient.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 100, H: 40})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
	if len(rec.Fills) == 0 && len(rec.Rounds) == 0 {
		t.Error("gradient Draw emitted no fill")
	}
}

func TestBuildOpacity(t *testing.T) {
	res := RenderDoc(
		`<body><div id="o">x</div></body>`,
		`#o { width: 50px; height: 20px; opacity: 0.5 }`,
		Options{},
	)
	box := &res.ByID["o"].(*El).Box
	if box.Style().Opacity != 0.5 {
		t.Errorf("opacity = %v, want 0.5", box.Style().Opacity)
	}
	// Draw must not panic with a SaveLayer wrap.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 50, H: 20})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
}

func TestBuildTransform(t *testing.T) {
	res := RenderDoc(
		`<body><div id="t">x</div></body>`,
		`#t { width: 50px; height: 20px; transform: rotate(45deg) scale(1.2) }`,
		Options{},
	)
	box := &res.ByID["t"].(*El).Box
	if box.Transform == nil {
		t.Fatal("transform not wired to Box.Transform")
	}
	if absf(box.Transform.Rotate-float32(math.Pi/4)) > 1e-4 {
		t.Errorf("rotate = %v, want pi/4", box.Transform.Rotate)
	}
	if box.Transform.SX != 1.2 || box.Transform.SY != 1.2 {
		t.Errorf("scale = (%v,%v), want 1.2", box.Transform.SX, box.Transform.SY)
	}
	// Draw applies matrix ops without panic.
	box.Layout(qui.Rect{X: 0, Y: 0, W: 50, H: 20})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
}

func TestCSSTransformMovesAXAndActionTarget(t *testing.T) {
	res := RenderDoc(
		`<body><button id="t">Move</button></body>`,
		`#t { width: 50px; height: 20px; transform: translateX(100px) }`,
		Options{},
	)
	button := res.ByID["t"].(*El)
	clicked := false
	button.SetOnClick(func() { clicked = true })
	win := qui.NewTestWindow(qui.Size{W: 300, H: 100})
	win.SetRoot(res.Root)
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})

	nodes, err := win.FindNodes("#t")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("FindNodes(#t) = %v, %v", nodes, err)
	}
	wantX := button.Bounds().X + 100
	if nodes[0].Bounds.X != wantX {
		t.Fatalf("AX x = %v, want transformed x %v", nodes[0].Bounds.X, wantX)
	}
	if err := win.Click("#t", qui.ClickOptions{}); err != nil {
		t.Fatalf("Click(#t): %v", err)
	}
	if !clicked {
		t.Fatal("selector action did not reach transformed button")
	}
}
