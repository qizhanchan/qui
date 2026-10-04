package anim

import (
	"time"

	"github.com/qizhanchan/qui"
)

// LoopMode decides how a Timeline behaves after reaching Duration.
type LoopMode int

const (
	// LoopNone stops at the end (default).
	LoopNone LoopMode = iota
	// LoopRestart jumps back to t=0 and plays again.
	LoopRestart
	// LoopReverse plays backwards to t=0, stops.
	LoopReverse
	// LoopPingPong alternates forward / backward indefinitely.
	LoopPingPong
)

// Keyframe pins a value to a point in time.
type Keyframe[T any] struct {
	At     time.Duration
	Value  T
	Easing EasingFunc // eases the segment ENDING at this keyframe; nil = Linear
}

// track is the internal non-generic form a Timeline holds. Generics
// don't survive into a heterogeneous slice, so each AddX helper
// closes over the generic type and returns a trackRunner.
type trackRunner struct {
	duration time.Duration
	sample   func(t time.Duration)
}

// Timeline composes multiple keyframed tracks into one Animator.
// All tracks share a single playhead, driven by AddTrack callbacks.
//
// Typical use:
//
//	tl := &anim.Timeline{Duration: 2 * time.Second, Loop: anim.LoopPingPong, DirtyBounds: w.Bounds}
//	tl.AddFloat(&w.opacity, []anim.Keyframe[float32]{
//	    {At: 0, Value: 0},
//	    {At: time.Second, Value: 1, Easing: anim.EaseOutCubic},
//	})
//	window.RegisterAnimator(tl)
type Timeline struct {
	Duration    time.Duration
	Loop        LoopMode
	OnDone      func()
	DirtyBounds func() qui.Rect

	tracks   []trackRunner
	started  bool
	stopped  bool
	done     bool
	startT   time.Time
	reversed bool
}

// Tick advances the timeline and samples each track at the current
// playhead. See LoopMode for end-of-duration behavior.
func (tl *Timeline) Tick(now time.Time) (qui.Rect, bool) {
	if tl == nil || tl.stopped || tl.done {
		return qui.Rect{}, true
	}
	if !tl.started {
		tl.started = true
		tl.startT = now
	}
	elapsed := now.Sub(tl.startT)
	dur := tl.Duration
	if dur <= 0 {
		dur = time.Millisecond
	}
	var head time.Duration
	done := false
	switch tl.Loop {
	case LoopNone:
		if elapsed >= dur {
			head = dur
			done = true
		} else {
			head = elapsed
		}
	case LoopRestart:
		head = elapsed % dur
	case LoopReverse:
		if elapsed >= dur {
			head = 0
			done = true
		} else {
			head = dur - elapsed
		}
	case LoopPingPong:
		cycle := elapsed % (2 * dur)
		if cycle <= dur {
			head = cycle
		} else {
			head = 2*dur - cycle
		}
	}
	for _, t := range tl.tracks {
		t.sample(head)
	}
	if done {
		tl.done = true
		if tl.OnDone != nil {
			tl.OnDone()
		}
	}
	return tl.dirty(), done
}

// Stop freezes the timeline wherever it is.
func (tl *Timeline) Stop() {
	if tl == nil {
		return
	}
	tl.stopped = true
}

// AddFloat adds a track of float32 keyframes that writes into *target.
func (tl *Timeline) AddFloat(target *float32, frames []Keyframe[float32]) *Timeline {
	if target == nil {
		return tl
	}
	return tl.AddFloatFunc(func(v float32) { *target = v }, frames)
}

// AddFloatFunc adds a float32 track that commits each sampled value through
// apply. Prefer this for controlled widget properties whose setters perform
// invalidation or maintain related state.
func (tl *Timeline) AddFloatFunc(apply func(float32), frames []Keyframe[float32]) *Timeline {
	return AddTrack(tl, frames, LerpFloat, apply)
}

// AddColor adds a color track that invokes apply.
func (tl *Timeline) AddColor(apply func(qui.Color), frames []Keyframe[qui.Color]) *Timeline {
	return AddTrack(tl, frames, qui.LerpColor, apply)
}

// AddRect adds a rect track that invokes apply.
func (tl *Timeline) AddRect(apply func(qui.Rect), frames []Keyframe[qui.Rect]) *Timeline {
	return AddTrack(tl, frames, LerpRect, apply)
}

// AddPoint adds a point track that invokes apply.
func (tl *Timeline) AddPoint(apply func(qui.Point), frames []Keyframe[qui.Point]) *Timeline {
	return AddTrack(tl, frames, LerpPoint, apply)
}

// AddTrack is the low-level generic entry point. Frames must be sorted
// by At ascending (empty slices are no-ops). Go forbids generic
// methods on non-generic types, so this is a package-level function.
func AddTrack[T any](tl *Timeline, frames []Keyframe[T], lerp func(a, b T, t float32) T, apply func(T)) *Timeline {
	if tl == nil || len(frames) == 0 || lerp == nil || apply == nil {
		return tl
	}
	// Make a defensive copy — callers often reuse literal slices.
	localFrames := make([]Keyframe[T], len(frames))
	copy(localFrames, frames)
	var maxT time.Duration
	for _, f := range localFrames {
		if f.At > maxT {
			maxT = f.At
		}
	}
	tl.tracks = append(tl.tracks, trackRunner{
		duration: maxT,
		sample: func(head time.Duration) {
			apply(sampleKeyframes(localFrames, head, lerp))
		},
	})
	if maxT > tl.Duration {
		tl.Duration = maxT
	}
	return tl
}

func (tl *Timeline) dirty() qui.Rect {
	if tl.DirtyBounds == nil {
		return qui.Rect{}
	}
	return tl.DirtyBounds()
}

// sampleKeyframes returns the interpolated value at head. Before the
// first keyframe we hold the first value; after the last we hold the
// last value. Between (i, i+1) we ease via frames[i+1].Easing.
func sampleKeyframes[T any](frames []Keyframe[T], head time.Duration, lerp func(a, b T, t float32) T) T {
	n := len(frames)
	if head <= frames[0].At {
		return frames[0].Value
	}
	if head >= frames[n-1].At {
		return frames[n-1].Value
	}
	for i := 0; i < n-1; i++ {
		a, b := frames[i], frames[i+1]
		if head < b.At {
			span := b.At - a.At
			if span <= 0 {
				return b.Value
			}
			t := float32(head-a.At) / float32(span)
			easing := b.Easing
			if easing == nil {
				easing = Linear
			}
			return lerp(a.Value, b.Value, easing(t))
		}
	}
	return frames[n-1].Value
}
