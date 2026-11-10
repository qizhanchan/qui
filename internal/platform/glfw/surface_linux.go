//go:build linux && cgo && qui_glfw

package glfw

import (
	"unsafe"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/qizhanchan/qui/internal/platform/contract"
)

// NativeSurfaceTarget implements contract.NativeSurface: the X11 window.
func (s glfwSurface) NativeSurfaceTarget() contract.SurfaceTarget {
	if s.win.handle == nil {
		return contract.SurfaceTarget{}
	}
	return contract.SurfaceTarget{
		XDisplay: unsafe.Pointer(glfw.GetX11Display()),
		XWindow:  uint64(s.win.handle.GetX11Window()),
	}
}
