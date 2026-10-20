//go:build darwin && cgo

package cocoa

// Event-field conversions shared by every macOS bridge that receives
// NSEvents: the cocoa backend, the GLFW gesture swizzle (-tags qui_glfw)
// and the keyboard capture/raw modes. Kept out of any one backend's file
// so building without that backend doesn't take them along.

// #include <stdint.h>
import "C"
import (
	"github.com/qizhanchan/qui/internal/platform/contract"
	"github.com/qizhanchan/qui/internal/platform/darwinkeys"
)

// Thin wrappers over darwinkeys, kept under the names the AppKit bridges
// in this package already use.

func mapNSGesturePhase(phase C.int) contract.GesturePhase { return darwinkeys.GesturePhase(int(phase)) }
func mapNSMods(mods C.int) contract.Modifiers             { return darwinkeys.Mods(int(mods)) }
func mapCocoaKey(code int) contract.Key                   { return darwinkeys.Map(code) }
func cocoaModifierKeyDown(keyCode int, flags uint64) (bool, bool) {
	return darwinkeys.ModifierKeyDown(keyCode, flags)
}
