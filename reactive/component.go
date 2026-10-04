package reactive

// Component declares a reusable, independently-stateful component.
//
//	func Counter(start int) reactive.Element {
//	    return reactive.Component("Counter", "", start, func(start int) reactive.Element {
//	        count, set := reactive.UseState(start)
//	        return ui.Button(fmt.Sprintf("count: %d", count)).
//	            OnClick(func() { set(count + 1) }).Build()
//	    })
//	}
//
// name groups instances of the same component type; key disambiguates
// sibling instances in a list (same role as a React key). props is the
// component's input. Component intentionally does NOT memoize: when a
// parent re-renders, the component renders again and receives fresh callback
// closures. Use MemoComponent only when the caller can state a safe, typed
// equality rule for its props.
//
// The render closure may call UseState / UseMemo / UseEffect / UseRef.
// Those hooks are stored on this instance's own fiber, so every Component
// instance keeps independent state — the capability the flat single-hook-
// list design could not provide.
func Component[P any](name, key string, props P, render func(P) Element) Element {
	if render == nil {
		panic("reactive: Component requires a render function")
	}
	return Element{
		Kind:  "component:" + name,
		Key:   key,
		props: props,
		render: func() Element {
			return render(props)
		},
	}
}

// MemoComponent is Component with explicit, opt-in memoization. equal is
// called with the previously rendered props and the next props; returning
// true skips this component's render unless it (or one of its descendants)
// is dirty.
//
// A comparator is required because Go function values have no reliable value
// equality: comparing callback code addresses can retain a closure that
// captured stale render-local state. Keep callback-relevant data in the
// comparison, or return false when in doubt.
func MemoComponent[P any](name, key string, props P, equal func(previous, next P) bool, render func(P) Element) Element {
	if equal == nil {
		panic("reactive: MemoComponent requires an equality comparator")
	}
	if render == nil {
		panic("reactive: MemoComponent requires a render function")
	}
	return Element{
		Kind:  "component:" + name,
		Key:   key,
		props: props,
		propsEqual: func(previous, next any) bool {
			var prev P
			if previous != nil {
				var ok bool
				prev, ok = previous.(P)
				if !ok {
					panic("reactive: MemoComponent props type changed")
				}
			}
			var nextProps P
			if next != nil {
				var ok bool
				nextProps, ok = next.(P)
				if !ok {
					panic("reactive: MemoComponent props type changed")
				}
			}
			return equal(prev, nextProps)
		},
		render: func() Element {
			return render(props)
		},
	}
}

// componentFiber is the per-instance state for a mounted component: its
// hook slots, the props it last rendered with, and whether a setState has
// marked it dirty since the last render (used for scoped re-render).
//
// parent is the nearest ANCESTOR component fiber (nil at the top), and
// childDirty means "some descendant fiber is dirty". Together they let a
// memo bailout stay correct: when a parent's props are unchanged, the
// reconciler skips the parent's render but still descends to re-render
// dirty descendants (visitDirty) instead of freezing them — React's
// childLanes mechanism in miniature.
type componentFiber struct {
	host   hookHost
	props  any
	render func() Element
	// propsEqual is nil for ordinary Component. MemoComponent supplies the
	// explicit comparison that enables bailout.
	propsEqual func(previous, next any) bool
	parent     *componentFiber
	dirty      bool
	// childDirty is set on every ancestor when a fiber is marked dirty.
	// The mark walk never early-outs (a cleared ancestor above an
	// already-set one would break the chain for the next pass).
	childDirty bool
	// contextSubs lists the providers this fiber read via Context.Use,
	// so unmount can drop the reverse subscriptions.
	contextSubs []*providerState
}

// renderFiber runs a component instance's render under its own hook scope
// and returns the produced subtree Element. Effects queued during the run
// land on the runtime's post-commit queue; cleanups for hooks that were
// dropped this pass are collected onto the reconcile pass.
func renderFiber(pass *reconcilePass, fiber *componentFiber) Element {
	rt := pass.runtime
	if rt == nil {
		panic("reactive: components require a Runtime (use reactive.Mount / NewRuntime)")
	}
	scope := &renderScope{
		runtime: rt,
		host:    &fiber.host,
		fiber:   fiber,
		pass:    pass,
		requestRender: func() {
			rt.mu.Lock()
			fiber.dirty = true
			for p := fiber.parent; p != nil; p = p.parent {
				p.childDirty = true
			}
			rt.mu.Unlock()
			rt.RequestRender()
		},
	}
	fiber.host.begin()
	pushScope(scope)
	elem := fiber.render()
	popScope()
	pass.cleanups = append(pass.cleanups, fiber.host.finish()...)
	return elem
}
