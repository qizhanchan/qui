package widgets

import (
	"math"

	. "github.com/qizhanchan/qui"
)

// Box is the generic block-level element — the qui equivalent of HTML's
// <div> (and, via the html-css engine's tag registry, <section>,
// <article>, <header>, <footer>, <nav>, <main>, <aside>, <figure>).
//
// It is a Container that also paints its own CSS box decorations from
// Style: background fill, border, and border-radius. Everything else —
// child composition, layout engine, hit-testing, dispatch — comes from
// Container. Pick any Layout engine (FlowLayout for document flow,
// FlexLayout for flexbox, Grid/Absolute as needed).
//
// Box deliberately reads its colors straight from Style (not the theme)
// so the html-css engine can drive it purely from computed CSS. Leave
// Background.A == 0 for a transparent box and BorderSize == 0 for no
// border — the common <div> case paints nothing of its own.
type Box struct {
	Container
	// BorderPath resolves CSS percentage / elliptical corner radii against
	// the final border box. inset is the border stroke's half width.
	BorderPath func(bounds Rect, inset float32) *Path

	// paintBounds memoizes untransformedPaintBounds for one paint pass
	// (qui.PaintPass); pbGen 0 = none.
	pbGen       uint64
	paintBounds Rect

	// Display selects how this box participates in its parent's flow
	// (FlowBlock is the default). The html-css engine sets it from the
	// element's computed CSS `display`.
	Display FlowLevel

	// Hover / Focus / Active, when set, override the box decorations
	// (Background, Border, BorderSize, Radius) while the box is in the
	// corresponding interactive state — the CSS `:hover` / `:focus` /
	// `:active` states. nil = no styling for that state. When several are
	// active at once they layer base → hover → focus → active (active
	// wins), each applying only the fields it actually sets.
	Hover  *Style
	Focus  *Style
	Active *Style
	// FocusVisible layers over Focus while focus came from the keyboard
	// (CSS :focus-visible) — a focus ring that a click doesn't show.
	FocusVisible *Style
	// AncestorHover overrides the box decorations while an ANCESTOR (or
	// preceding sibling) element's interactive state activates them (CSS
	// `.parent:hover .child`, `.a:active ~ .b`), independent of this box's
	// own state. The html-css engine resolves the style for the currently
	// active triggers each Draw and toggles it via SetAncestorHovered; nil =
	// no ancestor-state box styling. It layers below the self states
	// (base → ancestor-state → hover → focus → active).
	AncestorHover *Style

	// BackgroundShader, when non-nil, fills the box background with a
	// shader (e.g. a CSS gradient) instead of the solid Style.Background.
	// It receives the box's current bounds so gradient endpoints track
	// layout. Rounded corners are honored.
	BackgroundShader func(Rect) Shader

	// Transform applies a CSS-style 2D affine transform. It does not affect
	// layout, but it does affect painting, hit-testing, and PaintBounds.
	// nil = identity.
	Transform *BoxTransform

	// VisualTransform is an additional paint-only transform that deliberately
	// does not affect hit-testing. Drag ghosts use it so the source can follow
	// the pointer without hiding the real drop target underneath.
	VisualTransform *BoxTransform

	// Filter, when non-nil, composites the whole subtree through an offscreen
	// layer transformed by this ImageFilter (CSS filter: blur()/drop-shadow()).
	Filter ImageFilter

	// ClipPath, when non-nil, returns the clip region (CSS clip-path) for
	// the box's current border box: the box and its whole subtree paint
	// only inside the path, and points outside it are not hits.
	ClipPath func(bounds Rect) *Path

	// BackdropFilter, when non-nil, filters what is painted behind the box
	// (CSS backdrop-filter) inside its border box — rounded corners and
	// ClipPath included — before the box paints. Needs a canvas that can
	// read its pixels back (the CPU raster canvas); elsewhere it is skipped.
	BackdropFilter ImageFilter

	// Mask, when non-nil, paints the CSS mask layer for the box's border
	// box: the subtree is composited through an offscreen layer and only
	// kept where the mask paints coverage (its alpha), as mask-image does.
	Mask func(canvas Canvas, bounds Rect)

	// ClipChildren clips descendant painting to the box's bounds
	// (CSS overflow: hidden). Content that exceeds the box is not painted.
	ClipChildren bool

	// PosOffset shifts the box (and its whole subtree) from its normal
	// laid-out position without disturbing siblings — CSS
	// position:relative with top/left offsets. Applied in Layout so bounds
	// and hit-testing stay correct.
	PosOffset Point

	// Positioned marks a positioned box (CSS position other than static).
	// Positioned descendants are painted in layers by their stacking
	// context — after the in-flow content, ordered by z-index — rather than
	// in tree order inside their parent (CSS 2.1 Appendix E), and they are
	// hit-tested first. See stacking.go.
	Positioned bool
	// StackingContext marks a box that forms a CSS stacking context (a
	// z-index on a positioned box, opacity < 1, a transform or filter,
	// position: fixed / sticky, isolation): its positioned descendants
	// layer within it instead of in an outer context.
	StackingContext bool

	focused         bool // has keyboard focus (only meaningful when Focus != nil)
	focusVisible    bool // the focus came from the keyboard (see FocusVisible)
	pressed         bool // mouse button held down inside the box (:active)
	ancestorHovered bool // an ancestor is hovered (drives AncestorHover overlay)
}

// SetAncestorHovered records whether an ancestor-state trigger of this box
// is currently active, so the next Draw applies (or drops) the
// AncestorHover overlay. Set by the html-css engine's Draw.
func (b *Box) SetAncestorHovered(v bool) { b.ancestorHovered = v }

// BoxTransform is a paint-time affine transform applied about the box's
// center (CSS transform-origin: center). SX/SY default to 1 (identity)
// when zero-valued via NewBoxTransform; construct directly only with all
// fields set.
type BoxTransform struct {
	TX, TY float32 // translate (logical px)
	Rotate float32 // rotation in radians
	SX, SY float32 // scale factors (1 = identity)
	KX, KY float32 // skew (radians) along X / Y
	// Matrix, when non-nil, is a full affine override (CSS matrix(...)) applied
	// around the origin, superseding the decomposed fields above.
	Matrix *Matrix
	// OX, OY are the transform-origin as a fraction of the box (0..1), applied
	// only when HasOrigin; otherwise the box center (0.5, 0.5) is used.
	OX, OY    float32
	HasOrigin bool
	// Resolve, when non-nil, supersedes Matrix and the decomposed fields:
	// it builds the transform (about the origin) for a w×h box, so
	// percentage translations follow the box's size (CSS translate(50%)).
	Resolve func(w, h float32) Matrix
}

// FlowLevel reports the box's flow participation to a parent FlowLayout.
func (b *Box) FlowLevel() FlowLevel { return b.Display }

// Focusable reports whether the box can take focus. A plain <div> is not
// focusable — only a box carrying a CSS `:focus` style opts in, so the
// engine doesn't pollute tab order / focus for every container.
func (b *Box) Focusable() bool { return (b.Focus != nil || b.FocusVisible != nil) && b.Enabled() }

// SetFocusVisible records whether the current focus came from the keyboard
// (FocusVisibleAware). Called by the window's focus machinery.
func (b *Box) SetFocusVisible(v bool) {
	if b.focusVisible == v {
		return
	}
	b.focusVisible = v
	if b.FocusVisible != nil && b.focused {
		b.Invalidate()
	}
}

// FocusVisibleNow reports whether the box has focus that came from the
// keyboard.
func (b *Box) FocusVisibleNow() bool { return b.focused && b.focusVisible }

// Focused reports whether the box currently holds keyboard focus, and
// Pressed whether a mouse button is held inside it (:active). The html-css
// engine reads these to swap child text color/decoration for the
// :focus / :active states, which live on child labels the box can't
// repaint itself (see El.applyStateText). Hovering() comes from Container.
func (b *Box) Focused() bool { return b.focused }
func (b *Box) Pressed() bool { return b.pressed }

// SetFocused records keyboard focus and repaints so the :focus variant
// takes effect. Called by the window's focus machinery.
func (b *Box) SetFocused(focused bool) {
	if b.focused == focused {
		return
	}
	b.focused = focused
	if b.Focus != nil || b.FocusVisible != nil {
		b.Invalidate()
	}
}

// Handle tracks hover (via the embedded Container), press state (for
// :active) and repaints when a state style is configured. It never stops
// propagation (returns Container.Handle's value), so a child Label keeps
// receiving the same mouse events for selection.
func (b *Box) Handle(event Event) bool {
	handled := b.Container.Handle(event)
	me, ok := event.(MouseEvent)
	if !ok {
		return handled
	}
	switch me.Type() {
	case EventMouseEnter, EventMouseLeave:
		if b.Hover != nil {
			b.Invalidate()
		}
		if me.Type() == EventMouseLeave && b.pressed {
			// Safety: clear a stuck press if the up is never seen here.
			b.pressed = false
			if b.Active != nil {
				b.Invalidate()
			}
		}
	case EventMouseDown:
		// Reaches the box during the capture phase (box is an ancestor of
		// the hit target), so a press anywhere in the subtree counts.
		if me.Button == MouseButtonLeft && !b.pressed {
			b.pressed = true
			if b.Active != nil {
				b.Invalidate()
			}
		}
	case EventMouseUp:
		if b.pressed {
			b.pressed = false
			if b.Active != nil {
				b.Invalidate()
			}
		}
	}
	return handled
}

// NewBox constructs a Box with the given layout and children. Children
// are wired to the outer (Box) pointer via SetSelf so the dispatch
// parent chain is correct.
func NewBox(layout Layout, children ...Widget) *Box {
	b := &Box{}
	b.BaseWidget = NewBaseWidget()
	b.LayoutEngine = layout
	b.SetSelf(b)
	for _, c := range children {
		b.AddChild(c)
	}
	return b
}

// Layout applies the relative-position offset (if any) before laying out
// the box and its children, so the whole subtree shifts together and
// bounds/hit-testing remain consistent.
func (b *Box) Layout(rect Rect) {
	if b.PosOffset.X != 0 || b.PosOffset.Y != 0 {
		rect.X += b.PosOffset.X
		rect.Y += b.PosOffset.Y
	}
	b.Container.Layout(rect)
}

// SetPositionOffset updates the relative-position offset and invalidates
// layout so the box and its subtree move on the next frame.
func (b *Box) SetPositionOffset(offset Point) {
	if b.PosOffset == offset {
		return
	}
	b.PosOffset = offset
	b.InvalidateLayout()
}

// HitTest maps the window-space point back through the CSS transform before
// using the retained, untransformed layout tree. VisualTransform is excluded
// intentionally: it is feedback-only and must not move the input target.
func (b *Box) HitTest(p Point) Widget {
	if b.Transform != nil {
		inv, ok := boxTransformMatrix(b.Bounds(), b.Transform).Invert()
		if !ok {
			return nil
		}
		p = inv.TransformPoint(p)
	}
	// clip-path cuts the hit region too (CSS Masking 1 § 5: "pointer
	// events must not be dispatched on the clipped-out areas").
	if b.ClipPath != nil {
		if path := b.ClipPath(b.Bounds()); path == nil || !path.Contains(p) {
			return nil
		}
	}
	if !b.stackRoot() {
		if b.inInheritedFloat(p) {
			return nil
		}
		return b.Container.HitTest(p)
	}
	// Layers paint over the in-flow content, so they are hit first —
	// topmost (last painted) first — even outside this box's own bounds
	// (an absolutely positioned menu hanging below its header).
	if isWidgetHiddenForHit(b) {
		return nil
	}
	layers := b.stackLayers()
	split := 0
	for split < len(layers) && zIndexOf(layers[split]) < 0 {
		split++
	}
	clipped := b.ClipChildren && !b.Bounds().Contains(p)
	for i := len(layers) - 1; i >= split; i-- {
		if clipped {
			break
		}
		if hit := AcceptHit(layers[i].HitTest(p)); hit != nil {
			return hit
		}
	}
	if hit := b.Container.HitTest(p); !b.inInheritedFloat(p) && hit != nil && hit != Widget(b) && hit != b.Self() {
		return hit
	}
	for i := split - 1; i >= 0; i-- {
		if clipped {
			break
		}
		if hit := AcceptHit(layers[i].HitTest(p)); hit != nil {
			return hit
		}
	}
	if b.inInheritedFloat(p) {
		return nil
	}
	return b.Container.HitTest(p)
}

// Normal blocks span the full containing width underneath a float, while
// their inline content excludes it. Those block rectangles must not steal
// pointer input from the float painted above their backgrounds.
func (b *Box) inInheritedFloat(p Point) bool {
	fl, ok := b.LayoutEngine.(FlowLayout)
	if !ok || fl.Floats == nil || fl.Floats.ContainsFloats {
		return false
	}
	in := b.Style().ContentInsets()
	p.X -= b.Bounds().X + in.Left
	p.Y -= b.Bounds().Y + in.Top
	for _, f := range fl.Floats.Inherited {
		if f.Rect.Contains(p) {
			return true
		}
	}
	return false
}

// InteractionTransform is the CSS transform exposed to root-level AX/action
// geometry. VisualTransform is intentionally excluded.
func (b *Box) InteractionTransform() Matrix {
	return boxTransformMatrix(b.Bounds(), b.Transform)
}

// PaintTransform is the complete transform applied to this subtree.
func (b *Box) PaintTransform() Matrix { return b.paintTransformMatrix() }

// PaintBounds returns the transformed visual extent of the box and its
// descendants. It is conservative for filters and shadows, which is the
// correct direction for dirty-region culling.
func (b *Box) PaintBounds() Rect {
	return b.paintTransformMatrix().TransformRect(b.untransformedPaintBounds())
}

func (b *Box) untransformedPaintBounds() Rect {
	gen, painting := PaintPass()
	if !painting {
		return b.computePaintBounds()
	}
	if b.pbGen != gen {
		b.paintBounds, b.pbGen = b.computePaintBounds(), gen
	}
	return b.paintBounds
}

func (b *Box) computePaintBounds() Rect {
	bounds := b.Bounds()
	visual := bounds
	if !b.ClipChildren {
		for _, child := range b.ChildList() {
			if boxChildHidden(child) {
				continue
			}
			visual = visual.Union(PaintBoundsOf(child))
		}
	}
	st := b.Style()
	visual = visual.Union(shadowPaintBounds(bounds, st.Shadow))
	for _, shadow := range st.ExtraShadows {
		visual = visual.Union(shadowPaintBounds(bounds, shadow))
	}
	if b.Filter != nil {
		visual = expandRect(visual, 40)
	}
	return visual
}

func (b *Box) Draw(canvas Canvas) {
	bounds := b.Bounds()
	// CSS opacity (< 1) composites the whole subtree through an offscreen
	// layer; CSS transform applies an affine about the box center. Both are
	// paint-only and wrap the normal content draw. Set up once, unwind via
	// RestoreTo so partial state can't leak to siblings.
	//
	// The effects nest in CSS order (Filter Effects 1 § 2, Masking 1):
	// filter, then clip-path, then mask, then opacity — so the clip and the
	// mask cut the filtered result (a drop shadow outside the clip is gone)
	// and opacity fades what is left.
	opacity := b.Style().Opacity
	hasOpacity := opacity > 0 && opacity < 1
	if !hasOpacity && b.Transform == nil && b.VisualTransform == nil && b.Filter == nil &&
		b.ClipPath == nil && b.Mask == nil && b.BackdropFilter == nil {
		b.drawContent(canvas, bounds)
		return
	}
	base := canvas.Save()
	defer canvas.RestoreTo(base)
	if b.Transform != nil || b.VisualTransform != nil {
		canvas.Concat(b.paintTransformMatrix())
	}
	var clipPath *Path
	if b.ClipPath != nil {
		clipPath = b.ClipPath(bounds)
		if clipPath == nil || clipPath.IsEmpty() {
			return // an empty clip region hides the whole subtree
		}
	}
	// The backdrop is read before any layer of this box is pushed: inside
	// one there is nothing behind the box to read.
	if b.BackdropFilter != nil {
		if bf, ok := canvas.(BackdropFilterCanvas); ok {
			shape := clipPath
			if st := b.Style(); shape == nil && (!st.Corners.IsZero() || st.Radius > 0) {
				shape = cornerPath(bounds, st.Corners.Resolved(st.Radius))
			}
			bf.BackdropFilter(bounds, shape, ScaleImageFilter(b.BackdropFilter, canvasScale(canvas)))
		}
	}
	if clipPath != nil {
		canvas.ClipPath(clipPath)
	}
	layerBounds := b.untransformedPaintBounds()
	if hasOpacity || b.Mask != nil {
		p := Paint{}
		if hasOpacity {
			p.Alpha = opacity
		}
		canvas.SaveLayer(layerBounds, p)
	}
	filterDepth := -1
	if b.Filter != nil {
		filterDepth = canvas.SaveLayer(layerBounds, Paint{ImageFilter: ScaleImageFilter(b.Filter, canvasScale(canvas))})
	}
	b.drawContent(canvas, bounds)
	if b.Mask != nil {
		if filterDepth >= 0 {
			canvas.RestoreTo(filterDepth) // composite the filtered content first
		}
		// DstIn keeps the content only where the mask paints coverage. The
		// layer spans the whole content layer so the unmasked rest (outside
		// the mask's painting area) is cleared too, as mask-clip does.
		d := canvas.SaveLayer(layerBounds, Paint{BlendMode: BlendDstIn})
		b.Mask(canvas, bounds)
		canvas.RestoreTo(d)
	}
}

// BackdropFilterCanvas is a canvas that can filter its own pixels in place
// (the CPU raster canvas): BackdropFilter replaces what is painted inside
// bounds (and shape, when non-nil) with filter's output.
type BackdropFilterCanvas interface {
	BackdropFilter(bounds Rect, shape *Path, filter ImageFilter)
}

// ScalableImageFilter is an ImageFilter whose lengths are in logical
// pixels: ScaledImageFilter returns the filter for a device scale (filters
// run on the physical-pixel layer buffer).
type ScalableImageFilter interface {
	ImageFilter
	ScaledImageFilter(scale float32) ImageFilter
}

// ScaleImageFilter converts a filter given in logical pixels into one for
// a canvas whose device scale is scale (HiDPI): blur radii and shadow
// offsets grow with it. Filters it does not know pass through unchanged.
func ScaleImageFilter(f ImageFilter, scale float32) ImageFilter {
	if scale <= 0 || scale == 1 || f == nil {
		return f
	}
	switch v := f.(type) {
	case ScalableImageFilter:
		return v.ScaledImageFilter(scale)
	case BlurImageFilter:
		v.Radius *= scale
		return v
	case DropShadowImageFilter:
		v.Offset.X *= scale
		v.Offset.Y *= scale
		v.Blur *= scale
		return v
	}
	return f
}

// canvasScale is the canvas's current logical→physical scale factor.
func canvasScale(canvas Canvas) float32 {
	m := canvas.CurrentMatrix()
	det := m.A*m.D - m.B*m.C
	if det < 0 {
		det = -det
	}
	if det <= 0 {
		return 1
	}
	return float32(math.Sqrt(float64(det)))
}

// drawContent paints decorations then children, honoring the current clip.
func (b *Box) drawContent(canvas Canvas, bounds Rect) {
	var clip Rect
	if ca, ok := canvas.(ClipAware); ok {
		clip = ca.ClipBoundsLogical()
	}
	// Use the decoration/shadow extent, not the layout rect: when a
	// neighbor dirties only our shadow halo, Container still calls Draw
	// (PaintBounds intersects), and culling on Bounds would clear the halo
	// without repainting it.
	if clip.IsEmpty() || b.PaintBounds().Intersects(clip) {
		b.paintDecorations(canvas, bounds)
	}
	// overflow: hidden — clip descendants to the box's bounds. A per-corner
	// radius clips to the rounded path so children can't overrun the corners.
	if b.ClipChildren {
		depth := canvas.Save()
		defer canvas.RestoreTo(depth)
		if b.BorderPath != nil {
			canvas.ClipPath(b.BorderPath(bounds, 0))
		} else if st := b.Style(); !st.Corners.IsZero() {
			canvas.ClipPath(cornerPath(bounds, st.Corners.Resolved(st.Radius)))
		} else if st.Radius > 0 {
			canvas.ClipPath(NewPath().AddRRect(bounds, st.Radius))
		} else {
			canvas.ClipRect(bounds)
		}
		clip = bounds
		if ca, ok := canvas.(ClipAware); ok {
			clip = ca.ClipBoundsLogical()
		}
	}
	var layers []Widget
	root := b.stackRoot()
	if root {
		layers = b.stackLayers()
	}
	// Negative z-index layers paint under the in-flow content.
	split := 0
	for split < len(layers) && zIndexOf(layers[split]) < 0 {
		split++
	}
	drawLayers := func(ls []Widget) {
		for _, w := range ls {
			if !clip.IsEmpty() && !PaintBoundsOf(w).Intersects(clip) {
				continue
			}
			w.Draw(canvas)
		}
	}
	drawLayers(layers[:split])
	for _, child := range ChildrenInPaintOrder(b.ChildList()) {
		if boxChildHidden(child) || isPositionedBox(child) {
			continue // positioned children paint with their stacking context
		}
		if !clip.IsEmpty() && !PaintBoundsOf(child).Intersects(clip) {
			continue
		}
		child.Draw(canvas)
	}
	drawLayers(layers[split:])
}

// boxTransformMatrix builds the affine matrix used for box painting and
// is translated/rotated/scaled about its center (CSS transform-origin:
// center). Matrix composition applies right-to-left to a point.
func boxTransformMatrix(bounds Rect, t *BoxTransform) Matrix {
	if t == nil {
		return IdentityMatrix()
	}
	ox, oy := float32(0.5), float32(0.5)
	if t.HasOrigin {
		ox, oy = t.OX, t.OY
	}
	cx := bounds.X + ox*bounds.W
	cy := bounds.Y + oy*bounds.H
	m := TranslateMatrix(cx, cy)
	if t.Resolve != nil {
		return m.Concat(t.Resolve(bounds.W, bounds.H)).Concat(TranslateMatrix(-cx, -cy))
	}
	if t.Matrix != nil {
		return m.Concat(*t.Matrix).Concat(TranslateMatrix(-cx, -cy))
	}
	sx, sy := t.SX, t.SY
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	m = m.Concat(TranslateMatrix(t.TX, t.TY))
	if t.Rotate != 0 {
		m = m.Concat(RotateMatrix(t.Rotate))
	}
	if sx != 1 || sy != 1 {
		m = m.Concat(ScaleMatrix(sx, sy))
	}
	if t.KX != 0 || t.KY != 0 {
		m = m.Concat(Matrix{A: 1, B: tanf(t.KX), C: tanf(t.KY), D: 1})
	}
	return m.Concat(TranslateMatrix(-cx, -cy))
}

func (b *Box) paintTransformMatrix() Matrix {
	m := IdentityMatrix()
	// Visual feedback is in parent/window coordinates, so it wraps the CSS
	// transform rather than being scaled or rotated by it.
	if b.VisualTransform != nil {
		m = m.Concat(boxTransformMatrix(b.Bounds(), b.VisualTransform))
	}
	if b.Transform != nil {
		m = m.Concat(boxTransformMatrix(b.Bounds(), b.Transform))
	}
	return m
}

func shadowPaintBounds(bounds Rect, shadow ShadowStyle) Rect {
	if shadow.IsZero() || shadow.Color.A <= 0 || shadow.Inset {
		return Rect{}
	}
	halo := shadow.Blur*3 + shadow.Spread
	if halo < 0 {
		halo = 0
	}
	r := Rect{X: bounds.X + shadow.X, Y: bounds.Y + shadow.Y, W: bounds.W, H: bounds.H}
	return expandRect(r, halo)
}

func expandRect(r Rect, amount float32) Rect {
	if r.IsEmpty() || amount <= 0 {
		return r
	}
	return Rect{X: r.X - amount, Y: r.Y - amount, W: r.W + 2*amount, H: r.H + 2*amount}
}

func boxChildHidden(child Widget) bool {
	if collapsed, ok := child.(interface{ Collapsed() bool }); ok && collapsed.Collapsed() {
		return true
	}
	if hidden, ok := child.(VisibilityHider); ok && hidden.VisibilityHidden() {
		return true
	}
	return false
}

func tanf(r float32) float32 { return float32(math.Tan(float64(r))) }

// paintDecorations fills background then strokes the border, both
// honoring border-radius. Shared shape of the CSS "box" the html-css
// engine expects; kept a method so future additions (box-shadow,
// per-side borders) live in one place.
func (b *Box) paintDecorations(canvas Canvas, bounds Rect) {
	st := b.Style()
	bg, border, borderSize, radius := st.Background, st.Border, st.BorderSize, st.Radius
	// Layer the active state variants base → hover → focus → active, each
	// overriding only the fields it actually sets. Later layers win, so a
	// press (:active) shows over hover/focus.
	overlay := func(s *Style) {
		if s == nil {
			return
		}
		if s.Background.A > 0 {
			bg = s.Background
		}
		if s.BorderSize > 0 {
			border, borderSize = s.Border, s.BorderSize
		}
		if s.Radius > 0 {
			radius = s.Radius
		}
	}
	if b.ancestorHovered {
		overlay(b.AncestorHover)
	}
	if b.Hovering() {
		overlay(b.Hover)
	}
	if b.focused {
		overlay(b.Focus)
		if b.focusVisible {
			overlay(b.FocusVisible)
		}
	}
	if b.pressed {
		overlay(b.Active)
	}
	// Drop shadow paints behind everything (CSS box-shadow, outset only).
	// Extra layers (CSS comma-separated shadows) paint first (farthest back).
	// DrawShadow's Knockout already clips an outset shadow to outside a
	// uniform rounded rect. Only shapes it cannot describe (a border path,
	// per-corner radii) need the layer + dst-out cut, which costs a full
	// offscreen composite of the shadow's extent on every paint.
	shadowDepth := -1
	if (b.BorderPath != nil || !st.Corners.IsZero()) && hasOutsetShadow(st) {
		shadowDepth = canvas.SaveLayer(b.untransformedPaintBounds(), Paint{})
	}
	for i := len(st.ExtraShadows) - 1; i >= 0; i-- {
		if sh := st.ExtraShadows[i]; !sh.IsZero() && !sh.Inset {
			canvas.DrawShadow(bounds, radius, ElevationSpec{
				X: sh.X, Y: sh.Y, Blur: sh.Blur, Spread: sh.Spread, Opacity: 1, Knockout: true,
			}, sh.Color)
		}
	}
	if sh := st.Shadow; !sh.IsZero() && !sh.Inset {
		canvas.DrawShadow(bounds, radius, ElevationSpec{
			X: sh.X, Y: sh.Y, Blur: sh.Blur, Spread: sh.Spread, Opacity: 1, Knockout: true,
		}, sh.Color)
	}
	if shadowDepth >= 0 {
		// An outset shadow is clipped outside the original border box even
		// when the box has a transparent background.
		var shape Shape = ShapeRRect{Rect: bounds, Radius: radius}
		if b.BorderPath != nil {
			shape = ShapePath{Path: b.BorderPath(bounds, 0)}
		} else if !st.Corners.IsZero() {
			shape = ShapePath{Path: cornerPath(bounds, st.Corners.Resolved(radius))}
		}
		cut := canvas.SaveLayer(bounds, Paint{BlendMode: BlendDstOut})
		canvas.DrawShape(shape, Paint{Color: Color{A: 1}, AntiAlias: true})
		canvas.RestoreTo(cut)
		canvas.RestoreTo(shadowDepth)
	}
	// Per-corner radii (CSS border-*-radius) route background + solid border
	// through the path rasterizer; the uniform fast path stays otherwise.
	perCorner := !st.Corners.IsZero()
	var borderPath *Path
	if b.BorderPath != nil {
		borderPath = b.BorderPath(bounds, 0)
	}
	// CSS background-color is below the background image, including its
	// transparent pixels and the area outside a non-repeated image.
	if bg.A > 0 {
		if borderPath != nil {
			canvas.DrawShape(ShapePath{Path: borderPath}, Paint{Style: PaintFill, Color: bg, AntiAlias: true})
		} else if perCorner {
			canvas.DrawShape(ShapePath{Path: cornerPath(bounds, st.Corners.Resolved(radius))}, Paint{Color: bg, AntiAlias: true})
		} else if radius > 0 {
			canvas.FillRoundedRect(bounds, radius, bg)
		} else {
			canvas.FillRect(bounds, bg)
		}
	}
	if b.BackgroundShader != nil {
		if shader := b.BackgroundShader(bounds); shader != nil {
			var shape Shape = ShapeRect(bounds)
			if borderPath != nil {
				shape = ShapePath{Path: borderPath}
			} else if perCorner {
				shape = ShapePath{Path: cornerPath(bounds, st.Corners.Resolved(radius))}
			} else if radius > 0 {
				shape = ShapeRRect{Rect: bounds, Radius: radius}
			}
			canvas.DrawShape(shape, Paint{Style: PaintFill, Shader: shader, AntiAlias: true})
		}
	}
	// Inset shadows sit above the background and below the border/content.
	drawInset := func(sh ShadowStyle) {
		if sh.Inset && !sh.IsZero() {
			canvas.DrawShadow(bounds, radius, ElevationSpec{X: sh.X, Y: sh.Y, Blur: sh.Blur, Spread: sh.Spread, Opacity: 1, Inset: true}, sh.Color)
		}
	}
	for i := len(st.ExtraShadows) - 1; i >= 0; i-- {
		drawInset(st.ExtraShadows[i])
	}
	drawInset(st.Shadow)

	switch {
	case hasPerSideBorders(st):
		paintPerSideBorders(canvas, bounds, st)
	case borderSize > 0 && border.A > 0:
		if st.BorderStyle == BorderDashed || st.BorderStyle == BorderDotted {
			// Uniform but non-solid: paint four dashed/dotted edges (radius
			// is dropped — dashed rounded corners aren't supported).
			paintBorderEdge(canvas, Rect{X: bounds.X, Y: bounds.Y, W: bounds.W, H: borderSize}, true, borderSize, border, st.BorderStyle)
			paintBorderEdge(canvas, Rect{X: bounds.X, Y: bounds.Y + bounds.H - borderSize, W: bounds.W, H: borderSize}, true, borderSize, border, st.BorderStyle)
			paintBorderEdge(canvas, Rect{X: bounds.X, Y: bounds.Y, W: borderSize, H: bounds.H}, false, borderSize, border, st.BorderStyle)
			paintBorderEdge(canvas, Rect{X: bounds.X + bounds.W - borderSize, Y: bounds.Y, W: borderSize, H: bounds.H}, false, borderSize, border, st.BorderStyle)
		} else if borderPath != nil {
			d := canvas.SaveLayer(bounds, Paint{})
			canvas.DrawShape(ShapePath{Path: borderPath}, Paint{Color: border, AntiAlias: true})
			cut := canvas.SaveLayer(bounds, Paint{BlendMode: BlendDstOut})
			canvas.DrawShape(ShapePath{Path: b.BorderPath(bounds, borderSize)},
				Paint{Color: Color{A: 1}, AntiAlias: true})
			canvas.RestoreTo(cut)
			canvas.RestoreTo(d)
		} else if perCorner {
			// Inset by half the stroke width so the outline sits inside the box
			// (matches StrokeRoundedRect, which centers on the rect edge).
			half := borderSize / 2
			r := Rect{X: bounds.X + half, Y: bounds.Y + half, W: bounds.W - borderSize, H: bounds.H - borderSize}
			c := insetCorners(st.Corners.Resolved(radius), half)
			canvas.DrawShape(ShapePath{Path: cornerPath(r, c)},
				Paint{Style: PaintStroke, Color: border, StrokeWidth: borderSize, AntiAlias: true, Join: JoinRound})
		} else if radius > 0 {
			canvas.StrokeRoundedRect(bounds, radius, border, borderSize)
		} else {
			canvas.StrokeRect(bounds, border, borderSize)
		}
	}
}

// cornerPath builds a closed per-corner rounded-rect path for bounds.
func cornerPath(bounds Rect, c CornerRadii) *Path {
	return NewPath().AddRRectCorners(bounds, c.TL, c.TR, c.BR, c.BL)
}

// insetCorners shrinks each corner radius by d (never below 0) so a stroked
// outline inset by d keeps its corners concentric with the fill.
func insetCorners(c CornerRadii, d float32) CornerRadii {
	sub := func(v float32) float32 {
		if v -= d; v < 0 {
			return 0
		}
		return v
	}
	return CornerRadii{TL: sub(c.TL), TR: sub(c.TR), BR: sub(c.BR), BL: sub(c.BL)}
}

// hasPerSideBorders reports whether any per-side border width is set.
func hasPerSideBorders(st *Style) bool {
	w := st.BorderWidths
	return w.Top > 0 || w.Right > 0 || w.Bottom > 0 || w.Left > 0
}

// paintPerSideBorders strokes each edge independently with its own width /
// color / style. Radius is ignored for per-side borders (square corners).
func paintPerSideBorders(canvas Canvas, b Rect, st *Style) {
	if st.BorderStyle == BorderNone {
		return
	}
	w := st.BorderWidths
	sideColor := func(side Color) Color {
		if side.A > 0 {
			return side
		}
		return st.Border
	}
	if st.BorderStyle == BorderSolid {
		// Adjacent CSS borders meet on the corner diagonal. Rectangular
		// strips paint transparent-border arrows as squares and overwrite
		// neighbouring colors at ordinary corners.
		x0, y0, x3, y3 := b.X, b.Y, b.X+b.W, b.Y+b.H
		x1, x2 := x0+w.Left, x3-w.Right
		y1, y2 := y0+w.Top, y3-w.Bottom
		if x1 > x2 {
			x1 = x0 + b.W*w.Left/(w.Left+w.Right)
			x2 = x1
		}
		if y1 > y2 {
			y1 = y0 + b.H*w.Top/(w.Top+w.Bottom)
			y2 = y1
		}
		fill := func(width float32, color Color, a, c, d, e Point) {
			if width <= 0 || color.A <= 0 {
				return
			}
			path := NewPath().MoveTo(a.X, a.Y).LineTo(c.X, c.Y).LineTo(d.X, d.Y).LineTo(e.X, e.Y).Close()
			canvas.DrawShape(ShapePath{Path: path}, DefaultPaint(color))
		}
		fill(w.Top, sideColor(st.BorderColors.Top), Point{x0, y0}, Point{x3, y0}, Point{x2, y1}, Point{x1, y1})
		fill(w.Right, sideColor(st.BorderColors.Right), Point{x3, y0}, Point{x3, y3}, Point{x2, y2}, Point{x2, y1})
		fill(w.Bottom, sideColor(st.BorderColors.Bottom), Point{x3, y3}, Point{x0, y3}, Point{x1, y2}, Point{x2, y2})
		fill(w.Left, sideColor(st.BorderColors.Left), Point{x0, y3}, Point{x0, y0}, Point{x1, y1}, Point{x1, y2})
		return
	}
	if w.Top > 0 {
		paintBorderEdge(canvas, Rect{X: b.X, Y: b.Y, W: b.W, H: w.Top}, true, w.Top, sideColor(st.BorderColors.Top), st.BorderStyle)
	}
	if w.Bottom > 0 {
		paintBorderEdge(canvas, Rect{X: b.X, Y: b.Y + b.H - w.Bottom, W: b.W, H: w.Bottom}, true, w.Bottom, sideColor(st.BorderColors.Bottom), st.BorderStyle)
	}
	if w.Left > 0 {
		paintBorderEdge(canvas, Rect{X: b.X, Y: b.Y, W: w.Left, H: b.H}, false, w.Left, sideColor(st.BorderColors.Left), st.BorderStyle)
	}
	if w.Right > 0 {
		paintBorderEdge(canvas, Rect{X: b.X + b.W - w.Right, Y: b.Y, W: w.Right, H: b.H}, false, w.Right, sideColor(st.BorderColors.Right), st.BorderStyle)
	}
}

// paintBorderEdge fills one border edge. horizontal edges (top/bottom)
// tile dashes along X; vertical edges (left/right) along Y. Solid fills
// the whole edge in one rect.
func paintBorderEdge(canvas Canvas, r Rect, horizontal bool, thickness float32, color Color, style BorderStyle) {
	if color.A <= 0 || thickness <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	if style == BorderSolid || style == BorderNone {
		if style == BorderNone {
			return
		}
		canvas.FillRect(r, color)
		return
	}
	var dash, gap float32
	if style == BorderDotted {
		dash, gap = thickness, thickness
	} else { // dashed
		dash, gap = thickness*3, thickness*2
	}
	if dash <= 0 {
		dash = 1
	}
	if horizontal {
		for x := r.X; x < r.X+r.W; x += dash + gap {
			seg := dash
			if x+seg > r.X+r.W {
				seg = r.X + r.W - x
			}
			canvas.FillRect(Rect{X: x, Y: r.Y, W: seg, H: r.H}, color)
		}
	} else {
		for y := r.Y; y < r.Y+r.H; y += dash + gap {
			seg := dash
			if y+seg > r.Y+r.H {
				seg = r.Y + r.H - y
			}
			canvas.FillRect(Rect{X: r.X, Y: y, W: r.W, H: seg}, color)
		}
	}
}

// ClipsChildren reports whether the box clips its descendants to its bounds
// (CSS overflow: hidden): what lies beyond is not part of an enclosing
// scroller's scrollable overflow.
func (b *Box) ClipsChildren() bool { return b.ClipChildren }

func hasOutsetShadow(st *Style) bool {
	if !st.Shadow.IsZero() && !st.Shadow.Inset {
		return true
	}
	for _, sh := range st.ExtraShadows {
		if !sh.IsZero() && !sh.Inset {
			return true
		}
	}
	return false
}
