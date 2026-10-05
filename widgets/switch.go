package widgets

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// Switch is a toggle: a 52×32 pill-shaped track with a circular handle
// that slides between off (left) and on (right). Tap / Space / Enter
// flips On; the handle, track, and state layer each have their own
// on/off color field, so the whole cascade is caller-controlled.
//
// Geometry:
//   - track: 52×32, fully rounded
//   - track outline: 2 px, only when off — the on state fills the whole
//     track and the outline disappears
//   - handle: 16×16 off, 24×24 on, 28×28 pressed (either state)
//   - state layer: 40×40 around the handle in hover/focus/pressed
//
// The trailing Label is rendered in body text to the right of the track
// with a 16 px gap — the settings-list convention (label beside the
// switch, not below). Pass "" to render the bare control.
//
// A handle carrying a small check / cross glyph is not implemented —
// add an Icon field later if a caller needs it.
type Switch struct {
	BaseWidget
	Label string
	// labelKey, when set, supersedes Label and is resolved during
	// Measure/Draw. Read it through DisplayLabel().
	labelKey messageKey
	On       bool
	OnChange func(on bool)

	// Color knobs. Zero-configured NewSwitch fills them for a plain
	// look (gray off / blue on). On the htmlcss layer CSS drives the
	// on-track via accent-color; set these directly otherwise.
	OnTrackColor        Color // track fill when On=true
	OffTrackColor       Color // track fill when On=false
	OffTrackBorderColor Color // track outline when On=false
	OnHandleColor       Color // handle when On=true (idle)
	OffHandleColor      Color // handle when On=false (idle)
	// Handle color when the pointer is over the switch or it's
	// focused/pressed. Zero A → keep the idle color.
	OnHandleColorActive  Color
	OffHandleColorActive Color
	// StateLayer tints — differ by on/off. Zero A disables that side's
	// halo.
	OnStateLayerColor  Color
	OffStateLayerColor Color
	LabelColor         Color

	hovering     bool
	focused      bool
	focusVisible bool
	pressed      bool
	disabled     bool

	hoverTrans Transition
}

// Switch geometry constants.
const (
	switchTrackW       float32 = 52
	switchTrackH       float32 = 32
	switchTrackBorderW float32 = 2 // unselected track outline thickness
	switchHandleOff    float32 = 16
	switchHandleOn     float32 = 24
	switchHandlePress  float32 = 28
	switchStateSize    float32 = 40 // state-layer width/height
	switchLabelGap     float32 = 16 // gap between track and trailing label
)

// NewSwitch constructs a plain Switch with an optional trailing label.
// Look: gray track when off, blue track + white handle when on — a
// utilitarian toggle. For a designed look, render <input type=checkbox
// switch> on the htmlcss layer, or set the color fields directly.
func NewSwitch(label string, onChange func(bool)) *Switch {
	s := &Switch{
		BaseWidget:          NewBaseWidget(),
		Label:               label,
		OnChange:            onChange,
		OnTrackColor:        Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		OffTrackColor:       Color{R: 0.90, G: 0.90, B: 0.90, A: 1},
		OffTrackBorderColor: Color{R: 0.60, G: 0.60, B: 0.60, A: 1},
		OnHandleColor:       Color{R: 1, G: 1, B: 1, A: 1},
		OffHandleColor:      Color{R: 0.60, G: 0.60, B: 0.60, A: 1},
		LabelColor:          Color{R: 0.10, G: 0.10, B: 0.10, A: 1},
	}
	s.hoverTrans.Duration = toDuration(CurrentTheme().TransitionShort)
	// Switch is fixed at its pill size — flex shrink would deform it.
	s.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	return s
}

func (s *Switch) Focusable() bool { return s.Enabled() && !s.disabled }
func (s *Switch) CancelInteraction() {
	if s.pressed {
		s.pressed = false
		s.Invalidate()
	}
}
func (s *Switch) SetFocused(f bool) {
	if s.focused == f {
		return
	}
	s.focused = f
	if !f {
		s.focusVisible = false
	}
	s.Invalidate()
}

// SetFocusVisible — see qui's focusVisibleAware. Window calls this with
// true when focus was acquired via Tab/keyboard, false on mouse click.
// Only the visible variant paints the focus state-layer halo, matching
// CSS :focus-visible behavior on the web.
func (s *Switch) SetFocusVisible(v bool) {
	if s.focusVisible == v {
		return
	}
	s.focusVisible = v
	s.Invalidate()
}

func (s *Switch) SetEnabled(enabled bool) {
	s.BaseWidget.SetEnabled(enabled)
	s.disabled = !enabled
	s.Invalidate()
}

// SetOn programmatically flips the switch with OnChange. Returns true
// if the state actually changed.
func (s *Switch) SetOn(on bool) bool {
	if s.On == on {
		return false
	}
	s.On = on
	s.Invalidate()
	if s.OnChange != nil {
		s.OnChange(on)
	}
	return true
}

// trackOverhang returns the per-side state-layer overhang past the
// track edges — used in Measure + Draw to keep the geometry consistent.
// Returned value is in logical px at scale 1.
func switchOverhang() float32 {
	return (switchStateSize - switchHandleOff) / 2 // 12 px
}

// Measure returns the minimum bounding box that contains the track
// + the worst-case state-layer overhang on each horizontal side, plus
// a trailing label area when Label is set. Height takes the
// state-layer (40) so a 40-tall touch target is preserved.
func (s *Switch) Measure(available Size) Size {
	w := switchTrackW + 2*switchOverhang()
	h := switchStateSize
	if label := s.DisplayLabel(); label != "" {
		labelW, labelH := TextMetrics(label, ThemeFont(TextBody))
		w += switchLabelGap + labelW
		if labelH > h {
			h = labelH
		}
	}
	if available.W > 0 && w > available.W {
		w = available.W
	}
	return Size{W: w, H: h}
}

// trackBounds is the visible track rect (52×32 at scale=1) inside the
// widget bounds. X is offset by switchOverhang so the state-layer never
// crosses the widget's left edge (mirrors slider/icon-button conventions).
func (s *Switch) trackBounds() Rect {
	b := s.Bounds()
	return Rect{
		X: b.X + switchOverhang(),
		Y: b.Y + (b.H-switchTrackH)/2,
		W: switchTrackW,
		H: switchTrackH,
	}
}

func (s *Switch) Draw(canvas Canvas) {
	theme := CurrentTheme()
	track := s.trackBounds()
	trackH := switchTrackH
	trackRadius := trackH / 2
	borderW := switchTrackBorderW
	stateSize := switchStateSize

	trackFill, trackBorder, handleColor, layerColor := s.resolveColors()
	trackFill, trackBorder, handleColor = themed(trackFill), themed(trackBorder), themed(handleColor)

	if trackFill.A > 0 {
		canvas.FillRoundedRect(track, trackRadius, trackFill)
	}
	// Unselected track gets a 2 px outline (scale=1). Selected fills the
	// whole pill with Primary so the outline disappears.
	if trackBorder.A > 0 {
		canvas.StrokeRoundedRect(track, trackRadius, trackBorder, borderW)
	}

	// Handle size. Pressed wins (28); else 24 selected / 16 unselected.
	handleSize := switchHandleOff
	if s.On {
		handleSize = switchHandleOn
	}
	if s.pressed && !s.disabled {
		handleSize = switchHandlePress
	}

	// Handle center X. The handle sits so its centerline is
	// `handle-w/2 + track-outline-width` from the matching track edge —
	// off-handle hugs the left, on-handle hugs the right.
	insetFromEdge := borderW + handleSize/2
	var handleCx float32
	if s.On {
		handleCx = track.X + track.W - insetFromEdge
	} else {
		handleCx = track.X + insetFromEdge
	}
	handleCy := track.Y + track.H/2

	// State-layer + ripple at the handle position.
	if !s.disabled {
		stateRect := Rect{
			X: handleCx - stateSize/2,
			Y: handleCy - stateSize/2,
			W: stateSize,
			H: stateSize,
		}
		switch {
		case s.pressed:
			DrawStateLayer(canvas, stateRect, stateSize/2, layerColor, theme.PressedOpacity)
		case s.hovering:
			t := s.hoverTrans.Value(time.Now())
			DrawStateLayer(canvas, stateRect, stateSize/2, layerColor, theme.HoverOpacity*t)
		case s.focusVisible:
			DrawStateLayer(canvas, stateRect, stateSize/2, layerColor, theme.FocusOpacity)
		}
	}

	// Handle.
	handleRect := Rect{
		X: handleCx - handleSize/2,
		Y: handleCy - handleSize/2,
		W: handleSize,
		H: handleSize,
	}
	canvas.FillRoundedRect(handleRect, handleSize/2, handleColor)

	// Trailing label.
	if label := s.DisplayLabel(); label != "" {
		b := s.Bounds()
		labelX := track.X + track.W + switchLabelGap
		labelRect := Rect{
			X: labelX,
			Y: b.Y,
			W: b.X + b.W - labelX,
			H: b.H,
		}
		labelColor := themed(s.LabelColor)
		if s.disabled {
			labelColor = mixAlpha(labelColor, 0.38)
		}
		canvas.DrawText(label, labelRect, labelColor, ThemeFont(TextBody))
	}
}

// resolveColors picks track-fill / track-border / handle / state-layer
// colors from the widget's per-state Color fields. Disabled bakes in
// a 38% alpha treatment on top of the base colors — that reads as
// disabled out of any color palette, so it stays hardcoded here rather
// than surfaced as another set of fields.
func (s *Switch) resolveColors() (trackFill, trackBorder, handle, layer Color) {
	if s.disabled {
		if s.On {
			return mixAlpha(s.OnTrackColor, 0.4), ColorTransparent, mixAlpha(s.OnHandleColor, 0.6), ColorTransparent
		}
		return mixAlpha(s.OffTrackColor, 0.4), mixAlpha(s.OffTrackBorderColor, 0.4), mixAlpha(s.OffHandleColor, 0.6), ColorTransparent
	}
	if s.On {
		h := s.OnHandleColor
		if (s.pressed || s.focusVisible || s.hovering) && s.OnHandleColorActive.A > 0 {
			h = s.OnHandleColorActive
		}
		return s.OnTrackColor, ColorTransparent, h, s.OnStateLayerColor
	}
	h := s.OffHandleColor
	if (s.pressed || s.focusVisible || s.hovering) && s.OffHandleColorActive.A > 0 {
		h = s.OffHandleColorActive
	}
	return s.OffTrackColor, s.OffTrackBorderColor, h, s.OffStateLayerColor
}

func (s *Switch) Tick(now time.Time) Rect {
	if s.hoverTrans.Active(now) {
		return s.Bounds()
	}
	return Rect{}
}

func (s *Switch) Handle(event Event) bool {
	if !s.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventMouseEnter:
			now := time.Now()
			s.hoverTrans.Begin(s.hoverTrans.Value(now), 1, now)
			s.hovering = true
			s.Invalidate()
		case EventMouseLeave:
			now := time.Now()
			s.hoverTrans.Begin(s.hoverTrans.Value(now), 0, now)
			s.hovering = false
			s.Invalidate()
		case EventMouseDown:
			if e.Button == MouseButtonLeft && s.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				s.pressed = true
				s.Invalidate()
				return true
			}
		case EventMouseUp:
			if s.pressed && e.Button == MouseButtonLeft {
				s.pressed = false
				s.Invalidate()
				if s.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
					s.SetOn(!s.On)
				}
				return true
			}
		}
	case KeyEvent:
		if s.focused && e.Type() == EventKeyDown && (e.Key == KeySpace || e.Key == KeyEnter) {
			s.SetOn(!s.On)
			return true
		}
	}
	return false
}

func (s *Switch) HitTest(p Point) Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

// handleCenter returns the current handle center (used as the ripple
// origin so press feedback orbits the handle, not the click point).
func (s *Switch) handleCenter() Point {
	track := s.trackBounds()
	handleSize := switchHandleOff
	if s.On {
		handleSize = switchHandleOn
	}
	insetFromEdge := switchTrackBorderW + handleSize/2
	var cx float32
	if s.On {
		cx = track.X + track.W - insetFromEdge
	} else {
		cx = track.X + insetFromEdge
	}
	return Point{X: cx, Y: track.Y + track.H/2}
}
