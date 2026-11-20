package qui

import (
	"testing"
	"time"
)

// Frames that start a few milliseconds early or late still see times
// exactly one refresh apart; a dropped frame sees exactly two; a pause
// re-anchors on real time.
func TestFrameClockAdvancesOnRefreshGrid(t *testing.T) {
	w := &Window{}
	p := defaultRefreshPeriod
	t0 := time.Now()
	if got := w.frameTime(t0); !got.Equal(t0) {
		t.Fatal("first frame not anchored on real time")
	}
	jitter := []time.Duration{3, -4, 2, -1, 5, -3}
	prev, real := t0, t0
	for i, j := range jitter {
		real = t0.Add(time.Duration(i+1)*p + j*time.Millisecond)
		got := w.frameTime(real)
		if d := got.Sub(prev); d < p-time.Millisecond/2 || d > p+time.Millisecond/2 {
			t.Fatalf("frame %d advanced %v, want %v", i, d, p)
		}
		prev = got
	}
	// An extra Step 2ms later is the same frame.
	if got := w.frameTime(real.Add(2 * time.Millisecond)); !got.Equal(prev) {
		t.Fatalf("extra step moved the clock by %v", got.Sub(prev))
	}
	// A dropped frame: two periods.
	real = real.Add(2*p + time.Millisecond)
	if got := w.frameTime(real); got.Sub(prev) < 2*p-time.Millisecond || got.Sub(prev) > 2*p+time.Millisecond {
		t.Fatalf("dropped frame advanced %v, want %v", got.Sub(prev), 2*p)
	}
	// A pause: real time again.
	real = real.Add(time.Second)
	if got := w.frameTime(real); !got.Equal(real) {
		t.Fatal("clock did not re-anchor after a pause")
	}
}
