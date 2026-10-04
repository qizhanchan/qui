// Package svg is qui's SVG 1.1 subset. It does two things in one
// type:
//
//  1. Loads SVG files from disk or memory and rasterizes them at any
//     pixel size (the original Icon use case — toolbar glyphs that
//     stay sharp at every framebuffer scale).
//
//  2. Lets you build SVG scenes programmatically from Go code —
//     Document + Group + Rect/Circle/Ellipse/Line/Polyline/Polygon/
//     Path elements with fill, stroke, dashing, transforms — and
//     either rasterize the result or serialize it back to SVG XML.
//
// Quick start (file → bitmap):
//
//	icon, _ := svg.ParseFile("toolbar/bold.svg")
//	qui.DrawVector(canvas, icon, rect, tintColor)
//
// Quick start (programmatic):
//
//	doc := svg.NewDocument(100, 100)
//	doc.Add(&svg.Rect{X: 10, Y: 10, W: 80, H: 80,
//	    elementBase: svg.elementBase{
//	        Style: svg.Style{Fill: svg.SolidPaint(qui.Color{R:1,A:1})},
//	    },
//	})
//	doc.WriteSVG(os.Stdout)
//	img := doc.Rasterize(200, 200, qui.Color{}) // qui.Color{} = no tint
//
// Both Document and (the backwards-compatible alias) Icon satisfy
// qui.VectorSource — they plug into widgets.Button.IconVector and
// any other call site that expects a resolution-independent asset.
//
// Tinting: the (width, height, tint) tuple keys an internal cache
// so the typical UI case — a fixed-size toolbar icon redrawn every
// frame — only rasterizes once per (size, color) combination. A
// non-zero tint collapses the SVG to a monochrome silhouette in
// that color, which is exactly what the Font Awesome solid-icon set
// expects. Pass a zero tint (all components 0, including alpha) to
// keep the document's authored colors.
//
// Scope: shapes + fills + strokes + transforms only. <text>,
// gradients, masks, clip-paths, filters, and SMIL animation are
// deferred — they parse-and-skip without error, but render as if
// they weren't there.
package svg

// Icon is a backwards-compat alias for Document. The original API
// returned *Icon from Parse*; existing callers (examples/svg,
// q-excel toolbar, anything keyed *Icon in a map) keep compiling
// unchanged while gaining the full new Document API.
type Icon = Document
