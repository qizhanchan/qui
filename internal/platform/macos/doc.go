// Package macos holds the engine's AppKit service bridges that sit beside
// the windowing backend: pasteboard, cursors, file panels and alerts, Core
// Text emoji, global hotkeys, the NSTextInputClient/IME hooks, system key
// capture, native menus and status items, printing, NSSpellChecker and the
// NSWindow chrome (fullscreen, child windows, sheets, title bar).
//
// Every function takes plain Go values or NSWindow/NSMenu pointers, so the
// package imports nothing from qui; the engine registers a Host to receive
// the callbacks AppKit raises. It serves both the cocoa backend and GLFW
// builds on macOS, which is why it is not part of internal/platform/cocoa.
//
// It builds only on darwin with cgo; callers guard their imports the same way.
package macos
