package anim

import "math"

// EasingFunc maps linear progress t ∈ [0, 1] to eased progress.
// Return values outside [0, 1] are allowed (overshoot easings like
// Back / Elastic do this on purpose) — callers that interpolate
// colors or clamp ranges must be prepared for it.
type EasingFunc func(t float32) float32

// Linear is the identity easing.
func Linear(t float32) float32 { return t }

// EaseInQuad is t^2 — slow start, fast end.
func EaseInQuad(t float32) float32 { return t * t }

// EaseOutQuad is 1-(1-t)^2 — fast start, slow end.
func EaseOutQuad(t float32) float32 {
	u := 1 - t
	return 1 - u*u
}

// EaseInOutQuad accelerates then decelerates symmetrically.
func EaseInOutQuad(t float32) float32 {
	if t < 0.5 {
		return 2 * t * t
	}
	u := 1 - t
	return 1 - 2*u*u
}

// EaseInCubic is t^3.
func EaseInCubic(t float32) float32 { return t * t * t }

// EaseOutCubic is 1-(1-t)^3 — the default UI ease-out curve.
func EaseOutCubic(t float32) float32 {
	u := 1 - t
	return 1 - u*u*u
}

// EaseInOutCubic — symmetric cubic.
func EaseInOutCubic(t float32) float32 {
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := 1 - t
	return 1 - 4*u*u*u
}

// EaseInQuart, EaseOutQuart, EaseInOutQuart — stronger ease curves.
func EaseInQuart(t float32) float32 { return t * t * t * t }
func EaseOutQuart(t float32) float32 {
	u := 1 - t
	return 1 - u*u*u*u
}
func EaseInOutQuart(t float32) float32 {
	if t < 0.5 {
		return 8 * t * t * t * t
	}
	u := 1 - t
	return 1 - 8*u*u*u*u
}

// EaseOutBack overshoots slightly past 1 before settling. Good for
// "snap" motions (button press release, panel pop-in).
func EaseOutBack(t float32) float32 {
	const c1 = 1.70158
	const c3 = c1 + 1
	u := t - 1
	return 1 + c3*u*u*u + c1*u*u
}

// EaseInBack undershoots past 0 before starting. Rare but useful
// for anticipation animations.
func EaseInBack(t float32) float32 {
	const c1 = 1.70158
	const c3 = c1 + 1
	return c3*t*t*t - c1*t*t
}

// EaseOutElastic is a damped sine wave that settles at 1. Feels
// rubbery; use sparingly.
func EaseOutElastic(t float32) float32 {
	if t == 0 || t == 1 {
		return t
	}
	const c4 = 2 * math.Pi / 3
	return float32(math.Pow(2, -10*float64(t))*math.Sin((float64(t)*10-0.75)*c4) + 1)
}

// EaseOutBounce simulates a ball bouncing to rest at y=1.
func EaseOutBounce(t float32) float32 {
	const n1 = 7.5625
	const d1 = 2.75
	switch {
	case t < 1/d1:
		return n1 * t * t
	case t < 2/d1:
		u := t - 1.5/d1
		return n1*u*u + 0.75
	case t < 2.5/d1:
		u := t - 2.25/d1
		return n1*u*u + 0.9375
	default:
		u := t - 2.625/d1
		return n1*u*u + 0.984375
	}
}
