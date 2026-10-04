package anim

import (
	"math"
	"time"

	"github.com/qizhanchan/qui"
)

// Spring is a physics-based scalar animator. Instead of a duration +
// easing curve, you set a target; the value relaxes toward it under
// Hooke's law (F = -k·x) with viscous damping (F = -c·v). Mass,
// Stiffness and Damping together decide whether the motion is
// under-damped (oscillates), critically damped (fastest non-oscillating
// settle), or over-damped (slow settle, no overshoot).
//
// Typical presets:
//
//	// Snappy with no bounce:
//	Spring{Stiffness: 170, Damping: 26, Mass: 1}
//	// Bouncy:
//	Spring{Stiffness: 180, Damping: 12, Mass: 1}
//
// The solver is semi-implicit Euler with a fixed 1ms sub-step for
// stability — you can call Tick at any frame rate.
type Spring struct {
	Stiffness   float32
	Damping     float32
	Mass        float32
	Target      float32
	Value       float32
	Velocity    float32
	Apply       func(float32)
	DirtyBounds func() qui.Rect
	OnDone      func()

	lastTime time.Time
	stopped  bool
	done     bool
}

// Tick advances the spring. done=true once the motion has settled
// (|v| < eps and |value-target| < eps) or Stop has been called.
func (s *Spring) Tick(now time.Time) (qui.Rect, bool) {
	if s == nil || s.stopped || s.done {
		return qui.Rect{}, true
	}
	if s.Mass <= 0 {
		s.Mass = 1
	}
	if s.lastTime.IsZero() {
		s.lastTime = now
		if s.Apply != nil {
			s.Apply(s.Value)
		}
		return s.dirty(), false
	}
	dt := float32(now.Sub(s.lastTime).Seconds())
	s.lastTime = now
	// Clamp to avoid huge first-frame deltas after long stalls.
	if dt > 0.064 {
		dt = 0.064
	}
	// Sub-step at 1ms for numerical stability — spring stiffness up
	// to ~500 remains stable at this step.
	const sub = 0.001
	steps := int(math.Ceil(float64(dt) / sub))
	if steps < 1 {
		steps = 1
	}
	h := dt / float32(steps)
	for i := 0; i < steps; i++ {
		x := s.Value - s.Target
		a := (-s.Stiffness*x - s.Damping*s.Velocity) / s.Mass
		s.Velocity += a * h
		s.Value += s.Velocity * h
	}
	if s.Apply != nil {
		s.Apply(s.Value)
	}
	const eps = 0.001
	if math.Abs(float64(s.Velocity)) < eps && math.Abs(float64(s.Value-s.Target)) < eps {
		s.Value = s.Target
		s.Velocity = 0
		if s.Apply != nil {
			s.Apply(s.Value)
		}
		s.done = true
		if s.OnDone != nil {
			s.OnDone()
		}
		return s.dirty(), true
	}
	return s.dirty(), false
}

// Stop halts the spring where it is.
func (s *Spring) Stop() {
	if s == nil {
		return
	}
	s.stopped = true
}

// SetTarget updates the rest position. The spring wakes up and starts
// pulling toward the new target, carrying its current velocity.
func (s *Spring) SetTarget(target float32) {
	if s == nil {
		return
	}
	s.Target = target
	s.done = false
}

func (s *Spring) dirty() qui.Rect {
	if s.DirtyBounds == nil {
		return qui.Rect{}
	}
	return s.DirtyBounds()
}
