//go:build darwin && cgo

package qui

import (
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

func platformSetTitlebarStyle(ns unsafe.Pointer, s TitlebarStyle) bool {
	mode := 0
	switch s {
	case TitlebarOverlay:
		mode = 1
	case TitlebarOverlayTall:
		mode = 2
	}
	macos.TitlebarSetOverlay(ns, mode)
	return true
}

func platformTitlebarInsets(ns unsafe.Pointer) Insets {
	left, right, top := macos.TitlebarInsets(ns)
	return Insets{Top: float32(top), Left: float32(left), Right: float32(right)}
}

func platformBeginWindowDrag(ns unsafe.Pointer) bool {
	return macos.WindowBeginDrag(ns)
}

func platformToggleMaximize(ns unsafe.Pointer) bool {
	macos.WindowToggleZoom(ns)
	return true
}
