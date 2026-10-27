//go:build darwin && cgo

package qui

import (
	"unsafe"

	"github.com/qizhanchan/qui/internal/platform/darwinkeys"
	"github.com/qizhanchan/qui/internal/platform/macos"
)

// macHost receives the callbacks of the AppKit service bridges in
// internal/platform/macos. Window-scoped callbacks are looked up through
// the IME registry: the hooks live on the window's content view, keyed by
// the handle installIMEBackend registered.
type macHost struct{}

func init() { macos.SetHost(macHost{}) }

func (macHost) MenuAction(id int)  { postAction(id) }
func (macHost) HotKeyFired(id int) { fireHotKey(id) }

func (macHost) IMESetPreedit(handle uintptr, text string, cursor int) {
	if w := lookupIMEWindow(handle); w != nil {
		w.SetPreedit(text, cursor)
	}
}

// IMECaretRect hands back the caret in VIEW coordinates, which are
// window-logical: CaretRect is in viewport coordinates, so multiply by zoom
// or the candidate window drifts away from the caret once the user zooms.
func (macHost) IMECaretRect(handle uintptr) (x, y, w, h float64) {
	win := lookupIMEWindow(handle)
	if win == nil {
		return 0, 0, 0, 0
	}
	r := win.CaretRect()
	zoom := float64(win.Zoom())
	return float64(r.X) * zoom, float64(r.Y) * zoom, float64(r.W) * zoom, float64(r.H) * zoom
}

func (macHost) RawKeyboardMode(handle uintptr, keyCode, mods int) int {
	w := lookupIMEWindow(handle)
	if w == nil {
		return 0
	}
	return w.rawKeyMode(darwinkeys.Map(keyCode), darwinkeys.Mods(mods))
}

func (macHost) RawKeyboardActive(handle uintptr) bool {
	w := lookupIMEWindow(handle)
	// The escape chord's own keystroke stays out of the input method even
	// though handling its KeyDown may just have turned raw mode off.
	return w != nil && (w.rawKeyboardTarget() != nil || w.raw.escDown != KeyUnknown)
}

func (macHost) RawKeyboardChars(handle uintptr, text string, mods int) {
	if w := lookupIMEWindow(handle); w != nil {
		w.onRawChars(text, darwinkeys.Mods(mods))
	}
}

func (macHost) AcceleratorClaims(handle uintptr, keyCode, mods int) bool {
	w := lookupIMEWindow(handle)
	if w == nil || w.rawKeyboardTarget() != nil {
		return false
	}
	key, m := darwinkeys.Map(keyCode), darwinkeys.Mods(mods)
	// A menu shortcut the app keeps for itself (KeyToWindow), any
	// modifiers; otherwise a Command chord bound as an accelerator.
	return menuLeavesKey(key, m) || m&ModSuper != 0 && w.acceleratorClaims(key, m)
}

// TapKeyEvent runs on the tap thread: hand the key to the UI thread, in
// order, and return immediately.
func (macHost) TapKeyEvent(handle uintptr, kind, keyCode int, repeat bool, flags uint64, mods int, chars string, seq uint64) {
	w := lookupIMEWindow(handle)
	if w == nil {
		macos.TapAck(seq)
		return
	}
	md := darwinkeys.Mods(mods)
	w.PostPriorityJob(func() {
		w.injectCapturedKey(kind, keyCode, repeat, flags, md, chars)
		macos.TapAck(seq)
	})
}

// rawKeyCapturedHook hands the cocoa backend's event pump the C predicate
// it consults before re-sending a swallowed Command-chord keyUp
// (contract.Host.RawKeyCaptured).
func rawKeyCapturedHook() unsafe.Pointer { return macos.RawKeyCapturedHook() }

func currentUIThreadID() uint64 { return macos.CurrentThreadID() }
