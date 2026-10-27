package render

import (
	"math"

	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"

	"github.com/qizhanchan/qui/internal/gfx"
	"github.com/qizhanchan/qui/internal/typeset"
)

func (c imageCanvas) DrawShapedLine(line *typeset.ShapedLine, rect gfx.Rect, color gfx.Color, fontSpec gfx.Font) {
	if line == nil || len(line.Runs) == 0 || c.Backend == nil {
		return
	}
	capHeight := typeset.ShapedLineCapHeight(fontSpec)
	baseline := rect.Y + (rect.H+capHeight)/2
	c.drawShapedLineAtBaseline(line, rect.X, baseline, color, fontSpec)
}

func (c imageCanvas) drawShapedLineAtBaseline(line *typeset.ShapedLine, x, baseline float32, color gfx.Color, fontSpec gfx.Font) {
	capHeight := typeset.ShapedLineCapHeight(fontSpec)
	visual := typeset.VisualRunOrder(line.Runs)
	if bl, mode, ok := c.glyphMaskTarget(); ok {
		// Glyph-mask cache route (see text_glyph_cache.go). Emoji are
		// blitted first, as the Path route does (it fills the outline
		// path only after walking the whole line).
		penX := x
		for _, runIndex := range visual {
			for _, glyph := range line.Runs[runIndex].Glyphs {
				if glyph.GlyphID == gtfont.EmptyGlyph {
					drawAtomicGlyph(c, line, glyph, penX, baseline, capHeight, fontSpec, color)
				}
				penX += typeset.FixedToPx(glyph.Advance)
			}
		}
		c.drawShapedLineMasks(bl, mode, line, visual, x, baseline, color, fontSpec)
		if fontSpec.Bold {
			// Synthetic bold: second pass one logical px right, through
			// the same Translate the Path route uses.
			depth := c.Save()
			c.Translate(1, 0)
			c.drawShapedLineMasks(bl, mode, line, visual, x, baseline, color, fontSpec)
			c.RestoreTo(depth)
		}
		return
	}
	path := gfx.NewPath()
	penX := x
	for _, runIndex := range visual {
		run := &line.Runs[runIndex]
		for _, glyph := range run.Glyphs {
			if glyph.GlyphID == gtfont.EmptyGlyph {
				drawAtomicGlyph(c, line, glyph, penX, baseline, capHeight, fontSpec, color)
			} else {
				typeset.AppendGlyphOutline(path, run, glyph, penX, baseline, fontSpec)
			}
			penX += typeset.FixedToPx(glyph.Advance)
		}
	}
	if path.IsEmpty() {
		return
	}
	paint := gfx.Paint{Color: color, AntiAlias: true, FillRule: gfx.FillNonZero}
	c.DrawShape(gfx.ShapePath{Path: path}, paint)
	if fontSpec.Bold {
		depth := c.Save()
		c.Translate(1, 0)
		c.DrawShape(gfx.ShapePath{Path: path}, paint)
		c.RestoreTo(depth)
	}
}

// drawEmojiGlyph blits one emoji cluster at display resolution.
//
// The glyph's advance was measured at the logical font size, but the
// bitmap is fetched at fontSize × canvas scale so a 2× display (or a
// zoomed viewport) gets a 2× raster. Fetching at logical size and letting
// DrawImage stretch it — what this used to do — upscaled a 14px bitmap to
// 28 device pixels, which is why emoji looked soft next to the
// outline-rendered text around them.
func drawEmojiGlyph(c imageCanvas, line *typeset.ShapedLine, glyph shaping.Glyph, penX, baseline, capHeight float32, fontSpec gfx.Font) {
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
	img := typeset.LookupEmojiSequence(string(line.Text[start:end]), fontSpec.Size*s)
	if img == nil {
		return
	}
	bounds := img.Bounds()
	pw, ph := float32(bounds.Dx()), float32(bounds.Dy())
	// Logical box the raster covers: centered on the advance (rounding
	// size×scale can differ from advance×scale by a pixel) and on the
	// cap-height midline vertically, like the surrounding glyphs.
	w, h := pw/s, ph/s
	capCenter := baseline - capHeight/2
	rect := gfx.Rect{X: penX + (typeset.FixedToPx(glyph.Advance)-w)/2, Y: capCenter - h/2, W: w, H: h}
	m := top.Matrix
	if m.B != 0 || m.C != 0 || m.A <= 0 || m.D <= 0 || absf(m.A-m.D) > 1e-3 {
		// Rotated, mirrored or non-uniformly scaled: no 1:1 pixel mapping
		// exists, so let the general path resample.
		c.DrawImage(img, rect)
		return
	}
	// Uniform positive scale: snap the origin to a whole device pixel and
	// keep the bitmap's own pixel size so the blit is a straight copy.
	// A fractional origin would bilinear-sample every pixel and soften
	// the edges again.
	dev := m.TransformRect(rect)
	dev = gfx.Rect{X: float32(math.Round(float64(dev.X))), Y: float32(math.Round(float64(dev.Y))), W: pw, H: ph}
	c.Backend.DrawImage(img, dev, top.Clip)
}
