package qui

import "testing"

// releaseSpy counts the mouse presses and releases it is the target of.
type releaseSpy struct {
	*Container
	downs, ups int
}

func newReleaseSpy() *releaseSpy {
	s := &releaseSpy{Container: NewContainer(nil)}
	s.SetSelf(s)
	return s
}

func (s *releaseSpy) Handle(e Event) bool {
	if me, ok := e.(MouseEvent); ok && e.Phase() == PhaseTarget {
		switch me.Type() {
		case EventMouseDown:
			s.downs++
		case EventMouseUp:
			s.ups++
		}
	}
	return false
}

// closeOnPress is a popup row that acts on MouseDown and closes its popup
// right there, as menu rows do.
type closeOnPress struct {
	*Container
	win   *Window
	fired int
}

func (c *closeOnPress) Handle(e Event) bool {
	if me, ok := e.(MouseEvent); ok && me.Type() == EventMouseDown && e.Phase() == PhaseTarget {
		c.fired++
		c.win.RemoveOverlay(c)
		return true
	}
	return false
}

func pressRelease(win *Window, x, y float32) {
	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, x, y, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, x, y, MouseButtonLeft, 0))
}

// A popup that closes on MouseDown takes its press with it: the release must
// not be re-targeted by hit test onto whatever sat under the popup (picking a
// menu row over a toolbar button used to click the button too).
func TestReleaseOfClosedPopupPressDoesNotReachWidgetBeneath(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	under := newReleaseSpy()
	win.SetRoot(under)
	win.LayoutForTest()

	row := &closeOnPress{Container: NewContainer(nil), win: win}
	row.SetSelf(row)
	win.PushOverlay(row)
	row.Layout(Rect{X: 20, Y: 20, W: 120, H: 40})

	pressRelease(win, 60, 40)
	if row.fired != 1 {
		t.Fatalf("popup row fired %d times, want 1", row.fired)
	}
	if len(win.Overlays()) != 0 {
		t.Fatalf("popup still open")
	}
	if under.downs != 0 || under.ups != 0 {
		t.Fatalf("widget beneath the closed popup saw %d downs / %d ups, want 0 / 0", under.downs, under.ups)
	}

	// The next gesture is an ordinary click on the widget beneath.
	pressRelease(win, 60, 40)
	if under.downs != 1 || under.ups != 1 {
		t.Fatalf("follow-up click: %d downs / %d ups, want 1 / 1", under.downs, under.ups)
	}
}

// A press whose target survives keeps its release, including one dragged off
// the target (capture still delivers it there).
func TestCapturedReleaseStillDelivered(t *testing.T) {
	win := NewTestWindow(Size{W: 200, H: 200})
	spy := newReleaseSpy()
	win.SetRoot(spy)
	win.LayoutForTest()

	win.DispatchTestEvent(NewMouseEvent(EventMouseDown, 50, 50, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseMove, 400, 400, MouseButtonLeft, 0))
	win.DispatchTestEvent(NewMouseEvent(EventMouseUp, 400, 400, MouseButtonLeft, 0))
	if spy.downs != 1 || spy.ups != 1 {
		t.Fatalf("captured gesture: %d downs / %d ups, want 1 / 1", spy.downs, spy.ups)
	}
}
