package qui

import (
	"math"
	"sort"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/shaping"
)

// TextCaretStop is one visual caret position expressed as a rune offset and
// an X advance from the left edge of the shaped text.
type TextCaretStop struct {
	Offset int
	X      float32
}

// TextSelectionSegment is one contiguous visual portion of a logical text
// selection. Bidirectional selections may produce several segments.
type TextSelectionSegment struct {
	X     float32
	Width float32
}

type textCaretCell struct {
	start, end int
	x0, x1     float32
}

// TextCaretStops returns visual caret positions in left-to-right screen
// order. Offsets remain rune offsets into text.
func TextCaretStops(text string, fontSpec Font, direction TextDirection) []TextCaretStop {
	stops, _ := textCaretGeometry([]rune(text), fontSpec, direction)
	return stops
}

// TextXForOffset maps a rune offset to its shaped visual X position.
func TextXForOffset(text string, fontSpec Font, direction TextDirection, offset int) float32 {
	stops, _ := textCaretGeometry([]rune(text), fontSpec, direction)
	if len(stops) == 0 {
		return 0
	}
	best := stops[0]
	bestDistance := caretAbsInt(best.Offset - offset)
	for _, stop := range stops {
		if stop.Offset == offset {
			return stop.X
		}
		if distance := caretAbsInt(stop.Offset - offset); distance < bestDistance {
			best, bestDistance = stop, distance
		}
	}
	return best.X
}

// TextOffsetAtX maps an X advance to the nearest grapheme-safe rune offset.
func TextOffsetAtX(text string, fontSpec Font, direction TextDirection, x float32) int {
	stops, _ := textCaretGeometry([]rune(text), fontSpec, direction)
	if len(stops) == 0 {
		return 0
	}
	best := stops[0]
	bestDistance := absFloat(best.X - x)
	for _, stop := range stops[1:] {
		if distance := absFloat(stop.X - x); distance < bestDistance {
			best, bestDistance = stop, distance
		}
	}
	return best.Offset
}

// TextVisualNeighbor moves one visual caret stop left (-1) or right (+1).
func TextVisualNeighbor(text string, fontSpec Font, direction TextDirection, offset, delta int) int {
	stops, _ := textCaretGeometry([]rune(text), fontSpec, direction)
	if len(stops) == 0 || delta == 0 {
		return offset
	}
	index := 0
	for i := range stops {
		if stops[i].Offset == offset {
			index = i
			break
		}
	}
	if delta < 0 {
		index--
	} else {
		index++
	}
	if index < 0 {
		index = 0
	}
	if index >= len(stops) {
		index = len(stops) - 1
	}
	return stops[index].Offset
}

// TextSelectionSegments maps a logical rune range to visual horizontal
// segments. The result is ordered from left to right.
func TextSelectionSegments(text string, fontSpec Font, direction TextDirection, start, end int) []TextSelectionSegment {
	if start > end {
		start, end = end, start
	}
	_, cells := textCaretGeometry([]rune(text), fontSpec, direction)
	segments := make([]TextSelectionSegment, 0, 2)
	for _, cell := range cells {
		if cell.end <= start || cell.start >= end {
			continue
		}
		x0, x1 := cell.x0, cell.x1
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		if len(segments) > 0 {
			last := &segments[len(segments)-1]
			if absFloat(last.X+last.Width-x0) < 0.01 {
				last.Width = x1 - last.X
				continue
			}
		}
		segments = append(segments, TextSelectionSegment{X: x0, Width: x1 - x0})
	}
	return segments
}

func textCaretGeometry(text []rune, fontSpec Font, direction TextDirection) ([]TextCaretStop, []textCaretCell) {
	if len(text) == 0 {
		return []TextCaretStop{{}}, nil
	}
	line, ok := shapeSingleLine(text, fontSpec, direction)
	if !ok {
		return fallbackCaretGeometry(text, fontSpec)
	}
	graphemes := GraphemeBoundaries(text)
	stops := make([]TextCaretStop, 0, len(graphemes)+2)
	cells := make([]textCaretCell, 0, len(graphemes)-1)
	pen := float32(0)
	for _, runIndex := range visualRunOrder(line.runs) {
		run := &line.runs[runIndex]
		for glyphIndex := 0; glyphIndex < len(run.Glyphs); {
			glyph := run.Glyphs[glyphIndex]
			cluster := glyph.ClusterIndex
			clusterEnd := cluster + glyph.RuneCount
			clusterAdvance := float32(0)
			clusterGlyphs := 0
			for glyphIndex < len(run.Glyphs) && run.Glyphs[glyphIndex].ClusterIndex == cluster {
				clusterAdvance += fixedToPx(run.Glyphs[glyphIndex].Advance)
				clusterGlyphs++
				glyphIndex++
			}
			bounds := graphemeRange(graphemes, cluster, clusterEnd)
			if len(bounds) < 2 {
				bounds = []int{cluster, clusterEnd}
			}
			positions := clusterCaretPositions(run, glyph, bounds, clusterGlyphs, clusterAdvance)
			for i, off := range bounds {
				x := pen + positions[i]
				stops = append(stops, TextCaretStop{Offset: off, X: x})
				if i > 0 {
					prevX := pen + positions[i-1]
					cells = append(cells, textCaretCell{start: bounds[i-1], end: off, x0: prevX, x1: x})
				}
			}
			pen += clusterAdvance
		}
	}
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].X < stops[j].X })
	sort.SliceStable(cells, func(i, j int) bool { return minFloat(cells[i].x0, cells[i].x1) < minFloat(cells[j].x0, cells[j].x1) })
	stops = filterGraphemeCaretStops(dedupeCaretStops(stops), graphemes)
	cells = cellsFromCaretStops(stops, graphemes)
	return stops, cells
}

func clusterCaretPositions(run *shaping.Output, glyph shaping.Glyph, boundaries []int, glyphCount int, advance float32) []float32 {
	positions := make([]float32, len(boundaries))
	for i := range positions {
		t := float32(i) / float32(len(positions)-1)
		positions[i] = advance * t
		if run.Direction == di.DirectionRTL {
			positions[i] = advance * (1 - t)
		}
	}
	if glyphCount != 1 || len(boundaries) <= 2 || glyph.GlyphID == 0 {
		return positions
	}
	hbFont := harfbuzz.NewFont(run.Face)
	carets := hbFont.GetOTLigatureCarets(run.Direction.Harfbuzz(), glyph.GlyphID)
	if len(carets) != len(boundaries)-2 {
		return positions
	}
	scale := fixedToPx(run.Size) / float32(run.Face.Upem())
	values := make([]float32, len(carets))
	for i, caret := range carets {
		values[i] = float32(caret) * scale
		if values[i] <= 0 || values[i] >= advance {
			return positions
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if run.Direction == di.DirectionRTL {
		positions[0] = advance
		for i := range values {
			positions[i+1] = values[len(values)-1-i]
		}
		positions[len(positions)-1] = 0
		return positions
	}
	positions[0] = 0
	copy(positions[1:], values)
	positions[len(positions)-1] = advance
	return positions
}

func fallbackCaretGeometry(text []rune, fontSpec Font) ([]TextCaretStop, []textCaretCell) {
	boundaries := GraphemeBoundaries(text)
	stops := make([]TextCaretStop, 0, len(boundaries))
	cells := make([]textCaretCell, 0, len(boundaries)-1)
	x := float32(0)
	stops = append(stops, TextCaretStop{Offset: 0})
	for i := 1; i < len(boundaries); i++ {
		start, end := boundaries[i-1], boundaries[i]
		next := x + measureRunes(text[start:end], fontSpec, nil)
		cells = append(cells, textCaretCell{start: start, end: end, x0: x, x1: next})
		x = next
		stops = append(stops, TextCaretStop{Offset: end, X: x})
	}
	return stops, cells
}

func graphemeRange(boundaries []int, start, end int) []int {
	out := make([]int, 0, 4)
	for _, boundary := range boundaries {
		if boundary >= start && boundary <= end {
			out = append(out, boundary)
		}
	}
	return out
}

func dedupeCaretStops(stops []TextCaretStop) []TextCaretStop {
	out := stops[:0]
	for _, stop := range stops {
		if len(out) > 0 && out[len(out)-1].Offset == stop.Offset && absFloat(out[len(out)-1].X-stop.X) < 0.01 {
			continue
		}
		out = append(out, stop)
	}
	return out
}

func filterGraphemeCaretStops(stops []TextCaretStop, boundaries []int) []TextCaretStop {
	allowed := make(map[int]struct{}, len(boundaries))
	for _, boundary := range boundaries {
		allowed[boundary] = struct{}{}
	}
	out := stops[:0]
	for _, stop := range stops {
		if _, ok := allowed[stop.Offset]; ok {
			out = append(out, stop)
		}
	}
	return out
}

func cellsFromCaretStops(stops []TextCaretStop, boundaries []int) []textCaretCell {
	if len(boundaries) < 2 {
		return nil
	}
	xFor := func(offset int) float32 {
		for _, stop := range stops {
			if stop.Offset == offset {
				return stop.X
			}
		}
		return 0
	}
	cells := make([]textCaretCell, 0, len(boundaries)-1)
	for i := 1; i < len(boundaries); i++ {
		cells = append(cells, textCaretCell{
			start: boundaries[i-1],
			end:   boundaries[i],
			x0:    xFor(boundaries[i-1]),
			x1:    xFor(boundaries[i]),
		})
	}
	sort.SliceStable(cells, func(i, j int) bool {
		return minFloat(cells[i].x0, cells[i].x1) < minFloat(cells[j].x0, cells[j].x1)
	})
	return cells
}

func caretAbsInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func absFloat(value float32) float32 {
	return float32(math.Abs(float64(value)))
}

func minFloat(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
