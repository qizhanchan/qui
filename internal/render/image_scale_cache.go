package render

import (
	"container/list"
	"image"
	"math"
	"os"
	"sync"

	"github.com/qizhanchan/qui/internal/gfx"
	xdraw "golang.org/x/image/draw"
)

// Decoded images at drawing scale, after Chromium's ImageDecodeCache.
//
// A picture drawn through a transform (a rotated card, a scaled photo) is
// sampled once per destination pixel. Sampling the decoded original costs
// far more than its result is worth when the original is much larger than
// it is drawn — a 1200×900 JPEG shown as a 300×225 card — and when it is
// not RGBA (a JPEG's YCbCr planes need a color conversion per tap, four
// taps per bilinear sample). It also aliases: bilinear taps skip most of
// the source when minifying.
//
// So such images are resampled once, with a filter that integrates over
// the source (x/image BiLinear widens its kernel when downscaling), to the
// next quarter-octave scale at or above the one they are drawn at, as
// premultiplied RGBA; later draws at that scale reuse it. The cache is a
// process-wide LRU bounded by bytes. QUI_IMAGE_SCALE_CACHE=0 disables it.

var imageScaleCacheOff = os.Getenv("QUI_IMAGE_SCALE_CACHE") == "0"

const imageScaleCacheBudget = 128 << 20

type scaledKey struct {
	img    image.Image
	bucket int // quarter-octaves below 1×: scale = 2^(-bucket/4)
}

type scaledEntry struct {
	key scaledKey
	img *image.RGBA
}

var scaledCache = struct {
	sync.Mutex
	lru   *list.List // front = most recent
	items map[scaledKey]*list.Element
	bytes int
}{lru: list.New(), items: map[scaledKey]*list.Element{}}

// drawScale is the scale img is sampled at when drawn through m: its
// quarter-octave bucket and the size of the copy at that scale; copied is
// false when img itself is sampled (RGBA and not minified much).
func drawScale(img image.Image, m gfx.Matrix) (bucket, w, h int, copied bool) {
	b := img.Bounds()
	sx := math.Hypot(float64(m.A), float64(m.C))
	sy := math.Hypot(float64(m.B), float64(m.D))
	s := math.Max(sx, sy)
	_, isRGBA := img.(*image.RGBA)
	if b.Dx() <= 0 || b.Dy() <= 0 || s <= 0 || (isRGBA && s >= 0.75) {
		return 0, b.Dx(), b.Dy(), false
	}
	if s < 1 {
		bucket = int(math.Floor(-4 * math.Log2(s))) // round the scale UP to a quarter octave
	}
	scale := math.Pow(2, -float64(bucket)/4)
	w = max(1, int(math.Ceil(float64(b.Dx())*scale)))
	h = max(1, int(math.Ceil(float64(b.Dy())*scale)))
	return bucket, w, h, !(isRGBA && w == b.Dx() && h == b.Dy())
}

// scaledMatrix is the matrix drawing img's w×h copy where m draws img.
func scaledMatrix(img image.Image, m gfx.Matrix, w, h int) gfx.Matrix {
	b := img.Bounds()
	return m.Concat(gfx.ScaleMatrix(float32(b.Dx())/float32(w), float32(b.Dy())/float32(h)))
}

// dropScaled forgets img's cached copy at bucket (its GPU texture
// replaced it, gpu_raster.go).
func dropScaled(img image.Image, bucket int) {
	c := &scaledCache
	c.Lock()
	defer c.Unlock()
	if el, ok := c.items[scaledKey{img: img, bucket: bucket}]; ok {
		c.lru.Remove(el)
		delete(c.items, scaledKey{img: img, bucket: bucket})
		c.bytes -= len(el.Value.(*scaledEntry).img.Pix)
	}
}

// scaledForDrawReady is scaledForDraw when it needs no work — no copy to
// make, or the copy already cached; ready=false otherwise (the GPU raster
// then has the copy made in the background, gpu_raster.go).
func scaledForDrawReady(img image.Image, m gfx.Matrix) (out image.Image, mm gfx.Matrix, ready bool) {
	if imageScaleCacheOff {
		_, isRGBA := img.(*image.RGBA)
		return img, m, isRGBA
	}
	bucket, w, h, copied := drawScale(img, m)
	if !copied {
		_, isRGBA := img.(*image.RGBA)
		return img, m, isRGBA || img.Bounds().Empty()
	}
	if gfx.IsVolatile(img) {
		out, mm := scaledForDraw(img, m) // uncached, see there
		return out, mm, true
	}
	c := &scaledCache
	c.Lock()
	defer c.Unlock()
	el, ok := c.items[scaledKey{img: img, bucket: bucket}]
	if !ok {
		return nil, m, false
	}
	c.lru.MoveToFront(el)
	return el.Value.(*scaledEntry).img, scaledMatrix(img, m, w, h), true
}

// scaledForDraw returns the image to sample for drawing img through m and
// the matrix that draws it: img itself when it is RGBA and not minified
// much, else a cached premultiplied RGBA copy near the drawing scale.
func scaledForDraw(img image.Image, m gfx.Matrix) (image.Image, gfx.Matrix) {
	if imageScaleCacheOff {
		return img, m
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return img, m
	}
	sx := math.Hypot(float64(m.A), float64(m.C))
	sy := math.Hypot(float64(m.B), float64(m.D))
	s := math.Max(sx, sy)
	_, isRGBA := img.(*image.RGBA)
	if s <= 0 || (isRGBA && s >= 0.75) {
		return img, m
	}
	bucket := 0
	if s < 1 {
		bucket = int(math.Floor(-4 * math.Log2(s))) // round the scale UP to a quarter octave
	}
	scale := math.Pow(2, -float64(bucket)/4)
	w := max(1, int(math.Ceil(float64(b.Dx())*scale)))
	h := max(1, int(math.Ceil(float64(b.Dy())*scale)))
	if isRGBA && w == b.Dx() && h == b.Dy() {
		return img, m
	}
	if gfx.IsVolatile(img) {
		// Drawn into after this draw (a <canvas>): a cached copy would go
		// stale, and copies of it would only fill the cache.
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		if w == b.Dx() && h == b.Dy() {
			xdraw.Draw(out, out.Rect, img, b.Min, xdraw.Src)
		} else {
			xdraw.BiLinear.Scale(out, out.Rect, img, b, xdraw.Src, nil)
		}
		return out, m.Concat(gfx.ScaleMatrix(float32(b.Dx())/float32(w), float32(b.Dy())/float32(h)))
	}
	key := scaledKey{img: img, bucket: bucket}
	c := &scaledCache
	c.Lock()
	if el, ok := c.items[key]; ok {
		c.lru.MoveToFront(el)
		out := el.Value.(*scaledEntry).img
		c.Unlock()
		return out, m.Concat(gfx.ScaleMatrix(float32(b.Dx())/float32(w), float32(b.Dy())/float32(h)))
	}
	c.Unlock()

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == b.Dx() && h == b.Dy() {
		xdraw.Draw(out, out.Rect, img, b.Min, xdraw.Src)
	} else {
		xdraw.BiLinear.Scale(out, out.Rect, img, b, xdraw.Src, nil)
	}

	c.Lock()
	if _, ok := c.items[key]; !ok {
		c.items[key] = c.lru.PushFront(&scaledEntry{key: key, img: out})
		c.bytes += len(out.Pix)
		for c.bytes > imageScaleCacheBudget && c.lru.Len() > 1 {
			old := c.lru.Back()
			e := old.Value.(*scaledEntry)
			c.lru.Remove(old)
			delete(c.items, e.key)
			c.bytes -= len(e.img.Pix)
		}
	}
	c.Unlock()
	return out, m.Concat(gfx.ScaleMatrix(float32(b.Dx())/float32(w), float32(b.Dy())/float32(h)))
}
