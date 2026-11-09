package qui

import (
	"log"
	"os"
	"strings"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
	"github.com/qizhanchan/qui/internal/render"
)

// Renderer selection on surfaces that can present through Metal (cocoa) —
// see docs/rfc/0002-metal-renderer.zh.md.
//
// An app asks for NewGLRenderer (or sets no renderer at all); on a Metal
// surface the window runs a MetalRenderer instead, which rasterizes exactly
// like the GL renderer's default path and presents through a CAMetalLayer.
// A window whose widgets turn out to need raw GL (scene3d, the CEF webview,
// GL video) switches itself to a GLRenderer the first time one asks.
//
// QUI_RENDERER overrides the choice: "gl" keeps GL, "cpu" uses CPURenderer
// (layer contents, no GPU API), "metal" (the default where available).

// rendererPreference is QUI_RENDERER, lower-cased.
func rendererPreference() string {
	return strings.ToLower(strings.TrimSpace(os.Getenv("QUI_RENDERER")))
}

// metalSurfaceOf returns the window's Metal presentation path, if its
// surface has one.
func metalSurfaceOf(pw platformWindow) (contract.MetalSurface, bool) {
	if pw == nil {
		return nil, false
	}
	s := pw.surface()
	if cs, ok := s.(contractSurface); ok {
		ms, ok := cs.s.(contract.MetalSurface)
		return ms, ok
	}
	ms, ok := s.(contract.MetalSurface)
	return ms, ok
}

// preferredRenderer replaces a requested GLRenderer with what this window
// should actually run: a MetalRenderer on a Metal surface, CPURenderer
// under QUI_RENDERER=cpu. Other renderers, and every renderer once the
// window has fallen back to GL, pass through.
func (w *Window) preferredRenderer(r Renderer) Renderer {
	if _, isGL := r.(*GLRenderer); !isGL || w.forceGL {
		return r
	}
	switch rendererPreference() {
	case "gl":
		return r
	case "cpu":
		return NewCPURenderer()
	}
	if !render.MetalRendererAvailable {
		return r
	}
	// The GPU raster backend is GL-only for now: keep GL when asked for.
	if g, ok := r.(interface{ GPURaster() bool }); ok && g.GPURaster() {
		return r
	}
	if _, ok := metalSurfaceOf(w.plat); !ok {
		return r
	}
	return render.NewMetalRenderer()
}

// bindMetal hands a Metal renderer its window's layer before a frame. When
// the surface or the machine can't do Metal the window falls back to GL.
func (w *Window) bindMetal() {
	mr, ok := w.renderer.(render.MetalLayerRenderer)
	if !ok {
		return
	}
	ms, ok := metalSurfaceOf(w.plat)
	var layer unsafe.Pointer
	if ok {
		layer = ms.MetalLayer()
	}
	if layer != nil && mr.SetMetalLayer(layer) {
		if !w.metalLogged && os.Getenv("QUI_DEBUG_RENDERER") != "" {
			w.metalLogged = true
			log.Printf("qui: window %q presents through Metal", w.title)
		}
		return
	}
	log.Printf("qui: Metal presentation unavailable for this window; using OpenGL")
	w.fallBackToGL(ms)
}

// checkGLFallback switches the window to GL after a frame in which a
// widget asked the Metal renderer for raw GL.
func (w *Window) checkGLFallback() {
	fr, ok := w.renderer.(render.GLFallbackRenderer)
	if !ok || !fr.WantsGL() {
		return
	}
	ms, _ := metalSurfaceOf(w.plat)
	w.fallBackToGL(ms)
}

// fallBackToGL leaves Metal for good: the surface returns to its GL
// context and the window repaints everything with a GLRenderer.
func (w *Window) fallBackToGL(ms contract.MetalSurface) {
	if ms != nil {
		ms.ExitMetal()
	}
	w.closeRenderer()
	w.forceGL = true
	w.SetRenderer(NewGLRenderer())
	if os.Getenv("QUI_DEBUG_RENDERER") != "" {
		log.Printf("qui: window %q fell back to OpenGL", w.title)
	}
	if w.plat != nil {
		w.plat.makeCurrent()
	}
	w.Invalidate()
	w.InvalidateLayout()
}

// closeRenderer releases a renderer's native resources (a Metal device
// context) when the window stops using it.
func (w *Window) closeRenderer() {
	if c, ok := w.renderer.(interface{ Close() }); ok {
		c.Close()
	}
}

// rendererPresentsItself reports whether the renderer already presented
// the frame (a Metal renderer presents its own drawable).
func (w *Window) rendererPresentsItself() bool {
	sp, ok := w.renderer.(render.SelfPresentingRenderer)
	return ok && sp.PresentsItself()
}
