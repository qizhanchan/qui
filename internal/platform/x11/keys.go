package x11

import "github.com/qizhanchan/qui/internal/platform/contract"

// Keysym → Key. The keysym is read with index 0 (XLookupKeysym(ev, 0)),
// the unshifted symbol, so it names the physical key the way GLFW key
// codes and macOS virtual keycodes do: Shift+1 is still Key1. Values are
// the XK_* constants from X11/keysymdef.h, written literally so this
// table is pure Go and testable without Xlib.
func mapKeysym(sym uint64) contract.Key {
	switch {
	case sym >= 0x61 && sym <= 0x7a: // XK_a..XK_z
		return contract.KeyA + contract.Key(sym-0x61)
	case sym >= 0x41 && sym <= 0x5a: // XK_A..XK_Z (some layouts report these)
		return contract.KeyA + contract.Key(sym-0x41)
	case sym >= 0x30 && sym <= 0x39: // XK_0..XK_9
		return contract.Key0 + contract.Key(sym-0x30)
	case sym >= 0xffbe && sym <= 0xffc9: // XK_F1..XK_F12
		return contract.KeyF1 + contract.Key(sym-0xffbe)
	case sym >= 0xffca && sym <= 0xffd1: // XK_F13..XK_F20
		return contract.KeyF13 + contract.Key(sym-0xffca)
	case sym >= 0xffb0 && sym <= 0xffb9: // XK_KP_0..XK_KP_9
		return contract.KeyKP0 + contract.Key(sym-0xffb0)
	}
	if k, ok := keysyms[sym]; ok {
		return k
	}
	return contract.KeyUnknown
}

var keysyms = map[uint64]contract.Key{
	0x0020: contract.KeySpace,
	0xff0d: contract.KeyEnter, 0xff8d: contract.KeyEnter, // Return, KP_Enter
	0xff1b: contract.KeyEscape,
	0xff51: contract.KeyLeft, 0xff52: contract.KeyUp, 0xff53: contract.KeyRight, 0xff54: contract.KeyDown,
	0xff50: contract.KeyHome, 0xff57: contract.KeyEnd,
	0xff55: contract.KeyPageUp, 0xff56: contract.KeyPageDown,
	0xff08: contract.KeyBackspace,
	0xff09: contract.KeyTab, 0xfe20: contract.KeyTab, // Tab, ISO_Left_Tab (Shift+Tab)
	0xffff: contract.KeyDelete, 0xff63: contract.KeyInsert,
	0x002d: contract.KeyMinus, 0x003d: contract.KeyEqual, 0x002f: contract.KeySlash,
	0x002c: contract.KeyComma, 0x002e: contract.KeyPeriod, 0x003b: contract.KeySemicolon,
	0x0027: contract.KeyApostrophe, 0x005b: contract.KeyLeftBracket, 0x005d: contract.KeyRightBracket,
	0x005c: contract.KeyBackslash, 0x0060: contract.KeyGrave,
	0xffe1: contract.KeyLeftShift, 0xffe2: contract.KeyRightShift,
	0xffe3: contract.KeyLeftControl, 0xffe4: contract.KeyRightControl,
	0xffe9: contract.KeyLeftAlt, 0xffea: contract.KeyRightAlt,
	0xffe7: contract.KeyLeftAlt, 0xffe8: contract.KeyRightAlt, // Meta_L/R
	0xffeb: contract.KeyLeftSuper, 0xffec: contract.KeyRightSuper,
	0xffe5: contract.KeyCapsLock, 0xff67: contract.KeyMenu, 0xff7f: contract.KeyNumLock,
	0xffae: contract.KeyKPDecimal, 0xffaf: contract.KeyKPDivide, 0xffaa: contract.KeyKPMultiply,
	0xffad: contract.KeyKPSubtract, 0xffab: contract.KeyKPAdd, 0xffbd: contract.KeyKPEqual,
	// Keypad with Num Lock off reports navigation keysyms at index 0.
	0xff95: contract.KeyKP7, 0xff97: contract.KeyKP8, 0xff9a: contract.KeyKP9,
	0xff96: contract.KeyKP4, 0xff9d: contract.KeyKP5, 0xff98: contract.KeyKP6,
	0xff9c: contract.KeyKP1, 0xff99: contract.KeyKP2, 0xff9b: contract.KeyKP3,
	0xff9e: contract.KeyKP0, 0xff9f: contract.KeyKPDecimal,
}
