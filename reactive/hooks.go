package reactive

import "fmt"

type hookKind uint8

const (
	hookKindState hookKind = iota + 1
	hookKindMemo
	hookKindEffect
)

func (k hookKind) String() string {
	switch k {
	case hookKindState:
		return "state"
	case hookKindMemo:
		return "memo"
	case hookKindEffect:
		return "effect"
	default:
		return "unknown"
	}
}

type hookSlot struct {
	kind   hookKind
	state  any
	memo   memoState
	effect effectState
}

type memoState struct {
	ready   bool
	hasDeps bool
	deps    []any
	value   any
}

type effectState struct {
	hasDeps  bool
	deps     []any
	effect   func() func()
	cleanup  func()
	revision uint64
}

// effectRef points at a queued effect. host identifies which component's
// (or the root's) slot list owns it, so post-commit effect draining works
// uniformly across every component instance.
type effectRef struct {
	host     *hookHost
	index    int
	revision uint64
}

func panicHookType(expected, got hookKind) {
	panic(fmt.Sprintf("reactive: hook order changed (expected %s, got %s)", expected.String(), got.String()))
}

func cloneDeps(deps []any) []any {
	if len(deps) == 0 {
		return nil
	}
	out := make([]any, len(deps))
	copy(out, deps)
	return out
}

// depsEqual compares hook dependency arrays with valuesEqual. Non-nil
// function values are deliberately unequal: Go cannot compare closure
// captures, so an effect or memo depending on a fresh callback re-runs
// instead of retaining stale state.
func depsEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !valuesEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// setHostState commits a new state value into a specific host slot and
// triggers a re-render via the captured requestRender. Safe to call from
// any goroutine (e.g. an event handler) — it locks the runtime mutex.
func setHostState(rt *Runtime, host *hookHost, idx int, next any, requestRender func()) {
	rt.mu.Lock()
	if idx < 0 || idx >= len(host.hooks) || host.hooks[idx].kind != hookKindState {
		rt.mu.Unlock()
		return
	}
	if valuesEqual(host.hooks[idx].state, next) {
		rt.mu.Unlock()
		return
	}
	host.hooks[idx].state = next
	rt.mu.Unlock()
	if requestRender != nil {
		requestRender()
	} else {
		rt.RequestRender()
	}
}

// UseState creates render-persistent state and returns [value, set].
func UseState[T any](initial T) (T, func(T)) {
	value, set, _ := UseStateFn(initial)
	return value, set
}

// UseStateFn is UseState with an additional functional updater.
//
// update(fn) applies fn to the latest committed value, which avoids stale
// closure issues when multiple updates are batched in one frame.
func UseStateFn[T any](initial T) (value T, set func(T), update func(func(T) T)) {
	scope := requireScope()
	rt := scope.runtime
	host := scope.host
	requestRender := scope.requestRender
	idx, fresh := scope.nextHookIndex(hookKindState)

	rt.mu.Lock()
	if fresh {
		host.hooks[idx].state = initial
	}
	value, ok := host.hooks[idx].state.(T)
	rt.mu.Unlock()
	if !ok {
		panic(fmt.Sprintf("reactive: state hook type mismatch at index %d", idx))
	}

	set = func(next T) {
		setHostState(rt, host, idx, next, requestRender)
	}

	update = func(fn func(T) T) {
		if fn == nil {
			return
		}
		rt.mu.Lock()
		if idx < 0 || idx >= len(host.hooks) || host.hooks[idx].kind != hookKindState {
			rt.mu.Unlock()
			return
		}
		current, ok := host.hooks[idx].state.(T)
		if !ok {
			rt.mu.Unlock()
			panic(fmt.Sprintf("reactive: state hook type mismatch at index %d", idx))
		}
		nextVal := fn(current)
		if valuesEqual(current, nextVal) {
			rt.mu.Unlock()
			return
		}
		host.hooks[idx].state = nextVal
		rt.mu.Unlock()
		if requestRender != nil {
			requestRender()
		} else {
			rt.RequestRender()
		}
	}
	return value, set, update
}

// UseRef returns a stable mutable handle that survives re-renders and does
// NOT trigger a render when mutated — the React useRef escape hatch for
// holding widget handles, timers, or any value you want to persist without
// driving the UI. The returned *T is the same pointer on every render.
func UseRef[T any](initial T) *T {
	scope := requireScope()
	rt := scope.runtime
	host := scope.host
	idx, fresh := scope.nextHookIndex(hookKindState)

	rt.mu.Lock()
	if fresh {
		host.hooks[idx].state = &initial
	}
	ref, ok := host.hooks[idx].state.(*T)
	rt.mu.Unlock()
	if !ok {
		panic(fmt.Sprintf("reactive: ref hook type mismatch at index %d", idx))
	}
	return ref
}

// UseMemo memoizes a computed value by dependency array.
//
// When deps are omitted, compute runs every render.
func UseMemo[T any](compute func() T, deps ...any) T {
	if compute == nil {
		panic("reactive: UseMemo requires compute function")
	}
	scope := requireScope()
	rt := scope.runtime
	host := scope.host
	idx, _ := scope.nextHookIndex(hookKindMemo)
	hasDeps := len(deps) > 0

	rt.mu.Lock()
	memo := host.hooks[idx].memo
	needsCompute := !memo.ready || !hasDeps || !memo.hasDeps || !depsEqual(memo.deps, deps)
	if !needsCompute {
		value, ok := memo.value.(T)
		rt.mu.Unlock()
		if !ok {
			panic(fmt.Sprintf("reactive: memo hook type mismatch at index %d", idx))
		}
		return value
	}
	rt.mu.Unlock()

	computed := compute()

	rt.mu.Lock()
	host.hooks[idx].memo = memoState{
		ready:   true,
		hasDeps: hasDeps,
		deps:    cloneDeps(deps),
		value:   computed,
	}
	rt.mu.Unlock()
	return computed
}

// UseCallback returns a stable function identity that only changes when
// deps change — useful to keep an OnClick handler referentially stable so
// memoized children don't re-render. Thin wrapper over UseMemo.
func UseCallback[F any](fn F, deps ...any) F {
	return UseMemo(func() F { return fn }, deps...)
}

// UseEffect runs a side effect after commit.
//
// When deps are omitted, effect runs after every commit.
func UseEffect(effect func() func(), deps ...any) {
	if effect == nil {
		return
	}
	useEffectInternal(effect, len(deps) > 0, deps)
}

// UseEffectOnce runs an effect only on initial mount and cleanup on unmount.
func UseEffectOnce(effect func() func()) {
	if effect == nil {
		return
	}
	useEffectInternal(effect, true, nil)
}

func useEffectInternal(effect func() func(), hasDeps bool, deps []any) {
	scope := requireScope()
	rt := scope.runtime
	host := scope.host
	idx, fresh := scope.nextHookIndex(hookKindEffect)

	rt.mu.Lock()
	slot := &host.hooks[idx]
	run := fresh
	if !fresh {
		prev := slot.effect
		if !hasDeps {
			run = true
		} else if !prev.hasDeps || !depsEqual(prev.deps, deps) {
			run = true
		}
	}
	slot.effect.effect = effect
	slot.effect.hasDeps = hasDeps
	slot.effect.deps = cloneDeps(deps)
	if run {
		slot.effect.revision++
		rt.pendingEffects = append(rt.pendingEffects, effectRef{
			host:     host,
			index:    idx,
			revision: slot.effect.revision,
		})
	}
	rt.mu.Unlock()
}

// runPendingEffects drains effects queued during the flush pass, across
// every component instance that rendered. Cleanups from the previous run
// fire before the new effect, matching React's effect lifecycle.
func (r *Runtime) runPendingEffects() {
	type queuedEffect struct {
		host     *hookHost
		index    int
		revision uint64
		effect   func() func()
		cleanup  func()
	}

	r.mu.Lock()
	if len(r.pendingEffects) == 0 {
		r.mu.Unlock()
		return
	}
	refs := append([]effectRef(nil), r.pendingEffects...)
	r.pendingEffects = r.pendingEffects[:0]
	queue := make([]queuedEffect, 0, len(refs))
	for _, ref := range refs {
		host := ref.host
		if host == nil || ref.index < 0 || ref.index >= len(host.hooks) {
			continue
		}
		slot := &host.hooks[ref.index]
		if slot.kind != hookKindEffect || slot.effect.effect == nil {
			continue
		}
		queue = append(queue, queuedEffect{
			host:     host,
			index:    ref.index,
			revision: ref.revision,
			effect:   slot.effect.effect,
			cleanup:  slot.effect.cleanup,
		})
		slot.effect.cleanup = nil // consumed; new cleanup installed below
	}
	r.mu.Unlock()

	for _, item := range queue {
		if item.cleanup != nil {
			item.cleanup()
		}
		newCleanup := item.effect()
		if newCleanup == nil {
			continue
		}
		r.mu.Lock()
		if item.index < len(item.host.hooks) &&
			item.host.hooks[item.index].kind == hookKindEffect &&
			item.host.hooks[item.index].effect.revision == item.revision {
			item.host.hooks[item.index].effect.cleanup = newCleanup
			r.mu.Unlock()
			continue
		}
		r.mu.Unlock()
		newCleanup()
	}
}

// UseReducer is UseState driven through a reducer — the React pattern
// for state whose transitions are a closed set of actions:
//
//	type action struct{ kind string; n int }
//	state, dispatch := reactive.UseReducer(func(s model, a action) model {
//	    switch a.kind { ... }
//	}, initialModel)
//
// dispatch always reduces against the LATEST committed state (it rides
// UseStateFn's functional updater), so it is burst-safe like React's
// dispatch.
func UseReducer[S, A any](reducer func(S, A) S, initial S) (S, func(A)) {
	if reducer == nil {
		panic("reactive: UseReducer requires a reducer function")
	}
	state, _, update := UseStateFn(initial)
	dispatch := func(action A) {
		update(func(prev S) S { return reducer(prev, action) })
	}
	return state, dispatch
}
