//go:build darwin && cgo

package qui

import (
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

func nativeWindowMinimized(ns unsafe.Pointer) bool {
	return ns != nil && macos.WindowMiniaturized(ns)
}

func nativeAttachChild(parent, child unsafe.Pointer) bool {
	if parent == nil || child == nil {
		return false
	}
	macos.WindowAddChild(parent, child)
	return true
}

func nativeDetachChild(parent, child unsafe.Pointer) {
	if parent != nil && child != nil {
		macos.WindowRemoveChild(parent, child)
	}
}

func nativeBeginSheet(parent, child unsafe.Pointer) bool {
	if parent == nil || child == nil {
		return false
	}
	macos.WindowBeginSheet(parent, child)
	return true
}

func nativeEndSheet(parent, child unsafe.Pointer) {
	if parent != nil && child != nil {
		macos.WindowEndSheet(parent, child)
	}
}
