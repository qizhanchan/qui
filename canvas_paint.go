package qui

// Paint bundles the visual properties of a draw operation — color,
// fill vs stroke, stroke width, stroke caps / joins, fill rule, and
// anti-aliasing. Every shape draws with a Paint; the same Paint applies
// across rects, rounded rects, lines, polylines, and paths so a new
// property gets added in ONE place instead of multiplying across every
// primitive's signature.
//
// Skia's SkPaint is the model. The default zero-value Paint fills with
// transparent-black; callers always set Color explicitly. Anti-alias
// defaults TRUE because it's what every modern renderer expects;
// disable for pixel-perfect tests (snapshot diffs) or chunky retro looks.
type Paint struct {
	// Color is the source color the rasterizer composites against the
	// framebuffer. Pre-multiplied semantics live at the
	// fillRectBlend / drawCornerAA layer — Paint stores straight RGBA
	// so callers (`Paint{Color: theme.Surface}`) read naturally.
	Color Color

	// Style picks fill vs stroke. PaintFill is the zero value, so
	// `Paint{Color: x}` is a fill paint — the most common case is
	// `c.DrawShape(ShapeRect(r), Paint{Color: bg})`.
	Style PaintStyle

	// StrokeWidth is in LOGICAL pixels. The canvas's state-stack scale
	// (HiDPI / user Scale ops) is applied at rasterization time, same
	// as it is for rect coordinates.  Ignored when Style == PaintFill.
	StrokeWidth float32

	// Cap selects the shape painted at the start and end of an open
	// stroked subpath (lines, polylines, open paths). CapRound is the
	// zero value — it matches the existing capsule rasterizer behavior.
	// Skia's SkPaint::Cap.
	Cap StrokeCap

	// Join selects the shape painted where two stroked segments meet
	// inside a polyline / path. JoinMiter is the zero value to align
	// with Skia and CSS canvas. Joins are only relevant when StrokeWidth
	// is wide enough to make the meeting visible — at width 1 they all
	// look the same.
	Join StrokeJoin

	// MiterLimit caps the length of a miter spike at a sharp acute
	// angle: if the spike would exceed MiterLimit * StrokeWidth, the
	// join falls back to bevel. Default 10 matches Skia and SVG.
	// Ignored when Join != JoinMiter.
	MiterLimit float32

	// FillRule decides which side of a self-intersecting path counts as
	// "inside". FillNonZero (the default) follows SVG / Postscript /
	// Skia; FillEvenOdd matches the older "alternating" rule and is
	// what the pre-Phase-E rasterizer used.
	FillRule FillRule

	// AntiAlias enables edge AA for path fills and rounded-rect
	// rasterizers. True by default. Set to false for chunky pixel-art
	// looks or to make snapshot tests deterministic.
	AntiAlias bool

	// BlendMode selects the Porter-Duff operator used to composite the
	// source pixel against the destination. Only consumed by SaveLayer's
	// composite pass — regular DrawShape paths use SrcOver internally and
	// let Color.A drive the alpha blend. The zero value is BlendSrcOver,
	// matching Skia's default. See canvas_layer.go for the operator set.
	BlendMode BlendMode

	// Alpha is an additional opacity multiplier applied when a layer is
	// composited back onto its parent. Independent of Color.A so callers
	// can fade an entire subtree without touching per-shape colors.
	// Range 0..1; zero = fully transparent, one (or unset — see
	// effectiveAlpha) = fully opaque. Only consumed by SaveLayer.
	Alpha float32

	// Shader, when non-nil, replaces Paint.Color as the per-pixel color
	// source for fills. The rasterizer samples Shader.ColorAt(x, y) at
	// each pixel's center in LOGICAL coordinates (the same space rects
	// and paths are quoted in), then applies coverage as usual. Nil
	// Shader keeps the solid-color fast path.
	//
	// Only fills consume Shader — strokes always use Paint.Color. Fill
	// primitives (rect / rounded-rect / path) route through the path
	// rasterizer when Shader is set so gradient handling lives in one
	// place. See shader.go for LinearGradient / RadialGradient.
	Shader Shader

	// ColorFilter, when non-nil on a SaveLayer paint, transforms every
	// source pixel through ApplyColor before Porter-Duff blending.
	// Consumed by imageCanvas.restoreLayers — not by DrawShape. See
	// filter.go for MatrixColorFilter and helpers.
	ColorFilter ColorFilter

	// ImageFilter, when non-nil on a SaveLayer paint, replaces the
	// layer's offscreen buffer with the filter's output at composite
	// time. Enables blur, drop shadow, and other multi-pixel effects
	// that need the whole layer as input. See filter.go for
	// DropShadowImageFilter.
	ImageFilter ImageFilter
}

// effectiveAlpha returns the composite opacity multiplier. Uses 1.0 as
// the default so callers who leave Paint.Alpha unset get pass-through
// (a fully opaque composite, matching Skia's SkPaint::getAlphaf default).
// Zero is only meaningful as an explicit "hide this layer" request.
func (p Paint) effectiveAlpha() float32 {
	if p.Alpha == 0 {
		return 1
	}
	if p.Alpha < 0 {
		return 0
	}
	if p.Alpha > 1 {
		return 1
	}
	return p.Alpha
}

// DefaultPaint returns a Paint with AA on, miter join, miter limit 10 —
// the conventional Skia defaults. Most callers use the struct-literal
// form and rely on the zero value; this helper is for code that needs a
// starting Paint to mutate.
func DefaultPaint(color Color) Paint {
	return Paint{
		Color:      color,
		MiterLimit: 10,
		AntiAlias:  true,
	}
}

// effectiveAA returns whether anti-aliasing should be applied. The
// zero value of `AntiAlias bool` is false, but we want AA by default
// because that's what callers expect. To get that, the rasterizer
// checks `!paint.AAExplicitDisable()`: if AntiAlias is true OR the
// caller didn't touch the field (default), AA is on.
//
// We achieve this by treating false-zero as "default = on" and adding
// an explicit DisableAA struct option pattern — but to keep the field
// trivially settable, we instead invert: most rasterizers should call
// `aaOn := paint.AntiAlias || !paint.aaExplicitlySet` … too clever.
// Pragmatic answer: rasterizer reads paint.AntiAlias and treats
// zero-value Paint{} as having AA off, which matches non-AA
// drawLineInto's existing behavior anyway. Callers who want AA on a
// non-line primitive opt in via Paint{AntiAlias: true} or by going
// through the convenience helpers that set DefaultPaint.
func (p Paint) effectiveAA() bool { return p.AntiAlias }

// effectiveMiterLimit returns MiterLimit, defaulting to 10 when zero
// (callers using struct literals frequently leave it unset).
func (p Paint) effectiveMiterLimit() float32 {
	if p.MiterLimit <= 0 {
		return 10
	}
	return p.MiterLimit
}

// PaintStyle selects between fill and stroke. Future-proofed as an int
// so adding PaintFillAndStroke (Skia has it for paths) doesn't break
// the enumeration.
type PaintStyle int8

const (
	PaintFill PaintStyle = iota
	PaintStroke
)

// StrokeCap selects the cap shape at the start and end of an open
// stroked subpath. CapRound is the zero value because the existing
// capsule rasterizer already paints rounded caps — switching to
// CapButt / CapSquare means stamping a flat / extended rectangle
// instead of the round cap.
type StrokeCap int8

const (
	CapRound  StrokeCap = iota // semicircular cap centered at the endpoint
	CapButt                    // no cap — stroke ends exactly at the endpoint
	CapSquare                  // square cap extending halfWidth past the endpoint
)

// StrokeJoin selects the shape painted where two stroked segments
// meet inside a polyline / path. JoinMiter is the SVG / Canvas / Skia
// default; the polyline rasterizer falls back to bevel if a miter
// would exceed MiterLimit * StrokeWidth.
type StrokeJoin int8

const (
	JoinMiter StrokeJoin = iota // pointed corner (subject to MiterLimit)
	JoinRound                   // arc segment
	JoinBevel                   // straight edge between the outer corners
)

// FillRule picks the inside-determination algorithm for self-
// intersecting paths. SVG / Postscript / Skia default to non-zero;
// even-odd is what the pre-Phase-E rasterizer used.
type FillRule int8

const (
	FillNonZero FillRule = iota // signed winding count ≠ 0 → inside
	FillEvenOdd                 // odd number of crossings → inside
)

// Shape is the geometric input to Canvas.DrawShape. Every concrete
// drawable primitive (rect, rounded rect, line, polyline, future
// path) implements this single-method interface — the sealing avoids
// arbitrary types accidentally satisfying it while keeping the type-
// switch inside DrawShape exhaustive.
type Shape interface {
	isShape()
}

// ShapeRect is a non-rounded axis-aligned rectangle.
type ShapeRect Rect

func (ShapeRect) isShape() {}

// ShapeRRect is a rectangle with uniform corner radius. Radius == 0
// degenerates to a plain rect; Radius >= min(W,H)/2 produces a
// pill / circle (the scaleRoundedShape pill-detection path).
type ShapeRRect struct {
	Rect   Rect
	Radius float32
}

func (ShapeRRect) isShape() {}

// ShapeLine is a single segment between two points. Cap shape comes
// from the Paint. AA at the rim. Stroke width comes from the Paint.
type ShapeLine struct {
	P1, P2 Point
}

func (ShapeLine) isShape() {}

// ShapePolyline is a sequence of points joined by line segments.
// Joins between segments are painted according to Paint.Join.
// Fewer than two points is a no-op.
type ShapePolyline []Point

func (ShapePolyline) isShape() {}
