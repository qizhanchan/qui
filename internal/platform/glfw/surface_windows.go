//go:build windows && cgo

package glfw

/*
#include <windows.h>
static void* quiModule(void) { return (void*)GetModuleHandleW(NULL); }
*/
import "C"

import (
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// NativeSurfaceTarget implements contract.NativeSurface: the Win32 window.
func (s glfwSurface) NativeSurfaceTarget() contract.SurfaceTarget {
	if s.win.handle == nil {
		return contract.SurfaceTarget{}
	}
	return contract.SurfaceTarget{
		HInstance: C.quiModule(),
		HWND:      unsafe.Pointer(s.win.handle.GetWin32Window()),
	}
}
