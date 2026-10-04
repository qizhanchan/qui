//go:build !darwin || !cgo

package qui

// Non-macOS (or no-cgo) fallback for Phase 5.2 IME integration.
// No native backend is installed — Chinese / Japanese / Korean input
// would require platform-specific work (X11 XIM / ibus / fcitx on
// Linux, Win32 IMM on Windows). The Go-side IMEClient machinery
// still works, so tests can simulate IME events via Window.SetPreedit
// / Window.CommitIME even without a real backend.

func (w *Window) installIMEBackend() {
	// No-op for now.
}
