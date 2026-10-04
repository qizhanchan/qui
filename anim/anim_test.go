package anim

import (
	"math"
	"testing"
	"time"

	"github.com/qizhanchan/qui"
)

func TestEasingEndpoints(t *testing.T) {
	funcs := map[string]EasingFunc{
		"Linear":         Linear,
		"EaseInQuad":     EaseInQuad,
		"EaseOutQuad":    EaseOutQuad,
		"EaseInOutQuad":  EaseInOutQuad,
		"EaseInCubic":    EaseInCubic,
		"EaseOutCubic":   EaseOutCubic,
		"EaseInOutCubic": EaseInOutCubic,
		"EaseInQuart":    EaseInQuart,
		"EaseOutQuart":   EaseOutQuart,
		"EaseInOutQuart": EaseInOutQuart,
		"EaseOutBounce":  EaseOutBounce,
	}
	for name, fn := range funcs {
		if got := fn(0); math.Abs(float64(got)) > 1e-5 {
			t.Errorf("%s(0) = %v, want 0", name, got)
		}
		if got := fn(1); math.Abs(float64(got)-1) > 1e-5 {
			t.Errorf("%s(1) = %v, want 1", name, got)
		}
	}
}

func TestTweenFloatLinear(t *testing.T) {
	var applied float32
	tw := &Tween[float32]{
		From:     0,
		To:       100,
		Duration: 100 * time.Millisecond,
		Easing:   Linear,
		Apply:    func(v float32) { applied = v },
		Lerp:     LerpFloat,
	}
	start := time.Unix(0, 0)
	tw.Tick(start)
	if applied != 0 {
		t.Errorf("initial tick applied = %v, want 0", applied)
	}
	r, done := tw.Tick(start.Add(50 * time.Millisecond))
	if done {
		t.Error("tween reported done halfway through")
	}
	if math.Abs(float64(applied-50)) > 1e-5 {
		t.Errorf("midpoint applied = %v, want 50", applied)
	}
	_ = r
	_, done = tw.Tick(start.Add(200 * time.Millisecond))
	if !done {
		t.Error("tween did not report done past duration")
	}
	if math.Abs(float64(applied-100)) > 1e-5 {
		t.Errorf("final applied = %v, want 100", applied)
	}
}

func TestTweenOnDoneFires(t *testing.T) {
	var done bool
	tw := &Tween[float32]{
		From: 0, To: 1, Duration: 10 * time.Millisecond,
		Apply:  func(float32) {},
		Lerp:   LerpFloat,
		OnDone: func() { done = true },
	}
	start := time.Unix(0, 0)
	tw.Tick(start)
	tw.Tick(start.Add(20 * time.Millisecond))
	if !done {
		t.Error("OnDone did not fire")
	}
}

func TestSpringSettles(t *testing.T) {
	var applied float32
	s := &Spring{
		Stiffness: 170,
		Damping:   26,
		Mass:      1,
		Target:    1,
		Apply:     func(v float32) { applied = v },
	}
	now := time.Unix(0, 0)
	steps := 0
	for {
		_, done := s.Tick(now)
		now = now.Add(16 * time.Millisecond)
		steps++
		if done {
			break
		}
		if steps > 1000 {
			t.Fatal("spring did not settle within 1000 frames")
		}
	}
	if math.Abs(float64(applied-1)) > 0.01 {
		t.Errorf("spring settled at %v, expected ~1", applied)
	}
}

func TestTimelineKeyframeSampling(t *testing.T) {
	frames := []Keyframe[float32]{
		{At: 0, Value: 0},
		{At: 100 * time.Millisecond, Value: 10, Easing: Linear},
		{At: 200 * time.Millisecond, Value: 20, Easing: Linear},
	}
	v := sampleKeyframes(frames, 50*time.Millisecond, LerpFloat)
	if math.Abs(float64(v-5)) > 1e-5 {
		t.Errorf("50ms sample = %v, want 5", v)
	}
	v = sampleKeyframes(frames, 150*time.Millisecond, LerpFloat)
	if math.Abs(float64(v-15)) > 1e-5 {
		t.Errorf("150ms sample = %v, want 15", v)
	}
	// Before first and after last are clamped.
	if v := sampleKeyframes(frames, -1, LerpFloat); v != 0 {
		t.Errorf("negative sample = %v, want 0", v)
	}
	if v := sampleKeyframes(frames, time.Second, LerpFloat); v != 20 {
		t.Errorf("past-end sample = %v, want 20", v)
	}
}

func TestTimelineDurationDerivedFromTracks(t *testing.T) {
	tl := &Timeline{}
	var x float32
	tl.AddFloat(&x, []Keyframe[float32]{
		{At: 0, Value: 0},
		{At: 500 * time.Millisecond, Value: 1},
	})
	if tl.Duration != 500*time.Millisecond {
		t.Errorf("Duration derived = %v, want 500ms", tl.Duration)
	}
}

func TestTimelineAddFloatFuncAppliesSampledValues(t *testing.T) {
	tl := &Timeline{}
	var applied float32
	tl.AddFloatFunc(func(value float32) { applied = value }, []Keyframe[float32]{
		{At: 0, Value: 0},
		{At: 100 * time.Millisecond, Value: 10, Easing: Linear},
	})
	start := time.Unix(0, 0)
	tl.Tick(start)
	tl.Tick(start.Add(50 * time.Millisecond))
	if math.Abs(float64(applied-5)) > 1e-5 {
		t.Fatalf("AddFloatFunc applied %v, want 5", applied)
	}
}

func TestTimelinePingPong(t *testing.T) {
	tl := &Timeline{Loop: LoopPingPong}
	var x float32
	tl.AddFloat(&x, []Keyframe[float32]{
		{At: 0, Value: 0},
		{At: 100 * time.Millisecond, Value: 100, Easing: Linear},
	})
	start := time.Unix(0, 0)
	tl.Tick(start) // head=0
	if x != 0 {
		t.Errorf("t=0 ping-pong head value = %v, want 0", x)
	}
	tl.Tick(start.Add(50 * time.Millisecond)) // forward half
	if math.Abs(float64(x-50)) > 1e-5 {
		t.Errorf("t=50ms forward = %v, want 50", x)
	}
	tl.Tick(start.Add(150 * time.Millisecond)) // reverse half
	if math.Abs(float64(x-50)) > 1e-5 {
		t.Errorf("t=150ms reverse = %v, want 50", x)
	}
}

// Verify that Tween, Spring, Timeline all satisfy qui.Animator.
var (
	_ qui.Animator = (*Tween[float32])(nil)
	_ qui.Animator = (*Spring)(nil)
	_ qui.Animator = (*Timeline)(nil)
)
