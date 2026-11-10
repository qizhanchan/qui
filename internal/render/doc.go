// Package render holds qui's rasterizers: the pure-Go CPU raster backend and
// its ImageCanvas, SaveLayer compositing, shadows, glyph-mask caching and
// shaped-text drawing, plus the renderers windows drive: CPURenderer
// (frames handed to the platform compositor), MetalRenderer (macOS,
// CAMetalLayer) and WebGPURenderer (Dawn — any GPU API), with the retained
// compositor draws and the WebGPU hooks GPU widgets use.
//
// It builds on internal/gfx, internal/typeset and internal/wgpu and imports
// nothing from qui; the window talks to a renderer through gfx.Renderer and
// the optional capability interfaces in renderer_caps.go. There is no
// OpenGL (docs/rfc/0003-webgpu.zh.md). Root qui re-exports the public API
// (render.go, render_gpu.go).
package render
