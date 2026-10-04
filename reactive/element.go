package reactive

import (
	"fmt"

	"github.com/qizhanchan/qui"
)

// Flags describe how a render pass should invalidate the window.
//
// FlagPaint repaints pixels without forcing Measure/Layout.
// FlagLayout requests a full layout pass (and implies repaint).
type Flags uint8

const (
	FlagNone  Flags = 0
	FlagPaint Flags = 1 << iota
	FlagLayout
)

func (f Flags) has(flag Flags) bool { return f&flag != 0 }

// Element is the declarative unit reconciled into a concrete qui.Widget tree.
//
// Kind should identify the widget type (e.g. "container", "button").
// Key should be stable for dynamic siblings so reorder/reconcile can reuse
// widget instances.
type Element struct {
	Kind string
	Key  string

	Create      func() qui.Widget
	Update      func(widget qui.Widget) Flags
	SetChildren func(widget qui.Widget, children []qui.Widget)
	Children    []Element

	// Destroy, when set, runs once when this host instance unmounts —
	// release external resources acquired in Create (signal bindings,
	// timers, files). The reconciler captures it at MOUNT time; later
	// renders of the same instance do not replace it, so pair it with
	// the Create that allocates.
	Destroy func(widget qui.Widget)

	// Component path: when render is non-nil this Element is a component
	// instance rather than a direct host widget. render produces the
	// component's subtree (and may call hooks, isolated to this instance);
	// props is retained for an optional, explicit component memoization
	// comparator. Ordinary Component instances deliberately re-render when
	// their parent does, so fresh closures are always installed.
	render     func() Element
	props      any
	propsEqual func(previous, next any) bool

	// Provider path: non-nil when this Element carries a Context value
	// for its subtree (see Context.Provide). Like fragments, providers
	// have no host widget — Children[0] splices into the parent.
	provider *providerData

	// Portal path: non-nil when this Element mounts its child into the
	// window overlay stack (see Portal). Contributes no widget to the
	// parent's layout.
	portal *PortalOptions

	// Boundary path: non-nil when this Element guards its subtree
	// against render panics (see ErrorBoundary).
	boundary *boundaryData

	// Bound path: non-nil when this Element's children are driven
	// directly by a signal (see Show / For) — structural updates skip
	// the render/reconcile pass.
	bound *boundData
}

// Empty removes the current tree when rendered.
func Empty() Element { return Element{} }

// fragmentKind marks an Element that groups children WITHOUT a host
// widget of its own — the children splice directly into the nearest
// host ancestor's child list.
const fragmentKind = "#fragment"

// Fragment groups multiple elements without introducing a container
// widget — the React <>...</> equivalent. A component can return
// Fragment(row, details) and both widgets become siblings in the
// parent's layout.
//
// Not allowed as the ROOT of a Runtime (a window needs exactly one root
// widget); nest it inside a container there. Fragments may carry a Key
// for keyed reconciliation like any element.
func Fragment(children ...Element) Element {
	return Element{Kind: fragmentKind, Children: children}
}

// FragmentWithKey is Fragment with a reconciliation key, for fragments
// produced inside dynamic lists.
func FragmentWithKey(key string, children ...Element) Element {
	return Element{Kind: fragmentKind, Key: key, Children: children}
}

func (e Element) isFragment() bool { return e.Kind == fragmentKind && !e.isComponent() }

func (e Element) isEmpty() bool {
	return e.Kind == "" && e.Key == "" && e.Create == nil && e.Update == nil &&
		e.SetChildren == nil && len(e.Children) == 0 && e.render == nil &&
		e.provider == nil && e.portal == nil && e.boundary == nil && e.bound == nil
}

func (e Element) isComponent() bool { return e.render != nil }
func (e Element) isProvider() bool  { return e.provider != nil }
func (e Element) isPortal() bool    { return e.portal != nil }
func (e Element) isBoundary() bool  { return e.boundary != nil }
func (e Element) isBound() bool     { return e.bound != nil }

func (e Element) validate() {
	if e.isEmpty() {
		return
	}
	if e.Kind == "" {
		panic("reactive: element kind must not be empty")
	}
	if e.isComponent() {
		// Component elements own their subtree via render; they have no
		// host Create / SetChildren of their own.
		return
	}
	if e.isFragment() || e.isProvider() || e.isPortal() || e.isBoundary() || e.isBound() {
		// Fragments, providers, and portals have no Create/SetChildren of
		// their own — the reconciler special-cases them.
		return
	}
	if e.Create == nil {
		panic(fmt.Sprintf("reactive: element %q is missing Create", e.Kind))
	}
	if len(e.Children) > 0 && e.SetChildren == nil {
		panic(fmt.Sprintf("reactive: element %q has children but no SetChildren hook", e.Kind))
	}
}

// Node builds a typed element.
//
// create is called only when mounting a fresh widget instance.
// update runs on every render for the reused instance.
// setChildren applies the reconciled children list to the parent widget.
func Node[T qui.Widget](
	kind, key string,
	create func() T,
	update func(widget T) Flags,
	setChildren func(widget T, children []qui.Widget),
	children ...Element,
) Element {
	if create == nil {
		panic(fmt.Sprintf("reactive: node %q requires a create function", kind))
	}
	return Element{
		Kind: kind,
		Key:  key,
		Create: func() qui.Widget {
			return create()
		},
		Update: func(widget qui.Widget) Flags {
			if update == nil {
				return FlagNone
			}
			typed, ok := widget.(T)
			if !ok {
				panic(fmt.Sprintf("reactive: node %q expected widget type %T, got %T", kind, *new(T), widget))
			}
			return update(typed)
		},
		SetChildren: func(widget qui.Widget, kids []qui.Widget) {
			if setChildren == nil {
				return
			}
			typed, ok := widget.(T)
			if !ok {
				panic(fmt.Sprintf("reactive: node %q expected widget type %T, got %T", kind, *new(T), widget))
			}
			setChildren(typed, kids)
		},
		Children: children,
	}
}

// Leaf is a shorthand for Node without children.
func Leaf[T qui.Widget](kind, key string, create func() T, update func(widget T) Flags) Element {
	return Node(kind, key, create, update, nil)
}

// SetContainerChildren replaces children on a *qui.Container while preserving
// parent pointers and layout-dirty semantics expected by the engine.
func SetContainerChildren(container *qui.Container, children []qui.Widget) {
	if container == nil {
		return
	}
	container.SetChildren(children...)
}

// ForEach maps data into Element slices for list rendering.
func ForEach[T any](items []T, render func(index int, item T) Element) []Element {
	if len(items) == 0 {
		return nil
	}
	out := make([]Element, 0, len(items))
	for i, item := range items {
		out = append(out, render(i, item))
	}
	return out
}

// Build returns the element unchanged. It exists so a raw Element already
// satisfies the reactive/html Node interface: a component, a Show/For node
// or a portal can sit directly among h-builder children with no wrapper,
// and a component body can `return h.Div(...)` without a trailing .Build().
func (e Element) Build() Element { return e }
