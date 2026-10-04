package webview

import "errors"

// ErrNotSupported is returned when no backend is available on the
// current platform / build configuration. The default build of qui
// (no `webview_cef` tag, or non-darwin) returns this from every
// Backend / Page method.
var ErrNotSupported = errors.New("webview: not supported on this platform / build")

// ErrClosed is returned from Page methods called after Destroy.
// Idempotent: callers can safely race Destroy against late events
// from goroutines they don't fully control (the page just no-ops).
var ErrClosed = errors.New("webview: page is closed")

// ErrLoadFailed wraps a navigation failure surfaced via
// CefLoadHandler::OnLoadError or equivalent. Inspect the wrapped
// error via errors.Unwrap for the platform error string.
var ErrLoadFailed = errors.New("webview: load failed")

// ErrNotInitialized is returned when the CEF runtime failed to
// start — typically the Chromium Embedded Framework was not
// installed at the expected path, or the helper executable is
// missing. The error message includes the diagnostic path.
var ErrNotInitialized = errors.New("webview: CEF runtime not initialized")
