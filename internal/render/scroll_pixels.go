package render

import "image"

// scrollImage moves the pixels of img inside rect by (dx, dy) — a memmove
// per row, in the order that never reads a row it already overwrote.
// Reports false when rect is not inside img or the move leaves nothing.
func scrollImage(img *image.RGBA, rect image.Rectangle, dx, dy int) bool {
	if img == nil || !rect.In(img.Rect) {
		return false
	}
	w, h := rect.Dx(), rect.Dy()
	if dx <= -w || dx >= w || dy <= -h || dy >= h {
		return false
	}
	// Source columns [sx0, sx0+cw) move to [sx0+dx, ...): both inside rect.
	sx0, cw := rect.Min.X, w-iabs(dx)
	if dx < 0 {
		sx0 = rect.Min.X - dx
	}
	rowBytes := cw * 4
	moveRow := func(y int) {
		src := img.PixOffset(sx0, y)
		dst := img.PixOffset(sx0+dx, y+dy)
		copy(img.Pix[dst:dst+rowBytes], img.Pix[src:src+rowBytes])
	}
	if dy > 0 {
		for y := rect.Max.Y - 1 - dy; y >= rect.Min.Y; y-- {
			moveRow(y)
		}
	} else {
		for y := rect.Min.Y - dy; y < rect.Max.Y; y++ {
			moveRow(y)
		}
	}
	return true
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// CanScrollPixels implements PixelScroller.
func (r *CPURenderer) CanScrollPixels() bool { return r != nil && r.img != nil }

// ScrollPixels implements PixelScroller.
func (r *CPURenderer) ScrollPixels(rect image.Rectangle, dx, dy int) bool {
	return r != nil && scrollImage(r.img, rect, dx, dy)
}
