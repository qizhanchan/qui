package widgets

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"strings"

	. "github.com/qizhanchan/qui"
)

// InlineBox lays out a mix of styled text and atomic inline widgets in a
// single inline formatting context (CSS inline flow). Text wraps at word
// boundaries and atomic child widgets (inline <img>, inline-block boxes,
// badges) sit on the shared text baseline instead of breaking onto their own
// block line.
//
// It is the retained-mode counterpart to the root BuildInlineLayout engine:
// text runs are appended with AddText, atomic children with AddBox, and the
// widget measures / positions / paints them via the engine. Atomic children
// are real widgets in the tree (added through the embedded Container), so
// they hit-test, tick, and dispatch like any other child; the interleaved
// text is painted by the box itself.
type InlineBox struct {
	Container

	// Wrap enables soft-wrapping at the box's width (default true once set
	// by callers). Align controls per-line horizontal alignment.
	Wrap               bool
	Align              TextAlign
	LineHeightScale    float32
	RoundNormalMetrics bool
	Hyphenate          bool
	Language           string

	// PreserveWhitespace switches the IFC to word-processor whitespace
	// semantics (every space kept and addressable, wrap-boundary spaces
	// hang) — the white-space:pre / editing-surface mode. See
	// qui.InlineLayoutOptions.PreserveWhitespace.
	PreserveWhitespace bool
	// TrimTrailingEmptyLine selects CSS final-newline behavior.
	TrimTrailingEmptyLine bool

	// Strut makes the box's own font and line height a floor for every
	// line (CSS's strut; see InlineLayoutOptions.Strut).
	Strut bool
	// StrutNeedsText: see InlineLayoutOptions.StrutNeedsText.
	StrutNeedsText bool

	// BreakLongWords adds a character-level break for a run with no break
	// opportunity that is wider than the box (a pasted URL, a space-free
	// CJK/digit string). Off = CSS overflow-wrap:normal, where such a run
	// overflows. See qui.InlineLayoutOptions.BreakLongWords.
	BreakLongWords bool

	// floatEx are the floats this run flows around (SetFloatExclusions),
	// in its own coordinates; floatGen changes whenever they do.
	floatEx  []FloatExclusion
	floatGen uint64

	parts   []inlinePart
	layout  InlineLayout
	layoutW float32
	haveLay bool
	laidOut bool
	// layoutKey fingerprints the inputs the cached layout was built from.
	layoutKey inlineLayoutKey
	// fit memoizes the run's max-content size (fitKey: the inputs it was
	// built from). At any width at least that wide the run lays out
	// unbroken, so Measure answers without shaping it again: layout
	// engines probe a child at many widths per pass (nested flex items),
	// more than the measure memo keeps, and each miss re-shaped the text.
	// fitOK records that a bounded layout was checked to match it (an
	// aligned or right-to-left run spans its slot instead; fitBad).
	fit           Size
	fitKey        inlineLayoutKey
	haveFit       bool
	fitOK, fitBad bool

	// Cross-widget text selection state (qui.TextSelectable). Offsets are
	// rune indices into the LAID-OUT text: fragment texts in visual order,
	// with one '\n' between lines. selStart < 0 means no selection.
	//
	// During an intra-widget mouse drag selStart holds the fixed anchor and
	// selEnd tracks the cursor (either order — SelectedText sorts them).
	selStart int
	selEnd   int

	// focused mirrors keyboard focus (so a click on the paragraph text
	// doesn't leave focus stranded elsewhere).
	focused bool

	// Selectable gates window-driven + intra-box text selection (mirrors
	// Label.Selectable). Default true; set false for text that acts as a
	// control surface (e.g. a <label for> whose click toggles a checkbox)
	// so a drag doesn't turn it into selectable prose.
	Selectable bool

	// Inline elements (OpenSpan / CloseSpan; see inlineflow_spans.go).
	spans               []inlineSpanRec
	spanStack           []int32
	hoverSpan           int32
	spanFirst, spanLast []int

	// partTags are the host's source tags per part (TagLastPart; see
	// inlineflow_edit.go) — how an editor maps layout items to its model.
	partTags []any
}

// inlinePart is one appended piece: a styled text run or an atomic child.
type inlinePart struct {
	hyphenate bool
	language  string
	isBox     bool

	text      string
	font      Font
	color     Color
	deco      TextDecoration
	decoPaint DecorationPaint
	href      string
	linkSrc   any

	child  Widget
	valign InlineVAlign

	span     int32   // innermost open span when added
	shift    float32 // its cumulative baseline shift
	lh       float32 // its line-height scale (0 = the box's)
	inset    float32 // > 0: a span's start / end edge (no text, no box)
	insetEnd bool

	shadows []ShadowStyle // CSS text-shadow layers of a text run
}

// NewInlineBox constructs an empty inline box. Append content with
// AddText / AddBox, then hand it to a parent like any widget.
func NewInlineBox() *InlineBox {
	b := &InlineBox{Wrap: true, selStart: -1, Selectable: true}
	b.BaseWidget = NewBaseWidget()
	b.SetSelf(b)
	return b
}

// AddText appends a styled text run. A '\n' in text forces a line break.
func (b *InlineBox) AddText(text string, font Font, color Color, deco TextDecoration, href string) {
	b.AddTextPaint(text, font, color, deco, DecorationPaint{}, href)
}

// AddTextPaint is AddText with an explicit decoration paint (CSS
// text-decoration-color/-style/-thickness) for the run's underline etc.
func (b *InlineBox) AddTextPaint(text string, font Font, color Color, deco TextDecoration, decoPaint DecorationPaint, href string) {
	b.AddLinkText(text, font, color, deco, decoPaint, href, nil)
}

// AddLinkText is AddTextPaint for a run that belongs to a link, with an
// identity for that link (source — any comparable value, typically the
// element it came from). LinkSourceAt reports it back for a click, so two
// links sharing an href, or a link's own attributes, can be told apart.
func (b *InlineBox) AddLinkText(text string, font Font, color Color, deco TextDecoration, decoPaint DecorationPaint, href string, source any) {
	if text == "" {
		return
	}
	b.parts = append(b.parts, inlinePart{text: text, font: font, color: color, deco: deco, decoPaint: decoPaint, href: href, linkSrc: source,
		span: b.curSpan(), shift: b.curShift(), lh: b.curLineHeight(), hyphenate: b.currentHyphenation(), language: b.Language})
	b.haveLay = false
	b.haveFit = false
	b.InvalidateLayout()
}

// ShadowLastText gives the most recently added text run CSS text-shadow
// layers (first layer on top). A no-op when the last part is not text.
func (b *InlineBox) ShadowLastText(shadows []ShadowStyle) {
	if len(shadows) == 0 || len(b.parts) == 0 {
		return
	}
	p := &b.parts[len(b.parts)-1]
	if p.isBox || p.inset > 0 {
		return
	}
	p.shadows = shadows
}

// AddBox appends an atomic inline child widget aligned via valign. The child
// is wired into the tree (parent link, window attach) via the embedded
// Container so dispatch and hit-testing reach it.
func (b *InlineBox) AddBox(w Widget, valign InlineVAlign) {
	if w == nil {
		return
	}
	if !b.AddChild(w) { // appends to the Container child list in box order
		// Keep parts and children aligned: a refused child (cycle / foreign
		// owner) must not leave a phantom item that later indexes the wrong
		// child during Layout.
		b.InvalidateLayout()
		return
	}
	b.parts = append(b.parts, inlinePart{isBox: true, child: w, valign: valign, span: b.curSpan()})
	b.haveLay = false
	b.haveFit = false
	b.InvalidateLayout()
}

// AddBreak appends a forced line break (CSS <br>).
func (b *InlineBox) AddBreak() {
	b.parts = append(b.parts, inlinePart{text: "\n"})
	b.haveLay = false
	b.haveFit = false
	b.InvalidateLayout()
}

// Clear removes every appended part and atomic child so the box can be
// reassembled in place — the htmlcss live path rebuilds content on each
// restyle while keeping this widget instance (and its tree position) stable.
// Any text selection is dropped: its offsets referred to the old content.
func (b *InlineBox) Clear() {
	b.parts = b.parts[:0]
	b.spans = b.spans[:0]
	b.spanStack = b.spanStack[:0]
	b.hoverSpan = 0
	b.partTags = b.partTags[:0]
	b.ClearChildren()
	b.haveLay = false
	b.haveFit = false
	b.selStart, b.selEnd = -1, 0
	b.InvalidateLayout()
}

// buildItems measures atomic children at the given width and produces the
// engine's item list. Box items carry a BoxIndex counting box parts in order,
// which matches the order children were AddChild'd.
func (b *InlineBox) buildItems(maxW float32) []InlineItem {
	items := make([]InlineItem, 0, len(b.parts))
	boxN := 0
	for _, p := range b.parts {
		group := int32(0)
		for id := p.span; id > 0; id = b.spans[id-1].parent {
			if !b.spans[id-1].style.NoWrap {
				break
			}
			group = id
		}
		if p.inset > 0 {
			items = append(items, InlineItem{Inset: p.inset, InsetEnd: p.insetEnd, Span: p.span})
			continue
		}
		if p.isBox {
			// The box rides the line as its margin box (CSS: an atomic
			// inline's margins take inline space); Layout insets it again.
			m := boxMargin(p.child)
			sz := inlineChildSize(p.child, Size{W: maxW - m.Horizontal()})
			it := InlineItem{
				NoWrapGroup: group,
				Box:         true,
				Size:        Size{W: sz.W + m.Horizontal(), H: sz.H + m.Vertical()},
				VAlign:      p.valign,
				BoxIndex:    boxN,
				Span:        p.span,
			}
			// A text-carrying atomic box (e.g. a styled link kept as its own
			// box) reports its baseline so the IFC aligns it with the run.
			if bp, ok := p.child.(InlineBaselineProvider); ok {
				if bl := bp.InlineBaseline(); bl > 0 {
					it.Baseline = bl + m.Top
				}
			}
			items = append(items, it)
			boxN++
			continue
		}
		items = append(items, InlineItem{
			NoWrapGroup:     group,
			Text:            p.text,
			Font:            p.font,
			Color:           p.color,
			Decoration:      p.deco,
			DecorationPaint: p.decoPaint,
			Href:            p.href,
			LinkSource:      p.linkSrc,
			Span:            p.span,
			BaselineShift:   p.shift,
			LineHeightScale: p.lh,
			Hyphenate:       p.hyphenate,
			Language:        p.language,
		})
	}
	return items
}

func (b *InlineBox) options(maxW float32) InlineLayoutOptions {
	o := InlineLayoutOptions{
		MaxWidth:              maxW,
		Wrap:                  b.Wrap,
		Align:                 b.Align,
		LineHeightScale:       b.LineHeightScale,
		RoundNormalMetrics:    b.RoundNormalMetrics,
		BaseFont:              b.Style().Font,
		PreserveWhitespace:    b.PreserveWhitespace,
		BreakLongWords:        b.BreakLongWords,
		Strut:                 b.Strut,
		StrutNeedsText:        b.StrutNeedsText,
		TrimTrailingEmptyLine: b.TrimTrailingEmptyLine,
	}
	if maxW > 0 && len(b.floatEx) > 0 {
		in := b.Style().ContentInsets()
		for _, e := range b.floatEx {
			r := e.Rect
			r.X -= in.Left
			r.Y -= in.Top
			o.Exclusions = append(o.Exclusions, r)
		}
	}
	return o
}

// SetFloatExclusions implements qui.FloatExclusionReceiver: the run's
// lines narrow beside the floats (rects in the box's own coordinates).
func (b *InlineBox) SetFloatExclusions(ex []FloatExclusion) {
	if len(ex) == len(b.floatEx) {
		same := true
		for i := range ex {
			if ex[i] != b.floatEx[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	b.floatEx = append(b.floatEx[:0], ex...)
	b.floatGen++
	b.InvalidateLayout()
}

// inlineLayoutKey fingerprints everything that changes the shaped result.
// Font is not comparable (Features / Variations are a slice + a map), so its
// identity is spelled out field by field; those two are deliberately left out
// — mutating them in place already bypasses widget invalidation everywhere
// else in the framework (see Style.Clone's note).
type inlineLayoutKey struct {
	maxW     float32
	floatGen uint64
	// fontGen invalidates the cache when the font REGISTRY changes (see
	// qui.FontRegistryGeneration): same font spec, different faces behind it.
	fontGen   uint64
	localeGen uint64
	// childSig fingerprints the measured atomic children. Their sizes are
	// inputs to the inline layout too: an inline image finishing its async
	// load, or a badge changing text, invalidates the child's measure but
	// leaves this box's text/font key untouched.
	childSig              uint64
	wrap                  bool
	align                 TextAlign
	lineHeightScale       float32
	roundNormalMetrics    bool
	hyphenate             bool
	preserveWhitespace    bool
	breakLongWords        bool
	family                string
	size                  float32
	weight                FontWeight
	bold, italic          bool
	smallCaps             bool
	letterSpacing         float32
	wordSpacing           float32
	trimTrailingEmptyLine bool
}

func (b *InlineBox) layoutKeyFor(maxW float32) inlineLayoutKey {
	f := b.Style().Font
	return inlineLayoutKey{
		maxW: maxW, fontGen: FontRegistryGeneration(), floatGen: b.floatGen,
		localeGen: LocaleGeneration(), childSig: b.childLayoutSig(maxW),
		wrap: b.Wrap, align: b.Align,
		lineHeightScale:    b.LineHeightScale,
		roundNormalMetrics: b.RoundNormalMetrics,
		hyphenate:          b.Hyphenate,
		preserveWhitespace: b.PreserveWhitespace,
		breakLongWords:     b.BreakLongWords,
		family:             f.Family, size: f.Size, weight: f.Weight,
		trimTrailingEmptyLine: b.TrimTrailingEmptyLine,
		bold:                  f.Bold, italic: f.Italic, smallCaps: f.SmallCaps,
		letterSpacing: f.LetterSpacing, wordSpacing: f.WordSpacing,
	}
}

// childLayoutSig hashes the measured size of every atomic child. Measuring
// through MeasureChild keeps this cheap when the child subtree is clean and
// re-measures it when it is not, so the key changes exactly when the inline
// layout's inputs do.
func (b *InlineBox) childLayoutSig(maxW float32) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	for _, p := range b.parts {
		if !p.isBox || p.child == nil {
			continue
		}
		m := boxMargin(p.child)
		sz := inlineChildSize(p.child, Size{W: maxW - m.Horizontal()})
		for _, v := range [2]float32{sz.W, sz.H} {
			binary.LittleEndian.PutUint32(buf[:4], math.Float32bits(v))
			_, _ = h.Write(buf[:4])
		}
	}
	return h.Sum64()
}

// buildLayout shapes the paragraph, reusing the previous result when nothing
// that affects it changed.
//
// The guard matters far beyond micro-optimization: layout engines call
// Measure on every child of every relayout, and a ScrollView re-lays out its
// whole content subtree on each scroll event. Without it, one trackpad flick
// re-shaped every paragraph on the page through harfbuzz — 17 ms per event on
// a 100-row document, 180 ms on 1000 rows. Content mutations (AddText /
// AddBox / AddBreak / Clear) clear haveLay, and the key covers the option +
// font inputs, so a stale layout can only survive an in-place mutation of
// Font.Features / Font.Variations.
func (b *InlineBox) buildLayout(maxW float32) {
	key := b.layoutKeyFor(maxW)
	if b.haveLay && b.layoutKey == key {
		return
	}
	b.layout = BuildInlineLayout(b.buildItems(maxW), b.options(maxW))
	b.layoutW = maxW
	b.layoutKey = key
	b.haveLay = true
}

// MinContentWidth implements qui.MinContentWidther: the run laid out with
// a break at every opportunity and no word broken — its widest line.
func (b *InlineBox) MinContentWidth() float32 {
	if !b.Wrap {
		return BuildInlineLayout(b.buildItems(0), b.options(0)).Width
	}
	opts := b.options(0.01)
	opts.BreakLongWords = false
	return BuildInlineLayout(b.buildItems(0.01), opts).Width
}

// MaxContentWidth implements qui.MaxContentWidther: the run laid out on
// lines broken only where it must (hard breaks), atomic boxes at their own
// max-content widths.
func (b *InlineBox) MaxContentWidth() float32 {
	items := b.buildItems(0)
	for i, p := range b.parts {
		if !p.isBox || i >= len(items) {
			continue
		}
		m := boxMargin(p.child)
		w := MaxContentWidth(p.child)
		sz := inlineChildSize(p.child, Size{W: w})
		items[i].Size = Size{W: w + m.Horizontal(), H: sz.H + m.Vertical()}
	}
	return BuildInlineLayout(items, b.options(0)).Width
}

// MeasuresByWidthOnly implements qui.WidthOnlyMeasurer: the run's size
// follows from the width its lines break at (buildLayout).
func (b *InlineBox) MeasuresByWidthOnly() bool { return true }

func (b *InlineBox) Measure(available Size) Size {
	// Intrinsic probes can run after Layout (for example while an ancestor
	// resolves floats). They must not replace the lines used for painting,
	// selection, and hit testing at the widget's assigned width.
	if b.laidOut {
		layout, width, key, have := b.layout, b.layoutW, b.layoutKey, b.haveLay
		defer func() { b.layout, b.layoutW, b.layoutKey, b.haveLay = layout, width, key, have }()
	}
	if sz, ok := b.measureFit(available.W); ok {
		return sz
	}
	b.buildLayout(available.W)
	return Size{W: b.layout.ContentWidth, H: b.layout.Height}
}

// measureFit answers Measure from the max-content size when the run fits
// unbroken at width w (see fit), or does not wrap and is wider than w. An
// atomic box may size itself from the width: a run holding boxes keys the
// memo on their sizes at w.
func (b *InlineBox) measureFit(w float32) (Size, bool) {
	if w <= 0 || len(b.floatEx) > 0 {
		return Size{}, false
	}
	hasBox := false
	for _, p := range b.parts {
		if p.isBox {
			hasBox = true
			break
		}
	}
	key := b.layoutKeyFor(0)
	itemsW := float32(0)
	if hasBox {
		// The boxes as they measure at this width: the memo holds while
		// they keep those sizes.
		key.childSig, itemsW = b.childLayoutSig(w), w
	}
	if !b.haveFit || b.fitKey != key {
		l := BuildInlineLayout(b.buildItems(itemsW), b.options(0))
		b.fit = Size{W: l.ContentWidth, H: l.Height}
		b.fitKey, b.haveFit = key, true
		b.fitOK, b.fitBad = false, false
	}
	// Half a pixel of slack: at exactly its own width a run can break on
	// rounding.
	if w < b.fit.W+0.5 {
		// An unwrapped run narrower than its slot lays out unbounded: it
		// breaks nowhere, and alignment only shifts lines into room left
		// over — there is none.
		if !b.Wrap {
			return b.fit, true
		}
		return Size{}, false
	}
	if b.fitBad {
		return Size{}, false
	}
	if !b.fitOK {
		b.buildLayout(w)
		if (Size{W: b.layout.ContentWidth, H: b.layout.Height}) != b.fit {
			b.fitBad = true
			return Size{}, false
		}
		b.fitOK = true
	}
	return b.fit, true
}

func (b *InlineBox) Layout(rect Rect) {
	b.BaseWidget.Layout(rect)
	b.laidOut = true
	b.buildLayout(rect.W)
	origin := b.Bounds()
	for li := range b.layout.Lines {
		for _, f := range b.layout.Lines[li].Frags {
			if !f.Box {
				continue
			}
			if f.BoxIndex < 0 || f.BoxIndex >= b.ChildCount() {
				continue
			}
			child := b.ChildAt(f.BoxIndex)
			m := boxMargin(child)
			child.Layout(Rect{
				X: origin.X + f.Rect.X + m.Left,
				Y: origin.Y + f.Rect.Y + m.Top,
				W: max(f.Rect.W-m.Horizontal(), 0),
				H: max(f.Rect.H-m.Vertical(), 0),
			})
		}
	}
}

// InlineEnd reports where inline content placed after this box would
// start: the end of the last line (its top edge). A following out-of-flow
// box that was inline-level has its static position there.
func (b *InlineBox) InlineEnd() (Point, bool) {
	if !b.haveLay || len(b.layout.Lines) == 0 {
		return Point{}, false
	}
	origin := b.Bounds()
	ln := b.layout.Lines[len(b.layout.Lines)-1]
	x := float32(0)
	for _, f := range ln.Frags {
		x = max(x, f.Rect.X+f.Rect.W)
	}
	return Point{X: origin.X + x, Y: origin.Y + ln.Y}, true
}

func (b *InlineBox) Draw(canvas Canvas) {
	origin := b.Bounds()
	if b.Style().Background.A > 0 {
		canvas.FillRect(origin, b.Style().Background)
	}
	b.drawSpans(canvas, origin)
	b.drawSelection(canvas, origin)
	layout := b.hoverLayout()
	b.drawTextShadows(canvas, layout, origin)
	DrawInlineText(canvas, layout, origin)
	for _, child := range b.ChildList() {
		if boxChildHidden(child) {
			continue
		}
		child.Draw(canvas)
	}
}

// --- intra-widget mouse selection -----------------------------------
//
// The window's cross-widget selection controller drives selections that
// SPAN widgets, but for a drag that stays inside one selectable it defers
// to "the anchor's own drag handler" (see Window.extendTextSelection).
// Labels supply that handler; an InlineBox must too, or text that folds
// into a single inline run (a mixed <p>) can't be selected at all.

// Focusable makes a text-bearing inline box take focus on click, matching
// Label — so selecting its text doesn't leave focus stranded on a prior
// widget, and window-level Cmd/Ctrl+A/C route sensibly.
func (b *InlineBox) Focusable() bool { return b.Enabled() && b.Selectable && b.SelectableLength() > 0 }

// TabStop: like Label, selectable inline text is click-focusable but not a
// keyboard stop. Satisfies qui.TabStopper.
func (b *InlineBox) TabStop() bool { return false }

// SetFocused records keyboard focus.
func (b *InlineBox) SetFocused(f bool) {
	if b.focused == f {
		return
	}
	b.focused = f
	b.Invalidate()
}

// InlineBox handles no events itself. Selection — press-drag, double-click
// word, triple-click paragraph, cross-widget runs — is driven by the
// Window's selection controller through TextSelectable / SelectionGranular;
// clicks pass through so the parent El handles link navigation and onClick
// on MouseUp; the hover shape comes from CursorAt below. Atomic inline
// children (inputs/buttons/badges) are dispatched to directly by the window
// when they are the hit target.

// CursorAt reports the hover shape per point: hand over a folded link,
// I-beam over selectable text, decline otherwise (see qui/cursor.go).
func (b *InlineBox) CursorAt(p Point) (CursorShape, bool) {
	if !b.Enabled() {
		return CursorDefault, false
	}
	if _, ok := b.LinkAt(p); ok {
		return CursorHand, true
	}
	if b.Selectable && b.SelectableLength() > 0 {
		return CursorText, true
	}
	return CursorDefault, false
}

// --- qui.TextSelectable (window-level cross-widget selection) --------
//
// Selection offsets live in the laid-out text space: fragment texts in
// visual order (lines top-down, frags left-right), one '\n' rune between
// lines. Deriving the space from the layout keeps point↔offset↔text
// mappings mutually consistent — which is all the window's selection
// engine requires — without inventing source offsets for collapsed
// whitespace.

// InlineAtomContent is implemented by an atomic inline child (added via
// AddBox) whose content should join the enclosing InlineBox's text
// selection + clipboard copy — e.g. an inline-block badge (its text) or an
// inline <img> (a picture). plain defines both the plain-text copy and how
// much offset space the atom occupies (it selects as one unit); spans is
// the rich/HTML form. An atom returning an empty plain stays a
// non-selectable box (a spacer, or a control).
type InlineAtomContent interface {
	InlineAtom() (plain string, spans []TextSpan)
}

// OwnsDescendantSelection tells the window's selection collector not to
// descend into this box's children: an InlineBox already folds its atomic
// children's text into its own offset space (via InlineAtomContent), so
// collecting them separately would double-count. Satisfies the framework's
// selection-leaf contract.
func (b *InlineBox) OwnsDescendantSelection() bool { return true }

// TextSelectionEnabled lets the window's selection collector skip this box
// when it acts as a control surface (Selectable == false), mirroring Label.
func (b *InlineBox) TextSelectionEnabled() bool { return b.Selectable }

// atomContent returns the copy content an atomic box child contributes, or
// ("", nil) when the child opts out / isn't an InlineAtomContent.
func (b *InlineBox) atomContent(boxIdx int) (string, []TextSpan) {
	if boxIdx < 0 || boxIdx >= b.ChildCount() {
		return "", nil
	}
	if a, ok := b.ChildAt(boxIdx).(InlineAtomContent); ok {
		return a.InlineAtom()
	}
	return "", nil
}

// selFrag is one selectable fragment in visual order: a text run, or an
// atomic box that contributes copy content. start/end are its rune range
// in the box's selection offset space.
type selFrag struct {
	li         int
	frag       *InlineFrag
	start, end int
	isBox      bool
	atomPlain  string
	atomSpans  []TextSpan
}

// walkSelFrags visits every selectable fragment (text runs AND atomic
// boxes that carry content) in visual order, threading a continuous rune
// offset with one '\n' between lines. Returns the total selectable length.
// This is the single source of truth for the offset space that
// SelectableLength / SelectionOffsetAt / drawSelection / SelectedText /
// SelectedSpans all share, so atomic inline content (badges, images) is
// selectable and copyable just like text.
func (b *InlineBox) walkSelFrags(fn func(sf selFrag)) int {
	off := 0
	for li := range b.layout.Lines {
		if li > 0 {
			off++ // line boundary counts as '\n'
		}
		frags := b.layout.Lines[li].Frags
		for fi := range frags {
			f := &frags[fi]
			if f.Box {
				plain, spans := b.atomContent(f.BoxIndex)
				n := len([]rune(plain))
				if n == 0 {
					continue // non-selectable atom (spacer / control)
				}
				if fn != nil {
					fn(selFrag{li: li, frag: f, start: off, end: off + n, isBox: true, atomPlain: plain, atomSpans: spans})
				}
				off += n
				continue
			}
			n := len([]rune(f.Text))
			if fn != nil {
				fn(selFrag{li: li, frag: f, start: off, end: off + n})
			}
			off += n
		}
	}
	return off
}

// lineStartOffset returns the selection offset at the start of visual line
// li, including the one-rune boundary per preceding line. Unlike a frag
// walk it also works for an empty line.
func (b *InlineBox) lineStartOffset(li int) int {
	if li <= 0 || !b.haveLay {
		return 0
	}
	off := 0
	for i := 0; i < len(b.layout.Lines) && i < li; i++ {
		for fi := range b.layout.Lines[i].Frags {
			f := &b.layout.Lines[i].Frags[fi]
			if f.Box {
				plain, _ := b.atomContent(f.BoxIndex)
				off += len([]rune(plain))
				continue
			}
			off += len([]rune(f.Text))
		}
		off++ // line boundary
	}
	return off
}

// SelectableLength returns the rune count of the laid-out selectable
// content (text runs + atomic box content).
func (b *InlineBox) SelectableLength() int {
	if !b.haveLay {
		return 0
	}
	return b.walkSelFrags(nil)
}

// SelectionOffsetAt maps a window-space point to a rune offset.
func (b *InlineBox) SelectionOffsetAt(p Point) int {
	if !b.haveLay || len(b.layout.Lines) == 0 {
		return 0
	}
	origin := b.Bounds()
	y := p.Y - origin.Y
	// Pick the line: above the first → first, below the last → last.
	li := len(b.layout.Lines) - 1
	for i := range b.layout.Lines {
		ln := b.layout.Lines[i]
		if y < ln.Y+ln.Height {
			li = i
			break
		}
	}
	x := p.X - origin.X
	result := -1
	// Start from the line's real offset, not 0: clicking a blank line used
	// to jump the selection anchor to the top of the document.
	lineStart, lineEnd := b.lineStartOffset(li), b.lineStartOffset(li)
	first := true
	b.walkSelFrags(func(sf selFrag) {
		if sf.li != li {
			return
		}
		if first {
			lineStart = sf.start
			first = false
		}
		lineEnd = sf.end
		if result >= 0 {
			return
		}
		f := sf.frag
		if x < f.Rect.X {
			result = sf.start // before this frag on the line
			return
		}
		if x < f.Rect.X+f.Rect.W {
			if sf.isBox {
				// Atomic box: snap to whichever edge the point is nearer,
				// so a drag past it selects the whole atom.
				if x < f.Rect.X+f.Rect.W/2 {
					result = sf.start
				} else {
					result = sf.end
				}
				return
			}
			result = sf.start + TextOffsetAtX(f.Text, f.Font, TextDirectionAuto, x-f.Rect.X)
		}
	})
	if result >= 0 {
		return result
	}
	if first {
		return lineStart // line has no text frags
	}
	return lineEnd // past the last frag on the line
}

// flatText reconstructs the box's selection offset space as a string:
// text fragments and atomic-inline content in visual order, one '\n' per
// line boundary — rune index i in the result IS selection offset i.
func (b *InlineBox) flatText() string {
	var sb strings.Builder
	last := 0
	b.walkSelFrags(func(sf selFrag) {
		for last < sf.start {
			sb.WriteByte('\n') // line boundaries occupy one offset each
			last++
		}
		if sf.isBox {
			sb.WriteString(sf.atomPlain)
		} else {
			sb.WriteString(sf.frag.Text)
		}
		last = sf.end
	})
	return sb.String()
}

// SelectionRangeAt expands a rune offset to gesture bounds
// (qui.SelectionGranular): double-click selects the word under the
// cursor (atomic inline content like badges counts as word text);
// triple-click selects the whole paragraph — the entire box — matching
// how browsers treat a folded block of inline content.
func (b *InlineBox) SelectionRangeAt(off int, unit SelectionUnit) (int, int) {
	switch unit {
	case SelectionWord:
		return WordRange(b.flatText(), off)
	case SelectionLine:
		return 0, b.SelectableLength()
	}
	return off, off
}

// SetSelectionRange sets the highlighted range (order-independent). A
// negative start, or start == end, clears the highlight.
func (b *InlineBox) SetSelectionRange(start, end int) {
	if start < 0 {
		b.ClearTextSelection()
		return
	}
	n := b.SelectableLength()
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if end < 0 {
		end = 0
	}
	if b.selStart == start && b.selEnd == end {
		return
	}
	b.selStart, b.selEnd = start, end
	b.Invalidate()
}

// ClearTextSelection removes any selection highlight.
func (b *InlineBox) ClearTextSelection() {
	if b.selStart < 0 && b.selEnd == 0 {
		return
	}
	b.selStart, b.selEnd = -1, 0
	b.Invalidate()
}

// SelectedText returns the selected substring of the laid-out content,
// including atomic inline atoms (e.g. inline-block badge text).
func (b *InlineBox) SelectedText() string {
	plain, _ := b.collectSelection()
	return plain
}

// SelectedSpans returns the styled spans within the current selection so a
// rich (HTML) clipboard copy preserves per-run font / color / decoration,
// hyperlinks (folded <a>), AND atomic inline content — inline-block badge
// text and inline <img> pictures. Satisfies qui.RichTextSelectable.
func (b *InlineBox) SelectedSpans() []TextSpan {
	_, spans := b.collectSelection()
	return spans
}

// collectSelection walks the selection once and produces both the plain
// text and the rich spans, so the two never drift. A soft-wrap boundary
// becomes a '\n' (plain) / single-space span (HTML reflows). It also
// restores the one space the layout trims immediately before an atomic box
// (so "an <badge>" copies as "an NEW", not "anNEW"); HTML collapses any
// resulting redundant space, so this only ever adds missing separators.
func (b *InlineBox) collectSelection() (string, []TextSpan) {
	a, c := b.orderedSelection()
	if a >= c || !b.haveLay {
		return "", nil
	}
	var plain []rune
	var spans []TextSpan
	lastLine := -1
	prevBox := false
	prevEndsSpace := true // suppress a leading separator
	b.walkSelFrags(func(sf selFrag) {
		if sf.end <= a || sf.start >= c {
			return
		}
		if lastLine >= 0 && sf.li != lastLine {
			plain = append(plain, '\n')
			spans = append(spans, TextSpan{Text: " "})
			prevEndsSpace = true
		}

		var text string
		var pieceSpans []TextSpan
		if sf.isBox {
			text = sf.atomPlain
			pieceSpans = sf.atomSpans
		} else {
			f := sf.frag
			runes := []rune(f.Text)
			text = string(runes[maxInt(a, sf.start)-sf.start : minInt(c, sf.end)-sf.start])
			font := f.Font
			color := f.Color
			pieceSpans = []TextSpan{{Text: text, Font: &font, Color: &color, Decoration: f.Decoration, Href: f.Href}}
		}

		// Restore the trimmed separator at a text↔box boundary.
		if (prevBox || sf.isBox) && !prevEndsSpace && text != "" && !isSpaceRune([]rune(text)[0]) {
			plain = append(plain, ' ')
			spans = append(spans, TextSpan{Text: " "})
			prevEndsSpace = true
		}

		plain = append(plain, []rune(text)...)
		spans = append(spans, pieceSpans...)
		if r := []rune(text); len(r) > 0 {
			prevEndsSpace = isSpaceRune(r[len(r)-1])
		}
		prevBox = sf.isBox
		lastLine = sf.li
	})
	return string(plain), spans
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }

func (b *InlineBox) orderedSelection() (int, int) {
	if b.selStart < 0 {
		return 0, 0
	}
	if b.selStart <= b.selEnd {
		return b.selStart, b.selEnd
	}
	return b.selEnd, b.selStart
}

// drawSelection paints the highlight behind selected fragments, spanning
// each line box's full height (like Label / a browser).
func (b *InlineBox) drawSelection(canvas Canvas, origin Rect) {
	a, c := b.orderedSelection()
	if a >= c || !b.haveLay {
		return
	}
	selColor := SelectionHighlight(b)
	b.walkSelFrags(func(sf selFrag) {
		if sf.end <= a || sf.start >= c {
			return
		}
		f := sf.frag
		ln := b.layout.Lines[sf.li]
		var xs, xe float32
		if sf.isBox {
			// Atomic atom: highlight its whole box.
			xs, xe = f.Rect.X, f.Rect.X+f.Rect.W
		} else {
			lo, hi := maxInt(a, sf.start)-sf.start, minInt(c, sf.end)-sf.start
			segments := TextSelectionSegments(f.Text, f.Font, TextDirectionAuto, lo, hi)
			for _, segment := range segments {
				canvas.FillRect(Rect{
					X: origin.X + f.Rect.X + segment.X,
					Y: origin.Y + ln.Y,
					W: segment.Width,
					H: ln.Height,
				}, selColor)
			}
			return
		}
		if xe <= xs {
			return
		}
		canvas.FillRect(Rect{
			X: origin.X + xs,
			Y: origin.Y + ln.Y,
			W: xe - xs,
			H: ln.Height,
		}, selColor)
	})
}

func (b *InlineBox) runesWidth(runes []rune, font Font) float32 {
	w, _ := TextMetrics(string(runes), font)
	return w
}

// Text returns the concatenated text of all runs (atomic children
// excluded) — the box's accessible text content.
func (b *InlineBox) Text() string {
	var sb []byte
	for _, p := range b.parts {
		if !p.isBox {
			sb = append(sb, p.text...)
		}
	}
	return string(sb)
}

// LaidOut returns the run's current line layout (fragments in the box's
// content coordinates) — for hit-testing, client rects and debugging.
func (b *InlineBox) LaidOut() InlineLayout { return b.layout }

// LinkAt returns the hyperlink target under window-space point p when p falls
// on a text fragment carrying a non-empty Href.
func (b *InlineBox) LinkAt(p Point) (string, bool) {
	return b.layout.LinkAt(b.Bounds(), p)
}

// LinkSourceAt is LinkAt also returning the source the run was added with
// (AddLinkText); nil for runs added without one.
func (b *InlineBox) LinkSourceAt(p Point) (href string, source any, ok bool) {
	return b.layout.LinkSourceAt(b.Bounds(), p)
}

// inlineChildSize measures a child and applies its Style sizing hints
// (explicit width/height, min/max) — the engine only knows a box's final
// Size, so an inline-block with an explicit CSS width is honored here.
// boxMargin is an atomic inline child's margin (zero when it has no style).
func boxMargin(w Widget) Insets {
	if m, ok := w.(interface{ InlineMargin() Insets }); ok {
		return m.InlineMargin()
	}
	if st := w.Style(); st != nil {
		return st.Margin
	}
	return Insets{}
}

func inlineChildSize(w Widget, avail Size) Size {
	st := w.Style()
	if st != nil && st.Width <= 0 && st.WidthPct <= 0 {
		// CSS shrink-to-fit, constrained by percentage min/max against the
		// containing line rather than against the resulting intrinsic width.
		sw := ShrinkToFit(w, avail.W)
		if avail.W > 0 {
			if st.MaxWidthPct > 0 {
				sw = min(sw, st.MaxWidthPct*avail.W+st.PctExtra.W)
			}
			if st.MinWidthPct > 0 {
				sw = max(sw, st.MinWidthPct*avail.W+st.PctExtra.W)
			}
		}
		m := MeasureConstrained(w, Size{W: sw, H: avail.H})
		m.W = sw
		return m
	}
	return MeasureConstrained(w, avail)
}

// drawTextShadows paints the runs' CSS text-shadow layers under the text:
// pass k repaints every fragment whose run has a k-th shadow in that
// shadow's color, offset and blurred. The fragments map back to their run
// through SrcItem (parts and layout items are 1:1).
func (b *InlineBox) drawTextShadows(canvas Canvas, layout InlineLayout, origin Rect) {
	maxK := 0
	for i := range b.parts {
		if n := len(b.parts[i].shadows); n > maxK {
			maxK = n
		}
	}
	if maxK == 0 {
		return
	}
	partOf := func(f InlineFrag, prev int) int {
		if f.SrcItem >= 0 && f.SrcItem < len(b.parts) {
			return f.SrcItem
		}
		return prev // a synthesized joining space rides with its neighbor
	}
	for k := maxK - 1; k >= 0; k-- {
		pass := InlineLayout{Width: layout.Width, Height: layout.Height, Lines: make([]InlineLine, len(layout.Lines))}
		var sh ShadowStyle
		any := false
		for li, ln := range layout.Lines {
			ln.Frags = append([]InlineFrag(nil), ln.Frags...)
			prev := -1
			for fi := range ln.Frags {
				f := &ln.Frags[fi]
				if f.Box {
					continue
				}
				pi := partOf(*f, prev)
				prev = pi
				f.Background = Color{}
				if pi < 0 || k >= len(b.parts[pi].shadows) {
					f.Text = ""
					continue
				}
				s := b.parts[pi].shadows[k]
				if !any {
					sh, any = s, true
				}
				f.Color = s.Color
				f.DecorationPaint.Color = s.Color
			}
			pass.Lines[li] = ln
		}
		if !any {
			continue
		}
		// One offset and blur per pass (runs in a box nearly always share
		// an inherited text-shadow); colors are per fragment.
		sh.Color.A = 1
		drawTextShadowLayer(canvas, sh, Rect{X: origin.X, Y: origin.Y, W: max(origin.W, layout.Width), H: max(origin.H, layout.Height)}, false,
			func(c Canvas) { DrawInlineText(c, pass, origin) })
	}
}

// PaintBounds covers the runs' text shadows and the atomic boxes riding in
// the run (an inline-flex button's box-shadow), which paint past the box —
// a damage repaint clipped to the bounds alone cut a button's shadow off.
func (b *InlineBox) PaintBounds() Rect {
	r := b.Bounds()
	for i := range b.parts {
		if len(b.parts[i].shadows) > 0 {
			r = r.Union(textShadowBounds(b.Bounds(), b.parts[i].shadows))
		}
	}
	for _, c := range b.ChildList() {
		if c != nil {
			r = r.Union(PaintBoundsOf(c))
		}
	}
	return r
}
