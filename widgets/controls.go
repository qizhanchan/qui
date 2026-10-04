package widgets

import . "github.com/qizhanchan/qui"

// Small form controls: CheckBox, Switch, Progress.
//
// All three share a few design choices with the existing widget set:
//   - BaseWidget for rect/style/enabled plumbing
//   - HitTest overridden to return the concrete widget pointer
//     (BaseWidget.HitTest returns its embedded self, which would be
//     the wrong Widget for event routing)
//   - Hover tracked via Enter/Leave, not MouseMove polling
//   - Rounded backgrounds / AA circles via FillRoundedRect

// -------------------------------------------------------------------
// CheckBox

// CheckBox is a boolean toggle with an optional label. Click or
// press Space (when focused) flips Checked and fires OnChange.
//
// Look: plain by default — thin gray outline when unchecked, blue
// fill + white checkmark when checked, black label. For a designed
// look, render a checkbox on the htmlcss layer and drive its fill from
// CSS (accent-color); this native widget is retinted through its color
// fields.
type CheckBox struct {
	BaseWidget
	Label string
	// labelKey, when set, supersedes Label and is resolved during
	// Measure/Draw. Read it through DisplayLabel().
	labelKey messageKey
	Checked  bool
	OnChange func(checked bool)

	// Color knobs. Zero A on any field falls back to the raw HTML
	// default (see NewCheckBox); assign a theme token for a designed
	// look.
	OutlineColor     Color // unchecked outline
	CheckedFillColor Color // fill when Checked
	CheckMarkColor   Color // checkmark stroke
	LabelColor       Color // label text
	StateLayerColor  Color // hover/focus overlay; A=0 disables

	// Geometry knobs — native <input type=checkbox> defaults. A designed
	// checkbox typically enlarges the touch target: an 18 px box inside
	// a 40 px state layer, 2 px outline.
	//   BoxSize        visual box edge (default htmlCheckSize = 13).
	//   StateLayerSize hover/focus halo + click-target edge; 0 = none
	//                  (native), the box then sits flush at the leading
	//                  edge.
	//   BorderWidth    unchecked outline thickness (default 1).
	//   BoxRadius      box corner radius (default htmlControlRadius = 2).
	BoxSize        float32
	StateLayerSize float32
	BorderWidth    float32
	BoxRadius      float32

	focused      bool
	focusVisible bool
	hovering     bool
}

func NewCheckBox(label string, onChange func(bool)) *CheckBox {
	c := &CheckBox{
		BaseWidget:       NewBaseWidget(),
		Label:            label,
		OnChange:         onChange,
		OutlineColor:     htmlControlBorder,
		CheckedFillColor: htmlAccent,
		CheckMarkColor:   Color{R: 1, G: 1, B: 1, A: 1},
		LabelColor:       htmlControlText,
		BoxSize:          htmlCheckSize,
		BorderWidth:      1,
	}
	// Fix at density-scaled 18-in-40 layout so flex overflow can't
	// collapse the box into a rectangle.
	c.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	return c
}

func (c *CheckBox) Focusable() bool { return c.Enabled() }
func (c *CheckBox) SetFocused(f bool) {
	if c.focused == f {
		return
	}
	c.focused = f
	if !f {
		c.focusVisible = false
	}
	c.Invalidate()
}

// SetFocusVisible — see qui's focusVisibleAware. Only paints the focus
// state-layer when focus came from keyboard (Tab) or a key event landed
// on the checkbox, matching CSS :focus-visible on the web.
func (c *CheckBox) SetFocusVisible(v bool) {
	if c.focusVisible == v {
		return
	}
	c.focusVisible = v
	c.Invalidate()
}

// labelFont returns the checkbox label font: the caller-set Style().Font
// when present (so an inherited/explicit font-size applies), otherwise
// the theme's body text.
func (c *CheckBox) labelFont() Font {
	if f := c.Style().Font; f.Size > 0 {
		return f
	}
	return ThemeFont(TextBody)
}

// slotSize returns the horizontal/vertical area reserved for the control
// itself: the state-layer edge when one is configured, else the box
// edge (native <input type=checkbox>).
func (c *CheckBox) slotSize() (box, slot float32) {
	box = c.BoxSize
	if box <= 0 {
		box = htmlCheckSize
	}
	slot = box
	if c.StateLayerSize > 0 {
		slot = c.StateLayerSize
	}
	return box, slot
}

func (c *CheckBox) Measure(available Size) Size {
	_, slot := c.slotSize()
	h := slot
	w := slot
	if label := c.DisplayLabel(); label != "" {
		textW, textH := TextMetrics(label, c.labelFont())
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

func (c *CheckBox) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := c.Bounds()
	boxSize, slot := c.slotSize()
	// Box is centered within the reserved slot (== box edge when no
	// state layer, so it lands flush at the leading edge like <input>).
	box := Rect{
		X: b.X + (slot-boxSize)/2,
		Y: b.Y + (b.H-boxSize)/2,
		W: boxSize,
		H: boxSize,
	}

	// State layer overlay (opt-in via StateLayerColor + StateLayerSize).
	// Skipped for raw-HTML checkboxes.
	if c.StateLayerColor.A > 0 && c.StateLayerSize > 0 {
		stateRect := Rect{X: b.X, Y: b.Y + (b.H-slot)/2, W: slot, H: slot}
		switch {
		case c.hovering:
			DrawStateLayer(canvas, stateRect, slot/2, c.StateLayerColor, theme.HoverOpacity)
		case c.focusVisible:
			DrawStateLayer(canvas, stateRect, slot/2, c.StateLayerColor, theme.FocusOpacity)
		}
	}

	borderW := c.BorderWidth
	if borderW <= 0 {
		borderW = 1
	}
	radius := c.BoxRadius
	if radius <= 0 {
		radius = htmlControlRadius
	}
	if c.Checked {
		canvas.FillRoundedRect(box, radius, c.CheckedFillColor)
		// Checkmark — three-point polyline within an 18-unit box, same
		// anchors at any scale.
		ssx := boxSize / 18
		ssy := boxSize / 18
		pts := []Point{
			{X: box.X + 3.0*ssx, Y: box.Y + 9.5*ssy},
			{X: box.X + 7.5*ssx, Y: box.Y + 13.5*ssy},
			{X: box.X + 14.5*ssx, Y: box.Y + 6.0*ssy},
		}
		canvas.DrawPolyline(pts, c.CheckMarkColor, 2)
	} else {
		canvas.StrokeRoundedRect(box, radius, c.OutlineColor, borderW)
	}

	if label := c.DisplayLabel(); label != "" {
		labelRect := Rect{X: b.X + slot + 4, Y: b.Y, W: b.W - slot - 4, H: b.H}
		canvas.DrawText(label, labelRect, c.LabelColor, c.labelFont())
	}
}

func (c *CheckBox) Handle(event Event) bool {
	if !c.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			c.hovering = true
			c.Invalidate()
		case EventMouseLeave:
			c.hovering = false
			c.Invalidate()
		case EventMouseDown:
			if c.Bounds().Contains(Point{X: e.X, Y: e.Y}) && e.Button == MouseButtonLeft {
				c.toggle()
				return true
			}
		}
	case KeyEvent:
		if c.focused && e.Type() == EventKeyDown && (e.Key == KeySpace || e.Key == KeyEnter) {
			c.toggle()
			return true
		}
	}
	return false
}

func (c *CheckBox) HitTest(p Point) Widget {
	if c.Bounds().Contains(p) {
		return c
	}
	return nil
}

func (c *CheckBox) toggle() {
	c.Checked = !c.Checked
	c.Invalidate()
	if c.OnChange != nil {
		c.OnChange(c.Checked)
	}
}

// -------------------------------------------------------------------
// Switch

// (Switch lives in switch.go.)

// -------------------------------------------------------------------
// Progress

// Progress visualizes a numeric value between Min and Max.
// Not focusable, no interaction — purely a display widget.
// For indeterminate progress (unknown duration), set Indeterminate
// and the bar fills fully in a neutral color; animated "barber-pole"
// striping can come later once we have pattern fills.
type Progress struct {
	BaseWidget
	Min, Max      float32
	Value         float32
	Indeterminate bool

	// TrackColor / FillColor / Radius drive the look. NewProgress
	// fills them for a plain gray-track / blue-fill look; assign them to
	// follow a theme or a brand palette.
	TrackColor Color
	FillColor  Color
	Radius     float32
}

// NewProgress returns a plain progress bar covering [min, max].
// Light-gray track, blue fill. Set TrackColor / FillColor (e.g. from
// CurrentTheme) for a themed look.
func NewProgress(min, max float32) *Progress {
	return &Progress{
		BaseWidget: NewBaseWidget(),
		Min:        min,
		Max:        max,
		TrackColor: Color{R: 0.88, G: 0.88, B: 0.88, A: 1},
		FillColor:  Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		Radius:     4,
	}
}

func (p *Progress) Measure(available Size) Size {
	w := float32(200)
	if available.W > 0 && w > available.W {
		w = available.W
	}
	return Size{W: w, H: 8}
}

func (p *Progress) Draw(canvas Canvas) {
	b := p.Bounds()
	radius := p.Radius
	if p.TrackColor.A > 0 {
		canvas.FillRoundedRect(b, radius, p.TrackColor)
	}
	progress := p.fraction()
	if progress <= 0 {
		return
	}
	fillW := b.W * progress
	fill := Rect{X: b.X, Y: b.Y, W: fillW, H: b.H}
	r := radius
	if fillW < 2*r {
		r = fillW / 2
	}
	canvas.FillRoundedRect(fill, r, p.FillColor)
}

func (p *Progress) HitTest(pt Point) Widget {
	if p.Bounds().Contains(pt) {
		return p
	}
	return nil
}

// fraction returns the clamped [0,1] progress.
func (p *Progress) fraction() float32 {
	if p.Indeterminate {
		return 1
	}
	span := p.Max - p.Min
	if span <= 0 {
		return 0
	}
	f := (p.Value - p.Min) / span
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}
