//go:build darwin && cgo

package qui

import (
	"sync"

	"github.com/qizhanchan/qui/internal/platform/macos"
)

// imeRegistry maps uintptr handles to *Window. Cgo's pointer rules
// forbid passing Go pointers through C indefinitely, so we stash
// windows in a registry and pass around integer IDs instead.
var (
	imeRegistryMu sync.RWMutex
	imeRegistry           = map[uintptr]*Window{}
	imeNextID     uintptr = 1
)

func registerIMEWindow(w *Window) uintptr {
	imeRegistryMu.Lock()
	defer imeRegistryMu.Unlock()
	id := imeNextID
	imeNextID++
	imeRegistry[id] = w
	return id
}

func lookupIMEWindow(id uintptr) *Window {
	imeRegistryMu.RLock()
	defer imeRegistryMu.RUnlock()
	return imeRegistry[id]
}

// installIMEBackend wires the current window's NSView to our
// NSTextInputClient override. Called once from Window.installCallbacks.
// The Obj-C side (internal/platform/macos) uses method_setImplementation
// on _GLFWContentView's class to replace setMarkedText / unmarkText /
// firstRectForCharacterRange — one-time at process level, but every
// NSView instance carries a per-view associated-object pointing back
// to its owning Qui window (via the integer handle).
func (w *Window) installIMEBackend() {
	if w == nil || w.plat == nil {
		return
	}
	nsWindow := w.plat.nativeWindow()
	if nsWindow == nil {
		return
	}
	id := registerIMEWindow(w)
	w.nativeID = id
	macos.InstallIME(nsWindow, id)
}
