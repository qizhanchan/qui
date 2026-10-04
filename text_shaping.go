package qui

import (
	"bytes"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/segmenter"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/bidi"
)

// TextDirection controls the base direction of a paragraph. Auto resolves the
// first strong directional rune and falls back to left-to-right for neutral
// text.
type TextDirection uint8

const (
	TextDirectionAuto TextDirection = iota
	TextDirectionLTR
	TextDirectionRTL
)

type textBreak struct {
	offset    int
	mandatory bool
}

type shapedParagraph struct {
	text []rune
	// shapeText is the rune slice the glyphs were actually shaped from. It
	// differs from text only under synthesized small caps, where lowercase
	// letters shape as capitals; it is the same LENGTH, so every index into
	// text addresses the same position in it.
	shapeText  []rune
	direction  di.Direction
	runs       []shaping.Output
	graphemes  []int
	lineBreaks []textBreak
	advance    float32
}

type shapedLine struct {
	runs      []shaping.Output
	text      []rune
	shapeText []rune
	direction di.Direction
	start     int
	end       int
	advance   float32
}

// glyphText is the text a glyph's cluster should be read back as — what the
// glyph actually DRAWS, which is what a PDF's glyph→Unicode map has to agree
// with (a small-capital A is the A glyph, whatever the source rune was).
func (l *shapedLine) glyphText() []rune {
	if l.shapeText != nil {
		return l.shapeText
	}
	return l.text
}

type shapingFaceKey struct {
	font       *opentype.Font
	variations string
	ppem       uint16
}

type shapingFontmap []*gtfont.Face

func (m shapingFontmap) ResolveFace(r rune) *gtfont.Face {
	for _, face := range m {
		if _, ok := face.NominalGlyph(r); ok {
			return face
		}
	}
	return m[0]
}

var textShapingState struct {
	sync.Mutex
	shaper  shaping.HarfbuzzShaper
	seg     shaping.Segmenter
	fonts   map[*opentype.Font]*gtfont.Font
	sources map[*gtfont.Font]*opentype.Font
	faces   map[shapingFaceKey]*gtfont.Face
}

func init() {
	textShapingState.fonts = make(map[*opentype.Font]*gtfont.Font)
	textShapingState.sources = make(map[*gtfont.Font]*opentype.Font)
	textShapingState.faces = make(map[shapingFaceKey]*gtfont.Face)
	textShapingState.shaper.SetFontCacheSize(64)
}

func invalidateShapingFontCache() {
	textShapingState.Lock()
	textShapingState.fonts = make(map[*opentype.Font]*gtfont.Font)
	textShapingState.sources = make(map[*gtfont.Font]*opentype.Font)
	textShapingState.faces = make(map[shapingFaceKey]*gtfont.Face)
	textShapingState.shaper = shaping.HarfbuzzShaper{}
	textShapingState.shaper.SetFontCacheSize(64)
	textShapingState.Unlock()
}

func resolveTextDirection(text []rune, requested TextDirection) di.Direction {
	switch requested {
	case TextDirectionRTL:
		return di.DirectionRTL
	case TextDirectionLTR:
		return di.DirectionLTR
	}
	for _, r := range text {
		props, _ := bidi.LookupRune(r)
		switch props.Class() {
		case bidi.R, bidi.AL, bidi.RLE, bidi.RLO, bidi.RLI:
			return di.DirectionRTL
		case bidi.L, bidi.LRE, bidi.LRO, bidi.LRI:
			return di.DirectionLTR
		}
	}
	return di.DirectionLTR
}

// GraphemeBoundaries returns rune offsets for every extended grapheme
// boundary, including 0 and len(text).
func GraphemeBoundaries(text []rune) []int {
	boundaries := make([]int, 1, len(text)+1)
	boundaries[0] = 0
	if len(text) == 0 {
		return boundaries
	}
	var seg segmenter.Segmenter
	seg.Init(text)
	iter := seg.GraphemeIterator()
	for iter.Next() {
		g := iter.Grapheme()
		boundaries = append(boundaries, g.Offset+len(g.Text))
	}
	if boundaries[len(boundaries)-1] != len(text) {
		boundaries = append(boundaries, len(text))
	}
	return boundaries
}

func lineBreakBoundaries(text []rune) []textBreak {
	if len(text) == 0 {
		return []textBreak{{offset: 0, mandatory: true}}
	}
	var seg segmenter.Segmenter
	seg.Init(text)
	iter := seg.LineIterator()
	breaks := make([]textBreak, 0, len(text)/4+1)
	for iter.Next() {
		line := iter.Line()
		breaks = append(breaks, textBreak{
			offset:    line.Offset + len(line.Text),
			mandatory: line.IsMandatoryBreak,
		})
	}
	if len(breaks) == 0 || breaks[len(breaks)-1].offset != len(text) {
		breaks = append(breaks, textBreak{offset: len(text), mandatory: true})
	}
	return breaks
}

func nextGraphemeBoundary(text []rune, idx int) int {
	if idx < 0 {
		return 0
	}
	if idx >= len(text) {
		return len(text)
	}
	boundaries := GraphemeBoundaries(text)
	pos := sort.SearchInts(boundaries, idx+1)
	if pos >= len(boundaries) {
		return len(text)
	}
	return boundaries[pos]
}

func prevGraphemeBoundary(text []rune, idx int) int {
	if idx <= 0 {
		return 0
	}
	if idx > len(text) {
		idx = len(text)
	}
	boundaries := GraphemeBoundaries(text)
	pos := sort.SearchInts(boundaries, idx)
	if pos == 0 {
		return 0
	}
	return boundaries[pos-1]
}

func shapeParagraph(text []rune, fontSpec Font, requested TextDirection) (shapedParagraph, bool) {
	if fontSpec.Size <= 0 {
		fontSpec.Size = 14
	}
	if len(text) == 0 {
		return shapedParagraph{
			text:       []rune{},
			direction:  resolveTextDirection(nil, requested),
			graphemes:  []int{0},
			lineBreaks: []textBreak{{offset: 0, mandatory: true}},
		}, true
	}
	fontChain := fontChainForSpec(fontSpec)
	if len(fontChain) == 0 {
		return shapedParagraph{}, false
	}

	// Small caps shapes CAPITALS where the source has lowercase, so the runes
	// handed to the shaper are not the runes the caller passed in. Everything
	// that indexes text — grapheme boundaries, line breaks, cluster indices,
	// caret offsets — keeps using the ORIGINAL, which stays valid because the
	// mapping is one rune for one rune (see smallCapsShapingText).
	// capsText stays nil unless something was actually mapped, so it doubles
	// as "these glyphs are not the source runes" for everything downstream.
	var capsText []rune
	var capMask []bool
	if fontSpec.SmallCaps {
		capsText, capMask, _ = smallCapsShapingText(text)
	}
	shapeText := text
	if capsText != nil {
		shapeText = capsText
	}

	textShapingState.Lock()
	defer textShapingState.Unlock()

	faces := facesForSpecLocked(fontChain, fontSpec)
	if len(faces) == 0 {
		return shapedParagraph{}, false
	}
	smallSpec, smallFaces := fontSpec, faces
	if capMask != nil {
		smallSpec.Size = fontSpec.Size * smallCapsSizeFactor
		if smallFaces = facesForSpecLocked(fontChain, smallSpec); len(smallFaces) == 0 {
			return shapedParagraph{}, false
		}
	}

	direction := resolveTextDirection(text, requested)
	graphemes := GraphemeBoundaries(text)
	runs := make([]shaping.Output, 0, len(text)/8+2)
	// shapeRange shapes shapeText[start:end) at one size. The Segmenter owns
	// the slice it returns, so each range must be fully shaped before the
	// next Split call.
	shapeRange := func(start, end int, spec Font, faces shapingFontmap) {
		input := shaping.Input{
			Text:         shapeText,
			RunStart:     start,
			RunEnd:       end,
			Direction:    direction,
			Language:     shapingLanguage(),
			FontFeatures: shapingFeatures(spec.Features),
			Size:         fixed.Int26_6(math.Round(float64(spec.Size * 64))),
		}
		for _, segment := range textShapingState.seg.Split(input, faces) {
			runs = append(runs, shapeSegmentWithEmojiLocked(segment, graphemes, spec)...)
		}
	}
	if capMask == nil {
		shapeRange(0, len(text), fontSpec, faces)
	} else {
		// One range per run of same-cased runes: a small capital is just the
		// capital shaped at a smaller size, so the size changes at every
		// case boundary.
		for start := 0; start < len(capMask); {
			end := start + 1
			for end < len(capMask) && capMask[end] == capMask[start] {
				end++
			}
			if capMask[start] {
				shapeRange(start, end, smallSpec, smallFaces)
			} else {
				shapeRange(start, end, fontSpec, faces)
			}
			start = end
		}
	}
	shaping.AddSpacing(
		runs,
		shapeText,
		fixed.Int26_6(math.Round(float64(fontSpec.WordSpacing*64))),
		fixed.Int26_6(math.Round(float64(fontSpec.LetterSpacing*64))),
	)
	if fontSpec.LetterSpacing != 0 {
		addTrailingLetterSpacing(runs, len(text), pxToFixed(fontSpec.LetterSpacing))
	}

	var advance fixed.Int26_6
	for i := range runs {
		advance += runs[i].Advance
	}
	return shapedParagraph{
		text:       append([]rune(nil), text...),
		shapeText:  capsText,
		direction:  direction,
		runs:       runs,
		graphemes:  graphemes,
		lineBreaks: lineBreakBoundaries(text),
		advance:    float32(advance) / 64,
	}, true
}

func facesForSpecLocked(fontChain []*opentype.Font, spec Font) shapingFontmap {
	faces := make(shapingFontmap, 0, len(fontChain))
	for _, source := range fontChain {
		if face, ok := shapingFaceLocked(source, spec); ok {
			faces = append(faces, face)
		}
	}
	return faces
}

// smallCapsSizeFactor sizes a synthesized small capital against the nominal
// font size.
//
// 0.7 is MEASURED, not chosen: rendering "Ex" at 60pt in Google Docs and
// reading the cap heights off its canvas gives 115 device px for the full E
// and 81 for the small x — 0.7043. (It is also about where a face's x-height
// sits: Arial's is 0.72 of its cap height.) An earlier 0.8 guess drew small
// capitals noticeably taller than Docs does.
const smallCapsSizeFactor = 0.7

// smallCapsShapingText returns text with every lowercase rune replaced by its
// capital, plus a mask marking which positions were replaced (those are the
// ones drawn small). Reports false when nothing was case-mapped, so the
// caller can take the ordinary single-size path.
//
// unicode.ToUpper, deliberately, NOT strings.ToUpper: the latter is a full
// Unicode case mapping and expands ß to SS: one source rune would become two
// shaped runes and every cluster index — caret, hit test, selection — would
// slide by one from there on. Per-rune mapping keeps the two texts the same
// length, which is the whole reason the original text stays authoritative.
func smallCapsShapingText(text []rune) ([]rune, []bool, bool) {
	mapped, mask, any := []rune(nil), make([]bool, len(text)), false
	for i, r := range text {
		up := unicode.ToUpper(r)
		if up == r {
			continue
		}
		if !any {
			mapped = append([]rune(nil), text...)
			any = true
		}
		mapped[i] = up
		mask[i] = true
	}
	if !any {
		return nil, nil, false
	}
	return mapped, mask, true
}

func shapeSegmentWithEmojiLocked(input shaping.Input, graphemes []int, fontSpec Font) []shaping.Output {
	if input.RunStart >= input.RunEnd {
		return nil
	}
	outputs := make([]shaping.Output, 0, 2)
	plainStart := input.RunStart
	flushPlain := func(end int) {
		if end <= plainStart {
			return
		}
		part := input
		part.RunStart = plainStart
		part.RunEnd = end
		outputs = append(outputs, shapeInputLocked(part))
	}
	for i := 1; i < len(graphemes); i++ {
		start, end := graphemes[i-1], graphemes[i]
		if end <= input.RunStart || start >= input.RunEnd {
			continue
		}
		if start < input.RunStart || end > input.RunEnd || !isEmojiCluster(input.Text[start:end]) {
			continue
		}
		flushPlain(start)
		outputs = append(outputs, emojiShapingOutput(input, start, end, fontSpec))
		plainStart = end
	}
	flushPlain(input.RunEnd)
	return outputs
}

func shapeInputLocked(input shaping.Input) shaping.Output {
	output := textShapingState.shaper.Shape(input)
	requested := fixedToPx(input.Size)
	shaperSize := float32(input.Size.Ceil())
	if requested <= 0 || shaperSize <= 0 || absFloat(requested-shaperSize) < 0.0001 {
		return output
	}
	ratio := requested / shaperSize
	scaleFixed := func(value fixed.Int26_6) fixed.Int26_6 {
		return fixed.Int26_6(math.Round(float64(float32(value) * ratio)))
	}
	for i := range output.Glyphs {
		glyph := &output.Glyphs[i]
		glyph.Width = scaleFixed(glyph.Width)
		glyph.Height = scaleFixed(glyph.Height)
		glyph.XBearing = scaleFixed(glyph.XBearing)
		glyph.YBearing = scaleFixed(glyph.YBearing)
		glyph.Advance = scaleFixed(glyph.Advance)
		glyph.XAdvance = scaleFixed(glyph.XAdvance)
		glyph.YAdvance = scaleFixed(glyph.YAdvance)
		glyph.XOffset = scaleFixed(glyph.XOffset)
		glyph.YOffset = scaleFixed(glyph.YOffset)
	}
	output.Advance = scaleFixed(output.Advance)
	output.LineBounds.Ascent = scaleFixed(output.LineBounds.Ascent)
	output.LineBounds.Descent = scaleFixed(output.LineBounds.Descent)
	output.LineBounds.Gap = scaleFixed(output.LineBounds.Gap)
	output.GlyphBounds.Ascent = scaleFixed(output.GlyphBounds.Ascent)
	output.GlyphBounds.Descent = scaleFixed(output.GlyphBounds.Descent)
	output.GlyphBounds.Gap = scaleFixed(output.GlyphBounds.Gap)
	return output
}

func isEmojiCluster(cluster []rune) bool {
	for _, r := range cluster {
		if isEmojiRune(r) && !isEmojiContinuation(r) {
			return true
		}
	}
	return false
}

func emojiShapingOutput(input shaping.Input, start, end int, fontSpec Font) shaping.Output {
	advance := float32(0)
	if image := lookupEmojiSequence(string(input.Text[start:end]), fontSpec.Size); image != nil {
		advance = float32(image.Bounds().Dx())
	}
	fixedAdvance := pxToFixed(advance)
	return shaping.Output{
		Advance:   fixedAdvance,
		Size:      input.Size,
		Direction: input.Direction,
		Face:      input.Face,
		Runes:     shaping.Range{Offset: start, Count: end - start},
		Glyphs: []shaping.Glyph{{
			GlyphID:      gtfont.EmptyGlyph,
			ClusterIndex: start,
			RuneCount:    end - start,
			GlyphCount:   1,
			Advance:      fixedAdvance,
			XAdvance:     fixedAdvance,
			Width:        fixedAdvance,
			Height:       -input.Size,
		}},
	}
}

func addTrailingLetterSpacing(runs []shaping.Output, textEnd int, spacing fixed.Int26_6) {
	for runIndex := len(runs) - 1; runIndex >= 0; runIndex-- {
		run := &runs[runIndex]
		for glyphIndex := range run.Glyphs {
			glyph := &run.Glyphs[glyphIndex]
			if glyph.ClusterIndex+glyph.RuneCount != textEnd {
				continue
			}
			glyph.Advance += spacing
			if run.Direction.IsVertical() {
				glyph.YAdvance += spacing
			} else {
				glyph.XAdvance += spacing
			}
			run.RecomputeAdvance()
			return
		}
	}
}

func wrapShapedParagraph(paragraph shapedParagraph, maxWidth, firstIndent float32, breakLongWords bool) []shapedLine {
	if len(paragraph.text) == 0 {
		return []shapedLine{{direction: paragraph.direction}}
	}
	policy := shaping.Never
	if breakLongWords {
		policy = shaping.WhenNecessary
	}
	config := shaping.WrapConfig{
		Direction:     paragraph.direction,
		BreakPolicy:   policy,
		TextContinues: false,
	}
	var wrapper shaping.LineWrapper
	wrapper.Prepare(config, paragraph.text, shaping.NewSliceIterator(paragraph.runs))

	if maxWidth <= 0 {
		maxWidth = float32(math.MaxInt32>>7) / 64
	}
	lines := make([]shapedLine, 0, 4)
	start := 0
	for lineIndex := 0; ; lineIndex++ {
		lineWidth := maxWidth
		if lineIndex == 0 && firstIndent > 0 {
			lineWidth -= firstIndent
			if lineWidth < 1 {
				lineWidth = 1
			}
		}
		wrapped, done := wrapper.WrapNextLineF(pxToFixed(lineWidth))
		if wrapped.Line != nil {
			lines = append(lines, makeShapedLine(wrapped.Line, paragraph.text, paragraph.shapeText,
				paragraph.direction, start, wrapped.NextLine))
		}
		start = wrapped.NextLine
		if done {
			break
		}
	}
	if len(lines) == 0 {
		lines = append(lines, shapedLine{direction: paragraph.direction})
	}
	return lines
}

func makeShapedLine(runs shaping.Line, text, shapeText []rune, direction di.Direction, start, end int) shapedLine {
	cloned := make([]shaping.Output, len(runs))
	var advance fixed.Int26_6
	for i := range runs {
		cloned[i] = runs[i]
		cloned[i].Glyphs = append([]shaping.Glyph(nil), runs[i].Glyphs...)
		advance += cloned[i].Advance
	}
	return shapedLine{
		runs:      cloned,
		text:      text,
		shapeText: shapeText,
		direction: direction,
		start:     start,
		end:       end,
		advance:   fixedToPx(advance),
	}
}

func shapeSingleLine(text []rune, fontSpec Font, direction TextDirection) (*shapedLine, bool) {
	paragraph, ok := shapeParagraph(text, fontSpec, direction)
	if !ok {
		return nil, false
	}
	lines := wrapShapedParagraph(paragraph, 0, 0, false)
	if len(lines) == 0 {
		return &shapedLine{direction: paragraph.direction}, true
	}
	return &lines[0], true
}

func pxToFixed(value float32) fixed.Int26_6 {
	return fixed.Int26_6(math.Round(float64(value * 64)))
}

func fixedToPx(value fixed.Int26_6) float32 {
	return float32(value) / 64
}

func shapingFaceLocked(source *opentype.Font, spec Font) (*gtfont.Face, bool) {
	ppem := uint16(math.Ceil(float64(spec.Size)))
	key := shapingFaceKey{
		font:       source,
		variations: canonicalVariationKey(spec.Variations),
		ppem:       ppem,
	}
	if face, ok := textShapingState.faces[key]; ok {
		return face, true
	}
	parsed := textShapingState.fonts[source]
	if parsed == nil {
		raw := rawFontBytesFor(source)
		if len(raw) == 0 {
			return nil, false
		}
		faces, err := gtfont.ParseTTC(bytes.NewReader(raw))
		if err != nil || len(faces) == 0 {
			return nil, false
		}
		parsed = faces[0].Font
		textShapingState.fonts[source] = parsed
		textShapingState.sources[parsed] = source
	}
	face := gtfont.NewFace(parsed)
	face.SetPpem(ppem, ppem)
	face.SetVariations(shapingVariations(spec.Variations))
	textShapingState.faces[key] = face
	return face, true
}

func sourceFontForShapingFace(face *gtfont.Face) *opentype.Font {
	if face == nil {
		return nil
	}
	textShapingState.Lock()
	source := textShapingState.sources[face.Font]
	textShapingState.Unlock()
	return source
}

func shapingFeatures(features []string) []shaping.FontFeature {
	if len(features) == 0 {
		return nil
	}
	out := make([]shaping.FontFeature, 0, len(features))
	for _, raw := range features {
		tag, value, ok := parseShapingSetting(raw, 1)
		if !ok || value < 0 {
			continue
		}
		out = append(out, shaping.FontFeature{Tag: tag, Value: uint32(value)})
	}
	return out
}

func shapingVariations(variations map[string]float32) []gtfont.Variation {
	if len(variations) == 0 {
		return nil
	}
	keys := make([]string, 0, len(variations))
	for key := range variations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]gtfont.Variation, 0, len(keys))
	for _, key := range keys {
		if len(key) != 4 {
			continue
		}
		out = append(out, gtfont.Variation{
			Tag:   gtot.NewTag(key[0], key[1], key[2], key[3]),
			Value: variations[key],
		})
	}
	return out
}

func parseShapingSetting(raw string, defaultValue int64) (gtot.Tag, int64, bool) {
	raw = strings.TrimSpace(raw)
	value := defaultValue
	if strings.HasPrefix(raw, "-") {
		value = 0
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "-"))
	}
	if idx := strings.IndexByte(raw, '='); idx >= 0 {
		parsed, err := strconv.ParseInt(strings.TrimSpace(raw[idx+1:]), 10, 32)
		if err != nil {
			return 0, 0, false
		}
		value = parsed
		raw = strings.TrimSpace(raw[:idx])
	}
	if len(raw) != 4 {
		return 0, 0, false
	}
	return gtot.NewTag(raw[0], raw[1], raw[2], raw[3]), value, true
}
