//go:build darwin && cgo

package macos

/*
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

import "unsafe"

// ResetMenuBar clears the main menu before it is rebuilt.
func ResetMenuBar() { C.quiResetMenuBar() }

// BeginSubmenu returns a retained NSMenu; hand it to EndSubmenu or
// StatusItemSetMenu to consume the retain.
func BeginSubmenu(title string) unsafe.Pointer {
	c, free := cStr(title)
	defer free()
	return C.quiBeginSubmenu(c)
}

// EndSubmenu nests submenu inside parent, or attaches it to the main menu
// bar when parent is nil.
func EndSubmenu(parent, submenu unsafe.Pointer) { C.quiEndSubmenu(parent, submenu) }

// AddItem appends an item whose activation calls Host.MenuAction(actionID).
func AddItem(parent unsafe.Pointer, title, keyEq string, mods, actionID int, enabled, checked bool) {
	cTitle, freeTitle := cStr(title)
	defer freeTitle()
	cKey, freeKey := cStr(keyEq)
	defer freeKey()
	C.quiAddItem(parent, cTitle, cKey, C.int(mods), C.int(actionID), cBool(enabled), cBool(checked))
}

// AddSeparator appends a separator item.
func AddSeparator(parent unsafe.Pointer) { C.quiAddSeparator(parent) }

// AddRoleItem appends an item bound to a standard AppKit action.
func AddRoleItem(parent unsafe.Pointer, title string, role int, keyEq string, mods int) {
	cTitle, freeTitle := cStr(title)
	defer freeTitle()
	cKey, freeKey := cStr(keyEq)
	defer freeKey()
	C.quiAddRoleItem(parent, cTitle, C.int(role), cKey, C.int(mods))
}

// InstallMainMenu makes the built menu the application's main menu.
func InstallMainMenu() { C.quiInstallMainMenu() }

// SetWindowsMenu marks menu as the application's Window menu.
func SetWindowsMenu(menu unsafe.Pointer) { C.quiSetWindowsMenu(menu) }

// SetHelpMenu marks menu as the application's Help menu.
func SetHelpMenu(menu unsafe.Pointer) { C.quiSetHelpMenu(menu) }
