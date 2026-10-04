package widgets

import (
	"testing"

	. "github.com/qizhanchan/qui"
)

func TestFieldSetSelfWiredForSubclassing(t *testing.T) {
	a := newPhaseSpy("a", Rect{}, nil)
	g := NewFieldSet("Settings", nil, a)
	if g.Self() != g {
		t.Error("FieldSet.Self() should point at the outer FieldSet")
	}
	// Child parent must be FieldSet pointer (not embedded Container).
	if a.Parent() != g {
		t.Errorf("child parent = %v, want FieldSet", a.Parent())
	}
}

func TestFieldSetChildListReturnsChildren(t *testing.T) {
	a := newPhaseSpy("a", Rect{}, nil)
	b := newPhaseSpy("b", Rect{}, nil)
	g := NewFieldSet("t", nil, a, b)
	// The title Label leads the content children so tree walks see the
	// title first (reading order for selection).
	list := g.ChildList()
	if len(list) != 3 {
		t.Fatalf("ChildList len = %d, want 3 (title + 2 children)", len(list))
	}
	if _, ok := list[0].(*Label); !ok {
		t.Errorf("ChildList[0] = %T, want *Label (title)", list[0])
	}
	if list[1] != a || list[2] != b {
		t.Errorf("ChildList content = %v/%v, want a/b", list[1], list[2])
	}
}

func TestFieldSetTitleIsSelectableAndCopyable(t *testing.T) {
	fc := withFakeClipboard(t)

	w := NewTestWindow(Size{W: 300, H: 200})
	body := NewLabel("body")
	g := NewFieldSet("Settings", FlexLayout{Direction: Vertical}, body)
	w.SetRoot(g)
	g.Layout(Rect{X: 0, Y: 0, W: 300, H: 200})
	g.ClearLayoutDirty()
	w.ClearDirtyRegion()

	// Drag across the title strip (title Label sits at y 4..26), then copy.
	w.DispatchTestEvent(NewMouseEvent(EventMouseDown, 12, 15, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseMove, 2000, 15, MouseButtonLeft, 0))
	w.DispatchTestEvent(NewMouseEvent(EventMouseUp, 2000, 15, MouseButtonLeft, 0))
	w.DispatchTestEvent(newCmdKeyDown(KeyC))

	if fc.buf != "Settings" {
		t.Fatalf("clipboard after title drag+copy = %q, want %q", fc.buf, "Settings")
	}
}
