package widgets

import . "github.com/qizhanchan/qui"

// ScrollOrientation selects which axis the ScrollView scrolls. Default
// (zero) is ScrollVertical — back-compat with the original Y-only
// ScrollView. ScrollHorizontal pivots the geometry so the bar lives at
// the bottom and Left/Right keys + DeltaY/DeltaX wheel input scroll
// along X.
//
// 2D scrolling (both axes simultaneously) is out of scope for v1; if a
// caller needs it they can nest a horizontal ScrollView inside a
// vertical one. The two scroll states are independent that way.
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
// Callers provide ContentSize explicitly (SetContent(widget, size))
// because Container.Measure doesn't always report intrinsic size for
// every layout family; supplying it directly sidesteps that. Typical
// vertical use:
//
//	sv := widgets.NewScrollView()
//	content := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, items...)
//	sv.SetContent(content, qui.Size{W: 300, H: float32(len(items)) * 22})
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
	// Orientation selects the scroll axis. Zero value = ScrollVertical.
	Orientation ScrollOrientation
	// ScrollStep is the pixel delta per wheel notch. 0 means the default.
	ScrollStep float32

	// scrollOff is the main-axis offset (Y for vertical, X for
	// horizontal). Kept under one field so the layout / bar / drag
	// math doesn't fork by axis at every step.
	scrollOff float32

	// Scrollbar drag state.
	barDragging        bool
	barDragStartMouse  float32 // mouse position at drag start, on the main axis
	barDragStartScroll float32

	// OnScroll, when set, is invoked whenever the scroll offset changes
	// value (wheel, drag, track click, keys, or ScrollTo). Hosts use it
	// to mirror scroll position across panes.
	OnScroll func()

	focused bool
}

const (
	scrollBarThickness = 10
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
	if s.Content == widget {
		s.ContentSize = contentSize
		s.ClampScroll()
		s.InvalidateLayout()
		return
	}
	if old := s.Content; old != nil {
		s.ReleaseChildForTransfer(old)
		DetachWidgetTree(old)
	}
	s.ContentSize = contentSize
	if widget != nil {
		if AdoptWidgetTree(widget, s, s.Window()) {
			s.Content = widget
		}
	}
	s.ClampScroll()
	s.InvalidateLayout()
}

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

func (s *ScrollView) Focusable() bool { return s.Enabled() }
func (s *ScrollView) CancelInteraction() {
	if s.barDragging {
		s.barDragging = false
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

func (s *ScrollView) Layout(rect Rect) {
	s.BaseWidget.Layout(rect)
	s.ensureContentSize(rect)
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
	s.Content.Measure(Size{W: r.W, H: r.H})
	s.Content.Layout(r)
}

// contentRect is the rect the content is laid out at: the viewport
// origin, the full content extent on the main axis, and the viewport
// extent minus the scrollbar slot on the cross axis.
func (s *ScrollView) contentRect() Rect {
	b := s.Bounds()
	if s.Orientation == ScrollHorizontal {
		h := b.H
		if s.hasBar() {
			h -= scrollBarThickness
		}
		return Rect{X: b.X, Y: b.Y, W: s.ContentSize.W, H: h}
	}
	w := b.W
	if s.hasBar() {
		w -= scrollBarThickness
	}
	return Rect{X: b.X, Y: b.Y, W: w, H: s.ContentSize.H}
}

// viewportRect is the on-screen area the content is visible through:
// our bounds minus the scrollbar slot.
func (s *ScrollView) viewportRect() Rect {
	b := s.Bounds()
	if !s.hasBar() {
		return b
	}
	if s.Orientation == ScrollHorizontal {
		b.H -= scrollBarThickness
	} else {
		b.W -= scrollBarThickness
	}
	return b
}

// contentTranslation is the offset from content coordinates to our own
// coordinate space.
func (s *ScrollView) contentTranslation() (dx, dy float32) {
	if s.Orientation == ScrollHorizontal {
		return -s.scrollOff, 0
	}
	return 0, -s.scrollOff
}

// ChildInteractionTransform maps the content subtree's retained (content-
// space) geometry into our own space — the root's contract for a widget
// that transforms its descendants without moving its own box. Hit
// testing, AX bounds, agent geometry, and per-widget event coordinates
// all flow through it.
func (s *ScrollView) ChildInteractionTransform() Matrix {
	dx, dy := s.contentTranslation()
	return TranslateMatrix(dx, dy)
}

// ChildPaintTransform is the paint-time counterpart: the same translation
// Draw concatenates onto the canvas, so a descendant's Invalidate() maps
// its dirty rect to the right screen pixels.
func (s *ScrollView) ChildPaintTransform() Matrix {
	dx, dy := s.contentTranslation()
	return TranslateMatrix(dx, dy)
}

// PaintClip reports the viewport, so a descendant that is scrolled out of
// view invalidates nothing instead of dirtying whatever occupies those
// pixels now.
func (s *ScrollView) PaintClip() Rect { return s.viewportRect() }

func (s *ScrollView) Draw(canvas Canvas) {
	b := s.Bounds()

	// Viewport background. Default to the theme Surface (the page
	// background) when the caller hasn't set an opaque one, so a top-level
	// scroll view's scrollbar gutter doesn't reveal the window clear color
	// behind it. Follows a retinted theme; callers set Style().Background
	// for a custom viewport fill.
	bg := s.Style().Background
	if bg.A == 0 {
		bg = CurrentTheme().Surface
	}
	canvas.FillRect(b, bg)

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
		if !effectiveClip.IsEmpty() {
			depth := canvas.Save()
			canvas.ClipRect(effectiveClip)
			// After this, ClipBoundsLogical() reports the clip in CONTENT
			// coordinates (canvasState inverts the current matrix), so the
			// content's own dirty-region culling keeps working unchanged.
			dx, dy := s.contentTranslation()
			canvas.Translate(dx, dy)
			s.Content.Draw(canvas)
			canvas.RestoreTo(depth)
		}
	}

	// Scrollbar on top, outside the content clip.
	if s.hasBar() {
		s.drawBar(canvas)
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
				delta := s.scrollDeltaFor(e.DeltaX, e.DeltaY)
				if delta != 0 {
					s.scrollBy(s.wheelPixels(delta, e.ScrollPhase))
					return true
				}
			}
		case EventMouseDown:
			if !s.Bounds().Contains(Point{X: e.X, Y: e.Y}) {
				return false
			}
			if s.hasBar() && s.barThumb().Contains(Point{X: e.X, Y: e.Y}) {
				s.barDragging = true
				s.barDragStartMouse = s.mainCoord(e.X, e.Y)
				s.barDragStartScroll = s.scrollOff
				s.Invalidate()
				return true
			}
			if s.hasBar() && s.barTrack().Contains(Point{X: e.X, Y: e.Y}) {
				// Click on the track (above/below the thumb) jumps by
				// roughly a page. Direction depends on thumb position.
				thumb := s.barThumb()
				thumbStart := s.mainCoord(thumb.X, thumb.Y)
				clickPos := s.mainCoord(e.X, e.Y)
				pageStep := s.mainExtent(s.Bounds()) * 0.9
				if clickPos < thumbStart {
					s.scrollBy(-pageStep)
				} else {
					s.scrollBy(pageStep)
				}
				return true
			}
		case EventMouseMove:
			if s.barDragging {
				s.barDragMove(s.mainCoord(e.X, e.Y))
				return true
			}
		case EventMouseUp:
			if s.barDragging {
				s.barDragging = false
				s.Invalidate()
				return true
			}
		}
	case KeyEvent:
		if s.focused && e.Type() == EventKeyDown {
			if s.Orientation == ScrollHorizontal {
				switch e.Key {
				case KeyLeft:
					s.scrollBy(-defaultKeyStep)
					return true
				case KeyRight:
					s.scrollBy(defaultKeyStep)
					return true
				}
			} else {
				switch e.Key {
				case KeyUp:
					s.scrollBy(-defaultKeyStep)
					return true
				case KeyDown:
					s.scrollBy(defaultKeyStep)
					return true
				}
			}
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
	// Scrollbar wins over content when the pointer is on the bar.
	if s.hasBar() && (s.barTrack().Contains(p) || s.barThumb().Contains(p)) {
		return s
	}
	if s.Content != nil {
		dx, dy := s.contentTranslation()
		local := Point{X: p.X - dx, Y: p.Y - dy}
		if hit := s.Content.HitTest(local); hit != nil {
			return hit
		}
	}
	return s
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
	s.ClampScroll()
	if s.scrollOff == old {
		return
	}
	s.Invalidate()
	if s.OnScroll != nil {
		s.OnScroll()
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
	max := s.maxScroll()
	if s.scrollOff < 0 {
		s.scrollOff = 0
	}
	if s.scrollOff > max {
		s.scrollOff = max
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
	if s.Orientation == ScrollHorizontal {
		if s.ContentSize.W > 0 {
			return
		}
		nat := s.Content.Measure(Size{W: 0, H: rect.H})
		s.ContentSize.W = nat.W
		if s.ContentSize.H <= 0 {
			s.ContentSize.H = rect.H
		}
	} else {
		if s.ContentSize.H > 0 {
			return
		}
		nat := s.Content.Measure(Size{W: rect.W, H: 0})
		s.ContentSize.H = nat.H
		if s.ContentSize.W <= 0 {
			s.ContentSize.W = rect.W
		}
	}
}

func (s *ScrollView) maxScroll() float32 {
	var over float32
	if s.Orientation == ScrollHorizontal {
		over = s.ContentSize.W - s.Bounds().W
	} else {
		over = s.ContentSize.H - s.Bounds().H
	}
	if over <= 0 {
		return 0
	}
	return over
}

// hasBar reports whether the scrollbar should render.
func (s *ScrollView) hasBar() bool {
	return s.maxScroll() > 0
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
		return delta * preciseDeltaScale
	}
	return delta * s.wheelStep()
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

// mainCoord projects an event point onto the main axis.
func (s *ScrollView) mainCoord(x, y float32) float32 {
	if s.Orientation == ScrollHorizontal {
		return x
	}
	return y
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
	b := s.Bounds()
	if s.Orientation == ScrollHorizontal {
		return Rect{
			X: b.X,
			Y: b.Y + b.H - scrollBarThickness,
			W: b.W,
			H: scrollBarThickness,
		}
	}
	return Rect{
		X: b.X + b.W - scrollBarThickness,
		Y: b.Y,
		W: scrollBarThickness,
		H: b.H,
	}
}

// barThumb returns the scrollbar thumb rectangle. The thumb fills a
// fraction of the track equal to viewport/content, with a 24 px floor
// so it stays draggable even at extreme content lengths.
func (s *ScrollView) barThumb() Rect {
	track := s.barTrack()
	contentMain := s.contentMainSize()
	if contentMain <= 0 {
		return track
	}
	trackMain := s.mainExtent(track)
	thumbMain := trackMain * trackMain / contentMain
	const minThumb float32 = 24
	if thumbMain < minThumb {
		thumbMain = minThumb
	}
	if thumbMain > trackMain {
		thumbMain = trackMain
	}
	travel := trackMain - thumbMain
	progress := float32(0)
	if max := s.maxScroll(); max > 0 {
		progress = s.scrollOff / max
	}
	if s.Orientation == ScrollHorizontal {
		return Rect{
			X: track.X + travel*progress,
			Y: track.Y,
			W: thumbMain,
			H: track.H,
		}
	}
	return Rect{
		X: track.X,
		Y: track.Y + travel*progress,
		W: track.W,
		H: thumbMain,
	}
}

// barDragMove maps a mouse position during drag (on the main axis) to
// a scrollOff via the thumb's travel range — preserves the grab offset
// so the thumb doesn't snap.
func (s *ScrollView) barDragMove(mousePos float32) {
	track := s.barTrack()
	thumb := s.barThumb()
	travel := s.mainExtent(track) - s.mainExtent(thumb)
	if travel <= 0 {
		return
	}
	// How far the thumb should travel equals how far the mouse moved.
	// Convert back to scroll offset via the maxScroll/travel ratio.
	d := mousePos - s.barDragStartMouse
	scrollPerPixel := s.maxScroll() / travel
	s.ScrollTo(s.barDragStartScroll + d*scrollPerPixel)
}

func (s *ScrollView) drawBar(canvas Canvas) {
	track := s.barTrack()
	thumb := s.barThumb()
	// Derive the bar colors by mixing the theme's OnSurface toward the
	// Surface (page) color, and pass OPAQUE results — the rounded-rect
	// fill doesn't alpha-blend, so a translucent color would paint solid.
	// Mixing follows a retinted theme: a faint track + mid-gray thumb.
	th := CurrentTheme()
	trackColor := LerpColor(th.Surface, th.Text, 0.08)
	thumbT := float32(0.30)
	if s.barDragging {
		thumbT = 0.45
	}
	thumbColor := LerpColor(th.Surface, th.Text, thumbT)
	canvas.FillRoundedRect(track, 2, trackColor)
	canvas.FillRoundedRect(thumb, 2, thumbColor)
}
