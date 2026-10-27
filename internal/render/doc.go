// Package render holds qui's rasterizers: the pure-Go CPU raster backend and
// its ImageCanvas, the opt-in OpenGL raster backend (QUI_GPU_RASTER=1),
// SaveLayer compositing, shadows, glyph-mask caching and shaped-text
// drawing, plus the two renderers windows drive, CPURenderer and GLRenderer,
// with their retained-GL and raw-GL hook plumbing.
//
// It builds on internal/gfx and internal/typeset and imports nothing from
// qui; the window talks to a renderer through gfx.Renderer and the optional
// capability interfaces in renderer_caps.go. GL code is compiled only with
// cgo, outside js, and without the qui_nogl tag; otherwise GLRenderer is an
// alias of CPURenderer. Root qui re-exports the public API (render.go,
// render_gl.go).
package render
