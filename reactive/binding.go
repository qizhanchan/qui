package reactive

import (
	"sync/atomic"

	"github.com/qizhanchan/qui"
)

// This file is the "Solid engine" fast path: signal → widget setter,
// with NO render and NO reconcile in between. A bound property updates
// the retained widget directly and rides qui's dirty-region
// invalidation, so a 60 Hz counter costs one setter call + one small
// repaint per tick — the VDOM layer never runs.
//
// Rules of use:
//   - Signal identity must be stable for the widget's lifetime: create
//     signals outside render, or inside a component via UseSignal.
//     Rebinding a DIFFERENT signal to the same mounted widget is not
//     supported — change the element's Key to remount instead.
//   - apply runs on the window's main goroutine (via PostJob) once the
//     widget is attached; before attachment it runs inline.

// Map derives a read-only signal by transforming src. The derived
// signal updates (and notifies) whenever src changes to a value that
// maps to a different output.
//
// Call Dispose on the returned signal when the derived value's owner goes
// away (for example, a temporary page or dynamically mounted component).
func Map[T, U any](src *Signal[T], fn func(T) U) *Signal[U] {
	if src == nil {
		panic("reactive: Map requires a source signal")
	}
	if fn == nil {
		panic("reactive: Map requires a mapping function")
	}
	out := NewSignal(fn(src.Get()))
	var disposed atomic.Bool
	unsub := src.Subscribe(func() {
		if disposed.Load() {
			return
		}
		out.Set(fn(src.Get()))
	})
	out.setDispose(func() {
		disposed.Store(true)
		unsub()
	})
	return out
}

// Watchable is anything with Subscribe — every Signal[T] satisfies it,
// so Computed can depend on signals of mixed value types.
type Watchable interface {
	Subscribe(fn func()) func()
}

// Computed derives a signal from multiple sources.
//
// With NO explicit deps, dependencies are tracked AUTOMATICALLY: every
// Signal.Get executed by compute registers that signal, and the set is
// re-collected on each run — a computed behind an `if` only listens to
// the branch it actually took. Use Signal.Peek inside compute to read
// without depending.
//
//	total := reactive.Computed(func() string {
//	    return fmt.Sprintf("%d of %d", done.Get(), all.Get())
//	})
//
// With explicit deps the compute runs exactly on those notifications
// and no Get interception applies — pick this when compute reads
// signals you deliberately don't want to track.
//
// Either way, equal results skip notification (Signal.Set semantics). Call
// Dispose on the returned signal when its derived lifetime ends; disposal
// releases every source subscription, including dynamically tracked ones.
func Computed[T any](compute func() T, deps ...Watchable) *Signal[T] {
	if compute == nil {
		panic("reactive: Computed requires a compute function")
	}
	if len(deps) == 0 {
		return newAutoComputation(compute)
	}
	out := NewSignal(compute())
	var disposed atomic.Bool
	recompute := func() {
		if !disposed.Load() {
			out.Set(compute())
		}
	}
	unsubs := make([]func(), 0, len(deps))
	for _, dep := range deps {
		if dep != nil {
			unsubs = append(unsubs, dep.Subscribe(recompute))
		}
	}
	out.setDispose(func() {
		disposed.Store(true)
		for _, unsub := range unsubs {
			unsub()
		}
	})
	return out
}

// UseSignal returns a per-component-instance signal, created on first
// render and stable afterwards — the hook-shaped way to get a signal
// whose identity satisfies BindWidget's stability rule.
//
// Unlike UseState, setting a UseSignal value does NOT re-render the
// component; only widgets bound to it update.
func UseSignal[T any](initial T) *Signal[T] {
	scope := requireScope()
	rt := scope.runtime
	host := scope.host
	idx, fresh := scope.nextHookIndex(hookKindState)

	rt.mu.Lock()
	if fresh {
		host.hooks[idx].state = NewSignal(initial)
	}
	sig, ok := host.hooks[idx].state.(*Signal[T])
	rt.mu.Unlock()
	if !ok {
		panic("reactive: UseSignal type mismatch — hook order changed?")
	}
	return sig
}

// BindWidget applies sig's value to a widget property now and on every
// future change, marshaled onto the widget's window main goroutine.
// Returns the unbind function — callers running inside a ui builder
// get this wired to Element.Destroy automatically.
//
// apply is responsible for invalidation; most qui setters (SetText,
// SetEnabled) self-invalidate, style mutations should pair with
// InvalidateRect.
func BindWidget[T any](w qui.Widget, sig *Signal[T], apply func(T)) (unbind func()) {
	if w == nil || sig == nil || apply == nil {
		return func() {}
	}
	apply(sig.Get())

	// Window() lives on BaseWidget, not the Widget interface.
	windowed, _ := w.(interface{ Window() *qui.Window })

	// pending coalesces bursts: N sets between two frames queue ONE job
	// which reads the latest value at run time.
	var pending atomic.Bool
	return sig.Subscribe(func() {
		var win *qui.Window
		if windowed != nil {
			win = windowed.Window()
		}
		if win == nil {
			// Not attached yet (mount in progress) or already detached
			// — apply inline; a detached widget has no frame loop to
			// race with.
			apply(sig.Get())
			return
		}
		if !pending.CompareAndSwap(false, true) {
			return
		}
		win.PostJob(func() {
			pending.Store(false)
			apply(sig.Get())
		})
	})
}
