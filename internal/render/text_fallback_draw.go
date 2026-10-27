package render

import (
	"image"
	"image/color"

	"github.com/go-text/typesetting/shaping"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
)

// drawFallbackGlyph draws one fallback cluster at display resolution:
// rasterized at fontSize × canvas scale and tinted with the text color,
// its baseline on the line's baseline.
func drawFallbackGlyph(c imageCanvas, line *typeset.ShapedLine, glyph shaping.Glyph, penX, baseline float32, fontSpec gfx.Font, col gfx.Color) {
	start := glyph.ClusterIndex
	end := start + glyph.RuneCount
	if start < 0 || end > len(line.Text) || start >= end {
		return
	}
	top := c.TopCopy()
	s := top.Matrix.AvgScale()
	if s <= 0 {
		return
	}
	size := fontSpec.Size
	if size <= 0 {
		size = 14
	}
	m := typeset.LookupFallbackMask(string(line.Text[start:end]), typeset.FontDescFor(fontSpec, size*s))
	if m == nil {
		return
	}
	img := tintMask(m.Mask, col)
	b := img.Bounds()
	rect := gfx.Rect{
		X: penX - float32(m.OriginX)/s,
		Y: baseline - float32(m.Baseline)/s,
		W: float32(b.Dx()) / s,
		H: float32(b.Dy()) / s,
	}
	mat := top.Matrix
	if mat.B != 0 || mat.C != 0 || mat.A <= 0 || mat.D <= 0 || absf(mat.A-mat.D) > 1e-3 {
		c.DrawImage(img, rect)
		return
	}
	// Uniform positive scale: snap to a whole device pixel and blit the
	// raster 1:1, as drawEmojiGlyph does, so the glyph stays crisp.
	dev := mat.TransformRect(rect)
	dev = gfx.Rect{X: typeset.Roundf(dev.X), Y: typeset.Roundf(dev.Y), W: float32(b.Dx()), H: float32(b.Dy())}
	c.Backend.DrawImage(img, dev, top.Clip)
}

// tintMask colors a coverage mask with col as premultiplied RGBA.
func tintMask(mask *image.Alpha, col gfx.Color) *image.RGBA {
	b := mask.Bounds()
	out := image.NewRGBA(b)
	c := toRGBA(col)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := uint32(mask.AlphaAt(x, y).A) * uint32(c.A) / 255
			if a == 0 {
				continue
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(uint32(c.R) * a / 255),
				G: uint8(uint32(c.G) * a / 255),
				B: uint8(uint32(c.B) * a / 255),
				A: uint8(a),
			})
		}
	}
	return out
}

// drawAtomicGlyph draws a glyph the shaper did not produce from a font:
// an emoji bitmap, or a platform-fallback cluster.
func drawAtomicGlyph(c imageCanvas, line *typeset.ShapedLine, glyph shaping.Glyph, penX, baseline, capHeight float32, fontSpec gfx.Font, col gfx.Color) {
	start, end := glyph.ClusterIndex, glyph.ClusterIndex+glyph.RuneCount
	if start >= 0 && end <= len(line.Text) && start < end && !typeset.IsEmojiCluster(line.Text[start:end]) &&
		typeset.IsFallbackCluster(line.Text[start:end]) {
		drawFallbackGlyph(c, line, glyph, penX, baseline, fontSpec, col)
		return
	}
	drawEmojiGlyph(c, line, glyph, penX, baseline, capHeight, fontSpec)
}
