package webview

import "github.com/qizhanchan/qui"

// LoadState mirrors the navigation lifecycle a page goes through.
// Updated by the backend's LoadHandler callbacks.
type LoadState int

const (
	// LoadStateIdle is the initial state and the state after Stop
	// or a navigation error.
	LoadStateIdle LoadState = iota
	// LoadStateLoading is set from OnLoadStart to OnLoadEnd. The
	// page may or may not have visible content yet.
	LoadStateLoading
	// LoadStateLoaded is set once the main frame finishes loading
	// (DOMContentLoaded equivalent).
	LoadStateLoaded
	// LoadStateError is set when OnLoadError fires for the main
	// frame. Inspect via LoadEvent.Err.
	LoadStateError
)

// NavigationKind classifies why a navigation happened. Used by
// NavigationEvent so listeners can distinguish a user clicking a link
// from history.replaceState() to from-script reloads.
type NavigationKind int

const (
	NavigationLinkClicked NavigationKind = iota
	NavigationFormSubmitted
	NavigationBackForward
	NavigationReload
	NavigationScript
	NavigationOther
)

// NavigationEvent fires before each navigation attempt. The backend
// dispatches it on the qui main goroutine, so listeners can mutate
// widget state freely.
type NavigationEvent struct {
	URL  string
	Kind NavigationKind
}

// LoadEvent fires on load start / end / error for the main frame.
// Err is non-nil only for LoadStateError.
type LoadEvent struct {
	State LoadState
	URL   string
	Err   error
}

// CursorKind reports what cursor CEF requested via OnCursorChange.
// Backends translate platform cursor IDs into this enum; the WebView
// widget forwards to GLFW SetCursor via the Window. A consumer that
// wants finer-grained cursors (resize handles, IBeam variants) can
// register its own cursor listener and override.
type CursorKind int

const (
	CursorArrow CursorKind = iota
	CursorIBeam
	CursorHand
	CursorCrosshair
	CursorMove
	CursorWait
	CursorNotAllowed
	CursorResizeHorizontal
	CursorResizeVertical
	CursorResizeNESW
	CursorResizeNWSE
)

// JSValue is the typed result of EvaluateJS. Only the zero or one
// of (Bool, Number, String, JSON) is set per the Kind tag. Null /
// undefined map to KindNull with all fields zero.
//
// We deliberately don't expose CefV8Value across the cgo boundary —
// the DevTools Runtime.evaluate protocol returns JSON, so JSON is
// what we surface here. Caller-side type discrimination is cheaper
// and avoids leaking V8 internals.
type JSValue struct {
	Kind   JSValueKind
	Bool   bool
	Number float64
	String string
	// JSON is the raw JSON encoding for objects/arrays. Caller
	// json.Unmarshal's into whatever target shape they want.
	JSON []byte
}

// JSValueKind tags JSValue.
type JSValueKind int

const (
	JSKindNull JSValueKind = iota
	JSKindBool
	JSKindNumber
	JSKindString
	JSKindJSON
)

// MessageHandler is the Go-side endpoint of a JS→Go bridge call.
// payload is the JSON-encoded argument the JS side passed (always a
// string at the JS API boundary — apps that want richer types JSON-
// encode at the call site). reply is sent back as the resolved value
// of the JS Promise; err rejects the Promise instead.
//
// Handlers may run on a worker goroutine, NOT the qui main goroutine.
// Don't touch widget state directly — post results back through the
// usual qui invalidation primitives if you need to update the UI.
type MessageHandler func(payload []byte) (reply []byte, err error)

// Config is the WebView widget's construction-time options. Fields
// left zero get sensible defaults.
type Config struct {
	// InitialURL is loaded on Open. Empty means "about:blank".
	InitialURL string
	// UserAgent overrides the default Chromium UA. Empty = default.
	UserAgent string
	// Transparent requests a transparent backing (alpha in the
	// IOSurface). Useful for overlays; defaults to opaque white.
	Transparent bool
	// DeviceScale forces a specific DPR. 0 = derive from the window
	// each frame (recommended). Set explicitly for headless tests.
	DeviceScale float32
}

// PageConfig is what the WebView widget hands to Backend.NewPage when
// it opens. Backends use it to size the initial CefBrowser.
type PageConfig struct {
	Width, Height int
	DeviceScale   float32
	Transparent   bool
	UserAgent     string
	InitialURL    string
}

// Capabilities lets callers (and tests) check what's supported
// without actually opening a page.
type Capabilities struct {
	WebGL    bool
	WebRTC   bool
	Video    bool // H.264 / AAC playback
	JSBridge bool
}

// rectAlias keeps the package import surface tidy — internal helpers
// take qui.Rect by name, exported APIs stick to qui.Rect explicitly.
type rectAlias = qui.Rect
