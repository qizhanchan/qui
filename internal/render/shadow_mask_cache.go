package render

import (
	"container/list"
	"image"
	"math"
	"os"
	"sync"

	"github.com/qizhanchan/qui/internal/gfx"
)

// shadowMaskCache keeps blurred outset-shadow masks (Skia's SkMaskCache):
// the mask of a shadow depends only on its size, its sub-pixel phase,
// radii, blur and knockout geometry, not on where it is drawn. Masks are
// immutable once cached and the cache is locked: raster workers share it.

var shadowMaskCacheOff = os.Getenv("QUI_SHADOW_CACHE") == "0"

const (
	shadowMaskBudget = 32 << 20 // bytes of masks kept
	shadowMaskMax    = 4 << 20  // larger masks are blurred per draw, windowed
)

type shadowMaskKey struct {
	w, h, fx, fy, radius float32 // shadow rect size and phase vs the buffer origin
	kx, ky, kw, kh, kr   float32 // knockout rect relative to the buffer origin, and radius
	knock                bool
	boxR, bufW, bufH     int
}

type shadowMaskEntry struct {
	key  shadowMaskKey
	mask []uint8
}

var shadowMasks = struct {
	sync.Mutex
	lru   *list.List
	items map[shadowMaskKey]*list.Element
	bytes int
}{lru: list.New(), items: map[shadowMaskKey]*list.Element{}}

// cachedShadowMask returns the whole blurred mask (bufW×bufH, origin at
// device (x0, y0)) of the shadow, computing and caching it on a miss; nil
// when the shadow is too large to keep.
func cachedShadowMask(shadowRect gfx.Rect, shadowRadius float32, rect gfx.Rect, radius float32, knockout bool,
	blur float32, boxR, margin, x0, y0, bufW, bufH int) []uint8 {
	if shadowMaskCacheOff || bufW*bufH > shadowMaskMax {
		return nil
	}
	ox, oy := float32(x0), float32(y0)
	k := shadowMaskKey{
		w: shadowRect.W, h: shadowRect.H, fx: shadowRect.X - ox, fy: shadowRect.Y - oy, radius: shadowRadius,
		knock: knockout, boxR: boxR, bufW: bufW, bufH: bufH,
	}
	if blur <= 0 {
		k.boxR = 0
	}
	if knockout {
		k.kx, k.ky, k.kw, k.kh, k.kr = rect.X-ox, rect.Y-oy, rect.W, rect.H, radius
	}
	c := &shadowMasks
	c.Lock()
	if el, ok := c.items[k]; ok {
		c.lru.MoveToFront(el)
		m := el.Value.(*shadowMaskEntry).mask
		c.Unlock()
		return m
	}
	c.Unlock()

	mask := make([]uint8, bufW*bufH)
	stampShadowAlpha(mask, bufW, bufH, x0, y0, shadowRect, shadowRadius)
	if k.boxR > 0 {
		tmp := gfx.GetPix(bufW*bufH, false)
		gfx.BoxBlurH(mask, tmp, bufW, bufH, boxR)
		gfx.BoxBlurV(tmp, mask, bufW, bufH, boxR)
		gfx.BoxBlurH(mask, tmp, bufW, bufH, boxR)
		gfx.BoxBlurV(tmp, mask, bufW, bufH, boxR)
		gfx.PutPix(tmp)
	}
	if knockout {
		hole := gfx.GetPix(bufW*bufH, false)
		stampShadowAlpha(hole, bufW, bufH, x0, y0, rect, radius)
		for i, coverage := range hole {
			mask[i] = uint8(uint32(mask[i]) * uint32(255-coverage) / 255)
		}
		gfx.PutPix(hole)
	}

	c.Lock()
	if _, ok := c.items[k]; !ok {
		c.items[k] = c.lru.PushFront(&shadowMaskEntry{key: k, mask: mask})
		c.bytes += len(mask)
		for c.bytes > shadowMaskBudget && c.lru.Len() > 1 {
			old := c.lru.Back()
			e := old.Value.(*shadowMaskEntry)
			c.lru.Remove(old)
			delete(c.items, e.key)
			c.bytes -= len(e.mask)
		}
	}
	c.Unlock()
	return mask
}

// blendShadowMask composites mask (bufW wide, origin at device (x0, y0))
// in shadowColor at alpha over img inside eff — the blend of
// drawShadowImplRaw.
func blendShadowMask(img *image.RGBA, mask []uint8, x0, y0, bufW int, eff image.Rectangle, shadowColor gfx.Color, alpha float32) {
	bufH := len(mask) / bufW
	r := uint32(shadowColor.R * 255)
	g := uint32(shadowColor.G * 255)
	bch := uint32(shadowColor.B * 255)
	area := eff.Intersect(image.Rect(x0, y0, x0+bufW, y0+bufH))
	for dy := area.Min.Y; dy < area.Max.Y; dy++ {
		row := mask[(dy-y0)*bufW : (dy-y0+1)*bufW]
		for dx := area.Min.X; dx < area.Max.X; dx++ {
			a := row[dx-x0]
			if a == 0 {
				continue
			}
			pixA := uint32(float32(a) * alpha)
			if pixA == 0 {
				continue
			}
			idx := img.PixOffset(dx, dy)
			pix := img.Pix[idx : idx+4 : idx+4]
			invA := 255 - pixA
			pix[0] = uint8((r*pixA + uint32(pix[0])*invA) / 255)
			pix[1] = uint8((g*pixA + uint32(pix[1])*invA) / 255)
			pix[2] = uint8((bch*pixA + uint32(pix[2])*invA) / 255)
			outA := pixA + uint32(pix[3])*invA/255
			if outA > 255 {
				outA = 255
			}
			pix[3] = uint8(outA)
		}
	}
}

var _ = math.Abs
