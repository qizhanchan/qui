package widgets

import (
	"strings"
	"time"

	. "github.com/qizhanchan/qui"
)

// TextArea provides multiline text input.
type TextArea struct {
	BaseWidget
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
	cursorPos         int // cursor position in runes
	selStart          int // selection start (-1 if no selection)
	selEnd            int // selection end
	focused           bool
	hovering          bool
	selecting         bool
	scrollX           float32 // horizontal scroll offset in pixels (AutoWrap=false only)
	scrollY           float32 // vertical scroll offset in pixels
	barDraggingX      bool
	barDraggingY      bool
	barDragStartX     float32
	barDragStartY     float32
	barDragScrollX    float32
	barDragScrollY    float32
	cursorBlinkTime   int64 // timestamp for cursor blink animation (milliseconds)
	cursorVisible     bool  // current cursor visibility state
	OnChange          func(text string)

	// IME composition state. preeditText is the in-flight
	// composed string; preeditCursor is the rune position inside it.
	// Rendered near the caret with an underline while composing.
	preeditText   string
	preeditCursor int

	// Undo / redo history — same coalescing model as Input.
	history undoHistory

	// lastChangeUser: see Input.lastChangeUser — drives the
	// controlled-value overwrite diagnostic.
	lastChangeUser bool

	clickCount  int
	lastClickMS int64
	lastClickX  float32
	lastClickY  float32
}

type visualLine struct {
	start int
	end   int
}

func NewTextArea(placeholder string) *TextArea {
	t := &TextArea{
		BaseWidget:      NewBaseWidget(),
		Placeholder:     placeholder,
		AutoWrap:        false,
		Paragraph:       DefaultParagraphStyle(),
		ShowScrollBarX:  true,
		ShowScrollBarY:  true,
		MinWidth:        htmlInputWidth,
		MinHeight:       40,
		cursorPos:       0,
		selStart:        -1,
		cursorVisible:   true,
		cursorBlinkTime: 0,
	}
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
		width = max(width, t.naturalContentWidth()+t.Style().Padding.Horizontal())
	}
	if t.AutoSizeHeight {
		height = max(height, float32(t.logicalLineCount())*t.lineHeight()+t.Style().Padding.Vertical())
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

func (t *TextArea) Draw(canvas Canvas) {
	rect := t.Bounds()

	// Draw background
	bg := t.Style().Background
	if t.focused {
		bg = Color{R: min(bg.R+0.05, 1), G: min(bg.G+0.05, 1), B: min(bg.B+0.05, 1), A: bg.A}
	}
	if t.Style().Radius > 0 {
		canvas.FillRoundedRect(rect, t.Style().Radius, bg)
	} else {
		canvas.FillRect(rect, bg)
	}

	// Draw border
	borderColor := t.Style().Border
	if t.focused {
		borderColor = Color{R: 0.4, G: 0.6, B: 1.0, A: 1}
	}
	if t.Style().BorderSize > 0 {
		canvas.StrokeRect(rect, borderColor, t.Style().BorderSize)
	}

	content, showScrollX, showScrollY := t.visibleContentRect(rect)
	contentID := canvas.Save()
	canvas.ClipRect(content)

	// Draw text or placeholder
	displayText := t.Text
	textColor := t.Style().Foreground
	if displayText == "" && !t.focused {
		displayText = t.DisplayPlaceholder()
		textColor = Color{R: 0.5, G: 0.5, B: 0.5, A: 1}
	}

	// While composing, INSERT the in-flight preedit into the displayed text
	// at the caret so it flows inline (pushing following text right and
	// rewrapping) instead of being painted on top of the following
	// characters. The caret/underline are positioned from the pre-insertion
	// cursor (getCursorPosition reads t.Text), which is exactly where the
	// preedit begins.
	composing := t.preeditText != "" && t.focused
	if composing {
		runes := []rune(t.Text)
		cp := t.cursorPos
		if cp > len(runes) {
			cp = len(runes)
		}
		displayText = string(runes[:cp]) + t.preeditText + string(runes[cp:])
		textColor = t.Style().Foreground
	}

	// Draw selection beneath text
	if t.Text != "" && t.selStart >= 0 && t.selStart != t.selEnd {
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
		preeditW, _ := TextMetrics(t.preeditText, t.font())
		canvas.FillRect(
			Rect{X: cursorX, Y: cursorY + t.lineHeight() - 2, W: preeditW, H: 1},
			textColor)
	}

	// Draw cursor whenever focused. Blink toggling happens in Tick, so
	// this path only reads cursorVisible without side effects. While
	// composing, ignore blink so the IME anchor stays visible.
	if t.focused {
		if t.cursorVisible || t.preeditText != "" {
			cursorX, cursorY := t.getCursorPosition(content)
			if !t.wrapEnabled() {
				cursorX -= t.scrollX
			}
			cursorY -= t.scrollY
			cursorY = float32(int(cursorY))
			cursorHeight := t.lineHeight()

			// While composing, shift cursor past the preedit cursor offset.
			if t.preeditText != "" {
				runes := []rune(t.preeditText)
				cur := t.preeditCursor
				if cur > len(runes) {
					cur = len(runes)
				}
				cursorX += TextXForOffset(t.preeditText, t.font(), TextDirectionAuto, cur)
			}

			// Only draw cursor if visible in content area
			if cursorY >= content.Y && cursorY+cursorHeight <= content.Y+content.H {
				canvas.FillRect(
					Rect{X: cursorX, Y: cursorY, W: 1, H: cursorHeight},
					t.Style().Foreground,
				)
			}
		}
	}

	canvas.RestoreTo(contentID)

	// Draw scrollbars if configured and content overflows.
	t.drawScrollbars(canvas, content, showScrollX, showScrollY)
}

func (t *TextArea) getCursorPosition(content Rect) (x, y float32) {
	runes := []rune(t.Text)
	lineHeight := t.lineHeight()
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return content.X, content.Y
	}
	if t.cursorPos < 0 {
		t.cursorPos = 0
	}

	x = content.X
	y = content.Y

	lineIdx := 0
	for i, line := range lines {
		if t.cursorPos >= line.start && t.cursorPos <= line.end {
			lineIdx = i
			break
		}
		if i == len(lines)-1 {
			lineIdx = i
		}
	}

	line := lines[lineIdx]
	y += float32(lineIdx) * lineHeight
	end := t.cursorPos
	if end < line.start {
		end = line.start
	}
	if end > line.end {
		end = line.end
	}
	lineText := string(runes[line.start:line.end])
	x = content.X + TextXForOffset(lineText, t.font(), t.Paragraph.Direction, end-line.start)
	return x, y
}

func (t *TextArea) drawVisibleText(canvas Canvas, text string, content Rect, color Color) {
	opts := t.Paragraph.LayoutOptions(0)
	opts.MaxLines = 0
	opts.Ellipsis = false
	if t.wrapEnabled() {
		opts.Wrap = true
		opts.MaxWidth = content.W
	} else {
		opts.Wrap = false
		opts.MaxWidth = 0
	}
	layout := BuildTextLayout(text, t.font(), opts)
	drawRect := Rect{X: content.X, Y: content.Y - t.scrollY, W: content.W, H: layout.Height}
	if !t.wrapEnabled() {
		drawRect.X -= t.scrollX
	}
	DrawTextLayout(canvas, layout, drawRect, color, t.font())
}

func (t *TextArea) drawSelection(canvas Canvas, content Rect) {
	if t.selStart < 0 || t.selStart == t.selEnd {
		return
	}

	start := t.selStart
	end := t.selEnd
	if start > end {
		start, end = end, start
	}

	runes := []rune(t.Text)
	lineHeight := t.lineHeight()
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return
	}

	selColor := SelectionHighlight(t)

	for i, line := range lines {
		y := content.Y - t.scrollY + float32(i)*lineHeight
		if y+lineHeight <= content.Y || y >= content.Y+content.H {
			continue
		}
		lineSelStart := start
		if lineSelStart < line.start {
			lineSelStart = line.start
		}
		lineSelEnd := end
		if lineSelEnd > line.end {
			lineSelEnd = line.end
		}
		if lineSelStart >= lineSelEnd {
			continue
		}
		lineText := string(runes[line.start:line.end])
		for _, segment := range TextSelectionSegments(lineText, t.font(), t.Paragraph.Direction,
			lineSelStart-line.start, lineSelEnd-line.start) {
			startX := content.X + segment.X
			if !t.wrapEnabled() {
				startX -= t.scrollX
			}
			canvas.FillRect(
				Rect{X: startX, Y: y, W: segment.Width, H: lineHeight},
				selColor,
			)
		}
	}
}

func (t *TextArea) drawScrollbars(canvas Canvas, content Rect, showX, showY bool) {
	if !showX && !showY {
		return
	}
	trackColor := Color{R: 0.15, G: 0.15, B: 0.15, A: 1}
	thumbColor := Color{R: 0.5, G: 0.5, B: 0.5, A: 1}

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

func (t *TextArea) verticalBarTrack(content Rect) Rect {
	return Rect{X: content.X + content.W, Y: content.Y, W: 8, H: content.H}
}

func (t *TextArea) horizontalBarTrack(content Rect) Rect {
	return Rect{X: content.X, Y: content.Y + content.H, W: content.W, H: 8}
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
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	prevText, prevScrollX, prevScrollY := t.Text, t.scrollX, t.scrollY
	prevPreedit, prevPreeditCursor := t.preeditText, t.preeditCursor
	prevSelecting := t.selecting
	prevDragX, prevDragY := t.barDraggingX, t.barDraggingY
	prevCursorVisible := t.cursorVisible
	defer func() {
		t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
		if t.Text != prevText ||
			t.cursorPos != prevCursor ||
			t.selStart != prevSelStart ||
			t.selEnd != prevSelEnd ||
			t.scrollX != prevScrollX ||
			t.scrollY != prevScrollY ||
			t.preeditText != prevPreedit ||
			t.preeditCursor != prevPreeditCursor ||
			t.selecting != prevSelecting ||
			t.barDraggingX != prevDragX ||
			t.barDraggingY != prevDragY ||
			t.cursorVisible != prevCursorVisible {
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
			if t.selecting {
				t.handleMouseDrag(e)
				return true
			}
		case EventMouseUp:
			if t.barDraggingX || t.barDraggingY {
				t.barDraggingX = false
				t.barDraggingY = false
				return true
			}
			if t.selecting {
				t.handleMouseUp()
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
			if t.preeditText != "" {
				return true
			}
			return t.handleKeyDown(e)
		}
	case CharEvent:
		if t.focused {
			return t.handleChar(e)
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
	pos := t.positionFromPoint(Point{X: e.X, Y: e.Y})
	switch t.registerClick(e) {
	case 2:
		a, b := t.wordBoundsAt(pos)
		t.selStart = a
		t.selEnd = b
		t.cursorPos = b
	case 3:
		a, b := t.logicalLineBoundsAt(pos)
		t.selStart = a
		t.selEnd = b
		t.cursorPos = b
	default:
		t.cursorPos = pos
		t.selStart = pos
		t.selEnd = pos
	}
	t.selecting = true
	t.hovering = true
	t.history.breakCoalesce()

	// Reset cursor blink on click
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
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
	pos := t.positionFromPoint(Point{X: e.X, Y: e.Y})
	t.cursorPos = pos
	if t.selStart < 0 {
		t.selStart = pos
	}
	t.selEnd = pos
	t.scrollToCursor()
}

func (t *TextArea) handleMouseUp() {
	t.selecting = false
	if t.selStart == t.selEnd {
		t.selStart = -1
	}
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

func (t *TextArea) positionFromPoint(p Point) int {
	content, _, _ := t.visibleContentRect(t.Bounds())
	runes := []rune(t.Text)
	lineHeight := t.lineHeight()
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return 0
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
	return t.closestPosInLine(runes, line.start, line.end, clickX)
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

func (t *TextArea) handleKeyDown(e KeyEvent) bool {
	runes := []rune(t.Text)
	shift := e.Mods&ModShift != 0

	// Reset cursor blink on any key press
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()

	if IsCommandMod(e.Mods) {
		if e.Key == KeyZ {
			if e.Mods&ModShift != 0 {
				t.redo()
			} else {
				t.undo()
			}
			return true
		}
		if e.Key == KeyY {
			t.redo()
			return true
		}
		if handled := t.handleClipboardShortcut(e.Key, runes); handled {
			return true
		}
	}

	switch e.Key {
	case KeyBackspace:
		if !t.beforeInput("delete", "") {
			return true
		}
		before := t.snapshot()
		if t.selStart >= 0 && t.selStart != t.selEnd {
			// Delete selection
			start := int(min(float32(t.selStart), float32(t.selEnd)))
			end := int(max(float32(t.selStart), float32(t.selEnd)))
			t.Text = string(runes[:start]) + string(runes[end:])
			t.cursorPos = start
			t.selStart = -1
		} else if t.cursorPos > 0 {
			// Delete one grapheme cluster — backspacing 👌🏻 removes
			// the whole emoji in one keystroke instead of leaving a
			// stranded base after stripping the skin-tone modifier.
			target := PrevClusterBoundary(runes, t.cursorPos)
			t.Text = string(runes[:target]) + string(runes[t.cursorPos:])
			t.cursorPos = target
		}
		t.pushUndo("delete", before)
		t.invalidateLayoutIfAutoSize()
		if t.OnChange != nil {
			t.fireChange()
		}
		t.scrollToCursor()
		return true

	case KeyLeft:
		t.moveCursorWithSelection(t.visualHorizontalTarget(-1), shift)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true

	case KeyRight:
		t.moveCursorWithSelection(t.visualHorizontalTarget(1), shift)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true

	case KeyUp:
		t.moveCursorVertically(-1)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true

	case KeyDown:
		t.moveCursorVertically(1)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true
	case KeyHome:
		lineStart, _ := t.currentLineBounds()
		t.moveCursorWithSelection(lineStart, shift)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true
	case KeyEnd:
		_, lineEnd := t.currentLineBounds()
		t.moveCursorWithSelection(lineEnd, shift)
		t.history.breakCoalesce()
		t.scrollToCursor()
		return true

	case KeyEnter:
		if !t.EnterWouldInsertNewline(e) {
			return false
		}
		if !t.beforeInput("enter", "\n") {
			return true
		}
		before := t.snapshot()
		if t.selStart >= 0 && t.selStart != t.selEnd {
			start := int(min(float32(t.selStart), float32(t.selEnd)))
			end := int(max(float32(t.selStart), float32(t.selEnd)))
			t.Text = string(runes[:start]) + "\n" + string(runes[end:])
			t.cursorPos = start + 1
			t.selStart = -1
		} else {
			t.Text = string(runes[:t.cursorPos]) + "\n" + string(runes[t.cursorPos:])
			t.cursorPos++
		}
		t.pushUndo("enter", before)
		// Newline splits coalescing groups — next typing starts fresh.
		t.history.breakCoalesce()
		t.invalidateLayoutIfAutoSize()
		if t.OnChange != nil {
			t.fireChange()
		}
		t.scrollToCursor()
		return true
	}

	return false
}

// handleClipboardShortcut processes Cmd/Ctrl + A/C/V/X. `runes` is the
// caller's already-computed rune slice of Text (handleKeyDown takes it
// once and reuses it across cases). Returns true if the key matched a
// clipboard shortcut, so the outer switch doesn't fire its default
// binding for KeyA etc.
func (t *TextArea) handleClipboardShortcut(k Key, runes []rune) bool {
	switch k {
	case KeyA:
		if len(runes) == 0 {
			return true
		}
		t.selStart = 0
		t.selEnd = len(runes)
		t.cursorPos = len(runes)
		t.scrollToCursor()
		return true
	case KeyC:
		if t.selStart >= 0 && t.selStart != t.selEnd {
			start := t.selStart
			end := t.selEnd
			if start > end {
				start, end = end, start
			}
			SetClipboardText(string(runes[start:end]))
		}
		return true
	case KeyX:
		if t.selStart >= 0 && t.selStart != t.selEnd {
			if !t.beforeInput("cut", "") {
				return true
			}
			start := t.selStart
			end := t.selEnd
			if start > end {
				start, end = end, start
			}
			SetClipboardText(string(runes[start:end]))
			before := t.snapshot()
			t.Text = string(runes[:start]) + string(runes[end:])
			t.cursorPos = start
			t.selStart = -1
			t.pushUndo("cut", before)
			t.invalidateLayoutIfAutoSize()
			if t.OnChange != nil {
				t.fireChange()
			}
			t.scrollToCursor()
		}
		return true
	case KeyV:
		text := GetClipboardText()
		if text == "" {
			return true
		}
		if !t.beforeInput("paste", text) {
			return true
		}
		insert := []rune(text)
		before := t.snapshot()
		if t.selStart >= 0 && t.selStart != t.selEnd {
			start := t.selStart
			end := t.selEnd
			if start > end {
				start, end = end, start
			}
			t.Text = string(runes[:start]) + text + string(runes[end:])
			t.cursorPos = start + len(insert)
			t.selStart = -1
		} else {
			t.Text = string(runes[:t.cursorPos]) + text + string(runes[t.cursorPos:])
			t.cursorPos += len(insert)
		}
		t.pushUndo("paste", before)
		t.invalidateLayoutIfAutoSize()
		if t.OnChange != nil {
			t.fireChange()
		}
		t.scrollToCursor()
		return true
	}
	return false
}

func (t *TextArea) handleChar(e CharEvent) bool {
	// Ignore control characters
	if e.Rune < 32 && e.Rune != '\n' {
		return false
	}
	if !t.beforeInput("type", string(e.Rune)) {
		return true
	}
	// IME commit: the only time a CharEvent reaches us with non-empty
	// preedit is right after the OS input method committed. The OS
	// may or may not fire unmarkText; clearing preedit here ensures
	// the committed characters aren't visually doubled by a stale
	// preedit being drawn on top.
	if t.preeditText != "" {
		t.preeditText = ""
		t.preeditCursor = 0
	}

	runes := []rune(t.Text)

	before := t.snapshot()
	if t.selStart >= 0 && t.selStart != t.selEnd {
		// Replace selection
		start := int(min(float32(t.selStart), float32(t.selEnd)))
		end := int(max(float32(t.selStart), float32(t.selEnd)))
		t.Text = string(runes[:start]) + string(e.Rune) + string(runes[end:])
		t.cursorPos = start + 1
		t.selStart = -1
	} else {
		t.Text = string(runes[:t.cursorPos]) + string(e.Rune) + string(runes[t.cursorPos:])
		t.cursorPos++
	}
	t.pushUndo("type", before)
	// Word-boundary typing breaks the coalescing group so undo
	// unwinds one word at a time, matching macOS/Cocoa conventions.
	if e.Rune == ' ' || e.Rune == '\t' {
		t.history.breakCoalesce()
	}

	t.invalidateLayoutIfAutoSize()
	if t.OnChange != nil {
		t.fireChange()
	}

	// Reset cursor blink when typing
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()

	// Auto-scroll to cursor
	t.scrollToCursor()

	return true
}

func (t *TextArea) handleScroll(e MouseEvent) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	// Scroll speed: 20 logical pixels per wheel unit.
	t.scrollY -= e.DeltaY * 20.0
	if !t.wrapEnabled() {
		hStep := e.DeltaX
		if e.Mods&ModShift != 0 && hStep == 0 {
			hStep = e.DeltaY
		}
		t.scrollX -= hStep * 20.0
	}
	t.clampScroll(content)
}

func (t *TextArea) moveCursorVertically(direction int) {
	runes := []rune(t.Text)
	content, _, _ := t.visibleContentRect(t.Bounds())
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return
	}

	// Find current visual line and x position
	lineIdx := 0
	for i, line := range lines {
		if t.cursorPos >= line.start && t.cursorPos <= line.end {
			lineIdx = i
			break
		}
		if i == len(lines)-1 {
			lineIdx = i
		}
	}
	currentLine := lines[lineIdx]
	end := t.cursorPos
	if end > currentLine.end {
		end = currentLine.end
	}
	currentText := string(runes[currentLine.start:currentLine.end])
	targetX := TextXForOffset(currentText, t.font(), t.Paragraph.Direction, end-currentLine.start)

	targetIdx := lineIdx + direction
	if targetIdx < 0 {
		targetIdx = 0
	}
	if targetIdx >= len(lines) {
		targetIdx = len(lines) - 1
	}
	targetLine := lines[targetIdx]
	t.cursorPos = t.closestPosInLine(runes, targetLine.start, targetLine.end, targetX)
	t.selStart = -1
}

func (t *TextArea) visualHorizontalTarget(delta int) int {
	runes := []rune(t.Text)
	content, _, _ := t.visibleContentRect(t.Bounds())
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return 0
	}
	lineIndex := len(lines) - 1
	for i, line := range lines {
		if t.cursorPos >= line.start && t.cursorPos <= line.end {
			lineIndex = i
			break
		}
	}
	line := lines[lineIndex]
	text := string(runes[line.start:line.end])
	local := t.cursorPos - line.start
	target := line.start + TextVisualNeighbor(text, t.font(), t.Paragraph.Direction, local, delta)
	if target != t.cursorPos {
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

func (t *TextArea) moveCursorWithSelection(pos int, extend bool) {
	if !extend {
		t.cursorPos = pos
		t.selStart = -1
		t.selEnd = 0
		return
	}
	if t.selStart < 0 {
		t.selStart = t.cursorPos
	}
	t.cursorPos = pos
	t.selEnd = pos
}

func (t *TextArea) currentLineBounds() (start, end int) {
	content, _, _ := t.visibleContentRect(t.Bounds())
	runes := []rune(t.Text)
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return 0, 0
	}
	for _, line := range lines {
		if t.cursorPos >= line.start && t.cursorPos <= line.end {
			return line.start, line.end
		}
	}
	last := lines[len(lines)-1]
	return last.start, last.end
}

func (t *TextArea) beforeInput(kind, text string) bool {
	if t.BeforeInput == nil {
		return true
	}
	return t.BeforeInput(BeforeInputEvent{
		Kind:      kind,
		Text:      text,
		CursorPos: t.cursorPos,
		SelStart:  t.selStart,
		SelEnd:    t.selEnd,
		Value:     t.Text,
	})
}

func (t *TextArea) emitStateChange(prevCursor, prevSelStart, prevSelEnd int) {
	if t.cursorPos != prevCursor && t.OnCursorChange != nil {
		t.OnCursorChange(t.cursorPos)
	}
	if (t.selStart != prevSelStart || t.selEnd != prevSelEnd) && t.OnSelectionChange != nil {
		t.OnSelectionChange(t.selStart, t.selEnd)
	}
}

func (t *TextArea) registerClick(e MouseEvent) int {
	now := e.When.UnixMilli()
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	if now-t.lastClickMS <= 400 &&
		absf(e.X-t.lastClickX) <= 4 &&
		absf(e.Y-t.lastClickY) <= 4 {
		t.clickCount++
	} else {
		t.clickCount = 1
	}
	if t.clickCount > 3 {
		t.clickCount = 1
	}
	t.lastClickMS = now
	t.lastClickX = e.X
	t.lastClickY = e.Y
	return t.clickCount
}

func (t *TextArea) wordBoundsAt(pos int) (int, int) {
	runes := []rune(t.Text)
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

func (t *TextArea) logicalLineBoundsAt(pos int) (int, int) {
	runes := []rune(t.Text)
	if len(runes) == 0 {
		return 0, 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	if pos < 0 {
		pos = 0
	}
	start := pos
	if start == len(runes) && start > 0 {
		start--
	}
	for start > 0 && runes[start-1] != '\n' {
		start--
	}
	end := pos
	if end < start {
		end = start
	}
	for end < len(runes) && runes[end] != '\n' {
		end++
	}
	return start, end
}

func (t *TextArea) HitTest(p Point) Widget {
	if t.Bounds().Contains(p) {
		return t
	}
	return nil
}

// WidgetCursor shows the text I-beam over an editable area; a disabled area
// declines (see Input.WidgetCursor and qui/cursor.go).
func (t *TextArea) WidgetCursor() (CursorShape, bool) {
	return CursorText, t.Enabled()
}

func (t *TextArea) Focusable() bool {
	return true
}

func (t *TextArea) CancelInteraction() {
	t.selecting = false
	t.barDraggingX = false
	t.barDraggingY = false
}

func (t *TextArea) SetFocused(focused bool) {
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	t.focused = focused
	// Focus transitions seal the current coalescing group so a
	// later re-focus + typing starts a new undo entry.
	t.history.breakCoalesce()
	if focused {
		// Reset cursor blink when gaining focus
		t.cursorVisible = true
		t.cursorBlinkTime = time.Now().UnixMilli()
	} else {
		t.selStart = -1
		t.hovering = false
		t.barDraggingX = false
		t.barDraggingY = false
	}
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

func (t *TextArea) GetText() string {
	return t.Text
}

// fireChange invokes OnChange (if wired) and publishes a semantic "Input"
// record for the debug surface. Single choke point for every text
// mutation.
// NotifyTextChanged implements qui.TextChangeNotifier: publish the current
// text as a user-equivalent change. SetText itself stays silent (a
// programmatic set must not re-enter the app's handler), so the agent's Type
// action calls this after its bulk set to make the two paths behave alike.
func (t *TextArea) NotifyTextChanged() { t.fireChange() }

func (t *TextArea) fireChange() {
	t.lastChangeUser = true
	if t.OnChange != nil {
		t.OnChange(t.Text)
	}
	if w := t.Window(); w != nil {
		w.RecordInputChange(t, "", t.Text, "user")
	}
}

// TextState reports the textarea's editing state for the AX / agent debug
// surface (qui.TextEditable).
func (t *TextArea) TextState() TextState {
	return TextState{
		Value:    t.Text,
		Caret:    t.cursorPos,
		SelStart: t.selStart,
		SelEnd:   t.selEnd,
		Preedit:  t.preeditText,
		CanUndo:  len(t.history.undo) > 0,
		CanRedo:  len(t.history.redo) > 0,
	}
}

func (t *TextArea) SetText(text string) {
	if t.Text == text {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	before := t.snapshot()
	t.Text = text
	runes := []rune(text)
	if t.cursorPos > len(runes) {
		t.cursorPos = len(runes)
	}
	t.scrollX = 0
	t.scrollY = 0
	t.selStart = -1
	t.pushUndo("set", before)
	source := "program"
	if t.lastChangeUser {
		source = "program-overwrite"
	}
	t.lastChangeUser = false
	if w := t.Window(); w != nil {
		w.RecordInputChange(t, before.text, text, source)
	}
	t.invalidateLayoutIfAutoSize()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// MoveCursorToEnd positions the caret after the last rune and clears
// any selection. Useful after programmatically seeding text (e.g. an
// inline cell editor preloading the existing cell value) so subsequent
// keystrokes append instead of inserting at the start.
func (t *TextArea) MoveCursorToEnd() {
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	t.cursorPos = len([]rune(t.Text))
	t.selStart = -1
	t.selEnd = 0
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
}

// getContentHeight calculates the total height of the text content
func (t *TextArea) getContentHeight(content Rect) float32 {
	runes := []rune(t.Text)
	lineHeight := t.lineHeight()
	lines := t.visualLines(runes, content.W)
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
		}
	}
}

func (t *TextArea) lineHeight() float32 {
	opts := t.Paragraph.LayoutOptions(0)
	return BuildTextLayout("M", t.font(), opts).LineHeight
}

func (t *TextArea) wrapEnabled() bool {
	return t.AutoWrap || t.Paragraph.Wrap
}

func (t *TextArea) visualLines(runes []rune, contentWidth float32) []visualLine {
	opts := t.Paragraph.LayoutOptions(0)
	opts.MaxLines = 0
	opts.Ellipsis = false
	if t.wrapEnabled() {
		opts.Wrap = true
		opts.MaxWidth = contentWidth
	} else {
		opts.Wrap = false
		opts.MaxWidth = 0
	}
	layout := BuildTextLayout(string(runes), t.font(), opts)
	lines := make([]visualLine, 0, len(layout.Lines))
	for _, line := range layout.Lines {
		lines = append(lines, visualLine{start: line.Start, end: line.End})
	}
	return lines
}

func (t *TextArea) getContentWidth(content Rect) float32 {
	runes := []rune(t.Text)
	lines := t.visualLines(runes, content.W)
	if len(lines) == 0 {
		return 0
	}
	maxW := float32(0)
	for _, line := range lines {
		w := t.runesWidth(runes[line.start:line.end])
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

func (t *TextArea) logicalLineCount() int {
	if t.Text == "" {
		return 1
	}
	return len(strings.Split(strings.ReplaceAll(t.Text, "\r\n", "\n"), "\n"))
}

func (t *TextArea) naturalContentWidth() float32 {
	lines := strings.Split(strings.ReplaceAll(t.Text, "\r\n", "\n"), "\n")
	maxW := float32(0)
	for _, line := range lines {
		w := t.runesWidth([]rune(line))
		if w > maxW {
			maxW = w
		}
	}
	return maxW
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
	base := rect.Inset(t.Style().Padding)
	scrollbarSize := float32(8)
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
	t.preeditText = text
	t.preeditCursor = cursor
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
	t.Invalidate()
}

// CommitIME finalizes composition by inserting `text` at the caret,
// replacing any active selection, and clearing preedit state.
func (t *TextArea) CommitIME(text string) {
	t.preeditText = ""
	t.preeditCursor = 0
	if text == "" {
		return
	}
	if !t.beforeInput("ime", text) {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	runes := []rune(t.Text)
	before := t.snapshot()
	if t.selStart >= 0 && t.selStart != t.selEnd {
		start := int(min(float32(t.selStart), float32(t.selEnd)))
		end := int(max(float32(t.selStart), float32(t.selEnd)))
		t.Text = string(runes[:start]) + text + string(runes[end:])
		t.cursorPos = start + len([]rune(text))
		t.selStart = -1
	} else {
		t.Text = string(runes[:t.cursorPos]) + text + string(runes[t.cursorPos:])
		t.cursorPos += len([]rune(text))
	}
	t.pushUndo("ime", before)
	t.invalidateLayoutIfAutoSize()
	if t.OnChange != nil {
		t.fireChange()
	}
	t.scrollToCursor()
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// CursorPos returns the caret position as a rune index into the text.
func (t *TextArea) CursorPos() int { return t.cursorPos }

// SetCursorPos moves the caret to a rune index (clamped to the text)
// and collapses any selection. Fires the cursor-change callback.
func (t *TextArea) SetCursorPos(pos int) {
	n := len([]rune(t.Text))
	if pos < 0 {
		pos = 0
	}
	if pos > n {
		pos = n
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	t.cursorPos = pos
	t.selStart = -1
	t.selEnd = pos
	t.scrollToCursor()
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// SelectRange selects [start, end) (rune indices, clamped) and parks the
// caret at end. start==end collapses to a caret with no selection.
func (t *TextArea) SelectRange(start, end int) {
	n := len([]rune(t.Text))
	clamp := func(v int) int {
		if v < 0 {
			return 0
		}
		if v > n {
			return n
		}
		return v
	}
	start, end = clamp(start), clamp(end)
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	if start == end {
		t.selStart = -1
	} else {
		t.selStart = start
	}
	t.selEnd = end
	t.cursorPos = end
	t.scrollToCursor()
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// InsertText inserts text at the caret, replacing any active selection,
// and fires OnChange. Undoable as a single step. This is the
// programmatic edit path used by autocomplete and point-mode reference
// insertion (CommitIME is the same operation, tagged as IME input).
func (t *TextArea) InsertText(text string) {
	if text == "" {
		return
	}
	if !t.beforeInput("insert", text) {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	runes := []rune(t.Text)
	before := t.snapshot()
	if t.selStart >= 0 && t.selStart != t.selEnd {
		start := t.selStart
		end := t.selEnd
		if start > end {
			start, end = end, start
		}
		t.Text = string(runes[:start]) + text + string(runes[end:])
		t.cursorPos = start + len([]rune(text))
	} else {
		t.Text = string(runes[:t.cursorPos]) + text + string(runes[t.cursorPos:])
		t.cursorPos += len([]rune(text))
	}
	t.selStart = -1
	t.selEnd = t.cursorPos
	t.pushUndo("insert", before)
	t.invalidateLayoutIfAutoSize()
	if t.OnChange != nil {
		t.fireChange()
	}
	t.scrollToCursor()
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
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
	if t.preeditText != "" {
		runes := []rune(t.preeditText)
		cur := t.preeditCursor
		if cur > len(runes) {
			cur = len(runes)
		}
		cx += TextXForOffset(t.preeditText, t.font(), TextDirectionAuto, cur)
	}
	return Rect{X: cx, Y: cy - t.scrollY, W: 1, H: t.lineHeight()}
}

// Tick advances cursor-blink state. Returns the widget's bounds as the
// dirty region when visibility toggles; an empty Rect otherwise.
//
// Returning the full widget bounds (vs. just the cursor stripe) is
// imprecise but correct — the cursor rect is expensive to compute outside
// Draw. It's cheap: a ~1KB scissor upload and a partial buffer re-paint.
func (t *TextArea) Tick(now time.Time) Rect {
	if !t.focused || !t.hovering {
		return Rect{}
	}
	currentTime := now.UnixMilli()
	if currentTime-t.cursorBlinkTime > 500 {
		t.cursorVisible = !t.cursorVisible
		t.cursorBlinkTime = currentTime
		return t.Bounds()
	}
	return Rect{}
}

// ----------------------------------------------------------------------
// Undo / redo

func (t *TextArea) snapshot() undoEntry {
	return undoEntry{
		text:      t.Text,
		cursorPos: t.cursorPos,
		selStart:  t.selStart,
		selEnd:    t.selEnd,
	}
}

func (t *TextArea) applyUndoEntry(e undoEntry) {
	t.Text = e.text
	t.cursorPos = e.cursorPos
	t.selStart = e.selStart
	t.selEnd = e.selEnd
	// Clear IME preedit on restore — a stale preedit pinned to the
	// old caret would mis-render after the text swap.
	t.preeditText = ""
	t.preeditCursor = 0
	t.invalidateLayoutIfAutoSize()
	t.scrollToCursor()
	t.cursorVisible = true
	t.cursorBlinkTime = time.Now().UnixMilli()
}

func (t *TextArea) pushUndo(kind string, before undoEntry) {
	t.history.push(kind, before, t.snapshot())
}

// undo restores the previous snapshot, if any. Fires OnChange when
// the text actually changes.
func (t *TextArea) undo() bool {
	current := t.snapshot()
	prev := t.history.popUndo(current)
	if prev == nil {
		return false
	}
	changed := prev.text != t.Text
	t.applyUndoEntry(*prev)
	if changed && t.OnChange != nil {
		t.fireChange()
	}
	return true
}

func (t *TextArea) redo() bool {
	current := t.snapshot()
	next := t.history.popRedo(current)
	if next == nil {
		return false
	}
	changed := next.text != t.Text
	t.applyUndoEntry(*next)
	if changed && t.OnChange != nil {
		t.fireChange()
	}
	return true
}
