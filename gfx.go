package qui

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
)

// The graphics vocabulary — geometry, color, fonts as style values, paint,
// paths, shaders, filters and the Canvas contract — lives in internal/gfx so
// the text engine and the rasterizers can share it without importing the
// engine. The aliases and wrappers below are its public face: qui.Rect and
// gfx.Rect are the same type.

type (
	// BlendMode selects the Porter-Duff / blend operator used to composite
	// a source pixel against a destination pixel. Consumed exclusively by
	// SaveLayer's composite pass — the regular DrawShape rasterizers use a
	// hard-coded SrcOver internally (via fillRectBlend / blendPixel).
	BlendMode = gfx.BlendMode

	// BlurImageFilter blurs a whole layer with a separable box blur run three
	// times (a close Gaussian approximation). Radius is the box-blur radius in
	// physical pixels; 0 is a no-op. Used for CSS filter: blur().
	BlurImageFilter = gfx.BlurImageFilter

	// CPUFrameSource is an optional Renderer capability: the renderer can hand
	// out the finished frame as a CPU image instead of presenting it itself.
	CPUFrameSource = gfx.CPUFrameSource

	// Canvas is the drawing surface. Every draw method honors the current
	// state-stack frame: input rects are in LOGICAL coordinates, the canvas
	// scales them by the cumulative scale and intersects with the current
	// clip before rasterizing.
	Canvas = gfx.Canvas

	// ClipAware is implemented by Canvas (the state stack carries the
	// current clip). The interface stays exported because external widgets
	// (webview's GL scissor, ScrollView dirty-region short-circuit) use it
	// as a capability probe — every qui Canvas satisfies it.
	ClipAware = gfx.ClipAware

	// Color is a simple RGBA color definition.
	Color = gfx.Color

	// ColorFilter transforms every pixel of a source buffer through a
	// stateless per-pixel function. Applied by SaveLayer's composite step
	// BEFORE Porter-Duff blending, so a shape drawn inside a
	// ColorFilter-carrying layer looks as if every source color was
	// pre-transformed. Grayscale, invert, hue-shift, saturation adjust
	// all fit here.
	ColorFilter = gfx.ColorFilter

	// ConicGradient sweeps its stops around Center by angle, starting at Angle
	// (radians, clockwise from 12 o'clock — CSS `conic-gradient(from …)`), one full
	// turn mapping [0,1] over the stops.
	ConicGradient = gfx.ConicGradient

	// DropShadowImageFilter renders a Gaussian-ish drop shadow of the
	// layer's alpha silhouette behind the original content. The shadow
	// follows the layer's shape (not just a rect), so a rounded-rect
	// button, a circular avatar, and an SVG heart all cast shadows that
	// track their outlines — this is the value proposition over
	// qui.DrawShadow, which stamps a rounded-rect coverage regardless of
	// what content is on top.
	DropShadowImageFilter = gfx.DropShadowImageFilter

	// ElevationSpec is one component of a drop shadow: offset, blur, spread
	// and an opacity that blends with Theme.Shadow. Two of them make one
	// elevation level — a tight "key" shadow plus a wider, softer "ambient"
	// one, which is what makes a raised surface read as lifted rather than
	// outlined.
	ElevationSpec = gfx.ElevationSpec

	// FillRule picks the inside-determination algorithm for self-
	// intersecting paths. SVG / Postscript / Skia default to non-zero;
	// even-odd is what the earlier rasterizer used.
	FillRule = gfx.FillRule

	// Font represents font styling; actual rasterization is delegated to renderer.
	Font = gfx.Font

	// FontWeight tracks CSS-like numeric weight.
	FontWeight = gfx.FontWeight

	// GradientStop is one anchor of a gradient — a position on [0, 1]
	// (with 0 meaning "at start", 1 meaning "at end") paired with the
	// color the gradient hits there. Callers are expected to supply
	// stops sorted by Offset ascending; the samplers linearly
	// interpolate between adjacent stops.
	GradientStop = gfx.GradientStop

	// ImageFilter is applied to a WHOLE layer buffer at composite time —
	// it returns a new (or same) image that replaces the source buffer
	// before Porter-Duff blending. Blur, drop-shadow, offset, and other
	// effects that need multi-pixel neighborhood access live here (a
	// ColorFilter would only see one pixel at a time).
	ImageFilter = gfx.ImageFilter

	// ImageFit selects how an image maps onto a box (CSS background-size /
	// object-fit).
	ImageFit = gfx.ImageFit

	// Insets represents margins/padding.
	Insets = gfx.Insets

	// LinearGradient interpolates colors along the vector from Start to
	// End. Points on the perpendicular line through Start receive the
	// first stop's color; points on the perpendicular through End receive
	// the last stop's color. Anything past the endpoints saturates to the
	// nearest stop (clamp spread — Skia calls this SkTileMode::kClamp).
	LinearGradient = gfx.LinearGradient

	// Matrix is a 2x3 affine transform stored row-major: [a b tx; c d ty].
	Matrix = gfx.Matrix

	// MatrixColorFilter is a 4x5 color-matrix filter, the standard model
	// used by CSS `filter: contrast(...)`, Android ColorMatrix, and Skia's
	// SkColorMatrix. Each output channel is a linear combination of the
	// four input channels plus a constant offset:
	MatrixColorFilter = gfx.MatrixColorFilter

	// NoopRenderer is a placeholder renderer that performs no drawing.
	NoopRenderer = gfx.NoopRenderer

	// Paint bundles the visual properties of a draw operation — color,
	// fill vs stroke, stroke width, stroke caps / joins, fill rule, and
	// anti-aliasing. Every shape draws with a Paint; the same Paint applies
	// across rects, rounded rects, lines, polylines, and paths so a new
	// property gets added in ONE place instead of multiplying across every
	// primitive's signature.
	Paint = gfx.Paint

	// PaintStyle selects between fill and stroke. Future-proofed as an int
	// so adding PaintFillAndStroke (Skia has it for paths) doesn't break
	// the enumeration.
	PaintStyle = gfx.PaintStyle

	// Path is a Skia-style sequence of moveTo / lineTo / quadTo / cubicTo /
	// close commands that compose an arbitrary shape. Use as the geometry
	// input to Canvas.DrawShape via ShapePath.
	Path = gfx.Path

	// Point represents an X/Y coordinate.
	Point = gfx.Point

	// RadialGradient interpolates colors from Center outward along a
	// disk of radius Radius. Points at the center receive the first
	// stop's color; points at distance == Radius receive the last stop's;
	// beyond Radius saturates to the last stop (clamp spread).
	RadialGradient = gfx.RadialGradient

	// Rect represents a rectangle in window coordinates.
	Rect = gfx.Rect

	// Renderer drives per-frame rendering and exposes a canvas.
	Renderer = gfx.Renderer

	// Shader is a per-pixel color generator. When a Paint has a non-nil
	// Shader, the rasterizer samples ColorAt(x, y) at each pixel's center
	// in LOGICAL coordinates (the same space rects and paths are quoted
	// in), then applies coverage as usual. When Shader is nil the
	// rasterizer takes the solid-Color fast path — zero overhead vs. the
	// pre-shader implementation.
	Shader = gfx.Shader

	// Shape is the geometric input to Canvas.DrawShape. Every concrete
	// drawable primitive (rect, rounded rect, line, polyline, future
	// path) implements this single-method interface — the sealing avoids
	// arbitrary types accidentally satisfying it while keeping the type-
	// switch inside DrawShape exhaustive.
	Shape = gfx.Shape

	// ShapeLine is a single segment between two points. Cap shape comes
	// from the Paint. AA at the rim. Stroke width comes from the Paint.
	ShapeLine = gfx.ShapeLine

	// ShapePath is the Shape-compatible wrapper around a Path.
	ShapePath = gfx.ShapePath

	// ShapePolyline is a sequence of points joined by line segments.
	// Joins between segments are painted according to Paint.Join.
	// Fewer than two points is a no-op.
	ShapePolyline = gfx.ShapePolyline

	// ShapeRRect is a rectangle with uniform corner radius. Radius == 0
	// degenerates to a plain rect; Radius >= min(W,H)/2 produces a
	// pill / circle (the ScaleRoundedShape pill-detection path).
	ShapeRRect = gfx.ShapeRRect

	// ShapeRect is a non-rounded axis-aligned rectangle.
	ShapeRect = gfx.ShapeRect

	// Size represents width and height in logical pixels.
	Size = gfx.Size

	// StrokeCap selects the cap shape at the start and end of an open
	// stroked subpath. CapRound is the zero value because the existing
	// capsule rasterizer already paints rounded caps — switching to
	// CapButt / CapSquare means stamping a flat / extended rectangle
	// instead of the round cap.
	StrokeCap = gfx.StrokeCap

	// StrokeJoin selects the shape painted where two stroked segments
	// meet inside a polyline / path. JoinMiter is the SVG / Canvas / Skia
	// default; the polyline rasterizer falls back to bevel if a miter
	// would exceed MiterLimit * StrokeWidth.
	StrokeJoin = gfx.StrokeJoin

	// VectorSource is something that can rasterize itself at an arbitrary
	// pixel size. The svg subpackage's Icon type is the canonical
	// implementation, but the interface is intentionally minimal so other
	// resolution-independent assets (e.g. an emoji vector path, a future
	// path/glyph type) can plug in without depending on a specific format.
	VectorSource = gfx.VectorSource
)

const (
	// BlendDstIn keeps the destination only where the source has coverage:
	// out = dst * src.a. The primitive behind mask filters — paint a
	// coverage source into a temporary layer, then composite the covered
	// region back with DstIn to punch through anything outside the mask.
	BlendDstIn = gfx.BlendDstIn

	// BlendDstOut is the inverse: out = dst * (1 - src.a). Used for
	// erasing / cutouts (donut cutout, inner-shadow inner-mask step).
	BlendDstOut = gfx.BlendDstOut

	// BlendMultiply darkens: out = src * dst (component-wise). Common
	// for tinting under a shadow layer.
	BlendMultiply = gfx.BlendMultiply

	// BlendPlus adds linearly (clamped): out = min(1, src + dst).
	// Skia's "plus" — used for additive light overlays where SrcOver's
	// alpha-weighted addition undersells the intensity.
	BlendPlus = gfx.BlendPlus

	// BlendScreen lightens: out = 1 - (1-src) * (1-dst). Used for
	// bloom / glare-style additive-but-clamped compositing.
	BlendScreen = gfx.BlendScreen

	// BlendSrc replaces the destination with the source (Porter-Duff Copy).
	// Useful for cutting a hole (paint (0,0,0,0) with BlendSrc) or for
	// initializing a layer that ignores its parent.
	BlendSrc = gfx.BlendSrc

	// BlendSrcOver is the standard alpha-composite: out = src + dst*(1-src.a).
	// The default (zero-value) mode — matches Skia and CSS canvas.
	BlendSrcOver = gfx.BlendSrcOver

	// no cap — stroke ends exactly at the endpoint
	CapButt = gfx.CapButt

	// semicircular cap centered at the endpoint
	CapRound = gfx.CapRound

	// square cap extending halfWidth past the endpoint
	CapSquare = gfx.CapSquare

	// odd number of crossings → inside
	FillEvenOdd = gfx.FillEvenOdd

	// signed winding count ≠ 0 → inside
	FillNonZero          = gfx.FillNonZero
	FontWeightBlack      = gfx.FontWeightBlack
	FontWeightBold       = gfx.FontWeightBold
	FontWeightExtraBold  = gfx.FontWeightExtraBold
	FontWeightExtraLight = gfx.FontWeightExtraLight
	FontWeightLight      = gfx.FontWeightLight
	FontWeightMedium     = gfx.FontWeightMedium
	FontWeightNormal     = gfx.FontWeightNormal
	FontWeightSemiBold   = gfx.FontWeightSemiBold
	FontWeightThin       = gfx.FontWeightThin

	// scale to fit inside, letterboxing (center)
	ImageContain = gfx.ImageContain

	// scale to cover, cropping overflow (center)
	ImageCover = gfx.ImageCover

	// fill the box (100% 100%), aspect ignored
	ImageStretch = gfx.ImageStretch

	// straight edge between the outer corners
	JoinBevel = gfx.JoinBevel

	// pointed corner (subject to MiterLimit)
	JoinMiter = gfx.JoinMiter

	// arc segment
	JoinRound   = gfx.JoinRound
	PaintFill   = gfx.PaintFill
	PaintStroke = gfx.PaintStroke
)

// VARS: copies, review!
var (
	ColorBlack       = gfx.ColorBlack
	ColorBlue        = gfx.ColorBlue
	ColorGray        = gfx.ColorGray
	ColorTransparent = gfx.ColorTransparent
	ColorWhite       = gfx.ColorWhite
)

// AdjustBrightness returns c with R/G/B shifted by delta, clamped.
// Alpha is preserved.
func AdjustBrightness(c Color, delta float32) Color { return gfx.AdjustBrightness(c, delta) }

// DefaultPaint returns a Paint with AA on, miter join, miter limit 10 —
// the conventional Skia defaults. Most callers use the struct-literal
// form and rely on the zero value; this helper is for code that needs a
// starting Paint to mutate.
func DefaultPaint(color Color) Paint { return gfx.DefaultPaint(color) }

// DrawVector is a thin shim that forwards to the canvas's own DrawVector
// method. Kept as a free function for the historical call sites (svg
// examples, widget helpers, embedded apps) that pre-date DrawVector
// being part of Canvas. New code should call canvas.DrawVector(…)
// directly.
func DrawVector(canvas Canvas, src VectorSource, rect Rect, tint Color) {
	gfx.DrawVector(canvas, src, rect, tint)
}

// GrayscaleColorFilter returns a matrix that maps every color to its
// luminance in the ITU-R BT.601 formula (Rec.601, which is what CSS
// `filter: grayscale(1)` uses). Alpha unchanged.
func GrayscaleColorFilter() MatrixColorFilter { return gfx.GrayscaleColorFilter() }

// IdentityMatrix returns the identity (no-op) matrix.
func IdentityMatrix() Matrix { return gfx.IdentityMatrix() }

// IdentityMatrixColorFilter returns the no-op color matrix. Handy as a
// starting point for callers building a custom matrix, and as a
// sentinel value that produces no visual change.
func IdentityMatrixColorFilter() MatrixColorFilter { return gfx.IdentityMatrixColorFilter() }

// ImageShader returns a Shader that samples img onto bounds with the given fit.
func ImageShader(img image.Image, bounds Rect, fit ImageFit) Shader {
	return gfx.ImageShader(img, bounds, fit)
}

// InvertColorFilter returns a matrix that flips each color channel
// (1 - c) while leaving alpha untouched.
func InvertColorFilter() MatrixColorFilter { return gfx.InvertColorFilter() }

// LerpColor linearly interpolates between a and b at t ∈ [0, 1].
// Straight RGB interpolation — fine for close-hue transitions (hover
// tints); for wide hue sweeps a proper HSL/OKLAB lerp is better but
// out of scope.
func LerpColor(a Color, b Color, t float32) Color { return gfx.LerpColor(a, b, t) }

// NewPath returns an empty Path ready for building.
func NewPath() *Path { return gfx.NewPath() }

// RotateMatrix returns a rotation matrix (radians).
func RotateMatrix(theta float32) Matrix { return gfx.RotateMatrix(theta) }

// ScaleMatrix returns a scale matrix.
func ScaleMatrix(sx float32, sy float32) Matrix { return gfx.ScaleMatrix(sx, sy) }

// SolidShader returns a Shader that always returns c. Handy for
// callers that need a Shader-typed value.
func SolidShader(c Color) Shader { return gfx.SolidShader(c) }

// ThemeDefault returns c marked as a framework-seeded default: the light
// baseline value of a slot that should follow the active theme. A widget
// constructor seeds its color slots with marked colors and maps them to
// theme tokens at draw time. Anything later written into the slot is
// unmarked and paints exactly as written, even when its RGBA happens to
// equal the default. The theme mapping never decides by value which colors
// are its own.
func ThemeDefault(c Color) Color { return gfx.ThemeDefault(c) }

// TranslateMatrix returns a translation matrix.
func TranslateMatrix(dx float32, dy float32) Matrix { return gfx.TranslateMatrix(dx, dy) }

// WithClipRect is the lexically-scoped clip helper. It pushes a save
// frame, intersects the current clip with `rect`, runs `fn` against
// the same canvas (so the state stack is the only source of truth for
// the current clip), and restores when fn returns — even on panic.
func WithClipRect(canvas Canvas, rect Rect, fn func(Canvas)) { gfx.WithClipRect(canvas, rect, fn) }

type (
	// LayerRecord captures a SaveLayer call — the requested bounds, the
	// paint (BlendMode + Alpha for composite), and the save depth returned.
	LayerRecord = gfx.LayerRecord

	// LineRecord captures a DrawLine call.
	LineRecord = gfx.LineRecord

	// PathRecord captures a DrawShape(ShapePath) call — the path's (logical→
	// physical) bounds plus whether it was a stroke.
	PathRecord = gfx.PathRecord

	// RecordingCanvas captures draw calls for test verification. It
	// implements Canvas without any rasterization, so tests can assert on
	// call counts / args without booting a GL context. Shared across root
	// and subpackage tests to keep the stub surface in one place as new
	// Canvas methods are added.
	RecordingCanvas = gfx.RecordingCanvas

	// RoundedRecord captures a FillRoundedRect call.
	RoundedRecord = gfx.RoundedRecord

	// ShadowRecord captures a DrawShadow call.
	ShadowRecord = gfx.ShadowRecord
)
