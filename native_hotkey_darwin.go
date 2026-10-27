//go:build darwin && cgo

package qui

import (
	"fmt"

	"github.com/qizhanchan/qui/internal/platform/darwinkeys"
	"github.com/qizhanchan/qui/internal/platform/macos"
)

func registerPlatformHotKey(id int, key Key, mods Modifiers) (func(), error) {
	code := darwinkeys.CodeFor(key)
	if code < 0 {
		return nil, fmt.Errorf("qui: global hotkey: key %v has no macOS key code", key)
	}
	ref := macos.HotKeyRegister(id, code, darwinkeys.ModsMask(mods))
	if ref == nil {
		return nil, fmt.Errorf("qui: global hotkey: the chord is taken or invalid")
	}
	return func() { macos.HotKeyUnregister(ref) }, nil
}
