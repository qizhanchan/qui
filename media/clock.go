package media

import (
	"sync"
	"time"
)

// Clock is the master beat the video pump uses to decide when a
// decoded frame is "due". Phase 2 ships only wallClock; Phase 3 will
// add an audioClock that wraps AudioRenderer.HostTime so video stays
// locked to the audio output. The pump (in video_view.go) is
// clock-agnostic — swapping implementations doesn't touch it.
//
// HostTime advances monotonically while the clock is playing; it
// freezes on Pause and resumes from the same media time on the next
// Play. SeekTo and SetRate rebase the internal anchor so HostTime
// never jumps backwards (except across an explicit SeekTo).
type Clock interface {
	HostTime() time.Duration
	Play()
	Pause()
	SeekTo(t time.Duration)
	SetRate(r float32)
	Rate() float32
}

// nowFunc indirects time.Now so clock_test.go can pin a virtual clock.
// Package-private; production code never reassigns it.
var nowFunc = time.Now

// wallClock paces playback against wall time, scaled by rate. It has
// no notion of A/V sync — that's audioClock's job in Phase 3.
//
// Internally tracked state:
//
//	startWall  — the real time at which the current play span began
//	startMedia — the media-time position at that moment
//
// While playing, HostTime() = startMedia + (now - startWall) * rate.
// While paused, HostTime() = startMedia (latched).
type wallClock struct {
	mu         sync.Mutex
	rate       float32
	playing    bool
	startWall  time.Time
	startMedia time.Duration
}

func newWallClock() *wallClock {
	return &wallClock{rate: 1}
}

func (c *wallClock) HostTime() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hostTimeLocked()
}

func (c *wallClock) hostTimeLocked() time.Duration {
	if !c.playing {
		return c.startMedia
	}
	delta := nowFunc().Sub(c.startWall)
	if c.rate != 1 {
		delta = time.Duration(float64(delta) * float64(c.rate))
	}
	return c.startMedia + delta
}

func (c *wallClock) Play() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.playing {
		return
	}
	// Latch startWall so the next HostTime() reads startMedia + 0.
	c.startWall = nowFunc()
	c.playing = true
}

func (c *wallClock) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.playing {
		return
	}
	c.startMedia = c.hostTimeLocked()
	c.playing = false
}

func (c *wallClock) SeekTo(t time.Duration) {
	if t < 0 {
		t = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startMedia = t
	c.startWall = nowFunc()
}

func (c *wallClock) SetRate(r float32) {
	if r <= 0 {
		r = 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Rebase as if we paused-then-resumed at the new rate so HostTime
	// is continuous across the change.
	c.startMedia = c.hostTimeLocked()
	c.startWall = nowFunc()
	c.rate = r
}

func (c *wallClock) Rate() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate
}

// audioClock is the Phase-3 master clock for sources that have an
// AudioRenderer. HostTime delegates to the renderer (= audio hardware
// position) so video frames are paced against the actual audio
// playback, not wall time. This removes the slow accumulating drift
// the wall clock would otherwise show on long files.
//
// Play / Pause / SeekTo / SetRate / SetVolume all forward to the
// renderer so the audio engine stays in lockstep with the view's
// state. The clock keeps a local rate value so VideoView's SetRate
// surface remains backend-agnostic.
//
// Caveats:
//   - When the renderer hasn't produced its first frame yet, HostTime
//     returns 0 (renderer convention). The video pump treats that the
//     same as a paused clock: pumpFrame stays parked until the first
//     audio sample is rendered.
//   - On Pause, the audio renderer freezes its clock immediately;
//     HostTime returns the frozen value. No rebase math needed here.
type audioClock struct {
	renderer AudioRenderer
	mu       sync.Mutex
	rate     float32
}

func newAudioClock(r AudioRenderer) *audioClock {
	return &audioClock{renderer: r, rate: 1}
}

func (c *audioClock) HostTime() time.Duration {
	if c.renderer == nil {
		return 0
	}
	return c.renderer.HostTime()
}

func (c *audioClock) Play() {
	if c.renderer != nil {
		_ = c.renderer.Play()
	}
}

func (c *audioClock) Pause() {
	if c.renderer != nil {
		_ = c.renderer.Pause()
	}
}

func (c *audioClock) SeekTo(t time.Duration) {
	if t < 0 {
		t = 0
	}
	if c.renderer != nil {
		_ = c.renderer.SeekTo(t)
	}
}

func (c *audioClock) SetRate(r float32) {
	if r <= 0 {
		r = 1
	}
	c.mu.Lock()
	c.rate = r
	c.mu.Unlock()
	if c.renderer != nil {
		c.renderer.SetRate(r)
	}
}

func (c *audioClock) Rate() float32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rate
}
