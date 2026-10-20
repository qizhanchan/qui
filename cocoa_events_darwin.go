//go:build darwin && cgo

package qui

// Event-field conversions for the engine's own AppKit bridges (keyboard
// capture, raw keyboard mode, global hotkeys). The tables themselves live
// in internal/platform/darwinkeys, shared with the cocoa backend.

// #include <stdint.h>
import "C"
import "github.com/qizhanchan/qui/internal/platform/darwinkeys"

// Thin wrappers over darwinkeys, kept under the names the AppKit bridges
// in this package already use.

func mapNSGesturePhase(phase C.int) GesturePhase { return darwinkeys.GesturePhase(int(phase)) }
func mapNSMods(mods C.int) Modifiers             { return darwinkeys.Mods(int(mods)) }
func mapCocoaKey(code int) Key                   { return darwinkeys.Map(code) }
func cocoaModifierKeyDown(keyCode int, flags uint64) (bool, bool) {
	return darwinkeys.ModifierKeyDown(keyCode, flags)
}
func cocoaKeyCodeFor(k Key) int  { return darwinkeys.CodeFor(k) }
func nsModsMask(m Modifiers) int { return darwinkeys.ModsMask(m) }

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}
