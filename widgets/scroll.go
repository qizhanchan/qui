package widgets

import . "github.com/qizhanchan/qui"

// ScrollOrientation selects which axis the ScrollView scrolls. Default
// (zero) is ScrollVertical — back-compat with the original Y-only
// ScrollView. ScrollHorizontal pivots the geometry so the bar lives at
// the bottom and Left/Right keys + DeltaY/DeltaX wheel input scroll
// along X.
//
// Both axes at once (CSS `overflow: auto` on x and y): a vertical view with
// CrossScroll also scrolls X over whatever its content lays out past the
// viewport's right edge.
type ScrollOrientation uint8

const (
	ScrollVertical ScrollOrientation = iota
	ScrollHorizontal
)

// ScrollView is a viewport that shows a portion of a larger Content
// widget. The content is laid out ONCE, at the viewport origin, and
// scrolling is a transform: Draw translates the canvas by -scrollOff on
// the main axis (Y when Orientation is ScrollVertical, X when
// ScrollHorizontal), and the framework maps geometry through it via the
// root's ChildInteractionTransformer / ChildPaintTransformer /
// PaintClipper contracts.
//
// Consequences worth knowing (this changed in the scroll-performance
// rework — it used to re-layout the content on every scroll event, which
// cost O(content) per wheel notch):
//
//   - Child widgets' Bounds() are in CONTENT coordinates: unaffected by
//     the scroll position, offset from the window by -scrollOff. Code that
//     needs on-screen geometry for a descendant uses qui.InteractionBoundsOf
//     (AX tree, agent bounds, overlay anchors) — never raw Bounds().
//   - Event dispatch hands each widget coordinates in its own space, so
//     widgets inside the content compare event coords against their bounds
//     exactly as before (see qui's eventInWidgetSpace).
//   - ScrollTo is O(1): it moves an offset and dirties the viewport rect.
//
// Content size: pass a zero Size to SetContent (or call SetContentAuto)
// and the view measures its content on every layout, so content that grows
// or shrinks scrolls correctly with no bookkeeping. Pass an explicit size
// when the content can't measure itself (a custom layout without
// LayoutMeasurer) or is virtual. Typical vertical use:
//
//	sv := widgets.NewScrollView()
//	content := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, items...)
//	sv.SetContentAuto(content)
//
// Typical horizontal use (e.g. an info-dense toolbar):
//
//	sv := widgets.NewScrollView()
//	sv.Orientation = widgets.ScrollHorizontal
//	natural := toolbar.Measure(qui.Size{W: 1<<14, H: 64})
//	sv.SetContent(toolbar, natural)
//
// Features:
//   - Single-axis scrollbar (right for vertical, bottom for horizontal)
//   - Mouse-wheel scrolling over the viewport (DeltaY drives both axes —
//     horizontal mode also accepts DeltaX from trackpads natively)
//   - Drag the scrollbar thumb to scrub
//   - Arrow keys when focused nudge one "line" (24 px) — Up/Down for
//     vertical, Left/Right for horizontal
//   - PageUp/PageDown deferred until the event enum grows
type ScrollView struct {
	BaseWidget
	Content     Widget
	ContentSize Size
	// FixedViewport makes this view the viewport of a document: its
	// content's position: fixed boxes are laid out against the visible
	// area and its position: sticky boxes stick to it, both following the
	// scroll position (qui layout_viewport.go). A browser sets it on the
	// view showing the page.
	FixedViewport bool
	// StickyScrollport makes this view the scrollport of the position:
	// sticky boxes in its content (a CSS overflow: auto box): they stick
	// to its visible area as it scrolls. FixedViewport implies it.
	StickyScrollport bool
	// UserScrollLocked, when set and reporting true, makes the view ignore
	// the wheel (the page under a modal: overflow hidden on the document
	// root); ScrollTo still moves it.
	UserScrollLocked func() bool
	vpBoxes          []Widget        // fixed / sticky boxes of the content
	cache            *scrollCache    // content cache, see scroll_cache.go
	gpuSet           *gpuLayerSet    // layers composited on the GPU (anim_layer_gpu.go)
	stickyBase       map[Widget]Rect // sticky boxes' in-flow rects
	stickyPlaced     map[Widget]Rect // where placeViewportBoxes last put them
	// Orientation selects the scroll axis. Zero value = ScrollVertical.
	Orientation ScrollOrientation
	// ScrollStep is the pixel delta per wheel notch. 0 means the default.
	ScrollStep float32
	// FocusDelegated makes the view unfocusable because the widget hosting
	// it takes focus instead (an htmlcss overflow element with tabindex)
	// and forwards its scrolling keys through ScrollKey — so the pair is
	// one Tab stop and one click-focus target, not two.
	FocusDelegated bool

	// scrollOff is the main-axis offset (Y for vertical, X for
	// horizontal). Kept under one field so the layout / bar / drag
	// math doesn't fork by axis at every step.
	scrollOff float32

	// BoxModel lays the viewport out like a CSS box: Style().Padding and
	// Border move the content inward and the scrollbar sits on the padding
	// edge. Off by default for compatibility with the historical
	// bounds-as-viewport behavior; opt in when you style the ScrollView
	// itself. htmlcss applies padding to an inner wrapper, so it leaves
	// this off to avoid double-inset.
	BoxModel bool

	// Scrollbar drag state.
	barDrag ScrollBarDrag
	// wheelLatched: the trackpad gesture in progress began on this view and
	// scrolled it, so the rest of it (momentum included) stays here even at
	// the edge — Chrome's scroll latching. A gesture that began elsewhere
	// passes through.
	wheelLatched bool

	// sizeAuto: ContentSize is re-measured from Content on every layout.
	// lastAuto is the last derived size; a ContentSize that differs was
	// written by the app, which turns auto sizing off.
	sizeAuto bool
	lastAuto Size
	// explicitW/H record that the caller supplied that axis via SetContent.
	// The auto axis is re-measured; the other stays authoritative.
	explicitW, explicitH bool

	// BarColors overrides the scrollbar palette (zero fields = theme).
	BarColors ScrollbarColors
	// HideBar drops the scrollbar entirely — no slot, no paint, no hit
	// area — while wheel, keys and ScrollTo keep scrolling (CSS
	// `scrollbar-width: none`; e.g. an editor's tab strip).
	HideBar bool
	// OverlayBar paints the scrollbar over the content instead of in a
	// slot beside it, so the content keeps the full viewport width — the
	// overlay scrollbars of macOS (and of Chrome there). htmlcss scroll
	// containers use it, matching how a browser lays them out.
	OverlayBar bool
	// FitContent sizes the viewport on the scroll axis like a CSS
	// `overflow: auto` box of auto height: as tall (wide) as its content,
	// up to the space available — a max-height-capped popup list shrinks
	// to a single row. Off, the viewport fills whatever it is given.
	FitContent bool

	// CrossScroll makes a vertical view scroll on X too, like a CSS box with
	// `overflow: auto` on both axes: the content is still laid out at the
	// viewport's width, and whatever of it overflows to the right (the
	// widest descendant's right edge — CSS scrollable overflow) can be
	// scrolled into view with a horizontal bar, DeltaX wheel input and
	// the Left / Right keys.
	CrossScroll bool
	// scrollX is the CrossScroll offset; overflowW the content's
	// scrollable width at the last layout.
	scrollX   float32
	overflowW float32
	barDragX  ScrollBarDrag

	contentZoom float32 // SetContentZoom (scroll_zoom.go); 0 = none

	// PixelScroll moves the pixels already on screen when the offset
	// changes (Window.ScrollPixels) and repaints only the strip the move
	// exposes, the scrollbars and the fixed / sticky boxes, instead of the
	// whole viewport. Only for content that moves rigidly with the scroll
	// (see scroll_blit.go); a browser page view sets it.
	PixelScroll bool

	// OnScroll, when set, is invoked whenever the scroll offset changes
	// value (wheel, drag, track click, keys, or ScrollTo). Hosts use it
	// to mirror scroll position across panes.
	OnScroll func()

	focused bool
}

const (
	scrollBarThickness = VBarWidth
	defaultWheelStep   = 40
	defaultKeyStep     = 24
)

// NewScrollView creates an empty vertical scroll view. Set Orientation
// before SetContent if you want horizontal. Call SetContent before
// adding it to the tree.
func NewScrollView() *ScrollView {
	s := &ScrollView{BaseWidget: NewBaseWidget()}
	s.SetSelf(s)
	// Transparent viewport by default — a scroll container should show
	// whatever is behind it (the parent / page background), like a CSS
	// overflow box. Callers set Style().Background for an opaque viewport.
	// (The old near-black default clashed with light surfaces and showed
	// through the now-translucent scrollbar track.)
	s.Style().Background = Color{}
	return s
}

// SetContent assigns the widget to scroll and its natural size.
// The ScrollView takes parent ownership — content.Parent() is wired
// back so phased event dispatch can walk the tree.
func (s *ScrollView) SetContent(widget Widget, contentSize Size) {
	main := contentSize.H
	if s.Orientation == ScrollHorizontal {
		main = contentSize.W
	}
	auto := main <= 0
	explicitW, explicitH := contentSize.W > 0, contentSize.H > 0
	if s.Content == widget {
		s.sizeAuto = auto
		s.lastAuto = contentSize
		s.explicitW, s.explicitH = explicitW, explicitH
		s.ContentSize = contentSize
		s.ClampScroll()
		s.InvalidateLayout()
		return
	}
	// Adopt the replacement first. If it is owned by a parent that cannot
	// release it, the old content stays mounted instead of leaving the
	// ScrollView empty.
	if widget != nil && !AdoptWidgetTree(widget, s, s.Window()) {
		s.InvalidateLayout()
		return
	}
	if old := s.Content; old != nil && old != widget {
		s.ReleaseChildForTransfer(old)
		DetachWidgetTree(old)
	}
	s.sizeAuto = auto
	s.lastAuto = contentSize
	s.explicitW, s.explicitH = explicitW, explicitH
	s.ContentSize = contentSize
	s.Content = widget
	s.ClampScroll()
	s.InvalidateLayout()
}

// SetContentAuto sets the content and has the view measure it on every
// layout (ContentSize then always reflects the content's natural size).
func (s *ScrollView) SetContentAuto(widget Widget) { s.SetContent(widget, Size{}) }

func (s *ScrollView) ReleaseChildForTransfer(child Widget) bool {
	if s.Content != child {
		return false
	}
	s.Content = nil
	if child.Parent() == s {
		child.SetParent(nil)
	}
	s.InvalidateLayout()
	return true
}

// ChildList lets the framework walk into our content for focus
// collection, tick propagation, and broadcast-style traversals.
func (s *ScrollView) ChildList() []Widget {
	if s.Content == nil {
		return nil
	}
	return []Widget{s.Content}
}

func (s *ScrollView) Focusable() bool { return s.Enabled() && !s.FocusDelegated }
func (s *ScrollView) CancelInteraction() {
	ended := s.barDrag.End()
	if s.barDragX.End() || ended {
		s.Invalidate()
	}
}
func (s *ScrollView) SetFocused(f bool) {
	if s.focused == f {
		return
	}
	s.focused = f
	s.Invalidate()
}

func (s *ScrollView) Measure(available Size) Size {
	// Main-axis: report `available` so the viewport flexes to whatever
	// the parent allocates (the entire point of a scroll view is to
	// clip oversized content into the parent's slot).
	//
	// Cross-axis: snap to the content's natural cross dimension when
	// known and smaller than available. Without this, a horizontal
	// ScrollView dropped into a vertical flex would report a huge H —
	// stretching the row to fill the screen even though the toolbar
	// inside only needs 76 px. The vertical case is symmetric.
	out := available
	if s.Content != nil && s.FitContent {
		natural := s.fitNatural(available)
		// A non-positive extent is unconstrained: report the content's.
		if s.Orientation == ScrollHorizontal && (out.W <= 0 || natural.W < out.W) {
			out.W = natural.W
		}
		if s.Orientation != ScrollHorizontal && (out.H <= 0 || natural.H < out.H) {
			out.H = natural.H
		}
	}
	if s.Content != nil {
		if s.Orientation == ScrollHorizontal && s.ContentSize.H > 0 && s.ContentSize.H < out.H {
			out.H = s.ContentSize.H
		}
		if s.Orientation == ScrollVertical && s.ContentSize.W > 0 && s.ContentSize.W < out.W {
			out.W = s.ContentSize.W
		}
	}
	return out
}

// fitNatural is the content's size for FitContent. Content that overflows
// the box unbounded (measured with the scroll axis open, which only the
// cross axis keys, so it stays memoized while the box resizes) fills the
// box at any bounded size too; the box then takes available whole and the
// bounded measurement is skipped. Re-measuring large content at every
// height of a dragged panel border made each frame of the drag re-measure
// all of it. Smaller content is measured bounded, as before.
func (s *ScrollView) fitNatural(available Size) Size {
	open := Size{W: available.W}
	main := func(sz Size) float32 { return sz.H }
	if s.Orientation == ScrollHorizontal {
		open = Size{H: available.H}
		main = func(sz Size) float32 { return sz.W }
	}
	natural := MeasureChild(s.Content, open)
	if avail := main(available); avail > 0 && main(natural) >= avail {
		return natural
	}
	return s.Content.Measure(available)
}

func (s *ScrollView) Layout(rect Rect) {
	s.BaseWidget.Layout(rect)
	s.registerScrollLayer()
	s.ensureContentSize(s.unzoom(s.innerBounds()))
	s.ClampScroll()
	if s.Content == nil {
		return
	}
	// Content rect: anchored at the viewport origin (NOT offset by the
	// scroll position) and sized to the full content extent, minus the
	// cross-axis slot reserved for the scrollbar. The scroll offset is a
	// paint/interaction transform, so this layout is independent of it —
	// scrolling never re-enters here.
	r := s.contentRect()
	MeasureChild(s.Content, Size{W: r.W, H: r.H})
	s.Content.Layout(r)
	if s.FixedViewport || s.StickyScrollport {
		s.vpBoxes = ViewportPositioned(s.Content)
		if !s.FixedViewport {
			// Fixed boxes belong to the document viewport, not to a
			// nested scroller.
			kept := s.vpBoxes[:0]
			for _, w := range s.vpBoxes {
				if ViewportPositioningOf(w) == ViewportSticky {
					kept = append(kept, w)
				}
			}
			s.vpBoxes = kept
		}
		old, placed := s.stickyBase, s.stickyPlaced
		s.stickyBase = map[Widget]Rect{}
		s.stickyPlaced = map[Widget]Rect{}
		for _, w := range s.vpBoxes {
			if ViewportPositioningOf(w) != ViewportSticky {
				continue
			}
			// A subtree the layout pass skipped (unchanged) still holds
			// the shifted rect this view gave it: keep its in-flow base.
			if p, ok := placed[w]; ok && p == w.Bounds() {
				s.stickyBase[w] = old[w]
			} else {
				s.stickyBase[w] = w.Bounds()
			}
		}
	}
	if s.crossScrolls() {
		s.overflowW = scrollableWidth(s.Content, r.X)
	} else {
		s.overflowW = 0
	}
	s.clampX(s.scrollX)
	s.placeViewportBoxes()
}

// placeViewportBoxes lays the content's fixed and sticky boxes out for the
// current scroll position.
func (s *ScrollView) placeViewportBoxes() {
	if (!s.FixedViewport && !s.StickyScrollport) || len(s.vpBoxes) == 0 {
		return
	}
	cr := s.contentRect()
	vp := s.unzoom(s.viewportRect())
	view := Rect{X: cr.X + s.scrollX, Y: cr.Y + s.scrollOff, W: vp.W, H: vp.H}
	if s.Orientation == ScrollHorizontal {
		view = Rect{X: cr.X + s.scrollOff, Y: cr.Y, W: vp.W, H: vp.H}
	}
	scroll := Point{X: view.X - cr.X, Y: view.Y - cr.Y}
	for _, w := range s.vpBoxes {
		switch ViewportPositioningOf(w) {
		case ViewportFixed:
			PlaceFixed(w, view, scroll)
		case ViewportSticky:
			base, ok := s.stickyBase[w]
			if !ok {
				continue
			}
			r := StickyRect(w, base, view)
			if r != w.Bounds() {
				w.Layout(r)
			}
			s.stickyPlaced[w] = r
		}
	}
}

// crossScrolls reports whether the view scrolls on X as well.
func (s *ScrollView) crossScrolls() bool {
	return s.CrossScroll && s.Orientation == ScrollVertical
}

// scrollableWidth is how far right of originX w's subtree reaches: the
// right edge of every visible descendant, not looking inside one that
// clips its children (overflow: hidden, a nested scroller) — what CSS
// counts as a scroll container's scrollable overflow.
func scrollableWidth(w Widget, originX float32) float32 {
	var right float32
	var walk func(Widget)
	walk = func(w Widget) {
		if w == nil || boxChildHidden(w) {
			return
		}
		if ViewportPositioningOf(w) == ViewportFixed {
			return // a fixed box does not extend the document
		}
		if b := w.Bounds(); b.W > 0 && b.X+b.W > right {
			right = b.X + b.W
		}
		if _, ok := w.(PaintClipper); ok {
			return
		}
		if c, ok := w.(interface{ ClipsChildren() bool }); ok && c.ClipsChildren() {
			return
		}
		if cl, ok := w.(ChildLister); ok {
			for _, k := range cl.ChildList() {
				walk(k)
			}
		}
	}
	walk(w)
	return max(0, right-originX)
}

// contentRect is the rect the content is laid out at: the viewport
// origin, the full content extent on the main axis, and the viewport
// extent minus the scrollbar slot on the cross axis.
// innerBounds is the padding+border box the viewport lives in.
func (s *ScrollView) innerBounds() Rect {
	b := s.Bounds()
	if !s.BoxModel {
		return b
	}
	b = b.Inset(s.Style().ContentInsets())
	if b.W < 0 {
		b.W = 0
	}
	if b.H < 0 {
		b.H = 0
	}
	return b
}

func (s *ScrollView) contentRect() Rect {
	b := s.innerBounds()
	if s.Orientation == ScrollHorizontal {
		return Rect{X: b.X, Y: b.Y, W: s.ContentSize.W, H: (b.H - s.barSlot()) / s.zoomFactor()}
	}
	return Rect{X: b.X, Y: b.Y, W: (b.W - s.barSlot()) / s.zoomFactor(), H: s.ContentSize.H}
}

// viewportRect is the on-screen area the content is visible through:
// our bounds minus the scrollbar slot.
func (s *ScrollView) viewportRect() Rect {
	b := s.innerBounds()
	if s.Orientation == ScrollHorizontal {
		b.H -= s.barSlot()
	} else {
		b.W -= s.barSlot()
	}
	return b
}

// barSlot is the cross-axis space the scrollbar takes from the content: its
// thickness for a classic bar, nothing for an overlay one (or none).
func (s *ScrollView) barSlot() float32 {
	if !s.hasBar() || s.OverlayBar {
		return 0
	}
	return scrollBarThickness
}

// contentTranslation is the offset from content coordinates to our own
// coordinate space.
func (s *ScrollView) contentTranslation() (dx, dy float32) {
	if s.Orientation == ScrollHorizontal {
		return -s.scrollOff, 0
	}
	return -s.scrollX, -s.scrollOff
}

// ChildInteractionTransform maps the content subtree's retained (content-
// space) geometry into our own space — the root's contract for a widget
// that transforms its descendants without moving its own box. Hit
// testing, AX bounds, agent geometry, and per-widget event coordinates
// all flow through it.
func (s *ScrollView) ChildInteractionTransform() Matrix {
	return s.contentMatrix()
}

// ChildPaintTransform is the paint-time counterpart: the same translation
// Draw concatenates onto the canvas, so a descendant's Invalidate() maps
// its dirty rect to the right screen pixels.
func (s *ScrollView) ChildPaintTransform() Matrix {
	return s.contentMatrix()
}

// PaintClip reports the viewport, so a descendant that is scrolled out of
// view invalidates nothing instead of dirtying whatever occupies those
// pixels now.
func (s *ScrollView) PaintClip() Rect { return s.viewportRect() }

// viewportFill returns the color to paint behind the viewport and whether to
// paint at all. A transparent Style().Background — the NewScrollView default —
// yields ok=false so a nested scroll host stays transparent and reveals
// whatever its parent already painted. The window clears to the theme page
// color each frame, so a root scroll view needs no fill.
func (s *ScrollView) viewportFill() (Color, bool) {
	bg := s.Style().Background
	if bg.A == 0 {
		return Color{}, false
	}
	return bg, true
}

func (s *ScrollView) Draw(canvas Canvas) {
	b := s.Bounds()

	// Paint the viewport only when the caller set an opaque background (see
	// viewportFill). Transparent is the default and must remain transparent so
	// a NESTED scroll host shows what its parent already drew — the CSS
	// overflow-box contract. Substituting the theme Surface here (the old
	// behavior) repainted an opaque card over the ancestor background, which
	// broke light UIs running on a dark theme.
	if bg, ok := s.viewportFill(); ok {
		if st := s.Style(); st.Radius > 0 {
			canvas.FillRoundedRect(b, st.Radius, bg)
		} else {
			canvas.FillRect(b, bg)
		}
	}
	if st := s.Style(); s.BoxModel && st.BorderSize > 0 && st.Border.A > 0 {
		if st.Radius > 0 {
			canvas.StrokeRoundedRect(b, st.Radius, st.Border, st.BorderSize)
		} else {
			canvas.StrokeRect(b, st.Border, st.BorderSize)
		}
	}

	// Clip content drawing to the viewport (minus the bar slot, so the
	// content area never paints under the bar), then translate by the
	// scroll offset: the content subtree is retained at content
	// coordinates and moves only here.
	viewportClip := s.viewportRect()
	if s.Content != nil {
		var effectiveClip Rect
		if ca, ok := canvas.(ClipAware); ok {
			// Intersect our viewport with the outer clip so we don't
			// paint beyond whatever the dirty-region pass allows. Use the
			// LOGICAL clip — viewportClip and the ClipRect call below
			// both work in logical coords; ClipBounds() (physical pixels)
			// would mismatch on HiDPI and cull/clip the content wrongly.
			effectiveClip = ca.ClipBoundsLogical().Intersect(viewportClip)
		} else {
			effectiveClip = viewportClip
		}
		if !effectiveClip.IsEmpty() && !s.drawCached(canvas, effectiveClip, viewportClip) {
			depth := canvas.Save()
			canvas.ClipRect(effectiveClip)
			// After this, ClipBoundsLogical() reports the clip in CONTENT
			// coordinates (canvasState inverts the current matrix), so the
			// content's own dirty-region culling keeps working unchanged.
			s.applyContentTransform(canvas)
			if !boxChildHidden(s.Content) {
				s.Content.Draw(canvas)
			}
			canvas.RestoreTo(depth)
		}
	}

	// Scrollbar on top, outside the content clip.
	if s.hasBar() {
		s.drawBar(canvas)
	}
	if s.hasXBar() {
		s.barX().Draw(canvas, s.barDragX.Active, s.BarColors)
	}
}

func (s *ScrollView) Handle(event Event) bool {
	if !s.Enabled() {
		return false
	}
	switch e := event.(type) {
	case MouseEvent:
		switch e.Type() {
		case EventScroll:
			// Let the target widget consume wheel first (nested scrollable
			// controls like TextArea). We only act at target/bubble phases.
			if e.Phase() == PhaseCapture {
				return false
			}
			if s.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return s.wheel(e)
			}
		case EventMouseDown:
			if !s.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return false
			}
			// Thumb press starts a drag; a track press either side of it
			// pages toward the click.
			if s.hasBar() {
				if off, hit := s.bar().Press(Point{X: e.X, Y: e.Y}, &s.barDrag); hit {
					s.ScrollTo(off)
					s.Invalidate()
					return true
				}
			}
			if s.hasXBar() {
				if off, hit := s.barX().Press(Point{X: e.X, Y: e.Y}, &s.barDragX); hit {
					s.ScrollToX(off)
					s.Invalidate()
					return true
				}
			}
		case EventMouseMove:
			if s.barDrag.Active {
				s.ScrollTo(s.barDrag.Offset(s.bar(), Point{X: e.X, Y: e.Y}))
				return true
			}
			if s.barDragX.Active {
				s.ScrollToX(s.barDragX.Offset(s.barX(), Point{X: e.X, Y: e.Y}))
				return true
			}
		case EventMouseUp:
			ended := s.barDrag.End()
			if s.barDragX.End() || ended {
				s.Invalidate()
				return true
			}
		}
	case KeyEvent:
		if s.focused {
			return s.ScrollKey(e)
		}
	}
	return false
}

// wheel scrolls by a wheel / trackpad event and reports whether the view
// took it. Like Chrome's scroll chaining, a view that cannot move in the
// event's direction (content that fits, or already at that edge) lets it
// bubble to an enclosing scroller instead of swallowing it — a FitContent
// list inside a scrolling page used to eat every wheel over it. A trackpad
// gesture latches to the view its first event scrolled (see wheelLatched).
func (s *ScrollView) wheel(e MouseEvent) bool {
	if s.UserScrollLocked != nil && s.UserScrollLocked() {
		return false
	}
	if s.crossScrolls() {
		return s.wheel2D(e)
	}
	delta := s.scrollDeltaFor(e.DeltaX, e.DeltaY)
	switch e.ScrollPhase {
	case GesturePhaseBegan:
		s.wheelLatched = false
	case GesturePhaseChanged, GesturePhaseMomentum:
		if s.wheelLatched {
			if delta != 0 {
				s.scrollBy(s.wheelPixels(delta, e.ScrollPhase))
			}
			return true
		}
	case GesturePhaseEnded, GesturePhaseCancelled:
		latched := s.wheelLatched
		if e.ScrollPhase == GesturePhaseCancelled || delta == 0 {
			s.wheelLatched = false
		}
		if latched {
			if delta != 0 {
				s.scrollBy(s.wheelPixels(delta, e.ScrollPhase))
			}
			return true
		}
	}
	if delta == 0 {
		return false
	}
	old := s.scrollOff
	s.scrollBy(s.wheelPixels(delta, e.ScrollPhase))
	if s.scrollOff == old {
		return false
	}
	if e.ScrollPhase != GesturePhaseNone {
		s.wheelLatched = true
	}
	return true
}

// wheel2D is wheel for a view scrolling both axes: DeltaY moves Y and
// DeltaX moves X, with the same chaining and gesture latching.
func (s *ScrollView) wheel2D(e MouseEvent) bool {
	dy, dx := -e.DeltaY, -e.DeltaX
	apply := func() {
		if dy != 0 {
			s.scrollBy(s.wheelPixels(dy, e.ScrollPhase))
		}
		if dx != 0 {
			s.ScrollToX(s.scrollX + s.wheelPixels(dx, e.ScrollPhase))
		}
	}
	switch e.ScrollPhase {
	case GesturePhaseBegan:
		s.wheelLatched = false
	case GesturePhaseChanged, GesturePhaseMomentum:
		if s.wheelLatched {
			apply()
			return true
		}
	case GesturePhaseEnded, GesturePhaseCancelled:
		latched := s.wheelLatched
		if e.ScrollPhase == GesturePhaseCancelled || (dx == 0 && dy == 0) {
			s.wheelLatched = false
		}
		if latched {
			apply()
			return true
		}
	}
	if dx == 0 && dy == 0 {
		return false
	}
	oldY, oldX := s.scrollOff, s.scrollX
	apply()
	if s.scrollOff == oldY && s.scrollX == oldX {
		return false
	}
	if e.ScrollPhase != GesturePhaseNone {
		s.wheelLatched = true
	}
	return true
}

// ScrollKey applies the view's keyboard scrolling (the arrow keys along its
// axis) to a key-down and reports whether it scrolled. Handle calls it
// while the view is focused; a host with FocusDelegated calls it for the
// keys its own focused widget didn't use.
func (s *ScrollView) ScrollKey(e KeyEvent) bool {
	if e.Type() != EventKeyDown {
		return false
	}
	back, fwd := KeyUp, KeyDown
	if s.Orientation == ScrollHorizontal {
		back, fwd = KeyLeft, KeyRight
	}
	switch e.Key {
	case back:
		s.scrollBy(-defaultKeyStep)
		return true
	case fwd:
		s.scrollBy(defaultKeyStep)
		return true
	}
	if s.crossScrolls() && s.maxScrollX() > 0 {
		switch e.Key {
		case KeyLeft:
			s.ScrollToX(s.scrollX - defaultKeyStep)
			return true
		case KeyRight:
			s.ScrollToX(s.scrollX + defaultKeyStep)
			return true
		}
	}
	return false
}

// HitTest maps the point back through the scroll translation before
// descending into the retained content tree, whose bounds are in content
// coordinates.
func (s *ScrollView) HitTest(p Point) Widget {
	if !s.Bounds().Contains(p) {
		return nil
	}
	// The thumb always wins. A classic (in-slot) bar's track does too, but
	// an overlay bar is painted OVER the content: claiming its whole 10 px
	// strip would make that strip of the page unclickable, so content gets
	// first refusal and the bar is only the fallback.
	if s.hasBar() {
		if s.barThumb().Contains(p) {
			return s
		}
		if !s.OverlayBar && s.barTrack().Contains(p) {
			return s
		}
	}
	if s.hasXBar() {
		if s.barX().Thumb().Contains(p) {
			return s
		}
		if !s.OverlayBar && s.barX().Track.Contains(p) {
			return s
		}
	}
	if s.Content != nil {
		local := s.toContent(p)
		if hit := s.Content.HitTest(local); hit != nil {
			return hit
		}
		if s.crossScrolls() {
			// Scrolled sideways, the point can sit right of the content's
			// own box, over a descendant that overflows it (a nowrap row):
			// containers stop at their own bounds, so look for it directly.
			if hit := hitOverflow(s.Content, local); hit != nil {
				return hit
			}
		}
	}
	return s
}

// hitOverflow finds the topmost descendant of w under p when p lies outside
// w's own box: CSS hit-tests a child that overflows a non-clipping parent.
// Inside a widget that contains p its own HitTest takes over.
func hitOverflow(w Widget, p Point) Widget {
	if w == nil || boxChildHidden(w) {
		return nil
	}
	if w.Bounds().Contains(p) {
		return w.HitTest(p)
	}
	if _, ok := w.(PaintClipper); ok {
		return nil
	}
	if c, ok := w.(interface{ ClipsChildren() bool }); ok && c.ClipsChildren() {
		return nil
	}
	cl, ok := w.(ChildLister)
	if !ok {
		return nil
	}
	kids := ChildrenInPaintOrder(cl.ChildList())
	for i := len(kids) - 1; i >= 0; i-- {
		if hit := hitOverflow(kids[i], p); hit != nil {
			return hit
		}
	}
	return nil
}

// ScrollY returns the current scroll offset on the active main axis.
// Name preserved for back-compat with the original vertical-only
// ScrollView; in horizontal mode this returns the X offset.
func (s *ScrollView) ScrollY() float32 { return s.scrollOff }

// ScrollOffset is the orientation-neutral accessor; identical to
// ScrollY for both axes.
func (s *ScrollView) ScrollOffset() float32 { return s.scrollOff }

// ScrollTo sets the scroll offset on the active main axis, clamped to
// valid range.
//
// O(1): the content keeps its layout and only the viewport rect is
// dirtied. No Measure/Layout runs, so scrolling a 10k-row document costs
// the same as scrolling a 10-row one.
func (s *ScrollView) ScrollTo(off float32) {
	old := s.scrollOff
	s.scrollOff = off
	s.clampFrom(old)
}

// ScrollX is the CrossScroll offset (0 without CrossScroll).
func (s *ScrollView) ScrollX() float32 { return s.scrollX }

// ScrollToX sets the CrossScroll offset, clamped to the content's
// overflow — O(1) like ScrollTo.
func (s *ScrollView) ScrollToX(x float32) {
	old := s.scrollX
	s.scrollX = x
	s.clampX(old)
}

// MaxScrollX is the largest valid CrossScroll offset (0 when the content
// does not overflow to the right, or without CrossScroll).
func (s *ScrollView) MaxScrollX() float32 { return s.maxScrollX() }

func (s *ScrollView) maxScrollX() float32 {
	if !s.crossScrolls() {
		return 0
	}
	return max(0, s.overflowW-s.unzoom(s.viewportRect()).W)
}

func (s *ScrollView) clampX(old float32) {
	s.scrollX = min(max(s.scrollX, 0), s.maxScrollX())
	if s.scrollX == old {
		return
	}
	s.scrolled(s.scrollX-old, 0)
	if s.OnScroll != nil {
		s.OnScroll()
	}
}

// hasXBar reports whether the CrossScroll bar should render.
func (s *ScrollView) hasXBar() bool {
	return !s.HideBar && s.maxScrollX() > 0
}

// barX is the CrossScroll bar along the bottom edge, stopping short of the
// vertical bar's corner when both show.
func (s *ScrollView) barX() ScrollBar {
	track := HBarTrackRect(s.innerBounds())
	if s.hasBar() {
		track.W -= scrollBarThickness
	}
	return ScrollBar{
		Track:      track,
		Horizontal: true,
		Content:    s.overflowW,
		Viewport:   s.unzoom(s.viewportRect()).W,
		Offset:     s.scrollX,
	}
}

// MaxScroll is the largest valid scroll offset on the main axis (0 when
// the content fits). Exposed so hosts can compute a scroll fraction.
func (s *ScrollView) MaxScroll() float32 { return s.maxScroll() }

// scrollBy offsets the scroll position by the delta, with clamping.
func (s *ScrollView) scrollBy(d float32) {
	s.ScrollTo(s.scrollOff + d)
}

// clampScroll keeps scrollOff within [0, maxScroll].
func (s *ScrollView) ClampScroll() {
	old := s.scrollOff
	s.clampFrom(old)
}

func (s *ScrollView) clampFrom(old float32) {
	max := s.maxScroll()
	if s.scrollOff < 0 {
		s.scrollOff = 0
	}
	if s.scrollOff > max {
		s.scrollOff = max
	}
	if s.scrollOff == old {
		return
	}
	// A content shrink / resize can clamp the offset without going through
	// ScrollTo; hosts mirroring the position via OnScroll must still hear
	// about it.
	if s.Orientation == ScrollHorizontal {
		s.scrolled(s.scrollOff-old, 0)
	} else {
		s.scrolled(0, s.scrollOff-old)
	}
	if s.OnScroll != nil {
		s.OnScroll()
	}
}

// maxScroll is the largest valid scroll offset on the main axis. Zero
// if content fits.
// ensureContentSize auto-derives the content's main-axis natural size by
// measuring it, but ONLY when that axis of ContentSize is still unset
// (<=0). Callers that assign ContentSize — either via SetContent or by
// writing the field directly each Layout (VirtualList / autoScroll) — keep
// full control; this just lets a host wrap arbitrary content in a
// ScrollView without pre-measuring it.
func (s *ScrollView) ensureContentSize(rect Rect) {
	if s.Content == nil {
		return
	}
	if s.sizeAuto {
		// Only the auto (main) axis is reset. Zeroing the whole Size
		// discarded an explicit cross-axis extent such as
		// SetContent(w, Size{W: 100, H: 0}).
		if s.Orientation == ScrollHorizontal {
			if s.ContentSize.W != s.lastAuto.W {
				s.sizeAuto = false
			} else {
				s.ContentSize.W = 0
			}
		} else {
			if s.ContentSize.H != s.lastAuto.H {
				s.sizeAuto = false
			} else {
				s.ContentSize.H = 0
			}
		}
	}
	defer func() {
		if s.sizeAuto {
			s.lastAuto = s.ContentSize
		}
	}()
	slot := float32(0)
	if !s.HideBar && !s.OverlayBar {
		slot = scrollBarThickness
	}
	if s.Orientation == ScrollHorizontal {
		if s.ContentSize.W > 0 {
			return
		}
		s.sizeAuto = true
		availH := rect.H
		nat := MeasureChild(s.Content, Size{W: 0, H: availH})
		if nat.W > rect.W && slot > 0 {
			// The bar shrinks the cross axis; wrapped content measured at
			// the wider height can hide overflow. Measure again at the
			// width/height the content will actually be laid out with.
			availH = rect.H - slot
			nat = MeasureChild(s.Content, Size{W: 0, H: availH})
		}
		s.ContentSize.W = nat.W
		if !s.explicitH {
			s.ContentSize.H = availH
		}
	} else {
		if s.ContentSize.H > 0 {
			return
		}
		s.sizeAuto = true
		availW := rect.W
		nat := MeasureChild(s.Content, Size{W: availW, H: 0})
		if nat.H > rect.H && slot > 0 {
			// Same ordering bug as horizontal: contentRect reserves the
			// scrollbar slot, so measure again at that narrower width.
			availW = rect.W - slot
			nat = MeasureChild(s.Content, Size{W: availW, H: 0})
		}
		s.ContentSize.H = nat.H
		if !s.explicitW {
			s.ContentSize.W = availW
		}
	}
}

func (s *ScrollView) maxScroll() float32 {
	var over float32
	inner := s.unzoom(s.innerBounds())
	if s.Orientation == ScrollHorizontal {
		over = s.ContentSize.W - inner.W
	} else {
		over = s.ContentSize.H - inner.H
	}
	if over <= 0 {
		return 0
	}
	return over
}

// hasBar reports whether the scrollbar should render.
func (s *ScrollView) hasBar() bool {
	return !s.HideBar && s.maxScroll() > 0
}

// hasVerticalBar kept for back-compat with the original API and tests.
// Returns true only when the active orientation is vertical AND
// content overflows; horizontal-mode ScrollViews report false here.
func (s *ScrollView) hasVerticalBar() bool {
	return s.Orientation == ScrollVertical && s.hasBar()
}

// hasHorizontalBar mirrors hasVerticalBar for the new axis.
func (s *ScrollView) hasHorizontalBar() bool {
	return s.Orientation == ScrollHorizontal && s.hasBar()
}

// preciseDeltaScale undoes the 0.1 factor the platform bridge applies to
// pixel-precise (trackpad / Magic Mouse) deltas so it can express them in
// GLFW's line-ish units. Multiplying by it recovers the original points.
const preciseDeltaScale = 10

// wheelPixels converts one scroll event's main-axis delta into pixels.
//
// The two input classes need different treatment, and conflating them is what
// made trackpad scrolling feel wrong:
//
//   - A notched wheel reports ±1 per click, so it has to be multiplied by a
//     step to move a useful distance.
//   - A trackpad reports the actual finger movement, already scaled to line
//     units by the platform bridge. Multiplying THAT by the step moved the
//     content 4x the finger (0.1 × 40), which reads as overshooting rather
//     than dragging the page. Undoing the bridge's scale instead tracks the
//     fingers 1:1, like every native scroll view.
//
// A reported gesture phase is the marker for the precise class: real wheels
// report none, trackpads report Began/Changed/Ended (and Momentum for
// inertia). Platforms without phase reporting keep the notched behavior.
func (s *ScrollView) wheelPixels(delta float32, phase GesturePhase) float32 {
	if phase != GesturePhaseNone {
		return delta * preciseDeltaScale / s.zoomFactor()
	}
	return delta * s.wheelStep() / s.zoomFactor()
}

func (s *ScrollView) wheelStep() float32 {
	if s.ScrollStep > 0 {
		return s.ScrollStep
	}
	return defaultWheelStep
}

// scrollDeltaFor maps a (DeltaX, DeltaY) wheel/trackpad event onto the
// active main axis. Vertical orientation just consumes DeltaY. Horizontal
// orientation prefers DeltaX when present (trackpad two-finger
// horizontal swipe) and falls back to DeltaY so users with a vertical
// scroll wheel can still drive the toolbar scroll. We flip sign for
// the DeltaY-on-horizontal-axis case so "wheel up" moves content right
// — the convention every web-based horizontal scroller follows.
func (s *ScrollView) scrollDeltaFor(dx, dy float32) float32 {
	if s.Orientation == ScrollHorizontal {
		if dx != 0 {
			return -dx
		}
		return -dy
	}
	return -dy
}

// mainExtent returns the main-axis size of rect.
func (s *ScrollView) mainExtent(r Rect) float32 {
	if s.Orientation == ScrollHorizontal {
		return r.W
	}
	return r.H
}

func (s *ScrollView) contentMainSize() float32 {
	if s.Orientation == ScrollHorizontal {
		return s.ContentSize.W
	}
	return s.ContentSize.H
}

// barTrack returns the bar's track rectangle along the trailing edge
// of the cross axis (right edge for vertical, bottom edge for
// horizontal).
func (s *ScrollView) barTrack() Rect {
	if s.Orientation == ScrollHorizontal {
		return HBarTrackRect(s.innerBounds())
	}
	t := VBarTrackRect(s.innerBounds())
	if s.hasXBar() {
		t.H -= scrollBarThickness
	}
	return t
}

// bar is the scrollbar geometry for the current scroll state.
func (s *ScrollView) bar() ScrollBar {
	return ScrollBar{
		Track:      s.barTrack(),
		Horizontal: s.Orientation == ScrollHorizontal,
		Content:    s.contentMainSize(),
		Viewport:   s.mainExtent(s.unzoom(s.innerBounds())),
		Offset:     s.scrollOff,
	}
}

// barThumb returns the scrollbar thumb rectangle.
func (s *ScrollView) barThumb() Rect { return s.bar().Thumb() }

func (s *ScrollView) drawBar(canvas Canvas) {
	s.bar().Draw(canvas, s.barDrag.Active, s.BarColors)
}
