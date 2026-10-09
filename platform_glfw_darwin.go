//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

int quiGLFWCursorAdopt(void* glfwCursor, void* nsCursor);
*/
import "C"

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

// glfwImageCursor builds a GLFW cursor for an image cursor. glfwCreateCursor
// maps one image pixel to one point, so a high-density image (scale > 1)
// would come out twice the size: the GLFW cursor is created from the 1x
// downsample, then its NSCursor is swapped for the full-resolution one
// (cursor_darwin.m). If the swap is refused the 1x cursor still works.
func glfwImageCursor(shape CursorShape, ic *imageCursor) *glfw.Cursor {
	cur := glfw.CreateCursor(ic.at1x(), int(ic.hotX), int(ic.hotY))
	if cur == nil || ic.scale == 1 {
		return cur
	}
	if ns := nativeNSCursor(shape); ns != nil {
		// go-gl's Cursor is struct{ data *C.GLFWcursor }.
		data := *(*unsafe.Pointer)(unsafe.Pointer(cur))
		C.quiGLFWCursorAdopt(data, ns)
	}
	return cur
}
