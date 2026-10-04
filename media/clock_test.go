package media

import (
	"testing"
	"time"
)

// withFakeTime runs fn with nowFunc pinned to a virtual clock whose
// advancement is controlled by calling tick(d). Reverts nowFunc on
// return regardless of test outcome.
func withFakeTime(fn func(tick func(d time.Duration))) {
	original := nowFunc
	defer func() { nowFunc = original }()
	t := time.Unix(0, 0)
	nowFunc = func() time.Time { return t }
	fn(func(d time.Duration) { t = t.Add(d) })
}

func TestWallClockStartsAtZeroPaused(t *testing.T) {
	withFakeTime(func(_ func(time.Duration)) {
		c := newWallClock()
		if got := c.HostTime(); got != 0 {
			t.Errorf("fresh clock HostTime = %v, want 0", got)
		}
		if got := c.Rate(); got != 1 {
			t.Errorf("fresh clock Rate = %v, want 1", got)
		}
	})
}

func TestWallClockAdvancesWhilePlaying(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		tick(500 * time.Millisecond)
		got := c.HostTime()
		if got != 500*time.Millisecond {
			t.Errorf("after 500ms play, HostTime = %v, want 500ms", got)
		}
	})
}

func TestWallClockPauseHoldsTime(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		tick(300 * time.Millisecond)
		c.Pause()
		tick(time.Second)
		if got := c.HostTime(); got != 300*time.Millisecond {
			t.Errorf("paused clock advanced from 300ms to %v", got)
		}
		// Resume — clock should pick up from 300ms.
		c.Play()
		tick(200 * time.Millisecond)
		if got := c.HostTime(); got != 500*time.Millisecond {
			t.Errorf("after resume + 200ms, HostTime = %v, want 500ms", got)
		}
	})
}

func TestWallClockSeekToJumps(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		tick(100 * time.Millisecond)
		c.SeekTo(5 * time.Second)
		if got := c.HostTime(); got != 5*time.Second {
			t.Errorf("after SeekTo(5s), HostTime = %v, want 5s", got)
		}
		tick(100 * time.Millisecond)
		if got := c.HostTime(); got != 5*time.Second+100*time.Millisecond {
			t.Errorf("after SeekTo+100ms play, HostTime = %v, want 5.1s", got)
		}
	})
}

func TestWallClockSeekToNegativeClamps(t *testing.T) {
	withFakeTime(func(_ func(time.Duration)) {
		c := newWallClock()
		c.SeekTo(-1 * time.Second)
		if got := c.HostTime(); got != 0 {
			t.Errorf("SeekTo(-1s) HostTime = %v, want 0", got)
		}
	})
}

func TestWallClockRateDoublesAdvance(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.SetRate(2)
		c.Play()
		tick(500 * time.Millisecond)
		if got := c.HostTime(); got != time.Second {
			t.Errorf("at rate=2 after 500ms wall, HostTime = %v, want 1s", got)
		}
	})
}

func TestWallClockRateChangeIsContinuous(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		tick(400 * time.Millisecond) // HostTime = 400ms
		c.SetRate(2)
		tick(200 * time.Millisecond) // adds 400ms of media time
		if got := c.HostTime(); got != 800*time.Millisecond {
			t.Errorf("after rate change, HostTime = %v, want 800ms", got)
		}
	})
}

func TestWallClockRateZeroDefaultsToOne(t *testing.T) {
	withFakeTime(func(_ func(time.Duration)) {
		c := newWallClock()
		c.SetRate(0)
		if got := c.Rate(); got != 1 {
			t.Errorf("SetRate(0) Rate = %v, want fallback to 1", got)
		}
		c.SetRate(-3)
		if got := c.Rate(); got != 1 {
			t.Errorf("SetRate(-3) Rate = %v, want fallback to 1", got)
		}
	})
}

func TestWallClockPlayIsIdempotent(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		tick(100 * time.Millisecond)
		// Second Play must not reset startWall, or HostTime would
		// regress to 0.
		c.Play()
		if got := c.HostTime(); got != 100*time.Millisecond {
			t.Errorf("double Play() reset clock to %v, want 100ms", got)
		}
	})
}

func TestWallClockMonotonicWhilePlaying(t *testing.T) {
	withFakeTime(func(tick func(time.Duration)) {
		c := newWallClock()
		c.Play()
		var prev time.Duration
		for i := 0; i < 100; i++ {
			tick(time.Millisecond)
			got := c.HostTime()
			if got < prev {
				t.Fatalf("HostTime regressed from %v to %v at i=%d", prev, got, i)
			}
			prev = got
		}
	})
}

// fakeAudioRenderer satisfies AudioRenderer with deterministic
// counters so audioClock can be unit-tested without cgo / coreaudio.
type fakeAudioRenderer struct {
	hostTime time.Duration
	playing  bool
	volume   float32
	rate     float32
	plays    int
	pauses   int
	seeks    int
}

func (r *fakeAudioRenderer) Play() error  { r.playing = true; r.plays++; return nil }
func (r *fakeAudioRenderer) Pause() error { r.playing = false; r.pauses++; return nil }
func (r *fakeAudioRenderer) Stop() error  { r.playing = false; r.hostTime = 0; return nil }
func (r *fakeAudioRenderer) SeekTo(t time.Duration) error {
	r.hostTime = t
	r.seeks++
	return nil
}
func (r *fakeAudioRenderer) SetVolume(v float32)     { r.volume = v }
func (r *fakeAudioRenderer) SetRate(rate float32)    { r.rate = rate }
func (r *fakeAudioRenderer) HostTime() time.Duration { return r.hostTime }
func (r *fakeAudioRenderer) OnEnded(_ func())        {}
func (r *fakeAudioRenderer) Close() error            { return nil }

func TestAudioClockDelegatesHostTime(t *testing.T) {
	r := &fakeAudioRenderer{hostTime: 750 * time.Millisecond}
	c := newAudioClock(r)
	if got := c.HostTime(); got != 750*time.Millisecond {
		t.Errorf("HostTime = %v, want 750ms", got)
	}
}

func TestAudioClockNilRendererReturnsZero(t *testing.T) {
	c := newAudioClock(nil)
	if got := c.HostTime(); got != 0 {
		t.Errorf("HostTime with nil renderer = %v, want 0", got)
	}
	// Should not panic on transport calls either.
	c.Play()
	c.Pause()
	c.SeekTo(time.Second)
	c.SetRate(2)
}

func TestAudioClockTransportForwards(t *testing.T) {
	r := &fakeAudioRenderer{}
	c := newAudioClock(r)
	c.Play()
	c.Pause()
	c.SeekTo(500 * time.Millisecond)
	if r.plays != 1 || r.pauses != 1 || r.seeks != 1 {
		t.Errorf("forwards plays=%d pauses=%d seeks=%d, want 1/1/1", r.plays, r.pauses, r.seeks)
	}
	if r.hostTime != 500*time.Millisecond {
		t.Errorf("seek not applied: HostTime = %v", r.hostTime)
	}
}

func TestAudioClockSetRateForwards(t *testing.T) {
	r := &fakeAudioRenderer{}
	c := newAudioClock(r)
	c.SetRate(2)
	if r.rate != 2 {
		t.Errorf("renderer rate = %v, want 2", r.rate)
	}
	if c.Rate() != 2 {
		t.Errorf("clock Rate() = %v, want 2", c.Rate())
	}
}

func TestAudioClockSeekNegativeClamps(t *testing.T) {
	r := &fakeAudioRenderer{}
	c := newAudioClock(r)
	c.SeekTo(-time.Second)
	if r.hostTime != 0 {
		t.Errorf("negative seek not clamped: HostTime = %v", r.hostTime)
	}
}
