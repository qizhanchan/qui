package svg

import (
	"encoding/xml"

	"github.com/srwiley/rasterx"
)

// Rect is the <rect> element. RX / RY give corner radii; if RX is set
// and RY is zero, both default to RX (matching the SVG spec). W or H
// <= 0 makes the element a no-op renderer (Bounds still reports its
// declared rect so layout-style code can see it).
type Rect struct {
	elementBase
	X, Y, W, H float32
	RX, RY     float32
}

// Circle is the <circle> element. R<=0 renders nothing.
type Circle struct {
	elementBase
	CX, CY, R float32
}

// Ellipse is the <ellipse> element.
type Ellipse struct {
	elementBase
	CX, CY, RX, RY float32
}

// Line is the <line> element. It only has a stroke — fill is ignored.
type Line struct {
	elementBase
	X1, Y1, X2, Y2 float32
}

// Polyline is the <polyline> element — open chain of segments. Stroke
// only by default; fill applies just like SVG (everyone's first
// surprise) so we pass through whatever Style says.
type Polyline struct {
	elementBase
	Points []Point
}

// Polygon is the <polygon> element — closed chain.
type Polygon struct {
	elementBase
	Points []Point
}

// --- Rect ----------------------------------------------------------

func (r *Rect) Bounds() ViewBox { return ViewBox{X: r.X, Y: r.Y, W: r.W, H: r.H} }

func (r *Rect) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	rx, ry := normalizeRectRadii(r.RX, r.RY, r.W, r.H)
	add := func(p rasterx.Adder) {
		if rx > 0 && ry > 0 {
			// rasterx's AddRoundRect signature: minX, minY, maxX, maxY, rx, ry, rot, gapFunc, adder
			rasterx.AddRoundRect(
				float64(r.X), float64(r.Y),
				float64(r.X+r.W), float64(r.Y+r.H),
				float64(rx), float64(ry),
				0, rasterx.RoundGap, p,
			)
			return
		}
		rasterx.AddRect(
			float64(r.X), float64(r.Y),
			float64(r.X+r.W), float64(r.Y+r.H),
			0, p,
		)
	}
	drawShape(d, m, s, opacity, add)
}

// normalizeRectRadii applies SVG's rect-radius defaulting:
//   - rx<0 and ry<0 → 0
//   - exactly one set → both default to that value
//   - rx > W/2 → clamp to W/2; similar for ry > H/2
func normalizeRectRadii(rx, ry, w, h float32) (float32, float32) {
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	if rx == 0 && ry > 0 {
		rx = ry
	} else if ry == 0 && rx > 0 {
		ry = rx
	}
	if rx > w/2 {
		rx = w / 2
	}
	if ry > h/2 {
		ry = h / 2
	}
	return rx, ry
}

// --- Circle --------------------------------------------------------

func (c *Circle) Bounds() ViewBox {
	return ViewBox{X: c.CX - c.R, Y: c.CY - c.R, W: c.R * 2, H: c.R * 2}
}

func (c *Circle) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if c.R <= 0 {
		return
	}
	add := func(p rasterx.Adder) {
		rasterx.AddCircle(float64(c.CX), float64(c.CY), float64(c.R), p)
	}
	drawShape(d, m, s, opacity, add)
}

// --- Ellipse -------------------------------------------------------

func (e *Ellipse) Bounds() ViewBox {
	return ViewBox{X: e.CX - e.RX, Y: e.CY - e.RY, W: e.RX * 2, H: e.RY * 2}
}

func (e *Ellipse) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if e.RX <= 0 || e.RY <= 0 {
		return
	}
	add := func(p rasterx.Adder) {
		rasterx.AddEllipse(float64(e.CX), float64(e.CY), float64(e.RX), float64(e.RY), 0, p)
	}
	drawShape(d, m, s, opacity, add)
}

// --- Line ----------------------------------------------------------

func (l *Line) Bounds() ViewBox {
	x, y, w, h := normBounds(l.X1, l.Y1, l.X2, l.Y2)
	return ViewBox{X: x, Y: y, W: w, H: h}
}

func (l *Line) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	add := func(p rasterx.Adder) {
		p.Start(rasterx.ToFixedP(float64(l.X1), float64(l.Y1)))
		p.Line(rasterx.ToFixedP(float64(l.X2), float64(l.Y2)))
		p.Stop(false)
	}
	// SVG <line> has no fill effect (it's a 1-segment path that's not
	// closed). Force fill to None for the duration of this draw.
	s.Fill = Paint{Kind: PaintNone}
	drawShape(d, m, s, opacity, add)
}

func normBounds(x1, y1, x2, y2 float32) (x, y, w, h float32) {
	x = x1
	if x2 < x {
		x = x2
	}
	y = y1
	if y2 < y {
		y = y2
	}
	w = x1 - x2
	if w < 0 {
		w = -w
	}
	h = y1 - y2
	if h < 0 {
		h = -h
	}
	return
}

// --- Polyline / Polygon -------------------------------------------

func (p *Polyline) Bounds() ViewBox { return pointsBounds(p.Points) }
func (p *Polygon) Bounds() ViewBox  { return pointsBounds(p.Points) }

func pointsBounds(pts []Point) ViewBox {
	if len(pts) == 0 {
		return ViewBox{}
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := minX, minY
	for _, p := range pts[1:] {
		if p.X < minX {
			minX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	return ViewBox{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

func (p *Polyline) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if len(p.Points) < 2 {
		return
	}
	add := func(ad rasterx.Adder) {
		ad.Start(rasterx.ToFixedP(float64(p.Points[0].X), float64(p.Points[0].Y)))
		for _, pt := range p.Points[1:] {
			ad.Line(rasterx.ToFixedP(float64(pt.X), float64(pt.Y)))
		}
		ad.Stop(false)
	}
	drawShape(d, m, s, opacity, add)
}

func (p *Polygon) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if len(p.Points) < 2 {
		return
	}
	add := func(ad rasterx.Adder) {
		ad.Start(rasterx.ToFixedP(float64(p.Points[0].X), float64(p.Points[0].Y)))
		for _, pt := range p.Points[1:] {
			ad.Line(rasterx.ToFixedP(float64(pt.X), float64(pt.Y)))
		}
		ad.Stop(true)
	}
	drawShape(d, m, s, opacity, add)
}

// --- Group draw / encode ------------------------------------------

func (g *Group) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	cm := applyElementMatrix(m, g.Transform)
	cs := resolveStyle(s, g.Style)
	cop := opacity
	if g.Style.Opacity.Set {
		cop = opacity * float64(clampUnit(g.Style.Opacity.V))
	}
	for _, child := range g.Children {
		drawElement(child, d, cm, cs, cop)
	}
}

// drawElement is the single entry point used by Document.Rasterize and
// by Group.draw — it composes the child's transform with the current
// matrix, resolves its style, then dispatches to the concrete type's
// draw method (which handles fill/stroke).
func drawElement(e Element, d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, op float64) {
	base := e.base()
	cm := applyElementMatrix(m, base.Transform)
	cs := resolveStyle(s, base.Style)
	cop := op
	if base.Style.Opacity.Set {
		cop = op * float64(clampUnit(base.Style.Opacity.V))
	}
	e.draw(d, cm, cs, cop)
}

// --- Encoding ------------------------------------------------------

func (r *Rect) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "rect"}}
	emitBaseAttrs(&start, &r.elementBase)
	emitFloatAttr(&start, "x", r.X, true)
	emitFloatAttr(&start, "y", r.Y, true)
	emitFloatAttr(&start, "width", r.W, false)
	emitFloatAttr(&start, "height", r.H, false)
	if r.RX != 0 {
		emitFloatAttr(&start, "rx", r.RX, false)
	}
	if r.RY != 0 {
		emitFloatAttr(&start, "ry", r.RY, false)
	}
	return encodeEmpty(enc, start)
}

func (c *Circle) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "circle"}}
	emitBaseAttrs(&start, &c.elementBase)
	emitFloatAttr(&start, "cx", c.CX, true)
	emitFloatAttr(&start, "cy", c.CY, true)
	emitFloatAttr(&start, "r", c.R, false)
	return encodeEmpty(enc, start)
}

func (e *Ellipse) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "ellipse"}}
	emitBaseAttrs(&start, &e.elementBase)
	emitFloatAttr(&start, "cx", e.CX, true)
	emitFloatAttr(&start, "cy", e.CY, true)
	emitFloatAttr(&start, "rx", e.RX, false)
	emitFloatAttr(&start, "ry", e.RY, false)
	return encodeEmpty(enc, start)
}

func (l *Line) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "line"}}
	emitBaseAttrs(&start, &l.elementBase)
	emitFloatAttr(&start, "x1", l.X1, true)
	emitFloatAttr(&start, "y1", l.Y1, true)
	emitFloatAttr(&start, "x2", l.X2, true)
	emitFloatAttr(&start, "y2", l.Y2, true)
	return encodeEmpty(enc, start)
}

func (p *Polyline) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "polyline"}}
	emitBaseAttrs(&start, &p.elementBase)
	emitPointsAttr(&start, p.Points)
	return encodeEmpty(enc, start)
}

func (p *Polygon) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "polygon"}}
	emitBaseAttrs(&start, &p.elementBase)
	emitPointsAttr(&start, p.Points)
	return encodeEmpty(enc, start)
}

func (g *Group) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "g"}}
	emitBaseAttrs(&start, &g.elementBase)
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	for _, child := range g.Children {
		if err := child.encode(enc); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}

// encodeEmpty writes a self-closing-ish element through encoding/xml.
// encoding/xml does not emit "<foo/>" but rather "<foo></foo>", which
// is fine for SVG and avoids needing a custom token writer.
func encodeEmpty(enc *xml.Encoder, start xml.StartElement) error {
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	return enc.EncodeToken(start.End())
}
