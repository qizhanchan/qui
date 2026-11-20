package render

import (
	"image"
	stddraw "image/draw"
)

// drawOverRGBA composites src over dst (both premultiplied RGBA) in r,
// with src aligned at sp: the same result, byte for byte, as
// draw.Draw(dst, r, src, sp, draw.Over), but run-length aware. The
// standard library blends every pixel with 16-bit multiplies and divides
// even where the source is opaque; a page's content cache and most image
// layers are opaque almost everywhere, so copying opaque runs with copy()
// and skipping transparent ones makes a full-viewport composite several
// times cheaper (an animated marquee recomposites a band of the viewport
// every frame).
func drawOverRGBA(dst *image.RGBA, r image.Rectangle, src *image.RGBA, sp image.Point) {
	// Clip like image/draw: to dst, then to src, moving sp along.
	orig := r.Min
	r = r.Intersect(dst.Rect)
	sp = sp.Add(r.Min.Sub(orig))
	sr := image.Rectangle{Min: sp, Max: sp.Add(r.Size())}.Intersect(src.Rect)
	r.Min = r.Min.Add(sr.Min.Sub(sp))
	r.Max = r.Min.Add(sr.Size())
	sp = sr.Min
	if r.Empty() {
		return
	}
	if dst.Pix != nil && src.Pix != nil && &dst.Pix[0] == &src.Pix[0] {
		// Overlapping buffers: the standard library handles the order.
		stddraw.Draw(dst, r, src, sp, stddraw.Over)
		return
	}
	w := r.Dx() * 4
	for y := 0; y < r.Dy(); y++ {
		d0 := dst.PixOffset(r.Min.X, r.Min.Y+y)
		s0 := src.PixOffset(sp.X, sp.Y+y)
		dpix := dst.Pix[d0 : d0+w : d0+w]
		spix := src.Pix[s0 : s0+w : s0+w]
		for i := 0; i < w; {
			switch spix[i+3] {
			case 0xff:
				j := i + 4
				for j < w && spix[j+3] == 0xff {
					j += 4
				}
				copy(dpix[i:j], spix[i:j])
				i = j
			case 0:
				// Premultiplied transparent: dst unchanged.
				i += 4
			default:
				// image/draw's drawCopyOver arithmetic, so results match it.
				const m = 1<<16 - 1
				sr := uint32(spix[i+0]) * 0x101
				sg := uint32(spix[i+1]) * 0x101
				sb := uint32(spix[i+2]) * 0x101
				sa := uint32(spix[i+3]) * 0x101
				a := (m - sa) * 0x101
				dpix[i+0] = uint8((uint32(dpix[i+0])*a/m + sr) >> 8)
				dpix[i+1] = uint8((uint32(dpix[i+1])*a/m + sg) >> 8)
				dpix[i+2] = uint8((uint32(dpix[i+2])*a/m + sb) >> 8)
				dpix[i+3] = uint8((uint32(dpix[i+3])*a/m + sa) >> 8)
				i += 4
			}
		}
	}
}
