package qui

import (
	"golang.org/x/image/font"

	"github.com/qizhanchan/qui/internal/typeset"
)

// The text engine — font registry, shaping, paragraph and inline layout,
// caret geometry, emoji — lives in internal/typeset, below the engine and
// the rasterizers. The aliases and wrappers below are its public face.

type (
	// DecorationLineStyle selects the stroke style of a text-decoration line
	// (CSS text-decoration-style). The zero value is a solid line.
	DecorationLineStyle = typeset.DecorationLineStyle

	// DecorationPaint carries the optional CSS text-decoration-color / -style /
	// -thickness overrides for a decoration line. The zero value means: paint in
	// the text color, as a solid line, at the auto (font-derived) thickness — so
	// callers that don't set it keep the historical behavior.
	DecorationPaint = typeset.DecorationPaint

	// EmojiProvider returns a rasterized RGBA bitmap for the given emoji
	// sequence at approximately the given font size. The sequence is a
	// UTF-8 string covering one extended grapheme cluster — a single base
	// emoji rune, optionally followed by variation selectors, skin-tone
	// modifiers, ZWJ-joined components, keycap suffixes, or tag sequences.
	// Returns nil when the sequence isn't a supported emoji (caller falls
	// through to plain text rendering). The bitmap's origin is the
	// top-left; blitting pairs it with text's baseline by centering within
	// one line height.
	EmojiProvider = typeset.EmojiProvider

	// FontFace bundles one weight/style of a font family for SetDefaultFont.
	// Data is a single-font .ttf / .otf (not a collection).
	FontFace = typeset.FontFace

	// InlineBaselineProvider is an optional interface for atomic inline boxes that
	// carry text: it reports the box's own text baseline (distance from the box's
	// top) so the IFC can baseline-align the box with surrounding text. Hosts that
	// build InlineItems (e.g. widgets.InlineBox) query it when adding a box.
	InlineBaselineProvider = typeset.InlineBaselineProvider

	// InlineFrag is one positioned piece of an inline layout: a drawable text run
	// or an atomic box placeholder. Rect is relative to the layout origin (0,0 at
	// the top-left). For a text frag the rect height is the glyph ink box (ascent
	// + descent) so DrawText centers the glyphs on the intended baseline.
	InlineFrag = typeset.InlineFrag

	// InlineItem is one participant in an inline formatting context: either a run
	// of styled text or an atomic box.
	InlineItem = typeset.InlineItem

	// InlineLayout is the immutable output of BuildInlineLayout.
	InlineLayout = typeset.InlineLayout

	// InlineLayoutOptions configures line breaking and placement.
	InlineLayoutOptions = typeset.InlineLayoutOptions

	// InlineLine is one laid-out line of an inline layout.
	InlineLine = typeset.InlineLine

	// InlineVAlign selects how an atomic inline box aligns against the line
	// baseline. Text always sits on the baseline; boxes can offset.
	InlineVAlign = typeset.InlineVAlign

	// ParagraphStyle describes high-level paragraph layout behavior.
	ParagraphStyle = typeset.ParagraphStyle

	// RichTextLayout is the immutable output of BuildRichTextLayout.
	RichTextLayout = typeset.RichTextLayout

	// RichTextLine stores one laid out line of rich text.
	RichTextLine = typeset.RichTextLine

	// RichTextRun is one drawable run inside a visual line.
	RichTextRun = typeset.RichTextRun

	// SourcePos addresses a rune boundary in the item stream a layout was built
	// from: Item indexes the []InlineItem slice passed to BuildInlineLayout, Off
	// is a rune offset within that item's Text. Atomic boxes span the synthetic
	// range [0,1] — Off 0 is "before the box", 1 is "after it". A SourcePos
	// between two runes is a caret position; editors map their model offsets
	// onto (item, offset) pairs and back.
	SourcePos = typeset.SourcePos

	// TabAlign is how the text following a tab stop sits against it.
	TabAlign = typeset.TabAlign

	// TabStop is one explicit tab position. Leader is the character painted
	// across the gap the tab opens (0 = nothing; '.' gives the dot leaders a
	// table of contents draws between a heading and its page number).
	TabStop = typeset.TabStop

	// TextAlign controls horizontal alignment inside the layout box.
	TextAlign = typeset.TextAlign

	// TextCaretStop is one visual caret position expressed as a rune offset and
	// an X advance from the left edge of the shaped text.
	TextCaretStop = typeset.TextCaretStop

	// TextDecoration is a bitmask of line decorations applied to text
	// (CSS text-decoration). The zero value draws nothing.
	TextDecoration = typeset.TextDecoration

	// TextDirection controls the base direction of a paragraph. Auto resolves the
	// first strong directional rune and falls back to left-to-right for neutral
	// text.
	TextDirection = typeset.TextDirection

	// TextLayout is an immutable line layout result for a text block.
	TextLayout = typeset.TextLayout

	// TextLayoutLine stores one visual line.
	TextLayoutLine = typeset.TextLayoutLine

	// TextLayoutOptions configures line breaking and line placement.
	TextLayoutOptions = typeset.TextLayoutOptions

	// TextSelectionSegment is one contiguous visual portion of a logical text
	// selection. Bidirectional selections may produce several segments.
	TextSelectionSegment = typeset.TextSelectionSegment

	// TextSpan is an inline rich-text segment.
	// When Font/Color are nil, base style is used.
	TextSpan = typeset.TextSpan
)

const (
	// CSSPixelsPerInch is the resolution the engine's LOGICAL pixel is defined
	// against — the CSS reference, not the display's. Physical DPI is the
	// backend's business (see the window scale factor); everything above the
	// rasterizer measures in these.
	CSSPixelsPerInch = typeset.CSSPixelsPerInch

	// - - -
	DecorationDashed = typeset.DecorationDashed

	// ·····
	DecorationDotted = typeset.DecorationDotted

	// ═════
	DecorationDouble = typeset.DecorationDouble

	// strike-through
	DecorationLineThrough = typeset.DecorationLineThrough

	// overline
	DecorationOverline = typeset.DecorationOverline

	// ─────
	DecorationSolid = typeset.DecorationSolid

	// underline
	DecorationUnderline = typeset.DecorationUnderline

	// ∿∿∿∿∿
	DecorationWavy = typeset.DecorationWavy

	// InlineBaseline puts the box's bottom edge on the text baseline (CSS
	// default for replaced elements and inline-block).
	InlineBaseline = typeset.InlineBaseline
	InlineBottom   = typeset.InlineBottom

	// InlineMiddle centers the box vertically on the baseline (CSS
	// vertical-align: middle, approximated as baseline-centered).
	InlineMiddle = typeset.InlineMiddle

	// InlineTop / InlineBottom align the box to the line box top / bottom.
	InlineTop = typeset.InlineTop

	// TabCenter centres the run between this tab and the next one (or the
	// end of the line) on the stop.
	TabCenter = typeset.TabCenter

	// TabLeft starts the following text AT the stop (the default).
	TabLeft = typeset.TabLeft

	// TabRight ends that run AT the stop — the shape a table of contents
	// needs for its page-number column.
	TabRight        = typeset.TabRight
	TextAlignCenter = typeset.TextAlignCenter
	TextAlignEnd    = typeset.TextAlignEnd

	// TextAlignJustify stretches every soft-wrapped line to the full layout
	// width by widening its word gaps (CSS text-align: justify). Lines that
	// end a paragraph — a '\n', the final line, a truncated line — stay
	// start-aligned, as do lines with no spaces. The per-line widening is
	// carried on TextLayoutLine.JustifyExtra and applied through the
	// Font.WordSpacing channel, so drawing, measurement, selection, and
	// hit-testing all see identical advances.
	TextAlignJustify  = typeset.TextAlignJustify
	TextAlignStart    = typeset.TextAlignStart
	TextDirectionAuto = typeset.TextDirectionAuto
	TextDirectionLTR  = typeset.TextDirectionLTR
	TextDirectionRTL  = typeset.TextDirectionRTL
)

// AutoLoadSystemCJKEnabled reports whether lazy init currently probes
// system CJK fonts before falling back to bundled goregular.
func AutoLoadSystemCJKEnabled() bool { return typeset.AutoLoadSystemCJKEnabled() }

// BuildInlineLayout lays out a mix of text runs and atomic boxes into lines.
func BuildInlineLayout(items []InlineItem, opts InlineLayoutOptions) InlineLayout {
	return typeset.BuildInlineLayout(items, opts)
}

// BuildRichTextLayout lays out styled spans with wrapping/alignment.
func BuildRichTextLayout(spans []TextSpan, baseFont Font, baseColor Color, opts TextLayoutOptions) RichTextLayout {
	return typeset.BuildRichTextLayout(spans, baseFont, baseColor, opts)
}

// BuildTextLayout computes wrapped/aligned lines for text. The result is
// immutable and may be shared: callers must not mutate Lines.
func BuildTextLayout(text string, fontSpec Font, opts TextLayoutOptions) TextLayout {
	return typeset.BuildTextLayout(text, fontSpec, opts)
}

// ClearFontVariants removes all registered style variants.
func ClearFontVariants() { typeset.ClearFontVariants() }

// DefaultParagraphStyle returns a safe default paragraph style.
func DefaultParagraphStyle() ParagraphStyle { return typeset.DefaultParagraphStyle() }

// DrawDecorationLine strokes one text-decoration line spanning [x, x+w]
// whose top is at y, in the given style. Apps use it for custom
// decorations such as spell-check squiggles (DecorationWavy strokes a
// ShapePath; the other styles fill axis-aligned rects).
func DrawDecorationLine(canvas Canvas, style DecorationLineStyle, x float32, y float32, w float32, thickness float32, color Color) {
	typeset.DrawDecorationLine(canvas, style, x, y, w, thickness, color)
}

// DrawInlineLine draws a single line of the layout at origin. Callers that
// reposition individual lines (pagination splitting a paragraph across page
// boundaries) offset origin per line instead of per layout.
func DrawInlineLine(canvas Canvas, layout InlineLayout, li int, origin Rect) {
	typeset.DrawInlineLine(canvas, layout, li, origin)
}

// DrawInlineText draws only the text fragments (and their decorations) of an
// inline layout at origin. Atomic box fragments are the caller's job — the
// hosting widget draws its child widgets separately. origin is the top-left
// the layout was measured against (typically the content rect).
func DrawInlineText(canvas Canvas, layout InlineLayout, origin Rect) {
	typeset.DrawInlineText(canvas, layout, origin)
}

// DrawRichTextLayout draws a precomputed rich text layout.
func DrawRichTextLayout(canvas Canvas, layout RichTextLayout, origin Rect) {
	typeset.DrawRichTextLayout(canvas, layout, origin)
}

// DrawTextBlock builds then draws a layout inside rect.
func DrawTextBlock(canvas Canvas, text string, rect Rect, color Color, fontSpec Font, opts TextLayoutOptions) TextLayout {
	return typeset.DrawTextBlock(canvas, text, rect, color, fontSpec, opts)
}

// DrawTextDecoration strokes underline / line-through lines over every
// visual line of a plain-text layout, using the same per-line geometry as
// DrawTextLayout. No-op when deco is zero.
func DrawTextDecoration(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font, deco TextDecoration) {
	typeset.DrawTextDecoration(canvas, layout, rect, color, fontSpec, deco)
}

// DrawTextDecorationPaint is DrawTextDecoration with explicit color / style /
// thickness overrides (CSS text-decoration-color/-style/-thickness).
func DrawTextDecorationPaint(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font, deco TextDecoration, paint DecorationPaint) {
	typeset.DrawTextDecorationPaint(canvas, layout, rect, color, fontSpec, deco, paint)
}

// DrawTextLayout draws a precomputed text layout.
func DrawTextLayout(canvas Canvas, layout TextLayout, rect Rect, color Color, fontSpec Font) {
	typeset.DrawTextLayout(canvas, layout, rect, color, fontSpec)
}

// DrawTextSpans builds and draws rich text in one call.
func DrawTextSpans(canvas Canvas, spans []TextSpan, rect Rect, baseFont Font, baseColor Color, opts TextLayoutOptions) RichTextLayout {
	return typeset.DrawTextSpans(canvas, spans, rect, baseFont, baseColor, opts)
}

// EmojiClusterEnd returns the exclusive end index of the emoji cluster
// starting at runes[start]. A cluster is one base emoji rune plus any
// trailing skin-tone modifiers, variation selectors, ZWJ-joined
// components, keycap combiners, or tag-sequence runes. Regional
// indicators consume one partner (two-letter flag glyph). Non-emoji
// starts return start+1.
func EmojiClusterEnd(runes []rune, start int) int { return typeset.EmojiClusterEnd(runes, start) }

// FontFamilyRegistered reports whether name resolves to a registered
// family (case-insensitive). Importers use it to decide whether a family
// recorded in a document can be honored or should fall back to the
// default — an unknown name would silently render as the default anyway.
func FontFamilyRegistered(name string) bool { return typeset.FontFamilyRegistered(name) }

// FontRegistryGeneration returns a counter that changes every time the font
// registry is mutated (SetDefaultFont, fallback registration, a GL renderer
// reset). Cheap enough to read on every cache lookup.
func FontRegistryGeneration() uint64 { return typeset.FontRegistryGeneration() }

func GetFontFace(size float32) font.Face { return typeset.GetFontFace(size) }

// GetFontFaceFor resolves a font face for a full Font spec.
func GetFontFaceFor(spec Font) font.Face { return typeset.GetFontFaceFor(spec) }

// GetFontFaces returns primary + fallback faces for the requested size.
// Order is stable: primary first, then fallback registration order.
func GetFontFaces(size float32) []font.Face { return typeset.GetFontFaces(size) }

// GetFontFacesFor returns primary + fallback faces for the requested
// font spec. Order is stable: primary first, then fallback registration
// order.
func GetFontFacesFor(spec Font) []font.Face { return typeset.GetFontFacesFor(spec) }

// GraphemeBoundaries returns rune offsets for every extended grapheme
// boundary, including 0 and len(text).
func GraphemeBoundaries(text []rune) []int { return typeset.GraphemeBoundaries(text) }

// LoadFallbackFontFromFile appends a fallback font to the face chain.
// Fallbacks are consulted only when the primary font has no glyph.
func LoadFallbackFontFromFile(path string) error { return typeset.LoadFallbackFontFromFile(path) }

// LoadFontFromBytes replaces the default font with an in-memory font.
// Useful for embedding a font via go:embed so the app doesn't depend
// on system fonts being present.
func LoadFontFromBytes(data []byte) error { return typeset.LoadFontFromBytes(data) }

// LoadFontFromFile replaces the default font with one loaded from the
// given path. Accepts single-font files (.ttf / .otf) and collections
// (.ttc / .otc — the first face of the collection is used, which for
// most system collections like PingFang.ttc is the primary/SC face).
func LoadFontFromFile(path string) error { return typeset.LoadFontFromFile(path) }

// LoadFontVariantFromBytes registers a variant from in-memory font data.
func LoadFontVariantFromBytes(family string, weight FontWeight, italic bool, data []byte) error {
	return typeset.LoadFontVariantFromBytes(family, weight, italic, data)
}

// LoadFontVariantFromFile registers a family variant straight from a font
// file on disk (TTF/OTF/TTC — the first font of a collection is used).
// Use it to expose system-installed families to Font{Family: ...} without
// embedding them:
func LoadFontVariantFromFile(family string, weight FontWeight, italic bool, path string) error {
	return typeset.LoadFontVariantFromFile(family, weight, italic, path)
}

// LoadSystemCJKFont tries a handful of well-known system font paths,
// in priority order per OS, and installs the first one that parses.
// Returns the path used, or an error if nothing worked. Safe to call
// after some widgets have been drawn — the cache is invalidated.
func LoadSystemCJKFont() (string, error) { return typeset.LoadSystemCJKFont() }

// NaturalLineHeight is the face's own line height (CSS `line-height:
// normal`): ascent + descent + line gap, as the font declares them. It is
// the unit a word processor's line spacing counts in — "one line".
func NaturalLineHeight(fontSpec Font) float32 { return typeset.NaturalLineHeight(fontSpec) }

// NextClusterBoundary returns the rune index one cluster forward from
// idx. For an emoji-cluster start it jumps past the whole sequence
// (base + modifiers / ZWJ joins); for any other rune it advances by
// one. Used for caret navigation so a multi-rune emoji moves as a
// single visual unit under the arrow keys, Backspace, and Delete.
func NextClusterBoundary(runes []rune, idx int) int { return typeset.NextClusterBoundary(runes, idx) }

// Points converts a typographic point size to logical pixels, and ToPoints
// inverts it.
func Points(pt float32) float32 { return typeset.Points(pt) }

// PrevClusterBoundary returns the rune index one cluster backward from
// idx. Walks forward from 0 to find the largest cluster boundary
// strictly less than idx — necessary because clusters are defined
// looking forward from their base (skin-tone modifiers, ZWJ chains).
// A naive "skip backwards past continuations" rule would mishandle
// ZWJ-joined sequences where the joining direction matters.
func PrevClusterBoundary(runes []rune, idx int) int { return typeset.PrevClusterBoundary(runes, idx) }

// RegisteredFontFamilies lists the display names of every font family
// registered via SetDefaultFont / LoadFontVariant*, sorted alphabetically.
// Font pickers build their choices from this; the empty default family is
// not included.
func RegisteredFontFamilies() []string { return typeset.RegisteredFontFamilies() }

// RuneAdvance returns the standalone pixel advance of one rune. It is kept
// for compatibility and isolated marker/layout helpers; paragraph layout,
// caret geometry, selection and rendering must use the shaping APIs because a
// rune's advance is not meaningful inside ligatures or complex-script runs.
// Emoji use the provider bitmap width. Continuation runes such as variation
// selectors and ZWJ return zero.
func RuneAdvance(r rune, fontSpec Font) float32 { return typeset.RuneAdvance(r, fontSpec) }

// SetAutoLoadSystemCJK toggles lazy system CJK probing on first font use.
func SetAutoLoadSystemCJK(enabled bool) { typeset.SetAutoLoadSystemCJK(enabled) }

// SetDefaultFontFeatures sets OpenType feature switches applied to all
// text before each Font's own Features (those win for the same tag) — an
// app-wide preference, e.g. {"-calt", "-liga", "-dlig"} so a code font
// installed with SetDefaultFont keeps "!=" as two characters everywhere,
// not only where a Font asks for it. A change drops the text caches and
// re-lays-out every window.
func SetDefaultFontFeatures(features ...string) {
	if !typeset.SetDefaultFontFeatures(features) {
		return
	}
	for _, w := range LiveWindows() {
		w.PostJob(func() {
			w.InvalidateLayout()
			w.Invalidate()
		})
	}
}

// DefaultFontFeatures returns the SetDefaultFontFeatures set.
func DefaultFontFeatures() []string { return typeset.DefaultFontFeatures() }

// SetDefaultFont swaps the entire global default font family in one call.
func SetDefaultFont(family string, regular []byte, weights ...FontFace) error {
	return typeset.SetDefaultFont(family, regular, weights...)
}

// SetEmojiProvider installs the provider used by DrawText for color
// emoji. Pass nil to disable emoji rendering entirely.
func SetEmojiProvider(p EmojiProvider) { typeset.SetEmojiProvider(p) }

// TextCaretBand returns the vertical band a caret — or a selection
// highlight — should span for one line of text drawn into rect with
// DrawText: the face's ascent above the baseline DrawText places, and its
// descent below. That is the text's own line box, so a caret in a tall
// field stays text-sized instead of running edge to edge. Clamped to rect.
func TextCaretBand(rect Rect, fontSpec Font) (float32, float32) {
	return typeset.TextCaretBand(rect, fontSpec)
}

// TextCaretStops returns visual caret positions in left-to-right screen
// order. Offsets remain rune offsets into text.
func TextCaretStops(text string, fontSpec Font, direction TextDirection) []TextCaretStop {
	return typeset.TextCaretStops(text, fontSpec, direction)
}

// TextMetrics returns the shaped bounding box of text rendered at the given
// font. Each logical line is measured as glyph runs, including OpenType
// substitutions/positioning, bidi ordering, spacing and emoji clusters.
// Height comes from the font face metrics.
func TextMetrics(text string, fontSpec Font) (float32, float32) {
	return typeset.TextMetrics(text, fontSpec)
}

// TextOffsetAtX maps an X advance to the nearest grapheme-safe rune offset.
func TextOffsetAtX(text string, fontSpec Font, direction TextDirection, x float32) int {
	return typeset.TextOffsetAtX(text, fontSpec, direction, x)
}

// TextScale rounds font.Size to an integer multiple of the base glyph
// height. Used by legacy bitmap-based paths; opentype paths read the
// size directly. Exported for widgets package use.
func TextScale(font Font) float32 { return typeset.TextScale(font) }

// TextSelectionSegments maps a logical rune range to visual horizontal
// segments. The result is ordered from left to right.
func TextSelectionSegments(text string, fontSpec Font, direction TextDirection, start int, end int) []TextSelectionSegment {
	return typeset.TextSelectionSegments(text, fontSpec, direction, start, end)
}

// TextVisualNeighbor moves one visual caret stop left (-1) or right (+1).
func TextVisualNeighbor(text string, fontSpec Font, direction TextDirection, offset int, delta int) int {
	return typeset.TextVisualNeighbor(text, fontSpec, direction, offset, delta)
}

// TextXForOffset maps a rune offset to its shaped visual X position.
func TextXForOffset(text string, fontSpec Font, direction TextDirection, offset int) float32 {
	return typeset.TextXForOffset(text, fontSpec, direction, offset)
}

// TextXHeightCenterY returns the y at which a single line of text's x-height
// band sits when drawn into rect — i.e. the optical center of a lowercase run.
func TextXHeightCenterY(rect Rect, fontSpec Font) float32 {
	return typeset.TextXHeightCenterY(rect, fontSpec)
}

func ToPoints(px float32) float32 { return typeset.ToPoints(px) }
