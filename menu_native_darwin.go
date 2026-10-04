//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>

void quiResetMenuBar(void);
void* quiBeginSubmenu(const char* title);
void quiEndSubmenu(void* parent, void* submenu);
void quiAddItem(void* parent, const char* title, const char* keyEq, int mods, int actionID, int enabled, int checked);
void quiAddSeparator(void* parent);
void quiInstallMainMenu(void);
*/
import "C"

import "unsafe"

//export quiMenuActionCallback
func quiMenuActionCallback(id C.int) {
	postAction(int(id))
}

// SetMenuBar installs a native NSMenu as the application menu. Must
// be called on the main goroutine (the one that created the App).
// Replaces any previously installed menu.
func (a *App) SetMenuBar(bar *NativeMenuBar) {
	if a == nil || bar == nil {
		return
	}
	resetActionsForScope(scopeMainMenu)
	C.quiResetMenuBar()
	for _, m := range bar.Menus {
		installMenu(m)
	}
	C.quiInstallMainMenu()
}

// installMenu builds a top-level NSMenu for the main menu bar and
// attaches it to g_mainMenu via quiEndSubmenu(nil, …).
func installMenu(m *NativeMenu) {
	submenu := buildSubmenu(m, scopeMainMenu)
	C.quiEndSubmenu(nil, submenu)
}

// buildSubmenu constructs an NSMenu populated with items from m.
// Returns a retained Objective-C pointer; the caller must consume the
// retain by handing it to quiEndSubmenu (nests it inside another menu
// or attaches to g_mainMenu) or quiStatusItemSetMenu (gives it to a
// tray icon). All action callbacks land under the supplied scope.
func buildSubmenu(m *NativeMenu, scope actionScope) unsafe.Pointer {
	cTitle, freeTitle := cStr(m.Title)
	defer freeTitle()
	submenu := C.quiBeginSubmenu(cTitle)
	buildMenuItems(unsafe.Pointer(submenu), m.Items, scope)
	return unsafe.Pointer(submenu)
}

// buildMenuItems populates an existing NSMenu pointer with items.
// Nested submenus recurse through buildSubmenu and attach via
// quiEndSubmenu, consuming each child's retain.
func buildMenuItems(parent unsafe.Pointer, items []*NativeMenuItem, scope actionScope) {
	for _, it := range items {
		if it.Separator {
			C.quiAddSeparator(parent)
			continue
		}
		if it.Submenu != nil {
			child := buildSubmenu(it.Submenu, scope)
			C.quiEndSubmenu(parent, child)
			continue
		}
		keyEq, mods := parseShortcut(it.Shortcut)
		cLabel, freeLabel := cStr(it.Label)
		cKey, freeKey := cStr(keyEq)
		enabled := C.int(0)
		if it.Enabled || it.Action != nil {
			enabled = 1
		}
		checked := C.int(0)
		if it.Checked {
			checked = 1
		}
		it.id = registerAction(it.Action, scope)
		C.quiAddItem(parent, cLabel, cKey, C.int(mods), C.int(it.id), enabled, checked)
		freeLabel()
		freeKey()
	}
}

// parseShortcut converts "Cmd+S", "Ctrl+Shift+N" into a single char
// key equivalent + macOS modifier mask bits.
//
//	bit 0 = Shift, bit 1 = Control, bit 2 = Option (Alt), bit 3 = Command
func parseShortcut(s string) (string, int) {
	if s == "" {
		return "", 0
	}
	mods := 0
	key := ""
	// Simple tokenization: split on '+', last token is the key.
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '+' {
			tok := s[start:i]
			start = i + 1
			switch tok {
			case "Shift":
				mods |= 1
			case "Ctrl", "Control":
				mods |= 2
			case "Alt", "Option", "Opt":
				mods |= 4
			case "Cmd", "Command", "Super", "Meta":
				mods |= 8
			default:
				// key token; preserve case — NSMenu uses lowercase for most
				// letter keys, uppercase triggers the shift glyph; callers
				// pick the form they want.
				if len(tok) > 0 {
					key = tok
				}
			}
		}
	}
	return key, mods
}
