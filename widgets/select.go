package widgets

import (
	"strings"
	"time"
	"unicode"

	. "github.com/qizhanchan/qui"
)

// SelectVariant selects the structural border geometry of the
// trigger surface. Filled paints a bottom underline (theme-agnostic —
// it's the accent stripe under a container-filled trigger); Outlined
// paints a full stroke around a transparent container.
//
// The variant controls only the border SHAPE (underline vs full ring)
// — the actual colors, thickness, and container fill come from
// States (Base / Focused / Disabled). The dropdown surface follows the
// current theme by default (see openDropdown).
type SelectVariant uint8

const (
	SelectFilled SelectVariant = iota
	SelectOutlined
)

// Select shows a single selected value in a select trigger
// (a Input-shaped surface with a trailing downward triangle), and
// opens a dropdown menu of alternatives on click / Space / Enter when
// focused. Selecting an item closes the dropdown and fires OnChange.
//
// Geometry follows the Input spec: 56 px field height + 8 px top
// overhang (= 64 widget height) so a hbox of Input + Select stacks
// with their visible-field centers aligned. The dropdown follows
// the shared menu look (raised surface, level-2 elevation, small
// corner radius, 48 px row height).
//
// The dropdown is a Popup pushed onto a window's overlay stack: the
// window passed to the constructor, or — when that is nil, as when the tree
// is built before it is mounted — the window the Select is attached to.
//
// Options: Items is the list of displayed labels. SetOptions supplies a
// richer model — a value distinct from the label, a catalog key, an icon,
// a group heading, a disabled flag — and keeps Items in sync with it.
//
// Usage:
//
//	cb := widgets.NewFilledComboBox(window, "Speed", []string{"0.5x", "1x", "2x"},
//	    func(idx int, value string) { log.Println("picked:", value) })
//	cb.SelectedIdx = 1  // pre-select "1x"
type Select struct {
	BaseWidget
	Items []string
	// ItemDisabled marks unselectable options, parallel to Items (a shorter
	// slice leaves the rest enabled). Set it through SetItemDisabled — see
	// there for the rendering contract.
	ItemDisabled []bool
	// ItemColors tints each dropdown row's text, parallel to Items (a
	// zero-alpha entry keeps the dropdown's default foreground; a shorter
	// slice leaves the rest untinted). For colour-coded vocabularies such
	// as an HTTP-method picker.
	ItemColors  []Color
	SelectedIdx int
	// Label is the floating label — rests in the content area at
	// body-large size when the field is unselected + unfocused, floats up
	// to body-small when selected or focused. Outlined variant cuts a
	// notch in the top border around the floating label.
	Label string
	// Placeholder is shown in the input area while empty + (Label == "" or
	// focused) — same semantic as Input.Placeholder.
	Placeholder string
	// placeholderKey, when set, supersedes Placeholder and is resolved
	// during Measure/Draw. Read it through DisplayPlaceholder().
	placeholderKey messageKey
	labelKey       messageKey
	Variant        SelectVariant
	// Dense collapses the trigger to button height (40 dp base, density-
	// scaled) and removes the top overhang strip. Mirrors Input.Dense
	// so a row of buttons + dense combos lines up flush. Implies Label is
	// ignored (no room for a floating label inside 40 px); set an
	// external Label widget above the combo if you still need a caption.
	Dense    bool
	OnChange func(idx int, value string)

	// States carries the layered container/accent styles. Base is the
	// resting look (Background, Border == accent color, BorderSize ==
	// accent thickness, Foreground == value text color). Focused and
	// Disabled override individual fields. Resolve is called every
	// frame from Draw.
	States StateStyle
	// LabelColor / LabelColorFocused override the floating-label color
	// per state — separate from Foreground since a designed field tints the label
	// differently from body text on focus. Zero A → fallback (Focused
	// → LabelColor, else Foreground). Raw HTML default is zero.
	LabelColor        Color
	LabelColorFocused Color

	// DropdownColors overrides the popup surface palette. Zero fields
	// fall back to the theme-derived default (SurfaceContainer bg,
	// OnSurface text, SecondaryContainer selected row — see openDropdown).
	DropdownColors MenuColors

	// OnOptionChange fires alongside OnChange with the full option (value
	// and label) when SetOptions is in use.
	OnOptionChange func(idx int, opt SelectOption)
	// RenderOption, when set, builds each dropdown row's content (an icon
	// plus two lines, a color swatch...). Rows keep normal behavior: hover,
	// keyboard highlight, click / Enter to pick.
	RenderOption func(opt SelectOption, selected bool) Widget
	// OnOpen / OnClose fire when the dropdown opens and closes.
	OnOpen  func()
	OnClose func()

	options []SelectOption
	// itemsLocale is the LocaleGeneration Items was last resolved under,
	// so a language switch re-resolves LabelKey options on next use.
	itemsLocale uint64
	overlayOwner
	hovering bool
	focused  bool
	isOpen   bool
	list     Widget // the open dropdown's list, which gets forwarded keys

	// Type-ahead state (see typeAhead): the letters typed so far and when
	// the last one arrived, so a pause starts a fresh search.
	typeBuf string
	typeAt  time.Time
}

// SelectOption is one choice of a Select in the rich model (SetOptions).
type SelectOption struct {
	// Value identifies the option to the app; it never changes with the
	// language. Empty means "same as Label".
	Value string
	// Label is shown; LabelKey, when set, resolves it from the catalog
	// with Label as the fallback.
	Label    string
	LabelKey string
	// Icon is drawn before the label in the dropdown.
	Icon VectorSource
	// Group is a heading: consecutive options with the same non-empty Group
	// are listed under one non-selectable heading row (an <optgroup>).
	// GroupKey, when set, resolves Group through the catalog.
	Group    string
	GroupKey string
	Disabled bool
}

// DisplayGroup is the optgroup heading shown to the user.
func (o SelectOption) DisplayGroup() string {
	if o.GroupKey == "" {
		return o.Group
	}
	return TOr(o.GroupKey, o.Group)
}

// DisplayLabel is the option's shown label.
func (o SelectOption) DisplayLabel() string {
	if o.LabelKey == "" {
		return o.Label
	}
	return TranslateOr("", o.LabelKey, o.Label, nil)
}

// ValueOrLabel is Value, or Label when Value is empty.
func (o SelectOption) ValueOrLabel() string {
	if o.Value != "" {
		return o.Value
	}
	return o.Label
}

// SetOptions installs the rich option model. Items and ItemDisabled are
// rebuilt from it (labels resolved now and again on every open / locale
// change), and the selection is kept when its value is still present.
func (cb *Select) SetOptions(opts []SelectOption) {
	prev := cb.SelectedOptionValue()
	cb.options = append([]SelectOption(nil), opts...)
	cb.ItemColors = nil // indexed the previous option list
	cb.syncItemsFromOptions()
	cb.SelectedIdx = cb.indexForValue(prev)
	if cb.isOpen {
		cb.closeDropdown()
	}
	cb.InvalidateLayout()
}

// Options returns the rich option model (nil when only Items is used).
func (cb *Select) Options() []SelectOption { return cb.options }

func (cb *Select) syncItemsFromOptions() {
	if cb.options == nil {
		return
	}
	items := make([]string, len(cb.options))
	disabled := make([]bool, len(cb.options))
	for i, o := range cb.options {
		items[i] = o.DisplayLabel()
		disabled[i] = o.Disabled
	}
	cb.Items, cb.ItemDisabled = items, disabled
	cb.itemsLocale = LocaleGeneration()
}

// refreshItems re-resolves the option labels when the language changed
// since they were last resolved. Everything that reads Items for display
// or matching — the trigger text, SelectedValue, type-ahead, the AX value
// and options — goes through it, so a closed Select follows a locale
// switch too, not only the next time its dropdown opens.
func (cb *Select) refreshItems() {
	if cb.options != nil && cb.itemsLocale != LocaleGeneration() {
		cb.syncItemsFromOptions()
	}
}

// option returns option i in the rich model, or a label-only option built
// from Items.
func (cb *Select) option(i int) SelectOption {
	if i >= 0 && i < len(cb.options) {
		return cb.options[i]
	}
	if i >= 0 && i < len(cb.Items) {
		return SelectOption{Label: cb.Items[i], Disabled: !cb.itemEnabled(i)}
	}
	return SelectOption{}
}

// SelectedOption returns the selected option, ok false when none.
func (cb *Select) SelectedOption() (SelectOption, bool) {
	cb.refreshItems()
	if cb.SelectedIdx < 0 || cb.SelectedIdx >= len(cb.Items) {
		return SelectOption{}, false
	}
	return cb.option(cb.SelectedIdx), true
}

// SelectedOptionValue returns the selected option's value (its label when
// it has none), or "".
func (cb *Select) SelectedOptionValue() string {
	o, ok := cb.SelectedOption()
	if !ok {
		return ""
	}
	return o.ValueOrLabel()
}

// SetValue selects the option whose value (or, failing that, label) is
// value, firing OnChange. Unknown values leave the selection unchanged
// and return false.
func (cb *Select) SetValue(value string) bool {
	idx := cb.indexForValue(value)
	if idx < 0 {
		return false
	}
	cb.selectIndex(idx)
	return true
}

func (cb *Select) indexForValue(value string) int {
	if value == "" {
		return -1
	}
	for i := range cb.Items {
		if cb.option(i).ValueOrLabel() == value {
			return i
		}
	}
	return -1
}

// win is the window the dropdown opens in: the one this select is
// currently attached to, else the constructor's (before mounting).
func (cb *Select) win() *Window {
	if w := cb.Window(); w != nil {
		return w
	}
	return cb.window
}

// SetWindow closes an open dropdown when the select leaves its window, so
// it cannot keep firing OnChange as an overlay from an unmounted control.
func (cb *Select) SetWindow(w *Window) {
	cb.BaseWidget.SetWindow(w)
	cb.ownerSetWindow(w)
}

// IsOpen reports whether the dropdown is showing.
func (cb *Select) IsOpen() bool { return cb.isOpen }

// ConsumesTab commits the field value and closes the open dropdown, then
// lets Tab advance focus. Without it Tab moved into the page while the
// dropdown stayed open and keyboard-dead.
func (cb *Select) ConsumesTab(shift bool) bool {
	if cb.isOpen {
		cb.closeDropdown()
	}
	return false
}

// NewSelect creates a plain, unstyled Select — Outlined variant
// with thin gray border, dark text, transparent container. For a
// designed look with per-state accent tinting and a floating label,
// render a <select> on the htmlcss layer and style it with CSS.
func NewSelect(window *Window, items []string, onChange func(int, string)) *Select {
	cb := &Select{
		BaseWidget:   NewBaseWidget(),
		Items:        items,
		SelectedIdx:  -1,
		Variant:      SelectOutlined,
		OnChange:     onChange,
		overlayOwner: overlayOwner{window: window},
	}
	cb.SetSelf(cb)
	cb.States = defaultSelectStates()
	return cb
}

// defaultSelectStates is the "raw HTML select" style: white container,
// thin gray border, dark text, light-blue focus outline.
func defaultSelectStates() StateStyle {
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
func (cb *Select) Style() *Style { return &cb.States.Base }

func (cb *Select) SetStyle(style Style) {
	old := cb.States.Base
	next := style.Clone()
	cb.States.Base = next
	// Focused/Disabled are full copies; carry the caller's base changes
	// into the variants that still held the old value.
	rebaseStateStyle(cb.States.Focused, old, next)
	rebaseStateStyle(cb.States.Disabled, old, next)
	rebaseStateStyle(cb.States.Hover, old, next)
	rebaseStateStyle(cb.States.Errored, old, next)
	cb.InvalidateLayout()
}

// currentState compiles the field's booleans into a State bitmask.
// isOpen folds into StateFocused (an open popup keeps the trigger
// visually active).
func (cb *Select) currentState() State {
	var s State
	if !cb.Enabled() {
		s |= StateDisabled
	}
	if cb.focused || cb.isOpen {
		s |= StateFocused
	}
	if cb.hovering {
		s |= StateHover
	}
	return s
}

// labelColorFor picks the floating-label color for the current state.
// Uses per-state widget tints when set (a designed field fills them
// with Accent / Error / TextMuted); otherwise falls back to the
// resolved Style.Foreground so the label reads without any extra
// config.
func (cb *Select) labelColorFor(state State, style *Style) Color {
	if state&StateFocused != 0 && cb.LabelColorFocused.A > 0 {
		return cb.LabelColorFocused
	}
	if cb.LabelColor.A > 0 {
		return cb.LabelColor
	}
	return style.Foreground
}

// font returns the combobox's input font.
func (cb *Select) font() Font {
	return cb.States.Base.Font
}

// Select geometry for the tall floating-label form field; the trigger
// inherits the text-field metrics so Input and Select line up, and the
// dropdown reuses the menu row height.
const (
	cbLeadingSpace        float32 = 16 // text-field-leading-space
	cbTrailingSpace       float32 = 12 // trailing-icon-space (smaller than leading for chevron)
	cbTrailingIconSize    float32 = 24 // trailing-icon-size
	cbFieldHeight         float32 = 56 // text-field container-height
	cbTopOverhang         float32 = 8  // same as tfTopOverhang — keeps centers aligned
	cbLabelPopulatedLineH float32 = 16
	cbLabelRestingLineH   float32 = 24
	cbLabelTopSpace       float32 = 8
	cbLabelBottomSpace    float32 = 8
	cbNoLabelTopSpace     float32 = 16
	cbOutlineLabelPad     float32 = 4

	cbMenuItemHeight float32 = 48 // dropdown row height, matching menus

	// Raw HTML <select> geometry (native() path). See widgets/native.go.
	htmlSelLeadPad   float32 = 5  // border(1) + UA left padding(4)
	htmlSelTrailPad  float32 = 23 // right area reserved for the arrow (padR + right border)
	htmlSelArrowW    float32 = 8  // chevron base width
	htmlSelArrowH    float32 = 5  // chevron height
	htmlSelArrowGap  float32 = 8  // gap from the right box edge to the chevron
	htmlSelMenuItemH float32 = 22 // native dropdown row height
)

// native reports whether the trigger uses raw-HTML <select> geometry:
// no floating Label and not Dense (the zero-config NewSelect default).
func (cb *Select) native() bool {
	return cb.Label == "" && !cb.Dense
}

// nativeInsets is the raw-HTML <select>'s horizontal geometry: the text's
// leading inset, the trailing area reserved for the arrow, and the arrow's
// gap from the right edge. UA values unless padding is authored, in which
// case the text starts at border + padding-left and the arrow sits
// border + padding-right in (never tighter than the UA gap).
func (cb *Select) nativeInsets() (lead, trail, arrowGap float32) {
	pad, ok := nativeBoxPadding(&cb.States.Base)
	if !ok {
		return htmlSelLeadPad, htmlSelTrailPad, htmlSelArrowGap
	}
	bw := cb.States.Base.BorderSize
	lead = bw + pad.Left
	arrowGap = bw + pad.Right
	if arrowGap < htmlSelArrowGap {
		arrowGap = htmlSelArrowGap
	}
	// Same text-to-arrow spacing as the UA layout (23 = 8 gap + 8 arrow + 7).
	trail = arrowGap + htmlSelArrowW + (htmlSelTrailPad - htmlSelArrowGap - htmlSelArrowW)
	return lead, trail, arrowGap
}

// SetItems swaps the dropdown's options. Closes any open popup (items
// were captured in the old item-widgets). If the current selection
// lands out of range it resets to -1.
func (cb *Select) SetItems(items []string) bool {
	if sameStrings(cb.Items, items) && cb.options == nil {
		return false
	}
	cb.options = nil
	cb.Items = items
	// Parallel arrays indexed the OLD list; leaving them attached re-labels
	// or re-disables new rows by coincidence.
	cb.ItemDisabled = nil
	cb.ItemColors = nil
	cb.itemsLocale = LocaleGeneration()
	if cb.SelectedIdx >= len(items) {
		cb.SelectedIdx = -1
	}
	if cb.isOpen {
		cb.closeDropdown()
	}
	// Measure sizes the trigger from the widest item, so new items can
	// change the widget's size, not just its pixels.
	cb.InvalidateLayout()
	return true
}

// typeaheadReset is how long the type-ahead buffer survives between
// keystrokes. Matches the ~1s window native selects use before a letter
// starts a new search instead of extending the current one.
const typeaheadReset = time.Second

// typeAhead implements the keyboard behavior every native <select> has and
// qui was missing: typing letters jumps to the matching option.
//
// Rules, in the order a browser applies them:
//
//   - Letters typed in quick succession accumulate into a prefix ("ge" finds
//     Germany, not Greece); a pause of typeaheadReset starts over.
//   - Repeating the SAME letter cycles through the options beginning with it
//     (press "g" three times to walk Georgia → Germany → Greece).
//   - A prefix that matches nothing falls back to searching for the last
//     letter alone, which is what makes a mistyped run recover instead of
//     going dead until the timeout.
//
// Disabled options (and synthesized optgroup headings) are never matched.
// Timing comes from the event's own timestamp, so tests and replayed
// recordings behave like live input.
func (cb *Select) typeAhead(r rune, when time.Time) bool {
	if !unicode.IsPrint(r) || r == ' ' {
		return false // Space toggles the dropdown; control chars are not text
	}
	if cb.isOpen {
		// Type-ahead commits immediately, so an open list's checkmarks and
		// highlight would be stale after the first character.
		cb.closeDropdown()
	}
	if !cb.typeAt.IsZero() && when.Sub(cb.typeAt) > typeaheadReset {
		cb.typeBuf = ""
	}
	cb.typeAt = when
	cb.typeBuf += string(unicode.ToLower(r))
	cb.refreshItems()

	// Same letter repeated → cycle to the NEXT match rather than sticking on
	// the first one.
	if first, repeated := repeatedRune(cb.typeBuf); repeated {
		if idx := cb.matchFrom(string(first), cb.SelectedIdx+1); idx >= 0 {
			cb.selectIndex(idx)
			return true
		}
	}
	if idx := cb.matchFrom(cb.typeBuf, 0); idx >= 0 {
		cb.selectIndex(idx)
		return true
	}
	// Prefix went nowhere — restart the search from this letter alone.
	cb.typeBuf = string(unicode.ToLower(r))
	if idx := cb.matchFrom(cb.typeBuf, 0); idx >= 0 {
		cb.selectIndex(idx)
		return true
	}
	return true // consumed either way: typing in a select is not a page shortcut
}

// matchFrom returns the first enabled option at or after `from` (wrapping)
// whose label starts with prefix, or -1.
func (cb *Select) matchFrom(prefix string, from int) int {
	if prefix == "" || len(cb.Items) == 0 {
		return -1
	}
	if from < 0 {
		from = 0
	}
	for off := 0; off < len(cb.Items); off++ {
		i := (from + off) % len(cb.Items)
		if !cb.itemEnabled(i) {
			continue
		}
		if strings.HasPrefix(strings.ToLower(cb.Items[i]), prefix) {
			return i
		}
	}
	return -1
}

// repeatedRune reports whether s is one character repeated (2+ times), and
// which character it is.
func repeatedRune(s string) (rune, bool) {
	rs := []rune(s)
	if len(rs) < 2 {
		return 0, false
	}
	for _, r := range rs[1:] {
		if r != rs[0] {
			return 0, false
		}
	}
	return rs[0], true
}

// SetItemDisabled marks options as unselectable, parallel to Items. A
// shorter (or nil) slice leaves the remaining options enabled, so callers
// can pass only the prefix they care about. Disabled rows still SHOW in the
// dropdown — greyed and inert, the way a browser renders
// `<option disabled>` and an `<optgroup>` heading — they just cannot be
// picked by click or arrow key. Returns whether anything changed.
func (cb *Select) SetItemDisabled(disabled []bool) bool {
	if sameBools(cb.ItemDisabled, disabled) {
		return false
	}
	cb.ItemDisabled = disabled
	if cb.isOpen {
		cb.closeDropdown() // rows captured the old flags
	}
	cb.Invalidate()
	return true
}

// itemEnabled reports whether option i can be selected.
func (cb *Select) itemEnabled(i int) bool {
	if i < 0 || i >= len(cb.Items) {
		return false
	}
	if i >= len(cb.ItemDisabled) {
		return true
	}
	return !cb.ItemDisabled[i]
}

// nextEnabledIdx walks from idx in direction step (+1 / -1) and returns the
// first selectable option, or -1 when the walk runs off the end. Used by
// keyboard navigation so arrowing over a disabled row skips it instead of
// stalling on it.
func (cb *Select) nextEnabledIdx(idx, step int) int {
	for i := idx; i >= 0 && i < len(cb.Items); i += step {
		if cb.itemEnabled(i) {
			return i
		}
	}
	return -1
}

func sameBools(a, b []bool) bool {
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

func sameStrings(a, b []string) bool {
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

func (cb *Select) Focusable() bool { return cb.Enabled() }
func (cb *Select) CancelInteraction() {
	if cb.isOpen {
		cb.closeDropdown()
	}
}
func (cb *Select) SetFocused(f bool) {
	if cb.focused == f {
		return
	}
	cb.focused = f
	cb.Invalidate()
}

// SelectedValue returns the current selection, or "" if none.
func (cb *Select) SelectedValue() string {
	cb.refreshItems()
	if cb.SelectedIdx < 0 || cb.SelectedIdx >= len(cb.Items) {
		return ""
	}
	return cb.Items[cb.SelectedIdx]
}

// fieldBounds returns the rect of the visually-drawn field inside the
// widget bounds (skipping cbTopOverhang reserved for the outlined
// variant's floating label). Dense mode skips the overhang — the whole
// widget is the painted field, matching button geometry.
func (cb *Select) fieldBounds() Rect {
	b := cb.Bounds()
	if cb.compact() {
		return b
	}
	b.Y += cbTopOverhang
	b.H -= cbTopOverhang
	return b
}

// compact mirrors Input.compact: use the button-height, no-overhang
// geometry for Dense fields and for label-less fields (a raw HTML
// <select>). The tall form-field geometry is reserved for combos with
// a floating Label.
func (cb *Select) compact() bool {
	return cb.Dense || cb.Label == ""
}

// hasLabel reports whether the combo should render its floating label.
// Dense mode suppresses it (no room in 40 dp).
func (cb *Select) hasLabel() bool {
	return cb.Label != "" && !cb.Dense
}

// horizontalPad returns (leading, trailing) padding. Dense mode tightens
// the leading inset (8 vs 16) so the value text doesn't crowd a narrow
// trigger, matching Input.Dense.
func (cb *Select) horizontalPad() (float32, float32) {
	if cb.native() {
		lead, _, gap := cb.nativeInsets()
		return lead, gap
	}
	if cb.compact() {
		return 8, cbTrailingSpace
	}
	return cbLeadingSpace, cbTrailingSpace
}

// isLabelFloating: floats whenever the field is focused, populated, or
// the dropdown is open.
func (cb *Select) isLabelFloating() bool {
	return cb.hasLabel() && (cb.focused || cb.isOpen || cb.SelectedIdx >= 0)
}

// inputBounds returns the rect where the selected-value text is drawn.
// Mirrors Input.inputBounds — filled variant shifts the input below
// the floating label; outlined keeps symmetric padding because its label
// rides above the field outline.
func (cb *Select) inputBounds() Rect {
	b := cb.fieldBounds()
	if cb.native() {
		// Raw HTML <select>: text inset by border+padding, right side
		// reserves htmlSelTrailPad for the chevron.
		lead, trail, _ := cb.nativeInsets()
		top, bottom := htmlControlPadY, htmlControlPadY
		if pad, ok := nativeBoxPadding(&cb.States.Base); ok {
			bw := cb.States.Base.BorderSize
			top, bottom = bw+pad.Top, bw+pad.Bottom
		}
		w := b.W - lead - trail
		if w < 0 {
			w = 0
		}
		h := b.H - top - bottom
		if h < 0 {
			h = 0
		}
		return Rect{X: b.X + lead, Y: b.Y + top, W: w, H: h}
	}
	lead, trail := cb.horizontalPad()
	top := cbNoLabelTopSpace
	bottom := cbNoLabelTopSpace
	if cb.compact() {
		// Same fix as Input.Dense: the 16+16 vertical padding is
		// sized for the 56-dp form field. Inside a 40-dp dense row
		// that leaves only 8 px for the input — body-large ink is
		// ~19 px, so DrawText's extraY clamps to 0 and the baseline
		// lands at the very top of the available slot, reading as
		// "text biased upward" relative to the visible field. 8+8
		// leaves 24 px of input height (matches Input.Dense),
		// putting the body-large face's ink center on the field's
		// vertical center.
		top = 8
		bottom = 8
	} else if cb.hasLabel() && cb.Variant == SelectFilled {
		top = cbLabelTopSpace + cbLabelPopulatedLineH
		bottom = cbLabelBottomSpace
	}
	if total := top + bottom; total >= b.H {
		scale := (b.H * 0.5) / total
		if scale > 1 {
			scale = 1
		}
		top *= scale
		bottom *= scale
	}
	w := b.W - lead - trail - cbTrailingIconSize
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

func (cb *Select) Measure(available Size) Size {
	cb.refreshItems()
	// Width: max of (widest item + chevron + padding) and the default
	// 200 px. Both label + placeholder participate so an empty initial
	// state doesn't shrink below text width.
	var maxTextW float32
	font := cb.font()
	for _, item := range cb.Items {
		w, _ := TextMetrics(item, font)
		if w > maxTextW {
			maxTextW = w
		}
	}
	if cb.Label != "" {
		w, _ := TextMetrics(cb.DisplayLabel(), font)
		if w > maxTextW {
			maxTextW = w
		}
	}
	if ph := cb.DisplayPlaceholder(); ph != "" {
		w, _ := TextMetrics(ph, font)
		if w > maxTextW {
			maxTextW = w
		}
	}
	if cb.native() {
		// Raw HTML <select>: width fits the widest option + arrow area,
		// height is the baseline control box. No 200 px floor.
		lead, trail, _ := cb.nativeInsets()
		width := maxTextW + lead + trail
		if available.W > 0 && width > available.W {
			width = available.W
		}
		height := htmlControlHeight
		if pad, ok := nativeBoxPadding(&cb.States.Base); ok {
			height = nativePaddedHeight(&cb.States.Base, pad)
		}
		return Size{W: width, H: height}
	}
	lead, trail := cb.horizontalPad()
	width := maxTextW + lead + trail + cbTrailingIconSize
	if width < 200 {
		width = 200
	}
	if available.W > 0 && width > available.W {
		width = available.W
	}
	// Density-scaled field + overhang; matches Input so a row of
	// mixed widgets aligns at the same visible-field center. Dense mode
	// drops both — collapses to button height (40 dp base) with no
	// overhang strip so a row of buttons + dense combos lines up flush.
	var height float32
	if cb.compact() {
		height = buttonHeight
	} else {
		height = cbFieldHeight + cbTopOverhang
	}
	return Size{W: width, H: height}
}

func (cb *Select) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := cb.fieldBounds()
	state := cb.currentState()
	style := themedStyle(cb.States.Resolve(state))
	radius := style.Radius
	if radius <= 0 {
		radius = theme.RadiusSmall
	}

	// Variant background. Filled paints its container fill; Outlined
	// stays transparent so its border does the work.
	if cb.Variant != SelectOutlined && style.Background.A > 0 {
		canvas.FillRoundedRect(b, radius, style.Background)
	}

	accent := style.Border
	thickness := style.BorderSize
	if thickness <= 0 && accent.A > 0 {
		thickness = 1
	}
	labelColor := cb.labelColorFor(state, style)
	floating := cb.isLabelFloating()

	// Border / underline geometry per variant.
	switch cb.Variant {
	case SelectOutlined:
		if accent.A > 0 {
			canvas.StrokeRoundedRect(b, radius, accent, thickness)
		}
		if floating && accent.A > 0 {
			// Notch the top border under the floating label so it reads
			// as sitting on the border line, not through it.
			labelW, _ := TextMetrics(cb.DisplayLabel(), ThemeFont(TextBodySmall))
			notchX := b.X + cbLeadingSpace - cbOutlineLabelPad
			notchW := labelW + cbOutlineLabelPad*2
			canvas.FillRect(Rect{X: notchX, Y: b.Y, W: notchW, H: thickness}, theme.Surface)
		}
	default:
		if accent.A > 0 {
			canvas.FillRect(Rect{X: b.X, Y: b.Y + b.H - thickness, W: b.W, H: thickness}, accent)
		}
	}

	// Floating / resting label. Suppressed in Dense mode.
	if cb.hasLabel() {
		lead, trail := cb.horizontalPad()
		var labelRect Rect
		var labelFont Font
		if floating {
			labelFont = ThemeFont(TextBodySmall)
			labelY := b.Y + cbLabelTopSpace
			if cb.Variant == SelectOutlined {
				labelY = b.Y - cbLabelPopulatedLineH/2
			}
			labelRect = Rect{
				X: b.X + lead,
				Y: labelY,
				W: b.W - lead - trail - cbTrailingIconSize,
				H: cbLabelPopulatedLineH,
			}
		} else {
			labelFont = ThemeFont(TextBodyLarge)
			labelRect = Rect{
				X: b.X + lead,
				Y: b.Y + cbNoLabelTopSpace,
				W: b.W - lead - trail - cbTrailingIconSize,
				H: cbLabelRestingLineH,
			}
		}
		canvas.DrawText(cb.DisplayLabel(), labelRect, labelColor, labelFont)
	}

	// Selected value (or placeholder).
	content := cb.inputBounds()
	text := cb.SelectedValue()
	textColor := style.Foreground
	if text == "" {
		if cb.Label == "" || cb.focused {
			text = cb.DisplayPlaceholder()
			textColor = theme.TextMuted
		}
	}
	if text != "" {
		// Hand DrawText the full input box and let its internal
		// extraY math (rect.H - (ascent+descent), split evenly) do
		// the vertical centering. The old helper computed Y from
		// TextMetrics height (ascent+descent+lineGap), which conflicts
		// with DrawText's ascent+descent-only inset by exactly the
		// face's lineGap — text drifted upward by a few px on any
		// font whose lineGap > 0. Mirrors how Input draws.
		//
		// Clip to the content box (like Input) so a long value — e.g.
		// "Times New Roman" in a narrow font picker — is cut at the box
		// edge instead of spilling past the border and under the chevron.
		clipID := canvas.Save()
		canvas.ClipRect(content)
		canvas.DrawText(text, content, textColor, cb.font())
		canvas.RestoreTo(clipID)
	}

	// Trailing chevron — small downward triangle to match md-select's
	// inline 10×5 polygon (points "7 10 12 15 17 10" inside a 24-conceptual
	// slot). The icon container is OnSurfaceVariant; rotates when isOpen.
	cb.drawChevron(canvas, b, accent)
}

// drawChevron paints the select-style downward triangle in a 24×24
// trailing icon slot. Triangle is rasterized as a small filled polygon
// pyramid built from FillRect rows — same "no path rendering" tradeoff
// as TreeView's chevron, but exact spec geometry (10×5 inside 24×24).
func (cb *Select) drawChevron(canvas Canvas, field Rect, color Color) {
	theme := CurrentTheme()
	if cb.native() {
		cb.drawNativeChevron(canvas, field)
		return
	}
	iconColor := theme.TextMuted
	if cb.focused || cb.isOpen {
		iconColor = color
	}
	// Slot: 24×24 centered vertically, anchored to the trailing edge
	// minus trailing-space. Trailing icon padding: 12 px from edge.
	slot := Rect{
		X: field.X + field.W - cbTrailingSpace - cbTrailingIconSize,
		Y: field.Y + (field.H-cbTrailingIconSize)/2,
		W: cbTrailingIconSize,
		H: cbTrailingIconSize,
	}
	// 10×5 triangle inside the slot. md-select polygon "7 10 12 15 17 10"
	// in a 24-grid → base 10 px wide, apex 5 px down. When open, point up.
	const baseW float32 = 10
	const triH float32 = 5
	cx := slot.X + slot.W/2
	cy := slot.Y + slot.H/2 - triH/2 // upper edge of base when pointing down
	for i := 0; i < int(triH); i++ {
		var y float32
		if cb.isOpen {
			// Pointing up: base at bottom, apex at top.
			y = cy + triH - 1 - float32(i)
		} else {
			// Pointing down: base at top, apex at bottom.
			y = cy + float32(i)
		}
		w := baseW * (1 - float32(i)/triH)
		if w < 1 {
			w = 1
		}
		canvas.FillRect(Rect{X: cx - w/2, Y: y, W: w, H: 1}, iconColor)
	}
}

// drawNativeChevron paints the raw HTML <select> arrow: a small solid
// downward triangle (8×5) tucked against the right edge, matching the
// CSS-triangle chevron in widgets/scripts/web/native.html.
func (cb *Select) drawNativeChevron(canvas Canvas, field Rect) {
	baseW := htmlSelArrowW
	triH := htmlSelArrowH
	_, _, gap := cb.nativeInsets()
	cx := field.X + field.W - gap - baseW/2
	cy := field.Y + (field.H-triH)/2
	color := Color{R: 0.2, G: 0.2, B: 0.2, A: 1} // #333
	steps := int(triH)
	if steps < 1 {
		steps = 1
	}
	for i := 0; i < steps; i++ {
		var y float32
		if cb.isOpen {
			y = cy + triH - 1 - float32(i) // point up when open
		} else {
			y = cy + float32(i)
		}
		w := baseW * (1 - float32(i)/triH)
		if w < 1 {
			w = 1
		}
		canvas.FillRect(Rect{X: cx - w/2, Y: y, W: w, H: 1}, color)
	}
}

func (cb *Select) Handle(event Event) bool {
	if !cb.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			cb.hovering = true
			cb.Invalidate()
		case EventMouseLeave:
			cb.hovering = false
			cb.Invalidate()
		case EventMouseDown:
			if e.Button == MouseButtonLeft && cb.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				// Take the press's focus before the popup opens, so the
				// popup restores focus to this select when it closes.
				if win := cb.Window(); win != nil {
					win.FlushPressFocus()
				}
				cb.toggleDropdown()
				return true
			}
		}
	case KeyEvent:
		if !cb.focused || e.Type() != EventKeyDown {
			return false
		}
		if cb.isOpen && cb.list != nil {
			// The open list navigates (arrows, Home / End), picks (Enter /
			// Space) and closes (Escape) — focus stays here meanwhile.
			switch e.Key {
			case KeyUp, KeyDown, KeyHome, KeyEnd, KeyEnter, KeySpace, KeyEscape:
				cb.list.Handle(e)
				return true
			}
		}
		switch e.Key {
		case KeySpace, KeyEnter:
			cb.toggleDropdown()
			return true
		case KeyDown:
			// Arrowing skips disabled options (an <option disabled> or an
			// <optgroup> heading row) rather than stalling on one.
			if next := cb.nextEnabledIdx(cb.SelectedIdx+1, +1); next >= 0 {
				cb.selectIndex(next)
			}
			return true
		case KeyUp:
			if cb.SelectedIdx > 0 {
				if prev := cb.nextEnabledIdx(cb.SelectedIdx-1, -1); prev >= 0 {
					cb.selectIndex(prev)
				}
			}
			return true
		}
	case CharEvent:
		// Type-ahead: letters jump to the matching option, like a native
		// <select>. Modified keystrokes stay available as app shortcuts.
		if !cb.focused || IsCommandMod(e.Mods) {
			return false
		}
		return cb.typeAhead(e.Rune, e.When)
	}
	return false
}

func (cb *Select) toggleDropdown() {
	if cb.isOpen {
		cb.closeDropdown()
	} else {
		cb.openDropdown()
	}
}

// openDropdown shows the option list anchored below the trigger as a
// stock live menu — the same panel, rows, hover and shadow as
// every other live menu (themedMenuStyle), with the current option marked
// by a ✓ in the icon column. The palette follows the current theme's popup tokens; an
// explicit DropdownColors then overrides field-by-field.
func (cb *Select) openDropdown() {
	w := cb.win()
	if w == nil || cb.isOpen {
		return
	}
	cb.syncItemsFromOptions() // pick up a locale change
	st := themedMenuStyle()
	st.ApplyColors(cb.DropdownColors)

	items := make([]*MenuItem, 0, len(cb.Items))
	group := ""
	for i, val := range cb.Items {
		idx := i
		opt := cb.option(idx)
		if g := opt.DisplayGroup(); g != group {
			group = g
			if group != "" {
				items = append(items, &MenuItem{Label: group, Disabled: true})
			}
		}
		item := &MenuItem{
			Label:    val,
			Icon:     opt.Icon,
			Selected: idx == cb.SelectedIdx,
			Disabled: !cb.itemEnabled(idx),
			OnClick: func() {
				cb.selectIndex(idx)
				cb.closeDropdown()
			},
		}
		if idx < len(cb.ItemColors) {
			item.Foreground = cb.ItemColors[idx]
		}
		if cb.RenderOption != nil {
			selected := idx == cb.SelectedIdx
			item.Content = func() Widget { return cb.RenderOption(opt, selected) }
		}
		items = append(items, item)
	}

	popup, list := buildMenuPopupStyled(w, nil, nil, items, st)
	if popup == nil {
		return
	}
	// Clicking another control while the dropdown is open should both
	// dismiss it and act on that control (browser behavior), not require a
	// second click.
	popup.ClickThrough = true
	// Measure against the window width so rows report their intrinsic text
	// width (rather than being constrained — and clipped — to the trigger).
	winW := w.Size().W
	size := list.Measure(Size{W: winW, H: w.Size().H})
	if cb.native() {
		// Raw-HTML <select>: widen the dropdown to fit the widest option
		// (e.g. full font names like "Times New Roman") like a browser does,
		// never narrower than the trigger and never wider than the window.
		if size.W < cb.Bounds().W {
			size.W = cb.Bounds().W
		}
		if winW > 0 && size.W > winW {
			size.W = winW
		}
	} else {
		// Designed select: the menu width matches the anchor field.
		size.W = cb.Bounds().W
	}
	// InteractionBoundsOf, not Bounds(): a Select inside a scroll container
	// has scroll-independent (content-space) bounds, while the popup is a
	// window-level overlay. Re-evaluated on every re-place, so the menu
	// follows the field as the window resizes.
	// The select keeps keyboard focus while its list is open (as in a
	// browser: document.activeElement stays the <select>) and forwards the
	// list's navigation keys from Handle.
	if showAnchoredPopupGap(w, popup, list, func() Rect { return InteractionBoundsOf(cb) }, size, 2, false, false) == nil {
		return
	}
	cb.list = list
	cb.holdPopup(popup, func() {
		cb.isOpen = false
		cb.list = nil
		cb.Invalidate()
		if cb.OnClose != nil {
			cb.OnClose()
		}
	})
	cb.isOpen = true
	cb.Invalidate()
	if cb.OnOpen != nil {
		cb.OnOpen()
	}
}

func (cb *Select) closeDropdown() {
	cb.closePopup()
	cb.isOpen = false
}

// SetText implements qui.TextSink so the agent Type action can pick an
// option in one call. An option's value or catalog key matches first, so
// a script written against values keeps working in every language — `type` the option's label instead of opening the
// popup and clicking a row (which is flaky to drive: the trigger toggles
// on mouse-down and arrow-key nav needs the trigger focused first).
// Matching is forgiving: exact label first, then a whitespace-trimmed
// match (so an indented tree label like "    OAuth" is reachable by
// typing "OAuth"), then a case-insensitive trimmed match. Unknown text is
// a no-op — the selection is left unchanged. Selecting fires OnChange and
// closes any open dropdown, exactly like a click on the row.
func (cb *Select) SetText(value string) {
	idx := cb.indexForLabel(value)
	if idx < 0 {
		return
	}
	if cb.isOpen {
		cb.closeDropdown()
	}
	cb.selectIndex(idx)
}

// indexForLabel resolves a label to its item index using the tiered match
// described on SetText. Returns -1 when nothing matches.
func (cb *Select) indexForLabel(value string) int {
	cb.refreshItems()
	for i := range cb.options {
		if o := cb.options[i]; (o.Value != "" && o.Value == value) || (o.LabelKey != "" && o.LabelKey == value) {
			if i < len(cb.Items) {
				return i
			}
		}
	}
	for i, it := range cb.Items {
		if it == value {
			return i
		}
	}
	trimmed := strings.TrimSpace(value)
	for i, it := range cb.Items {
		if strings.TrimSpace(it) == trimmed {
			return i
		}
	}
	lower := strings.ToLower(trimmed)
	for i, it := range cb.Items {
		if strings.ToLower(strings.TrimSpace(it)) == lower {
			return i
		}
	}
	return -1
}

// selectIndex updates SelectedIdx and fires OnChange.
func (cb *Select) selectIndex(idx int) {
	if idx < -1 || idx >= len(cb.Items) {
		idx = -1
	}
	if idx >= 0 && !cb.itemEnabled(idx) {
		return // disabled options are not selectable by any route
	}
	if idx == cb.SelectedIdx {
		return
	}
	cb.SelectedIdx = idx
	cb.Invalidate()
	if cb.OnChange != nil && idx >= 0 {
		// Hate to surprise callers: plain Items still report the label.
		// With the rich option model the stable Value is the useful second
		// argument (htmlcss/reactive document it as the option value).
		value := cb.Items[idx]
		if opt := cb.option(idx); opt.Value != "" {
			value = opt.Value
		}
		cb.OnChange(idx, value)
	}
	if cb.OnOptionChange != nil && idx >= 0 {
		cb.OnOptionChange(idx, cb.option(idx))
	}
}
