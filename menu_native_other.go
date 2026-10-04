//go:build !darwin

package qui

// SetMenuBar on non-darwin platforms is a no-op stub. Windows HMENU
// and Linux GTK integration land in follow-up phases. Apps that need
// a menu on these platforms should use the in-window MenuBar widget
// (widgets_menu.go) instead.
func (a *App) SetMenuBar(bar *NativeMenuBar) {
	// no-op; actions from the passed bar are not wired.
	_ = bar
}
