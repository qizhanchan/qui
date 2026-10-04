package qui

import (
	"errors"
	"image"
)

// ErrSystrayNotSupported is returned by App.NewStatusItem on
// platforms without a system tray implementation. v1 only ships
// macOS (NSStatusItem); Windows / Linux are deferred.
var ErrSystrayNotSupported = errors.New("qui: system tray not supported on this platform")

// StatusItemOptions configures a system tray icon. Either Icon or
// Image supplies the glyph; if both are zero the item shows Title
// text only. Menu and OnClick are mutually exclusive — Menu takes
// precedence when both are set.
type StatusItemOptions struct {
	// Icon is rasterized to a monochrome template image — macOS
	// auto-tints it for the current light/dark menu bar. Typical
	// source: an svg.Document. The output runs through Rasterize
	// at 44×44 px (22 pt @2x) with a white tint.
	Icon VectorSource

	// Image is a pre-rasterized RGBA bitmap used as-is (NOT a
	// template). Preferred when the brand requires fixed colors.
	// Image takes precedence over Icon.
	Image image.Image

	// Title is shown next to the icon (or alone if no icon was
	// supplied). Useful as a text-only fallback.
	Title string

	// Tooltip is shown on hover.
	Tooltip string

	// Menu pops up on left/right click. Reuses NativeMenu so the
	// same data type drives the application menu bar, tray
	// menus, and (future) right-click context menus.
	Menu *NativeMenu

	// OnClick fires on left-click when Menu is nil. Runs on the
	// Go main goroutine via the same action queue NativeMenuBar
	// uses, so it's safe to touch the widget tree.
	OnClick func()
}
