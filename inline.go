package qui

// Inline formatting context (IFC) engine.
//
// This is the word-level line-breaking + baseline-alignment engine used to
// lay out a mix of styled text and atomic (replaced / inline-block) boxes on
// shared lines — the missing piece that lets an <img>, a badge, or any
// inline-block element flow *inside* a run of text instead of breaking onto
// its own block line.
//
// It lives in root next to the rich-text engine (text_rich.go) for the same
// reason: text measurement and line breaking are root capabilities, and the
// consumer (widgets.InlineBox) is a thin widget over this pure layout. The
// engine has no notion of widgets — atomic boxes enter as a Size + a
// vertical-align mode and leave as a positioned rectangle keyed by an opaque
// BoxIndex the caller maps back to a child widget.

import (
	"sort"
	"strings"

	"github.com/go-text/typesetting/di"
)

// InlineVAlign selects how an atomic inline box aligns against the line
// baseline. Text always sits on the baseline; boxes can offset.
type InlineVAlign uint8

const (
	// InlineBaseline puts the box's bottom edge on the text baseline (CSS
	// default for replaced elements and inline-block).
	InlineBaseline InlineVAlign = iota
	// InlineMiddle centers the box vertically on the baseline (CSS
	// vertical-align: middle, approximated as baseline-centered).
	InlineMiddle
	// InlineTop / InlineBottom align the box to the line box top / bottom.
	InlineTop
	InlineBottom
)

// InlineItem is one participant in an inline formatting context: either a run
// of styled text or an atomic box.
type InlineItem struct {
	// Text — when non-empty this is a text item. Whitespace collapses: runs
	// of spaces/tabs are single break opportunities, a trailing space at a
	// soft wrap is dropped, and '\n' forces a line break (used for <br>).
	Text            string
	Font            Font
	Color           Color
	Decoration      TextDecoration
	DecorationPaint DecorationPaint
	Href            string
	// LinkSource identifies which link the run belongs to (the element a
	// folded <a> came from), so a click can be traced back to it rather
	// than only to its href. Opaque to the layout; must be comparable —
	// a pointer. Runs only merge into one fragment when it is equal.
	LinkSource any
	// Background, when non-transparent, fills the run's line-ink band
	// behind the glyphs (rich-text highlight). Participates in same-style
	// fragment merging like every other style field.
	Background Color
	// BaselineShift raises (+) or lowers (-) the run's glyphs relative to
	// the line baseline, in px — superscript/subscript. A raised run
	// grows the line's ascent so it never collides with the line above.
	BaselineShift float32

	// Box — when true this is an atomic inline box laid out as one
	// unbreakable unit. Size is its outer box; VAlign chooses vertical
	// alignment; BoxIndex is echoed into each produced box fragment so the
	// caller can map a placement back to its child.
	Box      bool
	Size     Size
	VAlign   InlineVAlign
	BoxIndex int
	// Baseline, when > 0 and VAlign is InlineBaseline, is the distance from the
	// box's top to its own (first-line) text baseline. The IFC then aligns that
	// baseline with the line baseline — so a text-carrying atomic box (e.g. a
	// styled link kept as its own box) sits on the same baseline as the
	// surrounding text instead of resting its bottom edge on it. Zero keeps the
	// legacy bottom-on-baseline behavior (used for non-text boxes like images).
	Baseline float32
}

// InlineBaselineProvider is an optional interface for atomic inline boxes that
// carry text: it reports the box's own text baseline (distance from the box's
// top) so the IFC can baseline-align the box with surrounding text. Hosts that
// build InlineItems (e.g. widgets.InlineBox) query it when adding a box.
type InlineBaselineProvider interface {
	InlineBaseline() float32
}

// InlineLayoutOptions configures line breaking and placement.
type InlineLayoutOptions struct {
	// MaxWidth in logical pixels. <=0 means unbounded (no wrapping).
	MaxWidth float32
	// Wrap enables soft-wrapping at word boundaries when MaxWidth > 0.
	Wrap bool
	// Align controls per-line horizontal alignment within MaxWidth.
	Align TextAlign
	// Direction controls the paragraph base direction. Auto resolves the
	// first strong directional rune.
	Direction TextDirection
	// LineHeightScale is a CSS `line-height`: a multiple of the FONT SIZE.
	// That is what htmlcss's computed property means. <=0 is CSS
	// `line-height: normal` — the face's own height.
	LineHeightScale float32
	// LineSpacing is a WORD PROCESSOR's line spacing: a multiple of the
	// font's natural line height ("one line"), which is what DOCX's w:line
	// lineRule="auto" and Word's/Docs' spacing dropdown count in. It is a
	// different unit from LineHeightScale, not a different spelling of it —
	// see lineBox. Non-zero wins over LineHeightScale.
	LineSpacing float32
	// BaseFont sizes empty lines (a blank line from <br><br>) and the
	// fallback line height when a line carries no text.
	BaseFont Font
	// FirstIndent (px) indents the first visual line of the layout (CSS
	// text-indent, mirroring TextLayoutOptions.FirstIndent): the first
	// line wraps within a width reduced by FirstIndent and its fragments
	// shift right by it. 0 = no indent.
	FirstIndent float32
	// PreserveWhitespace switches the tokenizer from CSS whitespace
	// collapsing to word-processor semantics: every space/tab rune is kept
	// verbatim and stays addressable as a source position, lines still
	// soft-wrap at space boundaries, and a run of spaces at a soft wrap
	// hangs past MaxWidth at the end of the line instead of being dropped
	// (Word/pre-wrap behavior). Spaces render with their own item's style
	// (an underlined run underlines its spaces). This is the mode editing
	// surfaces build on: with it, every fragment's text is an exact rune
	// slice of its source item, so the SourcePos caret/hit-test APIs are
	// exact. TextAlignJustify is a no-op in this mode. A trailing '\n'
	// produces a final empty line (the caret needs a line to sit on).
	PreserveWhitespace bool

	// TabStops are explicit tab positions in px from the layout's left
	// edge, in ascending order. TabInterval is the repeating default used
	// past the last explicit stop (and everywhere when TabStops is empty).
	//
	// When neither is set a '\t' measures as its glyph — the behaviour
	// before tab stops existed, and the right one for UI text. Word
	// processors set TabInterval to their default tab width and let the
	// user add explicit stops on the ruler.
	//
	// TabOrigin is how far the layout's left edge sits to the RIGHT of the
	// grid TabInterval is measured from. A word processor measures its default
	// tab stops from the text column's left edge (the page margin), not from
	// each paragraph's indent — so an indented paragraph's first default stop
	// is the next grid line past its indent, not one full interval past it.
	// Hosts that lay an indented paragraph out in its own space (left edge at
	// the indent) pass that indent here; 0 means the layout's left edge IS the
	// grid origin. Explicit TabStops are always in the layout's own space, so
	// they are unaffected.
	//
	// Only meaningful with PreserveWhitespace: CSS whitespace collapsing
	// folds tabs into ordinary spaces, which is what a browser does.
	TabStops    []TabStop
	TabInterval float32
	TabOrigin   float32

	// Exclusions are rectangles the text must flow AROUND — a floating image
	// with text wrapping beside it, CSS floats, a drop cap. Coordinates are
	// in the layout's own space: x from its left edge, y from its top.
	//
	// A line is cut into the SLOTS the exclusions leave of it (see
	// inlineSpansAt) and fills them left to right: text runs up to a float,
	// continues on its far side, and only then wraps to the next line — Word's
	// "both sides" wrapping and what a browser does with a float. A line with
	// no usable slot at all is pushed below the exclusion. A space at a slot
	// boundary hangs past it, exactly as one at a line break does.
	//
	// No-op when MaxWidth <= 0: without a right edge there is nothing for a
	// band to be measured against.
	Exclusions []Rect

	// BreakLongWords adds a character-level fallback break for a run with no
	// break opportunity in it — a pasted URL, a CJK/no-space string, a row of
	// digits. Word-level wrapping alone can only overflow such a run past
	// MaxWidth (which is CSS overflow-wrap:normal, the default here); with
	// this set the run is split at the last rune that fits, as late as
	// possible, matching CSS overflow-wrap:break-word and what word
	// processors do. Runs that fit on a line of their own are still moved
	// whole — the break only fires when nothing else can help. No-op unless
	// Wrap && MaxWidth > 0.
	BreakLongWords bool
}

// TabAlign is how the text following a tab stop sits against it.
type TabAlign uint8

const (
	// TabLeft starts the following text AT the stop (the default).
	TabLeft TabAlign = iota
	// TabCenter centres the run between this tab and the next one (or the
	// end of the line) on the stop.
	TabCenter
	// TabRight ends that run AT the stop — the shape a table of contents
	// needs for its page-number column.
	TabRight
)

// TabStop is one explicit tab position. Leader is the character painted
// across the gap the tab opens (0 = nothing; '.' gives the dot leaders a
// table of contents draws between a heading and its page number).
type TabStop struct {
	Pos    float32
	Align  TabAlign
	Leader rune
}

// tabStopAfter picks the stop a tab starting at x advances to: the first
// explicit stop past x, else the next multiple of TabInterval measured from
// the last explicit stop (or from the grid origin, TabOrigin left of the
// layout's left edge, when there are none). ok is false when tab expansion is
// switched off.
func tabStopAfter(x float32, opts *InlineLayoutOptions) (TabStop, bool) {
	for _, s := range opts.TabStops {
		if s.Pos > x+0.01 {
			return s, true
		}
	}
	if opts.TabInterval <= 0 {
		return TabStop{}, false
	}
	// The grid starts at the last explicit stop when there is one (stops the
	// user placed on the ruler re-anchor the default run past them), else at
	// the column origin — which is at -TabOrigin in the layout's own space.
	base := -opts.TabOrigin
	if n := len(opts.TabStops); n > 0 && opts.TabStops[n-1].Pos > base {
		base = opts.TabStops[n-1].Pos
	}
	k := 1
	for base+float32(k)*opts.TabInterval <= x+0.01 {
		k++
	}
	return TabStop{Pos: base + float32(k)*opts.TabInterval}, true
}

// tabAdvance resolves where a tab starting at x lands. segW is the width of
// the run that follows it up to the next tab or the end of the line, which
// is what centre- and right-aligned stops position. glyphW is the fallback
// advance when tab expansion is off.
//
// A stop that would move the caret BACKWARDS (the preceding text already
// overran it) collapses to zero width rather than overlapping what is
// already on the line.
func tabAdvance(x, segW, glyphW float32, opts *InlineLayoutOptions) (float32, TabStop) {
	stop, ok := tabStopAfter(x, opts)
	if !ok {
		return x + glyphW, TabStop{}
	}
	target := stop.Pos
	// A stop beyond the end of the line has nowhere to put the text. Real
	// documents carry them: Google Docs writes a table of contents with a
	// right stop at 12000 twips (8.3in) whatever the page is, so on A4 with
	// 1in margins every page number was pushed past the 6.3in text column
	// and wrapped onto a line of its own. Clamping to the line's right edge
	// is what Docs itself renders — and what a browser does with an
	// over-wide tab stop.
	if opts.MaxWidth > 0 && target > opts.MaxWidth {
		target = opts.MaxWidth
	}
	switch stop.Align {
	case TabRight:
		target -= segW
	case TabCenter:
		target -= segW / 2
	}
	if target < x {
		target = x
	}
	return target, stop
}

// inlineLineVMetrics measures one line's vertical extent: how far its tallest
// token reaches above and below the baseline, plus the ink ascent/descent of
// the text on it (an empty line falls back to the reference font, since the
// caret still needs a line to sit on).
func inlineLineVMetrics(toks []inlineTok, idxs []int, scale, spacing, mid, baseAsc, baseDesc, baseHalf float32) (ascent, descent, inkA, inkD float32) {
	for _, ti := range idxs {
		t := toks[ti]
		ab, bb := t.aboveBelow(scale, spacing, mid)
		if ab > ascent {
			ascent = ab
		}
		if bb > descent {
			descent = bb
		}
		if !t.box {
			if fa := inlineAscent(t.font); fa > inkA {
				inkA = fa
			}
			if fd := inlineDescent(t.font); fd > inkD {
				inkD = fd
			}
		}
	}
	if len(idxs) == 0 || (ascent == 0 && descent == 0) {
		ascent = baseAsc + baseHalf
		descent = baseDesc + baseHalf
	}
	if inkA == 0 && inkD == 0 {
		inkA, inkD = baseAsc, baseDesc
	}
	return ascent, descent, inkA, inkD
}

// inlineSpan is one horizontal slot a line can put content in: everything
// between the exclusions that cross the line's own vertical extent.
type inlineSpan struct{ left, width float32 }

// inlineSpansAt returns the free slots left for a line occupying the vertical
// range [top, top+height), in left-to-right order — the complement of the
// exclusions inside [0, MaxWidth).
//
// A float in the MIDDLE of the column leaves two of them, and a line uses
// them in order: text fills the space to the float's left and CONTINUES on
// its right, which is what "wrap text / both sides" means in a word processor
// and what a browser does with a float. Without exclusions there is exactly
// one slot — the whole column — and every line behaves as it did before
// exclusions existed.
func inlineSpansAt(opts *InlineLayoutOptions, top, height float32) []inlineSpan {
	full := []inlineSpan{{left: 0, width: opts.MaxWidth}}
	if opts.MaxWidth <= 0 || len(opts.Exclusions) == 0 {
		return full
	}
	if height <= 0 {
		height = 0.01 // a zero-height probe must still hit an exclusion it starts in
	}
	// The exclusions this line runs into, clipped to the column and sorted by
	// their left edge (insertion sort: there are a handful at most).
	var blocked []inlineSpan
	for _, ex := range opts.Exclusions {
		if ex.W <= 0 || ex.H <= 0 || ex.Y >= top+height || ex.Y+ex.H <= top {
			continue
		}
		a, b := ex.X, ex.X+ex.W
		if b <= 0 || a >= opts.MaxWidth {
			continue
		}
		if a < 0 {
			a = 0
		}
		if b > opts.MaxWidth {
			b = opts.MaxWidth
		}
		i := len(blocked)
		blocked = append(blocked, inlineSpan{left: a, width: b - a})
		for ; i > 0 && blocked[i-1].left > a; i-- {
			blocked[i-1], blocked[i] = blocked[i], blocked[i-1]
		}
	}
	if len(blocked) == 0 {
		return full
	}
	var out []inlineSpan
	x := float32(0)
	for _, b := range blocked {
		if b.left-x > inlineMinSpan {
			out = append(out, inlineSpan{left: x, width: b.left - x})
		}
		if right := b.left + b.width; right > x {
			x = right
		}
	}
	if opts.MaxWidth-x > inlineMinSpan {
		out = append(out, inlineSpan{left: x, width: opts.MaxWidth - x})
	}
	return out
}

// inlineMinSpan is the narrowest gap worth offering a line: anything thinner
// could not hold a character and would only produce a slot every token is
// bounced out of.
const inlineMinSpan = 4

// widestSpan is the roomiest slot in the set (0 when there is none) — what
// "does this line fit beside the floats at all" is measured against.
func widestSpan(spans []inlineSpan) float32 {
	w := float32(0)
	for _, s := range spans {
		if s.width > w {
			w = s.width
		}
	}
	return w
}

// inlineAbs is |v| for the span comparisons (root has no float32 abs).
func inlineAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func sameSpans(a, b []inlineSpan) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if inlineAbs(a[i].left-b[i].left) > 0.01 || inlineAbs(a[i].width-b[i].width) > 0.01 {
			return false
		}
	}
	return true
}

// inlineBandBottom is the lowest bottom edge among the exclusions a line at
// [top, top+height) runs into — where the line has to move to get clear of
// the shallowest of them. ok is false when nothing intersects.
func inlineBandBottom(opts *InlineLayoutOptions, top, height float32) (float32, bool) {
	if height <= 0 {
		height = 0.01
	}
	bottom, ok := float32(0), false
	for _, ex := range opts.Exclusions {
		if ex.W <= 0 || ex.H <= 0 || ex.Y >= top+height || ex.Y+ex.H <= top {
			continue
		}
		if b := ex.Y + ex.H; !ok || b < bottom {
			bottom, ok = b, true
		}
	}
	return bottom, ok
}

// SourcePos addresses a rune boundary in the item stream a layout was built
// from: Item indexes the []InlineItem slice passed to BuildInlineLayout, Off
// is a rune offset within that item's Text. Atomic boxes span the synthetic
// range [0,1] — Off 0 is "before the box", 1 is "after it". A SourcePos
// between two runes is a caret position; editors map their model offsets
// onto (item, offset) pairs and back.
type SourcePos struct {
	Item int
	Off  int
}

// Cmp orders p against q in source order: -1 when p precedes q, 0 when
// equal, +1 when p follows q.
func (p SourcePos) Cmp(q SourcePos) int {
	switch {
	case p.Item != q.Item:
		if p.Item < q.Item {
			return -1
		}
		return 1
	case p.Off != q.Off:
		if p.Off < q.Off {
			return -1
		}
		return 1
	}
	return 0
}

// InlineFrag is one positioned piece of an inline layout: a drawable text run
// or an atomic box placeholder. Rect is relative to the layout origin (0,0 at
// the top-left). For a text frag the rect height is the glyph ink box (ascent
// + descent) so DrawText centers the glyphs on the intended baseline.
type InlineFrag struct {
	Text            string
	Font            Font
	Color           Color
	Decoration      TextDecoration
	DecorationPaint DecorationPaint
	Href            string
	LinkSource      any
	Background      Color
	BaselineShift   float32

	Box      bool
	BoxIndex int

	// Tab marks the fragment as one expanded '\t': its Text is the single
	// tab rune but its width is the distance to the tab stop, so the
	// per-rune advance assumption the source-mapping helpers make about
	// ordinary text does not hold. Leader is the character painted across
	// it (0 = nothing).
	Tab    bool
	Leader rune

	Rect Rect

	// Source mapping. SrcItem is the item this fragment's text came from
	// (-1 when the fragment is not an exact source slice — only possible
	// in collapse mode when a synthesized joining space leads the
	// fragment). SrcStart/SrcEnd are rune offsets into that item's Text;
	// boxes span the synthetic range [0,1]. Invariant when SrcItem >= 0:
	// the first (SrcEnd-SrcStart) runes of Text equal the source slice
	// [SrcStart:SrcEnd) — in PreserveWhitespace mode the whole Text does
	// (fragments there merge only when source-contiguous); in collapse
	// mode same-style merging may append visually-identical text from a
	// non-contiguous source, which extends Text without extending SrcEnd.
	SrcItem  int
	SrcStart int
	SrcEnd   int
}

// InlineLine is one laid-out line of an inline layout.
type InlineLine struct {
	Frags    []InlineFrag
	Y        float32 // top of the line box, relative to origin
	Baseline float32 // baseline, relative to origin
	Width    float32
	Height   float32

	// InkAscent/InkDescent are the line's TEXT ink extents above/below
	// the baseline — glyph ascent+descent only, excluding line-height
	// leading and atomic-box extents. This is the caret height editors
	// want (a caret spanning the leaded line box reads far too tall, and
	// one stretched to an inline image reads wrong next to text). Lines
	// without text fall back to BaseFont's extents.
	InkAscent  float32
	InkDescent float32

	// SrcStart/SrcEnd bound the source range this line consumed. A soft
	// wrap makes line N's SrcEnd meet (or, in collapse mode where the
	// wrap space is dropped, precede) line N+1's SrcStart; a hard '\n'
	// break leaves the newline rune in the gap between the two lines.
	SrcStart SourcePos
	SrcEnd   SourcePos
}

// InlineLayout is the immutable output of BuildInlineLayout.
type InlineLayout struct {
	Lines  []InlineLine
	Width  float32
	Height float32
}

// inlineTok is one break unit: a word, an atomic box, a run of preserved
// whitespace (PreserveWhitespace mode only), or a forced break.
type inlineTok struct {
	forceBreak bool

	box      bool
	boxIndex int
	valign   InlineVAlign
	size     Size
	baseline float32 // box's own text baseline from top (0 = none)

	text       string
	font       Font
	color      Color
	deco       TextDecoration
	decoPaint  DecorationPaint
	href       string
	linkSrc    any
	background Color
	shift      float32
	width      float32

	// space marks a preserved-whitespace run token (PreserveWhitespace
	// mode): it never triggers a soft wrap itself — at a wrap boundary it
	// hangs past MaxWidth at the end of the line (Word semantics).
	space bool

	// tab marks a single '\t' token. Its width is not intrinsic — it is
	// resolved against the tab stops from wherever the token lands, in
	// both the line-breaking and the placement pass.
	tab bool

	// Source range: the token's runes are items[srcItem].Text[srcStart:
	// srcEnd] (rune offsets). Boxes span [0,1]; forced breaks cover their
	// '\n' rune.
	srcItem  int
	srcStart int
	srcEnd   int

	// spaceBefore marks a collapsible space between this token and the
	// previous one; spaceWidth is that space's advance. A leading space is
	// dropped when the token starts a line. The space* style fields record
	// the styling of the ITEM the space character came from, so placement
	// can attribute it to the matching neighbor (the space before a link
	// is not underlined; the space after one isn't either).
	spaceBefore    bool
	spaceWidth     float32
	spaceFont      Font
	spaceColor     Color
	spaceDeco      TextDecoration
	spaceDecoPaint DecorationPaint
	spaceHref      string
	spaceLinkSrc   any
	spaceBg        Color
	// Source range of the collapsed whitespace run the joining space
	// stands for, within the run's LAST contributing item (best-effort:
	// a run spanning items keeps only the final item's slice, matching
	// the style attribution above).
	spaceSrcItem  int
	spaceSrcStart int
	spaceSrcEnd   int
}

// BuildInlineLayout lays out a mix of text runs and atomic boxes into lines.
func BuildInlineLayout(items []InlineItem, opts InlineLayoutOptions) InlineLayout {
	// The two line-height units travel together so every metric below
	// resolves them the same way (see lineBox).
	scale, spacing := opts.LineHeightScale, opts.LineSpacing
	toks := tokenizeInline(items, opts.PreserveWhitespace)
	paragraphRunes := make([]rune, 0, 64)
	for _, item := range items {
		if item.Box {
			paragraphRunes = append(paragraphRunes, '\uFFFC')
		} else {
			paragraphRunes = append(paragraphRunes, []rune(item.Text)...)
		}
	}
	paragraphRTL := resolveTextDirection(paragraphRunes, opts.Direction) == di.DirectionRTL

	maxW := opts.MaxWidth
	wrap := opts.Wrap && maxW > 0

	// Vertical metrics of the reference (paragraph) font. They size an empty
	// line, and — with exclusions in play — they are also the height a line
	// is PROBED with before its own tokens are known (see the span tracking
	// below). mid is how far above the baseline the reference font's ink
	// center sits; vertical-align:middle boxes are centered there so they
	// read as optically centered with the surrounding text.
	baseAsc := inlineAscent(opts.BaseFont)
	baseDesc := inlineDescent(opts.BaseFont)
	baseHalf := inlineHalfLeading(opts.BaseFont, scale, spacing)
	mid := inlineMidRef(opts.BaseFont)
	strut := baseAsc + baseDesc + 2*baseHalf

	// Pass 1 — greedy line breaking into token-index groups. Each line also
	// records whether it ends "hard" (forced break / end of content) —
	// justify stretches only soft-wrapped lines — and the source range it
	// consumed.
	//
	// With exclusions the pass is also VERTICAL and SEGMENTED: a line's
	// usable slots depend on where it sits, so its top, height and spans are
	// resolved here and handed to pass 2 rather than recomputed there (the
	// two must agree, or text lands where it was not measured). A line with
	// a float in the middle of it has two slots and fills them in order.
	var (
		lineIdx                                        [][]int        // every token on the line, for its metrics
		lineSegs                                       [][]inlineSpan // the slots the line used
		lineSegToks                                    [][][]int      // tokens per slot
		lineSegW                                       [][]float32    // natural content width per slot
		lineHard                                       []bool
		lineSrc                                        [][2]SourcePos
		lineInd                                        []float32
		lineTop, lineAsc, lineDesc, lineInkA, lineInkD []float32
	)
	// State of the line being filled.
	var (
		cur     []int
		segs    []inlineSpan
		segToks [][]int
		segW    []float32
		seg     int
		y       float32
	)
	curProbeH := strut
	// Without exclusions every line has the same single slot, so it is built
	// once and shared: line breaking must not pay for a feature the layout
	// does not use.
	noExcl := len(opts.Exclusions) == 0 || maxW <= 0
	fullSpans := []inlineSpan{{left: 0, width: maxW}}
	spansAt := func(top, height float32) []inlineSpan {
		if noExcl {
			return fullSpans
		}
		return inlineSpansAt(&opts, top, height)
	}
	resetSpans := func(spans []inlineSpan) {
		segs = spans
		segToks = make([][]int, len(segs))
		segW = make([]float32, len(segs))
		seg = 0
	}
	resetSpans(spansAt(0, strut))
	// startLine re-probes the slots for a line about to begin at y, pushing it
	// below whatever it cannot fit beside. needH is the height of the first
	// token going on it — a line carrying a tall inline image sees floats a
	// text-sized probe would pass under.
	startLine := func(needW, needH float32) {
		curProbeH = strut
		if needH > curProbeH {
			curProbeH = needH
		}
		spans := spansAt(y, curProbeH)
		if !noExcl {
			// Nothing on this line can fit beside the floats: drop below the
			// shallowest of them and look again.
			for range opts.Exclusions {
				w := widestSpan(spans)
				if w > 0 && (needW <= 0 || needW <= w || w >= maxW) {
					break
				}
				bottom, ok := inlineBandBottom(&opts, y, curProbeH)
				if !ok || bottom <= y {
					break
				}
				y = bottom
				spans = spansAt(y, curProbeH)
			}
		}
		resetSpans(spans)
		// Start in the first slot that can hold what is going on the line: the
		// leftmost one when it fits (where reading and typing start), the next
		// one along when the float leaves too little room there.
		if needW > 0 && len(segs) > 1 {
			widest, chosen := 0, -1
			for i, sp := range segs {
				if sp.width >= needW {
					chosen = i
					break
				}
				if sp.width > segs[widest].width {
					widest = i
				}
			}
			if chosen >= 0 {
				seg = chosen
			} else {
				seg = widest
			}
		}
	}
	srcCursor := SourcePos{} // next unconsumed source position
	lineStart := srcCursor
	endLine := func(hard bool) {
		ind := float32(0)
		if len(lineIdx) == 0 {
			ind = opts.FirstIndent
		}
		asc, desc, inkA, inkD := inlineLineVMetrics(toks, cur, scale, spacing, mid, baseAsc, baseDesc, baseHalf)
		lineIdx = append(lineIdx, cur)
		lineSegs = append(lineSegs, segs)
		lineSegToks = append(lineSegToks, segToks)
		lineSegW = append(lineSegW, segW)
		lineHard = append(lineHard, hard)
		lineSrc = append(lineSrc, [2]SourcePos{lineStart, srcCursor})
		lineInd = append(lineInd, ind)
		lineTop = append(lineTop, y)
		lineAsc, lineDesc = append(lineAsc, asc), append(lineDesc, desc)
		lineInkA, lineInkD = append(lineInkA, inkA), append(lineInkD, inkD)
		y += asc + desc
		cur = nil
	}
	// segAvail is what is left of the current slot for new content: its width,
	// less the text-indent when this is the first slot of the first line.
	segAvail := func() float32 {
		if seg >= len(segs) {
			return 0
		}
		w := segs[seg].width
		if len(lineIdx) == 0 && seg == 0 {
			w -= opts.FirstIndent
		}
		return w
	}
	// segIndent is the shift the current slot's content carries (the same
	// value pass 2 applies), which tab stops have to be resolved against.
	segIndent := func() float32 {
		if len(lineIdx) == 0 && seg == 0 {
			return opts.FirstIndent
		}
		return 0
	}
	endsWithBreak := false
	// nToks is captured up front: the character-level fallback appends the
	// halves of a split run to toks and references them from cur directly
	// (lines address tokens by index, so appending never disturbs indices
	// already recorded). The loop must not walk into those appended pieces.
	nToks := len(toks)
	// growProbe widens the height a line is measured against as a taller token
	// joins it. If that changes the slots the line already put content in, the
	// line ends instead: content is measured against the slots it is placed
	// in, and a mid-line re-slot would break that. Returns true when the
	// caller must break before this token.
	growProbe := func(t inlineTok) bool {
		if noExcl {
			return false
		}
		ab, bb := t.aboveBelow(scale, spacing, mid)
		h := ab + bb
		if h <= curProbeH {
			return false
		}
		spans := spansAt(y, h)
		if sameSpans(spans, segs) {
			curProbeH = h
			return false
		}
		if len(cur) == 0 {
			curProbeH = h
			resetSpans(spans)
			return false
		}
		return true
	}
	// place puts a token in the current slot.
	place := func(ti int, gap, contentW float32) {
		segToks[seg] = append(segToks[seg], ti)
		segW[seg] += gap + contentW
		cur = append(cur, ti)
	}
	for ti := 0; ti < nToks; ti++ {
		t := toks[ti]
		if t.forceBreak {
			endLine(true)
			srcCursor = SourcePos{Item: t.srcItem, Off: t.srcEnd}
			lineStart = srcCursor
			endsWithBreak = true
			startLine(0, 0)
			continue
		}
		endsWithBreak = false
		contentW := t.width
		if t.box {
			contentW = t.size.W
		}
		if len(cur) == 0 {
			ab, bb := t.aboveBelow(scale, spacing, mid)
			startLine(contentW, ab+bb)
		} else if growProbe(t) {
			endLine(false)
			ab, bb := t.aboveBelow(scale, spacing, mid)
			startLine(contentW, ab+bb)
			lineStart = SourcePos{Item: t.srcItem, Off: t.srcStart}
			srcCursor = lineStart
		}
		gap := float32(0)
		if len(segToks[seg]) > 0 && t.spaceBefore {
			gap = t.spaceWidth
		}
		if t.tab {
			// The tab's advance has to be resolved HERE as well as at
			// placement: a right-aligned stop pulls the following run back
			// to the stop, and a line-breaking pass that measured the tab
			// as its glyph would wrap a table-of-contents line that
			// actually fits.
			x := segW[seg] + gap + segIndent()
			end, _ := tabAdvance(x, tokRunWidth(toks, ti+1, nToks), t.width, &opts)
			contentW = end - x
		}
		// Character-level fallback (CSS overflow-wrap:break-word): a run with
		// no break opportunity inside it and wider than a whole slot can only
		// overflow under word-level wrapping — a pasted URL or a long
		// space-free string runs off the edge. Split it at the last rune that
		// fits, filling the current slot first (break as late as possible),
		// and let the surviving tail fall through to the word-level path.
		// Runs narrower than a slot are never touched: those still move whole.
		if wrap && opts.BreakLongWords && !t.box && !t.space && contentW > segAvail() {
			rem, remW := t, contentW
			for {
				avail := segAvail() - segW[seg]
				if len(segToks[seg]) > 0 && rem.spaceBefore {
					avail -= rem.spaceWidth
				}
				if remW <= avail {
					break // the tail fits as-is
				}
				head, tail, ok := splitInlineTok(rem, avail, len(segToks[seg]) == 0)
				if !ok {
					if len(cur) == 0 {
						// A single glyph wider than the whole slot (or a
						// one-rune run): nothing left to break, let it
						// overflow. Also the loop's termination guarantee —
						// never end an empty line and retry.
						break
					}
					// Not even one rune fits in what is left here; move to the
					// next slot (or a fresh line), where the split is forced
					// to yield at least one rune.
					if seg+1 < len(segs) {
						seg++
					} else {
						endLine(false)
						startLine(0, 0)
					}
					lineStart = SourcePos{Item: rem.srcItem, Off: rem.srcStart}
					srcCursor = lineStart
					continue
				}
				headGap := float32(0)
				if len(segToks[seg]) > 0 && head.spaceBefore {
					headGap = head.spaceWidth
				}
				toks = append(toks, head)
				place(len(toks)-1, headGap, head.width)
				srcCursor = SourcePos{Item: head.srcItem, Off: head.srcEnd}
				if seg+1 < len(segs) {
					seg++
				} else {
					endLine(false)
					startLine(0, 0)
				}
				rem, remW = tail, tail.width
				lineStart = SourcePos{Item: tail.srcItem, Off: tail.srcStart}
				srcCursor = lineStart
			}
			gapNow := float32(0)
			if len(segToks[seg]) > 0 && rem.spaceBefore {
				gapNow = rem.spaceWidth
			}
			toks = append(toks, rem)
			place(len(toks)-1, gapNow, remW)
			srcCursor = SourcePos{Item: rem.srcItem, Off: rem.srcEnd}
			continue
		}
		// Preserved whitespace never wraps by itself: at the boundary it
		// hangs past the slot's edge at the end of the current one.
		if wrap && len(cur) > 0 && !t.space && segW[seg]+gap+contentW > segAvail() {
			// Round the float first: the rest of this line continues in the
			// next slot that can hold the token, and only when there is none
			// does the line end. A space at a slot boundary is dropped, the
			// same as at a line wrap.
			for seg+1 < len(segs) {
				seg++
				gap = 0
				if contentW <= segAvail() {
					break
				}
			}
			if segW[seg]+gap+contentW > segAvail() {
				endLine(false)
				ab, bb := t.aboveBelow(scale, spacing, mid)
				startLine(contentW, ab+bb)
				gap = 0
				lineStart = SourcePos{Item: t.srcItem, Off: t.srcStart}
			}
			place(ti, gap, contentW)
			srcCursor = SourcePos{Item: t.srcItem, Off: t.srcEnd}
			continue
		}
		place(ti, gap, contentW)
		srcCursor = SourcePos{Item: t.srcItem, Off: t.srcEnd}
	}
	if len(cur) > 0 || (opts.PreserveWhitespace && endsWithBreak) {
		endLine(true)
	}
	if len(lineIdx) == 0 {
		endLine(true)
	}

	// Pass 2 — fragment placement, on the geometry pass 1 resolved. Each slot
	// of a line is placed on its own and shifted to where that slot sits.
	out := InlineLayout{}
	var maxLineW float32
	for li := range lineIdx {
		ascent, descent := lineAsc[li], lineDesc[li]
		lineY := lineTop[li]
		baseline := lineY + ascent

		var frags []InlineFrag
		lineW := float32(0)
		for si, segIdxs := range lineSegToks[li] {
			if len(segIdxs) == 0 {
				continue
			}
			span := lineSegs[li][si]
			ind := float32(0)
			if si == 0 {
				ind = lineInd[li]
			}
			avail := span.width - ind

			// text-align: justify — stretch soft-wrapped content to the slot
			// width by widening each inter-token gap. The extra also rides on
			// the frag fonts' WordSpacing so drawn space glyphs (and selection
			// measurement over them) advance exactly like the placement did.
			// A slot the text was pushed out of is full by definition, so it
			// justifies even on the line that ends the paragraph.
			justify := float32(0)
			stretch := !lineHard[li] || si < len(lineSegToks[li])-1
			if opts.Align == TextAlignJustify && wrap && stretch {
				gaps := 0
				for pos, ti := range segIdxs {
					if pos > 0 && toks[ti].spaceBefore {
						gaps++
					}
				}
				if gaps > 0 && lineSegW[li][si] < avail {
					justify = (avail - lineSegW[li][si]) / float32(gaps)
				}
			}

			segFrags, segWidth := placeInlineLine(toks, segIdxs, baseline, mid, justify, ind,
				opts.PreserveWhitespace, &opts)
			// The slot's left edge and the first-line indent are both a shift
			// of the whole slot. Tab stops stay measured from the layout's
			// left edge (Word keeps them paragraph-relative), so content
			// pushed aside by an exclusion carries its tabbed columns with it.
			shift := span.left + ind
			if paragraphRTL && ind > 0 {
				// The first-line indent hangs off the side the paragraph
				// STARTS at, which right-to-left means the right edge. The
				// line already lost `ind` of width above; pushing it right by
				// `ind` as well would put the gap on the left and hang the
				// text past the right margin.
				shift = span.left
			}
			// Centre / right alignment happens INSIDE the slot: a centred line
			// beside a floating image centres in what is left of the column.
			alignRight := (opts.Align == TextAlignEnd && !paragraphRTL) || (opts.Align == TextAlignStart && paragraphRTL)
			if opts.MaxWidth > 0 && (opts.Align == TextAlignCenter || alignRight) {
				if off := avail - segWidth; off > 0 {
					if opts.Align == TextAlignCenter {
						off *= 0.5
					}
					shift += off
				}
			}
			for fi := range segFrags {
				segFrags[fi].Rect.X += shift
			}
			frags = append(frags, segFrags...)
			if right := shift + segWidth; right > lineW {
				lineW = right
			}
		}
		if lineW > maxLineW {
			maxLineW = lineW
		}
		out.Lines = append(out.Lines, InlineLine{
			Frags:      frags,
			Y:          lineY,
			Baseline:   baseline,
			Width:      lineW,
			Height:     ascent + descent,
			InkAscent:  lineInkA[li],
			InkDescent: lineInkD[li],
			SrcStart:   lineSrc[li][0],
			SrcEnd:     lineSrc[li][1],
		})
	}
	out.Height = y
	out.Width = maxLineW

	// Unbounded layouts have no column to align against, so they align to the
	// widest line instead (the bounded case aligned inside its slots above).
	alignRight := (opts.Align == TextAlignEnd && !paragraphRTL) || (opts.Align == TextAlignStart && paragraphRTL)
	if opts.MaxWidth <= 0 && (opts.Align == TextAlignCenter || alignRight) {
		for li := range out.Lines {
			off := maxLineW - out.Lines[li].Width
			if opts.Align == TextAlignCenter {
				off *= 0.5
			}
			if off <= 0 {
				continue
			}
			for fi := range out.Lines[li].Frags {
				out.Lines[li].Frags[fi].Rect.X += off
			}
		}
	}
	return out
}

// aboveBelow returns the token's extent above and below the baseline. mid is
// the reference font's ink-center offset above the baseline (used to size
// vertical-align:middle boxes so the line grows symmetrically around them).
func (t inlineTok) aboveBelow(scale, spacing, mid float32) (above, below float32) {
	if t.box {
		switch t.valign {
		case InlineMiddle:
			// Box center sits `mid` above the baseline.
			above = t.size.H/2 + mid
			below = t.size.H/2 - mid
			if below < 0 {
				below = 0
			}
			return above, below
		default: // baseline / top / bottom
			if t.valign == InlineBaseline && t.baseline > 0 {
				// Box aligned by its own text baseline: it extends `baseline`
				// above the line baseline and the remainder below.
				below = t.size.H - t.baseline
				if below < 0 {
					below = 0
				}
				return t.baseline, below
			}
			return t.size.H, 0 // bottom on baseline
		}
	}
	fa := inlineAscent(t.font)
	fd := inlineDescent(t.font)
	half := inlineHalfLeading(t.font, scale, spacing)
	above, below = fa+half, fd+half
	if t.shift > 0 {
		above += t.shift
	} else {
		below -= t.shift
	}
	return above, below
}

// placeInlineLine positions one line's tokens at the given baseline, merging
// adjacent same-style text into single fragments (so decoration / selection /
// link hit-testing stay continuous). justify is the extra advance added to
// every inter-token gap (text-align: justify); it is also folded into the
// text fonts' WordSpacing so the drawn space glyphs match the placement.
// Returns the fragments and line width.
//
// Source bookkeeping: each fragment's Src range extends only while the
// appended text remains an exact rune slice of its source item; a merge from
// a non-contiguous source keeps the visual merge (collapse mode) but freezes
// SrcEnd, and a synthesized leading space that isn't the verbatim source
// rune marks the fragment SrcItem = -1. In preserve mode merging itself
// requires source contiguity, so fragments there are always exact.
// ind is the text-indent the caller will shift the finished line by; tab
// stops are measured from the layout's left edge, so the placement pass has
// to know about it before the shift happens.
func placeInlineLine(toks []inlineTok, idxs []int, baseline, mid, justify, ind float32, preserve bool, opts *InlineLayoutOptions) ([]InlineFrag, float32) {
	logicalIdxs := idxs
	idxs = visualInlineTokenOrder(toks, idxs, opts)
	reordered := !sameTokenOrder(logicalIdxs, idxs)
	var frags []InlineFrag
	var x float32
	for pos, ti := range idxs {
		t := toks[ti]
		if pos > 0 && !t.spaceBefore {
			prev := toks[idxs[pos-1]]
			if prev.spaceBefore {
				t.spaceBefore = true
				t.spaceWidth = prev.spaceWidth
				t.spaceFont = prev.spaceFont
				t.spaceColor = prev.spaceColor
				t.spaceDeco = prev.spaceDeco
				t.spaceDecoPaint = prev.spaceDecoPaint
				t.spaceHref = prev.spaceHref
				t.spaceLinkSrc = prev.spaceLinkSrc
				t.spaceBg = prev.spaceBg
				t.spaceSrcItem = prev.spaceSrcItem
				t.spaceSrcStart = prev.spaceSrcStart
				t.spaceSrcEnd = prev.spaceSrcEnd
			}
		}
		if justify != 0 && !t.box {
			// Copy-adjust the fonts BEFORE any style comparison below, so
			// same-style merging still sees equal fonts on a justified line.
			t.font.WordSpacing += justify
			t.spaceFont.WordSpacing += justify
		}
		gap := float32(0)
		if pos > 0 && t.spaceBefore {
			gap = t.spaceWidth + justify
		}
		if t.tab {
			start := x + gap
			end, stop := tabAdvance(start+ind, lineRunWidth(toks, idxs, pos+1, justify), t.width, opts)
			end -= ind
			fa, fd := inlineAscent(t.font), inlineDescent(t.font)
			frags = append(frags, InlineFrag{
				Text:          "\t",
				Tab:           true,
				Leader:        stop.Leader,
				Font:          t.font,
				Color:         t.color,
				Background:    t.background,
				Href:          t.href,
				LinkSource:    t.linkSrc,
				BaselineShift: t.shift,
				Rect:          Rect{X: start, Y: baseline - fa - t.shift, W: end - start, H: fa + fd},
				SrcItem:       t.srcItem,
				SrcStart:      t.srcStart,
				SrcEnd:        t.srcEnd,
			})
			x = end
			continue
		}
		if t.box {
			var by float32
			switch t.valign {
			case InlineMiddle:
				// Center the box on the reference ink midline (baseline - mid).
				by = baseline - mid - t.size.H/2
			default:
				if t.valign == InlineBaseline && t.baseline > 0 {
					by = baseline - t.baseline // align the box's own baseline
				} else {
					by = baseline - t.size.H // bottom on baseline
				}
			}
			frags = append(frags, InlineFrag{
				Box:      true,
				BoxIndex: t.boxIndex,
				Rect:     Rect{X: x + gap, Y: by, W: t.size.W, H: t.size.H},
				SrcItem:  t.srcItem,
				SrcStart: t.srcStart,
				SrcEnd:   t.srcEnd,
			})
			x += gap + t.size.W
			continue
		}
		fa := inlineAscent(t.font)
		fd := inlineDescent(t.font)
		ink := fa + fd
		ty := baseline - fa - t.shift
		// A joining space renders with the style of the item it came from
		// (browser semantics): the space before a link isn't underlined,
		// and the space after one isn't either. Attach it to the previous
		// fragment when their styles match; otherwise it stays with this
		// word (matching the common "plain space before styled word" and
		// "styled word after plain space" both resolve correctly because
		// the space's source is the plain run).
		if gap > 0 {
			if n := len(frags); n > 0 && !frags[n-1].Box && !frags[n-1].Tab &&
				fontStyleEqual(frags[n-1].Font, t.spaceFont) &&
				frags[n-1].Color == t.spaceColor &&
				frags[n-1].Decoration == t.spaceDeco &&
				frags[n-1].DecorationPaint == t.spaceDecoPaint &&
				frags[n-1].Href == t.spaceHref &&
				frags[n-1].LinkSource == t.spaceLinkSrc &&
				frags[n-1].Background == t.spaceBg &&
				frags[n-1].BaselineShift == 0 {
				prev := &frags[n-1]
				prev.Text += " "
				prev.Rect.W += gap
				// Extend the source range only when the appended " " is
				// the verbatim source rune right after the fragment.
				if prev.SrcItem == t.spaceSrcItem && prev.SrcEnd == t.spaceSrcStart &&
					t.spaceSrcEnd-t.spaceSrcStart == 1 {
					prev.SrcEnd = t.spaceSrcEnd
				}
				x += gap
				gap = 0
			}
		}
		// Merge into the previous fragment when it is same-style text. In
		// preserve mode the merge additionally requires source contiguity
		// so fragment text stays an exact source slice.
		if n := len(frags); n > 0 && !frags[n-1].Box && !frags[n-1].Tab &&
			fontStyleEqual(frags[n-1].Font, t.font) &&
			frags[n-1].Color == t.color &&
			frags[n-1].Decoration == t.deco &&
			frags[n-1].DecorationPaint == t.decoPaint &&
			frags[n-1].Href == t.href &&
			frags[n-1].LinkSource == t.linkSrc &&
			frags[n-1].Background == t.background &&
			frags[n-1].BaselineShift == t.shift {
			contig := frags[n-1].SrcItem == t.srcItem && frags[n-1].SrcEnd == t.srcStart
			if (!preserve && !reordered) || contig {
				prev := &frags[n-1]
				prev.Text += t.text
				prev.Rect.W += t.width
				if contig {
					prev.SrcEnd = t.srcEnd
				}
				x += t.width
				continue
			}
		}
		text := t.text
		w := t.width
		srcItem, srcStart, srcEnd := t.srcItem, t.srcStart, t.srcEnd
		if gap > 0 {
			// Line/box-leading space with no text fragment to host it.
			text = " " + t.text
			w = gap + t.width
			if t.spaceSrcItem == t.srcItem && t.spaceSrcEnd == t.srcStart &&
				t.spaceSrcEnd-t.spaceSrcStart == 1 {
				srcStart = t.spaceSrcStart // " word" is the verbatim slice
			} else {
				srcItem = -1 // synthesized space breaks the slice invariant
			}
		}
		frags = append(frags, InlineFrag{
			Text:            text,
			Font:            t.font,
			Color:           t.color,
			Decoration:      t.deco,
			DecorationPaint: t.decoPaint,
			Href:            t.href,
			LinkSource:      t.linkSrc,
			Background:      t.background,
			BaselineShift:   t.shift,
			Rect:            Rect{X: x, Y: ty, W: w, H: ink},
			SrcItem:         srcItem,
			SrcStart:        srcStart,
			SrcEnd:          srcEnd,
		})
		x += w
	}
	return frags, x
}

func visualInlineTokenOrder(toks []inlineTok, idxs []int, opts *InlineLayoutOptions) []int {
	if len(idxs) < 2 || opts == nil {
		return idxs
	}
	type tokenRange struct {
		index      int
		start, end int
		x          float32
	}
	text := make([]rune, 0, len(idxs)*4)
	ranges := make([]tokenRange, 0, len(idxs))
	for _, index := range idxs {
		tok := toks[index]
		if tok.spaceBefore && len(text) > 0 {
			text = append(text, ' ')
		}
		start := len(text)
		switch {
		case tok.box:
			text = append(text, '\uFFFC')
		case tok.tab:
			text = append(text, '\t')
		default:
			text = append(text, []rune(tok.text)...)
		}
		ranges = append(ranges, tokenRange{index: index, start: start, end: len(text)})
	}
	if len(text) == 0 {
		return idxs
	}
	stops := TextCaretStops(string(text), opts.BaseFont, opts.Direction)
	caretX := func(offset int) float32 {
		bestX := float32(0)
		bestDistance := int(^uint(0) >> 1)
		for _, stop := range stops {
			distance := stop.Offset - offset
			if distance < 0 {
				distance = -distance
			}
			if distance < bestDistance {
				bestDistance = distance
				bestX = stop.X
			}
			if distance == 0 {
				break
			}
		}
		return bestX
	}
	for i := range ranges {
		x0, x1 := caretX(ranges[i].start), caretX(ranges[i].end)
		if x1 < x0 {
			x0 = x1
		}
		ranges[i].x = x0
	}
	sort.SliceStable(ranges, func(i, j int) bool { return ranges[i].x < ranges[j].x })
	visual := make([]int, len(ranges))
	for i := range ranges {
		visual[i] = ranges[i].index
	}
	return visual
}

func sameTokenOrder(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// tokenizeInline breaks the item stream into words / boxes / forced breaks,
// carrying collapsible whitespace as spaceBefore on the following token. In
// preserve mode whitespace runs become real tokens instead (see
// tokenizePreserve).
func tokenizeInline(items []InlineItem, preserve bool) []inlineTok {
	if preserve {
		return tokenizePreserve(items)
	}
	var toks []inlineTok
	pendingSpace := false
	var spaceSrc InlineItem // item the pending space character came from
	var spaceSrcItem, spaceSrcStart, spaceSrcEnd int
	stampSpace := func(tok *inlineTok) {
		if !pendingSpace {
			return
		}
		tok.spaceBefore = true
		tok.spaceWidth, _ = TextMetrics(" ", spaceSrc.Font)
		tok.spaceFont = spaceSrc.Font
		tok.spaceColor = spaceSrc.Color
		tok.spaceDeco = spaceSrc.Decoration
		tok.spaceDecoPaint = spaceSrc.DecorationPaint
		tok.spaceHref = spaceSrc.Href
		tok.spaceLinkSrc = spaceSrc.LinkSource
		tok.spaceBg = spaceSrc.Background
		tok.spaceSrcItem = spaceSrcItem
		tok.spaceSrcStart = spaceSrcStart
		tok.spaceSrcEnd = spaceSrcEnd
	}
	for ii, it := range items {
		if it.Box {
			tok := inlineTok{box: true, boxIndex: it.BoxIndex, valign: it.VAlign, size: it.Size, baseline: it.Baseline,
				srcItem: ii, srcStart: 0, srcEnd: 1}
			stampSpace(&tok)
			toks = append(toks, tok)
			pendingSpace = false
			continue
		}
		runes := []rune(it.Text)
		i := 0
		for i < len(runes) {
			r := runes[i]
			if r == '\n' {
				toks = append(toks, inlineTok{forceBreak: true, srcItem: ii, srcStart: i, srcEnd: i + 1})
				pendingSpace = false
				i++
				continue
			}
			if isInlineSpace(r) {
				pendingSpace = true
				spaceSrc = it
				spaceSrcItem, spaceSrcStart = ii, i
				i++
				for i < len(runes) && isInlineSpace(runes[i]) {
					i++
				}
				spaceSrcEnd = i
				continue
			}
			start := i
			for i < len(runes) && runes[i] != '\n' && !isInlineSpace(runes[i]) {
				i++
			}
			word := runes[start:i]
			breakStart := 0
			for _, boundary := range lineBreakBoundaries(word) {
				if boundary.offset <= breakStart {
					continue
				}
				segment := word[breakStart:boundary.offset]
				tok := inlineTok{
					text:       string(segment),
					font:       it.Font,
					color:      it.Color,
					deco:       it.Decoration,
					decoPaint:  it.DecorationPaint,
					href:       it.Href,
					linkSrc:    it.LinkSource,
					background: it.Background,
					shift:      it.BaselineShift,
					width:      measureRunes(segment, it.Font, nil),
					srcItem:    ii,
					srcStart:   start + breakStart,
					srcEnd:     start + boundary.offset,
				}
				stampSpace(&tok)
				toks = append(toks, tok)
				pendingSpace = false
				breakStart = boundary.offset
			}
		}
	}
	return toks
}

// tokenizePreserve is the PreserveWhitespace tokenizer: every rune is kept
// and addressable. Words and whitespace runs become alternating tokens (a
// space token wraps never — it hangs at the end of a soft-wrapped line),
// '\n' still forces a break. Every token carries its exact source range, so
// downstream fragments are exact source slices and the SourcePos caret APIs
// are precise.
func tokenizePreserve(items []InlineItem) []inlineTok {
	var toks []inlineTok
	for ii, it := range items {
		if it.Box {
			toks = append(toks, inlineTok{box: true, boxIndex: it.BoxIndex, valign: it.VAlign, size: it.Size,
				baseline: it.Baseline, srcItem: ii, srcStart: 0, srcEnd: 1})
			continue
		}
		runes := []rune(it.Text)
		i := 0
		for i < len(runes) {
			if runes[i] == '\n' {
				toks = append(toks, inlineTok{forceBreak: true, srcItem: ii, srcStart: i, srcEnd: i + 1})
				i++
				continue
			}
			// A tab is its own token: unlike every other rune its advance
			// depends on where it lands, so it cannot ride inside a
			// whitespace run whose width is measured once.
			if runes[i] == '\t' {
				toks = append(toks, inlineTok{
					tab: true, space: true, text: "\t",
					font:       it.Font,
					color:      it.Color,
					deco:       it.Decoration,
					decoPaint:  it.DecorationPaint,
					href:       it.Href,
					linkSrc:    it.LinkSource,
					background: it.Background,
					shift:      it.BaselineShift,
					width:      measureRunes(runes[i:i+1], it.Font, nil),
					srcItem:    ii, srcStart: i, srcEnd: i + 1,
				})
				i++
				continue
			}
			start := i
			space := isInlineSpace(runes[i])
			for i < len(runes) && runes[i] != '\n' && runes[i] != '\t' && isInlineSpace(runes[i]) == space {
				i++
			}
			seg := runes[start:i]
			toks = append(toks, inlineTok{
				text:       string(seg),
				font:       it.Font,
				color:      it.Color,
				deco:       it.Decoration,
				decoPaint:  it.DecorationPaint,
				href:       it.Href,
				linkSrc:    it.LinkSource,
				background: it.Background,
				shift:      it.BaselineShift,
				width:      measureRunes(seg, it.Font, nil),
				space:      space,
				srcItem:    ii,
				srcStart:   start,
				srcEnd:     i,
			})
		}
	}
	return toks
}

// lineRunWidth is tokRunWidth over one line's token indices — the run a
// centre/right tab stop on that line positions. justify rides on the gaps
// exactly as placement adds it.
func lineRunWidth(toks []inlineTok, idxs []int, from int, justify float32) float32 {
	var w float32
	for pos := from; pos < len(idxs); pos++ {
		t := toks[idxs[pos]]
		if t.forceBreak || t.tab {
			break
		}
		if pos > from && t.spaceBefore {
			w += t.spaceWidth + justify
		}
		if t.box {
			w += t.size.W
		} else {
			w += t.width
		}
	}
	return w
}

// tokRunWidth is the natural width of the tokens in [from, end) up to the
// next tab or forced break — the run a centre/right tab stop positions.
func tokRunWidth(toks []inlineTok, from, end int) float32 {
	var w float32
	for i := from; i < end && i < len(toks); i++ {
		t := toks[i]
		if t.forceBreak || t.tab {
			break
		}
		if i > from && t.spaceBefore {
			w += t.spaceWidth
		}
		if t.box {
			w += t.size.W
		} else {
			w += t.width
		}
	}
	return w
}

// splitInlineTok cuts a text token at the last cluster boundary whose advance
// still fits in avail, returning the head (placed on the current line) and the
// tail (carried to the next). Splits land on grapheme-ish boundaries — an
// emoji cluster is never cut apart — and the tail loses spaceBefore since it
// starts a line. ok is false when nothing fits (caller should start a fresh
// line) or there is nothing to split; force makes it yield at least one
// cluster regardless of avail, which is what guarantees progress on an empty
// line whose width can't even hold a single glyph.
func splitInlineTok(t inlineTok, avail float32, force bool) (head, tail inlineTok, ok bool) {
	runes := []rune(t.text)
	if len(runes) < 2 {
		return head, tail, false
	}
	boundaries := GraphemeBoundaries(runes)
	var w float32
	cut := 0
	for bi := 1; bi < len(boundaries); bi++ {
		i, end := boundaries[bi-1], boundaries[bi]
		cw := measureRunes(runes[i:end], t.font, nil)
		if w+cw > avail {
			break
		}
		w += cw
		cut = end
	}
	if cut == 0 {
		if !force {
			return head, tail, false
		}
		cut = boundaries[1]
	}
	if cut >= len(runes) {
		return head, tail, false
	}
	head, tail = t, t
	head.text = string(runes[:cut])
	head.width = measureRunes(runes[:cut], t.font, nil)
	head.srcEnd = t.srcStart + cut
	tail.text = string(runes[cut:])
	tail.width = measureRunes(runes[cut:], t.font, nil)
	tail.srcStart = t.srcStart + cut
	tail.spaceBefore = false
	return head, tail, true
}

func isInlineSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\r' || r == '\f' || r == '\v'
}

// inlineAscent / inlineDescent are the run's ink box above and below the
// baseline.
//
// They are NOT rounded up to whole pixels. Ceiling each half separately cost
// Arial at 10pt nearly a whole pixel (12.07 + 2.83 became 13 + 3), which is
// more than the line gap the line box was supposed to add — so the ink box
// grew past the line box and silently took over as the line's height. A
// page then held 58 lines where Google Docs holds 60, and no amount of
// fixing the line-height formula could have moved it.
func inlineAscent(f Font) float32 {
	return float32(GetFontFaceFor(f).Metrics().Ascent) / 64
}

func inlineDescent(f Font) float32 {
	return float32(GetFontFaceFor(f).Metrics().Descent) / 64
}

// inlineMidRef returns how far above the baseline a font's ink center sits:
// (ascent - descent) / 2. vertical-align:middle boxes are centered there so
// they align optically with the text's vertical middle rather than dropping
// to the baseline. Clamped to >= 0.
func inlineMidRef(f Font) float32 {
	m := (inlineAscent(f) - inlineDescent(f)) / 2
	if m < 0 {
		return 0
	}
	return m
}

// inlineHalfLeading is the extra half-leading distributed above/below a text
// run's ink box to reach its scaled line height (0 when line-height is at or
// below the ink height).
func inlineHalfLeading(f Font, scale, spacing float32) float32 {
	lh := lineBox(f, scale, spacing)
	ink := inlineAscent(f) + inlineDescent(f)
	half := (lh - ink) / 2
	if half < 0 {
		return 0
	}
	return half
}

// DrawInlineText draws only the text fragments (and their decorations) of an
// inline layout at origin. Atomic box fragments are the caller's job — the
// hosting widget draws its child widgets separately. origin is the top-left
// the layout was measured against (typically the content rect).
func DrawInlineText(canvas Canvas, layout InlineLayout, origin Rect) {
	for li := range layout.Lines {
		DrawInlineLine(canvas, layout, li, origin)
	}
}

// DrawInlineLine draws a single line of the layout at origin. Callers that
// reposition individual lines (pagination splitting a paragraph across page
// boundaries) offset origin per line instead of per layout.
func DrawInlineLine(canvas Canvas, layout InlineLayout, li int, origin Rect) {
	if li < 0 || li >= len(layout.Lines) {
		return
	}
	line := &layout.Lines[li]
	for _, f := range line.Frags {
		if f.Box || f.Text == "" {
			continue
		}
		r := Rect{X: origin.X + f.Rect.X, Y: origin.Y + f.Rect.Y, W: f.Rect.W, H: f.Rect.H}
		if f.Tab {
			// Never draw the tab rune itself — the gap IS the tab. What is
			// drawn is the stop's leader, if it has one.
			drawTabLeader(canvas, f, r)
			continue
		}
		if f.Background.A > 0 {
			// Highlight fills the line's ink band (baseline-anchored,
			// like a marker pen), not the leaded line box.
			canvas.FillRect(Rect{
				X: r.X,
				Y: origin.Y + line.Baseline - line.InkAscent,
				W: r.W,
				H: line.InkAscent + line.InkDescent,
			}, f.Background)
		}
		canvas.DrawText(f.Text, r, f.Color, f.Font)
		if f.Decoration != 0 {
			drawDecorationRun(canvas, f.Decoration, r.X, r.W, r.Y, r.H, f.Color, f.Font, f.DecorationPaint)
		}
	}
}

// drawTabLeader paints a tab stop's leader across the gap the tab opened.
// The run is right-aligned against the stop and clipped to whole leader
// characters, which is what makes a column of dot leaders in a table of
// contents line up: every row's dots end at the same x.
func drawTabLeader(canvas Canvas, f InlineFrag, r Rect) {
	if f.Leader == 0 || r.W <= 0 {
		return
	}
	cw, _ := TextMetrics(string(f.Leader), f.Font)
	if cw <= 0 {
		return
	}
	n := int(r.W / cw)
	if n <= 0 {
		return
	}
	run := strings.Repeat(string(f.Leader), n)
	canvas.DrawText(run, Rect{X: r.X + r.W - float32(n)*cw, Y: r.Y, W: float32(n) * cw, H: r.H}, f.Color, f.Font)
}

// LinkAt returns the Href of the hyperlink fragment under pt, given the same
// origin DrawInlineText was called with. ok is false when pt lands off any
// link fragment.
func (l InlineLayout) LinkAt(origin Rect, pt Point) (href string, ok bool) {
	href, _, ok = l.LinkSourceAt(origin, pt)
	return href, ok
}

// LinkSourceAt is LinkAt also returning the fragment's LinkSource.
func (l InlineLayout) LinkSourceAt(origin Rect, pt Point) (href string, source any, ok bool) {
	for _, line := range l.Lines {
		for _, f := range line.Frags {
			r := Rect{X: origin.X + f.Rect.X, Y: origin.Y + f.Rect.Y, W: f.Rect.W, H: f.Rect.H}
			if f.Href != "" && r.Contains(pt) {
				return f.Href, f.LinkSource, true
			}
		}
	}
	return "", nil, false
}
