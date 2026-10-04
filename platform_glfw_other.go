//go:build !darwin || !cgo

package qui

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// glfwNativeGestures is false here: no native gesture source is wired up
// on these platforms yet (Windows would need a WndProc subclass over
// WM_POINTER, Wayland the zwp_pointer_gestures_v1 protocol GLFW 3.3
// doesn't expose). Pinch still works via the Ctrl+wheel synthesis in
// gesture.go — see gesture_other.go.
const glfwNativeGestures = false

// glfwNativeWindow has no portable equivalent: the native handle is only
// meaningful to a platform-specific bridge, and none exist here yet.
func glfwNativeWindow(_ *glfw.Window) unsafe.Pointer { return nil }

// installGestures is a no-op: no native gesture source exists on these
// platforms yet. Pinch still reaches widgets through the Ctrl+wheel
// synthesis in gesture.go, so this is a degradation (no scroll phase, no
// two-finger rotation) rather than a hole.
func (w *glfwWindow) installGestures() {}
