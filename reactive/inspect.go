package reactive

// Reactive-layer introspection: a "React DevTools"-shaped snapshot of the
// live component tree — component instances, their hook state (UseState /
// UseSignal / UseMemo / UseEffect), props, pending-render (dirty) status,
// and the backing widget for host nodes — so business logic built on the
// reactive engine is debuggable the way the widget (/tree) and HTML (/dom)
// layers already are.
//
// This is the third introspection tier. Where /dom answers "what does the
// UI look like and why is it styled this way", /reactive answers "which
// components are mounted, what is their state, and what is pending to
// re-render" — the "why isn't my component updating / why did it re-render"
// questions the other layers can't see.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/qizhanchan/qui"
)

// rtRegistry maps a window to the reactive Runtime driving it, so the agent
// layer (which only has the *qui.Window) can find the runtime to inspect.
// Registered in NewRuntime; last mount per window wins. Entries for closed
// windows are harmless (the agent only queries its own live window).
var rtRegistry sync.Map // *qui.Window -> *Runtime

func registerRuntime(w *qui.Window, r *Runtime) {
	if w == nil || r == nil {
		return
	}
	rtRegistry.Store(w, r)
}

// RuntimeForWindow returns the reactive Runtime mounted on w, or nil if the
// window isn't driven by a reactive runtime (e.g. a plain widget app).
func RuntimeForWindow(w *qui.Window) *Runtime {
	if w == nil {
		return nil
	}
	if v, ok := rtRegistry.Load(w); ok {
		return v.(*Runtime)
	}
	return nil
}

// HookInfo is one hook slot of a component instance.
type HookInfo struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`            // state | signal | memo | effect
	Value string `json:"value,omitempty"` // current value (state/signal/memo)
	// Watchers is the subscriber count for a signal hook (how many bindings
	// / computeds react to it) — useful for spotting a signal nothing reads.
	Watchers int `json:"watchers,omitempty"`
}

// ReactiveNode is one node of the reactive tree snapshot.
type ReactiveNode struct {
	Kind string `json:"kind"`           // component | host | fragment | provider | boundary | bound:show | bound:for | portal
	Name string `json:"name,omitempty"` // component name (for kind=component)
	Key  string `json:"key,omitempty"`
	// Dirty is true when the node (or a descendant) has a pending re-render
	// scheduled — answers "is this waiting to update?".
	Dirty bool `json:"dirty,omitempty"`
	// WidgetRole / WidgetID identify the backing widget for host nodes, so a
	// reactive node can be correlated with a /dom or /tree node.
	WidgetRole string          `json:"widgetRole,omitempty"`
	WidgetID   string          `json:"widgetId,omitempty"`
	Props      string          `json:"props,omitempty"`
	Hooks      []HookInfo      `json:"hooks,omitempty"`
	Children   []*ReactiveNode `json:"children,omitempty"`
}

// String renders the reactive tree as an indented outline (CLI-friendly).
func (n *ReactiveNode) String() string {
	if n == nil {
		return "<no reactive runtime>"
	}
	var b strings.Builder
	n.write(&b, 0)
	return b.String()
}

func (n *ReactiveNode) write(b *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
	switch {
	case n.Name != "":
		b.WriteString("<" + n.Name + ">")
	default:
		b.WriteString(n.Kind)
	}
	if n.Key != "" {
		b.WriteString("[" + n.Key + "]")
	}
	if n.WidgetRole != "" {
		b.WriteString(" " + n.WidgetRole)
		if n.WidgetID != "" {
			b.WriteString("#" + n.WidgetID)
		}
	}
	if n.Dirty {
		b.WriteString(" *dirty*")
	}
	b.WriteByte('\n')
	for _, h := range n.Hooks {
		for i := 0; i < depth+1; i++ {
			b.WriteString("  ")
		}
		b.WriteString(fmt.Sprintf("· %s", h.Kind))
		if h.Value != "" {
			b.WriteString(" = " + h.Value)
		}
		if h.Watchers > 0 {
			b.WriteString(fmt.Sprintf(" (%d watchers)", h.Watchers))
		}
		b.WriteByte('\n')
	}
	for _, c := range n.Children {
		c.write(b, depth+1)
	}
}

// Inspect returns a snapshot of the runtime's current component tree.
// Safe to call from another goroutine (locks the runtime like DebugTree);
// returns nil if nothing has mounted yet.
func (r *Runtime) Inspect() *ReactiveNode {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reconciler.root == nil {
		return nil
	}
	return inspectInstance(r.reconciler.root)
}

func inspectInstance(node *instance) *ReactiveNode {
	if node == nil {
		return nil
	}
	rn := &ReactiveNode{Key: node.key}
	switch {
	case node.comp != nil:
		rn.Kind = "component"
		rn.Name = strings.TrimPrefix(node.kind, "component:")
		rn.Dirty = node.comp.dirty || node.comp.childDirty
		rn.Hooks = inspectHooks(node.comp.host.hooks)
		if node.comp.props != nil {
			rn.Props = truncate(fmt.Sprintf("%v", node.comp.props), 100)
		}
	case strings.HasPrefix(node.kind, "#bound:"):
		rn.Kind = strings.TrimPrefix(node.kind, "#")
	case strings.HasPrefix(node.kind, "#provider:"):
		rn.Kind = "provider"
		rn.Name = strings.TrimPrefix(node.kind, "#provider:")
	case node.kind == "#boundary":
		rn.Kind = "boundary"
	case node.kind == fragmentKind:
		rn.Kind = "fragment"
	case node.isPortal:
		rn.Kind = "portal"
	default:
		rn.Kind = "host"
		if node.widget != nil {
			rn.WidgetRole = qui.WidgetRole(node.widget)
			rn.WidgetID = qui.WidgetID(node.widget)
		}
	}
	for _, c := range node.debugChildren() {
		if child := inspectInstance(c); child != nil {
			rn.Children = append(rn.Children, child)
		}
	}
	return rn
}

func inspectHooks(hooks []hookSlot) []HookInfo {
	var out []HookInfo
	for i, slot := range hooks {
		h := HookInfo{Index: i}
		switch slot.kind {
		case hookKindState:
			if sig, ok := slot.state.(anySignal); ok {
				h.Kind = "signal"
				h.Value = truncate(fmt.Sprintf("%v", sig.anyValue()), 80)
				h.Watchers = sig.watcherCount()
			} else {
				h.Kind = "state"
				h.Value = truncate(fmt.Sprintf("%v", slot.state), 80)
			}
		case hookKindMemo:
			h.Kind = "memo"
			h.Value = truncate(fmt.Sprintf("%v", slot.memo.value), 80)
		case hookKindEffect:
			h.Kind = "effect"
			h.Value = fmt.Sprintf("deps:%d cleanup:%t", len(slot.effect.deps), slot.effect.cleanup != nil)
		default:
			h.Kind = slot.kind.String()
		}
		out = append(out, h)
	}
	return out
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}
