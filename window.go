package qui

import (
	"errors"
	"log"
	"os"
	"sync"
	"time"
)

// paintDebugEnabled turns on QUI_DEBUG_PAINT=1 diagnostics: logs when a
// frame's repaint is promoted from scoped dirty rects to a full-window
// repaint because the layout pass moved a widget, naming the first mover.
// Use it to find what keeps a supposedly-idle window repainting.
var paintDebugEnabled = os.Getenv("QUI_DEBUG_PAINT") == "1"

// CursorShape is a semantic cursor style.
type CursorShape int

const (
	// CursorDefault is the platform default arrow cursor.
	CursorDefault CursorShape = iota
	// CursorText is the text I-beam cursor.
	CursorText
	// CursorCrosshair is a crosshair cursor.
	CursorCrosshair
	// CursorHand is a pointing hand cursor.
	CursorHand
	// CursorResizeEW is a horizontal resize cursor.
	CursorResizeEW
	// CursorResizeNS is a vertical resize cursor.
	CursorResizeNS
)

// Window is one OS window plus the widget tree it renders. The OS half
// lives behind the platform seam (see platform.go), so nothing here knows
// which windowing backend is in use.
type Window struct {
	// uiThread is an opt-in diagnostic owner check. It deliberately does not
	// synchronize UI state; PostJob remains the only cross-goroutine mutation
	// primitive. See ui_thread.go.
	uiThread uiThreadOwnership
	// plat is this window's platform backend (see platform.go). Nil for
	// test windows, which is why every method that touches it guards —
	// the whole test suite runs against a nil backend, which is what makes
	// swapping backends low-risk. platMu only protects the cross-goroutine
	// PostJob/synchronous-action paths against Destroy clearing plat; normal
	// platform access remains confined to the main thread. platformBacked is
	// immutable after construction and distinguishes a destroyed production
	// window from a headless test window.
	platMu         sync.RWMutex
	plat           platformWindow
	platformBacked bool
	root           Widget
	renderer       Renderer
	onRender       func(Canvas)
	lastSize       Size
	focused        Widget
	// overlayFocusScopes parallels overlays. Each entry remembers where
	// focus should return if that overlay owns focus when it is removed.
	overlayFocusScopes []overlayFocusScope
	// focusListeners run after keyboard focus moves between widgets
	// (AddFocusChangeListener). The html-css engine uses this to repaint
	// elements whose styling depends on an ancestor's :focus — focus can
	// move without any event reaching that ancestor.
	focusListeners []func()
	// focusVisible mirrors CSS :focus-visible — true when the current
	// focus was acquired via keyboard (Tab / Shift+Tab) or programmatic
	// SetFocus, false when via mouse click. Widgets implementing
	// focusVisibleAware get told via SetFocusVisible(bool) and use this
	// to decide whether to paint a focus ring / state-layer halo.
	focusVisible  bool
	dragCandidate Widget
	dragging      bool
	dragStart     Point
	// mouseCaptured remembers the widget that received MouseDown so that
	// the matching MouseUp reaches it even if the cursor has since moved
	// off. Standard "mouse capture" semantics: set on down, cleared on up.
	mouseCaptured Widget
	// gestureTarget is mouse capture's equivalent for multi-touch: the
	// widget that received a gesture's Began keeps every Changed/Ended
	// even if the fingers drift outside its bounds. See gesture.go.
	gestureTarget Widget
	// gestureScale / gestureRotation accumulate the platform's per-event
	// increments into the cumulative values GestureEvent reports.
	gestureScale    float32
	gestureRotation float32
	// zoom is the viewport zoom factor (1 == 100%); zoomRestore remembers
	// the last non-100% level for smart-magnify's toggle; zoomEnabled gates
	// the window-level zoom shortcuts and gesture fallback. See zoom.go.
	zoom        float32
	zoomRestore float32
	zoomEnabled bool
	// windowSize is the OS window's logical size. lastSize is the CONTENT
	// VIEWPORT (windowSize / zoom) — the space the widget tree lives in —
	// so the two differ only while zoomed. Keeping the viewport in lastSize
	// is what lets layout, dirty-region clipping and every widget-side
	// Size() call stay correct without knowing zoom exists.
	windowSize Size
	// onDrop is the user's OS file-drop callback (SetOnFileDrop). Held
	// here rather than in the backend so the handler can invoke it and
	// then force the repaint a drop almost always warrants.
	onDrop func(paths []string, x, y float32)
	// lastMods is the modifier state from the most recent key or
	// mouse-button event. Cursor motion doesn't report modifiers, so
	// moves borrow this rather than polling per pixel.
	lastMods Modifiers
	// textSel coordinates browser-style cross-widget text selection — a
	// drag that starts in one TextSelectable and runs across siblings.
	// See text_selection.go.
	textSel textSelectionDrag
	// selClicks counts consecutive presses for the selection controller's
	// multi-click granularity (double = word, triple = line).
	selClicks selectionClicks
	// hoverPath is the root→leaf chain of widgets currently under the
	// cursor. Updated on every MouseMove; diff against a new hit path
	// drives Enter/Leave synthesis.
	hoverPath []Widget
	// overlays is a stack of widget trees drawn above the main root,
	// used for Dialog / Popup / Tooltip / ContextMenu. Last entry is
	// topmost. Each overlay is a self-contained subtree with its own
	// Parent() chain terminating at nil (no parent link into the main
	// tree). HitTest, focus collection, and Tick walk the overlays in
	// addition to the main root; the topmost overlay gets first chance
	// at events within its bounds.
	overlays []Widget
	// overlayResizePending is set by resizeTo when the logical window
	// size changed, and consumed after the frame's layout pass to notify
	// OverlayResizer overlays (see notifyOverlaysResize). Deferring past
	// layout ensures anchored overlays read their trigger's new bounds.
	overlayResizePending bool
	// Tooltip state: widget → tooltip text registration. The topmost
	// hovered widget with a registered tooltip drives the displayed
	// popup; a change in that widget replaces the popup. Kept on
	// Window so any widget can be tooltipped without threading a
	// handle through its constructor.
	tooltips      map[Widget]string
	tooltipTarget Widget
	tooltipView   *tooltipView
	// tooltipAnchor is the rect the shown tooltip was placed against. For
	// an ordinary widget it is its Bounds(); for a TooltipAtProvider it is
	// the hovered ITEM's rect, which is what lets the bubble follow the
	// pointer from one canvas-drawn item to the next inside a single
	// widget (the target alone never changes there).
	tooltipAnchor Rect
	// tooltipCloseAt is the deadline for a grace-period close. When the
	// cursor leaves the tooltipped widget the tooltip is NOT hidden
	// immediately — it lingers until this deadline so the user can slide
	// the pointer into the tooltip (to select / copy its text). Moving
	// onto the tooltip clears the deadline; the deadline firing (checked
	// in Step via tickTooltip) closes it. Zero = no pending close.
	tooltipCloseAt time.Time
	// dirtyRegion is the union of rectangles that need to be re-rasterized
	// on the next Step. An empty Rect means "nothing to redraw" and
	// causes Step to skip the entire paint pass. Invalidate() fills it
	// to the full window; InvalidateRect unions a subrect; Tick results
	// and events contribute partial regions.
	dirtyRegion Rect
	// inLayoutPass + boundsChanged implement repaint scoping for layout
	// changes: Step sets inLayoutPass around the frame's Measure+Layout;
	// any widget whose bounds change during the pass sets boundsChanged
	// (via BaseWidget.Layout → noteBoundsChange), which promotes the
	// frame to a full-window repaint. A relayout that moves NOTHING (a
	// text tick measuring to the same size) leaves only the explicitly
	// invalidated rects dirty.
	inLayoutPass  bool
	boundsChanged bool
	// animators run per-frame alongside Tickable widgets but don't have
	// to live in the tree. Tween / Spring / Timeline register here.
	// Done animators are pruned in-place during Step.
	animators []Animator
	// inStep guards against synchronous redraws requested from platform
	// event callbacks while a frame is already being produced. Resizing on
	// macOS re-enters Step from inside a callback, so this is load-bearing.
	inStep bool
	// savedWindowed remembers the windowed position+size captured when
	// the window entered fullscreen, so SetFullscreen(nil) can restore it.
	// Zero W/H means "not currently fullscreen".
	savedWindowed Rect
	// titlebarStyle is the last style requested via SetTitlebarStyle. Held
	// even when the platform could not apply it, so TitlebarInsets and app
	// layout can be written against the request. See window_titlebar.go.
	titlebarStyle TitlebarStyle
	// kind is the OS-level role this window was created with. Fixed at
	// creation — a normal window cannot become non-activating, and the
	// framebuffer's opacity is chosen when the GPU context is made.
	// See window_overlay.go.
	kind WindowKind
	// cursorShape is the last cursor style requested by widgets.
	cursorShape CursorShape
	// accels is an optional registry of Cmd-S / Ctrl-Z style shortcuts
	// consulted AFTER normal widget dispatch. See AcceleratorRegistry.
	accels *AcceleratorRegistry
	// Debug overlay state is opt-in. Once configured, the debug shortcut
	// can toggle the overlay without rebuilding application UI.
	debugOverlayConfigured bool
	debugOverlayEnabled    bool
	debugOverlayOptions    DebugOverlayOptions
	// unsubscribeTheme removes this window's callback from the global theme
	// subscriber list when the native window is destroyed.
	unsubscribeTheme func()
	// unsubscribeLocale is the same for the global locale.
	unsubscribeLocale func()
	// locale overrides the process-wide DefaultLocale for this window
	// only. Zero means "follow the global locale" — the common case; a
	// per-window value exists so a multi-window app can show, say, a
	// translator side-by-side view without a second process.
	locale Locale
	// onClose is a slice of callbacks that run when ShouldClose becomes
	// true, *before* the GLFW window is destroyed. Used by long-lived
	// resources (CEF browsers, media decoders, file watchers, …) that
	// need to be torn down on the main goroutine while the message loop
	// is still pumpable. Handlers fire FIFO; exceptions / panics are
	// not caught — handlers should be defensive on their own. Cleared
	// after firing so re-entry doesn't double-invoke.
	onClose []func()
	// onFSChange callbacks fire when the window's fullscreen state changes —
	// including changes the app did not make (the traffic-light button,
	// Ctrl+Cmd+F). wasFullscreen is the last observed state; the check is a
	// per-Step poll that only runs while someone is listening.
	onFSChange    []func(bool)
	wasFullscreen bool
	// jobs is the cross-goroutine queue drained at the top of each Step.
	// It is embedded by value so even a zero-value Window can accept the
	// first PostJob concurrently without racing a lazy pointer install.
	jobs windowJobQueue
	// Event recording state. recorders is a fan-out list of
	// listener functions; publishEventRecord notifies each one
	// per dispatched event.
	recordersMu    sync.Mutex
	recorders      []recorderEntry
	nextRecorderID int64
}

// SetAcceleratorRegistry installs r as the window's accelerator
// registry. Pass nil to disable. Registrations are consulted by
// dispatch after the bubble phase completes and only when no widget
// consumed the key event.
func (w *Window) SetAcceleratorRegistry(r *AcceleratorRegistry) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetAcceleratorRegistry")
	w.accels = r
}

// AcceleratorShortcuts returns the window-level accelerators as registered.
// Part of the introspection surface: these are commands with no widget, so
// this is the only way an agent can see them.
func (w *Window) AcceleratorShortcuts() []string {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.AcceleratorShortcuts")
	return w.accels.Shortcuts()
}

// AcceleratorRegistry returns the currently installed registry, or nil.
func (w *Window) AcceleratorRegistry() *AcceleratorRegistry {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.AcceleratorRegistry")
	return w.accels
}

// NewWindow creates a new Window with a default no-op renderer.
func NewWindow(title string, width, height int) (*Window, error) {
	return NewSharedWindow(title, width, height, nil)
}

// NewSharedWindow creates a Window whose OpenGL context shares
// resources (textures, shaders, VBOs) with the given window. Useful
// when rendering the same Scene in a detached inspector viewport, or
// when a helper window wants to blit a texture produced by the main
// window. Passing nil is equivalent to NewWindow.
//
// Both windows must still be driven from the main goroutine, and the
// active context switches per Step (see Window.Step).
func NewSharedWindow(title string, width, height int, share *Window) (*Window, error) {
	assertProcessUIThread("NewSharedWindow")
	if share != nil {
		share.assertUIThread("NewSharedWindow")
	}
	plat, err := activePlatform()
	if err != nil {
		return nil, err
	}
	cfg := platformWindowConfig{Title: title, Width: width, Height: height}
	if share != nil {
		cfg.Share = share.plat
	}
	return newWindowFromConfig(plat, cfg)
}

// newWindowFromConfig is the one place a *Window is built on top of a
// platform window. NewSharedWindow and NewOverlayPanel differ only in the
// config they hand in, so everything downstream — handler wiring, theme
// subscription, first-frame invalidation — stays in a single path.
func newWindowFromConfig(plat platformApp, cfg platformWindowConfig) (*Window, error) {
	pw, err := plat.newWindow(cfg)
	if err != nil {
		return nil, err
	}
	pw.makeCurrent()

	width, height := cfg.Width, cfg.Height
	w := &Window{
		plat:           pw,
		platformBacked: true,
		renderer:       NoopRenderer{},
		kind:           cfg.Kind,
		lastSize:       Size{W: float32(width), H: float32(height)},
		windowSize:     Size{W: float32(width), H: float32(height)},
		zoom:           1,
		cursorShape:    CursorDefault,
	}
	w.initUIThreadOwnership()
	// Force a full paint on the first frame.
	w.dirtyRegion = Rect{W: float32(width), H: float32(height)}
	// One handler for every input and lifecycle event — see
	// window_handler.go.
	pw.setHandler(w)

	// Wire the OS clipboard so text widgets can implement Cmd/Ctrl+C/V/X.
	// Last-created window wins if the app opens several — acceptable
	// because the OS clipboard is process-wide and the window is largely
	// a formality in modern implementations.
	//
	// Overlay panels are excluded: one is typically created AFTER the real
	// window and lives for the whole process, so letting it win would hand
	// the app's clipboard to a window that has no text input at all.
	if cfg.Kind != WindowOverlayPanel {
		SetClipboardProvider(newOSClipboard(pw))
	}

	// Re-paint + re-layout on theme switch. Widgets read CurrentTheme()
	// fresh at Draw time, so no per-widget bookkeeping is needed —
	// we just need to flush the current cached frame.
	w.unsubscribeTheme = SubscribeTheme(func() {
		w.Invalidate()
		w.InvalidateLayout()
	})

	// Same for language switches. Widgets resolve message keys inside
	// Draw/Measure, so a re-layout is all that is needed — but it MUST
	// be a re-layout and not a bare repaint: translated strings have
	// different widths, so every text box may resize.
	w.unsubscribeLocale = SubscribeLocale(func() {
		w.Invalidate()
		w.InvalidateLayout()
	})

	// Phase 5.2 IME: install platform-specific backend (no-op stub
	// today; macOS Cgo bridge lands here later).
	w.installIMEBackend()

	return w, nil
}

// cursorPos returns the cursor position in logical window coordinates.
// Gesture anchoring needs it because indirect touch devices report finger
// positions relative to the trackpad, not the window.
func (w *Window) cursorPos() (float32, float32) {
	if w == nil || w.plat == nil {
		return 0, 0
	}
	x, y := w.plat.cursorPos()
	// The platform reports window-logical points; callers hit-test in
	// viewport space.
	return w.toViewport(x, y)
}

// Invalidate marks the entire window as needing a redraw on the next frame.
// Use this when the change area is unknown or spans the whole window
// (theme change, global resize, uncertain event side-effects).
func (w *Window) Invalidate() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Invalidate")
	w.dirtyRegion = Rect{W: w.lastSize.W, H: w.lastSize.H}
}

// resizeTo records a logical window-size change and wakes both layout
// and paint. Step calls this defensively because GLFW may deliver size
// and framebuffer callbacks in platform-specific orders during live
// window drags.
func (w *Window) resizeTo(size Size) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.resizeTo")
	if w.lastSize.W == size.W && w.lastSize.H == size.H {
		return
	}
	w.lastSize = size
	w.Invalidate()
	w.InvalidateLayout()
	// Defer the overlay notification until AFTER this frame's layout pass:
	// an overlay anchored to a widget in the main tree (a dropdown pinned
	// to a button) needs the trigger's NEW bounds to re-place correctly,
	// and those only refresh once root.Layout runs later in Step.
	w.overlayResizePending = true
}

// OverlayResizer is the optional interface an overlay implements to react
// to a logical window-size change while it sits on the overlay stack.
// Overlays are otherwise pre-positioned by their caller at show time and
// the frame loop never re-lays them out (their geometry is caller-driven),
// so without this hook a resize leaves them anchored to a stale absolute
// position — a bottom-anchored dropdown floats away from its trigger, a
// centered dialog drifts off-center. Anchored menus re-place against their
// (moved) trigger; modal dialogs re-center. Overlays that don't implement
// it keep the old behavior.
type OverlayResizer interface {
	OnWindowResize(newSize Size)
}

// notifyOverlaysResize fans a logical size change out to overlays that
// implement OverlayResizer. It snapshots the stack first because a handler
// may Close()/RemoveOverlay itself (a dismissing popup) and mutate the
// live slice mid-iteration.
func (w *Window) notifyOverlaysResize(size Size) {
	w.assertUIThread("Window.notifyOverlaysResize")
	if len(w.overlays) == 0 {
		return
	}
	snapshot := append([]Widget(nil), w.overlays...)
	for _, ov := range snapshot {
		if r, ok := ov.(OverlayResizer); ok {
			r.OnWindowResize(size)
		}
	}
}

// InvalidateRect marks a sub-region as dirty. The region is unioned into
// the current dirty region so multiple calls within a single frame
// accumulate. Coordinates are in logical (post-layout) window space.
func (w *Window) InvalidateRect(r Rect) {
	if w == nil || r.IsEmpty() {
		return
	}
	w.assertUIThread("Window.InvalidateRect")
	w.dirtyRegion = w.dirtyRegion.Union(r)
}

// noteBoundsChange records that a widget's layout bounds changed (see
// BaseWidget.Layout). During the frame's layout pass this flags the
// frame for a full repaint; outside the pass (caller-positioned
// overlays, programmatic Layout calls, tests) the widget's old and new
// paint extents are invalidated directly so the move is visible without
// a layout pass to promote it.
func (w *Window) noteBoundsChange(oldPaint, newPaint Rect) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.noteBoundsChange")
	if w.inLayoutPass {
		// Sub-pixel jitter (float text advances make flex hand a widget
		// 581.95px one frame and 582px the next) must not promote the
		// whole frame: both extents cover the same pixels, so repainting
		// them in place is exact. Only a real move (≥ boundsEpsilon on
		// some edge) needs the full repaint.
		if rectsNearlyEqual(oldPaint, newPaint, boundsEpsilon) {
			w.InvalidateRect(oldPaint)
			w.InvalidateRect(newPaint)
			return
		}
		if paintDebugEnabled && !w.boundsChanged {
			log.Printf("qui: paint promoted to full repaint; first moved widget: %+v -> %+v", oldPaint, newPaint)
		}
		w.boundsChanged = true
		return
	}
	w.InvalidateRect(oldPaint)
	w.InvalidateRect(newPaint)
}

// boundsEpsilon is the logical-pixel threshold below which a bounds
// change counts as sub-pixel jitter rather than a move. Real layout
// changes shift widgets by whole pixels; float text measurement noise
// is in the hundredths.
const boundsEpsilon = 0.25

// rectsNearlyEqual reports whether every edge of a and b is within eps.
func rectsNearlyEqual(a, b Rect, eps float32) bool {
	return absF(a.X-b.X) <= eps && absF(a.Y-b.Y) <= eps &&
		absF(a.X+a.W-b.X-b.W) <= eps && absF(a.Y+a.H-b.Y-b.H) <= eps
}

// RegisterAnimator adds an Animator to the window. The animator's
// Tick is invoked once per frame; when Tick reports done=true the
// animator is removed. Register forces an immediate paint so the
// first Tick observes a non-zero elapsed on frame 2.
func (w *Window) RegisterAnimator(a Animator) {
	if w == nil || a == nil {
		return
	}
	w.assertUIThread("Window.RegisterAnimator")
	w.animators = append(w.animators, a)
	w.Invalidate()
}

// tickAnimators ticks every currently-registered animator once, prunes
// any that reported done=true, and carries forward animators that were
// registered DURING a Tick (e.g. reactive's RequestRender path —
// state setter → RegisterAnimator — that commonly fires from inside
// another animator's Tick when async results bridge back into reactive
// state). Without the carry-forward step the newcomer would be
// silently dropped by the truncation and its first Tick would never
// run, leaving callers waiting for a render that never arrives.
func (w *Window) tickAnimators(now time.Time) {
	if len(w.animators) == 0 {
		return
	}
	n := len(w.animators)
	survivors := 0
	for i := 0; i < n; i++ {
		a := w.animators[i]
		dirty, done := a.Tick(now)
		w.dirtyRegion = w.dirtyRegion.Union(dirty)
		if !done {
			w.animators[survivors] = a
			survivors++
		}
	}
	// Pull newcomers from [n, end) down to [survivors, survivors+added).
	// When the survivor block already fills the original window the
	// moves are no-ops; correctness still holds because each newcomer
	// is read before any later write touches its slot.
	added := len(w.animators) - n
	for j := 0; j < added; j++ {
		w.animators[survivors+j] = w.animators[n+j]
	}
	end := survivors + added
	// Zero the tail so removed animators can be GC'd.
	for i := end; i < len(w.animators); i++ {
		w.animators[i] = nil
	}
	w.animators = w.animators[:end]
}

// UnregisterAnimator removes an animator before it signals done.
// Safe to call on an animator that's already been pruned.
func (w *Window) UnregisterAnimator(a Animator) {
	if w == nil || a == nil {
		return
	}
	w.assertUIThread("Window.UnregisterAnimator")
	for i, existing := range w.animators {
		if existing == a {
			w.animators = append(w.animators[:i], w.animators[i+1:]...)
			return
		}
	}
}

// Size returns the logical size of the CONTENT VIEWPORT — the coordinate
// space the widget tree is laid out and hit-tested in. Overlays (Popup,
// Dialog) use this to size themselves before ShowAt positions them.
//
// Under viewport zoom this is the window size divided by the zoom factor
// (see zoom.go), which is what content-space callers want. It equals the
// OS window's logical size at 100% zoom; use WindowSize when you
// specifically mean the window rather than its content.
func (w *Window) Size() Size {
	if w == nil {
		return Size{}
	}
	w.assertUIThread("Window.Size")
	return w.lastSize
}

// DevicePixelRatio returns the ratio of the framebuffer (physical) size
// to the logical window size — i.e. 1.0 on a standard display and 2.0
// on a macOS Retina display. Returns 1.0 when the window has no GLFW
// handle yet (test windows, detached widgets querying before mount).
func (w *Window) DevicePixelRatio() float32 {
	if w == nil {
		return 1
	}
	w.assertUIThread("Window.DevicePixelRatio")
	if w.plat == nil {
		return 1
	}
	width, height := w.plat.size()
	fbWidth, fbHeight := w.plat.framebufferSize()
	if width <= 0 || height <= 0 || fbWidth <= 0 || fbHeight <= 0 {
		return 1
	}
	// Use whichever axis is larger to defend against very narrow
	// windows on multi-monitor setups where one axis can briefly
	// report 0 during a move between displays. Both axes have the
	// same DPR in practice on every platform we target.
	rx := float32(fbWidth) / float32(width)
	ry := float32(fbHeight) / float32(height)
	if rx >= ry {
		return rx
	}
	return ry
}

// SetCursor updates the native cursor style for this window. Calling it
// repeatedly with the same shape is a no-op.
func (w *Window) SetCursor(shape CursorShape) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetCursor")
	if !isSupportedCursorShape(shape) {
		shape = CursorDefault
	}
	if shape == w.cursorShape {
		return
	}
	w.cursorShape = shape
	if w.plat == nil {
		return
	}
	w.plat.setCursor(shape)
}

// Cursor returns the currently requested cursor style.
func (w *Window) Cursor() CursorShape {
	if w == nil {
		return CursorDefault
	}
	w.assertUIThread("Window.Cursor")
	return w.cursorShape
}

func isSupportedCursorShape(shape CursorShape) bool {
	switch shape {
	case CursorDefault, CursorText, CursorCrosshair, CursorHand, CursorResizeEW, CursorResizeNS:
		return true
	default:
		return false
	}
}

// SetMinSize sets the minimum logical client size for the native
// window. Width/height values below zero are clamped to zero.
//
// Typical use: protect complex forms and demos from collapsing into an
// unusable layout when users resize the window too small.
func (w *Window) SetMinSize(width, height int) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetMinSize")
	if w.plat == nil {
		return
	}
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	w.plat.setSizeLimits(width, height, 0, 0)
}

// SetSize resizes the window's content area to width x height logical
// points, keeping its TOP-left corner where it is.
//
// The top-left anchor matters for an overlay panel sized to its content:
// the panel is placed under a text caret, and growing a row taller must
// push the bottom edge down rather than lift the panel off the caret.
//
// Sizing a panel to its content is not cosmetic. A window receives mouse
// events across its whole frame, transparent pixels included, so an
// oversized panel silently swallows clicks meant for the app underneath.
func (w *Window) SetSize(width, height int) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetSize")
	if w.plat == nil {
		return
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	w.plat.setSize(width, height)
}

// SetTitle updates the native window title bar. Multi-window apps use
// this to reflect per-window state (a terminal's cwd, a document name)
// that changes after the window is created.
func (w *Window) SetTitle(title string) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetTitle")
	if w.plat == nil {
		return
	}
	w.plat.setTitle(title)
}

// Position returns the window's top-left corner in screen coordinates
// (virtual-desktop logical pixels). Multi-window apps read this to
// persist and restore a window's placement, or to cascade a new window
// relative to an existing one.
//
// Returns the zero Point when the platform has no notion of global window
// position — which is the case on Wayland, where a client genuinely
// cannot know where it is on screen. Use PositionOK when the difference
// between "at the origin" and "unknowable" matters, and
// SupportsAbsolutePosition to decide whether to offer the feature at all.
func (w *Window) Position() Point {
	p, _ := w.PositionOK()
	return p
}

// PositionOK is Position with the platform's answer to "can I even know
// this". ok is false where absolute window coordinates don't exist; the
// returned Point is then meaningless, and the caller should fall back to
// letting the window manager place the window rather than computing
// geometry from it.
func (w *Window) PositionOK() (Point, bool) {
	if w == nil {
		return Point{}, false
	}
	w.assertUIThread("Window.PositionOK")
	if w.plat == nil {
		return Point{}, false
	}
	x, y, ok := w.plat.pos()
	if !ok {
		return Point{}, false
	}
	return Point{X: float32(x), Y: float32(y)}, true
}

// SupportsAbsolutePosition reports whether this window can be queried and
// moved in global screen coordinates. False on platforms that hide it
// (Wayland). Check it before building a feature on saved window geometry —
// restoring a layout, tiling, cascading — so the UI can omit the
// affordance instead of silently doing nothing.
func (w *Window) SupportsAbsolutePosition() bool {
	if w == nil {
		return false
	}
	w.assertUIThread("Window.SupportsAbsolutePosition")
	if w.plat == nil {
		return false
	}
	return w.plat.caps().AbsolutePosition
}

// SetPosition moves the window's top-left corner to the given screen
// coordinates. Pair with Position to cascade or tile windows.
func (w *Window) SetPosition(x, y int) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPosition")
	if w.plat == nil {
		return
	}
	w.plat.setPos(x, y)
}

// Raise brings the window above its siblings and gives it input focus.
// Multi-window apps call this to surface an already-open window instead
// of creating a duplicate (e.g. "reveal the settings window"). Named
// Raise rather than Focus because Window.Focus(target) already means
// "move keyboard focus to a widget" in the introspection API.
func (w *Window) Raise() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Raise")
	if w.plat == nil {
		return
	}
	w.plat.focus()
}

// Focused returns the widget that currently has keyboard focus, or nil.
func (w *Window) Focused() Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Focused")
	return w.focused
}

// PushOverlay adds a widget on top of the overlay stack. The caller is
// responsible for pre-laying out the widget (call widget.Layout(rect)
// before pushing) so it knows where to render. The overlay's bounds
// are automatically invalidated so the next Step paints it in.
func (w *Window) PushOverlay(widget Widget) {
	if w == nil || widget == nil {
		return
	}
	w.assertUIThread("Window.PushOverlay")
	for i, overlay := range w.overlays {
		if overlay != widget {
			continue
		}
		if i == len(w.overlays)-1 {
			return
		}
		scope := w.overlayFocusScopes[i]
		w.overlays = append(w.overlays[:i], w.overlays[i+1:]...)
		w.overlayFocusScopes = append(w.overlayFocusScopes[:i], w.overlayFocusScopes[i+1:]...)
		w.overlays = append(w.overlays, widget)
		w.overlayFocusScopes = append(w.overlayFocusScopes, scope)
		w.InvalidateRect(PaintBoundsInWindow(widget))
		return
	}
	scope := w.makeOverlayFocusScope(widget)
	if !AdoptWidgetTree(widget, nil, w) {
		return
	}
	w.overlays = append(w.overlays, widget)
	w.overlayFocusScopes = append(w.overlayFocusScopes, scope)
	w.InvalidateRect(PaintBoundsInWindow(widget))
}

// PopOverlay removes and returns the topmost overlay. Returns nil if
// the stack is empty. The removed overlay's bounds are invalidated so
// the area underneath gets repainted.
func (w *Window) PopOverlay() Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.PopOverlay")
	if len(w.overlays) == 0 {
		return nil
	}
	idx := len(w.overlays) - 1
	return w.removeOverlayAt(idx)
}

// RemoveOverlay removes the first matching overlay from the stack
// (scans top-down so topmost match wins). Returns true if removed.
// Use this when an overlay's lifecycle is driven by something other
// than Push/Pop discipline — e.g., a Tooltip that clears on timeout.
func (w *Window) RemoveOverlay(widget Widget) bool {
	if w == nil || widget == nil {
		return false
	}
	w.assertUIThread("Window.RemoveOverlay")
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if w.overlays[i] == widget {
			w.removeOverlayAt(i)
			return true
		}
	}
	return false
}

func (w *Window) removeOverlayAt(index int) Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.removeOverlayAt")
	if index < 0 || index >= len(w.overlays) {
		return nil
	}
	widget := w.overlays[index]
	focusedInside := widgetIsDescendant(w.focused, widget)
	scope := w.takeOverlayFocusScope(index, widget)
	w.overlays = append(w.overlays[:index], w.overlays[index+1:]...)
	w.InvalidateRect(PaintBoundsInWindow(widget))

	// Transfer focus while the removed subtree is still wired, so the old
	// focused widget receives SetFocused(false) and its transformed paint
	// bounds can be invalidated in the same focus transition.
	if focusedInside || (w.focused == nil && scope.claimed) {
		restore := scope.restore
		if restore != nil && !w.validFocusTarget(restore) {
			restore = nil
		}
		w.focusVisible = scope.focusVisible
		w.SetFocus(restore)
	}
	DetachWidgetTree(widget)
	return widget
}

func (w *Window) releaseTopLevelForTransfer(widget Widget) bool {
	if w == nil || widget == nil {
		return false
	}
	w.assertUIThread("Window.releaseTopLevelForTransfer")
	if w.root == widget {
		w.root = nil
		w.Invalidate()
		return true
	}
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if w.overlays[i] != widget {
			continue
		}
		w.takeOverlayFocusScope(i, widget)
		w.overlays = append(w.overlays[:i], w.overlays[i+1:]...)
		w.InvalidateRect(PaintBoundsInWindow(widget))
		return true
	}
	return false
}

// AttachTooltip registers tooltip text for a widget. When the widget
// (or any descendant) becomes the topmost hovered node, a non-modal
// tooltip popup appears near the cursor. Call DetachTooltip to remove.
// Passing an empty string removes the registration.
func (w *Window) AttachTooltip(widget Widget, text string) {
	if w == nil || widget == nil {
		return
	}
	w.assertUIThread("Window.AttachTooltip")
	if text == "" {
		w.DetachTooltip(widget)
		return
	}
	if w.tooltips == nil {
		w.tooltips = map[Widget]string{}
	}
	w.tooltips[widget] = text
}

// DetachTooltip removes a previously attached tooltip. Also hides any
// currently-visible tooltip targeting the given widget.
func (w *Window) DetachTooltip(widget Widget) {
	if w == nil || widget == nil {
		return
	}
	w.assertUIThread("Window.DetachTooltip")
	delete(w.tooltips, widget)
	if w.tooltipTarget == widget {
		w.closeTooltip()
	}
}

// tooltipGracePeriod is how long a tooltip lingers after the cursor
// leaves its widget, giving the user time to slide the pointer onto the
// tooltip to select / copy its content before it disappears.
const tooltipGracePeriod = 250 * time.Millisecond

// updateTooltipFromHover finds the topmost tooltipped widget in the
// current hoverPath and shows / replaces / schedules-close of the tooltip.
// Called from syncHoverPath after the hoverPath has been updated.
//
// Close is NOT immediate: when the cursor leaves the widget the tooltip
// enters a grace period (tickTooltip closes it when the deadline fires).
// Sliding the pointer onto the tooltip itself cancels the close so the
// text stays reachable for selection / copy.
func (w *Window) updateTooltipFromHover(cursorX, cursorY float32) {
	var target Widget
	var text string
	var anchor Rect
	cursor := Point{X: cursorX, Y: cursorY}
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		hw := w.hoverPath[i]
		if hw == w.tooltipView {
			continue // the tooltip overlay itself is not a tooltip trigger
		}
		// Per-point text wins over both whole-widget sources: a widget
		// that paints its own items knows which item the cursor is on,
		// and that answer is strictly more specific than either.
		//
		// The provider works in its own coordinate space (it compares the
		// point against its own item geometry), so the cursor goes in
		// localized and the anchor it hands back comes out mapped to
		// window coordinates — the tooltip overlay is positioned there.
		if tp, ok := hw.(TooltipAtProvider); ok {
			if t, rect, ok := tp.TooltipAt(WindowPointToLocal(hw, cursor)); ok && t != "" {
				target, text, anchor = hw, t, InteractionRectFor(hw, rect)
				break
			}
		}
		// Explicit AttachTooltip registration wins; otherwise fall back
		// to the widget's own TooltipProvider text (SetTooltip).
		if t, ok := w.tooltips[hw]; ok {
			target, text, anchor = hw, t, InteractionBoundsOf(hw)
			break
		}
		if tp, ok := hw.(TooltipProvider); ok {
			if t := tp.TooltipText(); t != "" {
				target, text, anchor = hw, t, InteractionBoundsOf(hw)
				break
			}
		}
	}

	if target != nil {
		// Over a tooltipped widget: cancel any pending close, and (re)show
		// if the anchor moved — a different widget, or a different item
		// inside the same TooltipAtProvider. Anchor to that rect, NOT the
		// cursor, so the tooltip lands in the same place regardless of
		// where the pointer entered.
		w.tooltipCloseAt = time.Time{}
		if target != w.tooltipTarget || anchor != w.tooltipAnchor {
			w.tooltipTarget = target
			w.tooltipAnchor = anchor
			w.closeTooltip()
			w.showTooltip(text, anchor)
		}
		return
	}

	// Not over any tooltipped widget.
	if w.tooltipView == nil {
		w.tooltipTarget, w.tooltipAnchor = nil, Rect{}
		return
	}
	// Keep the tooltip alive (cancel close) while the pointer is over it
	// or an active selection drag is in progress, so its text can be
	// selected / copied without the tooltip vanishing mid-gesture.
	if w.tooltipView.selecting ||
		w.tooltipView.Bounds().Contains(Point{X: cursorX, Y: cursorY}) {
		w.tooltipCloseAt = time.Time{}
		return
	}
	// Left both the widget and the tooltip — arm the grace-period close.
	if w.tooltipCloseAt.IsZero() {
		w.tooltipCloseAt = time.Now().Add(tooltipGracePeriod)
	}
}

// tickTooltip closes a grace-pending tooltip once its deadline fires.
// Called from Step (outside the overlay tick loop so removing the overlay
// doesn't mutate the slice being ranged). now is the frame time.
func (w *Window) tickTooltip(now time.Time) {
	if w.tooltipView == nil || w.tooltipCloseAt.IsZero() {
		return
	}
	// Don't yank the tooltip out from under an active selection drag.
	if w.tooltipView.selecting {
		return
	}
	if now.After(w.tooltipCloseAt) {
		w.tooltipTarget, w.tooltipAnchor = nil, Rect{}
		w.closeTooltip()
	}
}

// showTooltip pops a tooltip anchored to the widget rect `anchor`. The
// tooltip is centered horizontally under the widget with a small gap;
// position auto-adapts near the window edges — it clamps horizontally so
// it never runs off the left/right, and flips above the widget when it
// would overflow the bottom. Anchoring to the widget (not the cursor)
// keeps the tooltip stable while the pointer moves within the widget.
func (w *Window) showTooltip(text string, anchor Rect) {
	// Tooltips must NOT steal clicks — tooltipView.HitTest returns nil
	// so events fall through to the widget underneath. Purely visual.
	tv := newTooltipView(text)

	const gap = 6    // vertical gap between the widget and the tooltip
	const margin = 4 // keep a small gap from the window edges
	winW, winH := w.lastSize.W, w.lastSize.H

	// Cap the tooltip width so long text soft-wraps instead of running off
	// the window. Prefer a comfortable reading width, but never exceed what
	// fits inside the window margins. wrapWidth is content-only, so subtract
	// the horizontal padding.
	_, padding, _ := tv.scaled()
	const preferredW = 360 // logical px — comfortable multi-line reading width
	maxContentW := float32(preferredW)
	if fit := winW - 2*margin - padding.Horizontal(); fit < maxContentW {
		maxContentW = fit
	}
	if maxContentW < 1 {
		maxContentW = 1
	}
	tv.wrapWidth = maxContentW

	size := tv.Measure(w.lastSize)

	// Preferred anchor: centered horizontally under the widget.
	px := anchor.X + (anchor.W-size.W)/2
	py := anchor.Y + anchor.H + gap

	// Horizontal: clamp within the window so it never runs off either edge.
	if px+size.W > winW-margin {
		px = winW - margin - size.W
	}
	if px < margin {
		px = margin
	}

	// Vertical: if the tooltip would overflow the bottom, flip it above the
	// widget; if it fits neither fully below nor above, clamp to the bottom.
	if py+size.H > winH-margin {
		if above := anchor.Y - gap - size.H; above >= margin {
			py = above
		} else {
			py = winH - margin - size.H
			if py < margin {
				py = margin
			}
		}
	}

	tv.Layout(Rect{X: px, Y: py, W: size.W, H: size.H})
	w.PushOverlay(tv)
	w.tooltipView = tv
}

func (w *Window) closeTooltip() {
	w.tooltipCloseAt = time.Time{}
	if w.tooltipView != nil {
		w.tooltipView.close(w)
		w.tooltipView = nil
	}
}

// hitTestAll returns the topmost widget at p, checking overlays from
// top to bottom before falling through to the main root.
//
// Overlays that implement modalOverlay and report Modal()==true act
// as event firewalls: events that would have routed to a widget
// UNDERNEATH the modal (lower overlay or root) route to the modal
// overlay itself instead. Overlays above the modal still receive
// events (nested popups, tooltips on top of a dialog). Dialog's
// full-window HitTest achieves the same effect via geometry, but
// partial-geometry modals (a menu that doesn't cover the whole
// window yet still wants to swallow outside clicks) rely on this.
func (w *Window) hitTestAll(p Point) Widget {
	for i := len(w.overlays) - 1; i >= 0; i-- {
		if isWidgetHidden(w.overlays[i]) {
			continue
		}
		if hit := acceptHit(w.overlays[i].HitTest(p)); hit != nil {
			return hit
		}
		if m, ok := w.overlays[i].(modalOverlay); ok && m.Modal() {
			// Modal guard: anything below this overlay is unreachable.
			return w.overlays[i]
		}
	}
	if w.root == nil {
		return nil
	}
	return acceptHit(w.root.HitTest(p))
}

// SetRoot sets the root widget for the window. Also marks layout
// dirty so the next Step measures/lays out the fresh tree.
func (w *Window) SetRoot(root Widget) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetRoot")
	if w.root == root {
		return
	}
	if root != nil {
		if !AdoptWidgetTree(root, nil, w) {
			return
		}
	}
	old := w.root
	w.root = nil
	if old != nil {
		DetachWidgetTree(old)
	}
	w.root = root
	if root != nil {
		root.InvalidateLayout()
	}
	w.Invalidate()
}

// Root returns the currently installed root widget (nil if none).
func (w *Window) Root() Widget {
	if w == nil {
		return nil
	}
	w.assertUIThread("Window.Root")
	return w.root
}

// Bounds returns the window's full logical rect (origin at 0,0).
// Useful as a catch-all DirtyBounds for animators that move widgets
// across the window (the trail must also repaint).
func (w *Window) Bounds() Rect {
	if w == nil {
		return Rect{}
	}
	w.assertUIThread("Window.Bounds")
	return Rect{W: w.lastSize.W, H: w.lastSize.H}
}

// InvalidateLayout forces the next Step to run Measure + Layout on
// the tree. Use when changing a widget's size-affecting state from
// outside the normal invalidation path (e.g., programmatic edits
// without a wrapping Container.AddChild or widget SetText).
func (w *Window) InvalidateLayout() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.InvalidateLayout")
	if w.root == nil {
		return
	}
	w.root.InvalidateLayout()
}

// SetPreRender installs a raw-GL callback invoked each frame between
// the framebuffer clear and the 2D blit. Useful for shadertoy-style
// backgrounds that should sit under the widget tree. Pass nil to
// clear. No-ops when the current renderer doesn't implement raw GL
// hooks (e.g., NoopRenderer in tests).
func (w *Window) SetPreRender(fn func(GLState)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPreRender")
	if gl, ok := w.renderer.(*GLRenderer); ok {
		gl.SetPreRender(fn)
	}
}

// SetPostRender installs a raw-GL callback invoked each frame after
// the 2D blit, before SwapBuffers. Useful for GPU-composited overlays
// that should sit above the widget tree. Pass nil to clear.
func (w *Window) SetPostRender(fn func(GLState)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetPostRender")
	if gl, ok := w.renderer.(*GLRenderer); ok {
		gl.SetPostRender(fn)
	}
}

// SetGPURaster toggles the GPU raster backend on this window's
// renderer. No-op unless the current renderer is a *GLRenderer (e.g.
// NoopRenderer during tests). Also honored via the QUI_GPU_RASTER=1
// environment variable — set that once at startup and every
// GLRenderer created by NewGLRenderer picks it up.
func (w *Window) SetGPURaster(on bool) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetGPURaster")
	if gl, ok := w.renderer.(*GLRenderer); ok {
		gl.SetGPURaster(on)
	}
}

// SetRenderer assigns the renderer implementation.
func (w *Window) SetRenderer(renderer Renderer) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetRenderer")
	if renderer == nil {
		w.renderer = NoopRenderer{}
		return
	}
	w.renderer = renderer
	// An overlay panel's framebuffer must stay transparent where the widget
	// tree drew nothing. Wire it here rather than making every caller
	// remember: the window already knows its kind, and forgetting produces
	// a black rectangle, which looks like a rendering bug rather than a
	// missing call.
	if gr, ok := renderer.(*GLRenderer); ok {
		gr.SetTransparent(w.kind == WindowOverlayPanel)
	}
}

// SetCustomRender registers a custom render hook invoked every frame.
func (w *Window) SetCustomRender(fn func(Canvas)) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.SetCustomRender")
	w.onRender = fn
}

// IsHeadless reports whether this window has no OS window behind it —
// true for the test windows NewTestWindow builds. Callers use it to skip
// work that needs a real platform surface (animators, frame scheduling).
//
// This replaces the former Handle() accessor, which returned a *glfw.Window
// and so leaked the windowing library into the public API — precisely what
// the platform seam exists to prevent.
func (w *Window) IsHeadless() bool {
	if w == nil {
		return true
	}
	w.assertUIThread("Window.IsHeadless")
	return w.plat == nil
}

// ShouldClose indicates if the user requested closing the window.
func (w *Window) ShouldClose() bool {
	if w == nil {
		return true
	}
	w.assertUIThread("Window.ShouldClose")
	if w.plat == nil {
		return true
	}
	return w.plat.shouldClose()
}

// OnClose registers a callback that fires when the user closes the
// window (or some other code sets ShouldClose=true). Callbacks run on
// the qui main goroutine before the platform window is destroyed. Use this
// to drain cross-system resources (CEF browsers, audio decoders, native
// dialogs) that need the message loop alive during teardown.
//
// Multiple registrations are allowed and fire in registration order.
// A handler that needs to *cancel* the close should call
// w.handle.SetShouldClose(false) from inside the callback — but note
// that qui's standard loop will then keep running.
func (w *Window) OnClose(fn func()) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.OnClose")
	w.onClose = append(w.onClose, fn)
}

// OnFullscreenChange registers fn to run when the window enters or leaves
// fullscreen. It fires for EVERY change, not just ones made through
// SetFullscreen: the macOS traffic-light button and Ctrl+Cmd+F toggle native
// fullscreen behind the app's back, and a mode that must end with fullscreen
// (a slideshow) has to see those too. Callbacks run on the UI goroutine
// during Step; closing the window from inside one is safe.
func (w *Window) OnFullscreenChange(fn func(fullscreen bool)) {
	if w == nil || fn == nil {
		return
	}
	w.assertUIThread("Window.OnFullscreenChange")
	if len(w.onFSChange) == 0 {
		// Snapshot the state at first registration so listening while
		// already fullscreen does not immediately fire.
		w.wasFullscreen = w.IsFullscreen()
	}
	w.onFSChange = append(w.onFSChange, fn)
}

// pollFullscreenChange fires the OnFullscreenChange callbacks when the
// observed state differs from the last poll. Reports whether the window is
// still alive — a callback may close it, and the caller's frame must stop.
func (w *Window) pollFullscreenChange() bool {
	if len(w.onFSChange) == 0 {
		return true
	}
	fs := w.IsFullscreen()
	if fs == w.wasFullscreen {
		return true
	}
	w.wasFullscreen = fs
	handlers := make([]func(bool), len(w.onFSChange))
	copy(handlers, w.onFSChange)
	for _, fn := range handlers {
		fn(fs)
	}
	return w.plat != nil
}

// runCloseHandlers fires all registered OnClose callbacks once and
// then clears the slice so a subsequent Destroy (e.g. via App.Quit)
// doesn't re-invoke them. Called from App.RunStep right before
// w.Destroy(); also safe to invoke from Destroy directly so manual
// Destroy() callers get the same lifecycle.
func (w *Window) runCloseHandlers() {
	handlers := w.onClose
	w.onClose = nil
	for _, fn := range handlers {
		fn()
	}
}

// Destroy releases the GLFW window.
func (w *Window) Destroy() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Destroy")
	// Run any OnClose handlers first — even if the user calls Destroy
	// manually (rather than going through the ShouldClose path), they
	// should still get a chance to clean up subsystem resources before
	// GLFW tears the window down.
	w.runCloseHandlers()
	if w.unsubscribeTheme != nil {
		w.unsubscribeTheme()
		w.unsubscribeTheme = nil
	}
	if w.unsubscribeLocale != nil {
		w.unsubscribeLocale()
		w.unsubscribeLocale = nil
	}
	root := w.root
	overlays := append([]Widget(nil), w.overlays...)
	w.root = nil
	w.overlays = nil
	w.overlayFocusScopes = nil
	if root != nil {
		DetachWidgetTree(root)
	}
	for _, ov := range overlays {
		DetachWidgetTree(ov)
	}
	w.platMu.Lock()
	plat := w.plat
	w.plat = nil
	w.platMu.Unlock()
	if plat != nil {
		plat.destroy()
	}
}

// Step advances one frame of layout and rendering.
//
// Pipeline:
//  1. Tick animation-driven widgets (e.g., cursor blink). Each Tickable
//     returns its dirty Rect; the union is merged into dirtyRegion.
//  2. If dirtyRegion is empty, return without painting — the CPU image
//     buffer and front framebuffer retain the previous frame, so the
//     screen is stable. Paired with WaitEventsTimeout + SwapInterval(1),
//     idle CPU stays near zero.
//  3. Otherwise Measure/Layout the tree, then route Draw through a
//     clipCanvas scoped to dirtyRegion. Only pixels inside the clip are
//     touched; widgets outside the region are effectively no-ops at the
//     canvas layer. Background inside the region is reset to black so
//     text/cursor changes don't composite on top of stale pixels.
func (w *Window) Step() {
	if w == nil {
		return
	}
	w.assertUIThread("Window.Step")
	if w.plat == nil || w.inStep {
		return
	}
	w.inStep = true
	defer func() { w.inStep = false }()

	// Multi-window discipline: each Window owns an independent GL
	// context (optionally shared via NewSharedWindow). The renderer's
	// ensureInit and every GL call routed through Begin/End assume
	// "my" context is current — make it so before any GL work.
	w.plat.makeCurrent()
	// Drain cross-goroutine jobs (agent actions, deferred callbacks)
	// before any Tick / layout work so their state mutations show up
	// in this frame. New jobs posted during drain land in the next
	// Step — keeps frame time bounded.
	w.runJobs()
	// Fullscreen can change behind the app's back (traffic-light button,
	// Ctrl+Cmd+F); a change callback may close this window, ending the frame.
	if !w.pollFullscreenChange() {
		return
	}
	winW, winH := w.plat.size()
	viewport := w.noteWindowSize(winW, winH)
	w.resizeTo(viewport)
	now := time.Now()
	// Close a grace-pending tooltip before the overlay tick loop so
	// removing it doesn't mutate the slice ranged below.
	w.tickTooltip(now)
	w.tickAnimators(now)
	if w.root != nil {
		w.dirtyRegion = w.dirtyRegion.Union(tickWidget(w.root, now))
	}
	// Overlays animate independently. A popup with its own blink/timer
	// contributes its Tick Rect to the dirty region.
	for _, ov := range w.overlays {
		w.dirtyRegion = w.dirtyRegion.Union(tickWidget(ov, now))
	}
	// Layout caching: skip Measure+Layout unless something explicitly
	// invalidated the layout. This is the win for high-frequency
	// repaints like cursor blink — the layout hasn't changed, only
	// pixels need refreshing.
	//
	// Layout runs BEFORE the paint decision so the repaint can be
	// scoped: InvalidateLayout dirty-rects only its caller's extent; if
	// the relayout then MOVES any widget (boundsChanged), the frame is
	// promoted to a full repaint — moved widgets need their old and new
	// pixels (plus shadows/halos) refreshed and the single-rect dirty
	// region can't represent that precisely. A relayout that moves
	// nothing (text tick measuring to the same size) keeps the paint
	// clipped to the mutated widget's rect.
	w.lastSize = viewport
	if w.root != nil && w.root.IsLayoutDirty() {
		w.inLayoutPass = true
		w.boundsChanged = false
		w.root.Measure(w.lastSize)
		w.root.Layout(Rect{X: 0, Y: 0, W: w.lastSize.W, H: w.lastSize.H})
		w.root.ClearLayoutDirty()
		w.inLayoutPass = false
		if w.boundsChanged {
			w.Invalidate()
		}
	}
	// Overlay resize handling runs AFTER the layout pass so anchored
	// overlays (dropdowns pinned to a main-tree trigger) re-place against
	// the trigger's freshly-laid-out bounds. Their reposition dirties its
	// own rects, so this must precede the empty-dirty-region early return.
	if w.overlayResizePending {
		w.overlayResizePending = false
		w.notifyOverlaysResize(w.lastSize)
	}
	if w.dirtyRegion.IsEmpty() {
		return
	}
	fbWidth, fbHeight := w.plat.framebufferSize()
	// Overlays are pre-positioned by the caller via Layout before
	// PushOverlay. We don't re-layout them here — their geometry is
	// caller-driven. Just clear any stale dirty flags so they don't
	// accumulate.
	for _, ov := range w.overlays {
		if ov.IsLayoutDirty() {
			ov.ClearLayoutDirty()
		}
	}

	// Clamp dirty region to current window bounds (a widget could return
	// a Rect that extends past a resized window).
	clip := w.dirtyRegion.Intersect(Rect{W: w.lastSize.W, H: w.lastSize.H})
	if clip.IsEmpty() {
		w.dirtyRegion = Rect{}
		return
	}

	// Tell the renderer the true logical (point) size so deferred
	// GL hooks see a correct GLState.LogicalSize. Without this the
	// renderer would default to the framebuffer size and DPR math
	// inside QueueGLDraw closures would always compute to 1×.
	if glr, ok := w.renderer.(*GLRenderer); ok {
		glr.SetLogicalSize(w.lastSize)
	}
	canvas := w.renderer.Begin(Size{W: float32(fbWidth), H: float32(fbHeight)})
	if canvas != nil {
		// HiDPI: push the device-pixel ratio onto the canvas state stack
		// once per frame. Every subsequent draw call sees logical inputs
		// scaled to physical pixels. The Save/RestoreTo bookends below
		// keep this DPR isolated from any state widgets push internally.
		drawCanvas := canvas
		dprID := drawCanvas.Save()
		// Dividing the framebuffer by the VIEWPORT (not the window) makes
		// this ratio dpr×zoom, so viewport zoom needs no multiply of its
		// own here — and text, GL scissors and shadows all pick it up.
		if viewport.W > 0 && viewport.H > 0 && fbWidth > 0 && fbHeight > 0 {
			scaleX := float32(fbWidth) / viewport.W
			scaleY := float32(fbHeight) / viewport.H
			if scaleX != 1 || scaleY != 1 {
				drawCanvas.Scale(scaleX, scaleY)
			}
			// Grow the dirty region out to whole PHYSICAL pixels. Widget
			// bounds are fractional, so an un-snapped dirty rect cuts through
			// the middle of an edge pixel — and the primitives disagree about
			// what that means. An AA fill covers such a pixel partially (it
			// is clipped by the float rect), while a layer composite is a
			// per-pixel blit that either writes the pixel or doesn't. The
			// mismatch shows up as a one-pixel line of whatever was UNDER the
			// widget along the dirty rect's edge — a shape's drop shadow
			// bleeding through the bottom row of a repainted menu row, say.
			// Snapping outward costs at most one pixel per edge and makes
			// every primitive agree; a partial repaint then reproduces the
			// full repaint exactly.
			clip = snapRectOutward(clip, scaleX, scaleY)
		}
		// Route everything through clipCanvas so draws outside the dirty
		// region are elided. Clear only repaints the clipped area; it no
		// longer wipes the whole buffer, which is what preserves last
		// frame's un-dirty pixels.
		//
		// Clear to the active theme's Surface so any gap between widgets
		// (root container padding, inter-row gutters) reads as the
		// background color the rest of the UI sits on, instead of an
		// out-of-theme black that made showcases look like a
		// half-painted page. A theme swap automatically picks up the new
		// surface on the next paint.
		// Dirty-region clip: scope every widget draw to the invalidated
		// area so untouched pixels survive from the previous frame.
		drawCanvas.ClipRect(clip)
		// An overlay panel has no background of its own: it draws a rounded
		// card and lets the corners composite through to whatever is behind
		// the window. Clearing to Surface there would paint an opaque
		// rectangle over exactly the pixels the transparency exists for.
		if w.kind == WindowOverlayPanel {
			drawCanvas.Clear(ColorTransparent)
		} else {
			drawCanvas.Clear(CurrentTheme().Surface)
		}
		if w.root != nil {
			w.root.Draw(drawCanvas)
		}
		// Overlays paint after the main tree, bottom-of-stack first so
		// a later-pushed overlay sits on top. They share the same clip
		// so they're dirty-region-aware too.
		for _, ov := range w.overlays {
			if ov.Bounds().Intersects(clip) {
				ov.Draw(drawCanvas)
			}
		}
		if w.onRender != nil {
			w.onRender(drawCanvas)
		}
		if w.debugOverlayEnabled {
			w.drawDebugOverlay(drawCanvas)
		}
		drawCanvas.RestoreTo(dprID)
	}
	w.renderer.End()
	w.present()
	w.dirtyRegion = Rect{}
}

// present hands the finished frame to the platform.
//
// The CPU path is preferred when both sides support it: the reference
// rasterizer has already produced an *image.RGBA, so a compositor that can
// take it directly (CALayer contents, a wl_shm buffer) skips uploading a
// full-screen texture only to blit it straight back down — which is what
// the GL path must do every frame. When either side declines, the GPU path
// presents whatever was drawn into the backbuffer.
func (w *Window) present() {
	surface := w.plat.surface()
	if surface == nil {
		return
	}
	if src, ok := w.renderer.(CPUFrameSource); ok {
		if img := src.FrameImage(); img != nil && surface.presentCPU(img) {
			return
		}
	}
	surface.present()
}

// dispatch routes an event through the widget tree.
//
// Semantics:
//   - MouseDown finds the hit target, claims mouse capture, and runs
//     the three-phase dispatch along the root→target path.
//   - MouseUp prefers the captured widget (so Button.pressed gets
//     released even if the cursor wandered off) and releases capture.
//   - Scroll goes to the hit target under the cursor.
//   - Key/Char go to the focused widget (or root if no focus) along
//     the path from root to that widget.
//   - Drag events are synthesized here and dispatched directly to the
//     drag candidate — they don't participate in phased routing.
//
// During phased dispatch: capture runs root→parent-of-target, then the
// target fires, then bubble runs parent-of-target→root. At any point
// a handler may call event.StopPropagation() to halt further phases.
// A handler returning true is treated as an implicit StopPropagation
// for backward compatibility with pre-phased Handle implementations.
func (w *Window) dispatch(event Event) {
	if w == nil {
		return
	}
	w.assertUIThread("Window.dispatch")
	if w.root == nil {
		return
	}

	// Drag is synthesized from mouse events but dispatched out-of-band
	// (directly to the drag source/target) so it isn't affected by
	// phased routing.
	if me, ok := event.(MouseEvent); ok {
		switch me.eventType {
		case EventMouseDown:
			w.handleDragStartCandidate(me)
		case EventMouseMove:
			w.handleDragMove(me)
		case EventMouseUp:
			dragged := w.dragging
			w.handleDragEnd(me)
			if dragged {
				// The release that ended a drag keeps flowing (pressed
				// state has to unwind), but it is flagged so nothing
				// downstream reads it as a click on the dropped element.
				me.AfterDrag = true
				event = me
			}
		}
	}

	// Focus change happens on MouseDown, before the event is dispatched
	// into widgets — so a just-focused widget can see the MouseDown too.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseDown {
		w.updateFocusFromMouse(me)
	}

	// Tab / Shift-Tab navigate focus. Consumed before reaching widgets,
	// so TextArea et al. don't need to opt-out. A future "consumesTab"
	// interface could let specific widgets (code editors) intercept
	// Tab for their own use.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && ke.Key == KeyTab {
		// A focused widget (or ancestor) implementing TabConsumer can keep
		// Tab for itself; otherwise Tab navigates focus.
		if !w.tabConsumedByFocus(ke.Mods&ModShift != 0) {
			if ke.Mods&ModShift != 0 {
				w.FocusPrev()
			} else {
				w.FocusNext()
			}
			return
		}
	}

	// Any keyboard interaction with a mouse-focused widget upgrades its
	// focus to visible — matches CSS :focus-visible's "user is now
	// driving with keyboard" heuristic. Subsequent MouseDown elsewhere
	// flips it back to false.
	if !w.focusVisible && w.focused != nil {
		if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown {
			w.focusVisible = true
			if fv, ok := w.focused.(focusVisibleAware); ok {
				fv.SetFocusVisible(true)
			}
		}
	}

	// MouseMove: synthesize Enter/Leave based on hit-path diff, then
	// dispatch the MouseMove itself to the captured widget (if any,
	// for drag-select) or the hit target.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseMove {
		w.syncHoverPath(me)
		// Fall through to phased dispatch below; compute target next.
	}

	// Determine target for phased dispatch.
	target := w.eventTarget(event)
	if target == nil {
		target = w.root
	}

	// Recording fan-out — publish the event before widget handlers
	// run so listeners see the canonical (event, target) pair the
	// framework chose. Cheap when no listeners are registered.
	w.publishEventRecord(event, target)

	// Cross-widget copy. When a selection spans more than one widget,
	// Cmd/Ctrl+C is aggregated at the window level (in document order)
	// before the event reaches the focused widget's own single-widget
	// Cmd+C. A single-widget selection falls through unchanged.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown &&
		ke.Key == KeyC && IsCommandMod(ke.Mods) {
		if w.copyTextSelection() {
			return
		}
	}
	// Cross-widget select-all. When a read-only selectable (Label) holds
	// focus, Cmd/Ctrl+A selects the whole document; editable widgets keep
	// their own field-scoped select-all (they aren't TextSelectable).
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown &&
		ke.Key == KeyA && IsCommandMod(ke.Mods) {
		if w.selectAllText() {
			return
		}
	}

	// Mouse capture lifecycle. MouseDown claims; MouseUp releases.
	// MouseMove during capture routes to the captured widget so
	// drag-selection (TextArea) or button press-state updates keep
	// working even when the cursor wanders off the original widget.
	if me, ok := event.(MouseEvent); ok {
		switch me.eventType {
		case EventMouseDown:
			w.mouseCaptured = target
			w.beginTextSelectionDrag(target, me)
		case EventMouseMove:
			// A widget drag (handled out-of-band above) owns the gesture:
			// don't extend a text selection or route the move to the
			// captured widget's own drag-select.
			if w.dragging {
				return
			}
			if w.mouseCaptured != nil {
				// While a cross-widget text selection has taken over, the
				// controller owns the drag — skip routing to the captured
				// widget so its local selection doesn't fight the run.
				if w.updateTextSelectionDrag(me) {
					return
				}
				target = w.mouseCaptured
			}
		case EventMouseUp:
			if w.mouseCaptured != nil {
				target = w.mouseCaptured
			}
			w.endTextSelectionDrag()
			defer func() { w.mouseCaptured = nil }()
		}
	}

	// Gesture capture — mouse capture's multi-touch analogue. Began
	// claims the receiver, later events in the same gesture redirect to
	// it. See gesture.go.
	if ge, ok := event.(GestureEvent); ok {
		target = w.updateGestureCapture(ge, target)
	}

	path := pathToRoot(target)
	state := event.state()
	if state == nil {
		// Defensive: events created externally may not have shared state.
		// Install a fresh one so phase/target accessors return sane values.
		return
	}
	state.target = target

	// A blank-press selection drag is provisional: if any widget consumes
	// this MouseDown (scrollbar thumb, slider knob, …), that widget's own
	// gesture owns the mouse — disarm so drag-to-select doesn't fight it.
	if me, ok := event.(MouseEvent); ok && me.eventType == EventMouseDown &&
		w.textSel.active && w.textSel.fromBlank {
		defer func() {
			if state.stopped {
				w.disarmTextSelection()
			}
		}()
	}

	// Each node receives the event in its OWN coordinate space (see
	// eventInWidgetSpace) — identity for an untransformed tree, the inverse
	// scroll/CSS transform inside one.
	// Capture phase: root → parent-of-target (excludes target).
	for i := 0; i < len(path)-1; i++ {
		state.phase = PhaseCapture
		state.currentTarget = path[i]
		if path[i].Handle(eventInWidgetSpace(event, path[i])) {
			state.stopped = true
		}
		if state.stopped {
			return
		}
	}

	// Target phase.
	state.phase = PhaseTarget
	state.currentTarget = target
	if target.Handle(eventInWidgetSpace(event, target)) {
		state.stopped = true
	}
	if state.stopped {
		return
	}

	// Bubble phase: parent-of-target → root.
	for i := len(path) - 2; i >= 0; i-- {
		state.phase = PhaseBubble
		state.currentTarget = path[i]
		if path[i].Handle(eventInWidgetSpace(event, path[i])) {
			state.stopped = true
		}
		if state.stopped {
			return
		}
	}

	// Accelerator fallback. Only fires for unhandled KeyDown events so
	// focused widgets' local shortcuts (Input Cmd+Z, etc.) still win
	// when they want to. Menus bind their items' shortcuts here so
	// Cmd+S / Cmd+O work without the grid having to intercept them.
	//
	// The agent overlay shortcut (Cmd+Shift+A) is checked unconditionally
	// — it self-configures, unlike the layout overlay which requires a
	// prior EnableDebugOverlay() opt-in.
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && w.accels != nil {
		if w.handleZoomShortcut(ke) {
			return
		}
		if w.handleAgentOverlayShortcut(ke) {
			return
		}
		if w.handleDebugOverlayShortcut(ke) {
			return
		}
		w.accels.Match(ke)
	}
	if ke, ok := event.(KeyEvent); ok && ke.eventType == EventKeyDown && w.accels == nil {
		if w.handleZoomShortcut(ke) {
			return
		}
		if w.handleAgentOverlayShortcut(ke) {
			return
		}
		w.handleDebugOverlayShortcut(ke)
	}
}

// eventTarget chooses the target widget for phased dispatch based on
// event type. Returns nil if no natural target exists (caller falls
// back to root).
func (w *Window) eventTarget(event Event) Widget {
	if w.focused != nil && !w.validFocusTarget(w.focused) {
		w.SetFocus(nil)
	}
	switch e := event.(type) {
	case MouseEvent:
		return w.hitTestAll(Point{X: e.X, Y: e.Y})
	case GestureEvent:
		return w.hitTestAll(Point{X: e.X, Y: e.Y})
	case KeyEvent:
		if w.focused != nil {
			return w.focused
		}
	case CharEvent:
		if w.focused != nil {
			return w.focused
		}
	case FocusEvent:
		if w.focused != nil {
			return w.focused
		}
	}
	return nil
}

// updateFocusFromMouse transfers focus to the clicked widget if it is
// focusable. Called on MouseDown, before the event reaches the tree,
// so the newly-focused widget sees the same event.
func (w *Window) updateFocusFromMouse(me MouseEvent) {
	target := w.hitTestAll(Point{X: me.X, Y: me.Y})
	// Walk up from the hit target to the nearest focusable widget, so a
	// click on non-focusable content (e.g. plain text inside an interactive
	// container) focuses that container — matching how a browser focuses the
	// innermost focusable element under the pointer.
	for cur := target; cur != nil; cur = cur.Parent() {
		if f, ok := cur.(registerFocusable); ok && f.Focusable() {
			w.focusVisible = false
			w.SetFocus(cur)
			return
		}
	}
}

// pathToRoot returns [root, ..., widget] — the ancestor chain from the
// root down to the given widget, inclusive. Walks via Parent() pointers,
// which Container maintains on AddChild.
func pathToRoot(w Widget) []Widget {
	var path []Widget
	for cur := w; cur != nil; cur = cur.Parent() {
		path = append(path, cur)
	}
	// Reverse in place to get root-first order.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// syncHoverPath compares the current hoverPath with a freshly-computed
// hit path at the given cursor position and fires MouseLeave on
// widgets the cursor exited, MouseEnter on widgets it entered. Each
// synthesized event targets exactly one widget (no bubble — matches
// DOM mouseenter/mouseleave semantics).
func (w *Window) syncHoverPath(me MouseEvent) {
	newHit := w.hitTestAll(Point{X: me.X, Y: me.Y})
	var newPath []Widget
	if newHit != nil {
		newPath = pathToRoot(newHit)
	}

	inNew := make(map[Widget]bool, len(newPath))
	for _, wd := range newPath {
		inNew[wd] = true
	}
	inOld := make(map[Widget]bool, len(w.hoverPath))
	for _, wd := range w.hoverPath {
		inOld[wd] = true
	}

	// Leave: walk old path leaf-first so the deepest widget sees leave
	// before its ancestors.
	for i := len(w.hoverPath) - 1; i >= 0; i-- {
		wd := w.hoverPath[i]
		if inNew[wd] {
			continue
		}
		p := WindowPointToLocal(wd, Point{X: me.X, Y: me.Y})
		wd.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: wd, phase: PhaseTarget, currentTarget: wd}},
			eventType: EventMouseLeave,
			When:      me.When,
			X:         p.X,
			Y:         p.Y,
		})
	}
	// Enter: walk new path root-first so outer widgets enter before
	// their descendants.
	for _, wd := range newPath {
		if inOld[wd] {
			continue
		}
		p := WindowPointToLocal(wd, Point{X: me.X, Y: me.Y})
		wd.Handle(MouseEvent{
			baseEvent: baseEvent{shared: &eventState{target: wd, phase: PhaseTarget, currentTarget: wd}},
			eventType: EventMouseEnter,
			When:      me.When,
			X:         p.X,
			Y:         p.Y,
		})
	}
	w.hoverPath = newPath
	w.updateTooltipFromHover(me.X, me.Y)
	w.updateCursorFromHover(me.X, me.Y)
}

func (w *Window) handleDragStartCandidate(me MouseEvent) {
	if me.Button != MouseButtonLeft {
		return
	}
	target := w.hitTestAll(Point{X: me.X, Y: me.Y})
	if target == nil {
		w.dragCandidate = nil
		return
	}
	// Walk ancestors: the hit target is usually a leaf (a row's inner label
	// or button), while the draggable unit is a container above it. A widget
	// that is itself Draggable is its own first ancestor, so this stays
	// backward compatible with leaf-draggable widgets.
	if src := draggableAncestor(target); src != nil {
		w.dragCandidate = src
		w.dragStart = Point{X: me.X, Y: me.Y}
		w.dragging = false
	}
}

// draggableAncestor returns the nearest self-or-ancestor of w that reports
// Draggable() == true, or nil.
func draggableAncestor(w Widget) Widget {
	for cur := w; cur != nil; cur = cur.Parent() {
		if d, ok := cur.(Draggable); ok && d.Draggable() {
			return cur
		}
	}
	return nil
}

// droppableAncestor returns the nearest self-or-ancestor of w that reports
// Droppable() == true, or nil.
func droppableAncestor(w Widget) Widget {
	for cur := w; cur != nil; cur = cur.Parent() {
		if d, ok := cur.(Droppable); ok && d.Droppable() {
			return cur
		}
	}
	return nil
}

func (w *Window) handleDragMove(me MouseEvent) {
	if w.dragCandidate == nil {
		return
	}
	dx := me.X - w.dragStart.X
	dy := me.Y - w.dragStart.Y
	if !w.dragging {
		if dx*dx+dy*dy < 16 {
			return
		}
		w.dragging = true
		// A widget drag now owns the gesture. Drop any text selection that
		// formed during the sub-dead-zone moves and disarm the selection
		// drag so the highlight doesn't fight the drag (dispatch also stops
		// routing moves to the captured widget while w.dragging).
		w.clearAllTextSelection()
		w.endTextSelectionDrag()
		w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragStart, Point{X: me.X, Y: me.Y}))
	}
	if w.dragging {
		w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragMove, Point{X: me.X, Y: me.Y}))
		// Notify the droppable under the cursor (standard DnD dragover) so a
		// target can show a drop indicator. Fired every move; the target
		// dedupes if it wants.
		if over := droppableAncestor(w.hitTestAll(Point{X: me.X, Y: me.Y})); over != nil {
			over.Handle(w.dragEventFor(over, EventDragOver, Point{X: me.X, Y: me.Y}))
		}
	}
}

func (w *Window) handleDragEnd(me MouseEvent) {
	if w.dragCandidate == nil {
		return
	}
	if w.dragging {
		w.dragCandidate.Handle(w.dragEventFor(w.dragCandidate, EventDragEnd, Point{X: me.X, Y: me.Y}))
		hit := w.hitTestAll(Point{X: me.X, Y: me.Y})
		if target := droppableAncestor(hit); target != nil {
			target.Handle(w.dragEventFor(target, EventDrop, Point{X: me.X, Y: me.Y}))
		}
	}
	w.dragCandidate = nil
	w.dragging = false
}

// dragEventFor builds a synthesized drag event addressed to receiver, with
// the cursor position mapped into the RECEIVER's own coordinate space — a
// drop target inside a scroll container computes its insertion index from
// its children's retained (content-space) bounds, so a raw window
// coordinate would land in the wrong row.
func (w *Window) dragEventFor(receiver Widget, kind EventType, at Point) DragEvent {
	p := WindowPointToLocal(receiver, at)
	return DragEvent{
		baseEvent: baseEvent{shared: &eventState{}},
		eventType: kind,
		When:      time.Now(),
		X:         p.X,
		Y:         p.Y,
		Source:    w.dragCandidate,
	}
}

// Ensure window has a valid root set.
func (w *Window) EnsureRoot() error {
	if w == nil {
		return errors.New("window is nil")
	}
	w.assertUIThread("Window.EnsureRoot")
	if w.root == nil {
		return errors.New("window root is nil")
	}
	return nil
}
