package x11

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/contract"
)

func TestKeysymTableCoversShortcuts(t *testing.T) {
	for sym, want := range map[uint64]contract.Key{
		0x61: contract.KeyA, 0x7a: contract.KeyZ, 0x5a: contract.KeyZ, 0x31: contract.Key1,
		0xffbe: contract.KeyF1, 0xffc9: contract.KeyF12, 0xffca: contract.KeyF13,
		0xff0d: contract.KeyEnter, 0xff09: contract.KeyTab, 0xfe20: contract.KeyTab,
		0xff08: contract.KeyBackspace, 0xff1b: contract.KeyEscape, 0xffff: contract.KeyDelete,
		0xff51: contract.KeyLeft, 0xff53: contract.KeyRight, 0xff52: contract.KeyUp, 0xff54: contract.KeyDown,
		0xffeb: contract.KeyLeftSuper, 0xffb5: contract.KeyKP5,
	} {
		if got := mapKeysym(sym); got != want {
			t.Errorf("keysym %#x → %v, want %v", sym, got, want)
		}
	}
	if mapKeysym(0x1234567) != contract.KeyUnknown {
		t.Error("unknown keysym should map to KeyUnknown")
	}
}
