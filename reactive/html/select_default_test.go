package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
	"github.com/qizhanchan/qui/widgets"
)

func selectBacking(t *testing.T, root qui.Widget) *widgets.Select {
	t.Helper()
	el := findTag(root, "select")
	if el == nil {
		t.Fatal("no <select> in the tree")
	}
	for _, c := range el.ChildList() {
		if sel, ok := c.(*widgets.Select); ok {
			return sel
		}
	}
	t.Fatalf("<select> has no Select widget (children: %v)", el.ChildList())
	return nil
}

// h.Select without .Selected shows its first option, as a browser does; an
// explicit .Selected(-1) keeps it blank and a user pick survives a re-render.
func TestSelectDefaultsToFirstOption(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	var rerender func(int)
	rt := h.Mount(win, ``, func() h.Node {
		n, set := reactive.UseState(0)
		rerender = set
		_ = n
		return h.Div(
			h.Div(h.Select("Arial", "Times")).Class("plain"),
			h.Div(h.Select("Arial", "Times").Selected(-1)).Class("blank"),
			h.Div(h.Select("Arial", "Times").Selected(1)).Class("second"),
		)
	})
	root := rt.Root().(*htmlcss.El)
	want := map[string]int{"plain": 0, "blank": -1, "second": 1}
	check := func(stage string) {
		for cls, idx := range want {
			if got := selectBacking(t, findClass(root, cls)).SelectedIdx; got != idx {
				t.Errorf("%s: .%s SelectedIdx = %d, want %d", stage, cls, got, idx)
			}
		}
	}
	check("mount")

	sel := selectBacking(t, findClass(root, "plain"))
	sel.SelectedIdx = 1 // the user picked Times
	want["plain"] = 1
	rerender(1)
	rt.Flush()
	check("re-render")
}

// A dialog opened after the first frame builds its <select> into a subtree
// that has no window until the portal attaches it. The widget must exist as
// soon as it is mounted — a headless window never runs the frame that used
// to create it — and show the first option.
func TestSelectInLateDialogHasWidgetOnMount(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	var setOpen func(bool)
	rt := h.Mount(win, ``, func() h.Node {
		open, set := reactive.UseState(false)
		setOpen = set
		var dlg h.Node
		if open {
			dlg = h.Dialog(h.DialogProps{Title: "Font", Body: h.Div(h.Select("Arial", "Times"))})
		}
		return h.Div(h.P("page"), dlg)
	})
	rt.Flush()
	setOpen(true)
	rt.Flush()

	ovs := win.Overlays()
	if len(ovs) != 1 {
		t.Fatalf("overlays = %d, want the dialog", len(ovs))
	}
	if sel := selectBacking(t, ovs[0]); sel.SelectedIdx != 0 {
		t.Errorf("late dialog select SelectedIdx = %d, want 0", sel.SelectedIdx)
	}
}
