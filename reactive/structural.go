package reactive

import (
	"sync/atomic"

	"github.com/qizhanchan/qui"
)

// Structural signal bindings — Solid's <Show> / <For> for qui.
//
// Show and For mount/unmount SUBTREES straight from a signal change:
// the signal notifies → one coalesced main-thread job → a reconcile
// pass scoped to this node's children. The runtime's render function
// and the rest of the tree are never touched. Property bindings
// (BindWidget) cover value changes; these cover structure changes —
// together they make the reconciler an on-demand tool rather than a
// per-update tax.
//
// Both introduce one plain container widget (vertical flex, no gap, no
// padding) that owns the dynamic children — unlike ui.If/Fragment they
// do NOT splice into the parent, which is what makes the scoped sync
// possible.
//
// Inside the subtree everything stays fully featured: rows can be
// stateful components (their setState re-renders through the normal
// scoped-dirty path), read Context (the provider stacks are snapshotted
// at mount and refreshed on every regular pass), use effects (run and
// cleaned up on scoped syncs too).

// Show mounts build() while cond is true and unmounts it while false,
// driven directly by the signal.
//
//	visible := reactive.NewSignal(false)
//	...
//	reactive.Show("hint", visible, func() reactive.Element {
//	    return ui.Label("now you see me").Build()
//	})
func Show(key string, cond *Signal[bool], build func() Element) Element {
	if cond == nil || build == nil {
		panic("reactive: Show requires a condition signal and a build function")
	}
	return Element{
		Kind: "#bound:show",
		Key:  key,
		bound: &boundData{
			layout: BoundLayout{Direction: qui.Vertical},
			watch:  cond,
			build: func() []Element {
				if !cond.Get() {
					return nil
				}
				elem := build()
				if elem.isEmpty() {
					return nil
				}
				return []Element{elem}
			},
		},
	}
}

// For renders one subtree per item of the slice signal, keyed diffing
// included: give every row Element a stable Key and appends / removals
// / reorders reuse the surviving widgets.
//
//	todos := reactive.NewSignal([]todo{...})
//	...
//	reactive.For("rows", todos, func(i int, t todo) reactive.Element {
//	    return ui.Label(t.Text).Key(strconv.Itoa(t.ID)).Build()
//	})
//
// todos.Set / todos.Update re-syncs just this list — no render pass.
func For[T any](key string, items *Signal[[]T], render func(index int, item T) Element) Element {
	if items == nil || render == nil {
		panic("reactive: For requires an items signal and a render function")
	}
	return Element{
		Kind: "#bound:for",
		Key:  key,
		bound: &boundData{
			layout: BoundLayout{Direction: qui.Vertical},
			watch:  items,
			build: func() []Element {
				snapshot := items.Get()
				out := make([]Element, 0, len(snapshot))
				for i, item := range snapshot {
					out = append(out, render(i, item))
				}
				return out
			},
		},
	}
}

// BoundLayout configures the container a Show/For introduces.
//
// Direction follows qui.Direction semantics (zero value = Horizontal).
// The plain Show/For constructors default to Vertical; ShowWith/ForWith
// take the struct verbatim.
type BoundLayout struct {
	Direction qui.Direction
	Gap       float32
	Align     qui.AlignCross
	// Grow makes the container claim remaining space in ITS parent's
	// flex (CSS `flex: <grow>`).
	Grow float32
}

// ShowWith is Show with container layout options (e.g. Grow so the
// mounted subtree fills the slot).
func ShowWith(key string, layout BoundLayout, cond *Signal[bool], build func() Element) Element {
	elem := Show(key, cond, build)
	elem.bound.layout = layout
	return elem
}

// ForWith is For with container layout options (horizontal rows, gaps):
//
//	reactive.ForWith("chips", reactive.BoundLayout{Direction: qui.Horizontal, Gap: 6},
//	    chips, renderChip)
func ForWith[T any](key string, layout BoundLayout, items *Signal[[]T], render func(index int, item T) Element) Element {
	elem := For(key, items, render)
	elem.bound.layout = layout
	return elem
}

// boundData rides on the Element: the signal to watch and the closure
// producing the CURRENT child elements.
type boundData struct {
	watch  Watchable
	build  func() []Element
	layout BoundLayout
}

// boundState is the mounted node's live state.
type boundState struct {
	data  *boundData
	unsub func()

	// layout is the last-applied container layout, diffed on regular
	// passes.
	layout BoundLayout

	// Captured at mount / refreshed each regular pass so scoped syncs
	// replay the correct hook + context environment for the subtree.
	parentFiber *componentFiber
	providers   map[*contextKey][]*providerState

	syncPending atomic.Bool
}

func snapshotProviders(pass *reconcilePass) map[*contextKey][]*providerState {
	if len(pass.providers) == 0 {
		return nil
	}
	out := make(map[*contextKey][]*providerState, len(pass.providers))
	for key, stack := range pass.providers {
		if len(stack) == 0 {
			continue
		}
		out[key] = append([]*providerState(nil), stack...)
	}
	return out
}

// syncBoundNode runs the scoped reconcile for one bound node. Always on
// the window main goroutine (PostJob), which serializes it against full
// flushes and other scoped syncs.
func (r *Runtime) syncBoundNode(node *instance) {
	if r == nil || node == nil || node.bnd == nil || node.bnd.data == nil {
		return
	}
	var (
		cleanups []func()
		flags    Flags
		window   *qui.Window
	)

	// Scoped syncs also execute host Create/Update hooks and component hooks,
	// so they share the same process-global compatibility context as a full
	// render pass. Serialize the whole reconcile, not merely currentRT writes.
	runtimePassMu.Lock()
	func() {
		defer runtimePassMu.Unlock()

		prev := setCurrentRuntime(r)
		defer setCurrentRuntime(prev)
		pass := &reconcilePass{
			metrics: &RenderMetrics{},
			runtime: r,
		}
		if node.bnd.parentFiber != nil {
			pass.fiberStack = append(pass.fiberStack, node.bnd.parentFiber)
		}
		if len(node.bnd.providers) > 0 {
			pass.providers = make(map[*contextKey][]*providerState, len(node.bnd.providers))
			for key, stack := range node.bnd.providers {
				pass.providers[key] = append([]*providerState(nil), stack...)
			}
		}

		children, childFlags := reconcileChildren(node.children, node.bnd.data.build(), pass)
		node.children = children
		flags = childFlags | syncChildWidgets(node)
		cleanups = pass.cleanups

		// Honest accounting: scoped syncs add their node work to the total
		// profile without counting as a render pass.
		r.mu.Lock()
		pass.metrics.Nodes = pass.metrics.Mounts + pass.metrics.Reuses
		r.reconciler.totalProfile.add(*pass.metrics)
		window = r.window
		r.mu.Unlock()
	}()

	// Keep lifecycle callbacks outside the global pass gate: they may drive
	// another Runtime synchronously.
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}
	r.runPendingEffects()

	if window != nil {
		// Style any freshly-mounted host elements now (same job) so a list
		// that grew this sync doesn't paint one unstyled frame before its
		// deferred restyle lands.
		r.flushHostStyles()
		if flags.has(FlagLayout) {
			window.InvalidateLayout()
			window.Invalidate()
		} else if flags.has(FlagPaint) {
			window.Invalidate()
		}
	}
}

// scheduleBoundSync coalesces signal notifications into one main-thread
// job per frame.
func (r *Runtime) scheduleBoundSync(node *instance) {
	if r == nil || node == nil || node.bnd == nil {
		return
	}
	if !node.bnd.syncPending.CompareAndSwap(false, true) {
		return
	}
	run := func() {
		node.bnd.syncPending.Store(false)
		r.syncBoundNode(node)
	}
	r.mu.Lock()
	window := r.window
	r.mu.Unlock()
	if window != nil {
		// Real windows drain jobs at the top of each Step; test windows
		// drain via DrainJobsForTest.
		window.PostJob(run)
		return
	}
	run()
}

// newBoundContainer is the host widget: a bare flex container with no
// padding so it adds no visual footprint of its own.
func newBoundContainer(layout BoundLayout) *qui.Container {
	c := qui.NewContainer(qui.FlexLayout{
		Direction:  layout.Direction,
		Gap:        layout.Gap,
		AlignItems: layout.Align,
	})
	c.Style().Padding = qui.Insets{}
	c.Style().Background = qui.ColorTransparent
	if layout.Grow > 0 {
		c.SetFlex(layout.Grow)
	}
	return c
}

// applyBoundLayout re-applies changed layout options on a live bound
// container (a regular pass carried a different BoundLayout).
func applyBoundLayout(c *qui.Container, layout BoundLayout) {
	c.LayoutEngine = qui.FlexLayout{
		Direction:  layout.Direction,
		Gap:        layout.Gap,
		AlignItems: layout.Align,
	}
	c.SetFlex(layout.Grow)
}

// mountBound mounts a Show/For node: container + initial children from
// the signal's current value (inline, so hook scopes and provider
// stacks of the surrounding pass apply), then the signal subscription
// that drives future scoped syncs.
func mountBound(elem Element, pass *reconcilePass) (*instance, Flags) {
	rt := pass.runtime
	if rt == nil {
		panic("reactive: Show/For require a Runtime (use reactive.NewRuntime / Mount)")
	}
	if pass.metrics != nil {
		pass.metrics.Mounts++
	}

	container := newBoundContainer(elem.bound.layout)
	state := &boundState{
		data:        elem.bound,
		layout:      elem.bound.layout,
		parentFiber: pass.currentFiber(),
		providers:   snapshotProviders(pass),
	}
	node := &instance{
		kind:   elem.Kind,
		key:    elem.Key,
		widget: container,
		bnd:    state,
	}
	node.setChildren = func(_ qui.Widget, kids []qui.Widget) {
		SetContainerChildren(container, kids)
	}

	children, flags := reconcileChildren(nil, elem.bound.build(), pass)
	node.children = children
	flags |= syncChildWidgets(node)

	state.unsub = elem.bound.watch.Subscribe(func() {
		rt.scheduleBoundSync(node)
	})
	node.destroy = func(qui.Widget) {
		if state.unsub != nil {
			state.unsub()
		}
	}
	return node, flags | FlagLayout
}
