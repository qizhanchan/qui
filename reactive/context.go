package reactive

// Context carries a typed value down the tree without threading it
// through every intermediate component's props — the React
// createContext/Provider/useContext trio.
//
//	var ThemeCtx = reactive.NewContext("theme", DefaultTheme)
//
//	// somewhere high in the tree:
//	ThemeCtx.Provide(darkTheme, subtree)
//
//	// any component below, however deep:
//	theme := ThemeCtx.Use()
//
// When the provided value changes (compared with valuesEqual), every
// component that read it via Use is marked dirty and re-renders — even
// when memoized intermediates between the provider and the consumer
// bail out (the childDirty mechanism carries the re-render through).
//
// Subscriptions are cleaned up when a consumer unmounts. A consumer
// that stops calling Use (conditional read) stays subscribed until it
// unmounts — the cost is a spurious re-render, never a stale value.
type Context[T any] struct {
	key *contextKey
	def T
}

// contextKey is the identity of a Context — pointer comparison
// distinguishes two contexts even when they share a debug name.
type contextKey struct{ name string }

// providerData rides on the provider Element.
type providerData struct {
	key   *contextKey
	value any
}

// providerState is the mounted provider instance's state: the current
// value and the consumer fibers to mark dirty when it changes.
type providerState struct {
	key   *contextKey
	value any
	subs  map[*componentFiber]struct{}
}

// NewContext creates a context with a debug name (shown in the
// reconciler's debug tree as "#provider:<name>") and a default value
// returned by Use when no Provider is above the consumer.
func NewContext[T any](name string, def T) *Context[T] {
	return &Context[T]{key: &contextKey{name: name}, def: def}
}

// Provide wraps child so that every Use of this context below it
// resolves to value. Providers nest — the closest one wins.
func (c *Context[T]) Provide(value T, child Element) Element {
	return Element{
		Kind:     "#provider:" + c.key.name,
		provider: &providerData{key: c.key, value: value},
		Children: []Element{child},
	}
}

// Use reads the nearest provided value. Must be called during a
// component render (it is a hook — it also subscribes the component to
// value changes). In the Runtime's ROOT render function — which runs
// before any provider element exists — it returns the default.
func (c *Context[T]) Use() T {
	scope := requireScope()
	if scope.pass == nil {
		// Root render function: elements (including providers) are only
		// being BUILT here, nothing is mounted above us.
		return c.def
	}
	prov := scope.pass.topProvider(c.key)
	if prov == nil {
		return c.def
	}
	if scope.fiber != nil {
		subscribeFiber(scope.runtime, prov, scope.fiber)
	}
	value, ok := prov.value.(T)
	if !ok {
		// Only possible if two contexts share a key, which NewContext
		// makes impossible; defend anyway.
		return c.def
	}
	return value
}

func subscribeFiber(rt *Runtime, prov *providerState, fiber *componentFiber) {
	if rt != nil {
		rt.mu.Lock()
		defer rt.mu.Unlock()
	}
	if _, ok := prov.subs[fiber]; ok {
		return
	}
	prov.subs[fiber] = struct{}{}
	fiber.contextSubs = append(fiber.contextSubs, prov)
}

// dropSubscriptions detaches an unmounting fiber from every provider it
// read from.
func dropSubscriptions(fiber *componentFiber) {
	for _, prov := range fiber.contextSubs {
		delete(prov.subs, fiber)
	}
	fiber.contextSubs = nil
}

// markSubscribersDirty flags every consumer of a provider (and the
// childDirty chain up from each) so the in-flight reconcile pass
// re-renders them — including consumers hiding under memo bailouts.
func markSubscribersDirty(pass *reconcilePass, prov *providerState) {
	pass.runtimeLock()
	for fiber := range prov.subs {
		fiber.dirty = true
		for p := fiber.parent; p != nil; p = p.parent {
			p.childDirty = true
		}
	}
	pass.runtimeUnlock()
}

// ---- pass-scoped provider stacks --------------------------------------

func (p *reconcilePass) pushProvider(prov *providerState) {
	if p.providers == nil {
		p.providers = make(map[*contextKey][]*providerState)
	}
	p.providers[prov.key] = append(p.providers[prov.key], prov)
}

func (p *reconcilePass) popProvider(prov *providerState) {
	stack := p.providers[prov.key]
	if n := len(stack); n > 0 {
		p.providers[prov.key] = stack[:n-1]
	}
}

func (p *reconcilePass) topProvider(key *contextKey) *providerState {
	stack := p.providers[key]
	if n := len(stack); n > 0 {
		return stack[n-1]
	}
	return nil
}
