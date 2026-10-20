//go:build darwin && cgo && !qui_nogl && qui_glfw

package glfw

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

int quiGLFWCursorAdopt(void* glfwCursor, void* nsCursor);
*/
import "C"

import (
	"image"
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/qizhanchan/qui/internal/platform/contract"
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

// glfwImageCursor builds a GLFW cursor for an image cursor. glfwCreateCursor
// maps one image pixel to one point, so a high-density image (scale > 1)
// would come out twice the size: the GLFW cursor is created from the 1x
// downsample, then its NSCursor is swapped for the full-resolution one
// (cursor_darwin.m). If the swap is refused the 1x cursor still works.
func glfwImageCursor(shape contract.CursorShape, img1x *image.NRGBA, hotX, hotY int) *glfw.Cursor {
	cur := glfw.CreateCursor(img1x, hotX, hotY)
	if cur == nil {
		return cur
	}
	if ns := contract.Host.NativeCursor(shape); ns != nil {
		// go-gl's Cursor is struct{ data *C.GLFWcursor }.
		data := *(*unsafe.Pointer)(unsafe.Pointer(cur))
		C.quiGLFWCursorAdopt(data, ns)
	}
	return cur
}
