package qui

// Widget is the core UI element interface.
//
// Parent/SetParent form the tree-navigation spine. Containers call
// SetParent when a child is added; event dispatch walks up the chain
// to build capture/bubble paths without any ancillary parent map.
//
// InvalidateLayout / IsLayoutDirty / ClearLayoutDirty power layout
// caching. A widget whose size-affecting state changes should
// call InvalidateLayout so the next Window.Step re-runs Measure and
// Layout; otherwise idle frames (cursor blink, hover repaint) skip
// the layout pass entirely. Default implementations on BaseWidget
// bubble InvalidateLayout up via Parent() and record dirtiness only
// at the root — Window consults the root's flag each Step.
type Widget interface {
	Measure(available Size) Size
	Layout(rect Rect)
	Draw(canvas Canvas)
	Handle(event Event) bool
	Bounds() Rect
	// Style exposes legacy mutable storage. New code should read through
	// StyleValue and write through UpdateStyle or SetStyle so invalidation and
	// composite-field ownership remain correct.
	//
	// Deprecated: direct mutations such as w.Style().Padding = x bypass
	// invalidation and will be removed in a future major version.
	Style() *Style
	SetStyle(style Style)
	Enabled() bool
	SetEnabled(enabled bool)
	HitTest(p Point) Widget
	Parent() Widget
	SetParent(Widget)
	InvalidateLayout()
	IsLayoutDirty() bool
	ClearLayoutDirty()
}

// registerFocusable marks widgets that can receive focus.
type registerFocusable interface {
	Focusable() bool
	SetFocused(focused bool)
}

// Draggable marks widgets that can be dragged.
type Draggable interface {
	Draggable() bool
}

// Droppable marks widgets that can accept drops.
type Droppable interface {
	Droppable() bool
}

// PaintBounder reports a widget's visual extent — the rect of pixels it
// can actually write into, which may be larger than Bounds() when the
// widget paints a shadow halo, focus ring, or other effect outside its
// layout box. Container's dirty-region paint uses PaintBounds() rather
// than Bounds() to decide whether a child can be skipped, so a
// neighbor's invalidation that lands inside our halo doesn't clear the
// halo without re-painting it.
//
// Widgets that paint inside Bounds() only (most of them) don't need to
// implement this — the default falls back to Bounds().
type PaintBounder interface {
	PaintBounds() Rect
}

// InteractionTransformer publishes the affine transform that moves a
// widget's interactive subtree without changing retained layout bounds.
// CSS transforms implement this; visual-only drag feedback deliberately
// does not.
type InteractionTransformer interface {
	InteractionTransform() Matrix
}

// PaintTransformer publishes the full transform applied while painting a
// widget subtree, including visual-only feedback. It lets descendant
// invalidations map their dirty rect through transformed ancestors.
type PaintTransformer interface {
	PaintTransform() Matrix
}

// ChildInteractionTransformer publishes the affine transform a widget
// applies to its DESCENDANTS' coordinate space while leaving its own
// layout box where it is. A scroll container is the canonical case: its
// content is laid out once at the viewport origin and the scroll offset
// is a transform, so a child's retained Bounds are in CONTENT
// coordinates and this transform maps them into the container's own
// space.
//
// Contrast with InteractionTransformer (CSS transform), which moves the
// element itself: there, the widget's own bounds are pre-transform. Both
// compose — geometry helpers walk the parent chain applying a widget's
// own transform first, then its parent's child transform.
type ChildInteractionTransformer interface {
	ChildInteractionTransform() Matrix
}

// ChildPaintTransformer is the paint-time counterpart of
// ChildInteractionTransformer: the transform actually concatenated onto
// the canvas before descendants paint, visual-only feedback included.
// Descendant invalidations map their dirty rect through it.
type ChildPaintTransformer interface {
	ChildPaintTransform() Matrix
}

// PaintClipper is implemented by widgets that clip descendant painting to
// a rect expressed in their OWN coordinate space — a scroll viewport.
// Descendant dirty rects are intersected with it as they map outward, so
// invalidating a widget that is scrolled out of view repaints nothing
// instead of dirtying whatever now occupies those pixels.
type PaintClipper interface {
	PaintClip() Rect
}

// PaintBoundsOf returns the visual extent of w — PaintBounds() if
// implemented, otherwise Bounds(). Use this from any code that needs
// to test "does w's painted pixels reach into this rect", e.g.
// dirty-region intersect tests.
func PaintBoundsOf(w Widget) Rect {
	if pb, ok := w.(PaintBounder); ok {
		return pb.PaintBounds()
	}
	if w == nil {
		return Rect{}
	}
	return w.Bounds()
}

// InteractionMatrixOf returns the affine that maps w's OWN coordinate
// space — the space Widget.Bounds is expressed in — to window-logical
// coordinates. It is the composition of w's interaction transform with
// every ancestor's interaction transform and child transform.
//
// Use it (or the WindowPointToLocal / LocalPointToWindow wrappers) at any
// boundary where a window-space point meets retained widget geometry: a
// widget inside a scroll container has bounds in content coordinates, so
// comparing a raw cursor position against them is off by the scroll
// offset.
func InteractionMatrixOf(w Widget) Matrix {
	m := IdentityMatrix()
	for cur := w; cur != nil; cur = cur.Parent() {
		if transformer, ok := cur.(InteractionTransformer); ok {
			m = transformer.InteractionTransform().Concat(m)
		}
		if parent := cur.Parent(); parent != nil {
			if ct, ok := parent.(ChildInteractionTransformer); ok {
				m = ct.ChildInteractionTransform().Concat(m)
			}
		}
	}
	return m
}

// InteractionRectFor maps r through w's interaction transform and every
// transformed ancestor. The input rect is expressed in w's own layout
// coordinates (see InteractionMatrixOf); the result is window-logical.
func InteractionRectFor(w Widget, r Rect) Rect {
	return InteractionMatrixOf(w).TransformRect(r)
}

// InteractionBoundsOf returns the on-screen interactive bounds of w.
func InteractionBoundsOf(w Widget) Rect {
	if w == nil {
		return Rect{}
	}
	return InteractionRectFor(w, w.Bounds())
}

// WindowPointToLocal maps a window-logical point into w's own coordinate
// space, i.e. the space its Bounds and its internal text/hit geometry live
// in. Identity for a widget with no transformed ancestor, so it is safe to
// apply unconditionally. A singular transform returns p unchanged.
func WindowPointToLocal(w Widget, p Point) Point {
	if w == nil {
		return p
	}
	inv, ok := InteractionMatrixOf(w).Invert()
	if !ok {
		return p
	}
	return inv.TransformPoint(p)
}

// LocalPointToWindow is the inverse of WindowPointToLocal: it maps a point
// in w's own coordinate space out to window-logical coordinates. Code that
// takes an event coordinate and hands it to something positioned in window
// space (an overlay anchor, the IME candidate rect) needs this.
func LocalPointToWindow(w Widget, p Point) Point {
	if w == nil {
		return p
	}
	return InteractionMatrixOf(w).TransformPoint(p)
}

// PaintBoundsInWindow maps a widget's own PaintBounds through transformed
// ancestors, clipping at each PaintClipper it passes. PaintBoundsOf already
// includes the widget's own transform when it implements PaintBounder, so
// only ancestor transforms are applied here.
//
// A rect that a scroll viewport clips away entirely comes back empty:
// nothing on screen shows that widget, so nothing needs repainting.
func PaintBoundsInWindow(w Widget) Rect {
	if w == nil {
		return Rect{}
	}
	r := PaintBoundsOf(w)
	for cur := w; cur != nil; cur = cur.Parent() {
		parent := cur.Parent()
		if parent == nil {
			break
		}
		// Into the parent's own space: child transform first, then the
		// parent's clip (expressed in that same space), then the parent's
		// own transform which carries it up one more level.
		if ct, ok := parent.(ChildPaintTransformer); ok {
			r = ct.ChildPaintTransform().TransformRect(r)
		}
		if clipper, ok := parent.(PaintClipper); ok {
			if clip := clipper.PaintClip(); !clip.IsEmpty() {
				if r = r.Intersect(clip); r.IsEmpty() {
					return Rect{}
				}
			}
		}
		if transformer, ok := parent.(PaintTransformer); ok {
			r = transformer.PaintTransform().TransformRect(r)
		}
	}
	return r
}

// BaseWidget provides default behavior for widgets.
//
// The `self` field holds the outer embedding widget so that methods on
// BaseWidget / Container can return the right concrete Widget pointer
// (Go embedding alone can't do virtual dispatch). Widget subclasses
// that rely on being recognized as themselves by the framework —
// primarily by being stored as parents or returned from HitTest —
// should call SetSelf(outer) right after construction. Unset self is
// treated as "not a subclass, return the embedded widget as-is".
type BaseWidget struct {
	id        string
	rect      Rect
	style     Style
	enabled   bool
	parent    Widget
	window    *Window
	self      Widget
	minSize   Size
	maxSize   Size // zero = unconstrained on that axis
	preferred Size // zero = no override on that axis; non-zero replaces Measure on that axis
	tooltip   string
	// tooltipKey, when set, resolves the tooltip from the catalog each
	// time it is shown (tooltip is the fallback).
	tooltipKey string
	// axName overrides the widget's accessible name (SetAccessibleName).
	axName string
	cursor    CursorShape // declared pointer shape (CSS `cursor`); see cursor.go
	hasCursor bool        // whether cursor was declared at all
	// pointerThrough declines pointer targeting so events reach whatever is
	// behind (CSS pointer-events: none). See pointer_events.go.
	pointerThrough bool
	flex           FlexItem
	grid           GridItem
	absolute       AbsolutePosition
	outOfFlow      bool // taken out of normal flow (CSS position:absolute/fixed)
	absCB          bool // establishes a containing block for out-of-flow descendants
	collapsed      bool // removed from layout AND paint entirely (CSS display:none)
	layoutDirty    bool // only the root's flag is consulted; set via InvalidateLayout
	hooks          *widgetHooks // OnFocus / OnBlur / OnKeyDown; see widget_hooks.go
}

func NewBaseWidget() BaseWidget {
	return BaseWidget{style: DefaultStyle(), enabled: true}
}

func (b *BaseWidget) Measure(Size) Size { return Size{} }

// Layout records the assigned rect. Every widget's bounds flow through
// here (rect is unexported), so this is also where the window learns
// that something MOVED: during the frame's layout pass a bounds change
// promotes the paint to a full-window repaint (moved widgets need both
// their old and new pixels — plus shadows/halos — refreshed); outside
// the pass (caller-positioned overlays, tests) the old and new paint
// extents are invalidated directly.
func (b *BaseWidget) Layout(rect Rect) {
	b.assertUIThread("BaseWidget.Layout")
	if b.rect == rect {
		return
	}
	if b.window == nil {
		b.rect = rect
		return
	}
	self := Widget(b)
	if b.self != nil {
		self = b.self
	}
	oldPaint := PaintBoundsInWindow(self)
	b.rect = rect
	b.window.noteBoundsChange(oldPaint, PaintBoundsInWindow(self))
}
func (b *BaseWidget) Draw(Canvas)       {}
func (b *BaseWidget) Handle(Event) bool { return false }
func (b *BaseWidget) Bounds() Rect {
	b.assertUIThread("BaseWidget.Bounds")
	return b.rect
}
func (b *BaseWidget) Style() *Style {
	b.assertUIThread("BaseWidget.Style")
	return &b.style
}
func (b *BaseWidget) SetStyle(style Style) {
	b.assertUIThread("BaseWidget.SetStyle")
	next := style.Clone()
	if stylesEqual(b.style, next) {
		return
	}
	self := Widget(b)
	if b.self != nil {
		self = b.self
	}
	oldPaint := PaintBoundsInWindow(self)
	layoutEqual := styleLayoutEqual(b.style, next)
	b.style = next
	if !layoutEqual {
		b.InvalidateLayout()
		if b.window != nil {
			b.window.InvalidateRect(oldPaint)
		}
		return
	}
	if b.window != nil {
		b.window.InvalidateRect(oldPaint.Union(PaintBoundsInWindow(self)))
	}
}

// StyleValue returns an ownership-safe snapshot of widget's current style.
// Mutating the result has no effect until it is submitted with SetStyle.
func StyleValue(widget Widget) Style {
	if widget == nil {
		return Style{}
	}
	return widget.Style().Clone()
}

// UpdateStyle applies a controlled style mutation. The callback receives an
// ownership-safe copy; SetStyle commits it and performs the required layout
// or paint invalidation.
func UpdateStyle(widget Widget, update func(*Style)) {
	if widget == nil || update == nil {
		return
	}
	next := StyleValue(widget)
	update(&next)
	widget.SetStyle(next)
}

// ApplyStylePatch applies an explicit-presence patch and commits it through
// SetStyle. Use this when zero, false, transparent, nil, or empty values must
// actively clear an existing style field.
func ApplyStylePatch(widget Widget, patch StylePatch) {
	if widget == nil {
		return
	}
	widget.SetStyle(patch.Apply(StyleValue(widget)))
}
func (b *BaseWidget) Enabled() bool {
	b.assertUIThread("BaseWidget.Enabled")
	return b.enabled
}
func (b *BaseWidget) SetEnabled(enabled bool) {
	b.assertUIThread("BaseWidget.SetEnabled")
	if b.enabled == enabled {
		return
	}
	b.enabled = enabled
	b.Invalidate()
}
func (b *BaseWidget) HitTest(p Point) Widget {
	b.assertUIThread("BaseWidget.HitTest")
	self := Widget(b)
	if b.self != nil {
		self = b.self
	}
	if isWidgetEffectivelyHidden(self) {
		return nil
	}
	if b.rect.Contains(p) {
		// Return the outer widget (if subclassed via SetSelf) so callers
		// see the subclass pointer and dispatch reaches its Handle —
		// mirrors Container.HitTest.
		if s := b.self; s != nil {
			return s
		}
		return b
	}
	return nil
}
func (b *BaseWidget) Parent() Widget {
	b.assertUIThread("BaseWidget.Parent")
	return b.parent
}
func (b *BaseWidget) SetParent(p Widget) {
	b.assertUIThread("BaseWidget.SetParent")
	b.parent = p
}

// TooltipText returns the hover-tooltip text set via SetTooltip, or "".
// Satisfies TooltipProvider so any widget embedding BaseWidget (Button,
// Label, …) gets hover tooltips without per-widget wiring — the window's
// hover tracker reads it directly, no AttachTooltip call needed.
func (b *BaseWidget) TooltipText() string {
	b.assertUIThread("BaseWidget.TooltipText")
	if b.tooltipKey != "" {
		return TranslateOr("", b.tooltipKey, b.tooltip, nil)
	}
	return b.tooltip
}

// SetTooltipKey makes the tooltip come from the message catalog, resolved
// each time it shows, so it follows a language switch. fallback is the
// text without a catalog. Pass key "" to go back to SetTooltip's text.
func (b *BaseWidget) SetTooltipKey(key, fallback string) {
	b.assertUIThread("BaseWidget.SetTooltipKey")
	b.tooltipKey = key
	b.tooltip = fallback
}

// SetAccessibleName overrides the name the AX tree (and agent selectors)
// report for this widget — an icon-only button's "Delete", a field whose
// visible label is a separate widget. "" restores the widget's own name.
func (b *BaseWidget) SetAccessibleName(name string) {
	b.assertUIThread("BaseWidget.SetAccessibleName")
	b.axName = name
}

func (b *BaseWidget) accessibleNameOverride() string { return b.axName }

// TooltipKey returns the tooltip's message key, or "".
func (b *BaseWidget) TooltipKey() string { return b.tooltipKey }

// SetTooltip sets (or clears, with "") the hover-tooltip text. The tooltip
// appears near the cursor when this widget is the topmost hovered node and
// disappears when the cursor leaves. Position auto-adapts near the window's
// right / bottom edges (see Window.showTooltip).
func (b *BaseWidget) SetTooltip(text string) {
	b.assertUIThread("BaseWidget.SetTooltip")
	b.tooltip = text
}

// Window returns the window this widget tree is currently attached to.
// It is nil for detached widgets and test-only trees that have not been
// installed with Window.SetRoot / PushOverlay.
func (b *BaseWidget) Window() *Window { return b.window }

// SetWindow records the owning window. Framework tree-management helpers
// call this while mounting/unmounting widgets so programmatic widget
// setters can invalidate themselves without every widget constructor
// accepting a *Window.
func (b *BaseWidget) SetWindow(w *Window) {
	if b.window != nil {
		b.window.assertUIThread("BaseWidget.SetWindow")
	}
	if w != nil && w != b.window {
		w.assertUIThread("BaseWidget.SetWindow")
	}
	b.window = w
}

// Invalidate marks this widget's current bounds as needing repaint.
func (b *BaseWidget) Invalidate() {
	b.assertUIThread("BaseWidget.Invalidate")
	if b.window == nil {
		return
	}
	self := Widget(b)
	if b.self != nil {
		self = b.self
	}
	b.window.InvalidateRect(PaintBoundsInWindow(self))
}

// InvalidateRect marks a widget-local sub-region as dirty.
func (b *BaseWidget) InvalidateRect(r Rect) {
	b.assertUIThread("BaseWidget.InvalidateRect")
	if b.window == nil {
		return
	}
	b.window.InvalidateRect(r)
}

// Self returns the outer embedding widget if one has been registered
// via SetSelf, else nil. Container and friends use this to return the
// subclass pointer (instead of the embedded struct) from HitTest /
// when assigning parent pointers.
func (b *BaseWidget) Self() Widget {
	b.assertUIThread("BaseWidget.Self")
	return b.self
}

// SetSelf registers the outer embedding widget. Call this once after
// construction of any type that embeds BaseWidget (directly or
// transitively via Container) if the type needs to be recognized by
// the framework as itself through the Widget interface.
func (b *BaseWidget) SetSelf(w Widget) {
	b.assertUIThread("BaseWidget.SetSelf")
	b.self = w
}

// FlexItem returns legacy mutable flex storage.
// Deprecated: use FlexItemValue, SetFlexItem, UpdateFlexItem, or SetFlex.
func (b *BaseWidget) FlexItem() *FlexItem {
	b.assertUIThread("BaseWidget.FlexItem")
	return &b.flex
}

// FlexItemValue returns a snapshot of the widget's explicit flex hints.
func (b *BaseWidget) FlexItemValue() FlexItem {
	b.assertUIThread("BaseWidget.FlexItemValue")
	return b.flex
}

// SetFlexItem replaces the widget's explicit flex hints and invalidates
// layout when they changed.
func (b *BaseWidget) SetFlexItem(item FlexItem) {
	b.assertUIThread("BaseWidget.SetFlexItem")
	if b.flex == item {
		return
	}
	b.flex = item
	b.InvalidateLayout()
}

// UpdateFlexItem applies a controlled flex-hint mutation.
func (b *BaseWidget) UpdateFlexItem(update func(*FlexItem)) {
	if update == nil {
		return
	}
	b.assertUIThread("BaseWidget.UpdateFlexItem")
	next := b.flex
	update(&next)
	b.SetFlexItem(next)
}

// SetFlex configures this widget as a flex item that grows into
// remaining main-axis space. Matches CSS `flex: <grow> 1 0` — i.e.
// Grow=grow, Basis=0, default Shrink=1.
//
// Pass 1 (or any positive value) for "fill remaining space". When
// multiple siblings share Grow > 0 the remaining space is split in
// proportion to their Grow values, exactly like CSS flex-grow.
//
// Pass 0 to clear growth without resetting other fields.
func (b *BaseWidget) SetFlex(grow float32) {
	b.assertUIThread("BaseWidget.SetFlex")
	if grow < 0 {
		grow = 0
	}
	next := b.flex
	next.Grow = grow
	next.Basis = 0
	b.SetFlexItem(next)
}

// GridItem returns legacy mutable grid-placement storage.
// Deprecated: use GridItemValue, SetGridItem, UpdateGridItem, or SetGridCell.
func (b *BaseWidget) GridItem() *GridItem {
	b.assertUIThread("BaseWidget.GridItem")
	return &b.grid
}

// GridItemValue returns a snapshot of the widget's explicit grid placement.
func (b *BaseWidget) GridItemValue() GridItem {
	b.assertUIThread("BaseWidget.GridItemValue")
	return b.grid
}

// SetGridItem replaces grid placement and invalidates layout when changed.
func (b *BaseWidget) SetGridItem(item GridItem) {
	b.assertUIThread("BaseWidget.SetGridItem")
	if b.grid == item {
		return
	}
	b.grid = item
	b.InvalidateLayout()
}

// UpdateGridItem applies a controlled grid-placement mutation.
func (b *BaseWidget) UpdateGridItem(update func(*GridItem)) {
	if update == nil {
		return
	}
	b.assertUIThread("BaseWidget.UpdateGridItem")
	next := b.grid
	update(&next)
	b.SetGridItem(next)
}

// SetGridCell places the widget at an explicit grid cell while preserving
// its current spans.
func (b *BaseWidget) SetGridCell(col, row int) {
	b.UpdateGridItem(func(item *GridItem) { item.SetCell(col, row) })
}

// AbsolutePosition returns legacy mutable anchor storage.
// Deprecated: use AbsolutePositionValue, SetAbsolutePosition, or
// UpdateAbsolutePosition.
func (b *BaseWidget) AbsolutePosition() *AbsolutePosition {
	b.assertUIThread("BaseWidget.AbsolutePosition")
	return &b.absolute
}

// AbsolutePositionValue returns a snapshot of the widget's anchor geometry.
func (b *BaseWidget) AbsolutePositionValue() AbsolutePosition {
	b.assertUIThread("BaseWidget.AbsolutePositionValue")
	return b.absolute
}

// SetAbsolutePosition replaces anchor geometry and invalidates layout when
// changed.
func (b *BaseWidget) SetAbsolutePosition(position AbsolutePosition) {
	b.assertUIThread("BaseWidget.SetAbsolutePosition")
	if b.absolute == position {
		return
	}
	b.absolute = position
	b.InvalidateLayout()
}

// UpdateAbsolutePosition applies a controlled anchor-geometry mutation.
func (b *BaseWidget) UpdateAbsolutePosition(update func(*AbsolutePosition)) {
	if update == nil {
		return
	}
	b.assertUIThread("BaseWidget.UpdateAbsolutePosition")
	next := b.absolute
	update(&next)
	b.SetAbsolutePosition(next)
}

// FlexItemOf returns a widget's explicit flex hints through the controlled
// value API, with a compatibility fallback for legacy custom widgets.
func FlexItemOf(widget Widget) FlexItem {
	if widget == nil {
		return FlexItem{}
	}
	if provider, ok := widget.(interface{ FlexItemValue() FlexItem }); ok {
		return provider.FlexItemValue()
	}
	if legacy, ok := widget.(interface{ FlexItem() *FlexItem }); ok {
		if item := legacy.FlexItem(); item != nil {
			return *item
		}
	}
	return FlexItem{}
}

// UpdateFlexItem updates generic widgets while preserving compatibility with
// legacy custom widgets that expose only FlexItem().
func UpdateFlexItem(widget Widget, update func(*FlexItem)) {
	if widget == nil || update == nil {
		return
	}
	next := FlexItemOf(widget)
	update(&next)
	if setter, ok := widget.(interface{ SetFlexItem(FlexItem) }); ok {
		setter.SetFlexItem(next)
		return
	}
	if legacy, ok := widget.(interface{ FlexItem() *FlexItem }); ok {
		if item := legacy.FlexItem(); item != nil && *item != next {
			*item = next
			widget.InvalidateLayout()
		}
	}
}

// GridItemOf returns a widget's explicit grid placement through the value API,
// with a compatibility fallback for legacy custom widgets.
func GridItemOf(widget Widget) GridItem {
	if widget == nil {
		return GridItem{}
	}
	if provider, ok := widget.(interface{ GridItemValue() GridItem }); ok {
		return provider.GridItemValue()
	}
	if legacy, ok := widget.(interface{ GridItem() *GridItem }); ok {
		if item := legacy.GridItem(); item != nil {
			return *item
		}
	}
	return GridItem{}
}

// UpdateGridItem updates grid placement on generic widgets.
func UpdateGridItem(widget Widget, update func(*GridItem)) {
	if widget == nil || update == nil {
		return
	}
	next := GridItemOf(widget)
	update(&next)
	if setter, ok := widget.(interface{ SetGridItem(GridItem) }); ok {
		setter.SetGridItem(next)
		return
	}
	if legacy, ok := widget.(interface{ GridItem() *GridItem }); ok {
		if item := legacy.GridItem(); item != nil && *item != next {
			*item = next
			widget.InvalidateLayout()
		}
	}
}

// AbsolutePositionOf returns a widget's anchor geometry through the value API,
// with a compatibility fallback for legacy custom widgets.
func AbsolutePositionOf(widget Widget) AbsolutePosition {
	if widget == nil {
		return AbsolutePosition{}
	}
	if provider, ok := widget.(interface{ AbsolutePositionValue() AbsolutePosition }); ok {
		return provider.AbsolutePositionValue()
	}
	if legacy, ok := widget.(interface{ AbsolutePosition() *AbsolutePosition }); ok {
		if position := legacy.AbsolutePosition(); position != nil {
			return *position
		}
	}
	return AbsolutePosition{}
}

// UpdateAbsolutePosition updates anchor geometry on generic widgets.
func UpdateAbsolutePosition(widget Widget, update func(*AbsolutePosition)) {
	if widget == nil || update == nil {
		return
	}
	next := AbsolutePositionOf(widget)
	update(&next)
	if setter, ok := widget.(interface{ SetAbsolutePosition(AbsolutePosition) }); ok {
		setter.SetAbsolutePosition(next)
		return
	}
	if legacy, ok := widget.(interface{ AbsolutePosition() *AbsolutePosition }); ok {
		if position := legacy.AbsolutePosition(); position != nil && *position != next {
			*position = next
			widget.InvalidateLayout()
		}
	}
}

// OutOfFlow reports whether the widget is removed from its parent's normal
// flow (CSS position:absolute/fixed): the flow/flex/grid engines skip it
// when sizing + placing in-flow siblings, and the nearest containing-block
// ancestor positions it instead (see Container.Layout). SetOutOfFlow toggles
// it. Its AbsolutePosition anchors drive where it lands.
func (b *BaseWidget) OutOfFlow() bool {
	b.assertUIThread("BaseWidget.OutOfFlow")
	return b.outOfFlow
}
func (b *BaseWidget) SetOutOfFlow(v bool) {
	b.assertUIThread("BaseWidget.SetOutOfFlow")
	if b.outOfFlow == v {
		return
	}
	b.outOfFlow = v
	b.InvalidateLayout()
}

// Collapsed reports whether the widget is removed from its parent entirely
// (CSS display:none): parents skip it when measuring, laying out siblings —
// so flex/grid gaps don't leave a hole where it stood — and painting. Unlike
// OutOfFlow, nothing positions it later; it simply does not generate a box.
// The caller owns re-invalidating layout after a toggle.
func (b *BaseWidget) Collapsed() bool {
	b.assertUIThread("BaseWidget.Collapsed")
	return b.collapsed
}
func (b *BaseWidget) SetCollapsed(v bool) {
	b.assertUIThread("BaseWidget.SetCollapsed")
	if b.collapsed == v {
		return
	}
	self := Widget(b)
	if b.self != nil {
		self = b.self
	}
	if v && b.window != nil {
		b.window.releaseSubtreeInteractions(self)
	}
	b.collapsed = v
	b.InvalidateLayout()
}

// EstablishesAbsContainingBlock reports whether the widget is the containing
// block for out-of-flow descendants (CSS: any positioned ancestor —
// position != static). Its Container positions those descendants relative to
// its own box after laying out in-flow children.
func (b *BaseWidget) EstablishesAbsContainingBlock() bool {
	b.assertUIThread("BaseWidget.EstablishesAbsContainingBlock")
	return b.absCB
}
func (b *BaseWidget) SetEstablishesAbsContainingBlock(v bool) {
	b.assertUIThread("BaseWidget.SetEstablishesAbsContainingBlock")
	if b.absCB == v {
		return
	}
	b.absCB = v
	b.InvalidateLayout()
}

// SetMinSize sets the widget's minimum layout size. Layout engines
// honor these constraints when resolving child rects. Negative values
// are clamped to zero.
func (b *BaseWidget) SetMinSize(width, height float32) {
	b.assertUIThread("BaseWidget.SetMinSize")
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	b.minSize = Size{W: width, H: height}
	b.InvalidateLayout()
}

// MinSize returns the widget's minimum layout size constraint.
func (b *BaseWidget) MinSize() Size {
	b.assertUIThread("BaseWidget.MinSize")
	return b.minSize
}

// SetMaxSize caps the widget's layout size on either axis. A zero value
// on an axis means "unconstrained" (the layout engine's allotted size
// is used). Negative values are clamped to zero. Pair with SetMinSize
// for two-sided clamps, or use SetFixedWidth/SetFixedHeight/SetFixedSize
// when min and max should coincide.
func (b *BaseWidget) SetMaxSize(width, height float32) {
	b.assertUIThread("BaseWidget.SetMaxSize")
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	b.maxSize = Size{W: width, H: height}
	b.InvalidateLayout()
}

// MaxSize returns the widget's maximum layout size cap. Zero on an axis
// means unconstrained.
func (b *BaseWidget) MaxSize() Size {
	b.assertUIThread("BaseWidget.MaxSize")
	return b.maxSize
}

// SetPreferredSize overrides the widget's natural Measure() result on
// the axes where it is non-zero. Layout engines treat this as the
// widget's preferred natural size, then apply min/max constraints on
// top. A zero on an axis leaves Measure() in charge of that axis.
//
// Use when you want a widget to advertise a target size to layout
// without subclassing it to override Measure — e.g. capping a Label's
// natural width while still allowing it to shrink under flex.
func (b *BaseWidget) SetPreferredSize(width, height float32) {
	b.assertUIThread("BaseWidget.SetPreferredSize")
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	b.preferred = Size{W: width, H: height}
	b.InvalidateLayout()
}

// PreferredSize returns the widget's preferred-size override. Zero on
// an axis means "no override; use Measure()".
func (b *BaseWidget) PreferredSize() Size {
	b.assertUIThread("BaseWidget.PreferredSize")
	return b.preferred
}

// SetFixedWidth pins the widget's width by setting both min and max
// width to w. Layout engines honor this as a hard width; the cross
// axis remains driven by Measure / flex / grid as usual.
//
// Pass 0 to clear (resets both min and max width to 0).
func (b *BaseWidget) SetFixedWidth(w float32) {
	b.assertUIThread("BaseWidget.SetFixedWidth")
	if w < 0 {
		w = 0
	}
	b.minSize.W = w
	b.maxSize.W = w
	b.InvalidateLayout()
}

// SetFixedHeight pins the widget's height by setting both min and max
// height to h. Pass 0 to clear.
func (b *BaseWidget) SetFixedHeight(h float32) {
	b.assertUIThread("BaseWidget.SetFixedHeight")
	if h < 0 {
		h = 0
	}
	b.minSize.H = h
	b.maxSize.H = h
	b.InvalidateLayout()
}

// SetFixedSize pins both axes via SetFixedWidth + SetFixedHeight in one
// call. Pass 0 on an axis to leave it unconstrained on that axis.
func (b *BaseWidget) SetFixedSize(w, h float32) {
	b.assertUIThread("BaseWidget.SetFixedSize")
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	b.minSize = Size{W: w, H: h}
	b.maxSize = Size{W: w, H: h}
	b.InvalidateLayout()
}

// ID returns the developer-assigned stable identifier for this widget,
// or "" if none has been set. IDs are opt-in: tools and agents that
// need to address a specific widget across runs (a #submit button, a
// #search box) should call SetID once during construction.
//
// IDs are not required to be unique — Window.Find/FindAll match by
// equality, so a duplicate ID will return the first match in tree
// order. Empty IDs are skipped by selector matching entirely.
func (b *BaseWidget) ID() string {
	b.assertUIThread("BaseWidget.ID")
	return b.id
}

// SetID assigns a stable identifier. Pass "" to clear. Does not
// invalidate layout or repaint; IDs are introspection metadata only.
func (b *BaseWidget) SetID(id string) {
	b.assertUIThread("BaseWidget.SetID")
	b.id = id
}

// InvalidateLayout marks the widget tree as needing a Measure+Layout
// pass on the next Window.Step, and invalidates THIS widget's current
// visual extent. Cheap to call multiple times from the same frame:
// the dirty walk is O(depth) and each call is idempotent.
//
// Repaint scoping: only the caller's own paint bounds are invalidated
// here — NOT the whole window. If the subsequent layout pass moves any
// widget, Window.Step promotes to a full repaint (see BaseWidget.Layout);
// if nothing moves (a text tick that measures to the same size), the
// repaint stays scoped to this rect and the window can actually go idle
// between updates.
//
// Widgets should call this when any state that affects Measure return
// value or child arrangement changes (text content, item count,
// padding, etc.). Pure visual state changes (hover color, cursor
// blink) should use Window.Invalidate / dirtyRegion instead.
func (b *BaseWidget) InvalidateLayout() {
	b.assertUIThread("BaseWidget.InvalidateLayout")
	if b.window != nil {
		self := Widget(b)
		if b.self != nil {
			self = b.self
		}
		b.window.InvalidateRect(PaintBoundsInWindow(self))
	}
	b.markLayoutDirty()
}

// markLayoutDirty walks up the Parent chain and records layout
// dirtiness on the tree root — the only flag Window.Step consults —
// WITHOUT InvalidateLayout's repaint side effect (each ancestor's paint
// bounds grow toward the full window; invalidating them would defeat
// scoped repaints).
func (b *BaseWidget) markLayoutDirty() {
	if b.parent != nil {
		if m, ok := b.parent.(interface{ markLayoutDirty() }); ok {
			m.markLayoutDirty()
			return
		}
		if m, ok := b.parent.(LayoutDirtyMarker); ok {
			m.MarkLayoutDirty()
			return
		}
		// A parent that doesn't embed BaseWidget (out-of-tree custom
		// widget): fall back to its InvalidateLayout.
		b.parent.InvalidateLayout()
		return
	}
	b.layoutDirty = true
}

// MarkLayoutDirty records that the tree needs a relayout without
// repainting anything — the half of InvalidateLayout a custom container
// wants when its children moved but no pixels of its own changed. See
// LayoutDirtyMarker.
func (b *BaseWidget) MarkLayoutDirty() { b.markLayoutDirty() }

// IsLayoutDirty reports whether this widget (as tree root) needs a
// re-layout. Only meaningful on the tree root — child flags aren't
// set by default InvalidateLayout.
func (b *BaseWidget) IsLayoutDirty() bool {
	b.assertUIThread("BaseWidget.IsLayoutDirty")
	return b.layoutDirty
}

// ClearLayoutDirty resets the dirty flag. Called by Window after a
// completed layout pass.
func (b *BaseWidget) ClearLayoutDirty() {
	b.assertUIThread("BaseWidget.ClearLayoutDirty")
	b.layoutDirty = false
}

type windowAware = WindowAware

// AttachWindowTree binds a whole widget subtree to a window, or unbinds it
// when w is nil. It intentionally walks through ChildList so custom
// containers, overlays, and virtual row hosts get the same invalidation
// plumbing as Container children.
//
// Normal applications do not need to call this directly: Window.SetRoot,
// overlay mounting, and Container.AddChild handle it. Custom container-like
// widgets that take ownership of child widgets after they are already
// mounted can use it to keep programmatic child invalidation working.
func AttachWindowTree(widget Widget, w *Window) {
	if widget == nil {
		return
	}
	if w != nil {
		w.assertUIThread("AttachWindowTree")
	}
	if owner, ok := widget.(interface{ Window() *Window }); ok {
		if old := owner.Window(); old != nil && old != w {
			old.assertUIThread("AttachWindowTree")
			old.releaseSubtreeInteractions(widget)
		}
	}
	attachWindowTree(widget, w)
}

func attachWindowTree(widget Widget, w *Window) {
	if aw, ok := widget.(windowAware); ok {
		aw.SetWindow(w)
	}
	if children, ok := widget.(childLister); ok {
		for _, child := range children.ChildList() {
			attachWindowTree(child, w)
		}
	}
}
