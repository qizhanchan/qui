package qui

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Window is one OS window plus the widget tree it renders. The OS half
// lives behind the platform seam (see platform.go), so nothing here knows
// which windowing backend is in use.
type Window struct {
	// docNodeID / targetSeq are lazily allocated identities for the agent
	// protocol: the DOM #document node and the window's target ID. See
	// inspect_nodeid.go.
	docNodeID uint64
	targetSeq uint64
	// Protocol state (see protocol_events.go): the painted-frame counter,
	// the AX hash DOM.documentUpdated diffs against, the Overlay highlight
	// and whether inspect mode owns the pointer. UI goroutine only.
	frameSeq     uint64
	lastDOMHash  string
	highlight    *overlayHighlight
	inspectMode  bool
	inspectEatUp bool
	// lifecycle is when the window reached DOMContentLoaded / load (see
	// protocol_page.go); written on the UI goroutine, read from any.
	lifecycle pageLifecycle
	// anim is the animators' clock (Animation domain); metricsBefore the
	// size to restore after Emulation.setDeviceMetricsOverride.
	anim          animClock
	metricsBefore *Size
	// title is the last title set (NewWindow / SetTitle), read by the agent
	// protocol's target list. Guarded by titleMu: the list is read off the
	// UI goroutine.
	titleMu sync.Mutex
	title   string

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
	focusListeners []focusListener
	// eventFilters run before any dispatch (AddEventFilter), most recently
	// added first. nextHookID numbers filters and focus listeners so their
	// removers stay valid as other entries come and go.
	eventFilters []eventFilterEntry
	nextHookID   int64
	// focusVisible mirrors CSS :focus-visible's input-modality heuristic:
	// true while the user drives with the keyboard (Tab / Shift+Tab, any
	// key, or no interaction yet), false after a pointer press. Programmatic
	// SetFocus inherits it — autofocus on a fresh window shows a ring, a
	// focus() call from a click handler doesn't. Widgets implementing
	// focusVisibleAware get told via SetFocusVisible(bool) and use this to
	// decide whether to paint a focus ring / state-layer halo.
	focusVisible bool
	// pressFocus is the focus move a MouseDown owes, applied when its
	// dispatch ends (or earlier, through FlushPressFocus).
	pressFocus    pendingPressFocus
	dragCandidate Widget
	dragging      bool
	// dragEnding is set while handleDragEnd dispatches Drop / DragEnd. A
	// drop handler that moves the source ("remove the card here, add it
	// there") must not cancel the drag being finished: it ends normally,
	// Accepted as the target said.
	dragEnding bool
	// dragCancelled marks a drag Escape cancelled while the button was
	// still down: the release that follows ends that gesture, so it is
	// flagged AfterDrag rather than read as a click where it lands.
	dragCancelled bool
	dragStart     Point
	// drag is the in-flight drag's payload, hovered target and drag
	// image. See event_drag.go.
	drag dragSession
	// mouseCaptured remembers the widget that received MouseDown so that
	// the matching MouseUp reaches it even if the cursor has since moved
	// off. Standard "mouse capture" semantics: set on down, cleared on up.
	mouseCaptured Widget
	// pressOrphaned is set when the widget holding mouse capture is removed
	// from the window while the button is still down — typically a menu row
	// that acts on MouseDown and closes its popup. The release that follows
	// belongs to nothing: delivered by hit test instead it would land on
	// whatever was under the popup and fire that element's click.
	pressOrphaned bool
	// gestureTarget is mouse capture's equivalent for multi-touch: the
	// widget that received a gesture's Began keeps every Changed/Ended
	// even if the fingers drift outside its bounds. See event_gesture.go.
	gestureTarget Widget
	// gestureScale / gestureRotation accumulate the platform's per-event
	// increments into the cumulative values GestureEvent reports.
	gestureScale    float32
	gestureRotation float32
	// zoom is the viewport zoom factor (1 == 100%); zoomRestore remembers
	// the last non-100% level for smart-magnify's toggle; zoomEnabled gates
	// the window-level zoom shortcuts and gesture fallback. See window_zoom.go.
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
	// fileDropRetains counts RetainFileDrops holders (widget drop zones).
	fileDropRetains int
	// lastMods is the modifier state from the most recent key or
	// mouse-button event. Cursor motion doesn't report modifiers, so
	// moves borrow this rather than polling per pixel.
	lastMods Modifiers
	// textSel coordinates browser-style cross-widget text selection — a
	// drag that starts in one TextSelectable and runs across siblings.
	// See selection.go.
	textSel textSelectionDrag
	// selClicks counts consecutive presses for the selection controller's
	// multi-click granularity (double = word, triple = line).
	selClicks selectionClicks
	// clicks counts consecutive presses for MouseEvent.Clicks.
	clicks clickCounter
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
	// overlayLayers records each shown overlay's layer; overlays stays
	// sorted by it (PushOverlayLayer).
	overlayLayers map[Widget]OverlayLayer
	// afterLayout holds AfterLayout callbacks for the next layout pass.
	afterLayout []func()
	// afterFrame holds work that must observe a finished frame — agent
	// captures (see synchronouslyAfterFrame). Step runs it on the way out,
	// after this frame's jobs, ticks, layout and paint.
	afterFrame []func()
	// exitingOverlays were removed from the stack but are still painting
	// an exit animation (OverlayExiter). They take no input.
	exitingOverlays []Widget
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
	// tooltipStyle is SetTooltipStyle's value; tooltipShowAt / tooltipText
	// hold a tooltip waiting out TooltipStyle.Delay.
	tooltipStyle  TooltipStyle
	tooltipShowAt time.Time
	tooltipText   string
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
	// tickingAnimators is set while tickAnimators walks the slice, so an
	// UnregisterAnimator from inside a Tick nils the slot instead of
	// splicing the slice out from under the walk.
	tickingAnimators bool
	// tickAt is the earliest RequestTickAt deadline still pending; Step
	// clears it once reached. framePainted records that the last Step
	// painted, which buys the window another frame (see window_idle.go).
	tickAt       time.Time
	framePainted bool
	// glRedraw is a pending RequestGLRedraw: a frame that only replays
	// retained GL draws (internal/render/gl_retained.go). Atomic because any goroutine may
	// request one.
	glRedraw atomic.Bool
	// animQuietSince starts a stretch of Steps that ticked animators but
	// painted nothing; zero while the window paints or has no animators.
	animQuietSince time.Time
	// inPaint marks the widget Draw pass (see window_paint_debug.go). The paint
	// pass must be read-only; QUI_DEBUG_DRAW=1 enforces it.
	inPaint bool

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
	// See overlay_window.go.
	kind WindowKind
	// cursorShape is the last cursor style requested by widgets.
	cursorShape CursorShape
	// raw is the raw keyboard capture configuration (keyboard_raw.go).
	raw rawKeyboardState
	// keyCapture is system shortcut capture (keyboard_capture.go).
	keyCapture keyCaptureState
	// nativeID is the handle the darwin input bridges know this window by
	// (the IME registry id); 0 when there is none.
	nativeID uintptr
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
	onClose hookList[func()]
	// life holds close-request vetoes, activation / move / minimize
	// callbacks and window ownership. See window_lifecycle.go.
	life windowLifecycle
	// onFSChange callbacks fire when the window's fullscreen state changes —
	// including changes the app did not make (the traffic-light button,
	// Ctrl+Cmd+F). wasFullscreen is the last observed state; the check is a
	// per-Step poll that only runs while someone is listening.
	onFSChange    hookList[func(bool)]
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
		title:          cfg.Title,
		plat:           pw,
		platformBacked: true,
		renderer:       defaultRenderer(pw),
		kind:           cfg.Kind,
		lastSize:       Size{W: float32(width), H: float32(height)},
		windowSize:     Size{W: float32(width), H: float32(height)},
		zoom:           1,
		focusVisible:   true, // no pointer interaction yet
		cursorShape:    CursorDefault,
	}
	w.initUIThreadOwnership()
	// Whether the OS window has keyboard focus right now; onFocus tracks
	// it from here on. Asked, not assumed: a process launched from a
	// terminal is not necessarily the active app.
	if f, ok := platformOptional[contract.Focuser](pw); ok {
		w.keyCapture.osFocused = f.IsFocused()
	}
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

	// Install the platform-specific IME backend.
	w.installIMEBackend()

	registerLiveWindow(w)
	return w, nil
}

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
	w.titleMu.Lock()
	w.title = title
	w.titleMu.Unlock()
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

// PlatformBacked reports whether w was created on a real windowing
// backend (true even after Destroy) as opposed to a headless test window.
// Unlike IsHeadless it is safe from any goroutine: the flag is fixed at
// construction. Cross-goroutine code uses it to decide whether there is a
// UI thread to hop to.
func (w *Window) PlatformBacked() bool {
	return w != nil && w.platformBacked
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
// OnClose runs once the close is final; to cancel a close the user asked
// for (unsaved changes), register OnCloseRequest instead.
func (w *Window) OnClose(fn func()) (remove func()) {
	if w == nil || fn == nil {
		return func() {}
	}
	w.assertUIThread("Window.OnClose")
	return w.onClose.add(fn)
}

// OnFullscreenChange registers fn to run when the window enters or leaves
// fullscreen. It fires for EVERY change, not just ones made through
// SetFullscreen: the macOS traffic-light button and Ctrl+Cmd+F toggle native
// fullscreen behind the app's back, and a mode that must end with fullscreen
// (a slideshow) has to see those too. Callbacks run on the UI goroutine
// during Step; closing the window from inside one is safe.
func (w *Window) OnFullscreenChange(fn func(fullscreen bool)) (remove func()) {
	if w == nil || fn == nil {
		return func() {}
	}
	w.assertUIThread("Window.OnFullscreenChange")
	if w.onFSChange.len() == 0 {
		// Snapshot the state at first registration so listening while
		// already fullscreen does not immediately fire.
		w.wasFullscreen = w.IsFullscreen()
	}
	return w.onFSChange.add(fn)
}

// pollFullscreenChange fires the OnFullscreenChange callbacks when the
// observed state differs from the last poll. Reports whether the window is
// still alive — a callback may close it, and the caller's frame must stop.
func (w *Window) pollFullscreenChange() bool {
	if w.onFSChange.len() == 0 {
		return true
	}
	fs := w.IsFullscreen()
	if fs == w.wasFullscreen {
		return true
	}
	w.wasFullscreen = fs
	for _, fn := range w.onFSChange.snapshot() {
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
	handlers := w.onClose.snapshot()
	w.onClose.clear()
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
	defer unregisterLiveWindow(w)
	// Run any OnClose handlers first — even if the user calls Destroy
	// manually (rather than going through the ShouldClose path), they
	// should still get a chance to clean up subsystem resources before
	// GLFW tears the window down.
	w.runCloseHandlers()
	w.releaseOwnership()
	if w.unsubscribeTheme != nil {
		w.unsubscribeTheme()
		w.unsubscribeTheme = nil
	}
	if w.unsubscribeLocale != nil {
		w.unsubscribeLocale()
		w.unsubscribeLocale = nil
	}
	root := w.root
	overlays := append(append([]Widget(nil), w.overlays...), w.exitingOverlays...)
	w.root = nil
	w.overlays = nil
	w.overlayFocusScopes = nil
	w.overlayLayers = nil
	w.exitingOverlays = nil
	w.afterFrame = nil
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
