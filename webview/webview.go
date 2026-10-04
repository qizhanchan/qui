package webview

import (
	"image"
	"sync"
	"time"

	"github.com/qizhanchan/qui"
)

// cpuFrameProvider is an optional Page extension for backends that
// deliver pixels as a Go-owned image.RGBA instead of an OpenGL texture
// — the CPU path on darwin; the IOSurface zero-copy GL path is not
// implemented yet. WebView.Draw probes the page via type assertion; when
// found, it uses canvas.DrawImage and skips the GL composite. This is
// also the path that makes the widget work with qui.RecordingCanvas
// during tests, since DrawImage is available on every Canvas.
type cpuFrameProvider interface {
	LatestCPUFrame() *image.RGBA
}

// WebView is the cross-platform widget that embeds a web page into
// the qui widget tree. Construct with NewWebView; call Open to spawn
// the underlying page; LoadURL / LoadHTML to navigate; Destroy when
// done.
//
// Threading: every exported method is safe to call from any goroutine.
// The widget forwards to a Page; the Page serializes calls onto the
// backend's UI thread internally.
//
// Lifecycle:
//
//	v := webview.NewWebView(webview.Config{InitialURL: "https://..."})
//	if err := v.Open(); err != nil { ... }
//	defer v.Destroy()
//
//	root.AddChild(v) // place into a Container / Flex / Grid / ScrollView
//
// The widget implements qui.Tickable (drives the backend pump every
// frame and promotes new frames), qui.IMEClient (forwards CJK
// composition to the page), and registerFocusable (so MouseDown
// inside the widget focuses it and key events route here).
type WebView struct {
	qui.BaseWidget

	cfg Config

	mu          sync.RWMutex
	page        Page
	pageURL     string
	pageTitle   string
	loadState   LoadState
	hoverInside bool
	focused     bool

	// Last-known texture / size from AcquireFrame. Stashed so Draw
	// (which runs on main goroutine, no lock) can use them without
	// reaching back into the page.
	texHandle uint32
	texW      int
	texH      int
	hasFrame  bool

	// Last (w, h, scale) actually sent to Page.Resize. Used to suppress
	// no-op resizes and to detect DPR changes from Tick (the window's
	// framebuffer callback invalidates but doesn't trigger Layout, so
	// Tick is the place where a display-DPR change gets noticed).
	appliedW     int
	appliedH     int
	appliedScale float32

	// preeditActive mirrors the "CEF currently holds a marked-text
	// composition" state. SetPreedit sets/clears it; the next CharEvent
	// while it's set is treated as the IME's commit. See Handle for
	// the full lifecycle reasoning — the short version is that macOS
	// emits commit text as a stream of CharEvents (via GLFW's char
	// callback) while CEF's own marked-text state is independent and
	// only cleared by an explicit ImeCommitText / ImeFinishComposingText.
	// Without this guard, the marked text stays in place and the commit
	// chars get appended after it.
	preeditActive bool

	handlers *handlerTable

	// Multi-click tracking for double-click (select word) / triple-click
	// (select line/paragraph) semantics. We compute clickCount locally
	// and pass it through to CEF.
	lastClickWhen   time.Time
	lastClickX      float32
	lastClickY      float32
	lastClickButton qui.MouseButton
	lastClickCount  int
	downClickCount  int

	onLoadStart  func(string)
	onLoadFinish func(string)
	onTitle      func(string)
	onConsole    func(level int, message string)
	onCursor     func(CursorKind)
}

const (
	multiClickMaxGap = 500 * time.Millisecond
	multiClickSlopPx = float32(4)
)

// nowFunc indirects time.Now so click-counting tests can pin a virtual
// clock — see TestHandleMouseMultiClickCounts. Synthetic events built
// via qui.NewMouseEvent carry a zero When, so we cannot lean on
// e.Timestamp() for the gap measurement.
var nowFunc = time.Now

// NewWebView constructs an empty WebView. Open() spawns the underlying
// page; until then, LoadURL / Evaluate / handler registration are
// buffered or return ErrClosed. Calls SetSelf so framework walks see
// the concrete *WebView (required for any BaseWidget subclass).
func NewWebView(cfg Config) *WebView {
	v := &WebView{
		BaseWidget: qui.NewBaseWidget(),
		cfg:        cfg,
		handlers:   newHandlerTable(),
		loadState:  LoadStateIdle,
	}
	v.SetSelf(v)
	return v
}

// Open creates the underlying Page. Returns ErrNotSupported if no
// backend is registered for the current build. Subsequent calls after
// a successful Open are no-ops; after Destroy they return ErrClosed.
func (v *WebView) Open() error {
	v.mu.Lock()
	if v.page != nil {
		v.mu.Unlock()
		return nil
	}
	v.mu.Unlock()

	b := activeBackend()
	if b == nil {
		return ErrNotSupported
	}

	bounds := v.Bounds()
	w, h := int(bounds.W), int(bounds.H)
	if w <= 0 {
		w = 800
	}
	if h <= 0 {
		h = 600
	}
	scale := v.effectiveScale()

	pageCfg := PageConfig{
		Width:       w,
		Height:      h,
		DeviceScale: scale,
		Transparent: v.cfg.Transparent,
		UserAgent:   v.cfg.UserAgent,
		InitialURL:  v.cfg.InitialURL,
	}
	page, err := b.NewPage(pageCfg)
	if err != nil {
		return err
	}

	v.mu.Lock()
	v.page = page
	v.pageURL = v.cfg.InitialURL
	v.loadState = LoadStateLoading
	// Deliberately leave appliedW/H/Scale at zero. The first Layout (or
	// Tick) after Open then issues a Resize for the real bounds + DPR,
	// which keeps the page synced with the widget tree even when the
	// initial PageConfig sizes were a fallback.
	v.mu.Unlock()

	v.wireListeners(page)
	v.rewireHandlers(page)
	return nil
}

// wireListeners installs the WebView's internal forwarders on the
// Page so that backend callbacks update widget state and fire the
// user-facing On* hooks. Called once on Open.
func (v *WebView) wireListeners(page Page) {
	page.SetLoadListener(func(ev LoadEvent) {
		v.mu.Lock()
		v.loadState = ev.State
		if ev.URL != "" {
			v.pageURL = ev.URL
		}
		startCB, finishCB := v.onLoadStart, v.onLoadFinish
		focused := v.focused
		url := v.pageURL
		v.mu.Unlock()
		v.Invalidate()
		switch ev.State {
		case LoadStateLoading:
			if startCB != nil {
				startCB(url)
			}
		case LoadStateLoaded:
			// LoadURL tears down the old RenderFrameHost; the new frame
			// doesn't inherit the browser-level focus we set when the
			// widget was first clicked, so caret blink + input focus on
			// the new page would be broken. Re-arm focus when qui still
			// considers the WebView focused.
			if focused {
				_ = page.InjectFocus(true)
			}
			if finishCB != nil {
				finishCB(url)
			}
		}
	})
	page.SetTitleListener(func(t string) {
		v.mu.Lock()
		v.pageTitle = t
		cb := v.onTitle
		v.mu.Unlock()
		if cb != nil {
			cb(t)
		}
	})
	page.SetConsoleListener(func(level int, message string) {
		v.mu.RLock()
		cb := v.onConsole
		v.mu.RUnlock()
		if cb != nil {
			cb(level, message)
		}
	})
	page.SetCursorListener(func(k CursorKind) {
		v.mu.RLock()
		cb := v.onCursor
		v.mu.RUnlock()
		// Apply to the owning window by default so hover-aware cursors
		// (text fields, hyperlinks, resize handles in the page)
		// "just work" without every embedder wiring a listener. A user
		// callback can either complement this (return after the default
		// already ran) or override it by calling Window.SetCursor with
		// a different shape after their own logic.
		if win := v.Window(); win != nil {
			win.SetCursor(cursorKindToShape(k))
		}
		if cb != nil {
			cb(k)
		}
	})
}

// rewireHandlers re-registers all handlers from the local table onto
// a fresh Page. Called on Open and after any page recreation.
func (v *WebView) rewireHandlers(page Page) {
	for _, name := range v.handlers.Names() {
		fn := v.handlers.Get(name)
		if fn != nil {
			_ = page.RegisterHandler(name, fn)
		}
	}
}

// LoadURL navigates the page to url. ErrClosed if Destroy was called.
func (v *WebView) LoadURL(url string) error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.LoadURL(url)
}

// LoadHTML navigates to inline HTML. baseURL is used for relative
// resource resolution; pass "about:blank" for purely-self-contained
// snippets.
func (v *WebView) LoadHTML(html, baseURL string) error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.LoadHTML(html, baseURL)
}

// Reload re-fetches the current page.
func (v *WebView) Reload() error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.Reload()
}

// Stop cancels an in-flight navigation.
func (v *WebView) Stop() error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.StopLoad()
}

// GoBack navigates one entry back in session history if possible.
func (v *WebView) GoBack() error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.GoBack()
}

// GoForward navigates one entry forward in session history.
func (v *WebView) GoForward() error {
	p, err := v.requirePage()
	if err != nil {
		return err
	}
	return p.GoForward()
}

// URL returns the last-known navigated URL. Updated whenever a
// LoadEvent fires.
func (v *WebView) URL() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.pageURL
}

// Title returns the last-known document title. Updated on every
// CefDisplayHandler::OnTitleChange.
func (v *WebView) Title() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.pageTitle
}

// State returns the current load state.
func (v *WebView) State() LoadState {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.loadState
}

// EvaluateJS runs script in the main frame and returns the result.
// Blocks until the renderer process replies. Result is a JSValue
// tagged by JSValueKind — see types.go.
func (v *WebView) EvaluateJS(script string) (JSValue, error) {
	p, err := v.requirePage()
	if err != nil {
		return JSValue{}, err
	}
	return p.EvaluateJS(script)
}

// RegisterHandler installs a JS→Go bridge endpoint. The JS side calls
// `window.qui.<name>(payloadJSONString)` and gets a Promise back; the
// resolved value is the handler's `reply` (also a JSON string).
//
// Buffering: handlers registered before Open are queued and applied
// once the Page exists. Calling RegisterHandler after Destroy is a
// no-op (returns ErrClosed).
func (v *WebView) RegisterHandler(name string, fn MessageHandler) error {
	if fn == nil {
		return v.UnregisterHandler(name)
	}
	v.handlers.Set(name, fn)
	v.mu.RLock()
	p := v.page
	v.mu.RUnlock()
	if p == nil {
		return nil // buffered; rewireHandlers will apply on Open
	}
	return p.RegisterHandler(name, fn)
}

// UnregisterHandler removes a handler registered via RegisterHandler.
func (v *WebView) UnregisterHandler(name string) error {
	v.handlers.Delete(name)
	v.mu.RLock()
	p := v.page
	v.mu.RUnlock()
	if p == nil {
		return nil
	}
	return p.UnregisterHandler(name)
}

// OnLoadStart registers a callback that fires when the main frame
// begins loading. Fires on the qui main goroutine.
func (v *WebView) OnLoadStart(fn func(url string)) {
	v.mu.Lock()
	v.onLoadStart = fn
	v.mu.Unlock()
}

// OnLoadFinish registers a callback that fires after the main frame
// finishes loading (DOMContentLoaded equivalent).
func (v *WebView) OnLoadFinish(fn func(url string)) {
	v.mu.Lock()
	v.onLoadFinish = fn
	v.mu.Unlock()
}

// OnTitleChanged registers a callback for document.title changes.
func (v *WebView) OnTitleChanged(fn func(title string)) {
	v.mu.Lock()
	v.onTitle = fn
	v.mu.Unlock()
}

// OnConsoleMessage registers a callback for browser-side console
// output. level mirrors Chromium's logging severity (0=verbose,
// 1=info, 2=warning, 3=error). Useful for surfacing JS errors into
// Go logs during development.
func (v *WebView) OnConsoleMessage(fn func(level int, message string)) {
	v.mu.Lock()
	v.onConsole = fn
	v.mu.Unlock()
}

// OnCursorChange registers a callback when CEF requests a cursor
// change. The default behavior (when no listener is set) is to ignore
// — apps that want hover-aware cursors should hook this to
// glfw.Window.SetCursor or qui's equivalent.
func (v *WebView) OnCursorChange(fn func(CursorKind)) {
	v.mu.Lock()
	v.onCursor = fn
	v.mu.Unlock()
}

// Destroy releases the underlying Page. Idempotent and safe from any
// goroutine. After Destroy, all methods return ErrClosed.
func (v *WebView) Destroy() {
	v.mu.Lock()
	p := v.page
	v.page = nil
	v.mu.Unlock()
	if p != nil {
		p.Destroy()
	}
}

// requirePage returns the live Page or an error if not yet opened /
// already destroyed. Read-locked.
func (v *WebView) requirePage() (Page, error) {
	v.mu.RLock()
	p := v.page
	v.mu.RUnlock()
	if p == nil {
		if activeBackend() == nil {
			return nil, ErrNotSupported
		}
		return nil, ErrClosed
	}
	return p, nil
}

// -----------------------------------------------------------------------------
// qui.Widget / qui.Tickable / qui.IMEClient / qui.Focusable plumbing.
// -----------------------------------------------------------------------------

// Measure returns the preferred logical size. WebView is a "fill
// available" widget by default; callers wanting a fixed size should
// SetMinSize and place inside a layout that respects it. Returns 400×300
// fallback when there's no usable available rect (e.g. inside a flex
// container with grow=1 the layout will override anyway).
func (v *WebView) Measure(available qui.Size) qui.Size {
	w, h := available.W, available.H
	if w <= 0 {
		w = 400
	}
	if h <= 0 {
		h = 300
	}
	return qui.Size{W: w, H: h}
}

// Layout records the assigned rect and forwards to the Page so the
// CefBrowserHost::WasResized path runs. Layout is called by the
// framework whenever container layout settles; WebView watches for
// size changes only — same coordinates and the same DPR don't trigger
// a Resize. DPR-only changes (window dragged between displays) are
// picked up in Tick because the framebuffer callback skips Layout.
func (v *WebView) Layout(rect qui.Rect) {
	v.BaseWidget.Layout(rect)
	v.applyResize(int(rect.W+0.5), int(rect.H+0.5), v.effectiveScale())
}

// applyResize forwards (w, h, scale) to the Page unless they match the
// last-applied values, in which case it's a no-op. Safe to call from
// any goroutine.
func (v *WebView) applyResize(w, h int, scale float32) {
	if w <= 0 || h <= 0 {
		return
	}
	if scale <= 0 {
		scale = 1
	}
	v.mu.Lock()
	p := v.page
	if p == nil || (v.appliedW == w && v.appliedH == h && v.appliedScale == scale) {
		v.mu.Unlock()
		return
	}
	v.appliedW = w
	v.appliedH = h
	v.appliedScale = scale
	v.mu.Unlock()
	_ = p.Resize(w, h, scale)
}

// cursorKindToShape collapses CEF's richer cursor enum down to qui's
// small set. qui only exposes Default / Text / Crosshair / Hand /
// ResizeEW / ResizeNS at the moment, so most CEF-specific cursors
// (NESW/NWSE resize, Move, Wait, NotAllowed, …) fall back to a
// reasonable substitute or Default.
func cursorKindToShape(k CursorKind) qui.CursorShape {
	switch k {
	case CursorIBeam:
		return qui.CursorText
	case CursorHand:
		return qui.CursorHand
	case CursorCrosshair:
		return qui.CursorCrosshair
	case CursorResizeHorizontal:
		return qui.CursorResizeEW
	case CursorResizeVertical:
		return qui.CursorResizeNS
	case CursorResizeNESW, CursorResizeNWSE:
		// No diagonal-resize variant in qui — pick whichever axis the
		// kind primarily resizes. Both diagonals here read as "drag
		// corner", so an ambiguous horizontal-EW is fine until qui
		// grows the dedicated shapes.
		return qui.CursorResizeEW
	default:
		// CursorArrow, CursorMove, CursorWait, CursorNotAllowed →
		// default arrow.
		return qui.CursorDefault
	}
}

// effectiveScale resolves the DPR to render at. An explicit non-zero
// Config.DeviceScale wins (used by tests / headless callers); otherwise
// we derive from the owning Window's framebuffer/logical ratio so the
// page renders sharp on Retina displays. Falls back to 1 when neither
// is available (widget not yet attached to a Window).
func (v *WebView) effectiveScale() float32 {
	if v.cfg.DeviceScale > 0 {
		return v.cfg.DeviceScale
	}
	if win := v.Window(); win != nil {
		if s := win.DevicePixelRatio(); s > 0 {
			return s
		}
	}
	return 1
}

// HitTest claims the widget bounds. WebView is opaque to hit-testing
// (the web page is responsible for click feedback inside its area).
func (v *WebView) HitTest(p qui.Point) qui.Widget {
	if v.Bounds().Contains(p) {
		return v
	}
	return nil
}

// Tick drives the backend pump and acquires the latest frame. Returns
// the widget bounds when there's a frame on screen so the GL closure
// re-runs each frame (the framebuffer is cleared every End() — see
// media/VideoView for the same pattern).
//
// Two acquisition paths run in parallel: the GL path (AcquireFrame
// returns a texture) and the CPU path (cpuFrameProvider gives
// an image.RGBA) — they're mutually exclusive in practice
// because a backend picks one. Checking both lets the widget compile
// without per-platform branches.
func (v *WebView) Tick(_ time.Time) qui.Rect {
	v.mu.RLock()
	p := v.page
	v.mu.RUnlock()
	if p == nil {
		return qui.Rect{}
	}
	if b := activeBackend(); b != nil {
		b.Tick()
	}
	// Catch a display DPR change. SetFramebufferSizeCallback in qui's
	// Window only invalidates + steps — it does not InvalidateLayout —
	// so a window drag between a 1× and a 2× display won't reach
	// WebView.Layout. Probing here keeps CEF's device_scale_factor in
	// sync.
	bounds := v.Bounds()
	v.applyResize(int(bounds.W+0.5), int(bounds.H+0.5), v.effectiveScale())
	tex, w, h, dirty, hasNew := p.AcquireFrame()
	if hasNew {
		v.mu.Lock()
		v.texHandle = tex
		v.texW = w
		v.texH = h
		v.hasFrame = true
		v.mu.Unlock()
	} else {
		p.ReleaseFrame()
	}
	_ = dirty // returns full widget bounds; the GL path may use dirty for tighter scissor
	// CPU paint probe — once we have any CPU frame, mark hasFrame so
	// the widget keeps repainting (framebuffer is cleared every End()).
	if cpu, ok := p.(cpuFrameProvider); ok {
		if cpu.LatestCPUFrame() != nil {
			v.mu.Lock()
			v.hasFrame = true
			v.mu.Unlock()
		}
	}
	v.mu.RLock()
	has := v.hasFrame
	v.mu.RUnlock()
	if !has {
		return qui.Rect{}
	}
	return v.Bounds()
}

// Draw composites the latest frame onto the canvas.
//
// Three paint paths, picked in order:
//  1. CPU path: if the page is a cpuFrameProvider with a
//     ready image, fill background then canvas.DrawImage at widget
//     bounds. Works on every Canvas (including tests / non-GL).
//  2. GL path: hole-punch alpha=0 then queue a GL closure
//     that scissors to the canvas clip and blits the texture via
//     drawWebViewTexture (samplerRect shader for IOSurface).
//  3. Background-only: when no frame is available yet, just fill the
//     style background so the widget shows a clean rect.
func (v *WebView) Draw(canvas qui.Canvas) {
	v.mu.RLock()
	tex := v.texHandle
	texW, texH := v.texW, v.texH
	hasFrame := v.hasFrame
	p := v.page
	v.mu.RUnlock()

	bounds := v.Bounds()
	bg := v.Style().Background
	if bg == (qui.Color{}) {
		bg = qui.Color{R: 1, G: 1, B: 1, A: 1}
	}

	// CPU paint path — preferred when the backend produces one.
	// Skips GL entirely so it works on any Canvas type.
	if cpu, ok := p.(cpuFrameProvider); ok {
		if img := cpu.LatestCPUFrame(); img != nil {
			canvas.FillRect(bounds, bg)
			canvas.DrawImage(img, bounds)
			return
		}
	}

	gpu, ok := canvas.(qui.GPUCanvas)
	if !ok || !hasFrame || tex == 0 {
		canvas.FillRect(bounds, bg)
		return
	}

	// Opaque fill first (so a still-loading page shows the background
	// instead of whatever was on the framebuffer last frame), then
	// punch a transparent hole for the texture composite.
	canvas.FillRect(bounds, bg)
	canvas.FillRect(bounds, qui.Color{A: 0})

	clip := bounds
	if ca, isClip := canvas.(qui.ClipAware); isClip {
		clip = ca.ClipBounds()
		// Page-side hole-punch already established the dirty area;
		// downstream code only uses clip for scissor.
	}

	// Capture texW/texH for the closure.
	tw, th := texW, texH

	gpu.QueueGLDraw(func(state qui.GLState) {
		// Physical destination rect (logical → physical).
		scaleX, scaleY := float32(1), float32(1)
		if state.LogicalSize.W > 0 {
			scaleX = state.FramebufferSize.W / state.LogicalSize.W
		}
		if state.LogicalSize.H > 0 {
			scaleY = state.FramebufferSize.H / state.LogicalSize.H
		}
		physDst := qui.Rect{
			X: bounds.X * scaleX,
			Y: bounds.Y * scaleY,
			W: bounds.W * scaleX,
			H: bounds.H * scaleY,
		}
		// drawWebViewTexture is platform-specific: on darwin (with
		// webview_cef tag) it sets up the samplerRect shader for an
		// IOSurface-backed GL_TEXTURE_RECTANGLE; the stub for other
		// builds is a no-op. The clip is passed so the platform impl
		// can call qui.PhysicalScissor + gl.Scissor before drawing.
		drawWebViewTexture(state, tex, tw, th, physDst, clip)
	})

	if p != nil {
		p.ReleaseFrame()
	}
}

// Focusable reports whether the widget can take focus. WebView is
// focusable whenever it's enabled and has an open page.
func (v *WebView) Focusable() bool {
	if !v.Enabled() {
		return false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.page != nil
}

// SetFocused updates focus state and forwards to the page. Called by
// the focus system on focus enter/leave.
func (v *WebView) SetFocused(f bool) {
	v.mu.Lock()
	v.focused = f
	p := v.page
	v.mu.Unlock()
	if p != nil {
		_ = p.InjectFocus(f)
	}
	v.Invalidate()
}

// SetPreedit forwards IME composition to the focused web input and
// tracks the composing state so the next CharEvent can recognize itself
// as the commit. Implements qui.IMEClient.
func (v *WebView) SetPreedit(text string, cursor int) {
	p, err := v.requirePage()
	if err != nil {
		return
	}
	v.mu.Lock()
	v.preeditActive = text != ""
	v.mu.Unlock()
	_ = p.InjectIMEPreedit(text, cursor)
}

// CommitIME finalizes IME composition into the focused web input.
// Implements qui.IMEClient. Note: qui's macOS IME bridge does not
// actually call this — committed text is delivered as a sequence of
// CharEvents via GLFW. This entry point exists for callers and tests
// that want to drive composition programmatically.
func (v *WebView) CommitIME(text string) {
	p, err := v.requirePage()
	if err != nil {
		return
	}
	v.mu.Lock()
	v.preeditActive = false
	v.mu.Unlock()
	_ = p.InjectIMECommit(text)
}

// CaretRect returns the focused web input's caret rectangle in window
// coordinates, for the OS-IME candidate window. When CEF hasn't yet
// reported a composition range (first keystroke of a new composition,
// or the focused element doesn't accept IME), we fall back to a small
// rect near the WebView's bottom-left so the candidate panel anchors
// to the widget instead of jumping to the screen origin. Implements
// qui.IMEClient.
func (v *WebView) CaretRect() qui.Rect {
	v.mu.RLock()
	p := v.page
	v.mu.RUnlock()
	b := v.Bounds()
	if p != nil {
		r := p.CaretRect()
		if !r.IsEmpty() {
			r.X += b.X
			r.Y += b.Y
			return r
		}
	}
	// Fallback: a 1-px-wide rect at the WebView's interior so the
	// IME panel pops near our widget rather than the screen origin.
	// 16px down from the top is a reasonable guess at line-height.
	if b.IsEmpty() {
		return qui.Rect{}
	}
	return qui.Rect{X: b.X, Y: b.Y, W: 1, H: 16}
}

// Handle routes input events to the page. Returns true to consume —
// WebView consumes mouse and keyboard events that fall inside its
// bounds whenever it has a live page.
func (v *WebView) Handle(event qui.Event) bool {
	p, err := v.requirePage()
	if err != nil {
		return false
	}
	bounds := v.Bounds()
	switch e := event.(type) {
	case qui.MouseEvent:
		x := e.X - bounds.X
		y := e.Y - bounds.Y
		switch e.Type() {
		case qui.EventMouseMove:
			_ = p.InjectMouseMove(x, y, e.Mods)
			v.mu.Lock()
			v.hoverInside = true
			v.mu.Unlock()
			return true
		case qui.EventMouseDown:
			// DevTools / popup windows can steal Chromium's internal
			// focus even when qui's focus system still marks this widget
			// as focused. Re-arm focus on each click so key input/caret
			// work immediately after returning from those windows.
			_ = p.InjectFocus(true)
			now := nowFunc()
			v.mu.Lock()
			cc := 1
			if e.Button == v.lastClickButton &&
				!v.lastClickWhen.IsZero() &&
				now.Sub(v.lastClickWhen) <= multiClickMaxGap &&
				absf32(x-v.lastClickX) <= multiClickSlopPx &&
				absf32(y-v.lastClickY) <= multiClickSlopPx {
				cc = v.lastClickCount + 1
			}
			v.lastClickWhen = now
			v.lastClickX = x
			v.lastClickY = y
			v.lastClickButton = e.Button
			v.lastClickCount = cc
			v.downClickCount = cc
			v.mu.Unlock()
			_ = p.InjectMouseButton(x, y, e.Button, true, cc, e.Mods)
			return true
		case qui.EventMouseUp:
			v.mu.Lock()
			cc := v.downClickCount
			if cc <= 0 {
				cc = 1
			}
			v.downClickCount = 0
			v.mu.Unlock()
			_ = p.InjectMouseButton(x, y, e.Button, false, cc, e.Mods)
			return true
		case qui.EventScroll:
			_ = p.InjectScroll(x, y, e.DeltaX, e.DeltaY, e.Mods)
			return true
		case qui.EventMouseEnter:
			v.mu.Lock()
			v.hoverInside = true
			v.mu.Unlock()
			return false
		case qui.EventMouseLeave:
			// Send a synthesized "mouse left" by moving the cursor
			// outside the page so hover state clears in DOM.
			_ = p.InjectMouseMove(-1, -1, e.Mods)
			v.mu.Lock()
			v.hoverInside = false
			v.mu.Unlock()
			return false
		}
	case qui.KeyEvent:
		down := e.Type() == qui.EventKeyDown
		openDevToolsShortcut := down && (e.Key == qui.KeyF12 ||
			(e.Key == qui.KeyI && (e.Mods&qui.ModSuper) != 0 &&
				((e.Mods&qui.ModAlt) != 0 || (e.Mods&qui.ModShift) != 0)))
		if openDevToolsShortcut {
			_ = p.OpenDevTools()
			return true
		}
		_ = p.InjectKey(e.Key, e.ScanCode, down, e.Mods)
		return true
	case qui.CharEvent:
		// macOS IME commit path: when the OS commits composed text it
		// calls NSTextInputClient.insertText, which qui surfaces as a
		// CharEvent per rune. CEF's marked text is independent state —
		// we have to clear it explicitly or the chars get appended
		// after the lingering preedit. Route the first commit rune
		// through ImeCommitText (which replaces the marked text); any
		// subsequent runes in the same commit fall through to the
		// normal char path since the composition is now done.
		v.mu.Lock()
		active := v.preeditActive
		v.preeditActive = false
		v.mu.Unlock()
		if active {
			_ = p.InjectIMECommit(string(e.Rune))
		} else {
			_ = p.InjectChar(e.Rune, e.Mods)
		}
		return true
	case qui.FocusEvent:
		// FocusEvent is mainly informational; SetFocused handles the
		// state update.
		_ = e
		return false
	}
	return false
}

func absf32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
