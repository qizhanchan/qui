package qui

import (
	"errors"
	"testing"
)

func TestPopupWindowNeedsAPlatformOwner(t *testing.T) {
	if _, err := NewPopupWindow(NewTestWindow(Size{W: 100, H: 100}), 10, 10); !errors.Is(err, ErrPopupWindowUnsupported) {
		t.Fatalf("a test window has no OS window: want ErrPopupWindowUnsupported, got %v", err)
	}
	if _, err := NewPopupWindow(nil, 10, 10); !errors.Is(err, ErrPopupWindowUnsupported) {
		t.Fatalf("nil owner: want ErrPopupWindowUnsupported, got %v", err)
	}
}

func TestPopupWindowLifecycle(t *testing.T) {
	owner, err := NewHeadlessWindowForTest(800, 600)
	if err != nil {
		t.Fatal(err)
	}
	owner.SetPosition(100, 50)
	pop, err := NewPopupWindow(owner, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pop.Kind() != WindowPopup {
		t.Fatalf("kind %v, want WindowPopup", pop.Kind())
	}
	if pop.Owner() != owner {
		t.Fatal("popup is not owned by its owner")
	}
	if pop.IsVisible() {
		t.Fatal("a popup starts hidden")
	}
	pop.SetPopupFocusable(true)
	if !pop.PopupFocusable() {
		t.Fatal("SetPopupFocusable(true) not kept")
	}
	pop.SetSize(200, 120)
	pop.ShowAt(300, 400)
	if !pop.IsVisible() {
		t.Fatal("ShowAt left the popup hidden")
	}
	if p := pop.Position(); p.X != 300 || p.Y != 400 {
		t.Fatalf("popup at %v, want (300,400)", p)
	}
	pop.Hide()
	if pop.IsVisible() {
		t.Fatal("Hide left the popup visible")
	}

	// Transparent clear: nothing drawn → a fully transparent frame.
	pop.Show()
	pop.Step()
	if img := pop.Snapshot(); img == nil || img.RGBAAt(10, 10).A != 0 {
		t.Fatal("a popup's frame must clear to transparent")
	}

	r, ok := owner.ToScreen(Rect{X: 10, Y: 20, W: 30, H: 40})
	if !ok || r != (Rect{X: 110, Y: 70, W: 30, H: 40}) {
		t.Fatalf("ToScreen = %v,%v", r, ok)
	}
}
