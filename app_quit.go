package qui

import "sync/atomic"

// quitRequest is the process-wide handler for an OS-level quit request
// (macOS: Cmd+Q, Dock → Quit, logout), set by App.OnQuitRequest.
var quitRequest atomic.Pointer[func()]

// OnQuitRequest intercepts the OS asking the app to quit. Without a
// handler macOS's -[NSApplication terminate:] ends the process right away
// (exit(), no deferred Go code runs); with one, fn runs on the UI thread
// instead and the app shuts down itself, typically by calling App.Quit
// so Run returns. nil restores the default. Only macOS reports quit
// requests; elsewhere this is a no-op.
func (a *App) OnQuitRequest(fn func()) {
	if fn == nil {
		quitRequest.Store(nil)
		return
	}
	quitRequest.Store(&fn)
}

// runQuitRequest calls the registered handler and reports whether one
// handled the request (true: cancel the OS-level termination).
func runQuitRequest() bool {
	fn := quitRequest.Load()
	if fn == nil {
		return false
	}
	(*fn)()
	return true
}
