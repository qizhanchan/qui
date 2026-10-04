//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdint.h>

// Installer exported by ime_darwin.m. Takes the NSWindow pointer and
// a uintptr handle that we'll pass back to Go on each NSTextInputClient
// callback so the Go side can look up the right *Window.
void quiInstallIME(void* nsWindowPtr, uintptr_t handle);
*/
import "C"

import (
	"sync"
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

// quiIMESetPreedit is called by the Objective-C bridge whenever the
// OS's input method updates composition ("marked") text. text is the
// UTF-8 composition string; cursor is the rune index within it where
// the composition caret sits (from NSRange.location).
//
//export quiIMESetPreedit
func quiIMESetPreedit(id C.uintptr_t, text *C.char, cursor C.int) {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		return
	}
	w.SetPreedit(C.GoString(text), int(cursor))
}

// quiIMECaretRectCallback hands back the current caret rectangle in
// SCREEN coordinates. The Obj-C caller provides pointers for x/y/w/h
// and converts to NSRect itself. We take Qui's window-coord rect and
// convert to screen coords using the NSView / NSWindow geometry —
// done on the C side where we have access to NSView.convertRect.
//
//export quiIMECaretRect
func quiIMECaretRect(id C.uintptr_t, outX, outY, outW, outH *C.double) {
	w := lookupIMEWindow(uintptr(id))
	if w == nil {
		*outX, *outY, *outW, *outH = 0, 0, 0, 0
		return
	}
	// CaretRect is in viewport coordinates; the Obj-C side converts from
	// VIEW coordinates, which are window-logical. Multiply by zoom or the
	// candidate window drifts away from the caret as soon as the user
	// zooms — increasingly, since the error scales with the offset.
	r := w.CaretRect()
	zoom := C.double(w.Zoom())
	*outX = C.double(r.X) * zoom
	*outY = C.double(r.Y) * zoom
	*outW = C.double(r.W) * zoom
	*outH = C.double(r.H) * zoom
}

// installIMEBackend wires the current window's NSView to our
// NSTextInputClient override. Called once from Window.installCallbacks
// via ime_darwin.go's tag. The Obj-C side uses method_setImplementation
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
	C.quiInstallIME(nsWindow, C.uintptr_t(id))
}
