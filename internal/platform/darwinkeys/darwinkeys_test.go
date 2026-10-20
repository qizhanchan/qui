package darwinkeys

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func TestCocoaKeyTableCoversTheShortcutAlphabet(t *testing.T) {
	// These are the keycodes qui's own shortcuts depend on. A regression
	// here breaks Cmd+S / Cmd+Z / Cmd+C-V-X and the arrow keys — the
	// difference between "usable" and "looks fine, does nothing".
	want := map[int]contract.Key{
		0x00: contract.KeyA, 0x01: contract.KeyS, 0x06: contract.KeyZ, 0x07: contract.KeyX,
		0x08: contract.KeyC, 0x09: contract.KeyV, 0x0C: contract.KeyQ,
		0x24: contract.KeyEnter, 0x30: contract.KeyTab, 0x31: contract.KeySpace,
		0x33: contract.KeyBackspace, 0x35: contract.KeyEscape, 0x75: contract.KeyDelete,
		0x7B: contract.KeyLeft, 0x7C: contract.KeyRight, 0x7D: contract.KeyDown, 0x7E: contract.KeyUp,
	}
	for code, key := range want {
		if got := Map(code); got != key {
			t.Errorf("keycode 0x%02X mapped to %v, want %v", code, got, key)
		}
	}
}

func TestCocoaKeyTableIsInjectiveExceptForEnter(t *testing.T) {
	// Two keycodes mapping to the same contract.Key is almost always a copy-paste
	// slip in a 70-entry table. The one legitimate collision is Return and
	// keypad Enter, which qui deliberately does not distinguish.
	seen := map[contract.Key][]int{}
	for code, key := range keyCodes {
		seen[key] = append(seen[key], code)
	}
	for key, codes := range seen {
		if len(codes) == 1 {
			continue
		}
		if key == contract.KeyEnter && len(codes) == 2 {
			continue // Return + keypad Enter
		}
		t.Errorf("%v is produced by %d keycodes %#x; expected exactly one", key, len(codes), codes)
	}
}

func TestCocoaKeyTableRejectsUnknownCodes(t *testing.T) {
	for _, code := range []int{0xFF, -1} {
		if got := Map(code); got != contract.KeyUnknown {
			t.Errorf("keycode 0x%02X mapped to %v, want contract.KeyUnknown", code, got)
		}
	}
	// Modifier keycodes map to the modifier keys (they arrive through
	// flagsChanged:), never to a character key a shortcut could match.
	for code, want := range map[int]contract.Key{0x3B: contract.KeyLeftControl, 0x38: contract.KeyLeftShift, 0x37: contract.KeyLeftSuper, 0x3A: contract.KeyLeftAlt} {
		if got := Map(code); got != want {
			t.Errorf("keycode 0x%02X mapped to %v, want %v", code, got, want)
		}
	}
}

func TestCocoaModifierKeysUpDown(t *testing.T) {
	// Left Shift pressed while right Shift is already held: the device bit
	// for the LEFT key decides, not the shared NSEventModifierFlagShift.
	flags := uint64(FlagShift | DeviceRShift | DeviceLShift)
	if down, ok := ModifierKeyDown(0x38, flags); !ok || !down {
		t.Fatalf("left shift press = %v,%v", down, ok)
	}
	// Left released, right still held: Shift flag still set, left bit gone.
	flags = uint64(FlagShift | DeviceRShift)
	if down, _ := ModifierKeyDown(0x38, flags); down {
		t.Fatal("left shift release reported as down")
	}
	if down, _ := ModifierKeyDown(0x3C, flags); !down {
		t.Fatal("right shift reported up while held")
	}
	if down, _ := ModifierKeyDown(0x37, uint64(FlagCommand|DeviceLCmd)); !down {
		t.Fatal("left command")
	}
	if down, _ := ModifierKeyDown(0x3E, uint64(FlagControl|DeviceRCtl)); !down {
		t.Fatal("right control")
	}
	if down, _ := ModifierKeyDown(0x39, FlagCapsLock); !down {
		t.Fatal("caps lock on must be a contract.KeyDown")
	}
	if down, _ := ModifierKeyDown(0x39, 0); down {
		t.Fatal("caps lock off must be a contract.KeyUp")
	}
	if _, ok := ModifierKeyDown(0x00, 0); ok {
		t.Fatal("A is not a modifier key")
	}
	for code, want := range map[int]contract.Key{0x38: contract.KeyLeftShift, 0x36: contract.KeyRightSuper, 0x39: contract.KeyCapsLock,
		0x32: contract.KeyGrave, 0x2B: contract.KeyComma, 0x52: contract.KeyKP0, 0x69: contract.KeyF13} {
		if got := Map(code); got != want {
			t.Errorf("Map(%#x) = %v, want %v", code, got, want)
		}
	}
}
