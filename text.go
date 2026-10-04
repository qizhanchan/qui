package qui

import (
	"math"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	"golang.org/x/image/font"
)

// Text measurement helpers. These live in root because both the
// widgets subpackage (Label / Button / Input / TextArea / …) and
// the in-root tooltipView need to measure text for layout. Moving
// them out of widgets_basic.go kept widgets importable without the
// subpackage circling back into root-only helpers.

const (
	baseTextWidth  = 7
	baseTextHeight = 13
	// Renderer fallback italic draws by shearing each line to the right.
	// Keep text measurement/layout in sync with that paint-time geometry.
	italicFallbackShearFactor = 0.22
)

// CSSPixelsPerInch is the resolution the engine's LOGICAL pixel is defined
// against — the CSS reference, not the display's. Physical DPI is the
// backend's business (see the window scale factor); everything above the
// rasterizer measures in these.
const CSSPixelsPerInch = 96

// Points converts a typographic point size to logical pixels, and ToPoints
// inverts it.
//
// Font.Size is PIXELS. Every place a human or a file states a font size it
// is POINTS instead: a word processor's size box, DOCX's `w:sz` (half
// points), CSS's `pt` unit, Docs' clipboard `font-size:10pt`. The two differ
// by exactly 4/3, which is small enough to look plausible and large enough
// to be wrong — mistaking one for the other is how a document set to "10"
// fits 86 characters on a line where Google Docs fits 64. Convert at the
// boundary, with this, rather than by writing 1.3333 at each call site.
func Points(pt float32) float32 { return pt * CSSPixelsPerInch / 72 }

func ToPoints(px float32) float32 { return px * 72 / CSSPixelsPerInch }

// TextScale rounds font.Size to an integer multiple of the base glyph
// height. Used by legacy bitmap-based paths; opentype paths read the
// size directly. Exported for widgets package use.
func TextScale(font Font) float32 {
	if font.Size <= 0 {
		return 1
	}
	scale := float32(int(font.Size/float32(baseTextHeight) + 0.5))
	if scale < 1 {
		scale = 1
	}
	return scale
}

// TextAlign controls horizontal alignment inside the layout box.
type TextAlign int

const (
	TextAlignStart TextAlign = iota
	TextAlignCenter
	TextAlignEnd
	// TextAlignJustify stretches every soft-wrapped line to the full layout
	// width by widening its word gaps (CSS text-align: justify). Lines that
	// end a paragraph — a '\n', the final line, a truncated line — stay
	// start-aligned, as do lines with no spaces. The per-line widening is
	// carried on TextLayoutLine.JustifyExtra and applied through the
	// Font.WordSpacing channel, so drawing, measurement, selection, and
	// hit-testing all see identical advances.
	TextAlignJustify
)

// TextDecoration is a bitmask of line decorations applied to text
// (CSS text-decoration). The zero value draws nothing.
type TextDecoration uint8

const (
	DecorationUnderline   TextDecoration = 1 << iota // underline
	DecorationLineThrough                            // strike-through
	DecorationOverline                               // overline
)

// DecorationLineStyle selects the stroke style of a text-decoration line
// (CSS text-decoration-style). The zero value is a solid line.
type DecorationLineStyle uint8

const (
	DecorationSolid  DecorationLineStyle = iota // ─────
	DecorationDouble                            // ═════
	DecorationDotted                            // ·····
	DecorationDashed                            // - - -
	DecorationWavy                              // ∿∿∿∿∿
)

// DecorationPaint carries the optional CSS text-decoration-color / -style /
// -thickness overrides for a decoration line. The zero value means: paint in
// the text color, as a solid line, at the auto (font-derived) thickness — so
// callers that don't set it keep the historical behavior.
type DecorationPaint struct {
	Color     Color               // line color; used only when HasColor is set
	HasColor  bool                // when false, the decoration uses the text color
	Style     DecorationLineStyle // solid / double / dotted / dashed / wavy
	Thickness float32             // px; <= 0 = auto (font-derived)
}

// ParagraphStyle describes high-level paragraph layout behavior.
type ParagraphStyle struct {
	Align           TextAlign
	Direction       TextDirection
	Wrap            bool
	MaxLines        int
	Ellipsis        bool
	LineHeightScale float32
	// FirstIndent (px) indents the first line of the paragraph (CSS text-indent).
	FirstIndent float32
	// Decoration draws underline / line-through across the text lines
	// (CSS text-decoration). Applies to plain-text labels; rich spans
	// carry their own per-span decoration.
	Decoration TextDecoration
	// DecorationPaint carries the optional text-decoration-color / -style /
	// -thickness overrides for the plain-text decoration above. Zero value =
	// text color, solid line, auto thickness.
	DecorationPaint DecorationPaint
	// BreakLongWords allows a mid-word break for a word too long for a line
	// of its own (CSS overflow-wrap:break-word). See
	// TextLayoutOptions.BreakLongWords. Native widgets default this ON via
	// DefaultParagraphStyle — a label that clipped its long words would just
	// lose them; CSS-driven surfaces set it from the cascade instead.
	BreakLongWords bool
}

// DefaultParagraphStyle returns a safe default paragraph style.
func DefaultParagraphStyle() ParagraphStyle {
	return ParagraphStyle{
		Align:    TextAlignStart,
		Wrap:     false,
		MaxLines: 0,
		Ellipsis: false,
		// 0 = CSS `line-height: normal`. It used to be 1, which meant the
		// same thing back when the scale multiplied the face's natural
		// height; under CSS semantics 1 is a deliberately tight line box.
		LineHeightScale: 0,
		BreakLongWords:  true,
	}
}

// LayoutOptions converts paragraph style to concrete text layout options.
func (p ParagraphStyle) LayoutOptions(maxWidth float32) TextLayoutOptions {
	return TextLayoutOptions{
		MaxWidth:        maxWidth,
		Wrap:            p.Wrap,
		MaxLines:        p.MaxLines,
		Ellipsis:        p.Ellipsis,
		Align:           p.Align,
		Direction:       p.Direction,
		LineHeightScale: p.LineHeightScale,
		FirstIndent:     p.FirstIndent,
		BreakLongWords:  p.BreakLongWords,
	}
}

// TextLayoutOptions configures line breaking and line placement.
type TextLayoutOptions struct {
	// MaxWidth in logical pixels. <=0 means "unbounded".
	MaxWidth float32
	// Wrap enables soft-wrapping when MaxWidth > 0.
	Wrap bool
	// MaxLines clamps output line count. <=0 means unlimited.
	MaxLines int
	// Ellipsis appends an ellipsis (U+2026) when lines are truncated.
	Ellipsis bool
	// Align controls per-line horizontal alignment.
	Align TextAlign
	// Direction controls the paragraph base direction. Auto uses the first
	// strong directional rune.
	Direction TextDirection
	// LineHeightScale is a CSS `line-height`: a multiple of the FONT SIZE.
	// <=0 means CSS `line-height: normal` — the face's own height.
	LineHeightScale float32
	// FirstIndent (px) indents the first visual line (CSS text-indent):
	// the first line wraps within a width reduced by FirstIndent and is
	// shifted right by it. 0 = no indent.
	FirstIndent float32
	// BreakLongWords allows a break inside a word that is too long to fit a
	// line of its own — CSS overflow-wrap:break-word. Without it such a word
	// forms one line that overflows MaxWidth (CSS overflow-wrap:normal).
	// Mirrors InlineLayoutOptions.BreakLongWords, so both text engines wrap
	// the same way for the same input. No-op unless Wrap && MaxWidth > 0.
	BreakLongWords bool
}

// TextLayoutLine stores one visual line.
type TextLayoutLine struct {
	Text   string
	Width  float32
	Offset float32
	// Start / End are rune offsets into the source text that this visual
	// line spans, inclusive-exclusive. For soft-wrapped lines End reaches
	// the start of the next line (so it includes the wrap whitespace),
	// keeping the ranges contiguous — selection highlighting and hit
	// testing rely on these to match the rendered glyphs exactly instead
	// of re-deriving line breaks with a different algorithm.
	Start int
	End   int
	// JustifyExtra is the extra advance (px) each space character on this
	// line receives under TextAlignJustify. Consumers add it to the font's
	// WordSpacing when drawing or measuring this line. 0 = not justified.
	JustifyExtra float32
	shaped       *shapedLine
}

// TextLayout is an immutable line layout result for a text block.
type TextLayout struct {
	Lines      []TextLayoutLine
	Width      float32
	Height     float32
	LineHeight float32
	Truncated  bool
}

// Text-layout memoization. BuildTextLayout is pure in (text, font spec,
// options) for a fixed font registry, yet a full-tree relayout re-runs it
// for EVERY label — and flex measures many widgets more than once per
// pass. Without this cache a document with ~100 text widgets pays tens of
// milliseconds of rune measurement per relayout (an order of magnitude
// more than the layout math itself); with it, unchanged text is a map
// lookup. The cache is cleared whenever the font registry changes
// (clearFontFaceCachesUnlocked) and bounded by wholesale reset — no LRU
// bookkeeping on the hot path.
type textLayoutCacheKey struct {
	text string
	font fontCacheKey
	opts TextLayoutOptions
	// localeGen makes the memo locale-aware. Shaping is handed a
	// language tag (see shapingLanguage), so the SAME text in the SAME
	// font can produce different glyphs — and therefore different
	// advances and line breaks — in zh-Hans, zh-Hant and ja. Without
	// this field, switching language would serve measurements taken
	// under the previous one.
	//
	// invalidateTextLayoutCache already drops the whole map on a locale
	// change, so this is belt-and-braces against a stale entry surviving
	// a race between the bump and an in-flight measurement.
	localeGen uint64
}

var (
	textLayoutCacheMu sync.Mutex
	textLayoutCache   = map[textLayoutCacheKey]TextLayout{}
)

// textLayoutCacheLimit bounds the memo. When full the whole map is
// dropped: recomputing one document's worth of layouts is cheaper than
// per-entry eviction accounting.
const textLayoutCacheLimit = 8192

func invalidateTextLayoutCache() {
	textLayoutCacheMu.Lock()
	textLayoutCache = map[textLayoutCacheKey]TextLayout{}
	textLayoutCacheMu.Unlock()
}

// BuildTextLayout computes wrapped/aligned lines for text. The result is
// immutable and may be shared: callers must not mutate Lines.
func BuildTextLayout(text string, fontSpec Font, opts TextLayoutOptions) TextLayout {
	if !opts.Wrap && opts.MaxWidth > 0 {
		// Keep explicit false. Legacy callers may still pass max width
		// only to request alignment/clamping.
	} else if opts.MaxWidth > 0 {
		opts.Wrap = true
	}
	// LineHeightScale is NOT normalized here: 0 is a meaningful value (CSS
	// `line-height: normal`), and collapsing it to 1 would silently ask for
	// a line box the height of the font size.

	// Memo lookup AFTER option normalization (and with the same size
	// default GetFontFacesFor applies) so equivalent calls share a key.
	keySpec := fontSpec
	if keySpec.Size <= 0 {
		keySpec.Size = 14
	}
	key := textLayoutCacheKey{text: text, font: makeFontCacheKey(keySpec), opts: opts, localeGen: LocaleGeneration()}
	textLayoutCacheMu.Lock()
	if cached, ok := textLayoutCache[key]; ok {
		textLayoutCacheMu.Unlock()
		return cached
	}
	textLayoutCacheMu.Unlock()

	layout := buildTextLayoutUncached(text, fontSpec, opts)

	textLayoutCacheMu.Lock()
	if len(textLayoutCache) >= textLayoutCacheLimit {
		textLayoutCache = map[textLayoutCacheKey]TextLayout{}
	}
	textLayoutCache[key] = layout
	textLayoutCacheMu.Unlock()
	return layout
}

// buildTextLayoutUncached is the real line builder behind BuildTextLayout.
// opts arrive normalized (Wrap promotion, LineHeightScale default).
func buildTextLayoutUncached(text string, fontSpec Font, opts TextLayoutOptions) TextLayout {
	faces := GetFontFacesFor(fontSpec)
	lineHeight := scaledLineHeight(fontSpec, opts.LineHeightScale)

	lines := make([]TextLayoutLine, 0, 8)
	overhang := textVisualOverhang(fontSpec)
	wrapMaxWidth := opts.MaxWidth
	if opts.Wrap && opts.MaxWidth > 0 {
		wrapMaxWidth = opts.MaxWidth - overhang
		if wrapMaxWidth < 1 {
			wrapMaxWidth = 1
		}
	}
	// Split into logical lines on '\n' over the source runes so each
	// visual line carries accurate [Start,End) rune offsets into `text`.
	srcRunes := []rune(text)
	segStart := 0
	// hardEnd marks lines that end a paragraph segment (before a '\n' or at
	// the end of the text) rather than at a soft wrap — justify leaves them
	// start-aligned (CSS: the last line of a paragraph is not stretched).
	hardEnd := make([]bool, 0, 8)
	appendSegment := func(segEnd int) {
		segText := strings.TrimSuffix(string(srcRunes[segStart:segEnd]), "\r")
		segRunes := []rune(segText)
		segLen := len(segRunes)
		fi := float32(0)
		if segStart == 0 {
			fi = opts.FirstIndent
		}
		if paragraph, ok := shapeParagraph(segRunes, fontSpec, opts.Direction); ok {
			maxWidth := float32(0)
			if opts.Wrap && opts.MaxWidth > 0 {
				maxWidth = wrapMaxWidth
			}
			for _, sl := range wrapShapedParagraph(paragraph, maxWidth, fi, opts.BreakLongWords) {
				lineText := string(segRunes[sl.start:sl.end])
				if opts.Wrap && opts.MaxWidth > 0 {
					lineText = strings.Trim(lineText, " \t")
				}
				line := sl
				lines = append(lines, TextLayoutLine{
					Text:   lineText,
					Width:  withTextVisualOverhang(sl.advance, overhang),
					Start:  segStart + sl.start,
					End:    segStart + sl.end,
					shaped: &line,
				})
				hardEnd = append(hardEnd, false)
			}
			if len(hardEnd) > 0 {
				hardEnd[len(hardEnd)-1] = true
			}
			return
		}
		if opts.Wrap && opts.MaxWidth > 0 {
			// text-indent applies only to the first visual line of the block,
			// i.e. the first segment's first wrapped line.
			for _, wl := range wrapTextLine(segText, wrapMaxWidth, fi, opts.BreakLongWords, fontSpec, faces) {
				wl.Start += segStart
				wl.End += segStart
				lines = append(lines, wl)
				hardEnd = append(hardEnd, false)
			}
			if len(hardEnd) > 0 {
				hardEnd[len(hardEnd)-1] = true
			}
			return
		}
		w := withTextVisualOverhang(measureRunes([]rune(segText), fontSpec, faces), overhang)
		lines = append(lines, TextLayoutLine{Text: segText, Width: w, Start: segStart, End: segStart + segLen})
		hardEnd = append(hardEnd, true)
	}
	for i := 0; i < len(srcRunes); i++ {
		if srcRunes[i] == '\n' {
			appendSegment(i)
			segStart = i + 1
		}
	}
	appendSegment(len(srcRunes))
	if len(lines) == 0 {
		lines = append(lines, TextLayoutLine{})
		hardEnd = append(hardEnd, true)
	}

	truncated := false
	if opts.MaxLines > 0 && len(lines) > opts.MaxLines {
		lines = lines[:opts.MaxLines]
		hardEnd = hardEnd[:opts.MaxLines]
		hardEnd[len(hardEnd)-1] = true // an ellipsed/cut line is never stretched
		truncated = true
		if opts.Ellipsis {
			last := lines[len(lines)-1]
			last.Text, last.Width, last.shaped = ellipsizeTextLine(last.Text, opts.MaxWidth, fontSpec, opts.Direction, faces)
			lines[len(lines)-1] = last
		}
	}

	// Justify: widen each soft-wrapped line's word gaps until it fills the
	// layout width. The extra rides on JustifyExtra (per space) so drawing
	// and measurement apply it through Font.WordSpacing.
	if opts.Align == TextAlignJustify && opts.Wrap && opts.MaxWidth > 0 {
		for i := range lines {
			if hardEnd[i] {
				continue
			}
			spaces := strings.Count(lines[i].Text, " ")
			if spaces == 0 || lines[i].Width >= opts.MaxWidth {
				continue
			}
			lines[i].JustifyExtra = (opts.MaxWidth - lines[i].Width) / float32(spaces)
			lines[i].Width = opts.MaxWidth
			fs := fontSpec
			fs.WordSpacing += lines[i].JustifyExtra
			if shaped, ok := shapeSingleLine([]rune(lines[i].Text), fs, opts.Direction); ok {
				lines[i].shaped = shaped
			}
		}
	}

	var maxW float32
	for i := range lines {
		if lines[i].Width > maxW {
			maxW = lines[i].Width
		}
	}
	alignW := maxW
	if opts.MaxWidth > 0 {
		alignW = opts.MaxWidth
	}
	for i := range lines {
		rtl := lines[i].shaped != nil && lines[i].shaped.direction == di.DirectionRTL
		switch opts.Align {
		case TextAlignCenter:
			lines[i].Offset = (alignW - lines[i].Width) * 0.5
		case TextAlignEnd:
			if !rtl {
				lines[i].Offset = alignW - lines[i].Width
			}
		default:
			if rtl {
				lines[i].Offset = alignW - lines[i].Width
			}
		}
		if lines[i].Offset < 0 {
			lines[i].Offset = 0
		}
	}
	// text-indent shifts the first visual line right (added after alignment so
	// it composes with left / center / right).
	if opts.FirstIndent > 0 && len(lines) > 0 {
		lines[0].Offset += opts.FirstIndent
		if lines[0].Offset+lines[0].Width > maxW {
			maxW = lines[0].Offset + lines[0].Width
		}
	}
	return TextLayout{
		Lines:      lines,
		Width:      maxW,
		Height:     float32(len(lines)) * lineHeight,
		LineHeight: lineHeight,
		Truncated:  truncated,
	}
}

// DrawTextBlock builds then draws a layout inside rect.
func DrawTextBlock(canvas Canvas, text string, rect Rect, color Color, fontSpec Font, opts TextLayoutOptions) TextLayout {
	layout := BuildTextLayout(text, fontSpec, opts)
	DrawTextLayout(canvas, layout, rect, color, fontSpec)
	return layout
}

// DrawTextLayout draws a precomputed text layout.
func DrawTextLayout(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font) {
	y := rect.Y
	for _, line := range layout.Lines {
		if line.Text != "" {
			fs := fontSpec
			if line.JustifyExtra != 0 {
				// Justified line: widen its spaces via the word-spacing
				// channel so the drawn advances match the layout width.
				fs.WordSpacing += line.JustifyExtra
			}
			lineRect := Rect{
				X: rect.X + line.Offset,
				Y: y,
				W: line.Width,
				H: layout.LineHeight,
			}
			if shapedCanvas, ok := canvas.(shapedLineCanvas); ok && line.shaped != nil {
				shapedCanvas.drawShapedLine(line.shaped, lineRect, color, fs)
			} else {
				canvas.DrawText(line.Text, lineRect, color, fs)
			}
		}
		y += layout.LineHeight
	}
}

// DrawTextDecoration strokes underline / line-through lines over every
// visual line of a plain-text layout, using the same per-line geometry as
// DrawTextLayout. No-op when deco is zero.
func DrawTextDecoration(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font, deco TextDecoration) {
	DrawTextDecorationPaint(canvas, layout, rect, color, fontSpec, deco, DecorationPaint{})
}

// DrawTextDecorationPaint is DrawTextDecoration with explicit color / style /
// thickness overrides (CSS text-decoration-color/-style/-thickness).
func DrawTextDecorationPaint(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font, deco TextDecoration, paint DecorationPaint) {
	if deco == 0 {
		return
	}
	y := rect.Y
	for _, line := range layout.Lines {
		if line.Text != "" && line.Width > 0 {
			drawDecorationRun(canvas, deco, rect.X+line.Offset, line.Width, y, layout.LineHeight, color, fontSpec, paint)
		}
		y += layout.LineHeight
	}
}

// drawDecorationRun strokes the requested decoration line(s) across one
// run/line box [x, x+w] whose top is at `top` and height is `lineHeight`.
// The baseline is derived the same way the CPU text backend places glyphs
// (centered ink within the line box), so the underline sits just under the
// text regardless of extra line-height.
func drawDecorationRun(canvas Canvas, deco TextDecoration, x, w, top, lineHeight float32, color Color, fontSpec Font, paint DecorationPaint) {
	if deco == 0 || w <= 0 {
		return
	}
	m := GetFontFaceFor(fontSpec).Metrics()
	ascent := float32(m.Ascent.Ceil())
	descent := float32(m.Descent.Ceil())
	if ascent <= 0 {
		ascent = fontSpec.Size
	}
	ink := ascent + descent
	extra := (lineHeight - ink) / 2
	if extra < 0 {
		extra = 0
	}
	baseline := top + extra + ascent
	thickness := paint.Thickness
	if thickness <= 0 {
		thickness = ascent / 12
		if thickness < 1 {
			thickness = 1
		}
	}
	lineColor := color
	if paint.HasColor {
		lineColor = paint.Color
	}
	if deco&DecorationUnderline != 0 {
		drawDecoLine(canvas, paint.Style, x, baseline+descent*0.4, w, thickness, lineColor)
	}
	if deco&DecorationLineThrough != 0 {
		drawDecoLine(canvas, paint.Style, x, baseline-ascent*0.32, w, thickness, lineColor)
	}
	if deco&DecorationOverline != 0 {
		drawDecoLine(canvas, paint.Style, x, baseline-ascent, w, thickness, lineColor)
	}
}

// drawDecoLine strokes a single decoration line [x, x+w] whose top is at y,
// honoring the CSS text-decoration-style (solid / double / dotted / dashed /
// wavy). Solid/double/dotted/dashed use axis-aligned fills (crisp, cheap);
// wavy strokes a zig-zag path.
func drawDecoLine(canvas Canvas, style DecorationLineStyle, x, y, w, thickness float32, color Color) {
	if w <= 0 || thickness <= 0 {
		return
	}
	switch style {
	case DecorationDouble:
		canvas.FillRect(Rect{X: x, Y: y, W: w, H: thickness}, color)
		canvas.FillRect(Rect{X: x, Y: y + thickness*2, W: w, H: thickness}, color)
	case DecorationDotted:
		dot := thickness
		for dx := float32(0); dx < w; dx += dot * 2 {
			seg := dot
			if dx+seg > w {
				seg = w - dx
			}
			canvas.FillRect(Rect{X: x + dx, Y: y, W: seg, H: thickness}, color)
		}
	case DecorationDashed:
		dash := thickness * 4
		for dx := float32(0); dx < w; dx += dash + thickness*3 {
			seg := dash
			if dx+seg > w {
				seg = w - dx
			}
			canvas.FillRect(Rect{X: x + dx, Y: y, W: seg, H: thickness}, color)
		}
	case DecorationWavy:
		amp := thickness * 1.5
		half := amp * 2 // half a wave period
		if half <= 0 {
			half = 2
		}
		p := NewPath()
		p.MoveTo(x, y+amp)
		up := true
		for dx := float32(0); dx < w; dx += half {
			nx := x + dx + half
			if nx > x+w {
				nx = x + w
			}
			ny := y + amp
			if up {
				ny = y
			}
			p.LineTo(nx, ny)
			up = !up
		}
		canvas.DrawShape(ShapePath{p}, Paint{Style: PaintStroke, Color: color, StrokeWidth: thickness, AntiAlias: true})
	default: // solid
		canvas.FillRect(Rect{X: x, Y: y, W: w, H: thickness}, color)
	}
}

// RuneAdvance returns the standalone pixel advance of one rune. It is kept
// for compatibility and isolated marker/layout helpers; paragraph layout,
// caret geometry, selection and rendering must use the shaping APIs because a
// rune's advance is not meaningful inside ligatures or complex-script runs.
// Emoji use the provider bitmap width. Continuation runes such as variation
// selectors and ZWJ return zero.
func RuneAdvance(r rune, fontSpec Font) float32 {
	return runeAdvanceWithFaces(r, fontSpec, nil)
}

func runeAdvanceWithFaces(r rune, fontSpec Font, faces []font.Face) float32 {
	// Emoji-cluster continuation runes (VS-16, ZWJ, skin-tone modifiers,
	// keycap combiner, tag sequences) carry zero standalone advance —
	// they reshape the preceding base glyph. They have no meaningful
	// standalone advance; cluster-aware callers use the shaping APIs.
	if isEmojiContinuation(r) {
		return 0
	}
	if isEmojiRune(r) {
		if img := lookupEmojiImage(r, fontSpec.Size); img != nil {
			return float32(img.Bounds().Dx()) + trackFor(r, fontSpec)
		}
		// Emoji provider declined — rune will be invisible in Draw,
		// so advance 0 keeps cursor math consistent with painting.
		return 0
	}
	if len(faces) == 0 {
		faces = GetFontFacesFor(fontSpec)
	}
	for _, face := range faces {
		if advance, ok := face.GlyphAdvance(r); ok {
			// Keep sub-pixel precision (26.6 fixed-point) so cumulative
			// widths remain stable for narrow glyphs like "1". Rounding each
			// rune independently causes linear drift between text and caret.
			return float32(advance)/64.0 + trackFor(r, fontSpec)
		}
	}
	// No glyph, no emoji bitmap — fall back to a modest estimate so
	// cursor doesn't completely collapse on unknown runes.
	return float32(fontSpec.Size)*0.5 + trackFor(r, fontSpec)
}

// trackFor returns the CSS letter-spacing / word-spacing tracking added
// after a single visible rune: LetterSpacing for every glyph, plus
// WordSpacing for an ASCII space. Both are zero by default (no effect), so
// this is a no-op for the common case. The CPU glyph drawer adds the same
// amount so painted text matches measured widths.
func trackFor(r rune, fontSpec Font) float32 {
	if fontSpec.LetterSpacing == 0 && fontSpec.WordSpacing == 0 {
		return 0
	}
	s := fontSpec.LetterSpacing
	if r == ' ' {
		s += fontSpec.WordSpacing
	}
	return s
}

// TextMetrics returns the shaped bounding box of text rendered at the given
// font. Each logical line is measured as glyph runs, including OpenType
// substitutions/positioning, bidi ordering, spacing and emoji clusters.
// Height comes from the font face metrics.
func TextMetrics(text string, fontSpec Font) (width, height float32) {
	face := GetFontFaceFor(fontSpec)
	lineHeight := float32(face.Metrics().Height.Ceil())
	lines := splitLogicalLines(text)
	if len(lines) == 0 {
		return 0, 0
	}
	faces := GetFontFacesFor(fontSpec)
	var maxW float32
	overhang := textVisualOverhang(fontSpec)
	for _, line := range lines {
		w := withTextVisualOverhang(measureRunes([]rune(line), fontSpec, faces), overhang)
		if w > maxW {
			maxW = w
		}
	}
	return maxW, float32(len(lines)) * lineHeight
}

// TextXHeightCenterY returns the y at which a single line of text's x-height
// band sits when drawn into rect — i.e. the optical center of a lowercase run.
//
// It mirrors how the backend places single-line text: the baseline is centered
// on the CAP-height box (see drawTextLine in backend_cpu.go), and the x-height
// band hangs below that. Use it to line non-text marks up with real text drawn
// into the same rect — list bullets, password mask dots, any glyph substitute.
// Centering such a mark on the rect itself lands it on the CAP-height center,
// visibly above where lowercase text actually sits.
//
// Falls back to the rect's own center when the face reports no usable
// cap/x-height metrics.
func TextXHeightCenterY(rect Rect, fontSpec Font) float32 {
	metrics := GetFontFaceFor(fontSpec).Metrics()
	capHeight, xHeight := float32(metrics.CapHeight.Ceil()), float32(metrics.XHeight)/64
	if capHeight <= 0 || xHeight <= 0 {
		return rect.Y + rect.H/2
	}
	baseline := rect.Y + (rect.H+capHeight)/2
	return baseline - xHeight/2
}

func splitLogicalLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func measureRunes(runes []rune, fontSpec Font, faces []font.Face) float32 {
	if paragraph, ok := shapeParagraph(runes, fontSpec, TextDirectionAuto); ok {
		return paragraph.advance
	}
	var w float32
	for i := 0; i < len(runes); {
		r := runes[i]
		// Emoji-aware: gather the whole cluster (base + modifiers / ZWJ
		// joins) and credit its joined-glyph width to one step. Non-base
		// continuations at start (stray modifier) fall through to the
		// per-rune branch which returns 0.
		if isEmojiRune(r) && !isEmojiContinuation(r) {
			end := EmojiClusterEnd(runes, i)
			if end > i+1 {
				if img := lookupEmojiSequence(string(runes[i:end]), fontSpec.Size); img != nil {
					w += float32(img.Bounds().Dx())
					i = end
					continue
				}
			}
		}
		w += runeAdvanceWithFaces(r, fontSpec, faces)
		i++
	}
	return w
}

func ellipsizeTextLine(line string, maxWidth float32, fontSpec Font, direction TextDirection, faces []font.Face) (string, float32, *shapedLine) {
	overhang := textVisualOverhang(fontSpec)
	maxAdvance := maxWidth
	if maxWidth > 0 {
		maxAdvance -= overhang
	}
	runes := []rune(line)
	boundaries := GraphemeBoundaries(runes)
	for keep := len(boundaries) - 1; keep >= 0; keep-- {
		candidate := append(append([]rune(nil), runes[:boundaries[keep]]...), '…')
		shaped, ok := shapeSingleLine(candidate, fontSpec, direction)
		if !ok {
			break
		}
		width := withTextVisualOverhang(shaped.advance, overhang)
		if maxWidth <= 0 || shaped.advance <= maxAdvance || keep == 0 {
			return string(candidate), width, shaped
		}
	}
	text, width := ellipsizeLine(line, maxWidth, fontSpec, faces)
	return text, width, nil
}

func wrapTextLine(line string, maxWidth, firstIndent float32, breakLongWords bool, fontSpec Font, faces []font.Face) []TextLayoutLine {
	runes := []rune(line)
	if len(runes) == 0 {
		return []TextLayoutLine{{}}
	}
	out := make([]TextLayoutLine, 0, 1)
	overhang := textVisualOverhang(fontSpec)
	for start := 0; start < len(runes); {
		// CSS text-indent narrows only the first line's available width.
		lineMax := maxWidth
		if start == 0 && firstIndent > 0 {
			lineMax = maxWidth - firstIndent
			if lineMax < 1 {
				lineMax = 1
			}
		}
		end := start
		var w float32
		lastBreak := -1
		var lastBreakW float32
		for end < len(runes) {
			r := runes[end]
			adv := runeAdvanceWithFaces(r, fontSpec, faces)
			if w+adv > lineMax && end > start {
				break
			}
			w += adv
			end++
			if r == ' ' || r == '\t' {
				lastBreak = end
				lastBreakW = w
			}
			if w > lineMax && end == start+1 {
				break
			}
		}
		if end < len(runes) && lastBreak > start {
			text := string(trimTrailingWhitespaceRunes(runes[start:lastBreak]))
			width := withTextVisualOverhang(measureRunes([]rune(text), fontSpec, faces), overhang)
			if text == "" {
				text = string(runes[start:lastBreak])
				width = withTextVisualOverhang(lastBreakW, overhang)
			}
			next := trimLeadingWhitespaceIndex(runes, lastBreak)
			out = append(out, TextLayoutLine{Text: text, Width: width, Start: start, End: next})
			start = next
			continue
		}
		if end == start {
			end = start + 1
			w = runeAdvanceWithFaces(runes[start], fontSpec, faces)
		}
		if !breakLongWords && end < len(runes) {
			// overflow-wrap:normal — this word has no break opportunity
			// inside it and does not fit, so it stays whole and overflows
			// (browser behavior). Run out to the word's end instead of
			// cutting mid-word, then resume after the trailing spaces.
			for end < len(runes) && runes[end] != ' ' && runes[end] != '\t' {
				end++
			}
			w = measureRunes(runes[start:end], fontSpec, faces)
			next := trimLeadingWhitespaceIndex(runes, end)
			out = append(out, TextLayoutLine{Text: string(runes[start:end]), Width: withTextVisualOverhang(w, overhang), Start: start, End: next})
			start = next
			continue
		}
		out = append(out, TextLayoutLine{Text: string(runes[start:end]), Width: withTextVisualOverhang(w, overhang), Start: start, End: end})
		start = end
	}
	return out
}

func ellipsizeLine(line string, maxWidth float32, fontSpec Font, faces []font.Face) (string, float32) {
	if line == "" {
		return line, 0
	}
	overhang := textVisualOverhang(fontSpec)
	ellipsis := "…"
	ew := measureRunes([]rune(ellipsis), fontSpec, faces)
	maxAdvance := maxWidth
	if maxWidth > 0 {
		maxAdvance = maxWidth - overhang
	}
	if maxWidth <= 0 {
		text := line + ellipsis
		return text, withTextVisualOverhang(measureRunes([]rune(text), fontSpec, faces), overhang)
	}
	if maxAdvance <= 0 || ew >= maxAdvance {
		return ellipsis, withTextVisualOverhang(ew, overhang)
	}
	runes := []rune(line)
	var w float32
	cut := 0
	for cut < len(runes) {
		adv := runeAdvanceWithFaces(runes[cut], fontSpec, faces)
		if w+adv+ew > maxAdvance {
			break
		}
		w += adv
		cut++
	}
	text := string(runes[:cut]) + ellipsis
	return text, withTextVisualOverhang(w+ew, overhang)
}

func textVisualOverhang(fontSpec Font) float32 {
	if !fontSpec.Italic {
		return 0
	}
	lineHeight := float32(GetFontFaceFor(fontSpec).Metrics().Height.Ceil())
	if lineHeight <= 0 {
		return 0
	}
	return float32(math.Ceil(float64(lineHeight * italicFallbackShearFactor)))
}

func withTextVisualOverhang(width, overhang float32) float32 {
	if width <= 0 {
		return 0
	}
	return width + overhang
}

func trimTrailingWhitespaceRunes(runes []rune) []rune {
	n := len(runes)
	for n > 0 {
		if runes[n-1] != ' ' && runes[n-1] != '\t' {
			break
		}
		n--
	}
	return runes[:n]
}

func trimLeadingWhitespaceIndex(runes []rune, idx int) int {
	for idx < len(runes) {
		if runes[idx] != ' ' && runes[idx] != '\t' {
			break
		}
		idx++
	}
	return idx
}
