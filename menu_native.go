package qui

import "sync"

// Native application menu — the OS-provided top-level menu bar
// (macOS NSMenu, Windows HMENU, Linux GTK menu). Distinct from
// `MenuBar` in widgets_menu.go, which is an in-window widget.
//
// Both exist because macOS convention mandates the former (File /
// Edit / View / Window / Help in the system menu bar at screen top),
// while Windows/Linux commonly put menus inside the window. The
// design is: call App.SetMenuBar with the same NativeMenuBar on all
// platforms; on macOS the OS menu appears, elsewhere the in-window
// MenuBar widget is used.
//
// Menu actions run on the Go main goroutine, posted from the OS
// callback through actionQueue and drained by App.Run each tick.
// This keeps user code out of the Cocoa dispatch queue where
// re-entering the Go runtime is unsafe.

// NativeMenuBar is the top-level container (File, Edit, View, ...).
type NativeMenuBar struct {
	Menus []*NativeMenu
}

// NativeMenu is a single pull-down menu in the bar.
type NativeMenu struct {
	Title string
	Items []*NativeMenuItem
}

// NativeMenuItem is either an action item, a separator, or a
// submenu. Separator is true for a divider line (Label/Shortcut/
// Action ignored). Submenu non-nil turns this item into a nested
// menu (Action ignored).
type NativeMenuItem struct {
	Label     string
	Shortcut  string // e.g. "Cmd+S", "Ctrl+Shift+N"
	Action    func()
	Enabled   bool
	Checked   bool
	Separator bool
	Submenu   *NativeMenu

	// Populated by the backend after SetMenuBar so action dispatch
	// can find the Go-side callback. Opaque to callers.
	id int
}

// actionScope tags action IDs with the owning subsystem so a rebuild
// of one (e.g. SetMenuBar) doesn't invalidate callbacks for another
// (e.g. a tray icon's popup menu). Each scope owns its own slice of
// IDs and can be reset independently.
type actionScope int

const (
	// scopeMainMenu is the application's NSMenu / native menu bar.
	scopeMainMenu actionScope = 1
	// scopeStatusItemBase is the start of per-status-item scope IDs.
	// Each StatusItem allocates a fresh scope via nextStatusItemScope.
	scopeStatusItemBase actionScope = 1000
)

// actionRegistry maps backend action IDs (assigned sequentially) to
// the Go callback to invoke. Concurrent access from the OS UI thread
// is guarded by mu; the main goroutine reads via drain.
var (
	actionMu          sync.Mutex
	actionNextID      int
	actionByID        = map[int]func(){}
	actionScopeOfID   = map[int]actionScope{}
	actionIDsByScope  = map[actionScope][]int{}
	actionPending     []int
	nextStatusScopeID actionScope = scopeStatusItemBase
)

func registerAction(fn func(), scope actionScope) int {
	if fn == nil {
		return 0
	}
	actionMu.Lock()
	defer actionMu.Unlock()
	actionNextID++
	id := actionNextID
	actionByID[id] = fn
	actionScopeOfID[id] = scope
	actionIDsByScope[scope] = append(actionIDsByScope[scope], id)
	return id
}

// postAction is called from the backend (macOS NSMenuItem target, etc.)
// to queue an action. Safe to call from any thread; the main goroutine
// drains the queue once per loop iteration, so wake an idle loop.
func postAction(id int) {
	actionMu.Lock()
	actionPending = append(actionPending, id)
	actionMu.Unlock()
	WakeEventLoop()
}

// drainActions runs all queued menu actions on the caller's goroutine.
// App.Run calls this inside the frame loop so actions land on the
// main goroutine where they can safely touch the widget tree.
func drainActions() {
	actionMu.Lock()
	pending := actionPending
	actionPending = nil
	actionMu.Unlock()
	for _, id := range pending {
		actionMu.Lock()
		fn := actionByID[id]
		actionMu.Unlock()
		if fn != nil {
			fn()
		}
	}
}

// resetActionsForScope clears callbacks registered under a single
// scope. Other scopes are untouched so a SetMenuBar call doesn't
// disturb a tray icon's action callbacks, and vice versa.
func resetActionsForScope(scope actionScope) {
	actionMu.Lock()
	defer actionMu.Unlock()
	for _, id := range actionIDsByScope[scope] {
		delete(actionByID, id)
		delete(actionScopeOfID, id)
	}
	delete(actionIDsByScope, scope)
}

// allocStatusItemScope returns a fresh actionScope for one tray icon.
// IDs grow monotonically; the small overhead (one int per item ever
// created) is acceptable for the lifetime of a typical process.
func allocStatusItemScope() actionScope {
	actionMu.Lock()
	defer actionMu.Unlock()
	s := nextStatusScopeID
	nextStatusScopeID++
	return s
}
