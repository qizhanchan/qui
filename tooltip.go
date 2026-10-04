package qui

import "unicode"

// TooltipProvider is the optional interface a widget implements to carry
// its own hover-tooltip text. BaseWidget satisfies it (via SetTooltip /
// TooltipText), so every concrete widget — Button, Label, and the rest —
// gets hover tooltips for free; the window's hover tracker reads the
// interface off each widget in the hover path. This is an alternative to
// the explicit Window.AttachTooltip(widget, text) registration; both are
// honored, with an AttachTooltip entry taking precedence.
type TooltipProvider interface {
	TooltipText() string
}

// TooltipAtProvider is the per-POINT counterpart to TooltipProvider, for
// widgets that paint several interactive items themselves instead of
// composing child widgets — a document canvas' table handles, a chart's
// data points, a spreadsheet's column headers, rows a virtualized list
// draws on its own. Such a widget is one dispatch target, so
// TooltipProvider can only say what the WHOLE widget means, and its
// bubble anchors to the whole widget: on a full-page canvas that puts the
// text nowhere near the thing it describes.
//
// Return ok=false (or empty text) for a point that carries no tooltip.
// Both p and the returned anchor are in the WIDGET's own coordinate space —
// the same space as its Bounds() and the geometry it paints; the window maps
// them to/from screen coordinates, so a widget inside a scroll container
// needs no scroll awareness. A changed anchor re-shows the bubble, which is
// what lets the pointer slide from one item to the next inside a single
// widget and swap the text.
//
// Checked before AttachTooltip and TooltipProvider, so a widget can answer
// per item where it knows one and fall back to a whole-widget tooltip
// everywhere else.
type TooltipAtProvider interface {
	TooltipAt(p Point) (text string, anchor Rect, ok bool)
}

// tooltipView is the built-in tooltip renderer. Lives in root so
// Window.showTooltip doesn't need to import the widgets subpackage
// (which would be a cycle — widgets already imports root).
//
// It renders a rounded rectangle with (optionally wrapped) text. The
// text is selectable + copyable: after the pointer slides onto the
// tooltip during its grace period (see Window.updateTooltipFromHover),
// the user can drag to select and Cmd/Ctrl+C to copy — the same model
// widgets.Label uses, reimplemented here because root must not import
// widgets. Apps wanting a richer tooltip can use widgets.Popup instead.
type tooltipView struct {
	BaseWidget
	text string
	font Font
	// wrapWidth is the max content width (scaled px, padding excluded) the
	// text may occupy before soft-wrapping. 0 = unbounded (single line).
	// Set by Window.showTooltip from the window width so long tooltips wrap
	// instead of running off-screen.
	wrapWidth float32

	// Selection state, in rune offsets into text. selStart < 0 means no
	// selection. selecting is true between MouseDown and MouseUp.
	focused   bool
	selecting bool
	selStart  int
	selEnd    int

	// Double-click word-select tracking.
	clickCount  int
	lastClickMS int64
	lastClickX  float32
	lastClickY  float32
}

// ttLine is one visual line as a half-open rune range [start, end) into
// tooltipView.text. Ranges are contiguous and cover the whole string so
// selection offsets map cleanly across wrap points.
type ttLine struct {
	start, end int
}

func newTooltipView(text string) *tooltipView {
	t := &tooltipView{
		BaseWidget: NewBaseWidget(),
		text:       text,
		font:       DefaultStyle().Font,
		selStart:   -1,
	}
	// Colors are theme-driven (see colors()); only geometry lives in style.
	t.style.Padding = Insets{Top: 5, Right: 8, Bottom: 5, Left: 8}
	t.style.Radius = 4
	return t
}

// colors resolves the tooltip's background / text / border from the
// current theme: the lightest surface in the ladder with a gray outline,
// which is what a tooltip chip looks like on qui's light baseline.
//
// qui ships a light scheme only — a dark UI is an app concern expressed
// in CSS on the htmlcss layer, not a second global theme (see theme.go).
// So this reads tokens straight through instead of branching on the
// surface's luminance; retinting the theme retints the tooltip with it.
func (t *tooltipView) colors() (bg, fg, border Color) {
	th := CurrentTheme()
	fg = th.Text
	bg = th.SurfaceOverlay
	border = th.BorderStrong
	return
}

// scaled returns the font, padding, and corner radius the tooltip should
// paint at. Kept as a helper so Measure and Draw share one source.
func (t *tooltipView) scaled() (Font, Insets, float32) {
	return t.font, t.style.Padding, t.style.Radius
}

func (t *tooltipView) lineHeight(font Font) float32 {
	return BuildTextLayout("", font, TextLayoutOptions{}).LineHeight
}

func (t *tooltipView) runesWidth(runes []rune, font Font) float32 {
	w, _ := TextMetrics(string(runes), font)
	return w
}

// wrapLines breaks text into visual lines at newlines and (when maxWidth
// > 0) at word boundaries, falling back to a hard character break for
// words longer than the line. Ranges are contiguous — the space a line
// wraps at stays at the end of that line — so every rune belongs to
// exactly one line and selection offsets stay continuous.
func (t *tooltipView) wrapLines(runes []rune, font Font, maxWidth float32) []ttLine {
	opts := TextLayoutOptions{BreakLongWords: true}
	if maxWidth > 0 {
		opts.MaxWidth = maxWidth
		opts.Wrap = true
	}
	layout := BuildTextLayout(string(runes), font, opts)
	lines := make([]ttLine, 0, len(layout.Lines))
	for _, line := range layout.Lines {
		lines = append(lines, ttLine{start: line.Start, end: line.End})
	}
	return lines
}

func (t *tooltipView) Measure(Size) Size {
	font, padding, _ := t.scaled()
	opts := TextLayoutOptions{BreakLongWords: true}
	if t.wrapWidth > 0 {
		opts.MaxWidth, opts.Wrap = t.wrapWidth, true
	}
	layout := BuildTextLayout(t.text, font, opts)
	return Size{
		W: layout.Width + padding.Horizontal(),
		H: layout.Height + padding.Vertical(),
	}
}

func (t *tooltipView) Draw(canvas Canvas) {
	font, padding, radius := t.scaled()
	bg, fg, border := t.colors()
	rect := t.Bounds()
	if radius > 0 {
		canvas.FillRoundedRect(rect, radius, bg)
	} else {
		canvas.FillRect(rect, bg)
	}
	// 1 px border, inset by half its width so the stroke stays
	// fully inside the tooltip bounds rather than bleeding half outside.
	bw := float32(1)
	if bw > 0 {
		h := bw / 2
		canvas.StrokeRoundedRect(rect.Inset(Insets{Top: h, Right: h, Bottom: h, Left: h}), radius, border, bw)
	}

	content := rect.Inset(padding)
	runes := []rune(t.text)
	lines := t.wrapLines(runes, font, t.wrapWidth)
	lineHeight := t.lineHeight(font)

	// Selection highlight (behind the text).
	if t.hasSelection() {
		a, b := t.orderedSelection()
		selColor := SelectionHighlight(t)
		for i, ln := range lines {
			lo, hi := maxInt(a, ln.start), minInt(b, ln.end)
			if lo >= hi {
				continue
			}
			y := content.Y + float32(i)*lineHeight
			lineText := string(runes[ln.start:ln.end])
			for _, segment := range TextSelectionSegments(lineText, font, TextDirectionAuto, lo-ln.start, hi-ln.start) {
				canvas.FillRect(Rect{X: content.X + segment.X, Y: y, W: segment.Width, H: lineHeight}, selColor)
			}
		}
	}
	opts := TextLayoutOptions{BreakLongWords: true}
	if t.wrapWidth > 0 {
		opts.MaxWidth, opts.Wrap = t.wrapWidth, true
	}
	layout := BuildTextLayout(t.text, font, opts)
	DrawTextLayout(canvas, layout, content, fg, font)
}

// positionFromPoint maps a window-space point to a rune offset in text.
func (t *tooltipView) positionFromPoint(p Point) int {
	font, padding, _ := t.scaled()
	content := t.Bounds().Inset(padding)
	runes := []rune(t.text)
	lines := t.wrapLines(runes, font, t.wrapWidth)
	if len(lines) == 0 {
		return 0
	}
	lineHeight := t.lineHeight(font)
	rel := p.Y - content.Y
	if rel < 0 {
		rel = 0
	}
	idx := int(rel / lineHeight)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(lines) {
		idx = len(lines) - 1
	}
	line := lines[idx]
	targetX := p.X - content.X
	return line.start + TextOffsetAtX(string(runes[line.start:line.end]), font, TextDirectionAuto, targetX)
}

func (t *tooltipView) wordBoundsAt(pos int) (int, int) {
	runes := []rune(t.text)
	if len(runes) == 0 {
		return 0, 0
	}
	if pos >= len(runes) {
		pos = len(runes) - 1
	}
	if pos < 0 {
		pos = 0
	}
	isWord := textWordRune(runes[pos])
	start := pos
	for start > 0 && runes[start-1] != '\n' && textWordRune(runes[start-1]) == isWord {
		start--
	}
	end := pos + 1
	for end < len(runes) && runes[end] != '\n' && textWordRune(runes[end]) == isWord {
		end++
	}
	return start, end
}

func (t *tooltipView) hasSelection() bool { return t.selStart >= 0 && t.selStart != t.selEnd }

// WidgetCursor shows an I-beam over the tooltip's selectable text, so the
// grace-period hand-off (slide onto the bubble to select + copy) reads as
// text. See cursor.go for why this is a provider rather than a SetCursor
// call from Handle.
func (t *tooltipView) WidgetCursor() (CursorShape, bool) { return CursorText, true }

func (t *tooltipView) orderedSelection() (int, int) {
	if t.selStart < 0 {
		return 0, 0
	}
	if t.selStart < t.selEnd {
		return t.selStart, t.selEnd
	}
	return t.selEnd, t.selStart
}

// HitTest returns the tooltip itself so mouse events (select-drag) and
// focus (for Cmd+C) land on it. The tooltip is positioned clear of its
// trigger widget, so intercepting here doesn't shadow that widget.
func (t *tooltipView) HitTest(p Point) Widget {
	if t.Bounds().Contains(p) {
		return t
	}
	return nil
}

// Focusable / SetFocused let the tooltip take keyboard focus on click so
// its Cmd/Ctrl+C copy handler receives the key event.
func (t *tooltipView) Focusable() bool { return true }

func (t *tooltipView) SetFocused(f bool) { t.focused = f }

func (t *tooltipView) Handle(event Event) bool {
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseDown:
			if e.Button != MouseButtonLeft || !t.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return false
			}
			pos := t.positionFromPoint(Point{X: e.X, Y: e.Y})
			if t.registerClick(e) == 2 {
				t.selStart, t.selEnd = t.wordBoundsAt(pos)
			} else {
				t.selStart, t.selEnd = pos, pos
				t.selecting = true
			}
			t.Invalidate()
			return true
		case EventMouseMove:
			if !t.selecting {
				return false // cursor shape comes from WidgetCursor
			}
			t.selEnd = t.positionFromPoint(Point{X: e.X, Y: e.Y})
			t.Invalidate()
			return true
		case EventMouseUp:
			if t.selecting {
				t.selecting = false
				if t.selStart == t.selEnd {
					t.selStart = -1
				}
			}
			return true
		}
	case KeyEvent:
		if !t.focused || e.Type() != EventKeyDown || !IsCommandMod(e.Mods) {
			return false
		}
		runes := []rune(t.text)
		switch e.Key {
		case KeyA:
			if len(runes) > 0 {
				t.selStart, t.selEnd = 0, len(runes)
				t.Invalidate()
			}
			return true
		case KeyC:
			if t.hasSelection() {
				a, b := t.orderedSelection()
				SetClipboardText(string(runes[a:b]))
			}
			return true
		}
	}
	return false
}

func (t *tooltipView) registerClick(e MouseEvent) int {
	now := e.When.UnixMilli()
	if now != 0 && now-t.lastClickMS <= 400 && absF(e.X-t.lastClickX) <= 4 && absF(e.Y-t.lastClickY) <= 4 {
		t.clickCount++
	} else {
		t.clickCount = 1
	}
	if t.clickCount > 3 {
		t.clickCount = 1
	}
	t.lastClickMS, t.lastClickX, t.lastClickY = now, e.X, e.Y
	return t.clickCount
}

// --- TextSelectable: lets the window's cross-widget selection machinery
// (drag arming, Cmd+C aggregation) treat the tooltip like any other
// selectable, and keeps its offsets in the same rune space as the
// tooltip's own intra-widget selection above.

func (t *tooltipView) SelectionOffsetAt(p Point) int { return t.positionFromPoint(p) }

func (t *tooltipView) SetSelectionRange(start, end int) {
	if start < 0 {
		t.ClearTextSelection()
		return
	}
	n := len([]rune(t.text))
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if end < 0 {
		end = 0
	}
	if t.selStart == start && t.selEnd == end {
		return
	}
	t.selStart, t.selEnd = start, end
	t.Invalidate()
}

func (t *tooltipView) ClearTextSelection() {
	if t.selStart < 0 && t.selEnd == 0 {
		return
	}
	t.selStart, t.selEnd, t.selecting = -1, 0, false
	t.Invalidate()
}

func (t *tooltipView) SelectedText() string {
	if !t.hasSelection() {
		return ""
	}
	runes := []rune(t.text)
	a, b := t.orderedSelection()
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

func (t *tooltipView) SelectableLength() int { return len([]rune(t.text)) }

// close removes the tooltip from its window's overlay stack.
func (t *tooltipView) close(w *Window) {
	if w == nil {
		return
	}
	w.RemoveOverlay(t)
}

// textWordRune reports whether r is part of a "word" for double-click
// word selection — letters, digits, and underscore group together;
// everything else (spaces, punctuation) is its own class.
func textWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
