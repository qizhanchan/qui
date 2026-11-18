package typeset

import (
	gtfont "github.com/go-text/typesetting/font"

	"github.com/qizhanchan/qui/internal/gfx"
)

// TextOutline shapes text as one line and returns its glyph outlines with
// the pen starting at (x, baseline), plus the line's advance. Glyphs with
// no outline (color emoji, bitmap faces) are skipped, keeping their
// advance. A web canvas fills and strokes text through this, so text
// takes the same transforms, gradients and compositing as any path.
func TextOutline(text string, spec gfx.Font, x, baseline float32) (*gfx.Path, float32) {
	path := gfx.NewPath()
	line, ok := ShapeSingleLine([]rune(text), spec, TextDirectionAuto)
	if !ok {
		return path, 0
	}
	penX := x
	for _, ri := range VisualRunOrder(line.Runs) {
		run := &line.Runs[ri]
		for _, g := range run.Glyphs {
			if g.GlyphID != gtfont.EmptyGlyph {
				AppendGlyphOutline(path, run, g, penX, baseline, spec)
			}
			penX += FixedToPx(g.Advance)
		}
	}
	return path, penX - x
}

// FontVerticalMetrics is the face's ascent and descent in pixels (both
// positive) at spec's size.
func FontVerticalMetrics(spec gfx.Font) (ascent, descent float32) {
	m := FontMetricsFor(spec)
	a, d := FixedToPx(m.Ascent), FixedToPx(m.Descent)
	if d < 0 {
		d = -d
	}
	return a, d
}
