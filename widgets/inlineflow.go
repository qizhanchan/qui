package widgets

import (
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
	Wrap            bool
	Align           TextAlign
	LineHeightScale float32

	// PreserveWhitespace switches the IFC to word-processor whitespace
	// semantics (every space kept and addressable, wrap-boundary spaces
	// hang) — the white-space:pre / editing-surface mode. See
	// qui.InlineLayoutOptions.PreserveWhitespace.
	PreserveWhitespace bool

	// BreakLongWords adds a character-level break for a run with no break
	// opportunity that is wider than the box (a pasted URL, a space-free
	// CJK/digit string). Off = CSS overflow-wrap:normal, where such a run
	// overflows. See qui.InlineLayoutOptions.BreakLongWords.
	BreakLongWords bool

	parts   []inlinePart
	layout  InlineLayout
	layoutW float32
	haveLay bool
	// layoutKey fingerprints the inputs the cached layout was built from.
	layoutKey inlineLayoutKey

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
}

// inlinePart is one appended piece: a styled text run or an atomic child.
type inlinePart struct {
	isBox bool

	text      string
	font      Font
	color     Color
	deco      TextDecoration
	decoPaint DecorationPaint
	href      string

	child  Widget
	valign InlineVAlign
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
	if text == "" {
		return
	}
	b.parts = append(b.parts, inlinePart{text: text, font: font, color: color, deco: deco, decoPaint: decoPaint, href: href})
	b.haveLay = false
	b.InvalidateLayout()
}

// AddBox appends an atomic inline child widget aligned via valign. The child
// is wired into the tree (parent link, window attach) via the embedded
// Container so dispatch and hit-testing reach it.
func (b *InlineBox) AddBox(w Widget, valign InlineVAlign) {
	if w == nil {
		return
	}
	b.parts = append(b.parts, inlinePart{isBox: true, child: w, valign: valign})
	b.AddChild(w) // appends to the Container child list in box order
	b.haveLay = false
}

// AddBreak appends a forced line break (CSS <br>).
func (b *InlineBox) AddBreak() {
	b.parts = append(b.parts, inlinePart{text: "\n"})
	b.haveLay = false
	b.InvalidateLayout()
}

// Clear removes every appended part and atomic child so the box can be
// reassembled in place — the htmlcss live path rebuilds content on each
// restyle while keeping this widget instance (and its tree position) stable.
// Any text selection is dropped: its offsets referred to the old content.
func (b *InlineBox) Clear() {
	b.parts = b.parts[:0]
	b.ClearChildren()
	b.haveLay = false
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
		if p.isBox {
			it := InlineItem{
				Box:      true,
				Size:     inlineChildSize(p.child, Size{W: maxW}),
				VAlign:   p.valign,
				BoxIndex: boxN,
			}
			// A text-carrying atomic box (e.g. a styled link kept as its own
			// box) reports its baseline so the IFC aligns it with the run.
			if bp, ok := p.child.(InlineBaselineProvider); ok {
				it.Baseline = bp.InlineBaseline()
			}
			items = append(items, it)
			boxN++
			continue
		}
		items = append(items, InlineItem{
			Text:            p.text,
			Font:            p.font,
			Color:           p.color,
			Decoration:      p.deco,
			DecorationPaint: p.decoPaint,
			Href:            p.href,
		})
	}
	return items
}

func (b *InlineBox) options(maxW float32) InlineLayoutOptions {
	return InlineLayoutOptions{
		MaxWidth:           maxW,
		Wrap:               b.Wrap,
		Align:              b.Align,
		LineHeightScale:    b.LineHeightScale,
		BaseFont:           b.Style().Font,
		PreserveWhitespace: b.PreserveWhitespace,
		BreakLongWords:     b.BreakLongWords,
	}
}

// inlineLayoutKey fingerprints everything that changes the shaped result.
// Font is not comparable (Features / Variations are a slice + a map), so its
// identity is spelled out field by field; those two are deliberately left out
// — mutating them in place already bypasses widget invalidation everywhere
// else in the framework (see Style.Clone's note).
type inlineLayoutKey struct {
	maxW float32
	// fontGen invalidates the cache when the font REGISTRY changes (see
	// qui.FontRegistryGeneration): same font spec, different faces behind it.
	fontGen            uint64
	wrap               bool
	align              TextAlign
	lineHeightScale    float32
	preserveWhitespace bool
	breakLongWords     bool
	family             string
	size               float32
	weight             FontWeight
	bold, italic       bool
	smallCaps          bool
	letterSpacing      float32
	wordSpacing        float32
}

func (b *InlineBox) layoutKeyFor(maxW float32) inlineLayoutKey {
	f := b.Style().Font
	return inlineLayoutKey{
		maxW: maxW, fontGen: FontRegistryGeneration(),
		wrap: b.Wrap, align: b.Align,
		lineHeightScale:    b.LineHeightScale,
		preserveWhitespace: b.PreserveWhitespace,
		breakLongWords:     b.BreakLongWords,
		family:             f.Family, size: f.Size, weight: f.Weight,
		bold: f.Bold, italic: f.Italic, smallCaps: f.SmallCaps,
		letterSpacing: f.LetterSpacing, wordSpacing: f.WordSpacing,
	}
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

func (b *InlineBox) Measure(available Size) Size {
	b.buildLayout(available.W)
	return Size{W: b.layout.Width, H: b.layout.Height}
}

func (b *InlineBox) Layout(rect Rect) {
	b.BaseWidget.Layout(rect)
	if !b.haveLay || b.layoutW != rect.W {
		b.buildLayout(rect.W)
	}
	origin := b.Bounds()
	for li := range b.layout.Lines {
		for _, f := range b.layout.Lines[li].Frags {
			if !f.Box {
				continue
			}
			if f.BoxIndex < 0 || f.BoxIndex >= b.ChildCount() {
				continue
			}
			b.ChildAt(f.BoxIndex).Layout(Rect{
				X: origin.X + f.Rect.X,
				Y: origin.Y + f.Rect.Y,
				W: f.Rect.W,
				H: f.Rect.H,
			})
		}
	}
}

func (b *InlineBox) Draw(canvas Canvas) {
	origin := b.Bounds()
	if b.Style().Background.A > 0 {
		canvas.FillRect(origin, b.Style().Background)
	}
	b.drawSelection(canvas, origin)
	DrawInlineText(canvas, b.layout, origin)
	for _, child := range b.ChildList() {
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
	lineStart, lineEnd := 0, 0
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

// LinkAt returns the hyperlink target under window-space point p when p falls
// on a text fragment carrying a non-empty Href.
func (b *InlineBox) LinkAt(p Point) (string, bool) {
	return b.layout.LinkAt(b.Bounds(), p)
}

// inlineChildSize measures a child and applies its Style sizing hints
// (explicit width/height, min/max) — the engine only knows a box's final
// Size, so an inline-block with an explicit CSS width is honored here.
func inlineChildSize(w Widget, avail Size) Size {
	m := w.Measure(avail)
	st := w.Style()
	if st == nil {
		return m
	}
	if st.Width > 0 {
		m.W = st.Width
	}
	if st.Height > 0 {
		m.H = st.Height
	}
	if st.MinWidth > 0 && m.W < st.MinWidth {
		m.W = st.MinWidth
	}
	if st.MinHeight > 0 && m.H < st.MinHeight {
		m.H = st.MinHeight
	}
	if st.MaxWidth > 0 && m.W > st.MaxWidth {
		m.W = st.MaxWidth
	}
	if st.MaxHeight > 0 && m.H > st.MaxHeight {
		m.H = st.MaxHeight
	}
	return m
}
