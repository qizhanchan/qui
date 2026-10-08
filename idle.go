package qui

import (
	"fmt"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// On-demand frame pacing.
//
// App.Run does not wake on a fixed clock. Each iteration asks every window
// how soon it next needs a Step and blocks in the platform's event wait for
// exactly that long:
//
//   - a window that is visibly changing (it painted last frame, has running
//     animators, pending jobs, or dirty paint / layout) wants the next frame
//     at display cadence;
//   - a window waiting on a known moment (a caret blink, a tooltip delay)
//     wants to be woken at that moment — see RequestTickAt;
//   - otherwise the loop sleeps until the OS delivers an event or another
//     goroutine wakes it (PostJob, WakeEventLoop).
//
// "Painted last frame" is what keeps a Tickable animating without any
// explicit request: a Tick that returns a dirty rect causes a paint, and a
// paint buys one more frame, so the animation runs until its Tick reports
// nothing changed. Only a Tickable that changes after a quiet stretch — a
// blink — has to announce when it next needs to run.
//
// App.SetMaxIdleWait caps the sleep for apps whose own Tickables predate
// RequestTickAt.
//
// A registered Animator counts as "changing" only while frames keep coming.
// One that never reports done yet changes nothing — typically a goroutine→UI
// queue drained from Tick — would otherwise hold the loop at display cadence
// forever. After animatorQuietGrace without a painted frame, animators alone
// only buy a poll every animatorQuietPoll, they stop counting against
// IdleState.IsIdle, and each such animator type is logged once. Any paint
// restores full cadence.

// frameInterval is the cadence while a window is changing.
const frameInterval = time.Second / 60

// animatorQuietGrace is how long registered animators may tick without a
// frame being painted before the loop stops treating them as animating.
// animatorQuietPoll is how often quiet animators are still ticked, so an
// animator that polls for work keeps working, at a bounded latency.
const (
	animatorQuietGrace = 500 * time.Millisecond
	animatorQuietPoll  = 250 * time.Millisecond
)

// pumpForever asks the platform to block until an event or a wake arrives.
const pumpForever = time.Duration(math.MaxInt64)

// pumpBlocked is set while the loop sleeps past one frame interval, and
// wakeQueued dedups the wakes posted during that sleep. Both are touched
// only on the main thread; atomics just keep the race detector honest
// about the platform callbacks that read them.
var (
	pumpBlocked atomic.Bool
	wakeQueued  atomic.Bool
)

// wakeIfBlocked makes a blocking event wait return so the loop re-evaluates
// its schedule. Platform callbacks call it for state changes AppKit can
// deliver from inside the wait without an input event of their own — a
// Dock minimize, a fullscreen transition ending, a programmatic move — and
// that the loop polls for (lifecycle callbacks, fullscreen tracking) or
// that leave work behind. At most one wake is queued per wait.
func wakeIfBlocked() {
	if pumpBlocked.Load() && wakeQueued.CompareAndSwap(false, true) {
		WakeEventLoop()
	}
}

// SetMaxIdleWait caps how long the main loop sleeps when no window needs a
// frame. Zero (the default) means no cap: an idle app blocks until an event
// arrives and costs no CPU at all.
//
// The cap exists for Tickables written before RequestTickAt that change
// state after a quiet stretch — a hand-rolled blink or a timer polled in
// Tick. They keep working under a cap, ticking at least once per d.
// Prefer fixing the Tickable (call Window.RequestTickAt from Tick) and
// leaving the cap off.
func (a *App) SetMaxIdleWait(d time.Duration) {
	if a == nil || d < 0 {
		return
	}
	a.maxIdleWait = d
}

// idleTimeout is how long the next event wait may block.
func (a *App) idleTimeout(now time.Time) time.Duration {
	wait := pumpForever
	if a.maxIdleWait > 0 {
		wait = a.maxIdleWait
	}
	for _, w := range a.windows {
		if w == nil || w.plat == nil {
			continue
		}
		if w.ShouldClose() {
			// A job or callback asked to close; tear down promptly
			// instead of after the next input event.
			return 0
		}
		if d := w.nextFrameWait(now); d < wait {
			wait = d
		}
	}
	return wait
}

// RequestTickAt asks the loop to Step this window — ticking every Tickable
// — no later than at, even if nothing else happens in between. A Tickable
// that changes after a quiet stretch calls it from Tick with the moment it
// next needs to run (a caret blink requests the next toggle). Requests
// keep the earliest moment; a past moment means "next frame".
//
// Not needed for a Tick that returns a dirty rect every frame while it
// animates: painting already keeps frames coming.
func (w *Window) RequestTickAt(at time.Time) {
	if w == nil || at.IsZero() {
		return
	}
	w.assertUIThread("Window.RequestTickAt")
	if w.tickAt.IsZero() || at.Before(w.tickAt) {
		w.tickAt = at
	}
}

// nextFrameWait is how long the loop may sleep before this window needs a
// Step: frameInterval while it is changing, the distance to its earliest
// deadline while it waits on one, pumpForever when it is idle.
func (w *Window) nextFrameWait(now time.Time) time.Duration {
	if w.framePainted || !w.dirtyRegion.IsEmpty() ||
		w.PendingJobs() > 0 || len(w.afterLayout) > 0 || w.overlayResizePending ||
		(w.root != nil && w.root.IsLayoutDirty()) {
		return frameInterval
	}
	wait := pumpForever
	if len(w.animators) > 0 {
		wait = frameInterval
		if w.animatorsQuiet(now) {
			wait = animatorQuietPoll
		}
	}
	consider := func(at time.Time) {
		if at.IsZero() {
			return
		}
		d := at.Sub(now)
		if d <= 0 {
			// Already due yet still pending: Step could not act on it (a
			// tooltip held open by a selection drag). Recheck at frame
			// cadence rather than spinning on a zero timeout.
			d = frameInterval
		}
		if d < wait {
			wait = d
		}
	}
	consider(w.tickAt)
	consider(w.tooltipShowAt)
	if w.tooltipView != nil {
		consider(w.tooltipCloseAt)
	}
	return wait
}

// animatorsQuiet reports whether the registered animators have been ticking
// for at least animatorQuietGrace without the window painting a frame.
func (w *Window) animatorsQuiet(now time.Time) bool {
	return len(w.animators) > 0 && !w.animQuietSince.IsZero() &&
		now.Sub(w.animQuietSince) >= animatorQuietGrace
}

// noteAnimatorActivity runs at the end of every Step: a painted frame (or
// no animators at all) ends a quiet stretch, an unpainted one starts or
// extends it.
func (w *Window) noteAnimatorActivity(now time.Time) {
	if len(w.animators) == 0 || w.framePainted {
		w.animQuietSince = time.Time{}
		return
	}
	if w.animQuietSince.IsZero() {
		w.animQuietSince = now
		return
	}
	if w.animatorsQuiet(now) {
		warnQuietAnimators(w.animators)
	}
}

// quietAnimatorTypes dedups the quiet-animator warning per concrete type.
var quietAnimatorTypes sync.Map

func warnQuietAnimators(animators []Animator) {
	for _, a := range animators {
		name := fmt.Sprintf("%T", a)
		if _, seen := quietAnimatorTypes.LoadOrStore(name, struct{}{}); seen {
			continue
		}
		log.Printf("qui: animator %s ticks without ever painting; polling it every %v instead of every frame. "+
			"Return done=true once it is finished; hand work from goroutines to the UI with Window.PostJob, "+
			"and wait for a moment with Window.RequestTickAt.", name, animatorQuietPoll)
	}
}
