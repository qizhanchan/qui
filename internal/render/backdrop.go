package render

import (
	"image"
	"math"

	"github.com/qizhanchan/qui/internal/gfx"
)

// BackdropFilter runs filter over what is already painted under bounds
// (CSS backdrop-filter): the pixels of the active target inside bounds —
// and inside shape, when given (rounded corners, a clip-path) — are
// replaced by the filtered copy. The filter sees only the region itself,
// its edges extended (CSS: the backdrop is cropped to the border box).
// Lengths in filter are physical pixels. No-op without a CPU backend.
func (c imageCanvas) BackdropFilter(bounds gfx.Rect, shape *gfx.Path, filter gfx.ImageFilter) {
	b, ok := c.Backend.(*CPUBackend)
	if !ok || filter == nil {
		return
	}
	top := c.TopCopy()
	phys := top.Clip.Intersect(top.Matrix.TransformRect(bounds))
	region := image.Rect(
		int(math.Floor(float64(phys.X))), int(math.Floor(float64(phys.Y))),
		int(math.Ceil(float64(phys.X+phys.W))), int(math.Ceil(float64(phys.Y+phys.H))))
	var mask *image.Alpha
	if shape != nil && !shape.IsEmpty() {
		mask = rasterizePathToAlpha(shape.Flatten(top.Matrix.TransformPoint), region)
	}
	b.backdropFilter(region, filter, mask)
}

// backdropFilter filters region of the active target in place.
func (b *CPUBackend) backdropFilter(region image.Rectangle, filter gfx.ImageFilter, mask *image.Alpha) {
	img := b.target(region)
	if img == nil {
		return
	}
	region = region.Intersect(img.Bounds())
	if region.Empty() {
		return
	}
	src := gfx.GetRGBA(region, false) // fully copied below
	defer gfx.PutRGBA(src)
	for y := region.Min.Y; y < region.Max.Y; y++ {
		copy(src.Pix[src.PixOffset(region.Min.X, y):src.PixOffset(region.Max.X-1, y)+4],
			img.Pix[img.PixOffset(region.Min.X, y):img.PixOffset(region.Max.X-1, y)+4])
	}
	out := filter.ApplyImage(src)
	if out == nil {
		return
	}
	if out != src {
		defer gfx.PutRGBA(out)
	}
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			if !(image.Point{X: x, Y: y}).In(out.Rect) {
				continue
			}
			oi, di := out.PixOffset(x, y), img.PixOffset(x, y)
			if mask == nil {
				copy(img.Pix[di:di+4], out.Pix[oi:oi+4])
				continue
			}
			m := uint32(mask.AlphaAt(x, y).A)
			if m == 0 {
				continue
			}
			for ch := 0; ch < 4; ch++ {
				img.Pix[di+ch] = uint8((uint32(out.Pix[oi+ch])*m + uint32(img.Pix[di+ch])*(255-m) + 127) / 255)
			}
		}
	}
}
