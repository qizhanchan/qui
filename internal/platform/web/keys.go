package web

import (
	"strings"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

// Pure lookups, kept out of the js-tagged file so they are unit-tested on
// the host.

// domKey maps KeyboardEvent.code (physical key, layout-independent — the
// same thing GLFW key codes and macOS virtual key codes describe) to Key.
func domKey(code string) contract.Key {
	if k, ok := domKeys[code]; ok {
		return k
	}
	if strings.HasPrefix(code, "Key") && len(code) == 4 {
		if c := code[3]; c >= 'A' && c <= 'Z' {
			return contract.KeyA + contract.Key(c-'A')
		}
	}
	if strings.HasPrefix(code, "Digit") && len(code) == 6 {
		if c := code[5]; c >= '0' && c <= '9' {
			return contract.Key0 + contract.Key(c-'0')
		}
	}
	if strings.HasPrefix(code, "Numpad") && len(code) == 7 {
		if c := code[6]; c >= '0' && c <= '9' {
			return contract.KeyKP0 + contract.Key(c-'0')
		}
	}
	return contract.KeyUnknown
}

var domKeys = map[string]contract.Key{
	"Space": contract.KeySpace, "Enter": contract.KeyEnter, "NumpadEnter": contract.KeyEnter, "Escape": contract.KeyEscape,
	"ArrowLeft": contract.KeyLeft, "ArrowRight": contract.KeyRight, "ArrowUp": contract.KeyUp, "ArrowDown": contract.KeyDown,
	"Home": contract.KeyHome, "End": contract.KeyEnd, "Backspace": contract.KeyBackspace, "Tab": contract.KeyTab,
	"Delete": contract.KeyDelete, "Insert": contract.KeyInsert, "PageUp": contract.KeyPageUp, "PageDown": contract.KeyPageDown,
	"F1": contract.KeyF1, "F2": contract.KeyF2, "F3": contract.KeyF3, "F4": contract.KeyF4, "F5": contract.KeyF5, "F6": contract.KeyF6,
	"F7": contract.KeyF7, "F8": contract.KeyF8, "F9": contract.KeyF9, "F10": contract.KeyF10, "F11": contract.KeyF11, "F12": contract.KeyF12,
	"F13": contract.KeyF13, "F14": contract.KeyF14, "F15": contract.KeyF15, "F16": contract.KeyF16, "F17": contract.KeyF17,
	"F18": contract.KeyF18, "F19": contract.KeyF19, "F20": contract.KeyF20,
	"Minus": contract.KeyMinus, "Equal": contract.KeyEqual, "Slash": contract.KeySlash, "Comma": contract.KeyComma,
	"Period": contract.KeyPeriod, "Semicolon": contract.KeySemicolon, "Quote": contract.KeyApostrophe,
	"BracketLeft": contract.KeyLeftBracket, "BracketRight": contract.KeyRightBracket,
	"Backslash": contract.KeyBackslash, "Backquote": contract.KeyGrave,
	"ShiftLeft": contract.KeyLeftShift, "ShiftRight": contract.KeyRightShift,
	"ControlLeft": contract.KeyLeftControl, "ControlRight": contract.KeyRightControl,
	"AltLeft": contract.KeyLeftAlt, "AltRight": contract.KeyRightAlt,
	"MetaLeft": contract.KeyLeftSuper, "MetaRight": contract.KeyRightSuper, "OSLeft": contract.KeyLeftSuper, "OSRight": contract.KeyRightSuper,
	"CapsLock": contract.KeyCapsLock, "ContextMenu": contract.KeyMenu, "NumLock": contract.KeyNumLock,
	"NumpadDecimal": contract.KeyKPDecimal, "NumpadDivide": contract.KeyKPDivide, "NumpadMultiply": contract.KeyKPMultiply,
	"NumpadSubtract": contract.KeyKPSubtract, "NumpadAdd": contract.KeyKPAdd, "NumpadEqual": contract.KeyKPEqual,
}

// utf16ToRuneIndex converts a DOM string offset (UTF-16 code units) into
// a rune index.
func utf16ToRuneIndex(s string, units int) int {
	n, u := 0, 0
	for _, r := range s {
		if u >= units {
			break
		}
		u++
		if r >= 0x10000 {
			u++
		}
		n++
	}
	return n
}

// isApplePlatform classifies a navigator platform string.
func isApplePlatform(platform string) bool {
	p := strings.ToLower(platform)
	return strings.Contains(p, "mac") || strings.Contains(p, "iphone") ||
		strings.Contains(p, "ipad") || strings.Contains(p, "ipod")
}
