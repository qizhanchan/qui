package qui

// State is a bitmask of pseudo-states a widget can be in
// simultaneously. Dispatch of "which Style variant to use"
// follows a priority order defined in StateStyle.Resolve —
// most-specific (Disabled) wins over less-specific (Normal).
type State uint16

const (
	StateHover State = 1 << iota
	StatePressed
	StateFocused
	StateDisabled
	StateChecked
	// StateError flags an input/form widget as being in an error state
	// — the recipe should tint borders / underlines / labels accordingly.
	// Not part of Resolve()'s priority ladder (an errored field can still
	// be focused, hovered, disabled); widgets combine it with the other
	// bits and hand the whole mask to their recipe.
	StateError
)

// StateStyle stores a base Style plus optional per-state overrides.
// Nil override pointers mean "no variant at this state" — Resolve
// falls through to lower-priority states and eventually to Base.
//
// Priority order when multiple state bits are set (e.g., a focused
// button under the mouse):  Disabled > Pressed > Focused > Hover > Checked > Base.
//
// Rationale:
//   - Disabled outranks everything — it's a definitive "don't interact"
//   - Pressed outranks Hover — active press should feel distinct
//   - Focused + Hover can co-occur; Focused wins so the ring + pressed
//     color don't fight
//
// Typical setup:
//
//	var s qui.StateStyle
//	s.Base.Background = theme.Accent
//	s.Base.Foreground = theme.AccentText
//	s.Hover   = &qui.Style{Background: theme.AccentHover,   Foreground: theme.AccentText}
//	s.Pressed = &qui.Style{Background: theme.AccentPressed, Foreground: theme.AccentText}
type StateStyle struct {
	Base     Style
	Hover    *Style
	Pressed  *Style
	Focused  *Style
	Disabled *Style
	Checked  *Style
	// Errored is used by form-input widgets (Input, Spinner, etc.)
	// when their Error flag is set. Outranks Pressed / Focused / Hover
	// in Resolve so an errored field stays error-colored regardless of
	// which state the user is currently interacting through.
	Errored *Style
}

// Resolve returns the effective Style for the given state flags.
// Never returns nil — falls back to &s.Base when no override matches.
//
// Priority order (highest first):
//
//	Disabled → Errored → Pressed → Focused → Hover → Checked → Base
func (s *StateStyle) Resolve(states State) *Style {
	if states&StateDisabled != 0 && s.Disabled != nil {
		return s.Disabled
	}
	if states&StateError != 0 && s.Errored != nil {
		return s.Errored
	}
	if states&StatePressed != 0 && s.Pressed != nil {
		return s.Pressed
	}
	if states&StateFocused != 0 && s.Focused != nil {
		return s.Focused
	}
	if states&StateHover != 0 && s.Hover != nil {
		return s.Hover
	}
	if states&StateChecked != 0 && s.Checked != nil {
		return s.Checked
	}
	return &s.Base
}

// ResolveBackground picks the effective Style for these states and
// returns its Background — the most common per-state query.
func (s *StateStyle) ResolveBackground(states State) Color {
	return s.Resolve(states).Background
}

// StyleWith builds a partial override: copy base, then apply the
// functional mutations. Handy for expressing "like base but different
// background":
//
//	s.Hover = qui.StyleWith(s.Base, qui.Background(theme.AccentHover))
func StyleWith(base Style, mutators ...func(*Style)) *Style {
	out := base
	for _, m := range mutators {
		m(&out)
	}
	return &out
}

// Background returns a mutator that sets Style.Background.
func Background(c Color) func(*Style) { return func(s *Style) { s.Background = c } }

// Foreground mutator.
func Foreground(c Color) func(*Style) { return func(s *Style) { s.Foreground = c } }

// Border mutator — sets both color and size.
func Border(c Color, size float32) func(*Style) {
	return func(s *Style) { s.Border = c; s.BorderSize = size }
}

// BackgroundAdjust applies a brightness delta (additive to R/G/B,
// clamped to [0, 1]). Positive values lighten, negative darken.
// Useful when you want "slightly lighter on hover" without defining
// a separate token.
func BackgroundAdjust(delta float32) func(*Style) {
	return func(s *Style) { s.Background = AdjustBrightness(s.Background, delta) }
}

// AdjustBrightness returns c with R/G/B shifted by delta, clamped.
// Alpha is preserved.
func AdjustBrightness(c Color, delta float32) Color {
	return Color{
		R: clampColor(c.R + delta),
		G: clampColor(c.G + delta),
		B: clampColor(c.B + delta),
		A: c.A,
	}
}

// LerpColor linearly interpolates between a and b at t ∈ [0, 1].
// Straight RGB interpolation — fine for close-hue transitions (hover
// tints); for wide hue sweeps a proper HSL/OKLAB lerp is better but
// out of scope.
func LerpColor(a, b Color, t float32) Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	return Color{
		R: a.R + (b.R-a.R)*t,
		G: a.G + (b.G-a.G)*t,
		B: a.B + (b.B-a.B)*t,
		A: a.A + (b.A-a.A)*t,
	}
}

func clampColor(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
