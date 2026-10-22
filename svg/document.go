package svg

import (
	"image"
	"image/color"
	"os"
	"sync"

	"github.com/qizhanchan/qui"
)

// Document is the root of an SVG scene. It carries the viewBox /
// intrinsic dimensions, a root <g> equivalent, and an internal raster
// cache keyed by (width, height, tint). Both authored-by-hand documents
// (NewDocument + Add) and parsed-from-file documents (Parse*) end up
// as the same type, so callers can interchangeably rasterize, mutate,
// or serialize either.
type Document struct {
	ViewBox ViewBox
	Width   float32
	Height  float32
	Root    Group
	XMLNS   string

	mu    sync.Mutex
	cache map[cacheKey]*image.RGBA
	// order is cache's keys oldest-first, for eviction past
	// maxRasterCache; gen counts invalidations so a raster started before
	// one is not stored after it.
	order []cacheKey
	gen   uint64
}

// maxRasterCache bounds one document's cached rasters. Icon documents are
// shared process-wide, so a tint tween or a resize sweep would otherwise
// add a bitmap per distinct color/size forever.
const maxRasterCache = 32

type cacheKey struct {
	w, h       int
	r, g, b, a uint8
}

// NewDocument starts an empty document with viewBox = (0,0,w,h) and
// Width/Height matching. Add children with Add or by appending to
// Root.Children directly.
func NewDocument(width, height float32) *Document {
	return &Document{
		ViewBox: ViewBox{W: width, H: height},
		Width:   width,
		Height:  height,
		XMLNS:   "http://www.w3.org/2000/svg",
	}
}

// Add appends one or more elements to the document's root group and
// returns the document so calls chain: doc.Add(r).Add(c).
func (d *Document) Add(els ...Element) *Document {
	d.Root.Children = append(d.Root.Children, els...)
	d.invalidateCache()
	return d
}

// invalidateCache clears the raster cache. Called whenever the
// document's shape mutates so subsequent Rasterize calls don't return
// pixels from the old tree.
func (d *Document) invalidateCache() {
	d.mu.Lock()
	d.cache, d.order = nil, nil
	d.gen++
	d.mu.Unlock()
}

// Rasterize renders the document at the requested pixel size and tint.
// Repeat calls with the same arguments return the same cached
// *image.RGBA — callers must not mutate the returned image.
//
// width or height <= 0 returns nil. This matches the predecessor
// Icon.Rasterize behavior so existing callsites (Button.IconVector,
// toolbars) stay no-op safe when computing size from an empty
// rect.
func (d *Document) Rasterize(width, height int, tint qui.Color) image.Image {
	if d == nil || width <= 0 || height <= 0 {
		return nil
	}
	key := cacheKey{
		w: width, h: height,
		r: byteFrom(tint.R), g: byteFrom(tint.G),
		b: byteFrom(tint.B), a: byteFrom(tint.A),
	}
	d.mu.Lock()
	if img, ok := d.cache[key]; ok {
		d.mu.Unlock()
		return img
	}
	gen := d.gen
	d.mu.Unlock()

	// Render without the lock: other sizes/tints of a shared icon need
	// not wait on this one. Two callers racing on the same key both
	// render; the first to store wins.
	img := rasterizeDocument(d, width, height)
	if key.a == 0 {
		// Untinted path — keep the SVG's authored colors. Still need
		// to premultiply for image/draw composition.
		premultiplyInPlace(img)
	} else {
		applyTint(img, color.RGBA{R: key.r, G: key.g, B: key.b, A: key.a})
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gen != gen {
		return img // the document changed mid-render; don't cache stale pixels
	}
	if prev, ok := d.cache[key]; ok {
		return prev
	}
	if d.cache == nil {
		d.cache = make(map[cacheKey]*image.RGBA)
	}
	if len(d.order) >= maxRasterCache {
		delete(d.cache, d.order[0])
		d.order = d.order[1:]
	}
	d.cache[key] = img
	d.order = append(d.order, key)
	return img
}

// ParseFile opens path and parses its contents. Mirrors the previous
// signature so existing callers don't need to change.
func ParseFile(path string) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// MustParseBytes panics on parse failure. Appropriate for go:embed
// constants whose validity is asserted at build time.
func MustParseBytes(data []byte) *Document {
	doc, err := ParseBytes(data)
	if err != nil {
		panic(err)
	}
	return doc
}
