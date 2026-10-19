package widgets

import . "github.com/qizhanchan/qui"

// RadioGroup coordinates a set of RadioButton widgets so that at most
// one is Checked at a time. Buttons register themselves with the group
// at construction time; the group fires OnChange whenever the
// selection changes.
//
// Deliberately kept separate from Container — radio groups are a
// semantic coupling (mutex over state), not a layout coupling. You
// can put radio buttons from the same group in different containers
// and they still mutex correctly.
type RadioGroup struct {
	buttons  []*RadioButton
	selected *RadioButton
	OnChange func(selected *RadioButton)
}

func NewRadioGroup() *RadioGroup {
	return &RadioGroup{}
}

// Selected returns the currently checked RadioButton, or nil.
func (g *RadioGroup) Selected() *RadioButton { return g.selected }

// Select checks the given button and unchecks any previously selected
// one, firing OnChange if the selection changed. Passing nil clears
// the selection.
//
// Robustness: we don't just clear `g.selected`. We also walk every
// registered button and clear stale Checked=true entries — covers the
// case where a caller pre-selected a button by setting `rb.Checked = true`
// directly (bypassing this method, so `g.selected` was never updated).
// Without the sweep, the first Select() call after such manual init
// leaves the old button checked alongside the new one.
func (g *RadioGroup) Select(rb *RadioButton) {
	if g.selected == rb && (rb == nil || rb.Checked) {
		return
	}
	for _, b := range g.buttons {
		if b == rb {
			continue
		}
		if b.Checked {
			b.Checked = false
			b.Invalidate()
		}
	}
	g.selected = rb
	if rb != nil {
		rb.Checked = true
		rb.Invalidate()
	}
	if g.OnChange != nil {
		g.OnChange(rb)
	}
}

// add registers a RadioButton with this group. Called by NewRadioButton.
func (g *RadioGroup) add(rb *RadioButton) {
	g.buttons = append(g.buttons, rb)
}

// RadioButton is a single selectable option in a RadioGroup. Click or
// press Space when focused to select; the group ensures only one
// button in the group carries Checked=true at a time.
type RadioButton struct {
	BaseWidget
	labelKey messageKey
	Label    string
	Checked  bool
	// OnKeyActivate, when set, runs after a keyboard activation (Space)
	// changed the control — a browser's synthetic click. Optional.
	OnKeyActivate func()
	spaceArmed    bool // Space went down on this control; its keyup acts

	// Color knobs — same convention as CheckBox. Zero A on any field
	// falls back to the raw HTML default (see NewRadioButton).
	RingColor       Color // outer ring (unselected + selected)
	SelectedColor   Color // ring color + inner dot color when Checked
	LabelColor      Color // label text
	StateLayerColor Color // hover/focus overlay; A=0 disables

	// Geometry knobs — native <input type=radio> defaults. A designed
	// radio typically runs a 20 px ring inside a 40 px state layer. See
	// CheckBox for the slot/state-layer semantics.
	//   RingSize       outer circle diameter (default htmlCheckSize = 13).
	//   StateLayerSize hover/focus halo + click target; 0 = none (native).
	//   RingWidth      ring stroke thickness (default 1; designed = 2).
	RingSize       float32
	StateLayerSize float32
	RingWidth      float32

	group        *RadioGroup
	focused      bool
	focusVisible bool
	hovering     bool
}

// SetLabel replaces the literal label and re-measures.
func (r *RadioButton) SetLabel(label string) {
	if r.Label == label {
		return
	}
	r.Label = label
	r.InvalidateLayout()
}

// NewRadioButton constructs a plain radio button belonging to the
// given group. The group must be non-nil — grouping is what
// distinguishes radio buttons from checkboxes. Look is HTML-plain
// (gray ring, blue selected dot). For a designed look, render a radio
// on the htmlcss layer and drive it from CSS (accent-color).
func NewRadioButton(group *RadioGroup, label string) *RadioButton {
	if group == nil {
		panic("qui: RadioButton requires a non-nil RadioGroup")
	}
	r := &RadioButton{
		BaseWidget:    NewBaseWidget(),
		Label:         label,
		group:         group,
		RingColor:     htmlControlBorder,
		SelectedColor: htmlAccent,
		LabelColor:    htmlControlText,
		RingSize:      htmlCheckSize,
		RingWidth:     1,
	}
	r.SetSelf(r)
	r.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	group.add(r)
	return r
}

func (r *RadioButton) Focusable() bool { return r.Enabled() }

// PaintBounds includes the focus ring / state-layer halo.
func (r *RadioButton) PaintBounds() Rect { return expandRect(r.Bounds(), 8) }

// CancelInteraction drops transient pointer/keyboard state.
func (r *RadioButton) CancelInteraction() {
	r.spaceArmed = false
	r.hovering = false
	r.focusVisible = false
}

// labelFont mirrors CheckBox: an explicit Style().Font wins, else body.
func (r *RadioButton) labelFont() Font {
	if f := r.Style().Font; f.Size > 0 {
		return f
	}
	return ThemeFont(TextBody)
}
func (r *RadioButton) SetFocused(f bool) {
	if r.focused == f {
		return
	}
	r.focused = f
	if !f {
		r.focusVisible = false
	}
	r.Invalidate()
}

// SetFocusVisible — see qui's focusVisibleAware. Only paints the focus
// state-layer when focus came from keyboard (Tab) or a key event landed
// on the button, matching CSS :focus-visible on the web.
func (r *RadioButton) SetFocusVisible(v bool) {
	if r.focusVisible == v {
		return
	}
	r.focusVisible = v
	r.Invalidate()
}

// slotSize mirrors CheckBox.slotSize: (ring diameter, reserved slot).
func (r *RadioButton) slotSize() (ring, slot float32) {
	ring = r.RingSize
	if ring <= 0 {
		ring = htmlCheckSize
	}
	slot = ring
	if r.StateLayerSize > 0 {
		slot = r.StateLayerSize
	}
	return ring, slot
}

func (r *RadioButton) Measure(available Size) Size {
	_, slot := r.slotSize()
	h := slot
	w := slot
	if r.Label != "" || r.labelKey.key != "" {
		textW, textH := TextMetrics(r.DisplayLabel(), r.labelFont())
		if textH > h {
			h = textH
		}
		w += 4 + textW
	}
	if available.W > 0 && w > available.W {
		w = available.W
	}
	return Size{W: w, H: h}
}

func (r *RadioButton) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := r.Bounds()
	dotSize, slot := r.slotSize()
	outer := Rect{
		X: b.X + (slot-dotSize)/2,
		Y: b.Y + (b.H-dotSize)/2,
		W: dotSize,
		H: dotSize,
	}

	// State-layer overlay (opt-in via StateLayerColor + StateLayerSize).
	hasLayer := themed(r.StateLayerColor).A > 0 && r.StateLayerSize > 0
	if r.Enabled() && hasLayer {
		stateRect := Rect{X: b.X, Y: b.Y + (b.H-slot)/2, W: slot, H: slot}
		switch {
		case r.hovering:
			DrawStateLayer(canvas, stateRect, slot/2, themed(r.StateLayerColor), theme.HoverOpacity)
		case r.focusVisible:
			DrawStateLayer(canvas, stateRect, slot/2, themed(r.StateLayerColor), theme.FocusOpacity)
		}
	} else if r.focusVisible {
		ring := Rect{X: outer.X - 3, Y: outer.Y - 3, W: outer.W + 6, H: outer.H + 6}
		canvas.StrokeRoundedRect(ring, ring.W/2, theme.Accent, 2)
	}

	disabledAlpha := float32(1)
	if !r.Enabled() {
		disabledAlpha = 0.4
	}
	dim := func(col Color) Color {
		col = themed(col)
		if disabledAlpha == 1 {
			return col
		}
		return mixAlpha(col, disabledAlpha)
	}
	ringW := r.RingWidth
	if ringW <= 0 {
		ringW = 1
	}
	// Outer ring — RingColor unless selected, then SelectedColor.
	ringColor := dim(r.RingColor)
	if r.Checked {
		ringColor = dim(r.SelectedColor)
	}
	canvas.StrokeRoundedRect(outer, dotSize/2, ringColor, ringW)
	if r.Checked {
		// Inner dot ≈ half the ring diameter (20→10; native 13→~6.5).
		dotR := dotSize / 4
		inner := Rect{X: outer.X + outer.W/2 - dotR, Y: outer.Y + outer.H/2 - dotR, W: 2 * dotR, H: 2 * dotR}
		canvas.FillRoundedRect(inner, dotR, dim(r.SelectedColor))
	}

	if r.Label != "" || r.labelKey.key != "" {
		labelRect := Rect{X: b.X + slot + 4, Y: b.Y, W: b.W - slot - 4, H: b.H}
		canvas.DrawText(r.DisplayLabel(), labelRect, dim(r.LabelColor), r.labelFont())
	}
}

func (r *RadioButton) Handle(event Event) bool {
	if !r.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			r.hovering = true
			r.Invalidate()
		case EventMouseLeave:
			r.hovering = false
			r.Invalidate()
		case EventMouseDown:
			if r.Bounds().Contains(Point{X: e.X, Y: e.Y}) && e.Button == MouseButtonLeft {
				r.group.Select(r)
				return true
			}
		}
	case KeyEvent:
		if r.focused && e.Type() == EventKeyDown {
			switch e.Key {
			case KeyLeft, KeyUp:
				r.moveSelection(-1)
				return true
			case KeyRight, KeyDown:
				r.moveSelection(1)
				return true
			}
		}
		if r.focused && keyActivate(e, &r.spaceArmed) {
			r.group.Select(r)
			if r.OnKeyActivate != nil {
				r.OnKeyActivate()
			}
			return true
		}
		if r.focused && e.Key == KeySpace {
			return true // the keydown that armed it
		}
	}
	return false
}

// moveSelection implements ARIA radio-group arrow navigation: the checked
// item moves with the arrows and takes focus (a roving tab stop).
func (r *RadioButton) moveSelection(step int) {
	g := r.group
	if g == nil || len(g.buttons) == 0 {
		return
	}
	idx := -1
	for i, b := range g.buttons {
		if b == r {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	n := len(g.buttons)
	for k := 1; k <= n; k++ {
		j := (idx + step*k%n + n) % n
		b := g.buttons[j]
		if b == nil || !b.Enabled() {
			continue
		}
		g.Select(b)
		if w := r.Window(); w != nil {
			w.SetFocus(b)
		}
		return
	}
}
