//go:build darwin && cgo

package qui

import (
	"testing"

	"github.com/qizhanchan/qui/internal/platform/darwinkeys"
)

func TestCaptureKeyCodeInverse(t *testing.T) {
	for _, k := range []Key{KeyEscape, KeyA, KeyTab, KeyF12, KeyGrave, KeyEnter} {
		code := darwinkeys.CodeFor(k)
		if code < 0 || darwinkeys.Map(code) != k {
			t.Errorf("darwinkeys.CodeFor(%v) = %#x", k, code)
		}
	}
	if darwinkeys.ModsMask(ModControl|ModAlt) != 6 || darwinkeys.Mods(6) != ModControl|ModAlt {
		t.Fatal("nsModsMask is not mapNSMods' inverse")
	}
}

func TestInjectCapturedKey(t *testing.T) {
	w, target, _ := rawFixture(t)
	w.injectCapturedKey(2, 0x37, false, darwinkeys.FlagCommand|darwinkeys.DeviceLCmd, ModSuper, "")   // Cmd down
	w.injectCapturedKey(0, 0x30, false, darwinkeys.FlagCommand|darwinkeys.DeviceLCmd, ModSuper, "\t") // Tab down
	w.injectCapturedKey(1, 0x30, false, darwinkeys.FlagCommand|darwinkeys.DeviceLCmd, ModSuper, "")   // Tab up
	w.injectCapturedKey(2, 0x37, false, 0, 0, "")                                                     // Cmd up
	w.injectCapturedKey(0, 0x00, false, 0, 0, "a")                                                    // a
	if len(target.keys) != 5 {
		t.Fatalf("keys = %+v", target.keys)
	}
	if target.keys[0].Key != KeyLeftSuper || target.keys[0].Type() != EventKeyDown ||
		target.keys[1].Key != KeyTab || target.keys[3].Type() != EventKeyUp {
		t.Fatalf("keys = %+v", target.keys)
	}
	if len(target.chars) != 1 || target.chars[0].Rune != 'a' || !target.chars[0].Raw {
		t.Fatalf("chars = %+v", target.chars)
	}
	if len(w.keyCapture.down) != 1 {
		t.Fatalf("down-key tracking = %v (only 'a' should be held)", w.keyCapture.down)
	}
}
