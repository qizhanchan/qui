package typeset

import (
	"sort"

	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/shaping"

	"github.com/qizhanchan/qui/internal/gfx"
)

type shapedLineCanvas interface {
	DrawShapedLine(line *ShapedLine, rect gfx.Rect, color gfx.Color, fontSpec gfx.Font)
}

func ShapedLineCapHeight(fontSpec gfx.Font) float32 {
	metrics := FontMetricsFor(fontSpec)
	capHeight := float32(metrics.CapHeight.Ceil())
	if capHeight <= 0 {
		capHeight = float32(metrics.Ascent.Ceil() - metrics.Descent.Ceil())
	}
	return capHeight
}

func VisualRunOrder(runs []shaping.Output) []int {
	order := make([]int, len(runs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return runs[order[i]].VisualIndex < runs[order[j]].VisualIndex
	})
	return order
}

func AppendGlyphOutline(path *gfx.Path, run *shaping.Output, glyph shaping.Glyph, penX, baseline float32, fontSpec gfx.Font) {
	data := run.Face.GlyphData(glyph.GlyphID)
	outline, ok := data.(gtfont.GlyphOutline)
	if !ok {
		return
	}
	scale := FixedToPx(run.Size) / float32(run.Face.Upem())
	originX := penX + FixedToPx(glyph.XOffset)
	originY := baseline - FixedToPx(glyph.YOffset)
	point := func(p gtot.SegmentPoint) (float32, float32) {
		x := originX + p.X*scale
		y := originY - p.Y*scale
		if fontSpec.Italic {
			x += p.Y * scale * ItalicFallbackShearFactor
		}
		return x, y
	}
	// Font contours are implicitly closed, while flatten/fillPolygon do
	// not auto-close a subpath. Close each contour explicitly so one whose
	// last segment stops short of its start can never leave an unbalanced
	// edge crossing (non-zero fill leaking sideways along a subscan).
	open := false
	for _, segment := range outline.Segments {
		switch segment.Op {
		case gtot.SegmentOpMoveTo:
			if open {
				path.Close()
			}
			open = true
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
	if open {
		path.Close()
	}
}
