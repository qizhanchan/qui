package qui

import (
	"sort"

	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"
)

type shapedLineCanvas interface {
	drawShapedLine(line *shapedLine, rect Rect, color Color, fontSpec Font)
}

func (c imageCanvas) drawShapedLine(line *shapedLine, rect Rect, color Color, fontSpec Font) {
	if line == nil || len(line.runs) == 0 || c.backend == nil {
		return
	}
	metrics := GetFontFaceFor(fontSpec).Metrics()
	capHeight := float32(metrics.CapHeight.Ceil())
	if capHeight <= 0 {
		capHeight = float32(metrics.Ascent.Ceil() - metrics.Descent.Ceil())
	}
	baseline := rect.Y + (rect.H+capHeight)/2
	c.drawShapedLineAtBaseline(line, rect.X, baseline, color, fontSpec)
}

func (c imageCanvas) drawShapedLineAtBaseline(line *shapedLine, x, baseline float32, color Color, fontSpec Font) {
	path := NewPath()
	penX := x
	metrics := GetFontFaceFor(fontSpec).Metrics()
	capHeight := float32(metrics.CapHeight.Ceil())
	if capHeight <= 0 {
		capHeight = float32(metrics.Ascent.Ceil() - metrics.Descent.Ceil())
	}
	visual := visualRunOrder(line.runs)
	for _, runIndex := range visual {
		run := &line.runs[runIndex]
		for _, glyph := range run.Glyphs {
			if glyph.GlyphID == gtfont.EmptyGlyph {
				drawEmojiGlyph(c, line, glyph, penX, baseline, capHeight, fontSpec)
			} else {
				appendGlyphOutline(path, run, glyph, penX, baseline, fontSpec)
			}
			penX += fixedToPx(glyph.Advance)
		}
	}
	if path.IsEmpty() {
		return
	}
	paint := Paint{Color: color, AntiAlias: true, FillRule: FillNonZero}
	c.DrawShape(ShapePath{Path: path}, paint)
	if fontSpec.Bold {
		depth := c.Save()
		c.Translate(1, 0)
		c.DrawShape(ShapePath{Path: path}, paint)
		c.RestoreTo(depth)
	}
}

func drawEmojiGlyph(c imageCanvas, line *shapedLine, glyph shaping.Glyph, penX, baseline, capHeight float32, fontSpec Font) {
	start := glyph.ClusterIndex
	end := start + glyph.RuneCount
	if start < 0 || end > len(line.text) || start >= end {
		return
	}
	img := lookupEmojiSequence(string(line.text[start:end]), fontSpec.Size)
	if img == nil {
		return
	}
	bounds := img.Bounds()
	w, h := float32(bounds.Dx()), float32(bounds.Dy())
	capCenter := baseline - capHeight/2
	c.DrawImage(img, Rect{X: penX, Y: capCenter - h/2, W: w, H: h})
}

func visualRunOrder(runs []shaping.Output) []int {
	order := make([]int, len(runs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return runs[order[i]].VisualIndex < runs[order[j]].VisualIndex
	})
	return order
}

func appendGlyphOutline(path *Path, run *shaping.Output, glyph shaping.Glyph, penX, baseline float32, fontSpec Font) {
	data := run.Face.GlyphData(glyph.GlyphID)
	outline, ok := data.(gtfont.GlyphOutline)
	if !ok {
		return
	}
	scale := fixedToPx(run.Size) / float32(run.Face.Upem())
	originX := penX + fixedToPx(glyph.XOffset)
	originY := baseline - fixedToPx(glyph.YOffset)
	point := func(p gtot.SegmentPoint) (float32, float32) {
		x := originX + p.X*scale
		y := originY - p.Y*scale
		if fontSpec.Italic {
			x += p.Y * scale * italicFallbackShearFactor
		}
		return x, y
	}
	for _, segment := range outline.Segments {
		switch segment.Op {
		case gtot.SegmentOpMoveTo:
			x, y := point(segment.Args[0])
			path.MoveTo(x, y)
		case gtot.SegmentOpLineTo:
			x, y := point(segment.Args[0])
			path.LineTo(x, y)
		case gtot.SegmentOpQuadTo:
			cx, cy := point(segment.Args[0])
			x, y := point(segment.Args[1])
			path.QuadTo(cx, cy, x, y)
		case gtot.SegmentOpCubeTo:
			c1x, c1y := point(segment.Args[0])
			c2x, c2y := point(segment.Args[1])
			x, y := point(segment.Args[2])
			path.CubicTo(c1x, c1y, c2x, c2y, x, y)
		}
	}
}
