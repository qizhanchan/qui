// Package gfx is qui's graphics vocabulary: geometry (Point, Size, Rect,
// Insets), Color, Font as a style value, Matrix and the save/restore state
// stack every canvas shares (CanvasState), Paint, Path and Shape, shaders,
// color and image filters, and the Canvas, Renderer and VectorSource
// contracts.
//
// It is the bottom layer of the engine: it imports nothing from qui, and the
// text engine (internal/typeset), the rasterizers (internal/render), the
// exporters (pdf, svg) and the engine itself all build on it. Root qui
// re-exports the public names as aliases (gfx.go), so qui.Rect and gfx.Rect
// are the same type.
package gfx
