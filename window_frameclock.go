package qui

import (
	"os"
	"time"
)

// The animators' frame clock.
//
// Animators used to sample time.Now() when their Step ran. A Step starts
// wherever the loop happened to wake within a display refresh, while its
// frame is shown on the next refresh boundary: sampling a few milliseconds
// early on one frame and late on the next moves a steady animation by
// uneven amounts per displayed frame — visible judder on a slow marquee,
// even when no frame is dropped. A browser hands its animations the
// BeginFrame time instead, which advances on the vsync grid.
//
// frameTime does the same without a vsync timestamp from every backend:
// while frames keep coming, the clock advances by a whole number of
// refresh periods — the one closest to the real time that passed — so
// consecutive frames are exactly one period apart and a dropped frame
// shows as exactly two. The period is learned from the gaps between
// consecutive frames (60 Hz, a 120 Hz ProMotion panel). A pause longer
// than a few periods, or drift beyond half a period, re-anchors the clock
// on real time, so it never runs away from it.

// frameClockOff disables the clock (QUI_FRAME_CLOCK=0): animators see
// real time.
var frameClockOff = os.Getenv("QUI_FRAME_CLOCK") == "0"

type frameClock struct {
	last   time.Time     // the frame time handed out last
	period time.Duration // estimated refresh period
}

const (
	defaultRefreshPeriod = time.Second / 60
	minRefreshPeriod     = time.Second / 240
	maxRefreshPeriod     = time.Second / 24
)

// frameTime is the time this Step's animators see (see above).
func (w *Window) frameTime(now time.Time) time.Time {
	c := &w.frameClock
	if frameClockOff {
		return now
	}
	before := c.period
	t := c.advance(now)
	if c.period != before {
		w.refreshPeriod.Store(int64(c.period))
	}
	return t
}

// advance moves the clock to the frame at now (see above).
func (c *frameClock) advance(now time.Time) time.Time {
	if c.period == 0 {
		c.period = defaultRefreshPeriod
	}
	gap := now.Sub(c.last)
	if c.last.IsZero() || gap > 4*c.period || gap < -c.period/2 {
		c.last = now // first frame of a run: anchor on real time
		return now
	}
	if gap < c.period/2 {
		// An extra Step inside the same refresh (a job woke the loop):
		// the frame being built is the one already timed.
		return c.last
	}
	// Learn the period from consecutive single-frame gaps.
	if gap >= minRefreshPeriod && gap <= maxRefreshPeriod && gap < c.period*3/2 {
		c.period += (gap - c.period) / 64
	}
	k := (gap + c.period/2) / c.period
	if k < 1 {
		k = 1
	}
	t := c.last.Add(k * c.period)
	if d := t.Sub(now); d > c.period/2 || d < -c.period/2 {
		t = now // drifted: re-anchor
	}
	c.last = t
	return t
}
