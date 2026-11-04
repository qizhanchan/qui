package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// OwnWindow popovers on the headless backend: real popup windows that never
// reach the screen.

func TestPopoverOwnWindowPlacesAWindowAgainstTheDisplay(t *testing.T) {
	owner, err := qui.NewHeadlessWindowForTest(800, 600)
	if err != nil {
		t.Fatal(err)
	}
	owner.SetPosition(100, 50)
	var setOpen func(bool)
	dismissed := 0
	css := `.pop { width: 300px; height: 200px; }`
	rt := h.Mount(owner, css, func() h.Node {
		open, set := reactive.UseState(true)
		setOpen = set
		var pop h.Node
		if open {
			pop = h.PopoverAt(func() qui.Rect { return qui.Rect{X: 700, Y: 500, W: 10, H: 10} },
				h.PopoverOptions{OwnWindow: true, OnDismiss: func() { dismissed++; set(false) }},
				h.Div().Class("pop"))
		}
		return h.Div(h.Span("doc"), pop)
	})
	rt.Render()

	if n := len(owner.Overlays()); n != 0 {
		t.Fatalf("an OwnWindow popover must not use the overlay stack (%d overlays)", n)
	}
	popEl := findClass(popupRoot(t, owner), "pop")
	if popEl == nil {
		t.Fatal("popover content not found in a popup window")
	}
	pw := popEl.Window()
	if pw == nil || pw == owner || pw.Kind() != qui.WindowPopup {
		t.Fatalf("content lives in %v, want a WindowPopup", pw)
	}
	if !pw.IsVisible() {
		t.Fatal("popup window not shown")
	}
	// Anchor (700,500) in the owner at (100,50) is (800,550) on screen;
	// the popover goes below it with the 4px gap — past the owner's
	// bottom edge (650), which an overlay could not.
	if p, sz := pw.Position(), pw.WindowSize(); p.X != 800 || p.Y != 564 || sz.W != 300 || sz.H != 200 {
		t.Fatalf("popup at %v size %v, want (800,564) 300x200", p, sz)
	}
	if b := popEl.Bounds(); b.X != 0 || b.Y != 0 || b.W != 300 || b.H != 200 {
		t.Fatalf("content laid out at %v, want the whole popup window", b)
	}

	// A press in the owner dismisses (and is swallowed).
	owner.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	owner.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, 10, 10, qui.MouseButtonLeft, 0))
	if dismissed != 1 {
		t.Fatalf("owner press dismissed %d times, want 1", dismissed)
	}
	rt.Render()
	if pw.IsVisible() {
		t.Fatal("dismissed popup still visible")
	}
	if pw.Root() != nil {
		t.Fatal("a parked popup window must drop its content")
	}

	// Reopening reuses the parked window.
	setOpen(true)
	rt.Render()
	if el := findClass(popupRoot(t, owner), "pop"); el == nil || el.Window() != pw {
		t.Fatal("reopened popover should reuse the parked popup window")
	}
}

func TestPopoverOwnWindowStaysOnTheDisplay(t *testing.T) {
	owner, err := qui.NewHeadlessWindowForTest(800, 600)
	if err != nil {
		t.Fatal(err)
	}
	owner.SetPosition(1500, 700) // the headless display is 1920x1080
	rt := h.Mount(owner, `.pop { width: 500px; height: 300px; }`, func() h.Node {
		return h.Div(h.PopoverAt(func() qui.Rect { return qui.Rect{X: 300, Y: 300, W: 10, H: 10} },
			h.PopoverOptions{OwnWindow: true}, h.Div().Class("pop")))
	})
	rt.Render()
	pw := findClass(popupRoot(t, owner), "pop").Window()
	p, sz := pw.Position(), pw.WindowSize()
	if p.X+sz.W > 1920 || p.Y+sz.H > 1080 {
		t.Fatalf("popup %v+%v runs off the display", p, sz)
	}
	// No room below (1000+14+300 > 1080): flipped above the anchor.
	if p.Y+sz.H > 1000 {
		t.Fatalf("popup at y=%v should flip above the anchor at y=1000", p.Y)
	}
}

func TestPopoverOwnWindowFallsBackWithoutPopupWindows(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	rt := h.Mount(win, `.pop { width: 100px; height: 60px; }`, func() h.Node {
		return h.Div(h.PopoverAt(func() qui.Rect { return qui.Rect{X: 10, Y: 10, W: 1, H: 1} },
			h.PopoverOptions{OwnWindow: true}, h.Div().Class("pop")))
	})
	rt.Render()
	if len(win.Overlays()) != 1 {
		t.Fatal("without popup windows the popover is an ordinary overlay")
	}
}

// popupRoot is the root of the visible popup window owned by owner.
func popupRoot(t *testing.T, owner *qui.Window) qui.Widget {
	t.Helper()
	for _, w := range qui.LiveWindows() {
		if w.Owner() == owner && w.Kind() == qui.WindowPopup && w.Root() != nil {
			return w.Root()
		}
	}
	t.Fatal("no popup window with content")
	return nil
}
