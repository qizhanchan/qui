package htmlcss

import (
	"image"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
)

// ResourceLoader resolves a document's external resources — today the
// images behind <img src> and background-image: url(). A host that loads
// documents from the network (a browser) plugs its fetcher in through
// Options.Resources; without one, only local paths (against BaseDir) and
// data: URIs load.
//
// Loading is asynchronous: Image answers at once from what the loader
// already has. While the resource is still in flight it reports Pending
// and calls ready once — ON THE UI THREAD (post it there, e.g. through
// Window.PostJob) — after which the element asks again and repaints.
type ResourceLoader interface {
	Image(src string, ready func()) ImageResource
}

// ImageResource is one answer from ResourceLoader.Image. Exactly one of
// Raster / Vector is set for a loaded image; neither for a failed or
// pending one.
type ImageResource struct {
	Raster  image.Image
	Vector  *svg.Document
	Pending bool
}

// resources returns the engine's loader, or nil.
func (e *El) resources() ResourceLoader {
	if e == nil || e.engine == nil {
		return nil
	}
	return e.engine.opts.Resources
}

// loadExternal asks the host loader for src. ok is false when there is no
// loader (the caller falls back to local loading). When the resource is
// pending, the element re-applies its style (reloading the image) once
// the loader reports it ready.
func (e *El) loadExternal(src string) (res ImageResource, ok bool) {
	rl := e.resources()
	if rl == nil || src == "" {
		return ImageResource{}, false
	}
	res = rl.Image(src, func() { e.resourceReady() })
	return res, true
}

// rasterizeBackgroundSVG turns an SVG used as a background image into a
// bitmap at its intrinsic size (2x for crispness), keeping its colors.
func rasterizeBackgroundSVG(d *svg.Document) image.Image {
	w, h := d.Width, d.Height
	if w <= 0 || h <= 0 {
		w, h = d.ViewBox.W, d.ViewBox.H
	}
	if w <= 0 || h <= 0 {
		w, h = 16, 16
	}
	return d.Rasterize(int(w*2+0.5), int(h*2+0.5), qui.Color{})
}

// resourceReady re-runs the style application so ensureImg /
// applyBackgroundImage pick up a resource that finished loading.
func (e *El) resourceReady() {
	if e == nil || e.lastCS == nil {
		return
	}
	e.imgSrc = "\x00reload"
	e.bgImgSrc = "\x00reload"
	e.applyComputed(e.lastCS)
	e.syncChildren()
	e.InvalidateLayout()
	if e.folded {
		for p := e.elParent; p != nil; p = p.elParent {
			if !p.folded {
				p.markDirty()
				break
			}
		}
	}
}

// LoadLazyImages starts deferred images near a document-coordinate viewport.
// Native lazy loading is enabled only with scripting, as in HTML. The host
// calls this after layout and scrolling; unloaded images retain their authored
// dimensions but have no natural size until their resource is requested.
func (eng *StyleEngine) LoadLazyImages(viewport qui.Rect) {
	if eng == nil || eng.root == nil || viewport.W <= 0 || viewport.H <= 0 {
		return
	}
	root := eng.root.Bounds()
	origin := qui.LocalPointToWindow(eng.root, qui.Point{X: root.X, Y: root.Y})
	const distance = float32(1250) // desktop preload distance ahead of the viewport
	for e := range eng.lazyImages {
		hidden := false
		for p := e; p != nil; p = p.elParent {
			if p.displayNone || p.hidden {
				hidden = true
				break
			}
		}
		if hidden {
			continue
		}
		b := e.Bounds()
		pos := qui.LocalPointToWindow(e, qui.Point{X: b.X, Y: b.Y})
		x, y := pos.X-origin.X, pos.Y-origin.Y
		if y+b.H < viewport.Y-distance || y > viewport.Y+viewport.H+distance || x+b.W < viewport.X-distance || x > viewport.X+viewport.W+distance {
			continue
		}
		delete(eng.lazyImages, e)
		e.imgLazyStarted = true
		e.resourceReady()
	}
}
