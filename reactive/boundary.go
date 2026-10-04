package reactive

// ErrorBoundary catches panics raised while rendering or reconciling
// its child subtree and shows a fallback instead — without one, a
// panicking component render kills the whole process (this is a Go GUI,
// there is no browser to absorb it).
//
//	reactive.ErrorBoundary("editor", editorSubtree,
//	    func(err any, retry func()) reactive.Element {
//	        return ui.VBox().Children(
//	            ui.Label(fmt.Sprintf("editor crashed: %v", err)),
//	            ui.Button("Retry").OnClick(retry),
//	        ).Build()
//	    })
//
// Once tripped, the boundary keeps rendering the fallback until retry
// is called (which clears the error and re-renders the original child).
// Panics inside the FALLBACK itself propagate — a broken fallback is a
// programming error, not something to swallow.
//
// Scope: render/reconcile-time panics only. Panics in event handlers
// and effects run outside the reconcile pass and are not intercepted.
//
// Limitation (documented, not fixed): widgets mounted BEFORE the panic
// in the same pass are discarded unmounted, so effects they queued may
// run once without their cleanup ever firing. Keep renders side-effect
// free and this is invisible.
func ErrorBoundary(key string, child Element, fallback func(err any, retry func()) Element) Element {
	if fallback == nil {
		panic("reactive: ErrorBoundary requires a fallback function")
	}
	return Element{
		Kind:     "#boundary",
		Key:      key,
		boundary: &boundaryData{child: child, fallback: fallback},
	}
}

type boundaryData struct {
	child    Element
	fallback func(err any, retry func()) Element
}

// boundaryState is the mounted boundary's error latch.
type boundaryState struct {
	err     any
	tripped bool
}

// reconcileBoundaryChild reconciles the boundary's ACTIVE element
// (child, or fallback when tripped) with panic capture. Returns the new
// child instance; on a fresh panic it flips the latch and re-runs with
// the fallback.
func reconcileBoundaryChild(node *instance, data *boundaryData, pass *reconcilePass) Flags {
	rt := pass.runtime

	retry := func() {
		if node.boundary != nil {
			node.boundary.tripped = false
			node.boundary.err = nil
		}
		if rt != nil {
			rt.RequestRender()
		}
	}

	elem := data.child
	if node.boundary.tripped {
		elem = data.fallback(node.boundary.err, retry)
	}

	child, flags, panicked, err := reconcileGuarded(node.child, elem, pass)
	if panicked {
		if node.boundary.tripped {
			// Fallback itself panicked — do not loop, propagate.
			panic(err)
		}
		node.boundary.tripped = true
		node.boundary.err = err
		// The partially-built subtree was never attached; drop the old
		// child reference and mount the fallback fresh.
		if node.child != nil {
			unmountInstance(node.child, pass)
			node.child = nil
		}
		fbElem := data.fallback(err, retry)
		fb, fbFlags := reconcileNode(nil, fbElem, pass)
		node.child = fb
		return fbFlags | FlagLayout
	}
	node.child = child
	return flags
}

// reconcileGuarded runs reconcileNode under recover. On panic it trims
// the fiber / provider / render-scope stacks back to this frame's depth
// — the aborted subtree pushed frames it never got to pop.
func reconcileGuarded(prev *instance, elem Element, pass *reconcilePass) (child *instance, flags Flags, panicked bool, err any) {
	fiberDepth := len(pass.fiberStack)
	scopeSnapshot := renderScopeDepth()
	providerDepths := make(map[*contextKey]int, len(pass.providers))
	for key, stack := range pass.providers {
		providerDepths[key] = len(stack)
	}

	defer func() {
		if r := recover(); r != nil {
			panicked = true
			err = r
			pass.fiberStack = pass.fiberStack[:fiberDepth]
			trimRenderScopes(scopeSnapshot)
			for key, stack := range pass.providers {
				if depth := providerDepths[key]; len(stack) > depth {
					pass.providers[key] = stack[:depth]
				}
			}
		}
	}()

	child, flags = reconcileNode(prev, elem, pass)
	return child, flags, false, nil
}
