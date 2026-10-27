package qui

import (
	"errors"
	"image"
	"image/draw"
	"sync"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Hidden and image cursors.
//
// Both are ordinary CursorShape values, so they flow through the same
// resolution as the built-in shapes (native_cursor.go): a widget declares one
// with SetCursorShape or publishes it from CursorProvider /
// CursorAtProvider, and the window applies it while the pointer is over
// that widget. A remote-desktop viewer, for instance, declares
// CursorHidden on its frame view while the remote draws its own pointer,
// or mirrors the remote pointer with NewImageCursor.
//
// Image cursors are a process-wide registry. Creating one is cheap
// bookkeeping (any goroutine); the native cursor object is built lazily,
// on the UI thread, the first time a window shows it, and shared between
// windows where the platform allows. ReleaseCursor frees both.

// imageCursorBase is the first id handed to an image cursor; the range
// below it belongs to the built-in shapes.
const imageCursorBase CursorShape = 1 << 20

// ErrInvalidCursorImage is returned for an empty image or a non-positive
// scale.
var ErrInvalidCursorImage = errors.New("qui: invalid cursor image")

// imageCursor is one registered image cursor. img is straight-alpha, in device pixels;
// scale pixels make one logical point, and the hot spot is in points.
type imageCursor struct {
	img        *image.NRGBA
	scale      float32
	hotX, hotY float32
}

// pointSize is the cursor's logical size.
func (c *imageCursor) pointSize() (w, h float32) {
	return float32(c.img.Rect.Dx()) / c.scale, float32(c.img.Rect.Dy()) / c.scale
}

var (
	imageCursorsMu sync.RWMutex
	imageCursors   = map[CursorShape]*imageCursor{}
	nextCursorID   = imageCursorBase
)

// NewImageCursor registers img as a cursor whose pixels are logical points
// (scale 1). (hotX, hotY) is the click point, in points from the image's
// top-left; it is clamped into the image. Safe from any goroutine.
func NewImageCursor(img image.Image, hotX, hotY float32) (CursorShape, error) {
	return NewImageCursorScaled(img, 1, hotX, hotY)
}

// NewImageCursorScaled registers a high-density cursor image: scale image
// pixels make one logical point (2 for a Retina-resolution image), so a
// 64×64 image at scale 2 is a 32×32-point cursor drawn at full resolution
// on a 2× display. The hot spot is in points. Backends that cannot show a
// high-density cursor downsample it. Safe from any goroutine.
func NewImageCursorScaled(img image.Image, scale, hotX, hotY float32) (CursorShape, error) {
	if img == nil || img.Bounds().Empty() || !(scale > 0) {
		return CursorDefault, ErrInvalidCursorImage
	}
	b := img.Bounds()
	px := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(px, px.Rect, img, b.Min, draw.Src)
	c := &imageCursor{img: px, scale: scale}
	w, h := c.pointSize()
	c.hotX, c.hotY = clampf(hotX, 0, w-1/scale), clampf(hotY, 0, h-1/scale)

	imageCursorsMu.Lock()
	id := nextCursorID
	nextCursorID++
	imageCursors[id] = c
	imageCursorsMu.Unlock()
	return id, nil
}

// ReleaseCursor frees an image cursor made by NewImageCursor[Scaled]: any
// window showing it switches to CursorDefault, the native object is
// destroyed, and the shape becomes invalid (a widget still declaring it
// gets the default arrow). No-op for built-in shapes and unknown ids.
// Must run on the UI thread — native cursors are UI objects.
func ReleaseCursor(shape CursorShape) {
	if shape < imageCursorBase {
		return
	}
	imageCursorsMu.Lock()
	_, ok := imageCursors[shape]
	delete(imageCursors, shape)
	imageCursorsMu.Unlock()
	if !ok {
		return
	}
	appMu.Lock()
	app := processApp
	appMu.Unlock()
	if app != nil {
		for _, w := range app.windows {
			w.dropCursor(shape)
		}
	}
	releaseNativeCursor(shape)
}

// dropCursor stops w from showing shape and frees any per-window native
// cursor built for it.
func (w *Window) dropCursor(shape CursorShape) {
	if w == nil {
		return
	}
	if w.cursorShape == shape {
		w.cursorShape = CursorDefault
		if w.plat != nil {
			w.plat.setCursor(CursorDefault)
		}
	}
	if r, ok := platformOptional[contract.CursorReleaser](w.plat); ok {
		r.ReleaseCursor(shape)
	}
}

// imageCursorForBackend implements contract.Host.ImageCursor: the cursor's
// one-pixel-per-point rendition and hotspot, for backends that build their
// own cursor objects from pixels.
func imageCursorForBackend(shape CursorShape) (*image.NRGBA, int, int, bool) {
	ic := lookupImageCursor(shape)
	if ic == nil {
		return nil, 0, 0, false
	}
	return ic.at1x(), int(ic.hotX), int(ic.hotY), true
}

// lookupImageCursor returns the registered cursor for shape, or nil.
func lookupImageCursor(shape CursorShape) *imageCursor {
	if shape < imageCursorBase {
		return nil
	}
	imageCursorsMu.RLock()
	defer imageCursorsMu.RUnlock()
	return imageCursors[shape]
}

// IsImageCursor reports whether shape is a live image cursor.
func IsImageCursor(shape CursorShape) bool { return lookupImageCursor(shape) != nil }

// at1x returns the cursor image downsampled to one pixel per point, for
// backends that only take point-sized images. Box filter, colour
// weighted by alpha so transparent pixels don't darken the edges.
func (c *imageCursor) at1x() *image.NRGBA {
	if c.scale == 1 {
		return c.img
	}
	pw, ph := c.pointSize()
	w, h := int(pw+0.5), int(ph+0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	src := c.img
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a, n uint32
			for sy := y0; sy < y1 && sy < sh; sy++ {
				for sx := x0; sx < x1 && sx < sw; sx++ {
					o := src.PixOffset(sx, sy)
					pa := uint32(src.Pix[o+3])
					r += uint32(src.Pix[o]) * pa
					g += uint32(src.Pix[o+1]) * pa
					b += uint32(src.Pix[o+2]) * pa
					a += pa
					n++
				}
			}
			o := out.PixOffset(x, y)
			if a > 0 {
				out.Pix[o] = uint8(r / a)
				out.Pix[o+1] = uint8(g / a)
				out.Pix[o+2] = uint8(b / a)
				out.Pix[o+3] = uint8(a / n)
			}
		}
	}
	return out
}

func clampf(v, lo, hi float32) float32 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
