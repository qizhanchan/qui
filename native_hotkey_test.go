package qui

import "testing"

func TestGlobalHotKeyRejectsBadInput(t *testing.T) {
	if _, err := RegisterGlobalHotKey("Ctrl+Nope", func() {}); err == nil {
		t.Fatal("bad shortcut accepted")
	}
	if _, err := RegisterGlobalHotKey("Ctrl+Alt+Shift+Escape", nil); err == nil {
		t.Fatal("nil callback accepted")
	}
}

func TestFireHotKeyDispatchesByID(t *testing.T) {
	hits := 0
	hotKeyMu.Lock()
	hotKeyFns[-7] = func() { hits++ }
	hotKeyMu.Unlock()
	defer func() {
		hotKeyMu.Lock()
		delete(hotKeyFns, -7)
		hotKeyMu.Unlock()
	}()
	fireHotKey(-7)
	fireHotKey(-8) // unknown: ignored
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}
}
