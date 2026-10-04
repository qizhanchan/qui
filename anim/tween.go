package anim

import (
	"time"

	"github.com/qizhanchan/qui"
)

// Tween animates a single value of any type from From to To over
// Duration using Easing. Apply is invoked on each Tick with the
// interpolated value so the widget can update its state; DirtyBounds
// returns the rect that should be repainted (usually the widget's
// Bounds()). Lerp defines how to interpolate between From and To —
// float/color/rect/vec3 helpers are provided below.
//
// Tween satisfies qui.Animator structurally; register via
// window.RegisterAnimator(t).
type Tween[T any] struct {
	From, To    T
	Duration    time.Duration
	Easing      EasingFunc
	Apply       func(T)
	Lerp        func(a, b T, t float32) T
	OnDone      func()
	DirtyBounds func() qui.Rect

	started   bool
	stopped   bool
	startTime time.Time
}

// Tick drives the tween forward; done=true when the full duration
// has elapsed, Apply has been called with To, and OnDone has fired.
func (tw *Tween[T]) Tick(now time.Time) (qui.Rect, bool) {
	if tw == nil || tw.stopped {
		return qui.Rect{}, true
	}
	if tw.Apply == nil || tw.Lerp == nil {
		return qui.Rect{}, true
	}
	if !tw.started {
		tw.started = true
		tw.startTime = now
	}
	easing := tw.Easing
	if easing == nil {
		easing = Linear
	}
	var p float32 = 1
	if tw.Duration > 0 {
		elapsed := now.Sub(tw.startTime)
		if elapsed >= tw.Duration {
			p = 1
		} else {
			p = float32(elapsed) / float32(tw.Duration)
		}
	}
	tw.Apply(tw.Lerp(tw.From, tw.To, easing(p)))
	done := p >= 1
	if done && tw.OnDone != nil {
		tw.OnDone()
	}
	return tw.dirty(), done
}

// Stop halts the tween before completion. OnDone will not fire.
func (tw *Tween[T]) Stop() {
	if tw == nil {
		return
	}
	tw.stopped = true
}

func (tw *Tween[T]) dirty() qui.Rect {
	if tw.DirtyBounds == nil {
		return qui.Rect{}
	}
	return tw.DirtyBounds()
}

// LerpFloat interpolates two float32s; useful as Tween[float32].Lerp.
func LerpFloat(a, b float32, t float32) float32 { return a + (b-a)*t }

// LerpRect interpolates rect components independently.
func LerpRect(a, b qui.Rect, t float32) qui.Rect {
	return qui.Rect{
		X: LerpFloat(a.X, b.X, t),
		Y: LerpFloat(a.Y, b.Y, t),
		W: LerpFloat(a.W, b.W, t),
		H: LerpFloat(a.H, b.H, t),
	}
}

// LerpPoint interpolates two points.
func LerpPoint(a, b qui.Point, t float32) qui.Point {
	return qui.Point{X: LerpFloat(a.X, b.X, t), Y: LerpFloat(a.Y, b.Y, t)}
}
