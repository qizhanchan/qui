//go:build cgo && !js && !qui_nogl

package render

import "image"

// CanScrollPixels implements PixelScroller: only the CPU raster path keeps
// the frame in memory (the GPU raster backend's lives in an FBO).
func (r *GLRenderer) CanScrollPixels() bool {
	return r != nil && r.img != nil && !r.gpuRaster && r.texSrc == r.img
}

// ScrollPixels implements PixelScroller.
func (r *GLRenderer) ScrollPixels(rect image.Rectangle, dx, dy int) bool {
	return r.CanScrollPixels() && scrollImage(r.img, rect, dx, dy)
}
