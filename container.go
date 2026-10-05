package qui

import (
	"sort"
	"time"
)

// ZIndexed is implemented by widgets carrying a CSS z-index. Containers paint
// children in ascending z-index (stable), so a higher z-index paints on top
// WITHOUT changing layout order. Absent → treated as 0.
type ZIndexed interface{ ZIndex() int }

func zIndexOf(w Widget) int {
	if z, ok := w.(ZIndexed); ok {
		return z.ZIndex()
	}
	return 0
}

// ChildrenInPaintOrder returns children reordered by ascending z-index
// (stable). When every z-index is 0 (the common case) the input slice is
// returned unchanged with no allocation.
func ChildrenInPaintOrder(children []Widget) []Widget {
	need := false
	for _, c := range children {
		if zIndexOf(c) != 0 {
			need = true
			break
		}
	}
	if !need {
		return children
	}
	out := make([]Widget, len(children))
	copy(out, children)
	sort.SliceStable(out, func(i, j int) bool { return zIndexOf(out[i]) < zIndexOf(out[j]) })
	return out
}

// Container holds child widgets and applies layout.
type Container struct {
	BaseWidget
	children     []Widget
	LayoutEngine Layout

	// OnHoverChange fires when the cursor enters or leaves this
	// container's bounds (i.e. any descendant in the hover path).
	// Useful for parent-driven reveal effects like a tab's close button
	// that should only appear while the tab is hovered. Optional.
	OnHoverChange func(hovering bool)
	hovering      bool
}

func NewContainer(layout Layout, children ...Widget) *Container {
	c := &Container{BaseWidget: NewBaseWidget(), LayoutEngine: layout}
	// Plain Containers should be correct by construction. Container
	// subclasses override this with SetSelf(outer) before adding children.
	c.SetSelf(c)
	c.SetChildren(children...)
	return c
}

// SetSelf updates the outer widget identity and repairs existing child parent
// links. This makes subclass construction order safe: children added before
// SetSelf(outer) no longer remain parented to the embedded Container.
func (c *Container) SetSelf(widget Widget) {
	if c == nil {
		return
	}
	c.assertUIThread("Container.SetSelf")
	oldParent := c.parentWidget()
	c.BaseWidget.SetSelf(widget)
	newParent := c.parentWidget()
	if oldParent == newParent {
		return
	}
	for _, child := range c.children {
		if child.Parent() == oldParent || child.Parent() == nil {
			child.SetParent(newParent)
		}
	}
}

// AddChild appends a child and wires its Parent() link back to this
// container. Uses Self() so subclassed containers (via SetSelf) are
// recorded as the parent rather than the embedded Container struct.
// Use this instead of mutating the child list directly so event
// dispatch can walk the tree upward.
//
// Also triggers InvalidateLayout — child count affects parent layout,
// so Window must re-Measure+Layout on the next frame. The child's own
// dirty flag is cleared because it's no longer its own root (the flag
// only matters on the topmost widget of a tree; mid-tree nodes never
// consulted it).
func (c *Container) AddChild(child Widget) {
	if child == nil {
		return
	}
	c.assertUIThread("Container.AddChild")
	if containsWidget(c.children, child) {
		return
	}
	if !c.attachChild(child) {
		return
	}
	c.children = append(c.children, child)
	c.InvalidateLayout()
}

// InsertChild inserts child at index, clamping index to the valid range.
// Existing children are moved instead of duplicated.
func (c *Container) InsertChild(index int, child Widget) {
	if child == nil {
		return
	}
	c.assertUIThread("Container.InsertChild")
	if existing := indexOfWidget(c.children, child); existing >= 0 {
		c.children = append(c.children[:existing], c.children[existing+1:]...)
		if existing < index {
			index--
		}
	} else {
		if !c.attachChild(child) {
			return
		}
	}
	if index < 0 {
		index = 0
	}
	if index > len(c.children) {
		index = len(c.children)
	}
	c.children = append(c.children, nil)
	copy(c.children[index+1:], c.children[index:])
	c.children[index] = child
	c.InvalidateLayout()
}

// SetChildren replaces the ordered child list while preserving children that
// remain present. Only removed subtrees are detached from their window; newly
// added subtrees receive parent/window wiring exactly once.
func (c *Container) SetChildren(children ...Widget) {
	if c == nil {
		return
	}
	c.assertUIThread("Container.SetChildren")
	next := make([]Widget, 0, len(children))
	for _, child := range children {
		if child == nil || containsWidget(next, child) {
			continue
		}
		next = append(next, child)
	}

	unchanged := len(next) == len(c.children)
	if unchanged {
		for i := range next {
			if next[i] != c.children[i] {
				unchanged = false
				break
			}
		}
	}

	oldChildren := append([]Widget(nil), c.children...)
	for _, old := range oldChildren {
		if !containsWidget(next, old) {
			c.detachChild(old)
		}
	}
	for _, child := range next {
		if !containsWidget(c.children, child) {
			if !c.attachChild(child) {
				continue
			}
			continue
		}
		// Repair legacy/manual mutations without detaching a retained subtree.
		c.repairChild(child)
	}

	if cap(c.children) < len(next) {
		c.children = make([]Widget, len(next))
	} else {
		c.children = c.children[:len(next)]
	}
	copy(c.children, next)
	if !unchanged {
		c.InvalidateLayout()
	}
}

// ClearChildren detaches every child subtree.
func (c *Container) ClearChildren() { c.SetChildren() }

// RemoveChild removes child from the container (by pointer identity),
// unwires its Parent() link, detaches the window subtree, and
// InvalidateLayout()s. No-op if child is nil or not found.
//
// Mirror of AddChild — the right primitive when callers were reaching
// directly into c.children to splice an item out. Does not call
// SetSelf or otherwise reset the child; it remains valid and can be
// AddChild'd elsewhere.
func (c *Container) RemoveChild(child Widget) {
	if child == nil {
		return
	}
	c.assertUIThread("Container.RemoveChild")
	if index := indexOfWidget(c.children, child); index >= 0 {
		c.RemoveChildAt(index)
	}
}

// RemoveChildAt removes and returns the child at index. Invalid indices
// return nil.
func (c *Container) RemoveChildAt(index int) Widget {
	if c == nil {
		return nil
	}
	c.assertUIThread("Container.RemoveChildAt")
	if index < 0 || index >= len(c.children) {
		return nil
	}
	child := c.children[index]
	c.ReleaseChildForTransfer(child)
	DetachWidgetTree(child)
	return child
}

// Children returns a snapshot of the current child order. Mutating the
// returned slice cannot bypass parent/window/invalidation wiring.
func (c *Container) Children() []Widget {
	if c == nil {
		return nil
	}
	c.assertUIThread("Container.Children")
	if len(c.children) == 0 {
		return nil
	}
	return append([]Widget(nil), c.children...)
}

func (c *Container) ChildCount() int {
	if c == nil {
		return 0
	}
	c.assertUIThread("Container.ChildCount")
	return len(c.children)
}

func (c *Container) ChildAt(index int) Widget {
	if c == nil {
		return nil
	}
	c.assertUIThread("Container.ChildAt")
	if index < 0 || index >= len(c.children) {
		return nil
	}
	return c.children[index]
}

// ChildList exposes children for framework tree walks (broadcastEvent,
// etc.). Returns the underlying slice, not a copy — callers must not
// mutate the result.
func (c *Container) ChildList() []Widget {
	c.assertUIThread("Container.ChildList")
	return c.children
}

func (c *Container) attachChild(child Widget) bool {
	return AdoptWidgetTree(child, c.parentWidget(), c.Window())
}

func (c *Container) repairChild(child Widget) {
	if child.Parent() != c.parentWidget() {
		child.SetParent(c.parentWidget())
	}
	if owner, ok := child.(interface{ Window() *Window }); !ok || owner.Window() != c.Window() {
		AttachWindowTree(child, c.Window())
	}
}

func (c *Container) detachChild(child Widget) {
	if child == nil {
		return
	}
	if child.Parent() == c.parentWidget() {
		c.ReleaseChildForTransfer(child)
	}
	DetachWidgetTree(child)
}

// ReleaseChildForTransfer removes child ownership without changing its window.
// AdoptWidgetTree uses this to make same-window reparenting atomic.
func (c *Container) ReleaseChildForTransfer(child Widget) bool {
	c.assertUIThread("Container.ReleaseChildForTransfer")
	index := indexOfWidget(c.children, child)
	if index < 0 {
		return false
	}
	c.children = append(c.children[:index], c.children[index+1:]...)
	if child.Parent() == c.parentWidget() {
		child.SetParent(nil)
	}
	c.InvalidateLayout()
	return true
}

func (c *Container) parentWidget() Widget {
	if self := c.Self(); self != nil {
		return self
	}
	return c
}

func containsWidget(children []Widget, target Widget) bool {
	return indexOfWidget(children, target) >= 0
}

func indexOfWidget(children []Widget, target Widget) int {
	for i, child := range children {
		if child == target {
			return i
		}
	}
	return -1
}

// inFlowChildren returns the children that participate in normal flow —
// dropping any marked OutOfFlow (CSS position:absolute/fixed), which the
// containing-block ancestor positions separately, and any Collapsed
// (CSS display:none), which generate no box at all. Returns c.children
// unchanged (no allocation) in the common case of no skipped children.
func (c *Container) inFlowChildren() []Widget {
	n := 0
	for _, ch := range c.children {
		if isOutOfFlow(ch) || isCollapsed(ch) {
			n++
		}
	}
	if n == 0 {
		return c.children
	}
	out := make([]Widget, 0, len(c.children)-n)
	for _, ch := range c.children {
		if !isOutOfFlow(ch) && !isCollapsed(ch) {
			out = append(out, ch)
		}
	}
	return out
}

// isOutOfFlow reports whether w is removed from normal flow.
func isOutOfFlow(w Widget) bool {
	f, ok := w.(interface{ OutOfFlow() bool })
	return ok && f.OutOfFlow()
}

// isCollapsed reports whether w generates no box (CSS display:none).
func isCollapsed(w Widget) bool {
	f, ok := w.(interface{ Collapsed() bool })
	return ok && f.Collapsed()
}

// establishesAbsCB reports whether w is a containing block for out-of-flow
// descendants (a positioned element).
func establishesAbsCB(w Widget) bool {
	e, ok := w.(interface{ EstablishesAbsContainingBlock() bool })
	return ok && e.EstablishesAbsContainingBlock()
}

// Measure returns the container's natural size, based on its children
// and LayoutEngine plus padding. Engines implementing LayoutMeasurer
// (all the built-in ones, and any custom engine that opts in) report a
// real intrinsic size; others fall back to `available` because we can't
// reason about them without more info.
//
// This intrinsic-size reporting is what makes Popup / ContextMenu /
// Select dropdowns size themselves correctly instead of ballooning
// to the full window.
func (c *Container) Measure(available Size) Size {
	c.assertUIThread("Container.Measure")
	padding := c.style.Padding
	// Out-of-flow (position:absolute/fixed) children don't contribute to the
	// container's intrinsic size — measure only in-flow children.
	children := c.inFlowChildren()
	if len(children) == 0 {
		return Size{
			W: padding.Horizontal(),
			H: padding.Vertical(),
		}
	}
	padded := Size{
		W: available.W - padding.Horizontal(),
		H: available.H - padding.Vertical(),
	}
	if padded.W < 0 {
		padded.W = 0
	}
	if padded.H < 0 {
		padded.H = 0
	}
	if m, ok := c.LayoutEngine.(LayoutMeasurer); ok {
		// Built-in engines (Flex / Grid / Flow, by value or pointer) and any
		// custom engine implementing LayoutMeasurer report a real intrinsic
		// content size.
		content := m.Measure(children, padded)
		return Size{
			W: content.W + padding.Horizontal(),
			H: content.H + padding.Vertical(),
		}
	}
	// Engines that can't report a size keep the old "take all offered"
	// behavior.
	for _, child := range children {
		child.Measure(padded)
	}
	return available
}

func (c *Container) Layout(rect Rect) {
	c.assertUIThread("Container.Layout")
	c.BaseWidget.Layout(rect)
	if c.LayoutEngine == nil {
		return
	}
	padding := c.style.Padding
	content := rect.Inset(padding)
	c.LayoutEngine.Apply(c.inFlowChildren(), content)
	// If this container is a containing block (a positioned element, or the
	// root), position the out-of-flow descendants anchored to it now that its
	// box is known. The descendants were skipped by the flow above.
	if establishesAbsCB(c.selfOrContainer()) {
		c.layoutAbsoluteDescendants(rect)
	}
}

// selfOrContainer returns the subclass pointer (via SetSelf) when set, so
// flag checks read the outer widget's BaseWidget (El/Box embed Container).
func (c *Container) selfOrContainer() Widget {
	if s := c.Self(); s != nil {
		return s
	}
	return c
}

// layoutAbsoluteDescendants positions every out-of-flow descendant whose
// containing block is this container — its nearest positioned ancestor.
// The containing block is this container's own box (border box; CSS uses the
// padding box, a border-width inset — negligible for typical borders). Each
// descendant's AbsolutePosition anchors + explicit size drive placement,
// reusing the same axis solver AbsoluteLayout uses.
func (c *Container) layoutAbsoluteDescendants(cb Rect) {
	for _, ch := range c.collectAbsoluteDescendants(c.selfOrContainer()) {
		pos := AbsolutePositionOf(ch)
		x, w := resolveAnchorAxis(pos.Anchor, AnchorLeft, AnchorRight,
			pos.Left, pos.Right, pos.Width, cb.X, cb.W, ch, true)
		y, h := resolveAnchorAxis(pos.Anchor, AnchorTop, AnchorBottom,
			pos.Top, pos.Bottom, pos.Height, cb.Y, cb.H, ch, false)
		clamped := applyConstraints(Size{W: w, H: h}, widgetMinSize(ch), widgetMaxSize(ch))
		ch.Layout(Rect{X: x, Y: y, W: clamped.W, H: clamped.H})
	}
}

// collectAbsoluteDescendants gathers out-of-flow widgets in root's subtree
// for which root is the containing block: it descends through in-flow
// children but stops at any nested containing block (which positions its own
// out-of-flow descendants) and at each out-of-flow widget it finds (which,
// being positioned, is itself a containing block for anything deeper).
func (c *Container) collectAbsoluteDescendants(root Widget) []Widget {
	var out []Widget
	var walk func(w Widget, isRoot bool)
	walk = func(w Widget, isRoot bool) {
		if !isRoot {
			if isCollapsed(w) {
				return // display:none — generates no box, even when positioned
			}
			if isOutOfFlow(w) {
				out = append(out, w)
				return // positioned → its own containing block for deeper abs
			}
			if establishesAbsCB(w) {
				return // nested containing block handles its own descendants
			}
		}
		cl, ok := w.(interface{ ChildList() []Widget })
		if !ok {
			return
		}
		for _, ch := range cl.ChildList() {
			walk(ch, false)
		}
	}
	walk(root, true)
	return out
}

func (c *Container) Draw(canvas Canvas) {
	c.assertUIThread("Container.Draw")
	self := Widget(c)
	if c.Self() != nil {
		self = c.Self()
	}
	if isWidgetEffectivelyHidden(self) {
		return
	}
	// Ask the canvas for its clip bounds. If we're in a dirty-region paint
	// (clipCanvas is the common case), skip our own background draw and
	// any child whose bounds don't intersect the clip — they can't
	// contribute visible pixels to this frame.
	var clip Rect
	if ca, ok := canvas.(ClipAware); ok {
		clip = ca.ClipBoundsLogical()
	}
	bounds := c.Bounds()
	drawSelf := clip.IsEmpty() || bounds.Intersects(clip)
	if drawSelf && c.style.Background.A > 0 {
		if c.style.Radius > 0 {
			canvas.FillRoundedRect(bounds, c.style.Radius, c.style.Background)
		} else {
			canvas.FillRect(bounds, c.style.Background)
		}
	}
	for _, child := range ChildrenInPaintOrder(c.children) {
		// PaintBounds (not Bounds) is what matters here — a child with
		// a shadow halo paints pixels OUTSIDE its layout rect, so
		// using Bounds() would skip the child whenever the dirty
		// region only intersects its halo. That cleared the menu's
		// right-edge shadow whenever a neighboring Button's hover
		// halo overlapped it, with no follow-up redraw.
		if isWidgetHidden(child) {
			continue // no painted subtree; bounds may intentionally be retained
		}
		if !clip.IsEmpty() && !PaintBoundsOf(child).Intersects(clip) {
			continue
		}
		child.Draw(canvas)
	}
}

// Handle is called by Window.dispatch during capture/target/bubble phases.
// Containers no longer cascade to children themselves — Window owns the
// tree traversal. The default implementation only translates the per-
// frame hover-path Enter/Leave events (which Window dispatches to every
// widget on the hit path, not just the leaf) into an OnHoverChange
// callback — enough to power "show controls on parent hover" patterns
// like Postman's per-tab close button without writing a custom widget.
func (c *Container) Handle(event Event) bool {
	c.assertUIThread("Container.Handle")
	me, ok := event.(MouseEvent)
	if !ok {
		return false
	}
	switch me.Type() {
	case EventMouseEnter:
		if !c.hovering {
			c.hovering = true
			if c.OnHoverChange != nil {
				c.OnHoverChange(true)
			}
		}
	case EventMouseLeave:
		if c.hovering {
			c.hovering = false
			if c.OnHoverChange != nil {
				c.OnHoverChange(false)
			}
		}
	}
	return false
}

// Hovering reports whether the cursor is currently inside this
// container's bounds (including any descendant). Read-only — toggled
// by qui's hover dispatch as Enter/Leave events arrive.
func (c *Container) Hovering() bool {
	c.assertUIThread("Container.Hovering")
	return c.hovering
}

func (c *Container) HitTest(p Point) Widget {
	c.assertUIThread("Container.HitTest")
	self := Widget(c)
	if c.Self() != nil {
		self = c.Self()
	}
	if isWidgetEffectivelyHidden(self) {
		return nil
	}
	if !c.Bounds().Contains(p) {
		return nil
	}
	children := ChildrenInPaintOrder(c.children)
	for i := len(children) - 1; i >= 0; i-- {
		// A pointer-transparent target is discarded so the search continues
		// with the next sibling below in z-order and then with us — CSS
		// `pointer-events: none` letting the click through. Each nesting
		// level applies the same filter, so an opaque box containing a
		// transparent child still ends up the target itself.
		if hit := acceptHit(children[i].HitTest(p)); hit != nil {
			return hit
		}
	}
	// No child matched — we're the target. Return the outer widget
	// (if subclassed via SetSelf) so callers see the subclass pointer.
	if isWidgetPointerTransparent(self) {
		return nil // pass through to whatever is behind us
	}
	return self
}

// Tick propagates frame ticks to children. Returns the union of all
// dirty regions reported by descendants (empty if none).
func (c *Container) Tick(now time.Time) Rect {
	c.assertUIThread("Container.Tick")
	var dirty Rect
	for _, child := range c.children {
		if isCollapsed(child) {
			continue
		}
		dirty = dirty.Union(tickWidget(child, now))
	}
	return dirty
}
