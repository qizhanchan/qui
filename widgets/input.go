package widgets

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	. "github.com/qizhanchan/qui"
)

// Input is a single-line text input. For multi-line editing, use
// TextArea — this widget deliberately collapses the line concept so
// Enter maps to a submit action instead of inserting a newline, and
// horizontal overflow (no line wrap) is managed with text-offset
// scrolling so the cursor stays visible.
//
// Supports:
//   - Text entry + backspace / delete / arrow nav
//   - Mouse: click to position cursor, drag to select
//   - Shift+arrow extends selection
//   - Blinking cursor while focused
//   - Clipboard (Cmd/Ctrl + A/C/V/X)
//   - IME composition
//   - Undo / redo
//   - OnChange after any text mutation, OnSubmit on Enter, OnCommit on
//     Enter + focus-out (see the field's doc comment)
//
// InputVariant selects Filled (background fill + bottom underline)
// vs Outlined (transparent + stroke border) presentation. Maps to
// the filled and outlined text-field patterns on the web.
type InputVariant uint8

const (
	InputFilled InputVariant = iota
	InputOutlined
)

type Input struct {
	BaseWidget
	labelKey messageKey
	Text     string
	// Password masks the displayed text with dots while keeping the real
	// characters in Text for GetText/editing. The mask is drawn as geometry
	// at a fixed pitch (see maskPitchEm) rather than as glyphs, so caret,
	// selection and hit-test math are exact arithmetic on that pitch.
	// (HTML input type=password.)
	Password bool
	// Label is the floating label — rests in the content area at
	// body-large size when the field is empty + unfocused, and animates
	// up to a body-small caption when the field is focused or populated.
	// Outlined variant cuts a notch in the top border around the
	// floating label. Leave "" to opt out (the field then behaves like
	// a plain input that only shows Placeholder).
	Label string
	// Placeholder is the in-input ghost text shown while the field is
	// focused + empty. With a Label present this complements the label;
	// without a Label it acts as the only visible hint (vertically
	// centered, OnSurfaceVariant color).
	Placeholder string
	// placeholderKey, when set, supersedes Placeholder and is resolved
	// during Measure/Draw. Read it through DisplayPlaceholder().
	placeholderKey messageKey
	Variant        InputVariant
	// Error true switches the field into its error state — outline
	// or underline draws in Theme.Error; supporting text would too.
	Error bool
	// Dense collapses the field to button height (40 dp base, density-
	// scaled) and removes the top overhang strip so a row of buttons +
	// dense inputs lines up flush. Mirrors Select.Dense. Implies Label
	// is suppressed in both Measure and Draw (no room for a floating
	// label inside 40 dp); stack an external Label widget above when a
	// caption is still wanted. Bypasses the tall form-field geometry —
	// visually a thin outlined input that matches button height, not an
	// tall floating-label text field.
	Dense bool
	// States carries the layered container/accent styles. Base is the
	// resting look (Background, Border == accent color, BorderSize ==
	// accent thickness, Foreground == input text color). Hover /
	// Focused / Errored / Disabled override individual fields; Resolve
	// picks the right one every frame. Errored outranks Focused.
	States StateStyle
	// LabelColor / LabelColorFocused / LabelColorError override the
	// floating label color per state. Zero A means "fallback": Focused
	// falls back to LabelColor; Error falls back to LabelColorFocused
	// or LabelColor. Raw HTML default = zero (label uses Foreground);
	// a designed field sets them to Accent / Error / TextMuted.
	LabelColor        Color
	LabelColorFocused Color
	LabelColorError   Color
	// TrailingButton draws a UA-style affordance inside the field's right
	// edge (see InputTrailingButton). OnTrailingClick overrides its built-in
	// action; leave it nil for the default (Clear empties the field).
	TrailingButton  InputTrailingButton
	OnTrailingClick func()
	// OnStep fires for a stepper click: +1 for the up half, -1 for the down
	// half. The caller applies its own min / max / step rules.
	OnStep      func(dir int)
	BeforeInput func(BeforeInputEvent) bool
	OnChange    func(text string)
	OnSubmit    func(text string)
	// OnCommit fires when the user is FINISHED with a value: on Enter, and on
	// losing focus if the text changed while focused. It is the hook a field
	// whose value is expensive or disruptive to apply per keystroke needs — a
	// numeric size box, a colour hex, anything that pushes an undo step —
	// because OnChange fires on every rune ("1" of "12" is a valid number
	// too) and OnSubmit alone leaves a typed value silently unapplied when
	// the user clicks away instead of pressing Enter.
	OnCommit          func(text string)
	OnSelectionChange func(start, end int)
	OnCursorChange    func(pos int)

	// commitBaseline is the text as of the last focus-in / commit; OnCommit
	// fires only when the value actually moved, so clicking through a field
	// without editing it does not push an undo step.
	commitBaseline string

	cursorPos int // rune index of cursor (0..runeCount)
	selStart  int // rune indices of selection; -1 = none
	selEnd    int

	focused   bool
	hovering  bool
	selecting bool // mouse-drag selection in progress

	// scrollX offsets the visible text so the cursor stays within the
	// content area when Text is wider than the field.
	scrollX float32

	cursorVisible bool
	cursorBlinkMS int64

	// IME composition state. preeditText is the in-flight (not yet
	// committed) string from an input method; preeditCursor is the
	// rune index within it where the composition caret sits. Rendered
	// with a distinct visual cue (underline) at the main cursor
	// position while composing.
	preeditText   string
	preeditCursor int

	// Undo / redo history. Snapshots are taken on every mutation via
	// snapshot() and pushed through pushUndo(); consecutive "type" or
	// "delete" entries within a ~500ms window coalesce so Cmd+Z
	// reverses a word of typing instead of one character at a time.
	history undoHistory

	// lastChangeUser records whether the most recent value change came from
	// the user (a keystroke / paste / IME commit) rather than a
	// programmatic SetText. A programmatic SetText that then replaces a
	// user-set value is a controlled-value overwrite (see fireChange /
	// SetText), which the debug surface flags.
	lastChangeUser bool

	clickCount  int
	lastClickMS int64
	lastClickX  float32
	lastClickY  float32
}

// NewInput creates a plain, unstyled text field — thin gray border
// on all sides (Outlined variant), light-blue focus ring, no floating
// label styling — roughly matching a raw HTML `<input type=text>`.
// The argument is the placeholder text (matching HTML's placeholder
// attribute). For a designed look with per-state accent tinting,
// render a field on the htmlcss layer and style it with CSS.
func NewInput(placeholder string) *Input {
	t := &Input{
		BaseWidget:    NewBaseWidget(),
		Variant:       InputOutlined,
		Placeholder:   placeholder,
		selStart:      -1,
		cursorVisible: true,
	}
	t.States = defaultInputStates()
	return t
}

// defaultInputStates is the "raw HTML" text-field style: white bg
// (transparent so the parent shows), gray outline, black text, subtle
// blue focus outline. No error/hover coloring — those are designed
// concerns.
func defaultInputStates() StateStyle {
	base := Style{
		Background: htmlControlBg,
		Foreground: htmlControlText,
		Border:     htmlControlBorder,
		BorderSize: 1,
		Radius:     htmlControlRadius,
		Font:       Font{Size: htmlControlFontSize},
	}
	focused := base
	focused.Border = htmlAccent
	focused.BorderSize = 2
	disabled := base
	disabled.Foreground = htmlDisabledText
	disabled.Border = htmlDisabledBorder
	return StateStyle{
		Base:     base,
		Focused:  &focused,
		Disabled: &disabled,
	}
}

// Style returns the legacy mutable resting-state storage.
// Deprecated: use qui.StyleValue and qui.UpdateStyle.
func (t *Input) Style() *Style { return &t.States.Base }

func (t *Input) SetStyle(style Style) {
	t.States.Base = style.Clone()
	t.InvalidateLayout()
}

// currentState compiles the field's booleans into a State bitmask
// consumable by States.Resolve. Error becomes StateError; Focused,
// Hover, Disabled follow the standard bits.
func (t *Input) currentState() State {
	var s State
	if !t.Enabled() {
		s |= StateDisabled
	}
	if t.Error {
		s |= StateError
	}
	if t.focused {
		s |= StateFocused
	}
	if t.hovering {
		s |= StateHover
	}
	return s
}

// labelColorFor picks the floating-label color for the current state.
// Uses the per-state widget-level tints (LabelColor / LabelColorFocused
// / LabelColorError) with cascading fallback; when all three are
// unspecified (raw HTML default) the label reads the resolved style's
// Foreground so the label is legible without any extra config.
func (t *Input) labelColorFor(state State, style *Style) Color {
	if state&StateError != 0 && t.LabelColorError.A > 0 {
		return t.LabelColorError
	}
	if state&StateFocused != 0 && t.LabelColorFocused.A > 0 {
		return t.LabelColorFocused
	}
	if t.LabelColor.A > 0 {
		return t.LabelColor
	}
	return style.Foreground
}

// font returns the field's effective font. Every measurement / draw
// path reads through this helper.
func (t *Input) font() Font {
	return t.States.Base.Font
}

// Text-field spacing for the tall floating-label geometry.
const (
	tfLeadingSpace        float32 = 16 // leading-space / trailing-space
	tfTrailingSpace       float32 = 16
	tfNoLabelTopSpace     float32 = 16 // top-space / bottom-space (no label)
	tfNoLabelBottomSpace  float32 = 16
	tfLabelTopSpace       float32 = 8  // with-label-top-space
	tfLabelBottomSpace    float32 = 8  // with-label-bottom-space
	tfLabelPopulatedLineH float32 = 16 // body-small line-height
	tfLabelRestingLineH   float32 = 24 // body-large line-height
	tfOutlineLabelPadding float32 = 4  // outline-label-padding (half-gap each side)
	tfFieldHeight         float32 = 56 // base field height of the painted box
	// tfTopOverhang is reserved ABOVE the painted field. Outlined needs
	// it so the floating label (translated -8 px from the top border)
	// doesn't get clipped by the parent container. Filled doesn't need
	// it for label overhang — its floating label sits inside the field
	// — but we reserve the same strip anyway so a hbox of filled +
	// outlined fields keeps their visible-field centers (and resting
	// labels) aligned. Without this, AlignCenter in a flex row would
	// stack filled (56 tall) against outlined (64 tall) by widget
	// center, pushing filled's visible label 4 px higher than
	// outlined's — visually obvious in any form row.
	tfTopOverhang float32 = 8
)

// fieldBounds returns the rect of the visually-drawn field (background,
// border, underline, label, input) inside the widget bounds. Standard
// (non-Dense) fields reserve tfTopOverhang at the top so visible
// fields stack at the same Y when laid out together. Dense mode skips
// the strip entirely — the whole widget is the painted field.
// Bounds() stays the hit-test surface so a click on the overhang strip
// still focuses the field in standard mode.
func (t *Input) fieldBounds() Rect {
	b := t.Bounds()
	if t.compact() {
		return b
	}
	overhang := tfTopOverhang
	b.Y += overhang
	b.H -= overhang
	return b
}

// horizontalPad returns (leading, trailing) horizontal padding for the
// active mode. Tall form fields use 16+16 px so the input
// breathes inside a 200+ px container. Dense mode collapses to 8+8 —
// the toolbar font-size field is only ~44 px wide and the 32 px of
// padding leaves ~12 px for the digits, clipping "14" at the right
// edge. 8+8 keeps a visible inset without devouring the input area.
func (t *Input) horizontalPad() (float32, float32) {
	lead, trail := tfLeadingSpace, tfTrailingSpace
	switch {
	case t.native():
		lead, trail = htmlControlPadX, htmlControlPadX
	case t.compact():
		lead, trail = 8, 8
	}
	// A visible trailing affordance eats into the text area so glyphs never
	// run under it.
	return lead, trail + t.trailingButtonWidth()
}

// InputTrailingButton selects the in-field affordance drawn at the right
// edge of a text field — the UA-provided control a browser puts inside
// certain input types.
type InputTrailingButton uint8

const (
	// InputTrailingNone is the default: nothing drawn, full text width.
	InputTrailingNone InputTrailingButton = iota
	// InputTrailingClear is the ✕ that empties the field, which browsers
	// render inside <input type=search> once it has content.
	InputTrailingClear
	// InputTrailingStepper is the up/down pair browsers render inside
	// <input type=number>. The widget only reports WHICH way the user
	// clicked, through OnStep — min / max / step are HTML semantics and stay
	// with the caller (htmlcss reads the attributes).
	InputTrailingStepper
)

// trailingButtonSize is the affordance's square hit/paint box.
const trailingButtonSize float32 = 16

// showTrailingButton reports whether the affordance is currently drawn. The
// clear button follows the browser rule: only when there is text to clear.
func (t *Input) showTrailingButton() bool {
	switch t.TrailingButton {
	case InputTrailingClear:
		return t.Text != "" && t.Enabled()
	case InputTrailingStepper:
		// Always available (browsers reveal theirs on hover; a permanently
		// visible pair is steadier and one less thing to discover).
		return t.Enabled()
	}
	return false
}

// trailingButtonWidth is the horizontal space the affordance reserves (0
// when hidden), including a small gap from the text.
func (t *Input) trailingButtonWidth() float32 {
	if !t.showTrailingButton() {
		return 0
	}
	return trailingButtonSize + 4
}

// trailingButtonRect is the affordance's box in window coordinates, sized
// and centered inside the field's trailing inset.
func (t *Input) trailingButtonRect() Rect {
	b := t.fieldBounds()
	_, basePad := func() (float32, float32) {
		switch {
		case t.native():
			return htmlControlPadX, htmlControlPadX
		case t.compact():
			return 8, 8
		default:
			return tfLeadingSpace, tfTrailingSpace
		}
	}()
	return Rect{
		X: b.X + b.W - basePad - trailingButtonSize,
		Y: b.Y + (b.H-trailingButtonSize)/2,
		W: trailingButtonSize,
		H: trailingButtonSize,
	}
}

// TrailingButtonBounds reports the in-field affordance's box and whether one
// is currently showing. Exposed because the box is the widget's only
// interactive region that isn't derivable from Bounds() — needed to drive it
// from a test or to anchor a tooltip on it.
func (t *Input) TrailingButtonBounds() (Rect, bool) {
	if !t.showTrailingButton() {
		return Rect{}, false
	}
	return t.trailingButtonRect(), true
}

// drawTrailingButton paints the affordance. The ✕ is two strokes in a
// circle-free box (matching Chrome's flat search-cancel glyph rather than
// Safari's filled circle) and dims to the placeholder color so it reads as
// chrome, not content.
func (t *Input) drawTrailingButton(canvas Canvas, style *Style) {
	if !t.showTrailingButton() {
		return
	}
	r := t.trailingButtonRect()
	color := style.Foreground
	color.A *= 0.55
	switch t.TrailingButton {
	case InputTrailingStepper:
		// Two chevrons, one per half — the halves are also the hit zones.
		up, down := t.stepperZones()
		drawChevron(canvas, up, color, true)
		drawChevron(canvas, down, color, false)
	default:
		inset := trailingButtonSize * 0.28
		x0, y0 := r.X+inset, r.Y+inset
		x1, y1 := r.X+r.W-inset, r.Y+r.H-inset
		canvas.DrawLine(Point{X: x0, Y: y0}, Point{X: x1, Y: y1}, color, 1.5)
		canvas.DrawLine(Point{X: x1, Y: y0}, Point{X: x0, Y: y1}, color, 1.5)
	}
}

// stepperZones splits the affordance box into the increment (top) and
// decrement (bottom) halves — used for both painting and hit testing, so the
// glyph and its click target can never drift apart.
func (t *Input) stepperZones() (up, down Rect) {
	r := t.trailingButtonRect()
	half := r.H / 2
	up = Rect{X: r.X, Y: r.Y, W: r.W, H: half}
	down = Rect{X: r.X, Y: r.Y + half, W: r.W, H: half}
	return up, down
}

// drawChevron strokes a ‹^› (pointing up) or ‹v› (pointing down) centered in
// box, sized to leave breathing room on all sides.
func drawChevron(canvas Canvas, box Rect, color Color, pointUp bool) {
	padX := box.W * 0.28
	padY := box.H * 0.3
	left := Point{X: box.X + padX, Y: 0}
	right := Point{X: box.X + box.W - padX, Y: 0}
	apex := Point{X: box.X + box.W/2, Y: 0}
	if pointUp {
		left.Y, right.Y = box.Y+box.H-padY, box.Y+box.H-padY
		apex.Y = box.Y + padY
	} else {
		left.Y, right.Y = box.Y+padY, box.Y+padY
		apex.Y = box.Y + box.H - padY
	}
	canvas.DrawLine(left, apex, color, 1.5)
	canvas.DrawLine(apex, right, color, 1.5)
}

// clickTrailingButton runs the affordance's action if p is inside it,
// reporting whether it consumed the press. Clearing fires OnChange (the
// value really did change) but deliberately not OnSubmit.
func (t *Input) clickTrailingButton(p Point) bool {
	if !t.showTrailingButton() || !t.trailingButtonRect().Contains(p) {
		return false
	}
	if t.TrailingButton == InputTrailingStepper {
		// Which half decides the direction; OnStep owns min / max / step.
		up, _ := t.stepperZones()
		if t.OnStep != nil {
			if up.Contains(p) {
				t.OnStep(+1)
			} else {
				t.OnStep(-1)
			}
		}
		return true
	}
	if t.OnTrailingClick != nil {
		t.OnTrailingClick()
		return true
	}
	if t.TrailingButton == InputTrailingClear {
		t.SetText("") // fires OnChange itself
		t.cursorPos, t.selStart, t.selEnd = 0, -1, -1
		t.Invalidate()
	}
	return true
}

// inputBounds returns the rect where the input text, cursor, and
// selection are drawn — dependent on whether a Label is configured
// (which raises the top edge to make room for the floating label).
//
// When the caller lays out the field shorter than the 56 px height
// (legacy code, dense forms) the full padding would produce a negative
// or zero-height input box. We scale top/bottom proportionally so the
// input always has visible room — the field looks visually cramped
// but stays functional (cursor blink, hit-test, IME caret rect all
// remain non-degenerate).
func (t *Input) inputBounds() Rect {
	b := t.fieldBounds()
	top := tfNoLabelTopSpace
	bottom := tfNoLabelBottomSpace
	if t.native() {
		// Raw HTML <input>: text inset by border(1) + UA padding(1).
		top = htmlControlPadY
		bottom = htmlControlPadY
	} else if t.compact() {
		// The 16+16 vertical padding is sized for the 56 px form field.
		// Inside a 40-dp dense row that leaves only 8 px for the input —
		// TextBodyLarge is ~22 px tall, so ClipCanvas crops the bottom
		// half of every glyph (visible as "Filled dense" with cut-off
		// descenders). Use 8+8 to mirror horizontalPad's dense override
		// — leaves 24 px of input height, plenty for the body-large
		// face, and the natural top-of-rect text origin already lines
		// up where the resting label would have sat.
		top = 8
		bottom = 8
	} else if t.hasLabel() {
		// Filled's floating label sits INSIDE the field at rows 8..24,
		// so the input has to start below it (padding-top =
		// with-label-top-space + populated-line-height = 24, padding-
		// bottom = 8). Outlined's floating label sits ABOVE the top
		// border in the overhang strip — it doesn't overlap the input
		// area at all, so we keep symmetric padding and let DrawText
		// vertically center the text. This matches what MUI / native
		// reference renderings show (and what the user sees as
		// "properly centered") rather than the web reference's literal
		// asymmetric padding, which the actual `<input>` overrides
		// with `padding: 0` anyway.
		if t.Variant == InputOutlined {
			top = tfNoLabelTopSpace
			bottom = tfNoLabelBottomSpace
		} else {
			top = tfLabelTopSpace + tfLabelPopulatedLineH
			bottom = tfLabelBottomSpace
		}
	}
	if total := top + bottom; total >= b.H {
		// Field is shorter than the padding wanted — split available
		// height proportionally so we keep at least a sliver of input
		// area (and the cursor/IME caret keeps positive H).
		scale := (b.H * 0.5) / total
		if scale > 1 {
			scale = 1
		}
		top *= scale
		bottom *= scale
	}
	lead, trail := t.horizontalPad()
	w := b.W - lead - trail
	if w < 0 {
		w = 0
	}
	return Rect{
		X: b.X + lead,
		Y: b.Y + top,
		W: w,
		H: b.H - top - bottom,
	}
}

// compact reports whether the field uses the compact (button-height, no
// top overhang strip) geometry instead of the tall 56 px form
// field. True for Dense fields AND for plain fields with no floating
// Label — i.e. a raw HTML <input>. The tall geometry (and the
// overhang strip that reserves room above the top border) exists purely
// to house a floating label; without one, an input should be a modest
// button-height box that lines up with its neighbors. A designed theme
// opts into the tall look by setting Label.
func (t *Input) compact() bool {
	return t.Dense || t.Label == ""
}

// native reports whether the field uses the raw-HTML <input> geometry:
// no floating label AND not Dense. This is the zero-config default
// (NewInput). Dense keeps its own toolbar-height geometry; a floating
// Label opts into the tall floating-label form field. See widgets/native.go.
func (t *Input) native() bool {
	return t.Label == "" && !t.Dense
}

// hasLabel reports whether the field should render its floating label.
// Dense mode suppresses it — no room for a floating label inside a
// 40-dp row; downstream code can stack an external Label widget above
// when a caption is still wanted. Mirrors Select.hasLabel.
func (t *Input) hasLabel() bool {
	return t.Label != "" && !t.Dense
}

// isLabelFloating reports whether the label should ride in the floating
// position (top of field, body-small) vs the resting position
// (centered, body-large). It floats when focused OR populated.
func (t *Input) isLabelFloating() bool {
	return t.hasLabel() && (t.focused || t.Text != "" || t.preeditText != "")
}

func (t *Input) Focusable() bool    { return t.Enabled() }
func (t *Input) CancelInteraction() { t.selecting = false }

// Focused reports whether the field currently holds keyboard focus.
func (t *Input) Focused() bool { return t.focused }

func (t *Input) SetFocused(f bool) {
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	if f {
		t.commitBaseline = t.Text
	} else if t.focused {
		t.fireCommit()
	}
	t.focused = f
	// Focus transitions seal the current coalescing group — a
	// later re-focus + typing starts fresh rather than merging
	// with whatever was happening before blur.
	t.history.breakCoalesce()
	if f {
		t.cursorVisible = true
		t.cursorBlinkMS = time.Now().UnixMilli()
	}
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// SetText programmatically replaces the content and resets cursor/selection.
// Fires OnChange if the text actually changed. Pushes an undo entry so
// callers (e.g. a "Reset" button) remain reversible.
func (t *Input) SetText(s string) {
	if t.Text == s {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	before := t.snapshot()
	t.Text = s
	t.cursorPos = utf8.RuneCountInString(s)
	t.selStart = -1
	t.selEnd = 0
	t.scrollX = 0
	t.pushUndo("set", before)
	// SetText only runs when the value actually changes (early return
	// above), so if the last change was the user's own edit, this
	// programmatic replacement is overwriting it — the controlled-value
	// fight signal.
	source := "program"
	if t.lastChangeUser {
		source = "program-overwrite"
	}
	t.lastChangeUser = false
	if t.OnChange != nil {
		t.OnChange(s)
	}
	if w := t.Window(); w != nil {
		w.RecordInputChange(t, before.text, s, source)
	}
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// GetText returns the current text.
func (t *Input) GetText() string { return t.Text }

func (t *Input) Measure(available Size) Size {
	width := float32(200)
	var height float32
	switch {
	case t.native():
		// Raw HTML <input>: fixed 180×22 box at the baseline font, but
		// grow the height with a larger font so inherited font-size isn't
		// clipped (border 1 + padding 1 on each side = 4 px of chrome).
		width = htmlInputWidth
		height = htmlControlHeight
		// Grow only when an inherited font is larger than the baseline, so
		// the default 14 px field stays the exact 22 px UA box.
		if fs := t.States.Base.Font.Size; fs > htmlControlFontSize {
			if needed := fs*1.3 + 2*htmlControlPadY; needed > height {
				height = needed
			}
		}
	case t.Dense:
		// Toolbar height: button-row baseline (40 dp), grown with the font.
		height = buttonHeight
		if fs := t.States.Base.Font.Size; fs > 0 {
			if needed := fs*1.3 + 16; needed > height {
				height = needed
			}
		}
	default:
		// Floating-label form field: tall 56 px + overhang strip.
		height = tfFieldHeight + tfTopOverhang
	}
	if available.W > 0 && width > available.W {
		width = available.W
	}
	return Size{W: width, H: height}
}

func (t *Input) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := t.fieldBounds()
	state := t.currentState()
	style := themedStyle(t.States.Resolve(state))
	radius := style.Radius

	// Variant background. Filled paints its container fill; Outlined
	// stays transparent so its border does all the work. Base style's
	// Background is honored either way.
	if t.Variant != InputOutlined && style.Background.A > 0 {
		canvas.FillRoundedRect(b, radius, style.Background)
	}

	accent := style.Border
	thickness := style.BorderSize
	if thickness <= 0 && accent.A > 0 {
		thickness = 1
	}
	labelColor := t.labelColorFor(state, style)
	floating := t.isLabelFloating()

	lead, trail := t.horizontalPad()
	// Border / underline geometry per variant.
	switch t.Variant {
	case InputOutlined:
		if accent.A > 0 {
			canvas.StrokeRoundedRect(b, radius, accent, thickness)
		}
		if floating && accent.A > 0 {
			// Notch: paint the active surface color over the segment of
			// the top border under the label so the floating label reads
			// as sitting on the border line, not through it.
			labelW, _ := TextMetrics(t.DisplayLabel(), ThemeFont(TextBodySmall))
			notchX := b.X + lead - tfOutlineLabelPadding
			notchW := labelW + tfOutlineLabelPadding*2
			canvas.FillRect(Rect{
				X: notchX,
				Y: b.Y,
				W: notchW,
				H: thickness,
			}, theme.Surface)
		}
	default: // filled — bottom underline only
		if accent.A > 0 {
			canvas.FillRect(Rect{X: b.X, Y: b.Y + b.H - thickness, W: b.W, H: thickness}, accent)
		}
	}

	// Floating / resting label. Painted before the input so cursor +
	// selection sit above it visually (though they never overlap when
	// label is floating — input area starts below). Suppressed in Dense
	// mode via hasLabel.
	if t.hasLabel() {
		var labelRect Rect
		var labelFont Font
		if floating {
			labelFont = ThemeFont(TextBodySmall)
			labelY := b.Y + tfLabelTopSpace // filled: 8 px inside field
			if t.Variant == InputOutlined {
				// Outlined: center the label box on the top border line.
				// the web reference translates -100% + label-text-padding-bottom;
				// with line-height 16 the net offset is -8 px from field top.
				labelY = b.Y - tfLabelPopulatedLineH/2
			}
			labelRect = Rect{
				X: b.X + lead,
				Y: labelY,
				W: b.W - lead - trail,
				H: tfLabelPopulatedLineH,
			}
		} else {
			labelFont = ThemeFont(TextBodyLarge)
			labelRect = Rect{
				X: b.X + lead,
				Y: b.Y + tfNoLabelTopSpace, // top-space = 16
				W: b.W - lead - trail,
				H: tfLabelRestingLineH,
			}
		}
		canvas.DrawText(t.DisplayLabel(), labelRect, labelColor, labelFont)
	}

	// The in-field affordance sits in the trailing inset, OUTSIDE the text
	// content clip — draw it before that clip goes on. (It was invisible
	// while it ran after a deferred RestoreTo: the clip was still active.)
	t.drawTrailingButton(canvas, style)

	content := t.inputBounds()
	contentID := canvas.Save()
	canvas.ClipRect(content)
	defer canvas.RestoreTo(contentID)

	// Selection highlight (drawn under the text).
	if t.hasSelection() {
		a, c := t.orderedSelection()
		fill := func(offsetX, width float32) {
			x0 := content.X - t.scrollX + offsetX
			x1 := x0 + width
			if x0 < content.X {
				x0 = content.X
			}
			if x1 > content.X+content.W {
				x1 = content.X + content.W
			}
			if x1 > x0 {
				canvas.FillRect(Rect{X: x0, Y: content.Y, W: x1 - x0, H: content.H}, SelectionHighlight(t))
			}
		}
		if t.Password {
			// One contiguous run — a masked field is monospaced and LTR.
			pitch := t.maskPitch()
			fill(float32(a)*pitch, float32(c-a)*pitch)
		} else {
			for _, segment := range TextSelectionSegments(t.Text, t.font(), TextDirectionAuto, a, c) {
				fill(segment.X, segment.Width)
			}
		}
	}

	// Text or placeholder. With a Label present, placeholder only shows
	// while focused (the label rides at rest until focus, at which
	// point label floats and placeholder appears beneath). Without a
	// Label, placeholder shows whenever Text is empty — preserves the
	// legacy NewInput(placeholder) behavior callers depend on.
	// Suppress the placeholder while an IME composition is in flight:
	// the preedit text occupies the caret column and any placeholder
	// underneath bleeds through (visible as overlapped glyphs during
	// CJK input).
	textColor := style.Foreground
	text := t.Text
	if text == "" && t.preeditText == "" {
		if !t.hasLabel() || t.focused {
			text = t.DisplayPlaceholder()
			textColor = theme.TextMuted
		}
	}
	if t.preeditText != "" {
		// Composition in flight: split the committed text at the cursor and
		// render [before][preedit][after], so the in-flight text is INSERTED
		// at the caret (pushing the following text right) instead of being
		// painted on top of it. The preedit carries an underline. Everything
		// is clipped to `content` (ClipRect above), so widths can be generous.
		runes := []rune(t.Text)
		cp := t.cursorPos
		if cp > len(runes) {
			cp = len(runes)
		}
		baseX := content.X - t.scrollX
		beforeW := t.widthUpToRune(cp)
		preeditW, _ := TextMetrics(t.preeditText, t.font())
		fullW := content.W + t.scrollX + preeditW
		if before := string(runes[:cp]); before != "" {
			if t.Password {
				t.drawMaskRun(canvas, content, baseX, cp, textColor)
			} else {
				canvas.DrawText(before, Rect{X: baseX, Y: content.Y, W: fullW, H: content.H}, textColor, t.font())
			}
		}
		preeditX := baseX + beforeW
		canvas.DrawText(t.preeditText, Rect{X: preeditX, Y: content.Y, W: fullW, H: content.H}, style.Foreground, t.font())
		canvas.FillRect(Rect{X: preeditX, Y: content.Y + content.H - 2, W: preeditW, H: 1}, style.Foreground)
		if after := string(runes[cp:]); after != "" {
			if t.Password {
				t.drawMaskRun(canvas, content, preeditX+preeditW, len(runes)-cp, textColor)
			} else {
				canvas.DrawText(after, Rect{X: preeditX + preeditW, Y: content.Y, W: fullW, H: content.H}, textColor, t.font())
			}
		}
	} else if text != "" {
		// Mask only real text — never the placeholder (shown when Text is empty).
		if t.Password && t.Text != "" {
			t.drawMaskRun(canvas, content, content.X-t.scrollX, t.runeCount(), textColor)
		} else {
			textRect := Rect{X: content.X - t.scrollX, Y: content.Y, W: content.W + t.scrollX, H: content.H}
			canvas.DrawText(text, textRect, textColor, t.font())
		}
	}

	// Cursor.
	showCursor := t.focused && (t.cursorVisible || t.preeditText != "")
	if showCursor {
		caretX := content.X - t.scrollX + t.widthUpToRune(t.cursorPos)
		if t.preeditText != "" {
			runes := []rune(t.preeditText)
			cur := t.preeditCursor
			if cur > len(runes) {
				cur = len(runes)
			}
			caretX += TextXForOffset(t.preeditText, t.font(), TextDirectionAuto, cur)
		}
		if caretX >= content.X-0.5 && caretX <= content.X+content.W+0.5 {
			canvas.FillRect(Rect{X: caretX, Y: content.Y, W: 1, H: content.H}, style.Foreground)
		}
	}
}

func (t *Input) Handle(event Event) bool {
	if !t.Enabled() {
		return false
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	prevText, prevScrollX := t.Text, t.scrollX
	prevPreedit, prevPreeditCursor := t.preeditText, t.preeditCursor
	prevSelecting, prevCursorVisible := t.selecting, t.cursorVisible
	defer func() {
		t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
		if t.Text != prevText ||
			t.cursorPos != prevCursor ||
			t.selStart != prevSelStart ||
			t.selEnd != prevSelEnd ||
			t.scrollX != prevScrollX ||
			t.preeditText != prevPreedit ||
			t.preeditCursor != prevPreeditCursor ||
			t.selecting != prevSelecting ||
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
			if e.Button != MouseButtonLeft {
				return false
			}
			// The in-field affordance (search clear) takes the press before
			// caret placement, so clicking ✕ doesn't also move the cursor.
			if t.clickTrailingButton(Point{X: e.X, Y: e.Y}) {
				return true
			}
			if t.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				pos := t.runeIndexAt(e.X)
				clicks := t.registerClick(e)
				switch clicks {
				case 2:
					a, b := t.wordBoundsAt(pos)
					t.selStart = a
					t.selEnd = b
					t.cursorPos = b
				case 3:
					n := t.runeCount()
					t.selStart = 0
					t.selEnd = n
					t.cursorPos = n
				default:
					t.cursorPos = pos
					t.selStart = pos
					t.selEnd = pos
				}
				t.selecting = true
				t.history.breakCoalesce()
				t.resetBlink()
				return true
			}
		case EventMouseMove:
			if t.selecting {
				content := t.inputBounds()
				if e.X < content.X {
					t.scrollX -= min(16, content.X-e.X)
				} else if e.X > content.X+content.W {
					t.scrollX += min(16, e.X-(content.X+content.W))
				}
				if t.scrollX < 0 {
					t.scrollX = 0
				}
				t.selEnd = t.runeIndexAt(e.X)
				t.cursorPos = t.selEnd
				t.ensureCursorVisible()
				t.resetBlink()
				return true
			}
		case EventMouseUp:
			if t.selecting {
				t.selecting = false
				if t.selStart == t.selEnd {
					t.selStart = -1
				}
				return true
			}
		}
	case KeyEvent:
		if !t.focused || e.Type() != EventKeyDown {
			return false
		}
		// IME swallow: while composition is active (preedit non-empty),
		// every text-editing keydown belongs to the IME. GLFW's
		// keyDown: on macOS fires our KeyCallback BEFORE handing the
		// event to interpretKeyEvents / the input context — so without
		// this guard, Backspace would both shrink the preedit (via IME
		// → setMarkedText) and delete a rune from Text. Same concern
		// for Enter (IME commits), Escape (IME cancels), arrows (some
		// IMEs navigate candidates).
		if t.preeditText != "" {
			return true
		}
		return t.handleKey(e)
	case CharEvent:
		if !t.focused {
			return false
		}
		return t.handleChar(e.Rune)
	}
	return false
}

func (t *Input) HitTest(p Point) Widget {
	if t.Bounds().Contains(p) {
		return t
	}
	return nil
}

// WidgetCursor shows the text I-beam over an editable field. A disabled
// field declines so the shape falls back to its surroundings — that is what
// lets a CSS `cursor: not-allowed` wrapper read through (see qui/cursor.go).
func (t *Input) WidgetCursor() (CursorShape, bool) {
	return CursorText, t.Enabled()
}

// Tick drives cursor blink. Matches TextArea's 500ms interval.
// Returns widget Bounds as dirty region when visibility flips so the
// window repaints only this field.
func (t *Input) Tick(now time.Time) Rect {
	if !t.focused {
		return Rect{}
	}
	nowMS := now.UnixMilli()
	if nowMS-t.cursorBlinkMS > 500 {
		t.cursorVisible = !t.cursorVisible
		t.cursorBlinkMS = nowMS
		return t.Bounds()
	}
	return Rect{}
}

// ----------------------------------------------------------------------
// Keyboard handling

func (t *Input) handleKey(e KeyEvent) bool {
	if IsCommandMod(e.Mods) {
		// Cmd/Ctrl+Z = undo, Cmd/Ctrl+Shift+Z = redo, Cmd/Ctrl+Y = redo
		// (Windows-style alias). Checked before clipboard so Z/Y don't
		// get misrouted through handleClipboardShortcut.
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
		if handled := t.handleClipboardShortcut(e.Key); handled {
			return true
		}
	}
	shift := e.Mods&ModShift != 0
	switch e.Key {
	case KeyLeft:
		if len(t.Text) > 0 {
			target := t.visualNeighbor(-1)
			if shift {
				t.extendSelection(target)
			} else {
				t.clearSelection()
				t.cursorPos = target
			}
		}
		t.history.breakCoalesce()
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyRight:
		runes := []rune(t.Text)
		if len(runes) > 0 {
			target := t.visualNeighbor(1)
			if shift {
				t.extendSelection(target)
			} else {
				t.clearSelection()
				t.cursorPos = target
			}
		}
		t.history.breakCoalesce()
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyHome:
		if shift {
			t.extendSelection(0)
		} else {
			t.clearSelection()
			t.cursorPos = 0
		}
		t.history.breakCoalesce()
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyEnd:
		end := t.runeCount()
		if shift {
			t.extendSelection(end)
		} else {
			t.clearSelection()
			t.cursorPos = end
		}
		t.history.breakCoalesce()
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyBackspace:
		if !t.beforeInput("delete", "") {
			return true
		}
		before := t.snapshot()
		if t.hasSelection() {
			t.deleteSelection()
			t.pushUndo("delete", before)
			t.fireChange()
		} else if t.cursorPos > 0 {
			// Delete one grapheme cluster, not one rune: backspacing a
			// 👌🏻 should remove the whole emoji in one keystroke (and
			// leave nothing behind), not strip the skin-tone modifier
			// and leave a bare 👌.
			runes := []rune(t.Text)
			target := PrevClusterBoundary(runes, t.cursorPos)
			t.Text = string(runes[:target]) + string(runes[t.cursorPos:])
			t.cursorPos = target
			t.pushUndo("delete", before)
			t.fireChange()
		}
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyEnter:
		if t.OnSubmit != nil {
			t.OnSubmit(t.Text)
		}
		t.fireCommit()
		if w := t.Window(); w != nil {
			w.RecordInputSubmit(t, t.Text)
		}
		// Without a submit handler Enter keeps bubbling — the HTML
		// "implicit submission": a dialog's default action presses on
		// Enter while its text field has focus.
		return t.OnSubmit != nil
	case KeyEscape:
		// Esc first drops a selection. With none, it is not the field's
		// key: it bubbles to whatever dismisses (a dialog, a popup).
		if !t.hasSelection() {
			return false
		}
		t.clearSelection()
		t.history.breakCoalesce()
		t.resetBlink()
		return true
	}
	return false
}

func (t *Input) handleChar(r rune) bool {
	if r < 0x20 && r != 0x7F {
		return false // ignore control chars
	}
	if !t.beforeInput("type", string(r)) {
		return true
	}
	// CharEvents only reach the widget when the IME is NOT actively
	// composing — during composition the OS input method intercepts
	// every key. So a CharEvent with non-empty preedit means the IME
	// just committed: clear the preedit state so the committed text
	// doesn't get visually overlaid by the previous marked text.
	// (Some IMEs — notably macOS 拼音 — do not always call
	// unmarkText after commit, making this defensive clear essential.)
	if t.preeditText != "" {
		t.preeditText = ""
		t.preeditCursor = 0
	}
	before := t.snapshot()
	if t.hasSelection() {
		t.deleteSelection()
	}
	t.insertRune(t.cursorPos, r)
	t.cursorPos++
	// Whitespace breaks the coalescing group so Cmd+Z undoes one word
	// at a time, not the entire typing session.
	kind := "type"
	if r == ' ' || r == '\t' {
		t.pushUndo(kind, before)
		t.history.breakCoalesce()
	} else {
		t.pushUndo(kind, before)
	}
	t.fireChange()
	t.ensureCursorVisible()
	t.resetBlink()
	return true
}

// handleClipboardShortcut processes Cmd/Ctrl + A/C/V/X. Returns true
// if the key was a recognized shortcut (even when it was a no-op, e.g.
// Cmd+C with no selection) so the outer switch doesn't also fire its
// default bindings for KeyA / KeyC / etc.
func (t *Input) handleClipboardShortcut(k Key) bool {
	switch k {
	case KeyA:
		n := t.runeCount()
		if n == 0 {
			return true
		}
		t.selStart = 0
		t.selEnd = n
		t.cursorPos = n
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	case KeyC:
		if t.hasSelection() {
			a, b := t.orderedSelection()
			runes := []rune(t.Text)
			SetClipboardText(string(runes[a:b]))
		}
		return true
	case KeyX:
		if t.hasSelection() {
			if !t.beforeInput("cut", "") {
				return true
			}
			a, b := t.orderedSelection()
			runes := []rune(t.Text)
			SetClipboardText(string(runes[a:b]))
			before := t.snapshot()
			t.deleteSelection()
			t.pushUndo("cut", before)
			t.fireChange()
			t.ensureCursorVisible()
			t.resetBlink()
		}
		return true
	case KeyV:
		text := GetClipboardText()
		if text == "" {
			return true
		}
		// Input is single-line: collapse newlines so pasting from
		// a rich source doesn't smuggle in \n that the widget can't
		// represent without layout artifacts.
		text = strings.ReplaceAll(text, "\r\n", " ")
		text = strings.NewReplacer("\n", " ", "\r", " ").Replace(text)
		if !t.beforeInput("paste", text) {
			return true
		}
		before := t.snapshot()
		if t.hasSelection() {
			t.deleteSelection()
		}
		for _, r := range text {
			t.insertRune(t.cursorPos, r)
			t.cursorPos++
		}
		t.pushUndo("paste", before)
		t.fireChange()
		t.ensureCursorVisible()
		t.resetBlink()
		return true
	}
	return false
}

// ----------------------------------------------------------------------
// Undo / redo

func (t *Input) snapshot() undoEntry {
	return undoEntry{
		text:      t.Text,
		cursorPos: t.cursorPos,
		selStart:  t.selStart,
		selEnd:    t.selEnd,
	}
}

func (t *Input) applyUndoEntry(e undoEntry) {
	t.Text = e.text
	t.cursorPos = e.cursorPos
	t.selStart = e.selStart
	t.selEnd = e.selEnd
	// Undo/redo must not leave a stale IME preedit pinned to the old
	// caret — the restored text may have different length/offsets.
	t.preeditText = ""
	t.preeditCursor = 0
	t.ensureCursorVisible()
	t.resetBlink()
}

func (t *Input) pushUndo(kind string, before undoEntry) {
	t.history.push(kind, before, t.snapshot())
}

// undo restores the previous snapshot. Returns true if a snapshot
// was consumed. Fires OnChange when text actually changes.
func (t *Input) undo() bool {
	current := t.snapshot()
	prev := t.history.popUndo(current)
	if prev == nil {
		return false
	}
	changed := prev.text != t.Text
	t.applyUndoEntry(*prev)
	if changed {
		t.fireChange()
	}
	return true
}

func (t *Input) redo() bool {
	current := t.snapshot()
	next := t.history.popRedo(current)
	if next == nil {
		return false
	}
	changed := next.text != t.Text
	t.applyUndoEntry(*next)
	if changed {
		t.fireChange()
	}
	return true
}

// ----------------------------------------------------------------------
// Text manipulation

func (t *Input) runeCount() int { return utf8.RuneCountInString(t.Text) }

func (t *Input) insertRune(pos int, r rune) {
	runes := []rune(t.Text)
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}
	out := make([]rune, 0, len(runes)+1)
	out = append(out, runes[:pos]...)
	out = append(out, r)
	out = append(out, runes[pos:]...)
	t.Text = string(out)
}

func (t *Input) removeRune(pos int) {
	runes := []rune(t.Text)
	if pos < 0 || pos >= len(runes) {
		return
	}
	t.Text = string(append(runes[:pos], runes[pos+1:]...))
}

func (t *Input) deleteSelection() {
	a, b := t.orderedSelection()
	runes := []rune(t.Text)
	if a < 0 || b > len(runes) || a >= b {
		t.clearSelection()
		return
	}
	t.Text = string(append(runes[:a], runes[b:]...))
	t.cursorPos = a
	t.clearSelection()
}

// ----------------------------------------------------------------------
// Selection helpers

func (t *Input) hasSelection() bool {
	return t.selStart >= 0 && t.selStart != t.selEnd
}

func (t *Input) orderedSelection() (int, int) {
	if t.selStart < t.selEnd {
		return t.selStart, t.selEnd
	}
	return t.selEnd, t.selStart
}

func (t *Input) clearSelection() {
	t.selStart = -1
	t.selEnd = 0
}

// extendSelection moves cursor to pos and extends the current selection.
// If no selection exists yet, anchor at the old cursorPos.
func (t *Input) extendSelection(pos int) {
	if t.selStart < 0 {
		t.selStart = t.cursorPos
	}
	t.cursorPos = pos
	t.selEnd = pos
}

// ----------------------------------------------------------------------
// Layout + scroll arithmetic

// widthUpToRune returns the pixel width of Text[:runeIdx] using the
// widget's font metrics. Used to position cursor + selection.
func (t *Input) widthUpToRune(runeIdx int) float32 {
	runes := []rune(t.Text)
	if runeIdx < 0 {
		runeIdx = 0
	}
	if runeIdx > len(runes) {
		runeIdx = len(runes)
	}
	if t.Password {
		return float32(runeIdx) * t.maskPitch()
	}
	return TextXForOffset(t.Text, t.font(), TextDirectionAuto, runeIdx)
}

// Password mask geometry, in em (multiplied by the field's font size).
//
// The mask is deliberately NOT a font glyph. The primary font is commonly a
// system CJK face (PingFang on macOS, Noto CJK on Linux, YaHei on Windows),
// and in those U+2022 BULLET is FULL-WIDTH punctuation: a 1.0 em advance
// carrying a ~0.15 em dot. Masking through it made a password field render
// ~1.6x wider than the same text unmasked, with tiny dots adrift in
// whitespace — visibly out of step with every other field. Drawing the dots
// ourselves makes the field font-independent, and keeps the run monospaced
// so all the caret math below is exact rather than a shaping round-trip.
const (
	// maskPitchEm is the advance per masked rune. Sits in the middle of the
	// Latin advance band (a≈0.57, n≈0.62, o≈0.63 em), so an N-character
	// password occupies about the width N characters would unmasked.
	maskPitchEm = 0.60
	// maskDotRadiusEm gives a 0.30 em dot — the visual weight of a bullet in
	// a proportional face (goregular's is 0.27 em wide), leaving a gap the
	// same size as the dot.
	maskDotRadiusEm = 0.15
)

// maskFontSize is the font size the mask geometry scales off, resolving the
// "unset means default" convention the font stack uses.
func (t *Input) maskFontSize() float32 {
	if size := t.font().Size; size > 0 {
		return size
	}
	return 14
}

// maskPitch is the per-rune advance of the masked run.
func (t *Input) maskPitch() float32 { return t.maskFontSize() * maskPitchEm }

// maskDotRadius is the radius of one mask dot.
func (t *Input) maskDotRadius() float32 { return t.maskFontSize() * maskDotRadiusEm }

// maskDotCenterY is the y the dots center on: the middle of the font's
// x-height band, which is where a lowercase run's ink actually sits. Centering
// on the content box instead would float the dots ~2px above the line real
// text occupies in the very same field — the sort of near-miss that reads as
// "off" without being obviously wrong.
func (t *Input) maskDotCenterY(content Rect) float32 {
	return TextXHeightCenterY(content, t.font())
}

// drawMaskRun paints n mask dots rightward from startX, centered vertically
// in content and clipped to it — one dot per rune, at maskPitch, so the dots
// line up with the caret and selection rects computed from the same pitch.
func (t *Input) drawMaskRun(canvas Canvas, content Rect, startX float32, n int, color Color) {
	pitch, r := t.maskPitch(), t.maskDotRadius()
	if n <= 0 || pitch <= 0 || r <= 0 {
		return
	}
	cy := t.maskDotCenterY(content)
	for i := 0; i < n; i++ {
		cx := startX + float32(i)*pitch + pitch/2
		if cx+r < content.X {
			continue // scrolled off to the left
		}
		if cx-r > content.X+content.W {
			break // past the right edge — the rest are too
		}
		canvas.FillRoundedRect(Rect{X: cx - r, Y: cy - r, W: 2 * r, H: 2 * r}, r, color)
	}
}

// visualNeighbor returns the caret position one glyph left (dir<0) or right
// (dir>0) of the cursor. A masked field has no bidi or cluster structure —
// every dot is a plain LTR box — so it steps one rune instead of asking the
// shaper about a string the user never sees.
func (t *Input) visualNeighbor(dir int) int {
	if !t.Password {
		return TextVisualNeighbor(t.Text, t.font(), TextDirectionAuto, t.cursorPos, dir)
	}
	pos := t.cursorPos + dir
	if pos < 0 {
		pos = 0
	}
	if n := t.runeCount(); pos > n {
		pos = n
	}
	return pos
}

// runeIndexAt maps a mouse X in screen space to the closest rune index.
// Uses the per-character-closest-to-cursor rule (pick whichever side
// of a character boundary the click is closer to).
func (t *Input) runeIndexAt(x float32) int {
	content := t.inputBounds()
	localX := x - (content.X - t.scrollX)
	if t.Password {
		// Uniform pitch: snap to the nearest dot boundary.
		pitch, n := t.maskPitch(), t.runeCount()
		if pitch <= 0 {
			return n
		}
		idx := int(localX/pitch + 0.5)
		if idx < 0 {
			idx = 0
		}
		if idx > n {
			idx = n
		}
		return idx
	}
	return TextOffsetAtX(t.Text, t.font(), TextDirectionAuto, localX)
}

// ensureCursorVisible nudges scrollX so the cursor lands within the
// content area. Keeps a 4px visual margin on both sides.
func (t *Input) ensureCursorVisible() {
	content := t.inputBounds()
	if content.W <= 0 {
		return
	}
	cursorAbsX := t.widthUpToRune(t.cursorPos)
	// cursorAbsX is the cursor position within the text (0-based).
	// The visible window is [scrollX, scrollX + content.W].
	if cursorAbsX < t.scrollX+4 {
		t.scrollX = cursorAbsX - 4
	}
	if cursorAbsX > t.scrollX+content.W-4 {
		t.scrollX = cursorAbsX - content.W + 4
	}
	if t.scrollX < 0 {
		t.scrollX = 0
	}
}

func (t *Input) resetBlink() {
	t.cursorVisible = true
	t.cursorBlinkMS = time.Now().UnixMilli()
}

// NotifyTextChanged implements qui.TextChangeNotifier: publish the current
// text as a user-equivalent change. SetText itself stays silent (a
// programmatic set must not re-enter the app's handler), so the agent's Type
// action calls this after its bulk set to make the two paths behave alike.
func (t *Input) NotifyTextChanged() { t.fireChange() }

func (t *Input) fireChange() {
	t.lastChangeUser = true
	if t.OnChange != nil {
		t.OnChange(t.Text)
	}
	if w := t.Window(); w != nil {
		w.RecordInputChange(t, "", t.Text, "user")
	}
}

// TextState reports the input's editing state for the AX / agent debug
// surface (qui.TextEditable): caret, selection, in-flight IME preedit and
// undo availability.
func (t *Input) TextState() TextState {
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

// ----------------------------------------------------------------------
// IMEClient implementation — the widget side of IME support.

// SetPreedit stores in-flight composition text. Draw renders it with
// underline at the caret. Call with ("", 0) to clear.
func (t *Input) SetPreedit(text string, cursor int) {
	t.preeditText = text
	t.preeditCursor = cursor
	t.ensureCursorVisible()
	t.resetBlink()
	t.Invalidate()
}

// CommitIME finalizes a composition: clears preedit state and inserts
// the committed text at the caret, same path as normal char entry
// (honors selection replacement, fires OnChange, advances cursor).
func (t *Input) CommitIME(text string) {
	t.preeditText = ""
	t.preeditCursor = 0
	if text == "" {
		return
	}
	if !t.beforeInput("ime", text) {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	before := t.snapshot()
	if t.hasSelection() {
		t.deleteSelection()
	}
	for _, r := range text {
		t.insertRune(t.cursorPos, r)
		t.cursorPos++
	}
	t.pushUndo("ime", before)
	t.fireChange()
	t.ensureCursorVisible()
	t.resetBlink()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// CursorPos returns the caret position as a rune index into the text.
func (t *Input) CursorPos() int { return t.cursorPos }

// SetCursorPos moves the caret to a rune index (clamped) and collapses
// any selection. Fires the cursor-change callback.
func (t *Input) SetCursorPos(pos int) {
	n := len([]rune(t.Text))
	if pos < 0 {
		pos = 0
	}
	if pos > n {
		pos = n
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	t.cursorPos = pos
	t.clearSelection()
	t.ensureCursorVisible()
	t.resetBlink()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// SelectRange selects [start, end) (rune indices, clamped) and parks the
// caret at end. start==end collapses to a caret with no selection.
func (t *Input) SelectRange(start, end int) {
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
	t.ensureCursorVisible()
	t.resetBlink()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

// InsertText inserts text at the caret, replacing any active selection,
// and fires OnChange. Undoable as a single step. The programmatic edit
// path used by autocomplete and point-mode reference insertion.
func (t *Input) InsertText(text string) {
	if text == "" {
		return
	}
	if !t.beforeInput("insert", text) {
		return
	}
	prevCursor, prevSelStart, prevSelEnd := t.cursorPos, t.selStart, t.selEnd
	before := t.snapshot()
	if t.hasSelection() {
		t.deleteSelection()
	}
	for _, r := range text {
		t.insertRune(t.cursorPos, r)
		t.cursorPos++
	}
	t.pushUndo("insert", before)
	t.fireChange()
	t.ensureCursorVisible()
	t.resetBlink()
	t.emitStateChange(prevCursor, prevSelStart, prevSelEnd)
	t.Invalidate()
}

func (t *Input) beforeInput(kind, text string) bool {
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

func (t *Input) emitStateChange(prevCursor, prevSelStart, prevSelEnd int) {
	if t.cursorPos != prevCursor && t.OnCursorChange != nil {
		t.OnCursorChange(t.cursorPos)
	}
	if (t.selStart != prevSelStart || t.selEnd != prevSelEnd) && t.OnSelectionChange != nil {
		t.OnSelectionChange(t.selStart, t.selEnd)
	}
}

func (t *Input) registerClick(e MouseEvent) int {
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

func (t *Input) wordBoundsAt(pos int) (int, int) {
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
	for start > 0 && textWordRune(runes[start-1]) == isWord {
		start--
	}
	end := pos + 1
	for end < len(runes) && textWordRune(runes[end]) == isWord {
		end++
	}
	return start, end
}

func textWordRune(r rune) bool {
	if unicode.IsSpace(r) {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// CaretRect returns the current caret rectangle in window coordinates
// — the OS IME bridge uses this to anchor the candidate popup near
// where the user is typing.
func (t *Input) CaretRect() Rect {
	content := t.inputBounds()
	cx := content.X - t.scrollX + t.widthUpToRune(t.cursorPos)
	// If composing, the effective caret is after the preedit cursor.
	if t.preeditText != "" {
		runes := []rune(t.preeditText)
		if t.preeditCursor > len(runes) {
			t.preeditCursor = len(runes)
		}
		cx += TextXForOffset(t.preeditText, t.font(), TextDirectionAuto, t.preeditCursor)
	}
	return Rect{X: cx, Y: content.Y, W: 1, H: content.H}
}

// fireCommit reports a finished value (see OnCommit) and rebases the
// baseline, so Enter followed by a blur commits once, not twice.
func (t *Input) fireCommit() {
	if t.OnCommit == nil || t.Text == t.commitBaseline {
		t.commitBaseline = t.Text
		return
	}
	t.commitBaseline = t.Text
	t.OnCommit(t.Text)
}
