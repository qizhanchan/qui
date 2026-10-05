//go:build !darwin || !cgo

package qui

import "unsafe"

func nativeWindowMinimized(unsafe.Pointer) bool  { return false }
func nativeAttachChild(_, _ unsafe.Pointer) bool { return false }
func nativeDetachChild(_, _ unsafe.Pointer)      {}
func nativeBeginSheet(_, _ unsafe.Pointer) bool  { return false }
func nativeEndSheet(_, _ unsafe.Pointer)         {}
