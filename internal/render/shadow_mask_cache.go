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

// shadowElideOff (QUI_SHADOW_ELIDE=0) caches whole masks, to bisect.
var shadowElideOff = os.Getenv("QUI_SHADOW_ELIDE") == "0"

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

// shadowMask is a cached mask and how it maps onto the full shadow
// buffer. A rounded rectangle's blurred mask is constant across the
// middle of each side: away from the corners (and the blur's reach of
// them) every row is the same, and every column. The cache keeps the
// mask of the shadow shortened by those runs (Skia's nine-patch blurred
// rrect), so a card's mask is its corners and one row and column of
// middle, not the card's whole area: elided runs of dX columns and dY
// rows repeat column splitX and row splitY of the kept mask.
type shadowMask struct {
	mask       []uint8
	w, h       int // kept mask size
	splitX, dX int
	splitY, dY int
}

// shadowElideMin is the shortest run worth eliding.
const shadowElideMin = 8

// shadowElide finds, along one axis, the run of buffer rows (or columns)
// of a blurred rounded-rectangle mask that are all equal: s0/sLen/sr the
// shadow shape's start (buffer coordinates), length and clamped corner
// radius, k0/kLen/kr the knockout's when knock. It returns the first row
// of the run and how many rows after it repeat it (0: none elided).
func shadowElide(s0, sLen, sr, k0, kLen, kr float32, knock bool, boxR int) (split, d int) {
	// Pixel rows wholly inside the straight span of the shape: stamped
	// identically (stampShadowAlpha's mid rows).
	a := int(math.Ceil(float64(s0 + sr)))
	b := int(math.Floor(float64(s0+sLen-sr))) - 1
	if knock {
		a = max(a, int(math.Ceil(float64(k0+kr))))
		b = min(b, int(math.Floor(float64(k0+kLen-kr)))-1)
	}
	// Four box passes, two per axis, each reaching boxR: a row is equal
	// to its neighbors when everything it blurs is in the straight span.
	// The +1s keep a row of slack for float rounding.
	a += 2*boxR + 1
	b -= 2*boxR + 1
	if b-a < shadowElideMin || shadowElideOff {
		return 0, 0
	}
	return a, b - a
}

// cachedShadowMask returns the blurred mask of the shadow (whole: bufW×
// bufH with origin at device (x0, y0), possibly with middle runs elided),
// computing and caching it on a miss; false when the shadow is too large
// to keep.
func cachedShadowMask(shadowRect gfx.Rect, shadowRadius float32, rect gfx.Rect, radius float32, knockout bool,
	blur float32, boxR, margin, x0, y0, bufW, bufH int) (shadowMask, bool) {
	if shadowMaskCacheOff {
		return shadowMask{}, false
	}
	ox, oy := float32(x0), float32(y0)
	if blur <= 0 {
		boxR = 0
	}
	// Corner radii as stampShadowAlpha clamps them for the full shape;
	// the shortened shape keeps every straight span at least that long,
	// so it clamps them the same.
	sr := max(0, min(shadowRadius, min(shadowRect.W, shadowRect.H)/2))
	kr := max(0, min(radius, min(rect.W, rect.H)/2))
	sm := shadowMask{w: bufW, h: bufH}
	sm.splitX, sm.dX = shadowElide(shadowRect.X-ox, shadowRect.W, sr, rect.X-ox, rect.W, kr, knockout, boxR)
	sm.splitY, sm.dY = shadowElide(shadowRect.Y-oy, shadowRect.H, sr, rect.Y-oy, rect.H, kr, knockout, boxR)
	sm.w -= sm.dX
	sm.h -= sm.dY
	shadowRect.W -= float32(sm.dX)
	shadowRect.H -= float32(sm.dY)
	if knockout {
		rect.W -= float32(sm.dX)
		rect.H -= float32(sm.dY)
	}
	if sm.w*sm.h > shadowMaskMax {
		return shadowMask{}, false
	}
	k := shadowMaskKey{
		w: shadowRect.W, h: shadowRect.H, fx: shadowRect.X - ox, fy: shadowRect.Y - oy, radius: sr,
		knock: knockout, boxR: boxR, bufW: sm.w, bufH: sm.h,
	}
	if knockout {
		k.kx, k.ky, k.kw, k.kh, k.kr = rect.X-ox, rect.Y-oy, rect.W, rect.H, kr
	}
	c := &shadowMasks
	c.Lock()
	if el, ok := c.items[k]; ok {
		c.lru.MoveToFront(el)
		sm.mask = el.Value.(*shadowMaskEntry).mask
		c.Unlock()
		return sm, true
	}
	c.Unlock()

	w, h := sm.w, sm.h
	mask := make([]uint8, w*h)
	stampShadowAlpha(mask, w, h, x0, y0, shadowRect, sr)
	if boxR > 0 {
		tmp := gfx.GetPix(w*h, false)
		gfx.BoxBlurH(mask, tmp, w, h, boxR)
		gfx.BoxBlurV(tmp, mask, w, h, boxR)
		gfx.BoxBlurH(mask, tmp, w, h, boxR)
		gfx.BoxBlurV(tmp, mask, w, h, boxR)
		gfx.PutPix(tmp)
	}
	if knockout {
		hole := gfx.GetPix(w*h, false)
		stampShadowAlpha(hole, w, h, x0, y0, rect, kr)
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
	sm.mask = mask
	return sm, true
}

// keptIndex maps a full-buffer row (or column) to the kept mask's.
func keptIndex(i, split, d int) int {
	switch {
	case d == 0 || i <= split:
		return i
	case i <= split+d:
		return split
	}
	return i - d
}

// blendShadowMask composites sm (origin at device (x0, y0)) in
// shadowColor at alpha over img inside eff — the blend of
// drawShadowImplRaw.
func blendShadowMask(img *image.RGBA, sm shadowMask, x0, y0 int, eff image.Rectangle, shadowColor gfx.Color, alpha float32) {
	r := uint32(shadowColor.R * 255)
	g := uint32(shadowColor.G * 255)
	bch := uint32(shadowColor.B * 255)
	area := eff.Intersect(image.Rect(x0, y0, x0+sm.w+sm.dX, y0+sm.h+sm.dY))
	if area.Empty() {
		return
	}
	// The kept columns each destination column reads, once.
	cols := make([]int32, area.Dx())
	for i := range cols {
		cols[i] = int32(keptIndex(area.Min.X+i-x0, sm.splitX, sm.dX))
	}
	for dy := area.Min.Y; dy < area.Max.Y; dy++ {
		my := keptIndex(dy-y0, sm.splitY, sm.dY)
		row := sm.mask[my*sm.w : (my+1)*sm.w]
		pix := img.Pix[img.PixOffset(area.Min.X, dy):]
		for i, mx := range cols {
			a := row[mx]
			if a == 0 {
				continue
			}
			pixA := uint32(float32(a) * alpha)
			if pixA == 0 {
				continue
			}
			p := pix[i*4 : i*4+4 : i*4+4]
			invA := 255 - pixA
			p[0] = uint8((r*pixA + uint32(p[0])*invA) / 255)
			p[1] = uint8((g*pixA + uint32(p[1])*invA) / 255)
			p[2] = uint8((bch*pixA + uint32(p[2])*invA) / 255)
			outA := pixA + uint32(p[3])*invA/255
			if outA > 255 {
				outA = 255
			}
			p[3] = uint8(outA)
		}
	}
}

var _ = math.Abs
