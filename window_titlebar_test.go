package qui

import "testing"

// The contract that matters for portability: on a platform (or a window)
// without an overlay title bar, every primitive answers "no" and answers it
// cheaply — no panic, no half-applied state — so an app can branch on the
// return value and lay out below a native title bar instead. Test windows
// have no platform window, which is the same code path.

func TestTitlebarDefaultsToNative(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 100})
	if got := w.TitlebarStyle(); got != TitlebarNative {
		t.Errorf("default TitlebarStyle = %v, want TitlebarNative", got)
	}
	if got := w.TitlebarInsets(); got != (Insets{}) {
		t.Errorf("native title bar reserves %v, want no insets", got)
	}
}

func TestTitlebarStyleRemembersTheRequestItCouldNotApply(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 100})
	if w.SetTitlebarStyle(TitlebarOverlay) {
		t.Error("SetTitlebarStyle reported success on a window with no platform window")
	}
	// Reading back the REQUEST (not the platform's answer) is what lets app
	// layout be written once: one branch on the constructor's return value,
	// then everything else reads TitlebarStyle.
	if got := w.TitlebarStyle(); got != TitlebarOverlay {
		t.Errorf("TitlebarStyle = %v after an unapplied request, want TitlebarOverlay", got)
	}
	if got := w.TitlebarInsets(); got != (Insets{}) {
		t.Errorf("insets = %v with no platform window, want zero", got)
	}
}

func TestWindowDragAndZoomDegradeQuietly(t *testing.T) {
	w := NewTestWindow(Size{W: 200, H: 100})
	if w.BeginWindowDrag() {
		t.Error("BeginWindowDrag reported success with no platform window")
	}
	if w.ToggleMaximize() {
		t.Error("ToggleMaximize reported success with no platform window")
	}
}

func TestTitlebarAPIToleratesNilWindow(t *testing.T) {
	var w *Window
	if w.SetTitlebarStyle(TitlebarOverlay) || w.BeginWindowDrag() || w.ToggleMaximize() {
		t.Error("nil window reported success")
	}
	if got := w.TitlebarStyle(); got != TitlebarNative {
		t.Errorf("nil window TitlebarStyle = %v, want TitlebarNative", got)
	}
	if got := w.TitlebarInsets(); got != (Insets{}) {
		t.Errorf("nil window insets = %v, want zero", got)
	}
}
