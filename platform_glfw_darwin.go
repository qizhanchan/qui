//go:build darwin && cgo

package qui

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// glfwNativeGestures reports that this build has a real multi-touch
// source: the darwin bridge in gesture_darwin.m installs the NSEvent
// gesture selectors GLFW itself lacks.
const glfwNativeGestures = true

// glfwNativeWindow exposes the NSWindow behind a GLFW window, for the
// native bridges that must reach past the seam (IME, native fullscreen,
// the gesture bridge).
func glfwNativeWindow(handle *glfw.Window) unsafe.Pointer {
	if handle == nil {
		return nil
	}
	return unsafe.Pointer(handle.GetCocoaWindow())
}
