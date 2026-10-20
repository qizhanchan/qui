package web

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func TestDomKeyMapsLettersDigitsAndNamedKeys(t *testing.T) {
	for code, want := range map[string]contract.Key{
		"KeyA": contract.KeyA, "KeyV": contract.KeyV, "KeyZ": contract.KeyZ,
		"Digit0": contract.Key0, "Digit9": contract.Key9, "Numpad5": contract.KeyKP5,
		"Enter": contract.KeyEnter, "Backspace": contract.KeyBackspace, "ArrowLeft": contract.KeyLeft,
		"MetaLeft": contract.KeyLeftSuper, "F12": contract.KeyF12,
	} {
		if got := domKey(code); got != want {
			t.Errorf("domKey(%q) = %v, want %v", code, got, want)
		}
	}
	if domKey("Keyboard") != contract.KeyUnknown || domKey("Intl") != contract.KeyUnknown {
		t.Error("unknown codes must map to KeyUnknown")
	}
}

func TestUTF16ToRuneIndex(t *testing.T) {
	s := "a中😀b" // 😀 is two UTF-16 units
	for units, want := range map[int]int{0: 0, 1: 1, 2: 2, 4: 3, 5: 4} {
		if got := utf16ToRuneIndex(s, units); got != want {
			t.Errorf("utf16ToRuneIndex(%d) = %d, want %d", units, got, want)
		}
	}
}

func TestIsApplePlatform(t *testing.T) {
	for p, want := range map[string]bool{"macOS": true, "MacIntel": true, "iPhone": true, "Windows": false, "Linux x86_64": false, "": false} {
		if isApplePlatform(p) != want {
			t.Errorf("isApplePlatform(%q) != %v", p, want)
		}
	}
}
