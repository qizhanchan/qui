package reactive

import (
	"sync"
	"time"

	"github.com/qizhanchan/qui"
)

const maxFlushPasses = 32

// Runtime binds a declarative render function to a qui.Window.
type Runtime struct {
	mu         sync.Mutex
	window     *qui.Window
	reconciler Reconciler
	renderFn   func() Element

	pending   bool
	scheduled bool
	rendering bool

	// scheduleFn enqueues one animator tick. Return false when scheduling
	// is unavailable so Runtime can fall back to immediate Flush.
	scheduleFn func(animator qui.Animator) bool

	// Hook state for the root render function. Each component instance
	// owns its own hookHost (see fiber.go); rootHost is the top-level one.
	rootHost       hookHost
	pendingEffects []effectRef
	// pendingLayoutEffects wait for the window's next layout pass
	// (UseLayoutEffect); layoutScheduled says an AfterLayout is queued.
	pendingLayoutEffects []effectRef
	layoutScheduled      bool

	// hostData carries backend-specific state for the host layer driving
	// this runtime (e.g. reactive/html stores its *htmlcss.StyleEngine
	// here so element Create hooks resolve the right engine per runtime —
	// a package-level global would break with multiple windows).
	hostData any
}

// currentRT tracks the runtime whose render / reconcile / scoped-sync
// pass is executing right now, so host layers can resolve per-runtime
// state (HostData) from inside Create/Update hooks without a global.
// Saved/restored around each pass; guarded because tests may drive
// different runtimes from different goroutines.
var (
	// runtimePassMu makes the process-global CurrentRuntime and hook scope
	// stack safe across Runtime instances. The underlying qui UI loop is
	// single-threaded, but tests and callers may drive separate runtimes from
	// separate goroutines; a mutex protecting only individual reads/writes of
	// currentRT would still let those passes overwrite each other's context.
	runtimePassMu sync.Mutex
	currentRTMu   sync.Mutex
	currentRT     *Runtime
)

func setCurrentRuntime(r *Runtime) (prev *Runtime) {
	currentRTMu.Lock()
	prev, currentRT = currentRT, r
	currentRTMu.Unlock()
	return prev
}

// CurrentRuntime returns the runtime executing the current render,
// reconcile, or scoped structural sync — nil outside any pass. Element
// hook closures (Create/Update/SetChildren) always run inside a pass.
func CurrentRuntime() *Runtime {
	currentRTMu.Lock()
	defer currentRTMu.Unlock()
	return currentRT
}

// SetHostData attaches host-layer state to the runtime (see hostData).
func (r *Runtime) SetHostData(v any) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.hostData = v
	r.mu.Unlock()
}

// HostData returns the value set by SetHostData, or nil.
func (r *Runtime) HostData() any {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hostData
}

// hostStyleFlusher is an optional capability of the host backend attached
// via SetHostData: apply any pending, coalesced host-side styling right now.
// The htmlcss StyleEngine implements it — its restyle normally lands on the
// NEXT frame's job queue (it's scheduled from inside the current job drain),
// which would let one unstyled frame paint whenever a pass mounts fresh
// elements (a visible flash, e.g. a filtered list growing back). Flushing it
// in the same pass, before the window is invalidated, styles the new widgets
// before they paint. Backends without deferred styling simply don't implement
// it, so flushHostStyles is a no-op for them.
type hostStyleFlusher interface{ FlushPendingStyles() }

// flushHostStyles applies the host backend's pending styling synchronously,
// if it supports it. Call on the main goroutine at the end of a reconcile /
// bound-sync pass, right before invalidating the window.
func (r *Runtime) flushHostStyles() {
	if r == nil {
		return
	}
	if fh, ok := r.HostData().(hostStyleFlusher); ok {
		fh.FlushPendingStyles()
	}
}

// hostRootAdopter is an optional capability of the host backend: learn the
// tree root the moment the first pass produces it. The htmlcss StyleEngine
// implements it so h.Mount's very first pass is styled before that pass's
// effects run — an effect reading a custom element's hosted widget or its
// ComputedStyle on first mount would otherwise see nothing.
type hostRootAdopter interface{ AdoptRoot(qui.Widget) }

// adoptHostRoot hands root to the host backend, if it wants it.
func (r *Runtime) adoptHostRoot(root qui.Widget) {
	if a, ok := r.HostData().(hostRootAdopter); ok && root != nil {
		a.AdoptRoot(root)
	}
}

// NewRuntime creates a runtime without rendering immediately.
func NewRuntime(window *qui.Window, renderFn func() Element) *Runtime {
	r := &Runtime{window: window, renderFn: renderFn}
	r.scheduleFn = r.defaultSchedule
	r.reconciler.runtime = r
	registerRuntime(window, r)
	return r
}

// Mount creates a runtime and performs the initial render.
func Mount(window *qui.Window, renderFn func() Element) *Runtime {
	r := NewRuntime(window, renderFn)
	r.Render()
	return r
}

func (r *Runtime) defaultSchedule(animator qui.Animator) bool {
	if r == nil || animator == nil || r.window == nil || r.window.IsHeadless() {
		return false
	}
	r.window.RegisterAnimator(animator)
	return true
}

// SetRender updates the render function used by Render/RequestRender.
func (r *Runtime) SetRender(renderFn func() Element) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.renderFn = renderFn
	r.mu.Unlock()
}

// Root returns the current concrete root widget.
func (r *Runtime) Root() qui.Widget {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reconciler.RootWidget()
}

// Profile returns reconcile profiler metrics.
func (r *Runtime) Profile() ProfilerSnapshot {
	if r == nil {
		return ProfilerSnapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reconciler.Profile()
}

// DebugTree returns the current reconciler tree snapshot.
func (r *Runtime) DebugTree() *DebugNode {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reconciler.DebugTree()
}

// DebugTreeString renders the current debug tree into an indented string.
func (r *Runtime) DebugTreeString() string {
	n := r.DebugTree()
	if n == nil {
		return "<empty>"
	}
	return n.String()
}

// Render reconciles immediately (synchronous flush).
func (r *Runtime) Render() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.pending = true
	r.mu.Unlock()
	r.Flush()
}

// RequestRender batches updates and schedules one render on the next frame.
func (r *Runtime) RequestRender() {
	if r == nil {
		return
	}

	r.mu.Lock()
	r.pending = true
	if r.rendering {
		r.mu.Unlock()
		return
	}
	if r.scheduled {
		r.mu.Unlock()
		return
	}
	schedule := r.scheduleFn
	if schedule == nil {
		r.mu.Unlock()
		r.Flush()
		return
	}
	r.scheduled = true
	scheduler := &runtimeScheduler{runtime: r}
	r.mu.Unlock()

	if !schedule(scheduler) {
		r.mu.Lock()
		r.scheduled = false
		r.mu.Unlock()
		r.Flush()
	}
}

// Flush forces all pending renders to run now.
func (r *Runtime) Flush() {
	if r == nil {
		return
	}
	for i := 0; i < maxFlushPasses; i++ {
		r.mu.Lock()
		if !r.pending || r.rendering {
			r.mu.Unlock()
			return
		}
		r.pending = false
		r.rendering = true
		r.mu.Unlock()

		r.flushOnePass()

		r.mu.Lock()
		r.rendering = false
		more := r.pending
		r.mu.Unlock()
		if !more {
			return
		}
	}
	panic("reactive: exceeded max flush passes; probable render loop")
}

func (r *Runtime) flushOnePass() {
	var removedCleanups []func()

	// CurrentRuntime and scopeStack are process-global compatibility context
	// for host hooks. Keep the COMPLETE render/reconcile pass exclusive, not
	// just assignments to currentRT, or concurrent runtimes can resolve the
	// other window's host backend.
	runtimePassMu.Lock()
	func() {
		defer runtimePassMu.Unlock()

		prev := setCurrentRuntime(r)
		defer setCurrentRuntime(prev)

		r.mu.Lock()
		renderFn := r.renderFn
		window := r.window
		if renderFn == nil {
			r.mu.Unlock()
			return
		}
		// Reset the post-commit effect queue once per pass; every component
		// that renders this pass appends to it.
		r.pendingEffects = r.pendingEffects[:0]
		r.mu.Unlock()

		// Root render + reconcile run WITHOUT r.mu held: hooks (and the
		// component renders triggered during reconcile) lock r.mu themselves,
		// so holding it here would deadlock. The rendering=true gate set by
		// Flush already serializes passes against each other.
		r.rootHost.begin()
		pushScope(&renderScope{runtime: r, host: &r.rootHost, requestRender: r.RequestRender})
		element := renderFn()
		popScope()
		removedCleanups = r.rootHost.finish()

		root, flags := r.reconciler.Render(element)
		removedCleanups = append(removedCleanups, r.reconciler.TakeUnmountCleanups()...)

		if window != nil {
			// Style any freshly-mounted host elements now (same job) so they
			// don't paint one unstyled frame before their deferred restyle.
			r.flushHostStyles()
			if root != window.Root() {
				window.SetRoot(root)
			} else if flags.has(FlagLayout) {
				window.InvalidateLayout()
				window.Invalidate()
			} else if flags.has(FlagPaint) {
				window.Invalidate()
			}
			// After SetRoot, so the tree is window-attached when the host
			// first styles it (a <select> needs its window), and before
			// this pass's effects run below.
			r.adoptHostRoot(root)
		}
	}()

	// Effects and destroy callbacks run after the host commit, but OUTSIDE
	// the process-global render context. An effect may synchronously render
	// another Runtime without deadlocking the context gate above.
	for _, cleanup := range removedCleanups {
		if cleanup != nil {
			cleanup()
		}
	}
	r.runPendingEffects()
	r.scheduleLayoutEffects()
}

// scheduleLayoutEffects arranges for queued layout effects to run after
// the window's next layout pass. State they set is flushed right there, so
// the re-render (and the relayout it causes) lands before paint.
func (r *Runtime) scheduleLayoutEffects() {
	r.mu.Lock()
	if len(r.pendingLayoutEffects) == 0 || r.layoutScheduled {
		r.mu.Unlock()
		return
	}
	window := r.window
	if window != nil {
		r.layoutScheduled = true
	}
	r.mu.Unlock()
	if window == nil {
		r.runLayoutEffects()
		return
	}
	window.AfterLayout(func() {
		r.mu.Lock()
		r.layoutScheduled = false
		r.mu.Unlock()
		r.runLayoutEffects()
		r.Flush()
	})
}

// Bind subscribes a signal and triggers re-render on value changes.
func (r *Runtime) Bind(signal interface{ Subscribe(func()) func() }) func() {
	if r == nil || signal == nil {
		return func() {}
	}
	return signal.Subscribe(func() {
		r.RequestRender()
	})
}

type runtimeScheduler struct {
	runtime *Runtime
}

func (s *runtimeScheduler) Tick(time.Time) (qui.Rect, bool) {
	if s == nil || s.runtime == nil {
		return qui.Rect{}, true
	}
	s.runtime.Flush()

	s.runtime.mu.Lock()
	s.runtime.scheduled = false
	needsReschedule := s.runtime.pending && !s.runtime.rendering
	s.runtime.mu.Unlock()

	if needsReschedule {
		s.runtime.RequestRender()
	}
	return qui.Rect{}, true
}

func (s *runtimeScheduler) Stop() {}
