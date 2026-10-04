package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestTabViewDefaultSelectionIsFirst(t *testing.T) {
	tv := NewTabView(
		Tab{Title: "A", Content: newPhaseSpy("a", Rect{}, nil)},
		Tab{Title: "B", Content: newPhaseSpy("b", Rect{}, nil)},
	)
	if tv.SelectedIdx != 0 {
		t.Errorf("default SelectedIdx = %d, want 0", tv.SelectedIdx)
	}
}

func TestTabViewSelectFiresOnChange(t *testing.T) {
	called := -1
	tv := NewTabView(
		Tab{Title: "A", Content: newPhaseSpy("a", Rect{}, nil)},
		Tab{Title: "B", Content: newPhaseSpy("b", Rect{}, nil)},
	)
	tv.OnSelect = func(i int) { called = i }
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})

	tv.Select(1)
	if tv.SelectedIdx != 1 {
		t.Errorf("Select(1): SelectedIdx=%d", tv.SelectedIdx)
	}
	if called != 1 {
		t.Errorf("OnSelect got %d, want 1", called)
	}
	// Re-select same tab: no fire.
	called = -1
	tv.Select(1)
	if called != -1 {
		t.Error("re-selecting same tab should not fire OnSelect")
	}
}

func TestTabViewClickOnStripSelectsTab(t *testing.T) {
	tv := NewTabView(
		Tab{Title: "A", Content: newPhaseSpy("a", Rect{}, nil)},
		Tab{Title: "B", Content: newPhaseSpy("b", Rect{}, nil)},
		Tab{Title: "C", Content: newPhaseSpy("c", Rect{}, nil)},
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 300, H: 200})
	// Each tab ~ 100px wide. Click at x=150 → middle tab (index 1).
	tv.Handle(newMouseEventForHandle(EventMouseDown, 150, 10))
	if tv.SelectedIdx != 1 {
		t.Errorf("click on second tab: got %d, want 1", tv.SelectedIdx)
	}
}

func TestTabViewChildListReturnsOnlySelectedContent(t *testing.T) {
	a := newPhaseSpy("a", Rect{}, nil)
	b := newPhaseSpy("b", Rect{}, nil)
	tv := NewTabView(Tab{Title: "A", Content: a}, Tab{Title: "B", Content: b})

	list := tv.ChildList()
	if len(list) != 1 || list[0] != a {
		t.Errorf("ChildList: got %+v, want [a] only", list)
	}

	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	tv.Select(1)
	list = tv.ChildList()
	if len(list) != 1 || list[0] != b {
		t.Errorf("after Select(1), ChildList: got %+v, want [b]", list)
	}
}

func TestTabViewArrowKeysChangeSelection(t *testing.T) {
	tv := NewTabView(
		Tab{Title: "A"},
		Tab{Title: "B"},
		Tab{Title: "C"},
	)
	tv.Layout(Rect{X: 0, Y: 0, W: 300, H: 200})
	tv.SetFocused(true)

	tv.Handle(newKeyDown(KeyRight))
	if tv.SelectedIdx != 1 {
		t.Errorf("KeyRight: got %d, want 1", tv.SelectedIdx)
	}
	tv.Handle(newKeyDown(KeyRight))
	if tv.SelectedIdx != 2 {
		t.Errorf("KeyRight x2: got %d", tv.SelectedIdx)
	}
	// At last tab, KeyRight is a no-op.
	tv.Handle(newKeyDown(KeyRight))
	if tv.SelectedIdx != 2 {
		t.Errorf("KeyRight at end should not wrap; got %d", tv.SelectedIdx)
	}
	tv.Handle(newKeyDown(KeyLeft))
	if tv.SelectedIdx != 1 {
		t.Errorf("KeyLeft: got %d", tv.SelectedIdx)
	}
}

func TestTabViewHitTestStripReturnsSelf(t *testing.T) {
	content := newPhaseSpy("a", Rect{}, nil)
	tv := NewTabView(Tab{Title: "A", Content: content})
	tv.Layout(Rect{X: 0, Y: 0, W: 200, H: 200})
	content.Layout(Rect{X: 0, Y: tabStripH, W: 200, H: 200 - tabStripH})

	// Point on the tab strip.
	hit := tv.HitTest(Point{X: 50, Y: 10})
	if hit != tv {
		t.Errorf("strip hit: got %v, want TabView", hit)
	}
	// Point in content area.
	hit = tv.HitTest(Point{X: 50, Y: 100})
	if hit != content {
		t.Errorf("content hit: got %v, want content", hit)
	}
}
