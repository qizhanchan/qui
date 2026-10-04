package svg

import (
	"bytes"
	"image"
	"testing"

	"github.com/qizhanchan/qui"
)

const sampleDoc = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" width="100" height="100">
  <g transform="translate(10,10)">
    <rect x="0" y="0" width="80" height="80" fill="#ff0000"/>
    <circle cx="40" cy="40" r="20" fill="none" stroke="blue" stroke-width="4"/>
  </g>
</svg>`

func TestParseElementTree(t *testing.T) {
	doc, err := ParseBytes([]byte(sampleDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.ViewBox != (ViewBox{W: 100, H: 100}) {
		t.Errorf("viewBox = %v, want 0,0,100,100", doc.ViewBox)
	}
	if doc.Width != 100 || doc.Height != 100 {
		t.Errorf("size = %vx%v, want 100x100", doc.Width, doc.Height)
	}
	if len(doc.Root.Children) != 1 {
		t.Fatalf("want 1 root child, got %d", len(doc.Root.Children))
	}
	g, ok := doc.Root.Children[0].(*Group)
	if !ok {
		t.Fatalf("want *Group, got %T", doc.Root.Children[0])
	}
	if g.Transform == nil || g.Transform.E != 10 || g.Transform.F != 10 {
		t.Errorf("group transform = %+v, want translate(10,10)", g.Transform)
	}
	if len(g.Children) != 2 {
		t.Fatalf("want 2 group children, got %d", len(g.Children))
	}
	rect, ok := g.Children[0].(*Rect)
	if !ok {
		t.Fatalf("want *Rect, got %T", g.Children[0])
	}
	if rect.W != 80 || rect.H != 80 {
		t.Errorf("rect size = %vx%v", rect.W, rect.H)
	}
	if rect.Style.Fill.Kind != PaintSolid || rect.Style.Fill.Color.R < 0.99 {
		t.Errorf("rect fill = %+v, want red", rect.Style.Fill)
	}
	circ, ok := g.Children[1].(*Circle)
	if !ok {
		t.Fatalf("want *Circle, got %T", g.Children[1])
	}
	if circ.Style.Fill.Kind != PaintNone {
		t.Errorf("circle fill should be none, got %+v", circ.Style.Fill)
	}
	if circ.Style.Stroke.Kind != PaintSolid {
		t.Errorf("circle stroke = %+v, want solid blue", circ.Style.Stroke)
	}
	if !circ.Style.StrokeWidth.Set || circ.Style.StrokeWidth.V != 4 {
		t.Errorf("circle stroke-width = %+v, want 4", circ.Style.StrokeWidth)
	}
}

func TestProgrammaticDocument(t *testing.T) {
	doc := NewDocument(64, 64)
	red := qui.Color{R: 1, A: 1}
	r := &Rect{X: 8, Y: 8, W: 48, H: 48}
	r.Style.Fill = SolidPaint(red)
	doc.Add(r)

	img := doc.Rasterize(64, 64, qui.Color{})
	if img == nil {
		t.Fatal("nil image")
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 64, 64) {
		t.Fatalf("bounds = %v", got)
	}
	// Pixel near center should be red, premultiplied alpha.
	rgba := img.(*image.RGBA)
	r0, g0, b0, a0 := rgba.RGBAAt(32, 32).RGBA()
	if a0 == 0 {
		t.Fatalf("center pixel transparent")
	}
	if r0 < 0xfe00 || g0 > 0x0200 || b0 > 0x0200 {
		t.Errorf("center not red: r=%x g=%x b=%x a=%x", r0, g0, b0, a0)
	}
}

func TestPathBuilderRasterizes(t *testing.T) {
	doc := NewDocument(20, 20)
	p := NewPath().MoveTo(2, 2).LineTo(18, 2).LineTo(18, 18).LineTo(2, 18).Close()
	p.Style.Fill = SolidPaint(qui.Color{G: 1, A: 1})
	doc.Add(p)
	img := doc.Rasterize(20, 20, qui.Color{}).(*image.RGBA)
	_, g, _, a := img.RGBAAt(10, 10).RGBA()
	if a == 0 {
		t.Fatal("path interior should be filled")
	}
	if g < 0xfe00 {
		t.Errorf("path interior not green: g=%x a=%x", g, a)
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	// Build a document, serialize it, parse the result, and compare
	// the resulting trees. This is the contract that lets callers
	// edit a parsed SVG and write it back out.
	doc := NewDocument(50, 50)
	doc.Add(&Rect{X: 5, Y: 5, W: 40, H: 40,
		elementBase: elementBase{
			Style: Style{
				Fill:        SolidPaint(qui.Color{B: 1, A: 1}),
				StrokeWidth: SetLength(2),
				Stroke:      SolidPaint(qui.Color{R: 1, A: 1}),
			},
		},
	})

	var buf bytes.Buffer
	if err := doc.WriteSVG(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}

	doc2, err := ParseBytes(buf.Bytes())
	if err != nil {
		t.Fatalf("re-parse: %v\nXML was:\n%s", err, buf.String())
	}
	if doc2.ViewBox != doc.ViewBox {
		t.Errorf("viewBox: got %v, want %v", doc2.ViewBox, doc.ViewBox)
	}
	if len(doc2.Root.Children) != 1 {
		t.Fatalf("re-parsed children = %d, want 1", len(doc2.Root.Children))
	}
	r2, ok := doc2.Root.Children[0].(*Rect)
	if !ok {
		t.Fatalf("want *Rect, got %T", doc2.Root.Children[0])
	}
	if r2.X != 5 || r2.Y != 5 || r2.W != 40 || r2.H != 40 {
		t.Errorf("rect geometry = %+v", r2)
	}
	if r2.Style.Fill.Kind != PaintSolid || r2.Style.Fill.Color.B < 0.99 {
		t.Errorf("fill round-trip: %+v", r2.Style.Fill)
	}
	if r2.Style.Stroke.Kind != PaintSolid || r2.Style.Stroke.Color.R < 0.99 {
		t.Errorf("stroke round-trip: %+v", r2.Style.Stroke)
	}
	if !r2.Style.StrokeWidth.Set || r2.Style.StrokeWidth.V != 2 {
		t.Errorf("stroke-width round-trip: %+v", r2.Style.StrokeWidth)
	}
}

func TestTransformParseTranslate(t *testing.T) {
	t1, err := parseTransformAttr("translate(10, 20)")
	if err != nil {
		t.Fatal(err)
	}
	if t1 == nil || t1.E != 10 || t1.F != 20 || t1.A != 1 || t1.D != 1 {
		t.Errorf("translate parsed wrong: %+v", t1)
	}
}

func TestTransformComposition(t *testing.T) {
	// "translate(10,0) rotate(90)" — apply translate first then
	// rotate. A point (1,0) should land at... translate(10,0)(1,0)
	// = (11,0); rotate(90)(11,0) = (0,11).
	t1, err := parseTransformAttr("translate(10,0) rotate(90)")
	if err != nil {
		t.Fatal(err)
	}
	x, y := t1.apply(1, 0)
	if !approx(x, 0) || !approx(y, 11) {
		t.Errorf("composed transform output = (%v, %v), want (0, 11)", x, y)
	}
}

func TestRasterizePathWithArc(t *testing.T) {
	// Regression: AddArc indexes points[5] and points[6], so the points
	// slice must be length 7 (rx ry rotDeg large sweep endX endY) and
	// cx/cy must be the *ellipse center*, not the endpoint. Earlier
	// implementation panicked with "index out of range [6] with length
	// 5" on any A command in a Font Awesome path.
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
		<path d="M 50 10 A 40 40 0 1 1 50 90 L 50 10 Z" fill="black"/>
	</svg>`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	img := doc.Rasterize(64, 64, qui.Color{R: 1, A: 1}).(*image.RGBA)
	covered := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			covered++
		}
	}
	if covered < 100 {
		t.Fatalf("arc path rendered only %d pixels (expected > 100)", covered)
	}
}

func TestParseSkipsUnknownElements(t *testing.T) {
	// Unknown <text> + <defs> should be silently consumed without
	// breaking later siblings — q-excel uses real Font Awesome SVGs
	// which sometimes embed <!-- license --> comments and <defs>.
	src := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10">
		<defs><linearGradient id="g"/></defs>
		<rect x="0" y="0" width="10" height="10" fill="black"/>
		<text>ignored</text>
	</svg>`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Root.Children) != 1 {
		t.Fatalf("want only rect, got %d children", len(doc.Root.Children))
	}
	if _, ok := doc.Root.Children[0].(*Rect); !ok {
		t.Errorf("expected *Rect after defs+text were skipped, got %T", doc.Root.Children[0])
	}
}
