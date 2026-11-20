package gfx

import (
	"image"
	"runtime"
	"sync"
	"weak"
)

// Volatile images are bitmaps their owner keeps drawing into after
// handing them to a canvas — a <canvas> element's backing store. A
// recorded display list (render.Picture) that shares an image must not
// see it change before playback, possibly on another thread, so the
// recorder copies volatile images and shares all others.

var volatileImages sync.Map // weak.Pointer[image.RGBA] → struct{}

// MarkVolatile registers img as one its owner mutates in place.
func MarkVolatile(img *image.RGBA) {
	if img == nil {
		return
	}
	wp := weak.Make(img)
	if _, dup := volatileImages.LoadOrStore(wp, struct{}{}); dup {
		return
	}
	runtime.AddCleanup(img, func(wp weak.Pointer[image.RGBA]) { volatileImages.Delete(wp) }, wp)
}

// IsVolatile reports whether img was registered with MarkVolatile.
func IsVolatile(img image.Image) bool {
	r, ok := img.(*image.RGBA)
	if !ok || r == nil {
		return false
	}
	_, ok = volatileImages.Load(weak.Make(r))
	return ok
}

// StableImage returns img, or a copy of it when it is volatile.
func StableImage(img image.Image) image.Image {
	if !IsVolatile(img) {
		return img
	}
	r := img.(*image.RGBA)
	c := &image.RGBA{Pix: append([]uint8(nil), r.Pix...), Stride: r.Stride, Rect: r.Rect}
	// A copy per recording: keep it out of caches keyed by image (it is
	// never drawn again once its picture is gone).
	MarkVolatile(c)
	return c
}
