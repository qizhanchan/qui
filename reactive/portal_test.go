package reactive_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
)

// Toggling a Portal pushes/removes a window overlay, and the content is
// centered at its natural size.
func TestPortalMountsOverlayCentered(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	var content *qui.Container
	open := false
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiLabel("under"),
			uiWhen(open, func() uiNode {
				return uiPortal(uiVBox().
					Setup(func(c *qui.Container) {
						content = c
						c.SetMinSize(120, 80)
					}).
					Padding(0).
					Children(uiLabel("dialog")))
			}),
		).Build()
	})
	rt.Render()

	if got := window.IdleState().OverlayCount; got != 0 {
		t.Fatalf("closed: overlay count = %d, want 0", got)
	}

	open = true
	rt.Render()
	if got := window.IdleState().OverlayCount; got != 1 {
		t.Fatalf("open: overlay count = %d, want 1", got)
	}
	if content == nil {
		t.Fatal("portal content never created")
	}
	b := content.Bounds()
	if b.W < 120 || b.H < 80 {
		t.Fatalf("content not sized: %+v", b)
	}
	// Centered in 400×300.
	wantX := (400 - b.W) / 2
	wantY := (300 - b.H) / 2
	if b.X != wantX || b.Y != wantY {
		t.Errorf("content at (%v,%v), want centered (%v,%v)", b.X, b.Y, wantX, wantY)
	}

	open = false
	rt.Render()
	if got := window.IdleState().OverlayCount; got != 0 {
		t.Fatalf("re-closed: overlay count = %d, want 0", got)
	}
}

func TestPortalUnmountRestoresPreviousFocus(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	open := false
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiButton("main"),
			uiWhen(open, func() uiNode {
				return portalNode{
					opts:  reactive.PortalOptions{OnEscape: func() {}},
					child: uiVBox().Children(uiLabel("dialog")),
				}
			}),
		).Build()
	})
	rt.Render()
	root := rt.Root().(*qui.Container)
	main := root.ChildAt(0)
	window.SetFocus(main)

	open = true
	rt.Render()
	if window.Focused() == main {
		t.Fatal("portal did not take focus")
	}

	open = false
	rt.Render()
	if window.Focused() != main {
		t.Fatalf("focus after portal unmount = %v, want main", window.Focused())
	}
}

// A modal portal blocks clicks to the UI underneath, and a backdrop
// click fires the dismiss callback.
func TestModalPortalBlocksAndDismisses(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	underClicked := false
	dismissed := false
	open := true
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiHBox().ID("under-row").
				OnClick(func() { underClicked = true }).
				Children(uiLabel("underlying")),
			uiWhen(open, func() uiNode {
				return uiModalPortal(func() { dismissed = true },
					uiVBox().Setup(func(c *qui.Container) { c.SetMinSize(100, 60) }).
						Children(uiLabel("modal")))
			}),
		).Build()
	})
	rt.Render()

	root := rt.Root()
	root.Measure(qui.Size{W: 400, H: 300})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Click near the top-left — over the underlying row, but on the
	// modal's scrim. The row must NOT receive it; dismiss must fire.
	window.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	if underClicked {
		t.Error("click passed through a modal scrim to the underlying UI")
	}
	if !dismissed {
		t.Error("backdrop click did not fire the dismiss callback")
	}
}

// A non-modal portal is pointer-transparent outside its content.
func TestNonModalPortalIsClickThrough(t *testing.T) {
	window := qui.NewTestWindow(qui.Size{W: 400, H: 300})

	underClicked := false
	rt := reactive.NewRuntime(window, func() reactive.Element {
		return uiVBox().Children(
			uiHBox().OnClick(func() { underClicked = true }).
				Children(uiLabel("underlying")),
			uiElem(reactive.Portal(
				uiVBox().Setup(func(c *qui.Container) { c.SetMinSize(50, 30) }).
					Children(uiLabel("toast")).Build())),
		).Build()
	})
	rt.Render()

	root := rt.Root()
	root.Measure(qui.Size{W: 400, H: 300})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	// Top-left is outside the centered toast — click must reach the row.
	window.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, 10, 10, qui.MouseButtonLeft, 0))
	if !underClicked {
		t.Error("non-modal portal blocked a click outside its content")
	}
}
