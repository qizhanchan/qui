//go:build darwin && cgo

package qui

import (
	"image"
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

// nsCursors caches the NSCursor built for each image cursor (and the
// transparent one standing in for CursorHidden on the cocoa backend).
// NSCursor objects aren't tied to a window, so one serves every window.
// UI thread only.
var nsCursors = map[CursorShape]unsafe.Pointer{}

// nativeNSCursor returns the NSCursor for an image cursor or CursorHidden,
// building it on first use; nil for anything else.
func nativeNSCursor(shape CursorShape) unsafe.Pointer {
	if p, ok := nsCursors[shape]; ok {
		return p
	}
	var ic *imageCursor
	if shape == CursorHidden {
		ic = &imageCursor{img: image.NewNRGBA(image.Rect(0, 0, 1, 1)), scale: 1}
	} else if ic = lookupImageCursor(shape); ic == nil {
		return nil
	}
	pm := premultiplied(ic.img)
	w, h := ic.img.Rect.Dx(), ic.img.Rect.Dy()
	p := macos.CursorCreate(pm, w, h, float64(ic.scale), float64(ic.hotX), float64(ic.hotY))
	if p != nil {
		nsCursors[shape] = p
	}
	return p
}

// releaseNativeCursor frees the shared NSCursor of a released image cursor.
func releaseNativeCursor(shape CursorShape) {
	if p, ok := nsCursors[shape]; ok {
		delete(nsCursors, shape)
		macos.CursorRelease(p)
	}
}

// premultiplied returns img's pixels with color scaled by alpha, which is
// what an NSBitmapImageRep without the non-premultiplied flag expects.
func premultiplied(img *image.NRGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	out := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s := img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y)
			d := (y*w + x) * 4
			a := uint32(img.Pix[s+3])
			out[d] = uint8(uint32(img.Pix[s]) * a / 255)
			out[d+1] = uint8(uint32(img.Pix[s+1]) * a / 255)
			out[d+2] = uint8(uint32(img.Pix[s+2]) * a / 255)
			out[d+3] = uint8(a)
		}
	}
	return out
}

// nativeCursorForBackend implements contract.Host.NativeCursor.
func nativeCursorForBackend(shape CursorShape) unsafe.Pointer { return nativeNSCursor(shape) }
