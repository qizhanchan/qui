package reactive

import (
	"fmt"
	"strings"

	"github.com/qizhanchan/qui"
)

// RenderMetrics capture one render pass reconcile behavior.
type RenderMetrics struct {
	Renders int

	// Nodes = Mounts + Reuses.
	Nodes  int
	Mounts int
	Reuses int

	// Unmounts counts removed node instances.
	Unmounts int

	// Reorders counts pure sibling reorders where nodes were reused.
	Reorders int
	// ReorderChecks counts sibling reconciliations that were eligible for reorder detection.
	ReorderChecks int

	// Bailouts counts MemoComponent instances skipped because their explicit
	// comparator matched and they had no pending state update.
	Bailouts int
}

func (m RenderMetrics) MountHitRate() float64 {
	if m.Nodes == 0 {
		return 0
	}
	return float64(m.Mounts) / float64(m.Nodes)
}

func (m RenderMetrics) ReuseHitRate() float64 {
	if m.Nodes == 0 {
		return 0
	}
	return float64(m.Reuses) / float64(m.Nodes)
}

func (m RenderMetrics) ReorderHitRate() float64 {
	if m.ReorderChecks == 0 {
		return 0
	}
	return float64(m.Reorders) / float64(m.ReorderChecks)
}

func (m *RenderMetrics) add(other RenderMetrics) {
	m.Renders += other.Renders
	m.Nodes += other.Nodes
	m.Mounts += other.Mounts
	m.Reuses += other.Reuses
	m.Unmounts += other.Unmounts
	m.Reorders += other.Reorders
	m.ReorderChecks += other.ReorderChecks
	m.Bailouts += other.Bailouts
}

// ProfilerSnapshot contains last-pass and accumulated reconcile metrics.
type ProfilerSnapshot struct {
	Last  RenderMetrics
	Total RenderMetrics
}

// DebugNode is a serializable snapshot of the reconciled runtime tree.
type DebugNode struct {
	Kind     string      `json:"kind"`
	Key      string      `json:"key,omitempty"`
	Children []DebugNode `json:"children,omitempty"`
}

func (n *DebugNode) String() string {
	if n == nil {
		return "<empty>"
	}
	var b strings.Builder
	writeDebugNode(&b, *n, 0)
	return b.String()
}

func writeDebugNode(b *strings.Builder, n DebugNode, depth int) {
	for i := 0; i < depth; i++ {
		b.WriteString("  ")
	}
	if n.Key != "" {
		b.WriteString(fmt.Sprintf("%s[%s]\n", n.Kind, n.Key))
	} else {
		b.WriteString(n.Kind)
		b.WriteByte('\n')
	}
	for _, child := range n.Children {
		writeDebugNode(b, child, depth+1)
	}
}

// Reconciler keeps widget instances stable across renders and applies
// minimal tree mutations from declarative Element updates.
type Reconciler struct {
	root *instance

	// runtime, when set, lets component instances render under a hook
	// scope. Standalone reconciler use (no components) leaves it nil.
	runtime *Runtime

	// unmountCleanups accumulates effect cleanups from component instances
	// removed during the last Render, drained by the caller after commit.
	unmountCleanups []func()

	lastProfile  RenderMetrics
	totalProfile RenderMetrics
}

func (r *Reconciler) RootWidget() qui.Widget {
	if r == nil || r.root == nil {
		return nil
	}
	return instanceWidget(r.root)
}

func (r *Reconciler) Profile() ProfilerSnapshot {
	if r == nil {
		return ProfilerSnapshot{}
	}
	return ProfilerSnapshot{
		Last:  r.lastProfile,
		Total: r.totalProfile,
	}
}

// TakeUnmountCleanups returns and clears the cleanups gathered from
// components unmounted in the last Render. The Runtime runs them after
// committing to the window.
func (r *Reconciler) TakeUnmountCleanups() []func() {
	if r == nil {
		return nil
	}
	out := r.unmountCleanups
	r.unmountCleanups = nil
	return out
}

func (r *Reconciler) DebugTree() *DebugNode {
	if r == nil || r.root == nil {
		return nil
	}
	n := buildDebugNode(r.root)
	return &n
}

func buildDebugNode(node *instance) DebugNode {
	out := DebugNode{Kind: node.kind, Key: node.key}
	kids := node.debugChildren()
	if len(kids) == 0 {
		return out
	}
	out.Children = make([]DebugNode, 0, len(kids))
	for _, child := range kids {
		if child == nil {
			continue
		}
		out.Children = append(out.Children, buildDebugNode(child))
	}
	return out
}

func (r *Reconciler) setProfile(last RenderMetrics) {
	r.lastProfile = last
	r.totalProfile.add(last)
}

// reconcilePass threads per-render state through the recursive reconcile.
type reconcilePass struct {
	metrics  *RenderMetrics
	runtime  *Runtime
	cleanups []func() // unmount effect cleanups to run after commit

	// fiberStack tracks the component instance currently rendering, so
	// fibers mounted during its subtree reconcile get the right parent
	// link for childDirty propagation.
	fiberStack []*componentFiber

	// providers holds the per-context provider stacks visible at the
	// current point of the DFS (see context.go). Context.Use reads the
	// top of the stack for its key.
	providers map[*contextKey][]*providerState

	// hosts is the stack of host widgets enclosing the current point of
	// the DFS — the declaring context a Portal hands its content as a
	// style parent (see adoptPortalStyle).
	hosts []qui.Widget
}

func (p *reconcilePass) pushHost(w qui.Widget) { p.hosts = append(p.hosts, w) }
func (p *reconcilePass) popHost()              { p.hosts = p.hosts[:len(p.hosts)-1] }

// currentHost is the nearest enclosing host widget; outside any (a
// bound-list sync pass) the runtime's root widget.
func (p *reconcilePass) currentHost() qui.Widget {
	if n := len(p.hosts); n > 0 {
		return p.hosts[n-1]
	}
	if p.runtime != nil {
		return p.runtime.reconciler.RootWidget()
	}
	return nil
}

// styleParented is implemented by host widgets that inherit styling from
// an out-of-tree parent (htmlcss.El.SetStyleParent). Kept as a duck-typed
// interface so reactive stays backend-agnostic.
type styleParented interface{ SetStyleParent(qui.Widget) }

// adoptPortalStyle links a portal's content widgets to the host the
// portal was declared under, so the content cascades from its declaring
// context (inherited properties, ancestor selectors) even though it is
// mounted into the overlay stack.
func adoptPortalStyle(widgets []qui.Widget, parent qui.Widget) {
	if parent == nil {
		return
	}
	for _, w := range widgets {
		if sp, ok := w.(styleParented); ok {
			sp.SetStyleParent(parent)
		}
	}
}

func (p *reconcilePass) currentFiber() *componentFiber {
	if n := len(p.fiberStack); n > 0 {
		return p.fiberStack[n-1]
	}
	return nil
}

func (p *reconcilePass) pushFiber(f *componentFiber) { p.fiberStack = append(p.fiberStack, f) }
func (p *reconcilePass) popFiber()                   { p.fiberStack = p.fiberStack[:len(p.fiberStack)-1] }

// Render applies a new declarative tree and returns the concrete root widget
// plus invalidation flags to apply on the Window.
func (r *Reconciler) Render(next Element) (qui.Widget, Flags) {
	if r == nil {
		return nil, FlagNone
	}

	pass := &reconcilePass{metrics: &RenderMetrics{Renders: 1}, runtime: r.runtime}
	next.validate()
	if next.isFragment() {
		panic("reactive: the runtime root cannot be a Fragment — a window needs exactly one root widget")
	}
	if next.isPortal() {
		panic("reactive: the runtime root cannot be a Portal — it contributes no root widget")
	}

	if next.isEmpty() {
		if r.root != nil {
			unmountInstance(r.root, pass)
			r.root = nil
		}
		r.unmountCleanups = pass.cleanups
		r.setProfile(*pass.metrics)
		if pass.metrics.Unmounts > 0 {
			return nil, FlagLayout
		}
		return nil, FlagNone
	}

	root, flags := reconcileNode(r.root, next, pass)
	r.root = root
	r.unmountCleanups = pass.cleanups
	pass.metrics.Nodes = pass.metrics.Mounts + pass.metrics.Reuses
	r.setProfile(*pass.metrics)

	if root == nil {
		return nil, flags
	}
	return instanceWidget(root), flags
}

type instance struct {
	kind string
	key  string

	// Host instance fields (component instances leave these zero).
	widget      qui.Widget
	update      func(qui.Widget) Flags
	setChildren func(qui.Widget, []qui.Widget)
	destroy     func(qui.Widget) // captured at mount; runs on unmount
	children    []*instance
	// lastWidgets is the widget list most recently pushed through
	// setChildren. Children changes are detected by comparing collected
	// widgets — not instance pointers — so a reused component instance
	// whose render swapped its subtree ROOT widget still propagates to
	// the parent container.
	lastWidgets []qui.Widget

	// Component instance fields (host instances leave these nil).
	comp  *componentFiber
	child *instance // the reconciled output of the component's render

	// Provider instance field (nil elsewhere): the mounted context
	// provider's value + subscriber set.
	prov *providerState

	// Portal instance fields (zero elsewhere): the widget lives in the
	// window overlay stack (instance.widget IS the portalHost) and
	// contributes nothing to the parent's widget list.
	isPortal   bool
	portalOpts *PortalOptions
	// styleParent is the host a portal was declared under (see
	// adoptPortalStyle); re-applied when the portal's content widget
	// changes.
	styleParent qui.Widget

	// Boundary instance field (nil elsewhere): the error latch; the
	// guarded subtree (child or fallback) lives in .child.
	boundary *boundaryState

	// Bound instance field (nil elsewhere): signal-driven children
	// state (see Show / For in structural.go). instance.widget is the
	// hosting container.
	bnd *boundState
}

func (n *instance) isComponent() bool { return n != nil && n.comp != nil }

// debugChildren returns the children to walk for the debug/snapshot tree:
// a component shows its rendered subtree, a host shows its child list.
func (n *instance) debugChildren() []*instance {
	if n == nil {
		return nil
	}
	if n.comp != nil || n.boundary != nil {
		if n.child == nil {
			return nil
		}
		return []*instance{n.child}
	}
	return n.children
}

// instanceWidget resolves the single concrete widget an instance roots —
// a host's own widget, or a component's rendered subtree root. Used for
// the runtime ROOT only; a widget-less grouping node (fragment /
// provider) resolves to its first widget (Render rejects fragment
// roots, this is belt-and-braces for a component whose render returns
// a Fragment at the root).
func instanceWidget(node *instance) qui.Widget {
	if node == nil {
		return nil
	}
	if node.isPortal {
		return nil
	}
	if node.comp != nil || node.boundary != nil {
		return instanceWidget(node.child)
	}
	if node.widget == nil {
		for _, child := range node.children {
			if w := instanceWidget(child); w != nil {
				return w
			}
		}
		return nil
	}
	return node.widget
}

func reconcileNode(prev *instance, next Element, pass *reconcilePass) (*instance, Flags) {
	next.validate()

	if next.isEmpty() {
		if prev != nil {
			unmountInstance(prev, pass)
		}
		return nil, FlagLayout
	}

	// Type/identity change → unmount the old subtree, mount fresh.
	if prev == nil || prev.kind != next.Kind || prev.key != next.Key ||
		prev.isComponent() != next.isComponent() {
		if prev != nil {
			unmountInstance(prev, pass)
		}
		mounted, flags := mountNode(next, pass)
		return mounted, flags | FlagLayout
	}

	if next.isComponent() {
		return reconcileComponent(prev, next, pass)
	}

	if next.isFragment() {
		if pass.metrics != nil {
			pass.metrics.Reuses++
		}
		children, childFlags := reconcileChildren(prev.children, next.Children, pass)
		prev.children = children
		// No widget of our own — the parent host's syncChildWidgets
		// re-collects through us and detects any splice change.
		return prev, childFlags
	}

	if next.isBoundary() {
		if pass.metrics != nil {
			pass.metrics.Reuses++
		}
		return prev, reconcileBoundaryChild(prev, next.boundary, pass)
	}

	if next.isBound() {
		if pass.metrics != nil {
			pass.metrics.Reuses++
		}
		if prev.bnd == nil {
			unmountInstance(prev, pass)
			return mountNode(next, pass)
		}
		// Refresh the build closure (it captures the CURRENT render's
		// locals) and the scoped-sync environment, then reconcile the
		// children from the signal's current value like a regular host —
		// a full pass must also observe non-signal state the render
		// closure reads.
		prev.bnd.data = next.bound
		prev.bnd.parentFiber = pass.currentFiber()
		prev.bnd.providers = snapshotProviders(pass)
		if prev.bnd.layout != next.bound.layout {
			if c, ok := prev.widget.(*qui.Container); ok {
				applyBoundLayout(c, next.bound.layout)
			}
			prev.bnd.layout = next.bound.layout
		}
		children, childFlags := reconcileChildren(prev.children, next.bound.build(), pass)
		prev.children = children
		return prev, childFlags | syncChildWidgets(prev)
	}

	if next.isPortal() {
		if pass.metrics != nil {
			pass.metrics.Reuses++
		}
		host, _ := prev.widget.(*portalHost)
		if host == nil {
			// Kind matched but no host — remount defensively.
			unmountInstance(prev, pass)
			return mountNode(next, pass)
		}
		if !valuesEqual(*prev.portalOpts, *next.portal) {
			host.applyOpts(*next.portal)
			prev.portalOpts = next.portal
			layoutPortal(host, pass.runtime.window)
		}
		children, childFlags := reconcileChildren(prev.children, next.Children, pass)
		prev.children = children
		synced := syncChildWidgets(prev)
		if synced != FlagNone {
			adoptPortalStyle(prev.lastWidgets, prev.styleParent)
		}
		if synced != FlagNone || childFlags.has(FlagLayout) {
			layoutPortal(host, pass.runtime.window)
		} else if childFlags.has(FlagPaint) {
			pass.runtime.window.InvalidateRect(host.Bounds())
		}
		// Overlay invalidation handled locally — nothing for the parent
		// tree to do.
		return prev, FlagNone
	}

	if next.isProvider() {
		if pass.metrics != nil {
			pass.metrics.Reuses++
		}
		if prev.prov == nil || prev.prov.key != next.provider.key {
			// Same kind string but a different context identity — remount.
			unmountInstance(prev, pass)
			mounted, flags := mountNode(next, pass)
			return mounted, flags | FlagLayout
		}
		if !valuesEqual(prev.prov.value, next.provider.value) {
			prev.prov.value = next.provider.value
			// Consumers re-render this pass: direct descendants via the
			// normal walk below, memo-bailed ones via childDirty.
			markSubscribersDirty(pass, prev.prov)
		}
		pass.pushProvider(prev.prov)
		children, childFlags := reconcileChildren(prev.children, next.Children, pass)
		pass.popProvider(prev.prov)
		prev.children = children
		return prev, childFlags
	}

	if pass.metrics != nil {
		pass.metrics.Reuses++
	}

	prev.update = next.Update
	prev.setChildren = next.SetChildren
	flags := callUpdate(prev.update, prev.widget)

	if len(next.Children) > 0 && prev.setChildren == nil {
		panic("reactive: parent has children but SetChildren is nil")
	}

	pass.pushHost(prev.widget)
	children, childFlags := reconcileChildren(prev.children, next.Children, pass)
	pass.popHost()
	flags |= childFlags
	prev.children = children
	flags |= syncChildWidgets(prev)

	return prev, flags
}

// syncChildWidgets pushes the collected child widget list through the
// host's setChildren when it differs from the last committed list. This
// is the single place "did my children change?" is decided — by widget
// identity, which stays correct across component-instance reuse and
// fragment expansion.
func syncChildWidgets(node *instance) Flags {
	if node == nil || node.setChildren == nil {
		return FlagNone
	}
	widgets := collectWidgets(node.children)
	if widgetListsEqual(node.lastWidgets, widgets) {
		return FlagNone
	}
	node.setChildren(node.widget, widgets)
	node.lastWidgets = widgets
	return FlagLayout
}

func widgetListsEqual(a, b []qui.Widget) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reconcileComponent reuses a component instance. Ordinary Component
// instances re-render whenever their parent does; MemoComponent skips only
// when its explicit comparator matches and the fiber is clean. If a
// DESCENDANT is dirty (childDirty), the bailout still descends via visitDirty
// so nested setState survives memoized ancestors.
func reconcileComponent(prev *instance, next Element, pass *reconcilePass) (*instance, Flags) {
	fiber := prev.comp

	pass.runtimeLock()
	dirty := fiber.dirty
	childDirty := fiber.childDirty
	pass.runtimeUnlock()

	if !dirty && fiber.propsEqual != nil && fiber.propsEqual(fiber.props, next.props) {
		if pass.metrics != nil {
			pass.metrics.Reuses++
			pass.metrics.Bailouts++
		}
		if !childDirty {
			return prev, FlagNone
		}
		pass.runtimeLock()
		fiber.childDirty = false
		pass.runtimeUnlock()
		pass.pushFiber(fiber)
		flags := visitDirty(prev.child, pass)
		pass.popFiber()
		return prev, flags
	}

	if pass.metrics != nil {
		pass.metrics.Reuses++
	}

	pass.runtimeLock()
	fiber.props = next.props
	fiber.render = next.render
	fiber.propsEqual = next.propsEqual
	fiber.dirty = false
	fiber.childDirty = false
	pass.runtimeUnlock()

	pass.pushFiber(fiber)
	childElem := renderFiber(pass, fiber)
	child, flags := reconcileNode(prev.child, childElem, pass)
	pass.popFiber()
	prev.child = child
	return prev, flags
}

// visitDirty walks a bailed-out subtree's instance tree looking for
// dirty fibers, re-rendering exactly those (with their stored props) and
// re-syncing host child lists along the way. Prunes at any component
// whose fiber is neither dirty nor childDirty.
func visitDirty(node *instance, pass *reconcilePass) Flags {
	if node == nil {
		return FlagNone
	}
	if node.comp != nil {
		fiber := node.comp

		pass.runtimeLock()
		dirty := fiber.dirty
		childDirty := fiber.childDirty
		fiber.dirty = false
		fiber.childDirty = false
		pass.runtimeUnlock()

		switch {
		case dirty:
			if pass.metrics != nil {
				pass.metrics.Reuses++
			}
			pass.pushFiber(fiber)
			childElem := renderFiber(pass, fiber)
			child, flags := reconcileNode(node.child, childElem, pass)
			pass.popFiber()
			node.child = child
			return flags
		case childDirty:
			pass.pushFiber(fiber)
			flags := visitDirty(node.child, pass)
			pass.popFiber()
			return flags
		default:
			return FlagNone
		}
	}

	if node.boundary != nil {
		return visitDirty(node.child, pass)
	}

	// Host / grouping node: descend, then re-sync the child widget list
	// in case a dirty descendant swapped its subtree root widget.
	// Providers re-push their value so dirty consumers re-rendering
	// below still resolve Context.Use correctly.
	if node.prov != nil {
		pass.pushProvider(node.prov)
		defer pass.popProvider(node.prov)
	}
	var flags Flags
	if node.widget != nil && !node.isPortal {
		pass.pushHost(node.widget)
	}
	for _, child := range node.children {
		flags |= visitDirty(child, pass)
	}
	if node.widget != nil && !node.isPortal {
		pass.popHost()
	}
	flags |= syncChildWidgets(node)
	if node.isPortal {
		// Overlay geometry is portal-owned; invalidate locally and don't
		// bubble layout flags into the main tree.
		if host, ok := node.widget.(*portalHost); ok && flags != FlagNone && pass.runtime != nil {
			layoutPortal(host, pass.runtime.window)
		}
		return FlagNone
	}
	return flags
}

func mountNode(elem Element, pass *reconcilePass) (*instance, Flags) {
	elem.validate()
	if elem.isEmpty() {
		return nil, FlagLayout
	}

	if elem.isComponent() {
		return mountComponent(elem, pass)
	}

	if elem.isPortal() {
		return mountPortal(elem, pass)
	}

	if elem.isBoundary() {
		if pass.metrics != nil {
			pass.metrics.Mounts++
		}
		node := &instance{kind: elem.Kind, key: elem.Key, boundary: &boundaryState{}}
		flags := reconcileBoundaryChild(node, elem.boundary, pass)
		return node, flags | FlagLayout
	}

	if elem.isBound() {
		return mountBound(elem, pass)
	}

	if elem.isFragment() || elem.isProvider() {
		if pass.metrics != nil {
			pass.metrics.Mounts++
		}
		node := &instance{kind: elem.Kind, key: elem.Key}
		if elem.isProvider() {
			node.prov = &providerState{
				key:   elem.provider.key,
				value: elem.provider.value,
				subs:  make(map[*componentFiber]struct{}),
			}
			pass.pushProvider(node.prov)
			defer pass.popProvider(node.prov)
		}
		children := make([]*instance, 0, len(elem.Children))
		var flags Flags
		for _, childElem := range elem.Children {
			childNode, childFlags := mountNode(childElem, pass)
			children = append(children, childNode)
			flags |= childFlags
		}
		node.children = children
		return node, flags | FlagLayout
	}

	widget := elem.Create()
	if widget == nil {
		panic("reactive: Create returned nil widget")
	}

	if pass.metrics != nil {
		pass.metrics.Mounts++
	}

	node := &instance{
		kind:        elem.Kind,
		key:         elem.Key,
		widget:      widget,
		update:      elem.Update,
		setChildren: elem.SetChildren,
		destroy:     elem.Destroy,
	}

	flags := callUpdate(node.update, node.widget)
	if len(elem.Children) == 0 {
		return node, flags
	}
	if node.setChildren == nil {
		panic("reactive: mount has children but SetChildren is nil")
	}

	children := make([]*instance, 0, len(elem.Children))
	pass.pushHost(widget)
	defer pass.popHost()
	for _, childElem := range elem.Children {
		childNode, childFlags := mountNode(childElem, pass)
		// Empty children mount as nil placeholders so sibling indices
		// stay aligned with the Element list — the slot an If(false)
		// occupies must survive for positional (unkeyed) matching on
		// later renders. collectWidgets skips the nils.
		children = append(children, childNode)
		flags |= childFlags
	}
	node.children = children
	node.lastWidgets = collectWidgets(children)
	node.setChildren(node.widget, node.lastWidgets)
	flags |= FlagLayout
	return node, flags
}

// mountPortal creates the overlay host, mounts the child subtree into
// it, and pushes it onto the window overlay stack. The unmount path
// (instance.destroy) removes the overlay after commit.
func mountPortal(elem Element, pass *reconcilePass) (*instance, Flags) {
	rt := pass.runtime
	if rt == nil || rt.window == nil {
		panic("reactive: Portal requires a Runtime bound to a Window (use reactive.NewRuntime/Mount)")
	}
	window := rt.window
	if pass.metrics != nil {
		pass.metrics.Mounts++
	}

	host := newPortalHost(*elem.portal)
	node := &instance{
		kind:       elem.Kind,
		key:        elem.Key,
		widget:     host,
		isPortal:   true,
		portalOpts: elem.portal,
	}
	node.setChildren = func(_ qui.Widget, kids []qui.Widget) {
		SetContainerChildren(host.Container, kids)
	}
	node.destroy = func(qui.Widget) {
		window.RemoveOverlay(host)
	}

	children := make([]*instance, 0, len(elem.Children))
	for _, childElem := range elem.Children {
		childNode, _ := mountNode(childElem, pass)
		children = append(children, childNode)
	}
	node.children = children
	node.lastWidgets = collectWidgets(children)
	node.setChildren(host, node.lastWidgets)
	node.styleParent = pass.currentHost()
	adoptPortalStyle(node.lastWidgets, node.styleParent)

	layoutPortal(host, window)
	window.PushOverlay(host)
	// Lay out once more now that the content can reach the window: the
	// first pass ran detached, and mount-time work keyed on attachment
	// (an `autofocus` element posting its focus move) needs a window.
	layoutPortal(host, window)
	// Grab focus so Escape / Enter reach the host even when the content
	// has no focusable child. An `autofocus` element inside the content
	// moves focus on from here (its focus job runs after this).
	if elem.portal.OnEscape != nil || elem.portal.OnEnter != nil {
		window.SetFocus(host)
	}
	return node, FlagNone
}

func mountComponent(elem Element, pass *reconcilePass) (*instance, Flags) {
	if pass.metrics != nil {
		pass.metrics.Mounts++
	}
	fiber := &componentFiber{props: elem.props, render: elem.render, propsEqual: elem.propsEqual, parent: pass.currentFiber()}
	node := &instance{kind: elem.Kind, key: elem.Key, comp: fiber}

	pass.pushFiber(fiber)
	childElem := renderFiber(pass, fiber)
	child, flags := mountNode(childElem, pass)
	pass.popFiber()
	node.child = child
	return node, flags | FlagLayout
}

// unmountInstance counts the removed subtree and gathers component effect
// cleanups so they fire after commit.
func unmountInstance(node *instance, pass *reconcilePass) {
	if node == nil {
		return
	}
	if pass != nil && pass.metrics != nil {
		pass.metrics.Unmounts++
	}
	if node.comp != nil {
		if pass != nil {
			pass.cleanups = append(pass.cleanups, node.comp.host.cleanupAll()...)
		}
		dropSubscriptions(node.comp)
		unmountInstance(node.child, pass)
		return
	}
	if node.boundary != nil {
		unmountInstance(node.child, pass)
		return
	}
	for _, child := range node.children {
		unmountInstance(child, pass)
	}
	if node.destroy != nil {
		// Queue with the effect cleanups so it runs after commit, in
		// child-before-parent order (children were queued above).
		destroy, widget := node.destroy, node.widget
		if pass != nil {
			pass.cleanups = append(pass.cleanups, func() { destroy(widget) })
		} else {
			destroy(widget)
		}
	}
}

func reconcileChildren(prev []*instance, next []Element, pass *reconcilePass) ([]*instance, Flags) {
	if len(next) == 0 {
		if len(prev) > 0 {
			for _, p := range prev {
				unmountInstance(p, pass)
			}
		}
		return nil, FlagNone
	}

	nextNodes := make([]*instance, 0, len(next))
	used := make([]bool, len(prev))
	keyed := make(map[string][]int)
	for i, p := range prev {
		if p != nil && p.key != "" {
			keyed[p.key] = append(keyed[p.key], i)
		}
	}

	flags := FlagNone
	for i, elem := range next {
		// Empty element = a conditional slot that rendered nothing this
		// pass. Keep a nil placeholder at this index (React null-child
		// semantics) so unkeyed siblings after it don't shift positions.
		// It consumes prev[i] only when that was the same slot's content
		// (unkeyed instance or already a placeholder) — keyed instances
		// are left for key matching.
		if elem.isEmpty() {
			if i < len(prev) && !used[i] && (prev[i] == nil || prev[i].key == "") {
				used[i] = true
				if prev[i] != nil {
					unmountInstance(prev[i], pass)
					flags |= FlagLayout
				}
			}
			nextNodes = append(nextNodes, nil)
			continue
		}

		idx := -1
		if elem.Key != "" {
			candidates := keyed[elem.Key]
			for len(candidates) > 0 {
				candidate := candidates[0]
				candidates = candidates[1:]
				if candidate >= 0 && candidate < len(used) && !used[candidate] {
					idx = candidate
					break
				}
			}
			keyed[elem.Key] = candidates
		} else if i < len(prev) && !used[i] && prev[i] != nil && prev[i].key == "" {
			idx = i
		}

		var oldNode *instance
		if idx >= 0 {
			oldNode = prev[idx]
			used[idx] = true
		}

		node, nodeFlags := reconcileNode(oldNode, elem, pass)
		nextNodes = append(nextNodes, node)
		flags |= nodeFlags
	}

	for i, ok := range used {
		if ok || prev[i] == nil {
			continue
		}
		unmountInstance(prev[i], pass)
	}

	if pass.metrics != nil && (len(prev) > 1 || len(nextNodes) > 1) {
		pass.metrics.ReorderChecks++
		if isPureReorder(prev, nextNodes) {
			pass.metrics.Reorders++
		}
	}

	return nextNodes, flags
}

func isPureReorder(prev, next []*instance) bool {
	if len(prev) < 2 || len(prev) != len(next) {
		return false
	}
	allSamePos := true
	pool := make(map[*instance]int, len(prev))
	for i := range prev {
		pool[prev[i]]++
		if prev[i] != next[i] {
			allSamePos = false
		}
	}
	if allSamePos {
		return false
	}
	for _, n := range next {
		if pool[n] == 0 {
			return false
		}
		pool[n]--
	}
	for _, remain := range pool {
		if remain != 0 {
			return false
		}
	}
	return true
}

func countSubtree(node *instance) int {
	if node == nil {
		return 0
	}
	total := 1
	if node.comp != nil {
		return total + countSubtree(node.child)
	}
	for _, child := range node.children {
		total += countSubtree(child)
	}
	return total
}

func collectWidgets(nodes []*instance) []qui.Widget {
	if len(nodes) == 0 {
		return nil
	}
	widgets := make([]qui.Widget, 0, len(nodes))
	for _, node := range nodes {
		widgets = appendInstanceWidgets(node, widgets)
	}
	return widgets
}

// appendInstanceWidgets flattens an instance into the widgets it
// contributes to its parent: a host contributes its own widget, a
// component contributes whatever its rendered subtree contributes, and
// widget-less grouping nodes (fragments, providers) splice in every
// child's contribution.
func appendInstanceWidgets(node *instance, out []qui.Widget) []qui.Widget {
	if node == nil {
		return out
	}
	if node.isPortal {
		// Portal subtrees live in the overlay stack, not the parent.
		return out
	}
	if node.comp != nil || node.boundary != nil {
		return appendInstanceWidgets(node.child, out)
	}
	if node.widget == nil {
		for _, child := range node.children {
			out = appendInstanceWidgets(child, out)
		}
		return out
	}
	return append(out, node.widget)
}

func callUpdate(update func(qui.Widget) Flags, widget qui.Widget) Flags {
	if update == nil {
		return FlagNone
	}
	return update(widget)
}

// runtimeLock / runtimeUnlock guard component fiber fields (props/dirty)
// that event handlers on other goroutines also touch via requestRender.
func (p *reconcilePass) runtimeLock() {
	if p != nil && p.runtime != nil {
		p.runtime.mu.Lock()
	}
}

func (p *reconcilePass) runtimeUnlock() {
	if p != nil && p.runtime != nil {
		p.runtime.mu.Unlock()
	}
}
