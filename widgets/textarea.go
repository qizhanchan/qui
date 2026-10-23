package widgets

import (
	"strings"
	"time"

	. "github.com/qizhanchan/qui"
)

// TextArea provides multiline text input.
type TextArea struct {
	BaseWidget
	// Cols, when > 0, sizes a box-model (raw-HTML) area to that many
	// average characters — HTML's `cols` attribute (browsers default to
	// 20): Cols × the font's "0" advance (CSS `ch`) plus the 15px of
	// padding, border and scrollbar allowance a browser adds.
	Cols        int
	Text        string
	Placeholder string
	// placeholderKey, when set, supersedes Placeholder and is resolved
	// during Measure/Draw. Read it through DisplayPlaceholder().
	placeholderKey messageKey
	// EnterAddsNewline decides whether a KeyEnter event inserts '\n'.
	// nil means "always true" (default TextArea behavior).
	//
	// Example (spreadsheet-like):
	//   ta.EnterAddsNewline = func(e KeyEvent) bool { return e.Mods&ModAlt != 0 }
	EnterAddsNewline  func(KeyEvent) bool
	AutoWrap          bool // false: wrap only on '\n'; true: soft-wrap by width
	Paragraph         ParagraphStyle
	ShowScrollBarX    bool // show horizontal scrollbar when content overflows
	ShowScrollBarY    bool // show vertical scrollbar when content overflows
	AutoSizeWidth     bool // grow preferred width with the longest logical line
	AutoSizeHeight    bool // grow preferred height with logical line count
	MinWidth          float32
	MinHeight         float32
	MaxWidth          float32 // <=0 means no max
	MaxHeight         float32 // <=0 means no max
	BeforeInput       func(BeforeInputEvent) bool
	OnSelectionChange func(start, end int)
	OnCursorChange    func(pos int)
	// caretNextLine disambiguates a caret exactly on a soft-wrap boundary:
	// the offset is both the end of line N and the start of N+1, so the
	// affinity decides which visual line the caret/Home use.
	caretNextLine  bool
	focused        bool
	hovering       bool
	scrollX        float32 // horizontal scroll offset in pixels (AutoWrap=false only)
	scrollY        float32 // vertical scroll offset in pixels
	barDraggingX   bool
	barDraggingY   bool
	barDragStartX  float32
	barDragStartY  float32
	barDragScrollX float32
	barDragScrollY float32
	OnChange       func(text string)
	// OnSubmit fires on Cmd/Ctrl+Enter (the conventional "finish editing"
	// shortcut for a multi-line field), and OnCommit fires when the field
	// loses focus after the value changed, plus on that same shortcut.
	OnSubmit func(text string)
	OnCommit func(text string)

	// ed is the shared editing model: caret, selection, undo, IME
	// composition, blink, change/commit notification (see text_editor.go).
	ed textEditor

	// layoutCache keeps per-document derived data so paint, hit testing and
	// scrolling cost what the visible lines cost, not the document.
	layoutCache textAreaLayoutCache

	// DrawOverlay, when set, paints app decoration over the text (spelling
	// squiggles): it runs in Draw after the text and the preedit underline
	// and before the caret, clipped to the content rect, in window
	// coordinates. Pair it with RangeRects to locate text.
	DrawOverlay func(canvas Canvas)

	// BoxModel lays the field out like a CSS box (htmlcss sets it on every
	// <textarea>). Style().Padding is then padding INSIDE the border: the
	// text starts at border + padding (an unpadded field keeps the UA
	// inset), and the scrollbars sit in the border's inner edge instead of
	// over the padding. Without it Padding is the whole inset from the
	// outer edge, border included, which is what a Go caller that places
	// the text itself (a cell editor) wants.
	BoxModel bool

	// FocusedStyle, when non-nil, replaces the resting chrome (background,
	// border, radius, text color) while the field holds focus. Nil keeps
	// the built-in focus look: a slightly lifted background and an accent
	// border.
	FocusedStyle *Style
	// HoverStyle, when non-nil, replaces the resting chrome while the
	// pointer is over the field (and it is neither focused nor disabled).
	// Nil keeps the resting look.
	HoverStyle *Style
	// DisabledStyle, when non-nil, replaces the chrome while the field is
	// disabled. Nil dims the baseline text and border colors.
	DisabledStyle *Style
}

type visualLine struct {
	start  int
	end    int
	offset float32 // per-line alignment offset (center / end / justify)
}

func NewTextArea(placeholder string) *TextArea {
	t := &TextArea{
		BaseWidget:     NewBaseWidget(),
		Placeholder:    placeholder,
		AutoWrap:       false,
		Paragraph:      DefaultParagraphStyle(),
		ShowScrollBarX: true,
		ShowScrollBarY: true,
		MinWidth:       htmlInputWidth,
		MinHeight:      40,
		ed:             newTextEditor(),
	}
	t.SetSelf(t)
	// Raw HTML <textarea>: white box, thin #767676 border, black text,
	// 2 px corner, tight UA padding. For a designed look, style a
	// <textarea> on the htmlcss layer; CSS overrides these per-instance.
	t.Style().Background = htmlControlBg
	t.Style().Foreground = htmlControlText
	t.Style().Border = htmlControlBorder
	t.Style().BorderSize = 1
	t.Style().Radius = htmlControlRadius
	t.Style().Padding = Insets{Top: htmlControlPadY, Right: htmlControlPadX, Bottom: htmlControlPadY, Left: htmlControlPadX}
	t.Style().Font = Font{Size: htmlControlFontSize}
	return t
}

// SetAutoSize toggles width/height sizing from the document and re-measures.
func (t *TextArea) SetAutoSize(width, height bool) {
	if t.AutoSizeWidth == width && t.AutoSizeHeight == height {
		return
	}
	t.AutoSizeWidth, t.AutoSizeHeight = width, height
	t.InvalidateLayout()
}

// SetCols sets the raw-HTML `cols` width hint.
func (t *TextArea) SetCols(n int) {
	if t.Cols == n {
		return
	}
	t.Cols = n
	t.InvalidateLayout()
}

// SetParagraph replaces the paragraph layout options.
func (t *TextArea) SetParagraph(p ParagraphStyle) {
	t.Paragraph = p
	t.InvalidateLayout()
}

// font returns the textarea's font. All measurement / draw paths funnel
// through this so input text, preedit, cursor math, and wrap
// calculations stay aligned with the surrounding chrome.
func (t *TextArea) font() Font {
	return t.Style().Font
}

func (t *TextArea) Measure(available Size) Size {
	width := t.MinWidth
	height := t.MinHeight
	if width < 0 {
		width = 300
	}
	if height < 0 {
		height = 100
	}
	if t.AutoSizeWidth {
		width = max(width, t.naturalContentWidth()+t.insets().Horizontal())
	}
	if t.AutoSizeHeight {
		height = max(height, float32(t.logicalLineCount())*t.lineHeight()+t.insets().Vertical())
	}
	if t.BoxModel {
		// Two text lines inside the padding and border, as a browser sizes
		// a default <textarea rows=2>.
		height = max(height, 2*t.lineHeight()+t.insets().Vertical())
		if t.Cols > 0 {
			width = float32(t.Cols)*RuneAdvance('0', t.font()) + 15
		}
	}
	if t.MaxWidth > 0 && width > t.MaxWidth {
		width = t.MaxWidth
	}
	if t.MaxHeight > 0 && height > t.MaxHeight {
		height = t.MaxHeight
	}
	if available.W > 0 && width > available.W {
		width = available.W
	}
	if available.H > 0 && height > available.H {
		height = available.H
	}
	return Size{W: width, H: height}
}

// insets is the distance from the outer edge to the text on each side (see
// BoxModel).
func (t *TextArea) insets() Insets {
	st := t.Style()
	if !t.BoxModel {
		return st.Padding
	}
	bw := borderInsets(st)
	p := st.Padding
	if p == (Insets{}) {
		return Insets{
			Top: max(htmlControlPadY, bw.Top), Right: max(htmlControlPadX, bw.Right),
			Bottom: max(htmlControlPadY, bw.Bottom), Left: max(htmlControlPadX, bw.Left),
		}
	}
	return Insets{Top: bw.Top + p.Top, Right: bw.Right + p.Right, Bottom: bw.Bottom + p.Bottom, Left: bw.Left + p.Left}
}

// borderInsets is the border thickness on each side: the per-side widths
// when any is set (CSS border-top etc.), else the uniform BorderSize.
func borderInsets(st *Style) Insets {
	if hasPerSideBorders(st) {
		return st.BorderWidths
	}
	b := st.BorderSize
	return Insets{Top: b, Right: b, Bottom: b, Left: b}
}

// barGutter is the CSS padding between the content and the scrollbars,
// which BoxModel puts against the border's inner edge. Zero otherwise: the
// bars then sit flush against the content.
func (t *TextArea) barGutter() Insets {
	if !t.BoxModel {
		return Insets{}
	}
	in, bw := t.insets(), borderInsets(t.Style())
	return Insets{Top: max(in.Top-bw.Top, 0), Right: max(in.Right-bw.Right, 0), Bottom: max(in.Bottom-bw.Bottom, 0), Left: max(in.Left-bw.Left, 0)}
}

// barSize is the scrollbar thickness.
func (t *TextArea) barSize() float32 {
	if t.BoxModel {
		return VBarWidth
	}
	return 8
}

// chrome resolves the style the box paints with right now: disabled, then
// focused, then hovered, else resting. custom reports that the variant is
// an author-supplied one (so the built-in focus lift doesn't stack on it).
func (t *TextArea) chrome() (st *Style, custom bool) {
	switch {
	case !t.Enabled():
		if t.DisabledStyle != nil {
			return themedStyle(t.DisabledStyle), true
		}
		dim := *t.Style()
		if dim.Foreground == htmlControlText {
			dim.Foreground = htmlDisabledText
		}
		if dim.Border == htmlControlBorder {
			dim.Border = htmlDisabledBorder
		}
		return themedStyle(&dim), true
	case t.focused && t.FocusedStyle != nil:
		return themedStyle(t.FocusedStyle), true
	case !t.focused && t.hovering && t.HoverStyle != nil:
		return themedStyle(t.HoverStyle), true
	}
	return themedStyle(t.Style()), false
}

func (t *TextArea) Draw(canvas Canvas) {
	rect := t.Bounds()

	// Chrome: the resting style, or the disabled / focused / hover variant.
	// All go through themedStyle so the native baseline colors follow
	// SetTheme (like Input); colors an app set itself pass through.
	st, custom := t.chrome()
	bg := st.Background
	if t.focused && !custom {
		bg = Color{R: min(bg.R+0.05, 1), G: min(bg.G+0.05, 1), B: min(bg.B+0.05, 1), A: bg.A}
	}
	// CSS box-shadow (outset) behind the box, farthest layer first.
	for i := len(st.ExtraShadows) - 1; i >= 0; i-- {
		if sh := st.ExtraShadows[i]; !sh.IsZero() {
			canvas.DrawShadow(rect, st.Radius, ElevationSpec{X: sh.X, Y: sh.Y, Blur: sh.Blur, Spread: sh.Spread, Opacity: 1}, sh.Color)
		}
	}
	if sh := st.Shadow; !sh.IsZero() {
		canvas.DrawShadow(rect, st.Radius, ElevationSpec{X: sh.X, Y: sh.Y, Blur: sh.Blur, Spread: sh.Spread, Opacity: 1}, sh.Color)
	}
	perCorner := !st.Corners.IsZero()
	switch {
	case perCorner:
		canvas.DrawShape(ShapePath{Path: cornerPath(rect, st.Corners.Resolved(st.Radius))},
			Paint{Style: PaintFill, Color: bg, AntiAlias: true})
	case st.Radius > 0:
		canvas.FillRoundedRect(rect, st.Radius, bg)
	default:
		canvas.FillRect(rect, bg)
	}

	borderColor := st.Border
	if t.focused && !custom {
		borderColor = Color{R: 0.4, G: 0.6, B: 1.0, A: 1}
		if !themeIsBaseline() {
			borderColor = CurrentTheme().Accent
		}
	}
	switch {
	case hasPerSideBorders(st):
		sides := *st
		if t.focused && !custom {
			// The built-in focus ring recolours every side.
			sides.Border, sides.BorderColors = borderColor, SideColors{}
		}
		paintPerSideBorders(canvas, rect, &sides)
	case st.BorderSize <= 0:
	case perCorner:
		half := st.BorderSize / 2
		r := Rect{X: rect.X + half, Y: rect.Y + half, W: rect.W - st.BorderSize, H: rect.H - st.BorderSize}
		canvas.DrawShape(ShapePath{Path: cornerPath(r, insetCorners(st.Corners.Resolved(st.Radius), half))},
			Paint{Style: PaintStroke, Color: borderColor, StrokeWidth: st.BorderSize, AntiAlias: true, Join: JoinRound})
	case st.Radius > 0:
		canvas.StrokeRoundedRect(rect, st.Radius, borderColor, st.BorderSize)
	default:
		canvas.StrokeRect(rect, borderColor, st.BorderSize)
	}

	content, showScrollX, showScrollY := t.visibleContentRect(rect)
	contentID := canvas.Save()
	canvas.ClipRect(content)

	// Draw text or placeholder
	displayText := t.Text
	textColor := st.Foreground
	if displayText == "" && !t.focused {
		displayText = t.DisplayPlaceholder()
		textColor = CurrentTheme().TextMuted
	}

	// While composing, INSERT the in-flight preedit into the displayed text
	// at the caret so it flows inline (pushing following text right and
	// rewrapping) instead of being painted on top of the following
	// characters. The caret/underline are positioned from the pre-insertion
	// cursor (getCursorPosition reads t.Text), which is exactly where the
	// preedit begins.
	composing := t.ed.preedit != "" && t.focused
	if composing {
		runes := t.textRunes()
		cp := t.ed.cursor
		if cp > len(runes) {
			cp = len(runes)
		}
		displayText = string(runes[:cp]) + t.ed.preedit + string(runes[cp:])
		textColor = st.Foreground
	}

	// Draw selection beneath text
	// Only a focused field shows its selection; it is kept across a blur so
	// a refocus restores it, as in a browser.
	if t.focused && t.Text != "" && t.ed.hasSelection() {
		t.drawSelection(canvas, content)
	}

	if displayText != "" {
		t.drawVisibleText(canvas, displayText, content, textColor)
	}

	// Underline the preedit so users see it's not yet committed (the text
	// itself is now drawn inline as part of displayText above).
	if composing {
		cursorX, cursorY := t.getCursorPosition(content)
		if !t.wrapEnabled() {
			cursorX -= t.scrollX
		}
		cursorY -= t.scrollY
		preeditW, _ := TextMetrics(t.ed.preedit, t.font())
		canvas.FillRect(
			Rect{X: cursorX, Y: cursorY + t.lineHeight() - 2, W: preeditW, H: 1},
			textColor)
	}

	if t.DrawOverlay != nil {
		t.DrawOverlay(canvas)
	}

	// Draw cursor whenever focused. Blink toggling happens in Tick, so
	// this path only reads cursorVisible without side effects. While
	// composing, ignore blink so the IME anchor stays visible.
	if t.focused {
		if t.ed.caretVisible || t.ed.preedit != "" {
			cursorX, cursorY := t.getCursorPosition(content)
			if !t.wrapEnabled() {
				cursorX -= t.scrollX
			}
			cursorY -= t.scrollY
			cursorY = float32(int(cursorY))
			cursorHeight := t.lineHeight()

			// While composing, shift cursor past the preedit cursor offset.
			if t.ed.preedit != "" {
				runes := []rune(t.ed.preedit)
				cur := t.ed.preeditCursor
				if cur > len(runes) {
					cur = len(runes)
				}
				cursorX += TextXForOffset(t.ed.preedit, t.font(), TextDirectionAuto, cur)
			}

			// Only draw cursor if visible in content area
			if cursorY >= content.Y && cursorY+cursorHeight <= content.Y+content.H {
				canvas.FillRect(
					Rect{X: cursorX, Y: cursorY, W: 1, H: cursorHeight},
					st.Foreground,
				)
			}
		}
	}

	canvas.RestoreTo(contentID)

	// Draw scrollbars if configured and content overflows.
	t.drawScrollbars(canvas, content, showScrollX, showScrollY)
}

// cursorLineIndex maps the caret offset to a visual line. A soft-wrapped
// boundary belongs to two lines with the same rune offset; caretNextLine
// picks the lower one after a Right-arrow / click on the continuation.
func (t *TextArea) cursorLineIndex(lines []visualLine) int {
	if len(lines) == 0 {
		return 0
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if t.ed.cursor < line.start || t.ed.cursor > line.end {
			continue
		}
		if !t.caretNextLine && i > 0 && t.ed.cursor == line.start && lines[i-1].end == t.ed.cursor {
			continue // boundary without "next line" affinity -> previous line
		}
		return i
	}
	return 0
}

func (t *TextArea) getCursorPosition(content Rect) (x, y float32) {
	runes := t.textRunes()
	lineHeight := t.lineHeight()
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return content.X, content.Y
	}
	y = content.Y

	lineIdx := t.cursorLineIndex(lines)
	line := lines[lineIdx]
	y += float32(lineIdx) * lineHeight
	end := t.ed.cursor
	if end < line.start {
		end = line.start
	}
	if end > line.end {
		end = line.end
	}
	lineText := string(runes[line.start:line.end])
	x = content.X + line.offset + TextXForOffset(lineText, t.font(), t.Paragraph.Direction, end-line.start)
	return x, y
}

func (t *TextArea) drawVisibleText(canvas Canvas, text string, content Rect, color Color) {
	layout := t.layoutFor(text, content.W)
	drawRect := Rect{X: content.X, Y: content.Y - t.scrollY, W: content.W, H: layout.Height}
	if !t.wrapEnabled() {
		drawRect.X -= t.scrollX
	}
	DrawTextLayout(canvas, layout, drawRect, color, t.font())
}

func (t *TextArea) drawSelection(canvas Canvas, content Rect) {
	if t.ed.selStart < 0 || t.ed.selStart == t.ed.selEnd {
		return
	}
	selColor := SelectionHighlight(t)
	for _, r := range t.rangeRects(content, t.ed.selStart, t.ed.selEnd) {
		canvas.FillRect(r, selColor)
	}
}

// RangeRects returns the boxes covering runes [start, end) of Text in
// window coordinates, after scrolling: one box per visual line (a line of
// mixed-direction text yields one box per visual run, as the selection
// highlight does). Lines scrolled outside the content rect are omitted.
// The in-flight IME preedit is ignored — offsets index Text.
func (t *TextArea) RangeRects(start, end int) []Rect {
	content, _, _ := t.visibleContentRect(t.Bounds())
	return t.rangeRects(content, start, end)
}

func (t *TextArea) rangeRects(content Rect, start, end int) []Rect {
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if start == end {
		return nil
	}
	runes := t.textRunes()
	lineHeight := t.lineHeight()
	lines := t.visualLines(content.W)
	var out []Rect
	for i, line := range lines {
		y := content.Y - t.scrollY + float32(i)*lineHeight
		if y+lineHeight <= content.Y || y >= content.Y+content.H {
			continue
		}
		lineSelStart, lineSelEnd := start, end
		if lineSelStart < line.start {
			lineSelStart = line.start
		}
		if lineSelEnd > line.end {
			lineSelEnd = line.end
		}
		if lineSelStart >= lineSelEnd {
			continue
		}
		lineText := string(runes[line.start:line.end])
		for _, segment := range TextSelectionSegments(lineText, t.font(), t.Paragraph.Direction,
			lineSelStart-line.start, lineSelEnd-line.start) {
			startX := content.X + line.offset + segment.X
			if !t.wrapEnabled() {
				startX -= t.scrollX
			}
			out = append(out, Rect{X: startX, Y: y, W: segment.Width, H: lineHeight})
		}
	}
	return out
}

func (t *TextArea) drawScrollbars(canvas Canvas, content Rect, showX, showY bool) {
	if !showX && !showY {
		return
	}
	if t.BoxModel {
		t.drawBoxScrollbars(canvas, content, showX, showY)
		return
	}
	theme := CurrentTheme()
	trackColor := theme.SurfaceRaised
	thumbColor := theme.TextMuted

	if showY {
		trackRect := t.verticalBarTrack(content)
		thumbRect := t.verticalBarThumb(content)
		canvas.FillRect(trackRect, trackColor)
		canvas.FillRect(thumbRect, thumbColor)
	}
	if showX {
		trackRect := t.horizontalBarTrack(content)
		thumbRect := t.horizontalBarThumb(content)
		canvas.FillRect(trackRect, trackColor)
		canvas.FillRect(thumbRect, thumbColor)
	}
}

// drawBoxScrollbars paints the shared thin rounded scrollbar, clipped to
// the inside of the (rounded) border so a bar never overdraws it.
func (t *TextArea) drawBoxScrollbars(canvas Canvas, content Rect, showX, showY bool) {
	st := t.Style()
	inner := t.Bounds().Inset(borderInsets(st))
	id := canvas.Save()
	defer canvas.RestoreTo(id)
	if !st.Corners.IsZero() {
		canvas.ClipPath(cornerPath(inner, insetCorners(st.Corners.Resolved(st.Radius), st.BorderSize)))
	} else if st.Radius > 0 {
		canvas.ClipPath(NewPath().AddRRect(inner, max(st.Radius-st.BorderSize, 0)))
	} else {
		canvas.ClipRect(inner)
	}
	if showY {
		VBarDraw(canvas, t.verticalBarTrack(content), t.verticalBarThumb(content), t.barDraggingY)
	}
	if showX {
		VBarDraw(canvas, t.horizontalBarTrack(content), t.horizontalBarThumb(content), t.barDraggingX)
	}
}

func (t *TextArea) verticalBarTrack(content Rect) Rect {
	g := t.barGutter()
	return Rect{X: content.X + content.W + g.Right, Y: content.Y - g.Top, W: t.barSize(), H: content.H + g.Top + g.Bottom}
}

func (t *TextArea) horizontalBarTrack(content Rect) Rect {
	g := t.barGutter()
	return Rect{X: content.X - g.Left, Y: content.Y + content.H + g.Bottom, W: content.W + g.Left + g.Right, H: t.barSize()}
}

func (t *TextArea) verticalBarThumb(content Rect) Rect {
	track := t.verticalBarTrack(content)
	contentHeight := t.getContentHeight(content)
	if contentHeight <= 0 {
		return track
	}
	thumbH := (content.H / contentHeight) * content.H
	if thumbH < 20 {
		thumbH = 20
	}
	if thumbH > track.H {
		thumbH = track.H
	}
	maxScrollY := contentHeight - content.H
	scrollY := t.scrollY
	if scrollY < 0 {
		scrollY = 0
	}
	if maxScrollY > 0 && scrollY > maxScrollY {
		scrollY = maxScrollY
	}
	y := track.Y
	if maxScrollY > 0 {
		y += (scrollY / maxScrollY) * (track.H - thumbH)
	}
	return Rect{X: track.X, Y: y, W: track.W, H: thumbH}
}

func (t *TextArea) horizontalBarThumb(content Rect) Rect {
	track := t.horizontalBarTrack(content)
	contentWidth := t.getContentWidth(content)
	if contentWidth <= 0 {
		return track
	}
	thumbW := (content.W / contentWidth) * content.W
	if thumbW < 20 {
		thumbW = 20
	}
	if thumbW > track.W {
		thumbW = track.W
	}
	maxScrollX := contentWidth - content.W
	scrollX := t.scrollX
	if scrollX < 0 {
		scrollX = 0
	}
	if maxScrollX > 0 && scrollX > maxScrollX {
		scrollX = maxScrollX
	}
	x := track.X
	if maxScrollX > 0 {
		x += (scrollX / maxScrollX) * (track.W - thumbW)
	}
	return Rect{X: x, Y: track.Y, W: thumbW, H: track.H}
}

func (t *TextArea) Handle(event Event) bool {
	if !t.Enabled() {
		return false
	}
	// Text is an exported field, so a caller can replace it without going
	// through SetText. Clamp the caret/selection before any edit path
	// slices the rune buffer.
	t.ed.normalize(t.Text)
	prev := t.ed.view()
	prevText, prevScrollX, prevScrollY := t.Text, t.scrollX, t.scrollY
	prevDragX, prevDragY, prevHovering := t.barDraggingX, t.barDraggingY, t.hovering
	defer func() {
		t.ed.emitStateChange(t, prev.caret)
		if t.ed.view() != prev ||
			t.Text != prevText ||
			t.scrollX != prevScrollX ||
			t.scrollY != prevScrollY ||
			t.barDraggingX != prevDragX ||
			t.barDraggingY != prevDragY ||
			t.hovering != prevHovering {
			t.Invalidate()
		}
	}()

	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			t.hovering = true
		case EventMouseLeave:
			t.hovering = false
		case EventMouseDown:
			if t.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				if t.handleBarMouseDown(e) {
					return true
				}
				t.handleMouseDown(e)
				return true
			}
		case EventMouseMove:
			if t.barDraggingX || t.barDraggingY {
				t.handleBarDrag(e)
				return true
			}
			if t.ed.selecting {
				t.handleMouseDrag(e)
				return true
			}
		case EventMouseUp:
			if t.barDraggingX || t.barDraggingY {
				t.barDraggingX = false
				t.barDraggingY = false
				return true
			}
			if t.ed.selecting {
				t.ed.release()
				return true
			}
		case EventScroll:
			if t.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				t.handleScroll(e)
				return true
			}
		}
	case KeyEvent:
		if t.focused && e.Type() == EventKeyDown {
			// IME swallow: while composition is active, text-editing
			// keys (Backspace, Enter, arrows, Escape) belong to the
			// input method — GLFW fires our KeyCallback before
			// interpretKeyEvents reaches the IME, so without this
			// guard Backspace would hit both paths and delete from
			// Text AND from preedit.
			if t.ed.preedit != "" {
				return true
			}
			return t.handleKeyDown(e)
		}
	case CharEvent:
		if t.focused {
			return t.ed.typeRune(t, e.Rune)
		}
	}

	return false
}

// EnterWouldInsertNewline reports whether this KeyEnter event should be
// handled as a newline insertion for this TextArea.
func (t *TextArea) EnterWouldInsertNewline(e KeyEvent) bool {
	if t.EnterAddsNewline != nil {
		return t.EnterAddsNewline(e)
	}
	return true
}

func (t *TextArea) handleMouseDown(e MouseEvent) {
	if e.Button != MouseButtonLeft {
		return
	}
	t.ed.pressAt(t, t.positionFromPoint(Point{X: e.X, Y: e.Y}), e)
	t.hovering = true
}

func (t *TextArea) handleMouseDrag(e MouseEvent) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	if e.Y < content.Y {
		t.scrollY -= min(24, content.Y-e.Y)
	} else if e.Y > content.Y+content.H {
		t.scrollY += min(24, e.Y-(content.Y+content.H))
	}
	if !t.wrapEnabled() {
		if e.X < content.X {
			t.scrollX -= min(24, content.X-e.X)
		} else if e.X > content.X+content.W {
			t.scrollX += min(24, e.X-(content.X+content.W))
		}
	}
	t.clampScroll(content)
	t.ed.dragTo(t.positionFromPoint(Point{X: e.X, Y: e.Y}))
	t.scrollToCursor()
}

func (t *TextArea) handleBarMouseDown(e MouseEvent) bool {
	if e.Button != MouseButtonLeft {
		return false
	}
	content, showX, showY := t.visibleContentRect(t.Bounds())
	p := Point{X: e.X, Y: e.Y}
	if showY {
		thumb := t.verticalBarThumb(content)
		track := t.verticalBarTrack(content)
		if thumb.Contains(p) {
			t.barDraggingY = true
			t.barDragStartY = e.Y
			t.barDragScrollY = t.scrollY
			return true
		}
		if track.Contains(p) {
			if e.Y < thumb.Y {
				t.scrollY -= content.H * 0.9
			} else {
				t.scrollY += content.H * 0.9
			}
			t.clampScroll(content)
			return true
		}
	}
	if showX {
		thumb := t.horizontalBarThumb(content)
		track := t.horizontalBarTrack(content)
		if thumb.Contains(p) {
			t.barDraggingX = true
			t.barDragStartX = e.X
			t.barDragScrollX = t.scrollX
			return true
		}
		if track.Contains(p) {
			if e.X < thumb.X {
				t.scrollX -= content.W * 0.9
			} else {
				t.scrollX += content.W * 0.9
			}
			t.clampScroll(content)
			return true
		}
	}
	return false
}

func (t *TextArea) handleBarDrag(e MouseEvent) {
	content, showX, showY := t.visibleContentRect(t.Bounds())
	if t.barDraggingY && showY {
		track := t.verticalBarTrack(content)
		thumb := t.verticalBarThumb(content)
		travel := track.H - thumb.H
		maxScrollY := t.getContentHeight(content) - content.H
		if travel > 0 && maxScrollY > 0 {
			dy := e.Y - t.barDragStartY
			t.scrollY = t.barDragScrollY + dy*(maxScrollY/travel)
		}
	}
	if t.barDraggingX && showX {
		track := t.horizontalBarTrack(content)
		thumb := t.horizontalBarThumb(content)
		travel := track.W - thumb.W
		maxScrollX := t.getContentWidth(content) - content.W
		if travel > 0 && maxScrollX > 0 {
			dx := e.X - t.barDragStartX
			t.scrollX = t.barDragScrollX + dx*(maxScrollX/travel)
		}
	}
	t.clampScroll(content)
}

// OffsetAt returns the rune offset into Text nearest window point p (the
// caret position a click there would place), accounting for scrolling.
// It is a pure query: the caret's line affinity is left alone.
func (t *TextArea) OffsetAt(p Point) int {
	off, _ := t.offsetAndAffinityAt(p)
	return off
}

// positionFromPoint is OffsetAt for the press / drag paths, which place the
// caret there and so adopt the point's line affinity.
func (t *TextArea) positionFromPoint(p Point) int {
	off, nextLine := t.offsetAndAffinityAt(p)
	t.caretNextLine = nextLine
	return off
}

// offsetAndAffinityAt maps p to a rune offset, plus whether that offset
// sits at the start of a soft-wrapped line rather than the previous end.
func (t *TextArea) offsetAndAffinityAt(p Point) (int, bool) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	runes := t.textRunes()
	lineHeight := t.lineHeight()
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return 0, false
	}

	clickX := p.X - content.X
	if !t.wrapEnabled() {
		clickX += t.scrollX
	}
	clickY := p.Y - content.Y + t.scrollY

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
	nextLine := lineIndex > 0 && line.start == lines[lineIndex-1].end && clickX <= line.offset
	return t.closestPosInLine(runes, line.start, line.end, clickX-line.offset), nextLine
}

func (t *TextArea) runesWidth(runes []rune) float32 {
	if len(runes) == 0 {
		return 0
	}
	width, _ := TextMetrics(string(runes), t.font())
	return width
}

func (t *TextArea) closestPosInLine(runes []rune, lineStart, lineEnd int, targetX float32) int {
	if lineStart >= lineEnd {
		return lineStart
	}
	lineText := string(runes[lineStart:lineEnd])
	return lineStart + TextOffsetAtX(lineText, t.font(), t.Paragraph.Direction, targetX)
}

// handleKeyDown adds the multi-line Enter to the shared editing keys: a
// plain Enter inserts a newline (unless EnterAddsNewline declines it), and
// Cmd/Ctrl+Enter is the conventional "submit / finish" chord.
func (t *TextArea) handleKeyDown(e KeyEvent) bool {
	if e.Key != KeyEnter {
		return t.ed.handleKey(t, e)
	}
	if IsCommandMod(e.Mods) {
		// Commit before submit, as Input does.
		submitted := t.Text
		t.ed.fireCommit(t)
		if t.OnSubmit != nil {
			t.OnSubmit(submitted)
		}
		if w := t.Window(); w != nil {
			w.RecordInputSubmit(t, submitted)
		}
		return true
	}
	if !t.EnterWouldInsertNewline(e) {
		return false
	}
	t.ed.newline(t)
	return true
}

func (t *TextArea) handleScroll(e MouseEvent) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	dx, dy := e.WheelPixels()
	t.scrollY -= dy
	if !t.wrapEnabled() {
		if e.Mods&ModShift != 0 && dx == 0 {
			dx = dy
		}
		t.scrollX -= dx
	}
	t.clampScroll(content)
}

// editorVertical maps Up/Down to the nearest offset on the visual line
// above/below, keeping the caret's x.
func (t *TextArea) editorVertical(direction int) (int, bool) {
	runes := t.textRunes()
	content, _, _ := t.visibleContentRect(t.Bounds())
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return t.ed.cursor, true
	}
	lineIdx := t.cursorLineIndex(lines)
	currentLine := lines[lineIdx]
	end := t.ed.cursor
	if end > currentLine.end {
		end = currentLine.end
	}
	currentText := string(runes[currentLine.start:currentLine.end])
	targetX := TextXForOffset(currentText, t.font(), t.Paragraph.Direction, end-currentLine.start)
	targetIdx := clampInt(lineIdx+direction, 0, len(lines)-1)
	targetLine := lines[targetIdx]
	return t.closestPosInLine(runes, targetLine.start, targetLine.end, targetX), true
}

func (t *TextArea) visualHorizontalTarget(delta int) int {
	runes := t.textRunes()
	content, _, _ := t.visibleContentRect(t.Bounds())
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return 0
	}
	lineIndex := t.cursorLineIndex(lines)
	line := lines[lineIndex]
	text := string(runes[line.start:line.end])
	local := t.ed.cursor - line.start
	target := line.start + TextVisualNeighbor(text, t.font(), t.Paragraph.Direction, local, delta)
	if target != t.ed.cursor {
		return target
	}
	if delta < 0 && lineIndex > 0 {
		return lines[lineIndex-1].end
	}
	if delta > 0 && lineIndex+1 < len(lines) {
		return lines[lineIndex+1].start
	}
	return target
}

// editorLineBounds is the visual line holding the caret (Home / End).
func (t *TextArea) editorLineBounds() (start, end int) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return 0, 0
	}
	line := lines[t.cursorLineIndex(lines)]
	return line.start, line.end
}

// PaintBounds covers the shadow / focus-border halo painted outside the
// layout rect. Without it a neighbor's partial repaint can clear the halo
// and leave a stale fringe.
func (t *TextArea) PaintBounds() Rect {
	b := t.Bounds()
	st := t.Style()
	r := b.Union(shadowPaintBounds(b, st.Shadow))
	for _, sh := range st.ExtraShadows {
		r = r.Union(shadowPaintBounds(b, sh))
	}
	return expandRect(r, st.BorderSize/2+2)
}

// WidgetCursor shows the text I-beam over an editable area; a disabled area
// declines (see Input.WidgetCursor and qui/cursor.go).
func (t *TextArea) WidgetCursor() (CursorShape, bool) {
	return CursorText, t.Enabled()
}

func (t *TextArea) Focusable() bool {
	return t.Enabled()
}

func (t *TextArea) CancelInteraction() {
	t.ed.selecting = false
	t.hovering = false
	t.barDraggingX = false
	t.barDraggingY = false
	t.Invalidate()
}

func (t *TextArea) SetFocused(focused bool) {
	prev := t.ed.caretState()
	was := t.focused
	t.focused = focused
	t.ed.focusChanged(t, was, focused)
	if !focused {
		// Do not clear hovering here: the pointer may still be over the
		// field, and no new Enter event will arrive to restore it.
		t.barDraggingX = false
		t.barDraggingY = false
	}
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

func (t *TextArea) GetText() string {
	return t.Text
}

// NotifyTextChanged implements qui.TextChangeNotifier: publish the current
// text as a user-equivalent change. SetText itself stays silent (a
// programmatic set must not re-enter the app's handler), so the agent's Type
// action calls this after its bulk set to make the two paths behave alike.
func (t *TextArea) NotifyTextChanged() { t.ed.fireChange(t) }

// TextState reports the textarea's editing state for the AX / agent debug
// surface (qui.TextEditable).
func (t *TextArea) TextState() TextState { return t.ed.textState(t.Text) }

// SetText programmatically replaces the content and parks the caret at the
// end. Silent by design (see qui.TextChangeNotifier); call
// NotifyTextChanged to publish a user-equivalent change. Undoable.
func (t *TextArea) SetText(text string) {
	prev := t.ed.caretState()
	if !t.ed.setText(t, text) {
		return
	}
	t.scrollX = 0
	t.scrollY = 0
	t.scrollToCursor()
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

// MoveCursorToEnd positions the caret after the last rune and clears
// any selection. Useful after programmatically seeding text (e.g. an
// inline cell editor preloading the existing cell value) so subsequent
// keystrokes append instead of inserting at the start.
func (t *TextArea) MoveCursorToEnd() { t.SetCursorPos(len(t.textRunes())) }

// getContentHeight calculates the total height of the text content
func (t *TextArea) getContentHeight(content Rect) float32 {
	lineHeight := t.lineHeight()
	lines := t.visualLines(content.W)
	if len(lines) == 0 {
		return lineHeight
	}
	return float32(len(lines)) * lineHeight
}

// scrollToCursor ensures the cursor is visible by adjusting scrollY
func (t *TextArea) scrollToCursor() {
	content, _, _ := t.visibleContentRect(t.Bounds())
	cursorX, cursorY := t.getCursorPosition(content)
	lineHeight := t.lineHeight()

	// Adjust cursorY to be relative to content area (before scroll offset)
	cursorYRelative := cursorY - content.Y

	// Check if cursor is above visible area
	if cursorYRelative < t.scrollY {
		t.scrollY = cursorYRelative
	}

	// Check if cursor is below visible area
	if cursorYRelative+lineHeight > t.scrollY+content.H {
		t.scrollY = cursorYRelative + lineHeight - content.H
	}

	// Clamp scroll to valid range
	maxScroll := t.getContentHeight(content) - content.H
	if maxScroll < 0 {
		maxScroll = 0
	}
	if t.scrollY < 0 {
		t.scrollY = 0
	}
	if t.scrollY > maxScroll {
		t.scrollY = maxScroll
	}

	// Horizontal auto-scroll only matters when soft-wrap is disabled.
	if !t.wrapEnabled() {
		cursorXRelative := cursorX - content.X
		if cursorXRelative < t.scrollX+4 {
			t.scrollX = cursorXRelative - 4
		}
		if cursorXRelative > t.scrollX+content.W-4 {
			t.scrollX = cursorXRelative - content.W + 4
		}
		if t.scrollX < 0 {
			t.scrollX = 0
		}
		maxScrollX := t.getContentWidth(content) - content.W
		if maxScrollX <= 0 {
			// Keep autosize/fit-content scenarios anchored at x=0 so we
			// never hide leading glyphs before true overflow begins.
			t.scrollX = 0
		} else if t.scrollX > maxScrollX+4 {
			// The text shrank (or the box widened): don't keep scrolling
			// past its end. +4 matches the caret margin above.
			t.scrollX = maxScrollX + 4
		}
	}
}

// layoutOptions is the text layout configuration: the paragraph style and,
// in the box model, the CSS line-height of the field's style.
func (t *TextArea) layoutOptions() TextLayoutOptions {
	opts := t.Paragraph.LayoutOptions(0)
	if t.BoxModel && opts.LineHeightScale == 0 {
		opts.LineHeightScale = t.Style().LineHeight
	}
	return opts
}

func (t *TextArea) lineHeight() float32 {
	opts := t.layoutOptions()
	return BuildTextLayout("M", t.font(), opts).LineHeight
}

func (t *TextArea) wrapEnabled() bool {
	return t.AutoWrap || t.Paragraph.Wrap
}

// layoutFor lays text out for the content width (unbounded when the field
// does not wrap). BuildTextLayout memoizes whole texts and, below that,
// single paragraphs, so an unchanged document is a lookup and an edit
// re-shapes only the paragraph it touched.
func (t *TextArea) layoutFor(text string, contentWidth float32) TextLayout {
	opts := t.layoutOptions()
	opts.MaxLines = 0
	opts.Ellipsis = false
	if t.wrapEnabled() {
		opts.Wrap = true
		opts.MaxWidth = contentWidth
	} else {
		opts.Wrap = false
		opts.MaxWidth = 0
	}
	return BuildTextLayout(text, t.font(), opts)
}

// visualLines returns the document's visual lines at contentWidth. The
// conversion from the shared layout is cached per layout result: the memo
// hands back the same Lines array while nothing changed, so repeated calls
// within a frame (and across frames) do not reallocate it.
func (t *TextArea) visualLines(contentWidth float32) []visualLine {
	layout := t.layoutFor(t.Text, contentWidth)
	return t.layoutCache.linesFor(layout)
}

// getContentWidth is the widest visual line, read from the layout instead of
// re-measuring every line.
func (t *TextArea) getContentWidth(content Rect) float32 {
	if t.Text == "" {
		return 0
	}
	return t.layoutFor(t.Text, content.W).Width
}

func (t *TextArea) logicalLineCount() int {
	return strings.Count(t.Text, "\n") + 1
}

// naturalContentWidth is the longest logical line's width (AutoSizeWidth).
func (t *TextArea) naturalContentWidth() float32 {
	if t.Text == "" {
		return 0
	}
	opts := t.layoutOptions()
	opts.Wrap, opts.MaxWidth, opts.MaxLines, opts.Ellipsis = false, 0, 0, false
	return BuildTextLayout(t.Text, t.font(), opts).Width
}

func (t *TextArea) invalidateLayoutIfAutoSize() {
	if t.AutoSizeWidth || t.AutoSizeHeight {
		t.InvalidateLayout()
	}
}

func (t *TextArea) clampScroll(content Rect) {
	maxScrollY := t.getContentHeight(content) - content.H
	if maxScrollY < 0 {
		maxScrollY = 0
	}
	if t.scrollY < 0 {
		t.scrollY = 0
	}
	if t.scrollY > maxScrollY {
		t.scrollY = maxScrollY
	}
	if !t.wrapEnabled() {
		maxScrollX := t.getContentWidth(content) - content.W
		if maxScrollX < 0 {
			maxScrollX = 0
		}
		if t.scrollX < 0 {
			t.scrollX = 0
		}
		if t.scrollX > maxScrollX {
			t.scrollX = maxScrollX
		}
	} else if t.scrollX != 0 {
		t.scrollX = 0
	}
}

// visibleContentRect returns the text content rectangle and whether each
// scrollbar should be painted in the reserved gutter area.
func (t *TextArea) visibleContentRect(rect Rect) (Rect, bool, bool) {
	base := rect.Inset(t.insets())
	scrollbarSize := t.barSize()
	showX := false
	showY := false
	for i := 0; i < 3; i++ {
		content := base
		if showY {
			content.W -= scrollbarSize
		}
		if showX {
			content.H -= scrollbarSize
		}
		if content.W < 0 {
			content.W = 0
		}
		if content.H < 0 {
			content.H = 0
		}
		needY := t.ShowScrollBarY && t.getContentHeight(content) > content.H+0.5
		needX := t.ShowScrollBarX && !t.wrapEnabled() && t.getContentWidth(content) > content.W+0.5
		if needX == showX && needY == showY {
			return content, showX, showY
		}
		showX = needX
		showY = needY
	}
	content := base
	if showY {
		content.W -= scrollbarSize
	}
	if showX {
		content.H -= scrollbarSize
	}
	if content.W < 0 {
		content.W = 0
	}
	if content.H < 0 {
		content.H = 0
	}
	return content, showX, showY
}

// ----------------------------------------------------------------------
// IMEClient — widget side of IME composition.

// SetPreedit stores in-flight composition text. Drawn at the caret
// with an underline while composing. Call with ("", 0) to clear.
func (t *TextArea) SetPreedit(text string, cursor int) {
	t.ed.setPreedit(t, text, cursor)
	t.Invalidate()
}

// CommitIME finalizes composition by inserting `text` at the caret,
// replacing any active selection, and clearing preedit state.
func (t *TextArea) CommitIME(text string) {
	prev := t.ed.caretState()
	t.ed.commitIME(t, text)
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

// CursorPos returns the caret position as a rune index into the text.
func (t *TextArea) CursorPos() int { return t.ed.cursor }

// SetCursorPos moves the caret to a rune index (clamped to the text)
// and collapses any selection. Fires the cursor-change callback.
func (t *TextArea) SetCursorPos(pos int) {
	prev := t.ed.caretState()
	t.caretNextLine = false
	t.ed.setCursor(t, pos)
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

// SelectRange selects [start, end) (rune indices, clamped) and parks the
// caret at end. start==end collapses to a caret with no selection.
func (t *TextArea) SelectRange(start, end int) {
	prev := t.ed.caretState()
	t.caretNextLine = false
	t.ed.selectRange(t, start, end)
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

// InsertText inserts text at the caret, replacing any active selection,
// and fires OnChange. Undoable as a single step. This is the
// programmatic edit path used by autocomplete and point-mode reference
// insertion (CommitIME is the same operation, tagged as IME input).
func (t *TextArea) InsertText(text string) {
	prev := t.ed.caretState()
	t.ed.insert(t, "insert", text)
	t.ed.emitStateChange(t, prev)
	t.Invalidate()
}

// CaretRect returns the current caret rectangle in window coordinates.
// Offsets by the preedit cursor position when composing so the OS
// candidate window anchors to the end of the in-flight text.
func (t *TextArea) CaretRect() Rect {
	content, _, _ := t.visibleContentRect(t.Bounds())
	cx, cy := t.getCursorPosition(content)
	if !t.wrapEnabled() {
		cx -= t.scrollX
	}
	if t.ed.preedit != "" {
		runes := []rune(t.ed.preedit)
		cur := t.ed.preeditCursor
		if cur > len(runes) {
			cur = len(runes)
		}
		cx += TextXForOffset(t.ed.preedit, t.font(), TextDirectionAuto, cur)
	}
	return Rect{X: cx, Y: cy - t.scrollY, W: 1, H: t.lineHeight()}
}

// Tick drives cursor blink (see textEditor.tick). The dirty region is the
// whole field: the caret rect is expensive to compute outside Draw, and a
// partial repaint of one field is cheap.
func (t *TextArea) Tick(now time.Time) Rect { return t.ed.tick(t, t.focused, now) }

// ----------------------------------------------------------------------
// Undo / redo

// ----------------------------------------------------------------------
// textEditorHost: the multi-line answers to the shared editor's questions.

func (t *TextArea) editorText() *string { return &t.Text }

func (t *TextArea) editorHooks() editorHooks {
	return editorHooks{
		beforeInput: t.BeforeInput,
		onChange:    t.OnChange,
		onCommit:    t.OnCommit,
		onCursor:    t.OnCursorChange,
		onSelection: t.OnSelectionChange,
	}
}

func (t *TextArea) editorMultiline() bool { return true }
func (t *TextArea) editorSecret() bool    { return false }

// editorHorizontal steps one glyph along the visual line, crossing to the
// neighbouring line at its end. At a soft-wrap boundary the offset does not
// change (end of line N == start of N+1), so the step only flips the caret's
// line affinity.
func (t *TextArea) editorHorizontal(dir int) int {
	target := t.visualHorizontalTarget(dir)
	if target == t.ed.cursor {
		t.caretNextLine = dir > 0
	}
	return target
}

func (t *TextArea) editorTextChanged() {
	t.caretNextLine = false
	t.invalidateLayoutIfAutoSize()
}

func (t *TextArea) editorRevealCaret() { t.scrollToCursor() }

// textRunes returns Text as runes, converted once per document change.
// Callers must not modify the slice.
func (t *TextArea) textRunes() []rune {
	c := &t.layoutCache
	// Comparing an unchanged string to itself is O(1) (same data pointer).
	if c.runes == nil || c.runesText != t.Text {
		c.runesText = t.Text
		c.runes = []rune(t.Text)
	}
	return c.runes
}

// textAreaLayoutCache holds data derived from the shared text layout.
type textAreaLayoutCache struct {
	runesText string
	runes     []rune
	// Two slots: a wrapping field is laid out at two widths while it decides
	// whether the vertical scrollbar takes a gutter.
	lines [2]cachedVisualLines
	next  int
}

type cachedVisualLines struct {
	src   *TextLayoutLine // identity of the layout's Lines array
	n     int
	lines []visualLine
}

func (c *textAreaLayoutCache) linesFor(layout TextLayout) []visualLine {
	if len(layout.Lines) == 0 {
		return nil
	}
	src := &layout.Lines[0]
	for i := range c.lines {
		if e := &c.lines[i]; e.src == src && e.n == len(layout.Lines) {
			return e.lines
		}
	}
	lines := make([]visualLine, 0, len(layout.Lines))
	for _, line := range layout.Lines {
		lines = append(lines, visualLine{start: line.Start, end: line.End, offset: line.Offset})
	}
	c.lines[c.next] = cachedVisualLines{src: src, n: len(layout.Lines), lines: lines}
	c.next = (c.next + 1) % len(c.lines)
	return lines
}
