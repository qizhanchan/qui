//go:build !darwin && cgo && !js && !qui_nogl && (windows || qui_glfw)

package glfw

import (
	"image"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/qizhanchan/qui/internal/platform/contract"
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

// glfwImageCursor builds a GLFW cursor from the image's 1x rendition —
// GLFW takes images at one pixel per screen pixel here.
func glfwImageCursor(_ contract.CursorShape, img1x *image.NRGBA, hotX, hotY int) *glfw.Cursor {
	return glfw.CreateCursor(img1x, hotX, hotY)
}
