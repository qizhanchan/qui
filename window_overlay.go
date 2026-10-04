package qui

import "errors"

// WindowKind selects a window's role at the OS level. It is fixed at
// creation: whether a window can take keyboard focus, and whether its
// framebuffer has an alpha channel the compositor honors, are decided when
// the OS window and its GPU context are made.
type WindowKind int

const (
	// WindowNormal is an ordinary application window — title bar, in the
	// window cycle, takes keyboard focus when clicked.
	WindowNormal WindowKind = iota

	// WindowOverlayPanel is a borderless, transparent, always-on-top
	// surface that NEVER takes keyboard focus away from whatever the user
	// is typing into.
	//
	// This is not "a normal window with the chrome switched off". Three
	// properties make it a different kind of object, and all three are
	// load-bearing:
	//
	//   - Non-activating. Clicking the panel must not deactivate the app
	//     the user is typing into. An input method's candidate window that
	//     steals focus is not a candidate window — the host app's caret
	//     disappears and the next keystroke goes nowhere.
	//   - Transparent framebuffer. The panel draws its own rounded card and
	//     shadow, so the pixels outside that card must composite through to
	//     the desktop rather than being cleared to a background color.
	//   - Above everything, on every Space, including over fullscreen apps.
	//     A candidate window that is hidden behind the app it belongs to is
	//     useless.
	//
	// Because a panel cannot receive key events, it is a display-and-click
	// surface: mouse hit-testing works normally, keyboard input does not
	// reach it at all. The keystrokes belong to whatever the OS considers
	// focused; an input method receives them through its own OS channel and
	// drives the panel's widget tree from there.
	//
	// Not every windowing backend can offer this. NewOverlayPanel returns
	// an error rather than silently handing back a normal window: a
	// focus-stealing panel is worse than no panel, because the failure
	// shows up as the user's typing going to the wrong place rather than as
	// a missing feature.
	WindowOverlayPanel
)

// ErrOverlayPanelUnsupported is returned by NewOverlayPanel on a backend
// that cannot make a non-activating window.
var ErrOverlayPanelUnsupported = errors.New(
	"qui: the active platform backend cannot create overlay panels " +
		"(non-activating transparent windows); try QUI_PLATFORM=cocoa")

// NewOverlayPanel creates a WindowOverlayPanel — see that constant for
// what it is and why it is a distinct kind.
//
// The panel starts HIDDEN. An overlay exists to appear at a point the app
// computes at the moment it is needed (a caret rect reported by an input
// method, the mouse position, an anchor widget), so showing it at creation
// would flash it at the wrong place. Call Window.ShowAt.
//
// The window is not registered with any App; use App.NewOverlayPanel for
// that, so the app's Run loop steps it and tears it down.
func NewOverlayPanel(width, height int) (*Window, error) {
	assertProcessUIThread("NewOverlayPanel")
	plat, err := activePlatform()
	if err != nil {
		return nil, err
	}
	return newWindowFromConfig(plat, platformWindowConfig{
		Width:  width,
		Height: height,
		Kind:   WindowOverlayPanel,
	})
}

// NewOverlayPanel creates an overlay panel and registers it with the app so
// the Run loop steps it. See the package-level NewOverlayPanel.
func (a *App) NewOverlayPanel(width, height int) (*Window, error) {
	if a == nil {
		return nil, errors.New("app is nil")
	}
	w, err := NewOverlayPanel(width, height)
	if err != nil {
		return nil, err
	}
	a.windows = append(a.windows, w)
	return w, nil
}

// Kind reports the OS-level role this window was created with.
func (w *Window) Kind() WindowKind {
	if w == nil {
		return WindowNormal
	}
	return w.kind
}

// ShowAt moves the window so its top-left corner sits at the given GLOBAL
// logical coordinates, then makes it visible WITHOUT activating it.
//
// Position first, then show: the reverse order makes the panel appear at
// its previous location for a frame before jumping, which reads as a
// flicker every single keystroke in an input method.
//
// On a platform with no absolute positioning (Wayland) the move is skipped
// and the panel is shown wherever the compositor puts it — the same
// degradation as Window.SetPosition.
func (w *Window) ShowAt(x, y int) {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.ShowAt")
	w.plat.setPos(x, y)
	w.plat.setVisible(true, false)
	// A hidden window skips painting entirely, so whatever was on screen
	// last time is stale by definition. Force a full repaint rather than
	// trusting the accumulated dirty region.
	w.Invalidate()
}

// Show makes the window visible without moving it, and without activating
// it. Use ShowAt when the position matters, which for an overlay panel is
// almost always.
func (w *Window) Show() {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.Show")
	w.plat.setVisible(true, w.kind != WindowOverlayPanel)
	w.Invalidate()
}

// Hide orders the window off screen without destroying it.
//
// Hide rather than destroy is the point of the API: an overlay panel is
// shown and hidden on nearly every keystroke, and recreating an OS window
// plus a GPU context at that rate is far too slow. The widget tree, the
// context and the cached frame all survive.
func (w *Window) Hide() {
	if w == nil || w.plat == nil {
		return
	}
	w.assertUIThread("Window.Hide")
	w.plat.setVisible(false, false)
}

// IsVisible reports whether the OS considers this window on screen.
func (w *Window) IsVisible() bool {
	if w == nil || w.plat == nil {
		return false
	}
	w.assertUIThread("Window.IsVisible")
	return w.plat.isVisible()
}
