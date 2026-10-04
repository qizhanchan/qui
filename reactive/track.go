package reactive

import (
	"sync"
	"sync/atomic"
)

// Dependency auto-tracking for Computed(fn) with no explicit deps —
// Solid's model: while the compute function runs, every Signal.Get
// registers that signal as a dependency of the running computation.
//
// Concurrency contract (documented, not enforced): create computeds and
// drive their dependency signals from ONE goroutine (normally the main
// one). The implementation is race-FREE regardless — an unrelated
// goroutine calling Get during a tracked run just registers a spurious
// dependency (extra recomputes, never corruption, never a missed dep
// of that goroutine's own computations... which would require its own
// tracked run and those nest via save/restore on this same slot, so
// concurrent tracked runs on two goroutines may cross-attribute deps —
// don't do that).

// activeTracker is the currently-running computation's collector; nil
// almost always, so Signal.Get pays one atomic load on the fast path.
var activeTracker atomic.Pointer[depTracker]

// trackerMu guards mutation of the collector (and the save/restore of
// the slot) so unrelated concurrent Gets can't corrupt the dep set.
var trackerMu sync.Mutex

type depTracker struct {
	seen map[Watchable]struct{}
	deps []Watchable
}

// trackDep is called by Signal.Get. No-op unless a tracked run is
// active.
func trackDep(w Watchable) {
	if activeTracker.Load() == nil {
		return
	}
	trackerMu.Lock()
	if t := activeTracker.Load(); t != nil {
		if _, ok := t.seen[w]; !ok {
			t.seen[w] = struct{}{}
			t.deps = append(t.deps, w)
		}
	}
	trackerMu.Unlock()
}

// runTracked evaluates fn with dependency collection. Nested tracked
// runs save/restore the slot, so a computed reading another computed's
// OUTPUT registers just that output signal.
func runTracked[T any](fn func() T) (T, []Watchable) {
	t := &depTracker{seen: make(map[Watchable]struct{})}

	trackerMu.Lock()
	prev := activeTracker.Load()
	activeTracker.Store(t)
	trackerMu.Unlock()

	defer func() {
		trackerMu.Lock()
		activeTracker.Store(prev)
		trackerMu.Unlock()
	}()

	value := fn()
	return value, t.deps
}

// autoComputation re-runs fn on any dependency change and maintains the
// subscription set DYNAMICALLY: deps not read on the latest run are
// unsubscribed, newly-read ones subscribed — a computed behind an
// `if` only listens to the branch it actually took.
type autoComputation[T any] struct {
	out      *Signal[T]
	fn       func() T
	mu       sync.Mutex
	unsubs   map[Watchable]func()
	disposed bool
}

func newAutoComputation[T any](fn func() T) *Signal[T] {
	c := &autoComputation[T]{fn: fn, unsubs: make(map[Watchable]func())}
	value, deps := runTracked(fn)
	c.out = NewSignal(value)
	c.retarget(deps)
	c.out.setDispose(c.dispose)
	return c.out
}

func (c *autoComputation[T]) recompute() {
	c.mu.Lock()
	disposed := c.disposed
	c.mu.Unlock()
	if disposed {
		return
	}
	value, deps := runTracked(c.fn)
	c.retarget(deps)
	c.mu.Lock()
	disposed = c.disposed
	c.mu.Unlock()
	if disposed {
		return
	}
	c.out.Set(value)
}

// retarget diffs the live subscription set against the deps of the
// latest run.
func (c *autoComputation[T]) retarget(deps []Watchable) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disposed {
		return
	}

	current := make(map[Watchable]struct{}, len(deps))
	for _, dep := range deps {
		if dep == nil {
			continue
		}
		current[dep] = struct{}{}
		if _, ok := c.unsubs[dep]; !ok {
			c.unsubs[dep] = dep.Subscribe(c.recompute)
		}
	}
	for dep, unsub := range c.unsubs {
		if _, ok := current[dep]; !ok {
			unsub()
			delete(c.unsubs, dep)
		}
	}
}

func (c *autoComputation[T]) dispose() {
	c.mu.Lock()
	if c.disposed {
		c.mu.Unlock()
		return
	}
	c.disposed = true
	unsubs := c.unsubs
	c.unsubs = make(map[Watchable]func())
	c.mu.Unlock()
	for _, unsub := range unsubs {
		unsub()
	}
}
