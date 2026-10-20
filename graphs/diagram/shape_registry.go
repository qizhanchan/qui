package diagram

import "github.com/qizhanchan/qui"

// ShapePaint is the resolved paint passed to a registered shape.
type ShapePaint struct {
	Fill        qui.Color
	Stroke      qui.Color
	StrokeWidth float32
	Radius      float32
	ShapeSize   float32
	Dashed      bool
	Opacity     float32
}

// ShapeDefinition describes a custom vertex silhouette. Draw is required;
// Contains and Perimeter may be nil, in which case the rectangle bounds are
// used. Perimeter should return an intersection point on the silhouette
// between the rect center and toward.
type ShapeDefinition struct {
	Draw      func(canvas qui.Canvas, rect Rect, paint ShapePaint)
	Contains  func(rect Rect, p Point) bool
	Perimeter func(rect Rect, toward Point) Point
}

// ShapeRegistry maps built-in and application-defined shape names to their
// geometry/painting behavior. A registry is intentionally not global: each
// diagram/export can use an isolated catalog.
type ShapeRegistry struct {
	defs map[ShapeKind]ShapeDefinition
}

// NewShapeRegistry returns a registry containing the 12 built-in shapes.
func NewShapeRegistry() *ShapeRegistry {
	r := &ShapeRegistry{defs: map[ShapeKind]ShapeDefinition{}}
	for _, kind := range []ShapeKind{
		ShapeRect, ShapeRoundedRect, ShapeEllipse, ShapeDiamond, ShapeParallelogram,
		ShapeHexagon, ShapeCylinder, ShapeDocument, ShapeActor, ShapeCloud, ShapeNote, ShapeText,
	} {
		k := kind
		r.defs[k] = ShapeDefinition{
			Draw: func(canvas qui.Canvas, rect Rect, paint ShapePaint) {
				drawVertexShape(canvas, k, rect, paint.Fill, paint.Stroke, paint.StrokeWidth, paint.Radius, paint.ShapeSize, paint.Dashed, paint.Opacity)
			},
			Contains:  func(rect Rect, p Point) bool { return shapeContains(k, rect, p) },
			Perimeter: func(rect Rect, toward Point) Point { return perimeterPoint(k, rect, toward) },
		}
	}
	return r
}

// DefaultShapeRegistry returns a fresh built-in registry.
func DefaultShapeRegistry() *ShapeRegistry { return NewShapeRegistry() }

// Register installs or replaces a shape definition.
func (r *ShapeRegistry) Register(kind ShapeKind, def ShapeDefinition) {
	if r == nil || kind == "" || def.Draw == nil {
		return
	}
	if r.defs == nil {
		r.defs = map[ShapeKind]ShapeDefinition{}
	}
	r.defs[kind] = def
}

// Lookup returns the shape definition for kind.
func (r *ShapeRegistry) Lookup(kind ShapeKind) (ShapeDefinition, bool) {
	if r == nil {
		return ShapeDefinition{}, false
	}
	def, ok := r.defs[kind]
	return def, ok
}

// Draw paints a registered shape. Unknown shapes fall back to a rectangle.
func (r *ShapeRegistry) Draw(canvas qui.Canvas, kind ShapeKind, rect Rect, paint ShapePaint) {
	if def, ok := r.Lookup(kind); ok && def.Draw != nil {
		def.Draw(canvas, rect, paint)
		return
	}
	drawVertexShape(canvas, ShapeRect, rect, paint.Fill, paint.Stroke, paint.StrokeWidth, paint.Radius, paint.ShapeSize, paint.Dashed, paint.Opacity)
}

func (r *ShapeRegistry) contains(kind ShapeKind, rect Rect, p Point) bool {
	if def, ok := r.Lookup(kind); ok && def.Contains != nil {
		return def.Contains(rect, p)
	}
	if def, ok := r.Lookup(kind); ok && def.Draw != nil {
		return rect.Contains(p)
	}
	return shapeContains(ShapeRect, rect, p)
}

func (r *ShapeRegistry) perimeter(kind ShapeKind, rect Rect, toward Point) Point {
	if def, ok := r.Lookup(kind); ok && def.Perimeter != nil {
		return def.Perimeter(rect, toward)
	}
	return perimeterPoint(ShapeRect, rect, toward)
}
