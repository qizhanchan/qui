package svg

import (
	"encoding/xml"

	"github.com/srwiley/rasterx"
)

// Point is a 2D point in user (SVG viewBox) coordinates. Separate from
// qui.Point so the svg package can be used without importing qui types
// in its public surface — but the value layout is identical, so callers
// can free-convert if they want.
type Point struct{ X, Y float32 }

// ViewBox mirrors the SVG viewBox="x y w h" attribute. The package
// uses it both for the document's coordinate system and as a generic
// axis-aligned bounding box returned by Element.Bounds().
type ViewBox struct{ X, Y, W, H float32 }

// Element is the interface every renderable SVG element implements.
// Drawing, bounds, and serialization are all dispatched through it so
// renderer / serializer code can walk a heterogeneous tree without
// type switches.
//
// The interface is intentionally narrow — all the public state
// (geometry, style, transform) lives on the concrete types as plain
// fields so callers construct elements with struct literals and read
// them back with field access. The interface methods are unexported
// where possible to keep the SVG surface declarative.
type Element interface {
	// Bounds reports the element's axis-aligned bounding box in
	// user coordinates, ignoring its own transform. Group composes
	// child bounds via the standard min/max merge.
	Bounds() ViewBox

	// elementBase exposure — concrete types embed elementBase, so
	// any *T can return its style/transform/id through these.
	base() *elementBase

	// draw rasterises the element into d using accumulated matrix m,
	// inherited style s, and current opacity. Group recurses; leaf
	// shapes compile their geometry to rasterx paths.
	draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64)

	// encode emits the element as an SVG XML start element +
	// (optional) child content + end element through enc.
	encode(enc *xml.Encoder) error
}

// elementBase carries the fields every renderable element has in
// common. Concrete types embed it; users set its fields through normal
// struct-literal syntax (e.g. svg.Rect{ID: "frame", Style: ...}).
type elementBase struct {
	ID        string
	Style     Style
	Transform *Transform
}

func (b *elementBase) base() *elementBase { return b }

// SetID / SetStyle / SetTransform are tiny helpers so callers that
// build elements through helper functions (rather than struct literals)
// can still set base fields without poking into the embedded struct.
func (b *elementBase) SetID(id string)           { b.ID = id }
func (b *elementBase) SetStyle(s Style)          { b.Style = s }
func (b *elementBase) SetTransform(t *Transform) { b.Transform = t }

// Group is the SVG <g> element — a container for children that all
// share its transform and inherit its style. <svg> at the document
// root is implemented as a Group too.
type Group struct {
	elementBase
	Children []Element
}

// Add appends elements to the group and returns the receiver, so calls
// chain naturally: g := &svg.Group{}; g.Add(rect, circle).
func (g *Group) Add(els ...Element) *Group {
	g.Children = append(g.Children, els...)
	return g
}

// Bounds reports the union of children's transformed bounds. An empty
// Group returns the zero ViewBox.
func (g *Group) Bounds() ViewBox {
	if len(g.Children) == 0 {
		return ViewBox{}
	}
	first := true
	var minX, minY, maxX, maxY float32
	for _, c := range g.Children {
		b := transformedBounds(c.Bounds(), c.base().Transform)
		if first {
			minX, minY = b.X, b.Y
			maxX, maxY = b.X+b.W, b.Y+b.H
			first = false
			continue
		}
		if b.X < minX {
			minX = b.X
		}
		if b.Y < minY {
			minY = b.Y
		}
		if r := b.X + b.W; r > maxX {
			maxX = r
		}
		if y := b.Y + b.H; y > maxY {
			maxY = y
		}
	}
	return ViewBox{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// transformedBounds maps a child bounding box through its transform.
// Only the four corners are checked — a conservative upper bound is
// good enough for hit-test / clip purposes.
func transformedBounds(b ViewBox, t *Transform) ViewBox {
	if t == nil {
		return b
	}
	corners := [4][2]float32{
		{b.X, b.Y},
		{b.X + b.W, b.Y},
		{b.X, b.Y + b.H},
		{b.X + b.W, b.Y + b.H},
	}
	var minX, minY, maxX, maxY float32
	for i, c := range corners {
		x, y := t.apply(c[0], c[1])
		if i == 0 || x < minX {
			minX = x
		}
		if i == 0 || y < minY {
			minY = y
		}
		if i == 0 || x > maxX {
			maxX = x
		}
		if i == 0 || y > maxY {
			maxY = y
		}
	}
	return ViewBox{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

// toMatrix2D converts the svg Transform to rasterx's matrix shape.
// Same field layout, just float64.
func (t *Transform) toMatrix2D() rasterx.Matrix2D {
	if t == nil {
		return rasterx.Identity
	}
	return rasterx.Matrix2D{
		A: float64(t.A), B: float64(t.B),
		C: float64(t.C), D: float64(t.D),
		E: float64(t.E), F: float64(t.F),
	}
}

// applyElementMatrix composes m with the element's own transform.
// rasterx's Mult is in "a*b" order (apply b first); we want the SVG
// semantics where the parent matrix applies after the child's local
// transform, so we compute m.Mult(localM) — i.e. local applies first
// then parent.
func applyElementMatrix(m rasterx.Matrix2D, t *Transform) rasterx.Matrix2D {
	if t == nil {
		return m
	}
	return m.Mult(t.toMatrix2D())
}
