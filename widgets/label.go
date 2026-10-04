package widgets

import (
	"strings"

	. "github.com/qizhanchan/qui"
)

// Label displays text and supports optional rich spans.
//
// `text` and `spans` are intentionally unexported — mutating either
// changes the preferred size, which must trigger an InvalidateLayout
// before the next paint. Mutating a public `Text` field directly used
// to silently truncate the rendered text whenever the new string was
// wider than the original measurement; that footgun is now gone.
// Access via Text() / SetText / Spans() / SetSpans.
//
// Supports paragraph style (wrap/alignment/line-height) and selection
// and copy: double-click word, triple-click logical line, drag to
// expand, Cmd/Ctrl+C to copy the selected text.
type Label struct {
	BaseWidget
	Paragraph  ParagraphStyle
	Selectable bool

	text  string
	spans []TextSpan
	// textKey, when set, supersedes text: Text() resolves it through
	// the message catalog on every read. Set via SetTextKey.
	textKey messageKey

	// Rich-text layout cache. BuildRichTextLayout re-shapes through harfbuzz
	// (~128 µs for a handful of spans) and THREE hot paths want the same
	// answer: Measure — called on every child of every relayout, and a
	// ScrollView re-lays out its whole content subtree on each scroll event —
	// Draw, and LinkAt, which runs on every mouse move via CursorAt. Rebuilt
	// only when an input changes; see labelLayoutKey.
	//
	// The plain-text path needs no equivalent: BuildTextLayout is memoized
	// globally (see the text-layout memo in text.go).
	richLayout RichTextLayout
	richKey    labelLayoutKey
	haveRich   bool
	// contentRev counts content replacements (SetText / SetSpans). Spans hold
	// *Font pointers, so a revision counter is both cheaper and safer than
	// trying to fingerprint the slice itself.
	contentRev uint64

	focused   bool
	cursorPos int
	selStart  int
	selEnd    int

	// downLinkHref is the Href under the MouseDown point (empty if none),
	// so a press-release without a drag opens it — browser click semantics.
	downLinkHref string
}

func NewLabel(text string) *Label {
	l := &Label{
		BaseWidget: NewBaseWidget(),
		text:       text,
		Paragraph:  DefaultParagraphStyle(),
		Selectable: true,
		selStart:   -1,
	}
	return l
}

// Text returns the label's plain-text content. When the label is in
// rich-text mode (SetSpans), this returns the empty string — read the
// concatenated span text from Spans() and flatten it yourself if you
// need a single string from rich content.
//
// When a message key is set (SetTextKey), this returns the RESOLVED
// message — the string the label actually displays — so callers that
// measure or mirror label content stay correct across a language
// switch. Resolution is a map lookup; no layout work happens here.
func (l *Label) Text() string { return l.textKey.resolve(l.text) }

// Spans returns a read-only view of the label's rich-text spans. The
// returned slice MUST NOT be mutated — call SetSpans to replace.
func (l *Label) Spans() []TextSpan { return l.spans }

// SetText replaces the label text and invalidates layout so the next
// paint re-measures with the new string. Clears any rich-text spans.
// Public-field mutation (`l.Text = ...`) is not possible by design —
// see the type comment for why.
func (l *Label) SetText(text string) {
	if l.text == text && len(l.spans) == 0 {
		return
	}
	l.text = text
	l.spans = nil
	l.selStart = -1
	l.selEnd = 0
	l.cursorPos = 0
	l.contentRev++
	l.haveRich = false
	l.InvalidateLayout()
}

// SetSpans replaces the rich-text content and invalidates layout. The
// caller's slice is copied; subsequent mutations to it do NOT affect
// the label.
func (l *Label) SetSpans(spans []TextSpan) {
	l.spans = append(l.spans[:0], spans...)
	l.text = ""
	l.selStart = -1
	l.selEnd = 0
	l.cursorPos = 0
	l.contentRev++
	l.haveRich = false
	l.InvalidateLayout()
}

func (l *Label) Focusable() bool { return l.Enabled() && l.Selectable }

func (l *Label) SetFocused(focused bool) {
	if l.focused == focused {
		return
	}
	l.focused = focused
	l.Invalidate()
}

// resolveForeground returns the effective text color: when the style
// still carries the DefaultStyle ColorBlack (i.e. no caller override),
// fall back to the theme's body text token so a retinted theme carries
// through to plain labels. Callers who set Foreground explicitly — even
// to ColorBlack — get exactly what they asked for.
func (l *Label) resolveForeground() Color {
	fg := l.Style().Foreground
	if fg == ColorBlack {
		return CurrentTheme().Text
	}
	return fg
}

// scaledPadding returns Style().Padding used as Label's internal
// text-inset.
func (l *Label) scaledPadding() Insets {
	return l.Style().Padding
}

// labelLayoutKey fingerprints every input of a built rich-text layout.
//
// TextLayoutOptions is all scalars, so embedding it covers MaxWidth plus the
// whole ParagraphStyle surface a caller can mutate in place (Wrap, Align,
// MaxLines, Ellipsis, LineHeightScale, FirstIndent, BreakLongWords,
// Direction) without needing to re-list any of it. The base font is spelled
// out field by field because Font is not comparable (Features is a slice,
// Variations a map); mutating those two in place already bypasses widget
// invalidation everywhere else in the framework, so they are deliberately out.
// The resolved foreground is in the key too — BuildRichTextLayout bakes it
// into runs that inherit no color of their own.
type labelLayoutKey struct {
	opts TextLayoutOptions
	rev  uint64
	// fontGen invalidates the cache when the font REGISTRY changes (a
	// SetDefaultFont / fallback swap): the font spec is identical but the
	// faces behind it are not, so the shaped result moves. The global text
	// memo is cleared outright on such a change; a per-widget cache can only
	// watch the counter.
	fontGen uint64
	// localeGen invalidates the cache when the active locale changes. A
	// message key resolves to different text, and even identical text
	// can shape differently once locale-sensitive font features apply
	// (Han glyph variants differ between zh-Hans, zh-Hant and ja).
	localeGen     uint64
	fg            Color
	family        string
	size          float32
	weight        FontWeight
	bold          bool
	italic        bool
	smallCaps     bool
	letterSpacing float32
	wordSpacing   float32
}

func (l *Label) layoutKey(opts TextLayoutOptions, fg Color) labelLayoutKey {
	f := l.Style().Font
	return labelLayoutKey{
		opts: opts, rev: l.contentRev, fontGen: FontRegistryGeneration(),
		localeGen: LocaleGeneration(), fg: fg,
		family: f.Family, size: f.Size, weight: f.Weight,
		bold: f.Bold, italic: f.Italic, smallCaps: f.SmallCaps,
		letterSpacing: f.LetterSpacing, wordSpacing: f.WordSpacing,
	}
}

// richTextLayout returns the shaped rich-text layout for these options,
// reusing the cached one when nothing that affects it changed.
func (l *Label) richTextLayout(opts TextLayoutOptions, fg Color) RichTextLayout {
	key := l.layoutKey(opts, fg)
	if l.haveRich && l.richKey == key {
		return l.richLayout
	}
	l.richLayout = BuildRichTextLayout(l.spans, l.Style().Font, fg, opts)
	l.richKey = key
	l.haveRich = true
	return l.richLayout
}

func (l *Label) Measure(available Size) Size {
	padding := l.scaledPadding()
	maxW := float32(0)
	if available.W > 0 {
		maxW = available.W - padding.Horizontal()
		if maxW < 0 {
			maxW = 0
		}
	}
	opts := l.Paragraph.LayoutOptions(maxW)
	var width, height float32
	if len(l.spans) > 0 {
		layout := l.richTextLayout(opts, l.resolveForeground())
		width, height = layout.Width, layout.Height
	} else {
		layout := BuildTextLayout(l.Text(), l.Style().Font, opts)
		width, height = layout.Width, layout.Height
	}
	width += padding.Horizontal()
	height += padding.Vertical()
	if available.W > 0 && width > available.W {
		width = available.W
	}
	if available.H > 0 && height > available.H {
		height = available.H
	}
	return Size{W: width, H: height}
}

func (l *Label) Draw(canvas Canvas) {
	rect := l.Bounds()
	if l.Style().Background.A > 0 {
		if l.Style().Radius > 0 {
			canvas.FillRoundedRect(rect, l.Style().Radius, l.Style().Background)
		} else {
			canvas.FillRect(rect, l.Style().Background)
		}
	}
	content := rect.Inset(l.scaledPadding())
	if l.hasSelection() {
		l.drawSelection(canvas, content)
	}
	opts := l.Paragraph.LayoutOptions(content.W)
	fg := l.resolveForeground()
	if len(l.spans) > 0 {
		DrawRichTextLayout(canvas, l.richTextLayout(opts, fg), content)
		return
	}
	layout := DrawTextBlock(canvas, l.Text(), content, fg, l.Style().Font, opts)
	if l.Paragraph.Decoration != 0 {
		DrawTextDecorationPaint(canvas, layout, content, fg, l.Style().Font, l.Paragraph.Decoration, l.Paragraph.DecorationPaint)
	}
}

// CursorAt reports the hover shape per point: a hand over a hyperlink run,
// an I-beam over the rest of a selectable label. A non-selectable label
// with no links declines, so a `cursor`-declaring ancestor or the window
// default answers instead (see qui/cursor.go for the priority rule).
func (l *Label) CursorAt(p Point) (CursorShape, bool) {
	if !l.Enabled() {
		return CursorDefault, false
	}
	if _, ok := l.LinkAt(p); ok {
		return CursorHand, true
	}
	if l.Selectable {
		return CursorText, true
	}
	return CursorDefault, false
}

// Handle covers what remains widget-local: hyperlink clicks
// hyperlink clicks, and the focused-label clipboard shortcuts. Selection
// itself — press-drag, double-click word, triple-click line, cross-widget
// runs — is driven entirely by the Window's selection controller through
// the TextSelectable / SelectionGranular interfaces.
func (l *Label) Handle(event Event) bool {
	if !l.Enabled() || !l.Selectable {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseDown:
			if e.Button != MouseButtonLeft || !l.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return false
			}
			l.downLinkHref, _ = l.LinkAt(Point{X: e.X, Y: e.Y})
			return false
		case EventMouseMove:
			return false // cursor shape comes from CursorAt
		case EventMouseUp:
			if e.Button != MouseButtonLeft {
				return false
			}
			// A click (press-release without selecting anything) that landed
			// on and released over the same link opens it in the browser.
			if l.downLinkHref != "" && !l.hasSelection() {
				if href, ok := l.LinkAt(Point{X: e.X, Y: e.Y}); ok && href == l.downLinkHref {
					l.downLinkHref = ""
					_ = OpenURL(href)
					return true
				}
			}
			l.downLinkHref = ""
			return false
		}
	case KeyEvent:
		if !l.focused || e.Type() != EventKeyDown || !IsCommandMod(e.Mods) {
			return false
		}
		runes := []rune(l.contentText())
		switch e.Key {
		case KeyA:
			if len(runes) == 0 {
				return true
			}
			l.selStart = 0
			l.selEnd = len(runes)
			l.cursorPos = len(runes)
			l.Invalidate()
			return true
		case KeyC:
			if !l.hasSelection() {
				return true
			}
			a, b := l.orderedSelection()
			SetClipboardText(string(runes[a:b]))
			return true
		}
	}
	return false
}

// --- TextSelectable (window-level cross-widget selection) -----------
//
// These satisfy qui.TextSelectable so the Window can drive a selection
// that spans several Labels (browser-style drag + Cmd/Ctrl+C). They
// operate in the same rune-offset space as the label's own intra-widget
// selection, so the two coexist seamlessly.

// TextSelectionEnabled reports whether this label participates in
// window-driven selection — false for a non-Selectable label (e.g. a
// list-item marker), so it's neither highlighted by a cross-widget drag
// nor copied. Satisfies the window's optional selection-toggle interface.
func (l *Label) TextSelectionEnabled() bool { return l.Selectable }

// SelectionOffsetAt maps a window-space point to a rune offset.
func (l *Label) SelectionOffsetAt(p Point) int {
	return l.positionFromPoint(p)
}

// SetSelectionRange sets the highlighted range (order-independent). A
// negative start, or start == end, clears the highlight.
func (l *Label) SetSelectionRange(start, end int) {
	if start < 0 {
		l.ClearTextSelection()
		return
	}
	n := len([]rune(l.contentText()))
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if end < 0 {
		end = 0
	}
	if l.selStart == start && l.selEnd == end {
		return
	}
	l.selStart = start
	l.selEnd = end
	l.cursorPos = end
	l.Invalidate()
}

// ClearTextSelection removes any selection highlight.
func (l *Label) ClearTextSelection() {
	if l.selStart < 0 && l.selEnd == 0 {
		return
	}
	l.selStart = -1
	l.selEnd = 0
	l.Invalidate()
}

// SelectionRangeAt expands a rune offset to word / hard-line bounds
// (qui.SelectionGranular), giving the window controller double-click and
// triple-click granularity over this label.
func (l *Label) SelectionRangeAt(off int, unit SelectionUnit) (int, int) {
	switch unit {
	case SelectionWord:
		return WordRange(l.contentText(), off)
	case SelectionLine:
		return LineRange(l.contentText(), off)
	}
	return off, off
}

// SelectedText returns the currently-selected substring as plain text.
func (l *Label) SelectedText() string {
	if !l.hasSelection() {
		return ""
	}
	runes := []rune(l.contentText())
	a, b := l.orderedSelection()
	if a < 0 {
		a = 0
	}
	if b > len(runes) {
		b = len(runes)
	}
	if a >= b {
		return ""
	}
	return string(runes[a:b])
}

// SelectableLength returns the total rune count of the content.
func (l *Label) SelectableLength() int {
	return len([]rune(l.contentText()))
}

// SelectedSpans returns the styled spans within the current selection
// (qui.RichTextSelectable), so the window can build an HTML clipboard
// flavor. nil Font/Color on a span are resolved to the label's base
// style so each returned span fully describes itself.
func (l *Label) SelectedSpans() []TextSpan {
	if !l.hasSelection() {
		return nil
	}
	a, b := l.orderedSelection()
	baseFont := l.Style().Font
	baseColor := l.resolveForeground()

	// Plain-text mode: one span over the whole base style.
	if len(l.spans) == 0 {
		runes := []rune(l.Text())
		if a < 0 {
			a = 0
		}
		if b > len(runes) {
			b = len(runes)
		}
		if a >= b {
			return nil
		}
		f, c := baseFont, baseColor
		return []TextSpan{{Text: string(runes[a:b]), Font: &f, Color: &c}}
	}

	// Rich mode: slice each span by its overlap with [a, b], inheriting
	// the base style where the span leaves Font/Color unset.
	var out []TextSpan
	pos := 0
	for _, sp := range l.spans {
		r := []rune(sp.Text)
		s0, s1 := pos, pos+len(r)
		pos = s1
		lo, hi := maxInt(a, s0), minInt(b, s1)
		if lo >= hi {
			continue
		}
		seg := sp
		seg.Text = string(r[lo-s0 : hi-s0])
		if seg.Font == nil {
			f := baseFont
			seg.Font = &f
		}
		if seg.Color == nil {
			c := baseColor
			seg.Color = &c
		}
		out = append(out, seg)
	}
	return out
}

// LinkAt returns the hyperlink target under window-space point p when the
// label shows rich spans and p falls on a span carrying a non-empty Href.
// It rebuilds the same layout Draw uses, so hits line up with the painted
// glyphs. ok is false for plain-text labels or non-link positions.
func (l *Label) LinkAt(p Point) (string, bool) {
	if len(l.spans) == 0 {
		return "", false
	}
	content := l.Bounds().Inset(l.scaledPadding())
	opts := l.Paragraph.LayoutOptions(content.W)
	layout := l.richTextLayout(opts, l.resolveForeground())
	return layout.LinkAt(content, p)
}

func (l *Label) HitTest(p Point) Widget {
	if l.Bounds().Contains(p) {
		return l
	}
	return nil
}

type labelVisualLine struct {
	start  int
	end    int
	offset float32
	// justify is the extra advance per space on this line under
	// text-align:justify (TextLayoutLine.JustifyExtra) — selection and
	// hit-testing must add it so they match the stretched render.
	justify float32
	// width is the line's laid-out width (TextLayoutLine.Width). On a
	// justified line this is exactly the layout MaxWidth; the selection
	// highlight is clamped to it because [start,end) includes the wrap
	// whitespace that was trimmed from the rendered text — measuring that
	// trailing space (plus its justify share) would overshoot the column.
	width float32
}

func (l *Label) contentText() string {
	if len(l.spans) == 0 {
		return l.Text()
	}
	var b strings.Builder
	for _, span := range l.spans {
		b.WriteString(span.Text)
	}
	return b.String()
}

// lineHeight is the vertical pitch selection highlighting and hit-testing
// step by. It is asked of the layout engine (a memoized map lookup) rather
// than re-derived from face metrics: the painter advances by the layout's
// LineHeight, and a local copy of that formula silently diverged when the
// engine's line-height semantics changed (Ceil'd natural height × scale vs
// CSS font-size × scale) — leaving highlights 11 px off per line.
func (l *Label) lineHeight() float32 {
	return BuildTextLayout("", l.Style().Font, l.Paragraph.LayoutOptions(0)).LineHeight
}

// visualLines delegates to the root text layout engine so selection
// highlighting and hit-testing break lines EXACTLY where the renderer
// (DrawTextBlock) does — same word-wrap algorithm, same widths. It used
// to re-implement a char-level wrap here, which disagreed with the
// word-wrapped render and left the last wrapped line's highlight short
// (e.g. "…proper margins." with "margins." unhighlighted). Each returned
// line's [start,end) are rune offsets from the layout, so a full line's
// range reaches the true end of the text.
func (l *Label) visualLines(runes []rune, contentWidth float32) []labelVisualLine {
	if len(runes) == 0 {
		return []labelVisualLine{{start: 0, end: 0}}
	}
	opts := l.Paragraph.LayoutOptions(contentWidth)
	layout := BuildTextLayout(string(runes), l.Style().Font, opts)
	out := make([]labelVisualLine, 0, len(layout.Lines))
	for _, ln := range layout.Lines {
		out = append(out, labelVisualLine{start: ln.Start, end: ln.End, offset: ln.Offset, justify: ln.JustifyExtra, width: ln.Width})
	}
	if len(out) == 0 {
		out = append(out, labelVisualLine{start: 0, end: len(runes)})
	}
	return out
}

func (l *Label) drawSelection(canvas Canvas, content Rect) {
	runes := []rune(l.contentText())
	if len(runes) == 0 {
		return
	}
	a, b := l.orderedSelection()
	if a == b {
		return
	}
	lines := l.visualLines(runes, content.W)
	if len(lines) == 0 {
		return
	}
	lineHeight := l.lineHeight()
	selColor := SelectionHighlight(l)
	for i, line := range lines {
		lineSelStart := maxInt(a, line.start)
		lineSelEnd := minInt(b, line.end)
		if lineSelStart >= lineSelEnd {
			continue
		}
		offset := line.offset
		y := content.Y + float32(i)*lineHeight
		font := l.Style().Font
		font.WordSpacing += line.justify
		lineText := string(runes[line.start:line.end])
		for _, segment := range TextSelectionSegments(lineText, font, l.Paragraph.Direction,
			lineSelStart-line.start, lineSelEnd-line.start) {
			width := segment.Width
			if limit := line.width - segment.X; width > limit {
				width = limit
			}
			if width > 0 {
				canvas.FillRect(Rect{X: content.X + offset + segment.X, Y: y, W: width, H: lineHeight}, selColor)
			}
		}
	}
}

func (l *Label) positionFromPoint(p Point) int {
	content := l.Bounds().Inset(l.scaledPadding())
	runes := []rune(l.contentText())
	lines := l.visualLines(runes, content.W)
	if len(lines) == 0 {
		return 0
	}
	lineHeight := l.lineHeight()
	clickY := p.Y - content.Y
	if clickY < 0 {
		clickY = 0
	}
	lineIndex := int(clickY / lineHeight)
	if lineIndex < 0 {
		lineIndex = 0
	}
	if lineIndex >= len(lines) {
		lineIndex = len(lines) - 1
	}
	line := lines[lineIndex]
	offset := line.offset
	clickX := p.X - content.X - offset
	return l.closestPosInLine(runes, line.start, line.end, clickX, line.justify)
}

func (l *Label) lineOffset(lines []labelVisualLine, idx int, runes []rune, contentWidth float32) float32 {
	lineW := l.runesWidth(runes[lines[idx].start:lines[idx].end])
	alignW := contentWidth
	if !l.Paragraph.Wrap && (l.Paragraph.Align == TextAlignCenter || l.Paragraph.Align == TextAlignEnd) {
		alignW = 0
		for _, ln := range lines {
			w := l.runesWidth(runes[ln.start:ln.end])
			if w > alignW {
				alignW = w
			}
		}
	}
	switch l.Paragraph.Align {
	case TextAlignCenter:
		if alignW > lineW {
			return (alignW - lineW) * 0.5
		}
	case TextAlignEnd:
		if alignW > lineW {
			return alignW - lineW
		}
	}
	return 0
}

func (l *Label) runesWidth(runes []rune) float32 {
	return l.runesWidthJust(runes, 0)
}

// runesWidthJust measures runes adding `justify` extra advance per space —
// the per-line stretch a text-align:justify layout applies when drawing.
func (l *Label) runesWidthJust(runes []rune, justify float32) float32 {
	font := l.Style().Font
	font.WordSpacing += justify
	width, _ := TextMetrics(string(runes), font)
	return width
}

func (l *Label) closestPosInLine(runes []rune, lineStart, lineEnd int, targetX, justify float32) int {
	if lineStart >= lineEnd {
		return lineStart
	}
	font := l.Style().Font
	font.WordSpacing += justify
	return lineStart + TextOffsetAtX(string(runes[lineStart:lineEnd]), font, l.Paragraph.Direction, targetX)
}

func (l *Label) hasSelection() bool { return l.selStart >= 0 && l.selStart != l.selEnd }

func (l *Label) orderedSelection() (int, int) {
	if l.selStart < 0 {
		return 0, 0
	}
	if l.selStart < l.selEnd {
		return l.selStart, l.selEnd
	}
	return l.selEnd, l.selStart
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
