//go:build !darwin || !cgo

package qui

// releaseNativeCursor is a no-op: off macOS the only backend (GLFW) owns
// its cursors per window (platformCursorReleaser).
func releaseNativeCursor(CursorShape) {}
