package widgets

import (
	"time"

	. "github.com/qizhanchan/qui"
)

// Slider is the continuous-value control: a draggable handle
// slides along a horizontal track, with the portion to the left of the
// handle rendered as the "active" track and the portion to the right
// as the "inactive" track. Arrow keys step by Step when focused.
//
// Geometry:
//   - Widget block-size 40 px (= state-layer size)
//   - Track height 4 px, fully rounded; active = accent,
//     inactive = a raised surface tone
//   - Handle 20×20 round, accent-filled, level-1 elevation
//   - 40×40 state-layer centered on the handle in hover/focus/pressed
//   - Track is inset 18 px on each side (= state-layer-size/2 -
//     with-tick-marks-container-size); the handle center moves between
//     [b.X+18, b.X+b.W-18]
//
// The default minimum inline size is 200 px.
type Slider struct {
	BaseWidget
	Min, Max float32
	Value    float32
	// Step is the arrow-key increment. Zero means (Max-Min)/100.
	Step     float32
	OnChange func(value float32)

	// Color knobs. Raw HTML defaults on NewSlider; on the htmlcss layer
	// CSS drives the active track via accent-color.
	// Zero A on disabled fields → derive from base by alpha reduction.
	ActiveTrackColor   Color
	InactiveTrackColor Color
	HandleColor        Color
	StateLayerColor    Color // hover/focus/dragging halo; A=0 disables
	// Enable a level-1 shadow behind the handle. Off by default.
	HandleElevation int

	focused      bool
	focusVisible bool
	hovering     bool
	dragging     bool

	hoverTrans Transition
}

// Slider geometry constants.
const (
	sliderHandleSize float32 = 20 // handle-width / handle-height
	sliderStateSize  float32 = 40 // state-layer-size (also block-size)
	sliderTrackH     float32 = 4  // active-track-height / inactive-track-height
	sliderTrackPad   float32 = 18 // (state-layer-size/2) - with-tick-marks-container-size
)

func NewSlider(min, max, initial float32, onChange func(float32)) *Slider {
	if max < min {
		max = min
	}
	if initial < min {
		initial = min
	}
	if initial > max {
		initial = max
	}
	s := &Slider{
		BaseWidget:         NewBaseWidget(),
		Min:                min,
		Max:                max,
		Value:              initial,
		OnChange:           onChange,
		ActiveTrackColor:   Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
		InactiveTrackColor: Color{R: 0.88, G: 0.88, B: 0.88, A: 1},
		HandleColor:        Color{R: 0.30, G: 0.55, B: 0.85, A: 1},
	}
	s.hoverTrans.Duration = toDuration(CurrentTheme().TransitionShort)
	// Block-size stays fixed at 40 px so flex overflow doesn't collapse
	// the handle geometry vertically.
	s.UpdateFlexItem(func(item *FlexItem) { item.NoShrink = true })
	return s
}

func (s *Slider) Focusable() bool { return s.Enabled() }
func (s *Slider) CancelInteraction() {
	if s.dragging {
		s.dragging = false
		s.Invalidate()
	}
}
func (s *Slider) SetFocused(f bool) {
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
// Only the visible variant paints the focus state-layer halo around
// the handle, matching CSS :focus-visible behavior on the web.
func (s *Slider) SetFocusVisible(v bool) {
	if s.focusVisible == v {
		return
	}
	s.focusVisible = v
	s.Invalidate()
}

// Measure: 200×40 by default (min inline size + state-layer size).
func (s *Slider) Measure(available Size) Size {
	w := float32(200)
	if available.W > 0 && w > available.W {
		w = available.W
	}
	return Size{W: w, H: sliderStateSize}
}

func (s *Slider) Draw(canvas Canvas) {
	theme := CurrentTheme()
	b := s.Bounds()
	disabled := !s.Enabled()

	trackPad := sliderTrackPad
	trackH := sliderTrackH
	handleSize := sliderHandleSize
	stateSize := sliderStateSize

	// Track geometry: visible track inset trackPad on each side. Handle
	// center moves linearly along [trackX, trackX+trackW].
	trackX := b.X + trackPad
	trackW := b.W - 2*trackPad
	if trackW < 0 {
		trackW = 0
	}
	trackY := b.Y + (b.H-trackH)/2

	frac := s.fraction()
	handleCX := trackX + trackW*frac
	handleCY := b.Y + b.H/2

	// Inactive track.
	inactiveColor := themed(s.InactiveTrackColor)
	if disabled {
		inactiveColor = mixAlpha(inactiveColor, 0.5)
	}
	canvas.FillRoundedRect(
		Rect{X: trackX, Y: trackY, W: trackW, H: trackH},
		trackH/2, inactiveColor,
	)

	// Active track (left-of-handle).
	activeW := handleCX - trackX
	if activeW > 0 {
		activeColor := themed(s.ActiveTrackColor)
		if disabled {
			activeColor = mixAlpha(activeColor, 0.5)
		}
		r := trackH / 2
		if activeW < 2*r {
			r = activeW / 2
		}
		canvas.FillRoundedRect(
			Rect{X: trackX, Y: trackY, W: activeW, H: trackH},
			r, activeColor,
		)
	}

	// State-layer overlay (opt-in via StateLayerColor).
	if !disabled && themed(s.StateLayerColor).A > 0 {
		stateRect := Rect{
			X: handleCX - stateSize/2,
			Y: handleCY - stateSize/2,
			W: stateSize,
			H: stateSize,
		}
		switch {
		case s.dragging:
			DrawStateLayer(canvas, stateRect, stateSize/2, themed(s.StateLayerColor), theme.PressedOpacity)
		case s.hovering:
			t := s.hoverTrans.Value(time.Now())
			DrawStateLayer(canvas, stateRect, stateSize/2, themed(s.StateLayerColor), theme.HoverOpacity*t)
		case s.focusVisible:
			DrawStateLayer(canvas, stateRect, stateSize/2, themed(s.StateLayerColor), theme.FocusOpacity)
		}
	}

	// Handle.
	handleRect := Rect{
		X: handleCX - handleSize/2,
		Y: handleCY - handleSize/2,
		W: handleSize,
		H: handleSize,
	}
	handleColor := themed(s.HandleColor)
	handleRadius := handleSize / 2
	if disabled {
		handleColor = mixAlpha(handleColor, 0.5)
	} else if s.HandleElevation > 0 {
		DrawElevation(canvas, handleRect, handleRadius, s.HandleElevation)
	}
	canvas.FillRoundedRect(handleRect, handleRadius, handleColor)
}

func (s *Slider) Tick(now time.Time) Rect {
	if s.hoverTrans.Active(now) {
		return PaintBoundsInWindow(s)
	}
	return Rect{}
}

func (s *Slider) Handle(event Event) bool {
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
			if s.Bounds().Contains(Point{X: e.X, Y: e.Y}) && e.Button == MouseButtonLeft {
				s.dragging = true
				s.setValueFromX(e.X)
				s.Invalidate()
				return true
			}
		case EventMouseMove:
			// Only relevant when we've captured the mouse via MouseDown.
			// Window's mouse capture sends this event to us even when the
			// cursor has left the widget bounds, enabling drag past edges.
			if s.dragging {
				s.setValueFromX(e.X)
				return true
			}
		case EventMouseUp:
			if s.dragging {
				s.dragging = false
				s.Invalidate()
				return true
			}
		}
	case KeyEvent:
		if s.focused && e.Type() == EventKeyDown {
			step := s.step()
			switch e.Key {
			case KeyLeft, KeyDown:
				s.setValue(s.Value - step)
				return true
			case KeyRight, KeyUp:
				s.setValue(s.Value + step)
				return true
			}
		}
	}
	return false
}

func (s *Slider) HitTest(p Point) Widget {
	if s.Bounds().Contains(p) {
		return s
	}
	return nil
}

// handleCenter returns the current pixel position of the handle's center.
// Used to anchor ripples to the handle rather than the click point so
// the press feedback orbits the visual handle.
func (s *Slider) handleCenter() Point {
	b := s.Bounds()
	pad := sliderTrackPad
	trackX := b.X + pad
	trackW := b.W - 2*pad
	if trackW < 0 {
		trackW = 0
	}
	return Point{X: trackX + trackW*s.fraction(), Y: b.Y + b.H/2}
}

// setValueFromX maps a mouse-X coordinate to a value on [Min, Max].
// X is mapped against the visible track range, not the full widget
// bounds — clicking on the state-layer's left overhang at frac=0
// returns Min, not a negative-clamped value.
func (s *Slider) setValueFromX(x float32) {
	b := s.Bounds()
	pad := sliderTrackPad
	trackX := b.X + pad
	trackW := b.W - 2*pad
	if trackW <= 0 {
		return
	}
	frac := (x - trackX) / trackW
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	s.setValue(s.Min + frac*(s.Max-s.Min))
}

// setValue clamps to [Min, Max], updates Value, and fires OnChange when
// the value actually changed.
// SetValue moves the slider to v, clamped to [Min, Max], repaints, and
// fires OnChange when the value changed — the same path a drag takes, so
// state bound through OnChange stays in sync.
func (s *Slider) SetValue(v float32) { s.setValue(v) }

func (s *Slider) setValue(v float32) {
	if v < s.Min {
		v = s.Min
	}
	if v > s.Max {
		v = s.Max
	}
	if v == s.Value {
		return
	}
	s.Value = v
	s.Invalidate()
	if s.OnChange != nil {
		s.OnChange(v)
	}
}

// fraction returns the clamped [0,1] position of Value within [Min,Max].
func (s *Slider) fraction() float32 {
	span := s.Max - s.Min
	if span <= 0 {
		return 0
	}
	f := (s.Value - s.Min) / span
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// step returns the arrow-key increment — Step if set, else 1% of range.
func (s *Slider) step() float32 {
	if s.Step > 0 {
		return s.Step
	}
	return (s.Max - s.Min) / 100
}
