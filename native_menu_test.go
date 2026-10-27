package qui

import "testing"

// TestActionScopeIsolation verifies that resetActionsForScope only
// clears its own scope — a SetMenuBar rebuild must not invalidate
// tray icon callbacks that live in a separate scope.
func TestActionScopeIsolation(t *testing.T) {
	resetActionsForScope(scopeMainMenu)

	var menuFired, trayFired int
	menuID := registerAction(func() { menuFired++ }, scopeMainMenu)
	trayScope := allocStatusItemScope()
	trayID := registerAction(func() { trayFired++ }, trayScope)
	defer resetActionsForScope(trayScope)

	if menuID == 0 || trayID == 0 || menuID == trayID {
		t.Fatalf("expected distinct non-zero ids; got menu=%d tray=%d", menuID, trayID)
	}

	// Simulate the main menu rebuild — only main-menu callbacks
	// should be dropped.
	resetActionsForScope(scopeMainMenu)

	// Tray action must still fire after the rebuild.
	postAction(trayID)
	postAction(menuID) // stale id — should be a no-op
	drainActions()

	if trayFired != 1 {
		t.Errorf("tray callback should have fired once; got %d", trayFired)
	}
	if menuFired != 0 {
		t.Errorf("menu callback was reset; got %d fires", menuFired)
	}

	// Re-registering under scopeMainMenu after reset works again.
	menuFired = 0
	menuID2 := registerAction(func() { menuFired++ }, scopeMainMenu)
	postAction(menuID2)
	drainActions()
	if menuFired != 1 {
		t.Errorf("re-registered menu callback should fire once; got %d", menuFired)
	}

	resetActionsForScope(scopeMainMenu)
}

func TestAllocStatusItemScopeUnique(t *testing.T) {
	a := allocStatusItemScope()
	b := allocStatusItemScope()
	if a == b {
		t.Fatalf("expected unique scopes; got %d twice", a)
	}
	if a < scopeStatusItemBase || b < scopeStatusItemBase {
		t.Fatalf("status item scopes must be ≥ %d; got %d, %d", scopeStatusItemBase, a, b)
	}
}
