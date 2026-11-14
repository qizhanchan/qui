package qui

import "testing"

func TestSetAgentOverlayShortcut(t *testing.T) {
	defer func() {
		agentOverlayShortcut.Lock()
		agentOverlayShortcut.set = false
		agentOverlayShortcut.Unlock()
	}()
	if err := SetAgentOverlayShortcut("ctrl+alt+shift+a"); err != nil {
		t.Fatal(err)
	}
	k, m, err := defaultAgentOverlayShortcut()
	if err != nil || k != KeyA || m != ModControl|ModAlt|ModShift {
		t.Fatalf("got %v %v %v", k, m, err)
	}
	if err := SetAgentOverlayShortcut(""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := defaultAgentOverlayShortcut(); err == nil {
		t.Fatal("disabled shortcut still resolves")
	}
}
