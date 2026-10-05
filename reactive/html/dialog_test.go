package html_test

import (
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// findClass returns the first El under w whose class list has cls.
func findClass(w qui.Widget, cls string) *htmlcss.El {
	if el, ok := w.(*htmlcss.El); ok {
		if c, _ := el.Attr("class"); strings.Contains(" "+c+" ", " "+cls+" ") {
			return el
		}
	}
	if cl, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, c := range cl.ChildList() {
			if el := findClass(c, cls); el != nil {
				return el
			}
		}
	}
	return nil
}

func layoutAll(win *qui.Window, root qui.Widget) {
	root.Measure(win.Size())
	root.Layout(qui.Rect{W: win.Size().W, H: win.Size().H})
}

func TestDialogShellStructureRoleAndCSS(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	h.Mount(win, `div { background: #000000; }`, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{
			Title:       "Rename",
			Body:        h.P("body"),
			Actions:     []h.Node{h.Button("OK")},
			OnDismiss:   func() {},
			CloseButton: true,
		}))
	})
	ovs := win.Overlays()
	if len(ovs) != 1 {
		t.Fatalf("overlays = %d, want 1", len(ovs))
	}
	host := ovs[0]
	if r := host.(interface{ Role() string }).Role(); r != qui.RoleDialog {
		t.Errorf("overlay role = %q, want dialog", r)
	}
	if n := host.(interface{ AccessibleName() string }).AccessibleName(); n != "Rename" {
		t.Errorf("overlay name = %q, want Rename", n)
	}
	for _, cls := range []string{"q-dialog", "q-dialog-header", "q-dialog-title", "q-dialog-close", "q-dialog-body", "q-dialog-actions"} {
		if findClass(host, cls) == nil {
			t.Errorf("missing .%s", cls)
		}
	}
	// A bare author `div` rule beats the framework's `.q-dialog`
	// background, specificity notwithstanding.
	box := findClass(host, "q-dialog")
	if got := box.Style().Background; got != (qui.Color{A: 1}) {
		t.Errorf("author div background did not override framework CSS: %+v", got)
	}
	// The framework look applies where the author is silent.
	if got := box.Style().Radius; got != 8 {
		t.Errorf("framework radius = %v, want 8", got)
	}
}

func TestDialogEnterConfirmsButTextareaKeepsEnter(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	confirmed := 0
	rt := h.Mount(win, ``, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{
			Title:     "Note",
			Body:      h.Textarea("").Class("note").Autofocus(),
			OnDismiss: func() {},
			OnConfirm: func() { confirmed++ },
		}))
	})
	layoutAll(win, rt.Root())
	win.DrainJobsForTest()

	note := findClass(win.Overlays()[0], "note")
	if note == nil || win.Focused() != note.TextTarget() {
		t.Fatalf("autofocus: focused=%v, want the textarea", win.Focused())
	}
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	if confirmed != 0 {
		t.Errorf("Enter in a focused textarea confirmed the dialog")
	}

	win.SetFocus(nil) // e.g. nothing focusable: the key reaches the dialog
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	if confirmed != 1 {
		t.Errorf("unconsumed Enter confirmed %d times, want 1", confirmed)
	}
}

func TestDialogEscapeReachesFocusedChildFirst(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	dismissed, childEsc := 0, 0
	rt := h.Mount(win, ``, func() h.Node {
		return h.Div(h.Dialog(h.DialogProps{
			Body: h.Div("editor").Class("editor").OnKeyDown(func(k qui.KeyEvent) bool {
				if k.Key == qui.KeyEscape {
					childEsc++
					return true
				}
				return false
			}),
			OnDismiss: func() { dismissed++ },
		}))
	})
	layoutAll(win, rt.Root())
	editor := findClass(win.Overlays()[0], "editor")
	win.SetFocus(editor)

	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, 0))
	if childEsc != 1 || dismissed != 0 {
		t.Errorf("child handled %d, dismissed %d: want the focused child to take Escape", childEsc, dismissed)
	}
	win.SetFocus(nil)
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEscape, 0))
	if dismissed != 1 {
		t.Errorf("unconsumed Escape dismissed %d times, want 1", dismissed)
	}
}

func TestButtonKeyboardFocusAndPress(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	clicks := 0
	rt := h.Mount(win, ``, func() h.Node {
		return h.Div(
			h.Input("").Class("field"),
			h.Button("Go").Class("go").OnClick(func() { clicks++ }),
		)
	})
	root := rt.Root()
	layoutAll(win, root)
	field := findClass(root, "field")
	btn := findClass(root, "go")

	field.RequestFocus()
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyTab, 0))
	if win.Focused() != qui.Widget(btn) {
		t.Fatalf("Tab from the field focused %v, want the button", win.Focused())
	}
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeySpace, 0))
	win.DispatchTestEvent(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	if clicks != 2 {
		t.Errorf("Space + Enter on the focused button clicked %d times, want 2", clicks)
	}

	// A mouse click presses the button but leaves focus in the field.
	field.RequestFocus()
	b := btn.Bounds()
	x, y := b.X+b.W/2, b.Y+b.H/2
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	if clicks != 3 {
		t.Errorf("mouse click: clicks=%d, want 3", clicks)
	}
	if win.Focused() != field.TextTarget() {
		t.Errorf("clicking the button moved focus to %v; want it to stay in the field", win.Focused())
	}
}

func TestDialogInheritsFromDeclaringTree(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 800, H: 600})
	css := `
		:root { padding: 40px; }
		.app { --ink: #ff0000; color: var(--ink); }
		.app.dark .q-dialog { background: #000000; }
		.probe { color: var(--ink); }
	`
	var setDark func(bool)
	rt := h.Mount(win, css, func() h.Node {
		dark, set := reactive.UseState(true)
		setDark = set
		cls := "app"
		if dark {
			cls = "app dark"
		}
		return h.Div(
			h.P("page"),
			h.Dialog(h.DialogProps{Title: "T", Body: h.P("x").Class("probe"), OnDismiss: func() {}}),
		).Class(cls)
	})
	host := win.Overlays()[0]
	box := findClass(host, "q-dialog")
	if got := box.Style().Background; got != (qui.Color{A: 1}) {
		t.Errorf(".app.dark .q-dialog background = %+v, want black", got)
	}
	// Custom properties declared on .app resolve inside the dialog.
	red := qui.Color{R: 1, A: 1}
	probe := findClass(host, "probe")
	if got := probe.ChildList()[0].Style().Foreground; got != red {
		t.Errorf("var(--ink) from .app inside the dialog = %+v, want red", got)
	}
	// :root is the document root only — not every portal root.
	if got := box.Style().Padding.Top; got == 40 {
		t.Error(":root rule matched the dialog box")
	}

	// Toggling the ancestor class restyles the open dialog.
	setDark(false)
	rt.Flush()
	if got := findClass(win.Overlays()[0], "q-dialog").Style().Background; got == (qui.Color{A: 1}) {
		t.Error("dialog kept the dark background after .dark was removed")
	}
}
