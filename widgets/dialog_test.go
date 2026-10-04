package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func newKeyDown(key Key) KeyEvent {
	return NewKeyEvent(EventKeyDown, key, 0)
}

// ---- Popup ----

func TestPopupShowPushesOntoOverlays(t *testing.T) {
	content := newPhaseSpy("c", Rect{}, nil)
	pop := NewPopup(content)
	w := NewTestWindow(Size{W: 500, H: 500})

	if len(w.Overlays()) != 0 {
		t.Fatalf("precondition: overlays should start empty")
	}
	pop.ShowAt(w, 100, 100)
	if len(w.Overlays()) != 1 || w.Overlays()[0] != pop {
		t.Errorf("after ShowAt, overlays=%+v, want [popup]", w.Overlays())
	}
}

func TestPopupCloseRemovesFromOverlays(t *testing.T) {
	closed := false
	content := newPhaseSpy("c", Rect{}, nil)
	pop := NewPopup(content)
	pop.OnClose = func() { closed = true }
	w := NewTestWindow(Size{W: 500, H: 500})

	pop.ShowAt(w, 50, 50)
	pop.Close()
	if len(w.Overlays()) != 0 {
		t.Errorf("after Close, overlays should be empty; got %+v", w.Overlays())
	}
	if !closed {
		t.Error("OnClose should fire on Close()")
	}
}

func TestPopupOutsideClickDismisses(t *testing.T) {
	content := newPhaseSpy("c", Rect{X: 100, Y: 100, W: 50, H: 50}, nil)
	pop := NewPopup(content)
	w := NewTestWindow(Size{W: 500, H: 500})
	pop.ShowAt(w, 100, 100)

	// Click outside content (far away).
	consumed := pop.Handle(newMouseEventForHandle(EventMouseDown, 400, 400))
	if !consumed {
		t.Error("outside click should be consumed by popup")
	}
	if pop.window != nil {
		t.Error("outside click should have closed the popup")
	}
}

func TestPopupInsideClickDoesNotDismiss(t *testing.T) {
	content := newPhaseSpy("c", Rect{X: 100, Y: 100, W: 50, H: 50}, nil)
	pop := NewPopup(content)
	w := NewTestWindow(Size{W: 500, H: 500})
	pop.ShowAt(w, 100, 100)

	// Click inside content — popup stays open.
	pop.Handle(newMouseEventForHandle(EventMouseDown, 120, 120))
	if pop.window == nil {
		t.Error("inside click should not close popup")
	}
}

func TestPopupEscapeDismisses(t *testing.T) {
	content := newPhaseSpy("c", Rect{}, nil)
	pop := NewPopup(content)
	w := NewTestWindow(Size{W: 500, H: 500})
	pop.ShowAt(w, 0, 0)

	pop.Handle(newKeyDown(KeyEscape))
	if pop.window != nil {
		t.Error("Esc should close popup")
	}
}

func TestPopupHitTestRoutesToContent(t *testing.T) {
	content := newPhaseSpy("c", Rect{X: 100, Y: 100, W: 50, H: 50}, nil)
	pop := NewPopup(content)
	w := NewTestWindow(Size{W: 500, H: 500})
	pop.ShowAt(w, 100, 100)

	if hit := pop.HitTest(Point{X: 120, Y: 120}); hit != content {
		t.Errorf("hit inside content: got %v, want content", hit)
	}
	// Outside content, dismiss-on-outside-click grabs the click.
	if hit := pop.HitTest(Point{X: 300, Y: 300}); hit != pop {
		t.Errorf("hit outside: got %v, want popup (absorbing)", hit)
	}
}

// ---- Dialog ----

func TestDialogModalDeclaresFocusTrap(t *testing.T) {
	d := NewDialog("t", nil)
	if !d.Modal() {
		t.Error("Dialog.Modal() should return true")
	}
}

func TestDialogShowPushesAndLays(t *testing.T) {
	d := NewDialog("Title", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	if len(w.Overlays()) != 1 || w.Overlays()[0] != d {
		t.Errorf("Dialog.Show should push overlay; got %+v", w.Overlays())
	}
	if d.contentBounds.W == 0 || d.contentBounds.H == 0 {
		t.Errorf("Show should layout content box; got %+v", d.contentBounds)
	}
}

func TestDialogRecentersOnWindowResize(t *testing.T) {
	d := NewDialog("Title", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	before := d.contentBounds
	w.ResizeForTest(Size{W: 1200, H: 600})
	after := d.contentBounds
	// Wider window ⇒ the centered box shifts right (larger X origin).
	if after.X <= before.X {
		t.Errorf("dialog should re-center on resize; X %v -> %v", before.X, after.X)
	}
}

func TestDialogCloseRemovesOverlay(t *testing.T) {
	onCloseCalled := false
	d := NewDialog("Title", nil)
	d.OnClose = func() { onCloseCalled = true }
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	d.Close()
	if len(w.Overlays()) != 0 {
		t.Errorf("Close should remove overlay; got %+v", w.Overlays())
	}
	if !onCloseCalled {
		t.Error("OnClose should fire")
	}
}

func TestDialogEscCloses(t *testing.T) {
	d := NewDialog("t", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	d.Handle(newKeyDown(KeyEscape))
	if d.window != nil {
		t.Error("Esc should close dialog")
	}
}

func TestDialogBackdropClickAbsorbedNotDismissed(t *testing.T) {
	d := NewDialog("t", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	// Click at (0,0) — outside content box, on backdrop.
	consumed := d.Handle(newMouseEventForHandle(EventMouseDown, 0, 0))
	if !consumed {
		t.Error("backdrop click should be consumed (modal)")
	}
	if d.window == nil {
		t.Error("backdrop click should NOT dismiss dialog (Dialog only closes via button or Esc)")
	}
}

func TestDialogAddButtonAutoCloses(t *testing.T) {
	onClickFired := false
	d := NewDialog("t", nil)
	btn := d.AddButton("OK", func() { onClickFired = true })
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)

	btn.OnClick()
	if !onClickFired {
		t.Error("button click handler should fire")
	}
	if d.window != nil {
		t.Error("button click should auto-close dialog")
	}
}

func TestDialogHitTestRoutesToButton(t *testing.T) {
	d := NewDialog("t", nil)
	btn := d.AddButton("OK", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)

	// Pick a point inside the button.
	btnBounds := btn.Bounds()
	mid := Point{X: btnBounds.X + btnBounds.W/2, Y: btnBounds.Y + btnBounds.H/2}
	if hit := d.HitTest(mid); hit != btn {
		t.Errorf("hit on button: got %v, want button", hit)
	}
}

func TestDialogHitTestAbsorbsBackdrop(t *testing.T) {
	d := NewDialog("t", nil)
	w := NewTestWindow(Size{W: 800, H: 600})
	d.Show(w)
	// Backdrop click (outside content box).
	if hit := d.HitTest(Point{X: 0, Y: 0}); hit != d {
		t.Errorf("backdrop hit: got %v, want dialog (absorbing)", hit)
	}
}

func TestDialogModalTrapsFocusCollection(t *testing.T) {
	// Focusable in main tree — should NOT appear when modal dialog is up.
	mainFocusable := newFocusableSpy("main")
	root := NewContainer(nil, mainFocusable)
	w := windowWithRoot(Size{W: 800, H: 600}, root)

	dialogFocusable := newFocusableSpy("dialog")
	d := NewDialog("t", dialogFocusable)
	d.Show(w)

	list := w.CollectFocusables()
	for _, wd := range list {
		if wd == mainFocusable {
			t.Errorf("main-tree focusable should be trapped out when modal is up; list=%+v", list)
		}
	}
	// Dialog's own focusable content widget should appear.
	foundDialog := false
	for _, wd := range list {
		if wd == dialogFocusable {
			foundDialog = true
		}
	}
	if !foundDialog {
		t.Errorf("dialog focusable should appear; list=%+v", list)
	}
}

func TestDialogCloseRestoresPreviousFocus(t *testing.T) {
	mainFocusable := newFocusableSpy("main")
	root := NewContainer(nil, mainFocusable)
	w := windowWithRoot(Size{W: 800, H: 600}, root)
	w.SetFocus(mainFocusable)

	dialogFocusable := newFocusableSpy("dialog")
	d := NewDialog("t", dialogFocusable)
	d.Show(w)
	if w.Focused() != dialogFocusable {
		t.Fatalf("dialog focus = %v, want dialog content", w.Focused())
	}

	d.Close()

	if w.Focused() != mainFocusable || !mainFocusable.focused {
		t.Fatalf("focus after dialog close = %v, want main", w.Focused())
	}
}

func TestDialogChildListIncludesContentAndButtons(t *testing.T) {
	body := newPhaseSpy("body", Rect{}, nil)
	d := NewDialog("t", body)
	b1 := d.AddButton("A", nil)
	b2 := d.AddButton("B", nil)

	list := d.ChildList()
	if len(list) != 3 {
		t.Fatalf("ChildList len = %d, want 3", len(list))
	}
	if list[0] != body {
		t.Errorf("first child = %v, want body", list[0])
	}
	if list[1] != b1 || list[2] != b2 {
		t.Errorf("buttons wrong order: %+v", list[1:])
	}
}

func TestDialogAddButtonSetsParent(t *testing.T) {
	d := NewDialog("t", nil)
	btn := d.AddButton("OK", nil)
	if btn.Parent() != d {
		t.Errorf("button.Parent = %v, want dialog", btn.Parent())
	}
}

func TestDialogConstructorWiresContentParentAndSelf(t *testing.T) {
	body := newPhaseSpy("body", Rect{}, nil)
	d := NewDialog("t", body)

	if body.Parent() != d {
		t.Fatalf("content.Parent = %v, want dialog", body.Parent())
	}
	if d.Self() != d {
		t.Fatalf("dialog.Self = %v, want dialog", d.Self())
	}
}
