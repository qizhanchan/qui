package qui

import (
	"math"
	"sort"
	"unicode"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/opentype"
)

type styledTextRange struct {
	start, end int
	style      styledRune
}

type styledShapedOutput struct {
	output shaping.Output
	style  int
}

type styledShapedParagraph struct {
	text []rune
	// shapeText is non-nil only when some span asked for small caps: the
	// glyphs then come from capitals the source text does not contain. Same
	// length as text (see smallCapsShapingText), so every offset — style
	// ranges, cluster indices, run boundaries — addresses both.
	shapeText []rune
	direction di.Direction
	styles    []styledTextRange
	runs      []styledShapedOutput
}

func layoutStyledRunesShaped(stream []styledRune, baseFont Font, opts TextLayoutOptions) ([]RichTextLine, bool) {
	if len(stream) == 0 {
		return nil, true
	}
	lines := make([]RichTextLine, 0, 8)
	paragraph := make([]styledRune, 0, len(stream))
	flush := func() bool {
		shaped, ok := shapeStyledParagraph(paragraph, opts.Direction)
		if !ok {
			return false
		}
		wrapped := wrapStyledParagraph(shaped, opts, len(lines) == 0)
		lines = append(lines, richLinesFromStyled(shaped, wrapped, baseFont, opts.LineHeightScale)...)
		paragraph = paragraph[:0]
		return true
	}
	for _, styled := range stream {
		switch styled.r {
		case '\r':
			continue
		case '\n':
			if !flush() {
				return nil, false
			}
		default:
			paragraph = append(paragraph, styled)
		}
	}
	if !flush() {
		return nil, false
	}
	return lines, true
}

func shapeStyledParagraph(stream []styledRune, requested TextDirection) (styledShapedParagraph, bool) {
	text := make([]rune, len(stream))
	for i := range stream {
		text[i] = stream[i].r
	}
	// Small caps is resolved into the stream: a lowercase rune under it
	// becomes a capital drawn at a reduced size. Doing it here rather than in
	// the shaper means the size change splits style and shaping ranges by
	// itself, so the smaller run also DRAWS smaller — text keeps the source's
	// own case, which is what selection and copy read back.
	shapeStream, shapeText := resolveStyledSmallCaps(stream, text)
	styleStream := stream
	if shapeStream != nil {
		styleStream = shapeStream
	}
	styles := groupStyledRanges(styleStream)
	shapingRanges := groupShapingRanges(styleStream)
	if len(text) == 0 {
		return styledShapedParagraph{text: text, direction: resolveTextDirection(text, requested), styles: styles}, true
	}

	chains := make([][]*opentype.Font, len(shapingRanges))
	for i := range shapingRanges {
		chains[i] = fontChainForSpec(shapingRanges[i].style.font)
		if len(chains[i]) == 0 {
			return styledShapedParagraph{}, false
		}
	}

	textShapingState.Lock()
	defer textShapingState.Unlock()
	faceMaps := make([]shapingFontmap, len(shapingRanges))
	allFaces := make(shapingFontmap, 0, len(shapingRanges))
	for i := range shapingRanges {
		for _, source := range chains[i] {
			face, ok := shapingFaceLocked(source, shapingRanges[i].style.font)
			if ok {
				faceMaps[i] = append(faceMaps[i], face)
				allFaces = append(allFaces, face)
			}
		}
		if len(faceMaps[i]) == 0 {
			return styledShapedParagraph{}, false
		}
	}

	direction := resolveTextDirection(text, requested)
	base := styles[0].style.font
	if base.Size <= 0 {
		base.Size = 14
	}
	input := shaping.Input{
		Text:         shapeText,
		RunEnd:       len(shapeText),
		Language:     shapingLanguage(),
		Direction:    direction,
		FontFeatures: shapingFeatures(base.Features),
		Size:         pxToFixed(base.Size),
	}
	segments := textShapingState.seg.Split(input, allFaces)
	graphemes := GraphemeBoundaries(text)
	outputs := make([]styledShapedOutput, 0, len(segments)+len(styles))
	for _, segment := range segments {
		for shapingIndex, style := range shapingRanges {
			start := maxIntValue(segment.RunStart, style.start)
			end := minIntValue(segment.RunEnd, style.end)
			if start >= end {
				continue
			}
			fontSpec := style.style.font
			if fontSpec.Size <= 0 {
				fontSpec.Size = 14
			}
			part := segment
			part.RunStart = start
			part.RunEnd = end
			part.FontFeatures = shapingFeatures(fontSpec.Features)
			part.Size = pxToFixed(fontSpec.Size)
			faceParts := shaping.SplitByFontGlyphs(part, faceMaps[shapingIndex])
			for _, facePart := range faceParts {
				for _, output := range shapeSegmentWithEmojiLocked(facePart, graphemes, fontSpec) {
					applyStyledSpacing(&output, shapeText, fontSpec)
					outputs = append(outputs, splitStyledOutput(output, styles)...)
				}
			}
		}
	}
	sort.SliceStable(outputs, func(i, j int) bool {
		return outputs[i].output.Runes.Offset < outputs[j].output.Runes.Offset
	})
	shaped := styledShapedParagraph{text: text, direction: direction, styles: styles, runs: outputs}
	if shapeStream != nil {
		shaped.shapeText = shapeText
	}
	return shaped, true
}

// resolveStyledSmallCaps rewrites the runes and fonts a small-caps span
// shapes with: the capital in place of the lowercase letter, at
// smallCapsSizeFactor of the span's size. Returns nil, text when no span
// asked for it — the ordinary path.
func resolveStyledSmallCaps(stream []styledRune, text []rune) ([]styledRune, []rune) {
	any := false
	for i := range stream {
		if stream[i].font.SmallCaps {
			any = true
			break
		}
	}
	if !any {
		return nil, text
	}
	out := append([]styledRune(nil), stream...)
	shapeText := append([]rune(nil), text...)
	for i := range out {
		if !out[i].font.SmallCaps {
			continue
		}
		out[i].font.SmallCaps = false
		up := unicode.ToUpper(out[i].r)
		if up == out[i].r {
			continue
		}
		out[i].r = up
		shapeText[i] = up
		if out[i].font.Size <= 0 {
			out[i].font.Size = 14
		}
		out[i].font.Size *= smallCapsSizeFactor
	}
	return out, shapeText
}

func groupStyledRanges(stream []styledRune) []styledTextRange {
	if len(stream) == 0 {
		return nil
	}
	ranges := make([]styledTextRange, 0, 8)
	start := 0
	for i := 1; i <= len(stream); i++ {
		if i < len(stream) && styledRuneEqual(stream[start], stream[i]) {
			continue
		}
		ranges = append(ranges, styledTextRange{start: start, end: i, style: stream[start]})
		start = i
	}
	return ranges
}

func groupShapingRanges(stream []styledRune) []styledTextRange {
	if len(stream) == 0 {
		return nil
	}
	ranges := make([]styledTextRange, 0, 8)
	start := 0
	for i := 1; i <= len(stream); i++ {
		if i < len(stream) && fontShapingEqual(stream[start].font, stream[i].font) {
			continue
		}
		ranges = append(ranges, styledTextRange{start: start, end: i, style: stream[start]})
		start = i
	}
	return ranges
}

func fontShapingEqual(a, b Font) bool {
	return fontStyleEqual(a, b) && a.LetterSpacing == b.LetterSpacing && a.WordSpacing == b.WordSpacing
}

func splitStyledOutput(output shaping.Output, styles []styledTextRange) []styledShapedOutput {
	if len(output.Glyphs) == 0 {
		return nil
	}
	type glyphGroup struct {
		style      int
		start, end int
		glyphs     []shaping.Glyph
	}
	groups := make(map[int]*glyphGroup, len(styles))
	for _, glyph := range output.Glyphs {
		styleIndex := styleIndexForOffset(styles, glyph.ClusterIndex)
		group := groups[styleIndex]
		if group == nil {
			group = &glyphGroup{style: styleIndex, start: glyph.ClusterIndex, end: glyph.ClusterIndex + glyph.RuneCount}
			groups[styleIndex] = group
		}
		if glyph.ClusterIndex < group.start {
			group.start = glyph.ClusterIndex
		}
		if end := glyph.ClusterIndex + glyph.RuneCount; end > group.end {
			group.end = end
		}
		group.glyphs = append(group.glyphs, glyph)
	}
	ordered := make([]*glyphGroup, 0, len(groups))
	for _, group := range groups {
		ordered = append(ordered, group)
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].start < ordered[j].start })
	out := make([]styledShapedOutput, 0, len(ordered))
	for _, group := range ordered {
		part := output
		part.Glyphs = group.glyphs
		part.Runes = shaping.Range{Offset: group.start, Count: group.end - group.start}
		part.RecalculateAll()
		out = append(out, styledShapedOutput{output: part, style: group.style})
	}
	return out
}

func styledRuneEqual(a, b styledRune) bool {
	return fontStyleEqual(a.font, b.font) && a.color == b.color && a.deco == b.deco &&
		a.decoPaint == b.decoPaint && a.href == b.href
}

func applyStyledSpacing(output *shaping.Output, text []rune, fontSpec Font) {
	if fontSpec.WordSpacing != 0 {
		output.AddWordSpacing(text, pxToFixed(fontSpec.WordSpacing))
	}
	if fontSpec.LetterSpacing != 0 {
		output.AddLetterSpacing(pxToFixed(fontSpec.LetterSpacing), true, true)
		for i := range output.Glyphs {
			glyph := &output.Glyphs[i]
			if glyph.ClusterIndex+glyph.RuneCount == output.Runes.Offset+output.Runes.Count {
				glyph.Advance += pxToFixed(fontSpec.LetterSpacing)
				glyph.XAdvance += pxToFixed(fontSpec.LetterSpacing)
				break
			}
		}
		output.RecomputeAdvance()
	}
}

func wrapStyledParagraph(paragraph styledShapedParagraph, opts TextLayoutOptions, firstParagraph bool) []shaping.Line {
	if len(paragraph.text) == 0 || len(paragraph.runs) == 0 {
		return []shaping.Line{{}}
	}
	outputs := make([]shaping.Output, len(paragraph.runs))
	for i := range paragraph.runs {
		outputs[i] = paragraph.runs[i].output
	}
	policy := shaping.Never
	if opts.BreakLongWords {
		policy = shaping.WhenNecessary
	}
	config := shaping.WrapConfig{Direction: paragraph.direction, BreakPolicy: policy}
	var wrapper shaping.LineWrapper
	wrapper.Prepare(config, paragraph.text, shaping.NewSliceIterator(outputs))
	maxWidth := opts.MaxWidth
	if !opts.Wrap || maxWidth <= 0 {
		maxWidth = float32(math.MaxInt32>>7) / 64
	}
	lines := make([]shaping.Line, 0, 4)
	for lineIndex := 0; ; lineIndex++ {
		lineWidth := maxWidth
		if firstParagraph && lineIndex == 0 && opts.FirstIndent > 0 {
			lineWidth -= opts.FirstIndent
			if lineWidth < 1 {
				lineWidth = 1
			}
		}
		wrapped, done := wrapper.WrapNextLineF(pxToFixed(lineWidth))
		if wrapped.Line != nil {
			line := make(shaping.Line, len(wrapped.Line))
			for i := range wrapped.Line {
				line[i] = wrapped.Line[i]
				line[i].Glyphs = append([]shaping.Glyph(nil), wrapped.Line[i].Glyphs...)
			}
			lines = append(lines, line)
		}
		if done {
			break
		}
	}
	return lines
}

func richLinesFromStyled(paragraph styledShapedParagraph, wrapped []shaping.Line, baseFont Font, lineHeightScale float32) []RichTextLine {
	lines := make([]RichTextLine, 0, len(wrapped))
	for _, shapedRuns := range wrapped {
		line := RichTextLine{LineHeight: scaledLineHeight(baseFont, lineHeightScale)}
		if paragraph.direction == di.DirectionRTL {
			line.direction = TextDirectionRTL
		} else {
			line.direction = TextDirectionLTR
		}
		visual := visualRunOrder(shapedRuns)
		for _, runIndex := range visual {
			output := shapedRuns[runIndex]
			styleIndex := styleIndexForOffset(paragraph.styles, output.Runes.Offset)
			style := paragraph.styles[styleIndex].style
			shaped := makeShapedLine(shaping.Line{output}, paragraph.text, paragraph.shapeText,
				paragraph.direction, output.Runes.Offset, output.Runes.Offset+output.Runes.Count)
			start, end := output.Runes.Offset, output.Runes.Offset+output.Runes.Count
			line.Runs = append(line.Runs, RichTextRun{
				Text:            string(paragraph.text[start:end]),
				Font:            style.font,
				Color:           style.color,
				Width:           fixedToPx(output.Advance),
				Decoration:      style.deco,
				DecorationPaint: style.decoPaint,
				Href:            style.href,
				shaped:          &shaped,
			})
			line.Width += fixedToPx(output.Advance)
			if height := scaledLineHeight(style.font, lineHeightScale); height > line.LineHeight {
				line.LineHeight = height
			}
		}
		lines = append(lines, line)
	}
	return lines
}

func styleIndexForOffset(styles []styledTextRange, offset int) int {
	for i := range styles {
		if offset >= styles[i].start && offset < styles[i].end {
			return i
		}
	}
	if len(styles) == 0 {
		return 0
	}
	return len(styles) - 1
}

func maxIntValue(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minIntValue(a, b int) int {
	if a < b {
		return a
	}
	return b
}
