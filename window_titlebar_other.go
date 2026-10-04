//go:build !darwin || !cgo

package qui

import "unsafe"

// No client-side title bar here yet. Reporting "not supported" rather than
// faking it is deliberate: an app that gets false keeps its native title bar
// and lays its tab strip out below it, which looks intentional. A fake
// (borderless window, or moving the window from mouse deltas) would lose
// resize edges and snapping and feel broken.
//
// Windows would implement these by extending the frame into the client area
// (DwmExtendFrameIntoClientArea) and answering WM_NCHITTEST with HTCAPTION
// for drag regions; Wayland by asking the compositor for a move with
// xdg_toplevel.move, using the serial of the press.

func platformSetTitlebarStyle(unsafe.Pointer, TitlebarStyle) bool { return false }

func platformTitlebarInsets(unsafe.Pointer) Insets { return Insets{} }

func platformBeginWindowDrag(unsafe.Pointer) bool { return false }

func platformToggleMaximize(unsafe.Pointer) bool { return false }
