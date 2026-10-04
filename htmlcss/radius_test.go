package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// TestParseBorderRadius covers the 1–4 value shorthand mapping, the
// per-corner longhands, elliptical `a / b` (horizontal kept), and the
// all-equal collapse back to the uniform Radius fast path.
func TestParseBorderRadius(t *testing.T) {
	cases := []struct {
		name    string
		decls   map[string]string
		uniform float32
		corners qui.CornerRadii // zero == expect uniform fast path
	}{
		{"single uniform", map[string]string{"border-radius": "8px"}, 8, qui.CornerRadii{}},
		{"two values", map[string]string{"border-radius": "10px 4px"}, 0, qui.CornerRadii{TL: 10, TR: 4, BR: 10, BL: 4}},
		{"three values", map[string]string{"border-radius": "10px 4px 2px"}, 0, qui.CornerRadii{TL: 10, TR: 4, BR: 2, BL: 4}},
		{"four values", map[string]string{"border-radius": "1px 2px 3px 4px"}, 0, qui.CornerRadii{TL: 1, TR: 2, BR: 3, BL: 4}},
		{"all equal collapses", map[string]string{"border-radius": "5px 5px 5px 5px"}, 5, qui.CornerRadii{}},
		{"elliptical keeps horizontal", map[string]string{"border-radius": "10px 20px / 5px 8px"}, 0, qui.CornerRadii{TL: 10, TR: 20, BR: 10, BL: 20}},
		{"longhand only", map[string]string{"border-top-left-radius": "12px"}, 0, qui.CornerRadii{TL: 12}},
		{"longhand overrides shorthand corner", map[string]string{"border-radius": "6px", "border-bottom-right-radius": "0"}, 6, qui.CornerRadii{TL: 6, TR: 6, BR: 0, BL: 6}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := &ComputedStyle{}
			parseBorderRadius(c.decls, cs, 16)
			if cs.Radius != c.uniform {
				t.Errorf("Radius = %v, want %v", cs.Radius, c.uniform)
			}
			if cs.Corners != c.corners {
				t.Errorf("Corners = %+v, want %+v", cs.Corners, c.corners)
			}
		})
	}
}

// TestBuildPerCornerRadius verifies per-corner radii reach the backing Box
// Style and route painting through the path rasterizer (ShapePath) rather
// than the uniform FillRoundedRect fast path.
func TestBuildPerCornerRadius(t *testing.T) {
	res := RenderDoc(
		`<body><div id="c">card</div></body>`,
		`#c { width: 120px; height: 60px; background: #ccc; border-radius: 16px 16px 0 0 }`,
		Options{},
	)
	box := &res.ByID["c"].(*El).Box
	got := box.Style().Corners
	want := qui.CornerRadii{TL: 16, TR: 16, BR: 0, BL: 0}
	if got != want {
		t.Fatalf("Style.Corners = %+v, want %+v", got, want)
	}
	box.Layout(qui.Rect{X: 0, Y: 0, W: 120, H: 60})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
	if len(rec.Paths) == 0 {
		t.Fatal("per-corner background did not paint a ShapePath")
	}
	if len(rec.Rounds) != 0 {
		t.Errorf("per-corner box should not use the uniform FillRoundedRect path, got %d", len(rec.Rounds))
	}
	if b := rec.Paths[0]; absf(b.W-120) > 1 || absf(b.H-60) > 1 {
		t.Errorf("path bounds = %vx%v, want ~120x60", b.W, b.H)
	}
}

// TestPerCornerBorderStroke checks a solid border on a per-corner box strokes
// a path (not the uniform StrokeRoundedRect).
func TestPerCornerBorderStroke(t *testing.T) {
	res := RenderDoc(
		`<body><div id="c">x</div></body>`,
		`#c { width: 80px; height: 40px; border: 2px solid #333; border-radius: 12px 0 12px 0 }`,
		Options{},
	)
	box := &res.ByID["c"].(*El).Box
	box.Layout(qui.Rect{X: 0, Y: 0, W: 80, H: 40})
	var rec qui.RecordingCanvas
	box.Draw(&rec)
	stroked := false
	for _, p := range rec.Paths {
		if p.Stroke {
			stroked = true
			if absf(p.StrokeWidth-2) > 0.5 {
				t.Errorf("border stroke width = %v, want 2", p.StrokeWidth)
			}
		}
	}
	if !stroked {
		t.Error("per-corner border did not stroke a ShapePath")
	}
}
