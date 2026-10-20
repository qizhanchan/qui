// Package darwinkeys holds the macOS keyboard tables shared by the cocoa
// backend and the engine's own AppKit bridges (keyboard capture, raw
// keyboard mode, global hotkeys): virtual keycode ↔ Key, the normalized
// modifier mask the Objective-C side sends, NSEvent modifier-flag bits and
// NSEventPhase decoding. Pure data, no cgo, so it is testable anywhere.
package darwinkeys

import "github.com/qizhanchan/qui/internal/platform/contract"

// keyCodes maps macOS virtual keycodes to engine keys.
//
// The values are the kVK_* constants from Carbon's Events.h, written
// literally so this file needn't link Carbon. They are positions on the
// physical keyboard, NOT characters: 0x00 is the key labelled A on a US
// layout and stays "contract.KeyA" under Dvorak or AZERTY, which is what shortcut
// handling wants.
var keyCodes = map[int]contract.Key{
	0x00: contract.KeyA, 0x0B: contract.KeyB, 0x08: contract.KeyC, 0x02: contract.KeyD, 0x0E: contract.KeyE,
	0x03: contract.KeyF, 0x05: contract.KeyG, 0x04: contract.KeyH, 0x22: contract.KeyI, 0x26: contract.KeyJ,
	0x28: contract.KeyK, 0x25: contract.KeyL, 0x2E: contract.KeyM, 0x2D: contract.KeyN, 0x1F: contract.KeyO,
	0x23: contract.KeyP, 0x0C: contract.KeyQ, 0x0F: contract.KeyR, 0x01: contract.KeyS, 0x11: contract.KeyT,
	0x20: contract.KeyU, 0x09: contract.KeyV, 0x0D: contract.KeyW, 0x07: contract.KeyX, 0x10: contract.KeyY,
	0x06: contract.KeyZ,

	0x1D: contract.Key0, 0x12: contract.Key1, 0x13: contract.Key2, 0x14: contract.Key3, 0x15: contract.Key4,
	0x17: contract.Key5, 0x16: contract.Key6, 0x1A: contract.Key7, 0x1C: contract.Key8, 0x19: contract.Key9,

	0x1B: contract.KeyMinus, 0x18: contract.KeyEqual, 0x2C: contract.KeySlash,

	0x31: contract.KeySpace,
	0x24: contract.KeyEnter,
	0x4C: contract.KeyEnter, // keypad Enter
	0x30: contract.KeyTab,
	0x33: contract.KeyBackspace,
	0x35: contract.KeyEscape,
	0x75: contract.KeyDelete, // forward delete
	0x72: contract.KeyInsert, // Help; PC keyboards send Insert here
	0x73: contract.KeyHome,
	0x77: contract.KeyEnd,
	0x74: contract.KeyPageUp,
	0x79: contract.KeyPageDown,

	0x7B: contract.KeyLeft, 0x7C: contract.KeyRight, 0x7E: contract.KeyUp, 0x7D: contract.KeyDown,

	0x7A: contract.KeyF1, 0x78: contract.KeyF2, 0x63: contract.KeyF3, 0x76: contract.KeyF4,
	0x60: contract.KeyF5, 0x61: contract.KeyF6, 0x62: contract.KeyF7, 0x64: contract.KeyF8,
	0x65: contract.KeyF9, 0x6D: contract.KeyF10, 0x67: contract.KeyF11, 0x6F: contract.KeyF12,
	0x69: contract.KeyF13, 0x6B: contract.KeyF14, 0x71: contract.KeyF15, 0x6A: contract.KeyF16,
	0x40: contract.KeyF17, 0x4F: contract.KeyF18, 0x50: contract.KeyF19, 0x5A: contract.KeyF20,

	0x2B: contract.KeyComma, 0x2F: contract.KeyPeriod, 0x29: contract.KeySemicolon, 0x27: contract.KeyApostrophe,
	0x21: contract.KeyLeftBracket, 0x1E: contract.KeyRightBracket, 0x2A: contract.KeyBackslash, 0x32: contract.KeyGrave,

	// Modifier keys arrive through flagsChanged: (ModifierKeyDown).
	0x38: contract.KeyLeftShift, 0x3C: contract.KeyRightShift,
	0x3B: contract.KeyLeftControl, 0x3E: contract.KeyRightControl,
	0x3A: contract.KeyLeftAlt, 0x3D: contract.KeyRightAlt,
	0x37: contract.KeyLeftSuper, 0x36: contract.KeyRightSuper,
	0x39: contract.KeyCapsLock, 0x3F: contract.KeyFn, 0x6E: contract.KeyMenu,

	0x52: contract.KeyKP0, 0x53: contract.KeyKP1, 0x54: contract.KeyKP2, 0x55: contract.KeyKP3, 0x56: contract.KeyKP4,
	0x57: contract.KeyKP5, 0x58: contract.KeyKP6, 0x59: contract.KeyKP7, 0x5B: contract.KeyKP8, 0x5C: contract.KeyKP9,
	0x41: contract.KeyKPDecimal, 0x4B: contract.KeyKPDivide, 0x43: contract.KeyKPMultiply,
	0x4E: contract.KeyKPSubtract, 0x45: contract.KeyKPAdd, 0x51: contract.KeyKPEqual, 0x47: contract.KeyNumLock,
}

// NSEventModifierFlags bits read by ModifierKeyDown. The device-
// dependent bits (IOKit's NX_DEVICE*KEYMASK, present in the raw flags of
// every NSEvent) tell left from right, which the device-independent ones
// cannot — with both Shifts held, releasing one must report a contract.KeyUp for
// that key only.
const (
	FlagCapsLock = 1 << 16
	FlagShift    = 1 << 17
	FlagControl  = 1 << 18
	FlagOption   = 1 << 19
	FlagCommand  = 1 << 20
	FlagFunction = 1 << 23

	DeviceLCtl   = 0x0001
	DeviceLShift = 0x0002
	DeviceRShift = 0x0004
	DeviceLCmd   = 0x0008
	DeviceRCmd   = 0x0010
	DeviceLAlt   = 0x0020
	DeviceRAlt   = 0x0040
	DeviceRCtl   = 0x2000
)

// ModifierKeyDown decides whether a flagsChanged event for keyCode
// is the key going down or up, from the event's raw modifier flags. ok is
// false for key codes that are not modifier keys.
func ModifierKeyDown(keyCode int, flags uint64) (down, ok bool) {
	var bit uint64
	switch keyCode {
	case 0x38:
		bit = DeviceLShift
	case 0x3C:
		bit = DeviceRShift
	case 0x3B:
		bit = DeviceLCtl
	case 0x3E:
		bit = DeviceRCtl
	case 0x3A:
		bit = DeviceLAlt
	case 0x3D:
		bit = DeviceRAlt
	case 0x37:
		bit = DeviceLCmd
	case 0x36:
		bit = DeviceRCmd
	case 0x39:
		// Caps Lock reports its lock state, not the key: down = turned on.
		return flags&FlagCapsLock != 0, true
	case 0x3F:
		return flags&FlagFunction != 0, true
	default:
		return false, false
	}
	return flags&bit != 0, true
}

// Map returns the engine key for a macOS virtual keycode, or KeyUnknown.
func Map(code int) contract.Key {
	if k, ok := keyCodes[code]; ok {
		return k
	}
	return contract.KeyUnknown
}

// CodeFor inverts the keycode table (-1 when the key has no code).
func CodeFor(k contract.Key) int {
	if k == contract.KeyEnter {
		return 0x24 // Return, not keypad Enter
	}
	for code, key := range keyCodes {
		if key == k {
			return code
		}
	}
	return -1
}

// ModsMask is Mods' inverse: the 4-bit shift/control/option/command
// mask the C side compares.
func ModsMask(m contract.Modifiers) int {
	out := 0
	if m&contract.ModShift != 0 {
		out |= 1
	}
	if m&contract.ModControl != 0 {
		out |= 2
	}
	if m&contract.ModAlt != 0 {
		out |= 4
	}
	if m&contract.ModSuper != 0 {
		out |= 8
	}
	return out
}

// GesturePhase converts the NSEventPhase bit the ObjC side already
// reduced to a small integer into a GesturePhase. The reduction happens
// in C (quiPhaseOf) because NSEventPhase is a bitmask whose constants
// aren't worth re-declaring in Go.
func GesturePhase(phase int) contract.GesturePhase {
	switch phase {
	case 1:
		return contract.GesturePhaseBegan
	case 2:
		return contract.GesturePhaseChanged
	case 3:
		return contract.GesturePhaseEnded
	case 4:
		return contract.GesturePhaseCancelled
	case 5:
		return contract.GesturePhaseMomentum
	default:
		return contract.GesturePhaseNone
	}
}

// Mods converts the modifier bitmask the ObjC side normalized (see
// quiModsOf) into qui Modifiers.
func Mods(mods int) contract.Modifiers {
	var out contract.Modifiers
	if mods&1 != 0 {
		out |= contract.ModShift
	}
	if mods&2 != 0 {
		out |= contract.ModControl
	}
	if mods&4 != 0 {
		out |= contract.ModAlt
	}
	if mods&8 != 0 {
		out |= contract.ModSuper
	}
	return out
}
