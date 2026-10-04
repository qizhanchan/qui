package webview

import "github.com/qizhanchan/qui"

// Backend abstracts the platform-specific web engine. v1 ships one
// implementation: CEF on macOS (backend_darwin.go behind build tag
// webview_cef). A future Linux backend would plug in via the same
// interface.
//
// Backends register themselves exactly once at init time. There is
// no plugin / runtime swap mechanism — the build-tag-gated init() in
// backend_darwin.go (or backend_other.go) decides.
type Backend interface {
	// Capabilities reports what this backend can do. Static for the
	// life of the process.
	Capabilities() Capabilities

	// NewPage creates a new web page bound to a CEF browser (or the
	// platform equivalent). The returned Page owns its lifecycle —
	// call Destroy when done.
	NewPage(cfg PageConfig) (Page, error)

	// Tick pumps the underlying engine's message loop and processes
	// queued cross-goroutine commands. Called from every WebView.Tick
	// in a paint pass; backend implementations should debounce
	// internally if their pump is expensive (CEF's
	// CefDoMessageLoopWork is documented as cheap-when-idle so this
	// design is fine for the v1 CEF backend).
	Tick()
}

// Page is an open web page. Methods are safe to call from any
// goroutine — non-trivial calls serialize onto the backend's UI thread
// via an internal queue.
//
// All input-injection coordinates are LOGICAL pixels relative to the
// top-left of the page (i.e. widget-local). HiDPI scaling is the
// backend's responsibility.
type Page interface {
	// Navigation
	LoadURL(url string) error
	LoadHTML(html, baseURL string) error
	Reload() error
	StopLoad() error
	GoBack() error
	GoForward() error

	// Resize tells the backend the page is now (w, h) logical pixels
	// at DPR `scale`. Triggers a synchronous (cheap) layout invalidation
	// and an async re-paint.
	Resize(w, h int, scale float32) error

	// AcquireFrame returns the latest texture the backend has rendered.
	// On macOS this is a GL_TEXTURE_RECTANGLE bound to an IOSurface;
	// callers must use a samplerRect-aware shader to sample it.
	// hasNew=false means the texture is unchanged since the last
	// AcquireFrame — caller may re-use the previous draw. dirty is the
	// union of rectangles repainted this frame (logical px, page-local).
	//
	// Texture lifetime: the backend owns it; the caller may read
	// (sample) until the next AcquireFrame / Resize / Destroy on this
	// Page, but must NOT delete it. ReleaseFrame signals end-of-read.
	AcquireFrame() (tex uint32, texW, texH int, dirty qui.Rect, hasNew bool)
	ReleaseFrame()

	// Input injection. Coordinates are widget-local logical pixels.
	InjectMouseMove(x, y float32, mods qui.Modifiers) error
	InjectMouseButton(x, y float32, btn qui.MouseButton, down bool, clickCount int, mods qui.Modifiers) error
	InjectScroll(x, y, dx, dy float32, mods qui.Modifiers) error
	InjectKey(key qui.Key, scancode int, down bool, mods qui.Modifiers) error
	InjectChar(r rune, mods qui.Modifiers) error
	OpenDevTools() error
	InjectFocus(focused bool) error
	InjectIMEPreedit(text string, cursor int) error
	InjectIMECommit(text string) error
	CaretRect() qui.Rect

	// JS bridge
	EvaluateJS(script string) (JSValue, error)
	RegisterHandler(name string, fn MessageHandler) error
	UnregisterHandler(name string) error

	// Listeners — all callbacks fire on the qui main goroutine.
	SetNavigationListener(func(NavigationEvent))
	SetLoadListener(func(LoadEvent))
	SetTitleListener(func(string))
	SetCursorListener(func(CursorKind))
	SetConsoleListener(func(level int, message string))

	// State queries.
	URL() string
	Title() string
	State() LoadState

	// Destroy releases backend resources. Idempotent; safe from any
	// goroutine. Pending callbacks may still fire after Destroy
	// returns — they no-op on a closed page.
	Destroy()
}

// currentBackend is the registered platform backend. nil on
// unsupported builds. Mirrors media/backend.go's pattern.
var currentBackend Backend

// setBackend is the entry point platform files call from init().
// Unexported on purpose — callers can't swap backends at runtime.
func setBackend(b Backend) { currentBackend = b }

// activeBackend returns the registered backend or nil. Public code
// consults this before calling backend methods so it can return
// ErrNotSupported gracefully.
func activeBackend() Backend { return currentBackend }

// Shutdown closes every open page, waits for the underlying engine to
// release its resources, and tears the runtime down. Required for a
// clean process exit when the backend is CEF on macOS — CEF spawns
// multiple helper processes + IPC threads and its `CloseBrowser` is
// async, so naive teardown (just `defer page.Destroy()`) leaves orphan
// state that hangs the OS exit path.
//
// Call from a Window.OnClose handler so it runs on the main goroutine
// before GLFW tears the window down:
//
//	window.OnClose(func() {
//	    view.Destroy()
//	    webview.Shutdown()
//	})
//
// Idempotent and safe to call when no backend is registered.
func Shutdown() error {
	b := activeBackend()
	if b == nil {
		return nil
	}
	if sh, ok := b.(interface{ shutdown() error }); ok {
		return sh.shutdown()
	}
	return nil
}
