package qui

import "time"

// Animation represents a time-based transition.
type Animation struct {
	Duration time.Duration
	Start    time.Time
	Active   bool
	Update   func(progress float32)
	Done     func()
}

// Tick advances the animation and invokes Update.
func (a *Animation) Tick(now time.Time) {
	if !a.Active {
		return
	}
	if a.Start.IsZero() {
		a.Start = now
	}
	elapsed := now.Sub(a.Start)
	if elapsed >= a.Duration {
		if a.Update != nil {
			a.Update(1)
		}
		a.Active = false
		if a.Done != nil {
			a.Done()
		}
		return
	}
	if a.Update != nil {
		a.Update(float32(elapsed) / float32(a.Duration))
	}
}

// Transition is a scalar 0..1 value animated from From to To over
// Duration, meant to drive per-frame interpolation of colors / sizes
// from widget state changes (hover, focus, press). It integrates with
// Tickable: widgets with a live Transition return their Bounds from
// Tick to request re-paint each frame while the transition runs.
//
// Usage pattern:
//
//	// In MouseEnter/Leave:
//	b.hoverTrans.Begin(b.hoverTrans.Value(time.Now()), 1, time.Now())
//	// (or 0 on leave)
//
//	// In Tick:
//	if b.hoverTrans.Active(now) { return b.Bounds() }
//
//	// In Draw:
//	t := b.hoverTrans.Value(time.Now())
//	bg := qui.LerpColor(base, hover, t)
//
// Value() is pure — no side effects. Active() is also read-only; the
// transition's `done` flip happens lazily inside Value when the end
// time passes.
type Transition struct {
	Duration time.Duration
	From, To float32

	startTime time.Time
	started   bool
}

// Begin starts a transition from `from` to `to`, measured from `now`.
// Repeated Begin calls mid-flight re-anchor the start — useful when
// the user hovers-out-hovers-in rapidly (we resume from the current
// interpolated value, not a snap).
func (t *Transition) Begin(from, to float32, now time.Time) {
	t.From = from
	t.To = to
	t.startTime = now
	t.started = true
}

// Value returns the interpolated value at `now`. If the transition
// has completed, returns To.
func (t *Transition) Value(now time.Time) float32 {
	if !t.started {
		return t.To
	}
	if t.Duration <= 0 {
		return t.To
	}
	elapsed := now.Sub(t.startTime)
	if elapsed >= t.Duration {
		return t.To
	}
	p := float32(elapsed) / float32(t.Duration)
	return t.From + (t.To-t.From)*easeOutCubic(p)
}

// Active reports whether this transition still has frames to animate.
// Widgets use this in Tick to decide whether to keep requesting repaint.
func (t *Transition) Active(now time.Time) bool {
	if !t.started {
		return false
	}
	return now.Sub(t.startTime) < t.Duration
}

// easeOutCubic is the default easing — fast-in, slow-out, feels natural
// for UI state fades. Equivalent to CSS ease-out curve.
func easeOutCubic(t float32) float32 {
	u := 1 - t
	return 1 - u*u*u
}
