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
void quiAddRoleItem(void* parent, const char* title, int role, const char* keyEq, int mods);
void quiSetWindowsMenu(void* menu);
void quiSetHelpMenu(void* menu);
*/
import "C"

import (
	"strconv"
	"strings"
	"unsafe"
)

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
	menuWindowKeys = map[menuChord]bool{}
	C.quiResetMenuBar()
	for _, m := range bar.Menus {
		installMenu(m)
	}
	C.quiInstallMainMenu()
}

// menuWindowKeys are the main menu's KeyToWindow shortcuts: the key
// monitor hands these chords to the window instead of the menu bar.
// Touched only on the main thread (SetMenuBar, the key monitor).
var menuWindowKeys = map[menuChord]bool{}

type menuChord struct {
	key  Key
	mods Modifiers
}

const menuChordMods = ModShift | ModControl | ModAlt | ModSuper

// menuLeavesKey reports whether key+mods is a KeyToWindow menu shortcut.
func menuLeavesKey(key Key, mods Modifiers) bool {
	return menuWindowKeys[menuChord{key, mods & menuChordMods}]
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
	if m.Role == MenuRoleWindow {
		C.quiSetWindowsMenu(submenu)
	}
	if m.Role == MenuRoleHelp {
		C.quiSetHelpMenu(submenu)
	}
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
		if it.KeyToWindow && scope == scopeMainMenu && it.Shortcut != "" {
			if k, m, err := ParseShortcut(it.Shortcut); err == nil {
				menuWindowKeys[menuChord{k, m & menuChordMods}] = true
			}
		}
		if it.Role != MenuRoleNone {
			cLabel, freeLabel := cStr(it.Label)
			cKey, freeKey := cStr(keyEq)
			C.quiAddRoleItem(parent, cLabel, C.int(it.Role), cKey, C.int(mods))
			freeLabel()
			freeKey()
			continue
		}
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
				// pick the form they want. Named keys (F12, Left, Enter, …)
				// become the characters AppKit uses for them.
				if len(tok) > 0 {
					key = menuKeyEquivalent(tok)
				}
			}
		}
	}
	return key, mods
}

// menuKeyEquivalent maps a named key to its NSMenu key equivalent: the
// function-key code points of NSEvent.h (NSF1FunctionKey = 0xF704, …) or
// the control character AppKit expects. Other tokens pass through.
func menuKeyEquivalent(tok string) string {
	switch strings.ToLower(tok) {
	case "enter", "return":
		return "\r"
	case "tab":
		return "\t"
	case "space":
		return " "
	case "escape", "esc":
		return "\x1b"
	case "backspace":
		return "\b"
	case "delete", "del":
		return string(rune(0xF728)) // NSDeleteFunctionKey (forward delete)
	case "up":
		return string(rune(0xF700))
	case "down":
		return string(rune(0xF701))
	case "left":
		return string(rune(0xF702))
	case "right":
		return string(rune(0xF703))
	case "home":
		return string(rune(0xF729))
	case "end":
		return string(rune(0xF72B))
	case "pageup":
		return string(rune(0xF72C))
	case "pagedown":
		return string(rune(0xF72D))
	case "insert":
		return string(rune(0xF727))
	}
	if len(tok) >= 2 && (tok[0] == 'F' || tok[0] == 'f') {
		if n, err := strconv.Atoi(tok[1:]); err == nil && n >= 1 && n <= 35 {
			return string(rune(0xF704 + n - 1))
		}
	}
	return tok
}
