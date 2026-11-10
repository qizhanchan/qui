package render

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"os"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Deferred opaque fills — overdraw elimination for the CPU backend.
//
// A frame paints back to front: the window background, then each nested
// container's background, then the content. Every opaque background
// rewrites pixels the one under it just wrote, so a full repaint of an
// IDE-shaped window filled every pixel about five times.
//
// With deferral on, an opaque FillRect (or Clear / ClearRect — any write
// that replaces pixels without reading them) on the base framebuffer is
// recorded instead of drawn. A later replacing write subtracts its rect
// from the recorded ones (those pixels would be overwritten anyway), so
// the list stays pairwise disjoint and its order never matters. Every
// other operation first draws the recorded fills its region touches
// (target), and End draws the rest. The pixels are therefore exactly the
// ones immediate painting produces; only the writes that would have been
// overwritten are skipped.
//
// Only renderers that call End switch it on (CPURenderer, GLRenderer's
// CPU path, WebGPURenderer): a bare backend draws immediately.
// QUI_DEFER_FILLS=0 turns it off (to bisect a rendering difference).

// deferFillsDefault is what renderers pass to SetDeferFills.
var deferFillsDefault = os.Getenv("QUI_DEFER_FILLS") != "0"

// maxPendingFills bounds the list; past it everything pending is drawn.
const maxPendingFills = 64

type pendingFill struct {
	r image.Rectangle
	c color.RGBA // written with Src
}

// deferReplace records a replacing write of c over r on the base image.
func (b *CPUBackend) deferReplace(r image.Rectangle, c color.RGBA) {
	r = r.Intersect(b.base.Bounds())
	if r.Empty() {
		return
	}
	kept := b.scratch[:0]
	for _, p := range b.pending {
		if !p.r.Overlaps(r) {
			kept = append(kept, p)
			continue
		}
		kept = appendRectMinus(kept, p, r)
	}
	b.scratch, b.pending = b.pending[:0], append(kept, pendingFill{r: r, c: c})
	if len(b.pending) > maxPendingFills {
		b.flushPending(b.base.Bounds())
	}
}

// appendRectMinus appends the parts of p outside r (up to four bands).
func appendRectMinus(dst []pendingFill, p pendingFill, r image.Rectangle) []pendingFill {
	o := p.r
	i := o.Intersect(r)
	if i.Min.Y > o.Min.Y {
		dst = append(dst, pendingFill{image.Rect(o.Min.X, o.Min.Y, o.Max.X, i.Min.Y), p.c})
	}
	if i.Max.Y < o.Max.Y {
		dst = append(dst, pendingFill{image.Rect(o.Min.X, i.Max.Y, o.Max.X, o.Max.Y), p.c})
	}
	if i.Min.X > o.Min.X {
		dst = append(dst, pendingFill{image.Rect(o.Min.X, i.Min.Y, i.Min.X, i.Max.Y), p.c})
	}
	if i.Max.X < o.Max.X {
		dst = append(dst, pendingFill{image.Rect(i.Max.X, i.Min.Y, o.Max.X, i.Max.Y), p.c})
	}
	return dst
}

// flushPending draws the pending fills that touch region (whole, so the
// list does not fragment) and forgets them.
func (b *CPUBackend) flushPending(region image.Rectangle) {
	if len(b.pending) == 0 {
		return
	}
	kept := b.pending[:0]
	for _, p := range b.pending {
		if p.r.Overlaps(region) {
			stddraw.Draw(b.base, p.r, &image.Uniform{C: p.c}, image.Point{}, stddraw.Src)
		} else {
			kept = append(kept, p)
		}
	}
	b.pending = kept
}

// target is activeImg for an operation that touches at most region of
// it: pending fills under region land first.
func (b *CPUBackend) target(region image.Rectangle) *image.RGBA {
	img := b.activeImg()
	if len(b.pending) > 0 && img == b.base {
		b.flushPending(region)
	}
	return img
}

// replaceTarget reports whether a replacing write to the active target
// can be deferred.
func (b *CPUBackend) replaceTarget() bool {
	return b.deferFills && b.base != nil && b.activeImg() == b.base
}

// SetDeferFills turns deferred opaque fills on or off. The caller must
// call End before reading the base image.
func (b *CPUBackend) SetDeferFills(on bool) {
	if !on {
		b.flushPending(image.Rectangle{Max: image.Point{X: 1 << 30, Y: 1 << 30}})
	}
	b.deferFills = on
}

// outPix is the pixel rectangle covering every pixel r touches (with a
// pixel of margin for antialiasing).
func outPix(r gfx.Rect) image.Rectangle {
	return image.Rect(int(r.X)-1, int(r.Y)-1, int(r.X+r.W)+2, int(r.Y+r.H)+2)
}

func opaqueRGBA(c gfx.Color) (color.RGBA, bool) {
	if c.A < 0 {
		return color.RGBA{}, false
	}
	rgba := toRGBA(c)
	return rgba, rgba.A == 0xff
}

var colorTransparent = color.RGBA{}

// fillRectOn writes color into dst of img (the active target) like
// fillRectBlend, deferring it when it replaces pixels on the base image.
func (b *CPUBackend) fillRectOn(img *image.RGBA, dst image.Rectangle, c gfx.Color) {
	if img == b.base && b.deferFills {
		if rgba, ok := opaqueRGBA(c); ok {
			b.deferReplace(dst, rgba)
			return
		}
		b.flushPending(dst)
	}
	fillRectBlend(img, dst, c)
}

// pointsBounds is the pixel bounding box of pts grown by pad on each
// side (plus a pixel for antialiasing); empty input gives an empty box.
func pointsBounds(subs [][]gfx.Point, pad float32) image.Rectangle {
	first := true
	var x0, y0, x1, y1 float32
	for _, sub := range subs {
		for _, p := range sub {
			if first {
				x0, y0, x1, y1 = p.X, p.Y, p.X, p.Y
				first = false
				continue
			}
			x0, y0 = min(x0, p.X), min(y0, p.Y)
			x1, y1 = max(x1, p.X), max(y1, p.Y)
		}
	}
	if first {
		return image.Rectangle{}
	}
	return outPix(gfx.Rect{X: x0 - pad, Y: y0 - pad, W: x1 - x0 + 2*pad, H: y1 - y0 + 2*pad})
}
