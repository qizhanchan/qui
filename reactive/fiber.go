package reactive

import "sync"

// hookHost stores the positional hook slots for ONE render scope — either
// the Runtime's root render function or a single component instance.
//
// Giving every component instance its own hookHost is the change that
// makes UseState / UseMemo / UseEffect state isolated per instance, which
// is what lets you write a reusable stateful component (a Counter) and
// drop several of them into a list without their state colliding. In the
// pre-component design every hook shared one list on the Runtime, so
// there was effectively a single component.
type hookHost struct {
	hooks      []hookSlot
	hookCursor int
	// dead marks a host whose component unmounted; effects still queued
	// for it (a layout effect waiting for the next layout) are dropped.
	dead bool
}

// begin resets the cursor at the start of a render pass.
func (h *hookHost) begin() { h.hookCursor = 0 }

// finish trims slots past the cursor (hooks not called this pass, e.g. a
// conditional UseEffect that stopped running) and returns their effect
// cleanups so the caller can invoke them after commit.
func (h *hookHost) finish() []func() {
	if h.hookCursor >= len(h.hooks) {
		return nil
	}
	removed := h.hooks[h.hookCursor:]
	var cleanups []func()
	for _, slot := range removed {
		if slot.kind == hookKindEffect && slot.effect.cleanup != nil {
			cleanups = append(cleanups, slot.effect.cleanup)
		}
	}
	h.hooks = h.hooks[:h.hookCursor]
	return cleanups
}

// cleanupAll returns every live effect cleanup — used when the whole host
// is torn down (component unmount) so effects release their resources.
func (h *hookHost) cleanupAll() []func() {
	h.dead = true
	var cleanups []func()
	for _, slot := range h.hooks {
		if slot.kind == hookKindEffect && slot.effect.cleanup != nil {
			cleanups = append(cleanups, slot.effect.cleanup)
		}
	}
	return cleanups
}

// renderScope is the active hook context while a render function runs.
// host is the slot store hooks read/write; requestRender re-renders the
// owner (just this component for a fiber, or the whole runtime for the
// root). State setters capture requestRender so a setState fired later
// from an event handler schedules the right re-render.
type renderScope struct {
	runtime       *Runtime
	host          *hookHost
	requestRender func()
	// fiber is the component instance owning this render, nil for the
	// Runtime's root render function. Context subscriptions attach here.
	fiber *componentFiber
	// pass is the reconcile pass this render runs inside — carries the
	// provider stacks Context.Use reads. Nil for the root render
	// function, which runs before reconciliation starts.
	pass *reconcilePass
}

// Render is single-threaded per process (the Runtime serializes flushes),
// but components nest: rendering a parent and then reconciling its subtree
// renders the children. The stack tracks that nesting so each hook call
// resolves to the component currently rendering.
var (
	scopeMu    sync.Mutex
	scopeStack []*renderScope
)

func pushScope(s *renderScope) {
	scopeMu.Lock()
	scopeStack = append(scopeStack, s)
	scopeMu.Unlock()
}

func popScope() {
	scopeMu.Lock()
	if n := len(scopeStack); n > 0 {
		scopeStack = scopeStack[:n-1]
	}
	scopeMu.Unlock()
}

func currentScope() *renderScope {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	if n := len(scopeStack); n > 0 {
		return scopeStack[n-1]
	}
	return nil
}

func requireScope() *renderScope {
	s := currentScope()
	if s == nil {
		panic("reactive: hooks must be called during a Runtime render")
	}
	return s
}

// renderScopeDepth / trimRenderScopes support ErrorBoundary: a panic
// mid-render leaves pushed scopes behind; the boundary trims back to
// its own depth before rendering the fallback.
func renderScopeDepth() int {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	return len(scopeStack)
}

func trimRenderScopes(depth int) {
	scopeMu.Lock()
	if len(scopeStack) > depth {
		scopeStack = scopeStack[:depth]
	}
	scopeMu.Unlock()
}

// nextHookIndex advances the active host's cursor, allocating a fresh slot
// on first encounter. Locks the runtime mutex because state setters on
// other goroutines mutate the same slice.
func (s *renderScope) nextHookIndex(kind hookKind) (idx int, fresh bool) {
	rt := s.runtime
	rt.mu.Lock()
	h := s.host
	idx = h.hookCursor
	h.hookCursor++
	if idx >= len(h.hooks) {
		h.hooks = append(h.hooks, hookSlot{kind: kind})
		fresh = true
	}
	got := h.hooks[idx].kind
	rt.mu.Unlock()
	if !fresh && got != kind {
		panicHookType(kind, got)
	}
	return idx, fresh
}
