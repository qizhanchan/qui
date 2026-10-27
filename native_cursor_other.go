//go:build !darwin || !cgo

package qui

import "unsafe"

// releaseNativeCursor is a no-op: off macOS the only backend (GLFW) owns
// its cursors per window (platformCursorReleaser).
func releaseNativeCursor(CursorShape) {}

// nativeCursorForBackend implements contract.Host.NativeCursor; there is
// no shared native cursor object off macOS.
func nativeCursorForBackend(CursorShape) unsafe.Pointer { return nil }
