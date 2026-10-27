package qui

import (
	"errors"
	"sync"
)

// ErrGlobalHotKeyUnsupported is returned where the platform has no
// global hotkeys (everything but macOS today).
var ErrGlobalHotKeyUnsupported = errors.New("qui: global hotkeys are not supported on this platform")

var (
	hotKeyMu   sync.Mutex
	hotKeyNext int
	hotKeyFns  = map[int]func(){}
)

// RegisterGlobalHotKey calls fn whenever shortcut (ParseShortcut syntax,
// e.g. "Ctrl+Alt+Shift+Escape") is pressed, whichever app is frontmost.
// It needs no permission prompt on macOS (Carbon hotkeys, not an event
// tap), and the chord is consumed: the frontmost app does not see it.
// fn runs on the UI thread. Call from the UI thread; the returned
// function unregisters (idempotent).
func RegisterGlobalHotKey(shortcut string, fn func()) (unregister func(), err error) {
	assertProcessUIThread("RegisterGlobalHotKey")
	if fn == nil {
		return nil, errors.New("qui: RegisterGlobalHotKey: nil callback")
	}
	key, mods, err := ParseShortcut(shortcut)
	if err != nil {
		return nil, err
	}
	hotKeyMu.Lock()
	hotKeyNext++
	id := hotKeyNext
	hotKeyFns[id] = fn
	hotKeyMu.Unlock()
	release, err := registerPlatformHotKey(id, key, mods)
	if err != nil {
		hotKeyMu.Lock()
		delete(hotKeyFns, id)
		hotKeyMu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			hotKeyMu.Lock()
			delete(hotKeyFns, id)
			hotKeyMu.Unlock()
		})
	}, nil
}

// fireHotKey runs the callback for a platform hotkey id (UI thread).
func fireHotKey(id int) {
	hotKeyMu.Lock()
	fn := hotKeyFns[id]
	hotKeyMu.Unlock()
	if fn != nil {
		fn()
	}
}
