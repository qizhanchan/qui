package qui

import (
	"sync"
)

// jobs.go — main-goroutine job queue.
//
// The qui main loop is pinned to one OS thread (GLFW / Cocoa
// requirement). Any external system that wants to mutate widget
// state — agent server actions, async data loaders, scheduled
// timers — must hand work back to that thread before touching the
// tree. PostJob is the canonical primitive: enqueue + wake.
//
// Design:
//   - Two FIFO lanes: normal and priority. Priority drains first.
//     Priority is for continuations (drag intermediate frames, wait
//     condition checks) that must not be starved by an avalanche of
//     normal user actions.
//   - PostJob and the framework-owned priority lane are lossless. A caller
//     that explicitly wants bounded, lossy admission uses TryPostJob; its
//     default queue limit is 1024 and rejection is reported to the caller.
//   - WakeEventLoop nudges the platform event loop so Step runs without
//     waiting out the pump timeout. Skipped in test mode (no platform
//     window) so tests don't need a real windowing backend.
//   - DrainJobs runs every Step (at the top, before Tick) and
//     consumes everything currently queued. A job that itself
//     PostJob()s schedules its child for the next Step, not this
//     one — keeps Step time bounded.
//
// PendingJobs is the count of currently-queued jobs, used by
// IdleState to detect "no work outstanding".

const defaultJobQueueLimit = 1024

type windowJobQueue struct {
	mu       sync.Mutex
	priority []func()
	normal   []func()
	inFlight int
	limit    int // 0 means defaultJobQueueLimit
}

func (q *windowJobQueue) pushNormal(fn func()) {
	if fn == nil {
		return
	}
	q.mu.Lock()
	q.normal = append(q.normal, fn)
	q.mu.Unlock()
}

func (q *windowJobQueue) tryPushNormal(fn func()) bool {
	if fn == nil {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	limit := q.limit
	if limit == 0 {
		limit = defaultJobQueueLimit
	}
	if len(q.normal) >= limit {
		return false
	}
	q.normal = append(q.normal, fn)
	return true
}

func (q *windowJobQueue) pushPriority(fn func()) {
	if fn == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.priority = append(q.priority, fn)
}

func (q *windowJobQueue) drain() (priority, normal []func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.priority) > 0 {
		priority = q.priority
		q.priority = nil
	}
	if len(q.normal) > 0 {
		normal = q.normal
		q.normal = nil
	}
	q.inFlight += len(priority) + len(normal)
	return priority, normal
}

func (q *windowJobQueue) startOne() {
	q.mu.Lock()
	if q.inFlight > 0 {
		q.inFlight--
	}
	q.mu.Unlock()
}

func (q *windowJobQueue) pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.priority) + len(q.normal) + q.inFlight
}

// PostJob enqueues fn to run at the start of the next Window.Step on
// the main goroutine. Safe to call from any goroutine. Admission is
// lossless: every non-nil job is retained until it starts running.
//
// Jobs run AFTER the previous Step's End and BEFORE the new Step's
// Tick. They may freely mutate widget state, call dispatch, push
// overlays, etc. A job that needs to schedule follow-up work should
// PostJob/PostPriorityJob again; the follow-up runs on the
// subsequent Step.
//
// On a TestWindow (no GLFW handle) jobs queue normally but the
// loop-wake is skipped. Tests drive draining explicitly via
// DrainJobsForTest.
func (w *Window) PostJob(fn func()) {
	if w == nil || fn == nil {
		return
	}
	w.jobs.pushNormal(fn)
	w.wakeLoop()
}

// TryPostJob attempts to enqueue fn on the bounded normal lane. It returns
// false without enqueueing when the configured queue limit is reached. Use
// this only for work that can be explicitly coalesced, retried, or dropped;
// state transitions and completion callbacks should use lossless PostJob.
func (w *Window) TryPostJob(fn func()) bool {
	if w == nil || fn == nil {
		return false
	}
	if !w.jobs.tryPushNormal(fn) {
		return false
	}
	w.wakeLoop()
	return true
}

// PostPriorityJob is like PostJob but the job runs ahead of any
// pending normal jobs on the next Step. Reserved for framework
// continuations (drag interpolation, wait-for-condition polling)
// where a user-posted job must not interpose between continuation
// frames. Application code should prefer PostJob.
func (w *Window) PostPriorityJob(fn func()) {
	if w == nil || fn == nil {
		return
	}
	w.jobs.pushPriority(fn)
	w.wakeLoop()
}

// PendingJobs returns the number of jobs queued or drained but not yet
// started. A priority job can therefore observe normal work that will run
// later in the same Step instead of reporting a false idle snapshot.
// Used by IdleState to detect quiescence.
func (w *Window) PendingJobs() int {
	if w == nil {
		return 0
	}
	return w.jobs.pending()
}

// SetJobQueueLimit overrides the default 1024 admission limit used by
// TryPostJob. It does not affect lossless PostJob. Passing 0 restores the
// default; a negative value is rejected.
func (w *Window) SetJobQueueLimit(n int) {
	if w == nil || n < 0 {
		return
	}
	w.jobs.mu.Lock()
	w.jobs.limit = n
	w.jobs.mu.Unlock()
}

// DrainJobsForTest runs every queued job synchronously. Intended for
// unit tests that want to drive PostJob without a Step pump. Returns
// the total number of jobs that ran.
func (w *Window) DrainJobsForTest() int {
	if w == nil {
		return 0
	}
	w.assertUIThread("Window.DrainJobsForTest")
	return w.runJobs()
}

// runJobs is the internal drain invoked at the start of Step. Runs
// priority lane first, then normal lane. New jobs posted during
// execution land in the next-Step queue, NOT this drain — keeps
// Step time bounded.
func (w *Window) runJobs() int {
	if w == nil {
		return 0
	}
	w.assertUIThread("Window.runJobs")
	pri, nor := w.jobs.drain()
	count := 0
	for _, fn := range pri {
		w.jobs.startOne()
		if fn != nil {
			fn()
			count++
		}
	}
	for _, fn := range nor {
		w.jobs.startOne()
		if fn != nil {
			fn()
			count++
		}
	}
	return count
}

func (w *Window) wakeLoop() {
	if w == nil {
		return
	}
	w.platMu.RLock()
	defer w.platMu.RUnlock()
	if w.plat == nil {
		return
	}
	// Through this window's own backend, not the process-global one: a
	// window on a different backend has a different loop to wake.
	w.plat.wake()
}

// WakeEventLoop nudges the main event loop so the next Step runs promptly
// instead of waiting out the pump timeout. Safe to call from any
// goroutine, and a no-op before any window exists.
//
// Exported because subpackages driving their own async work (media's
// playback clock) need to ask for a frame without reaching for the
// windowing library themselves — that would put a platform dependency
// back into a subpackage, which the seam exists to prevent.
func WakeEventLoop() {
	// Deliberately does NOT initialize the platform: this is callable from
	// any goroutine, and lazily bringing up a windowing backend off the
	// main thread would crash. No platform yet means no loop to wake.
	plat := initializedPlatform()
	if plat == nil {
		return
	}
	plat.wake()
}
