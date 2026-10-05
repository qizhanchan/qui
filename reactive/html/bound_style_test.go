package html_test

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

// Elements mounted by Show / For live in a plain container the host's
// style engine cannot see through. They must still cascade from the
// declaring element: inherited custom properties, ancestor selectors,
// and a restyle when an ancestor's class flips.
func TestBoundChildrenCascadeFromDeclaringHost(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	css := `
		.app { --muted: #ff0000; }
		.app.dark { --muted: #0000ff; }
		.empty { color: var(--muted); }
		.list > .empty { padding: 7px; }
		.row { color: var(--muted); }
	`
	red := qui.Color{R: 1, A: 1}
	blue := qui.Color{B: 1, A: 1}

	shown := reactive.NewSignal(true)
	late := reactive.NewSignal(false)
	rows := reactive.NewSignal([]string{"a"})
	var setDark func(bool)
	rt := h.Mount(win, css, func() h.Node {
		dark, set := reactive.UseState(false)
		setDark = set
		cls := "app"
		if dark {
			cls = "app dark"
		}
		return h.Div(
			h.Div(
				h.Show("empty", shown, func() h.Node {
					return h.P("No files dropped yet.").Class("empty")
				}),
				h.Show("late", late, func() h.Node {
					return h.P("late").Class("empty late")
				}),
				h.For("rows", rows, func(_ int, s string) h.Node {
					return h.Span(s).Class("row").Key(s)
				}),
			).Class("list"),
		).Class(cls)
	})
	root := rt.Root()
	fg := func(cls string) qui.Color {
		t.Helper()
		el := findClass(root, cls)
		if el == nil {
			t.Fatalf("no element with class %q", cls)
		}
		return el.ChildList()[0].Style().Foreground
	}

	empty := findClass(root, "empty")
	if got := fg("empty"); got != red {
		t.Errorf("Show child var(--muted) = %+v, want red from .app", got)
	}
	if got := empty.Style().Padding.Top; got != 7 {
		t.Errorf("`.list > .empty` padding = %v, want 7 (ancestor selector through the Show container)", got)
	}
	if got := fg("row"); got != red {
		t.Errorf("For row var(--muted) = %+v, want red", got)
	}

	// Mounted later by a scoped signal sync rather than the first pass.
	late.Set(true)
	win.DrainJobsForTest()
	if got := fg("late"); got != red {
		t.Errorf("late Show child var(--muted) = %+v, want red", got)
	}
	rows.Set([]string{"a", "b"})
	win.DrainJobsForTest()
	if el := findClass(root, "row"); el == nil {
		t.Fatal("rows vanished")
	}

	// Flipping the ancestor's class re-themes every bound child.
	setDark(true)
	rt.Flush()
	win.DrainJobsForTest()
	if got := fg("empty"); got != blue {
		t.Errorf("Show child after .app.dark = %+v, want blue", got)
	}
	if got := fg("late"); got != blue {
		t.Errorf("late Show child after .app.dark = %+v, want blue", got)
	}
	var rowColors []qui.Color
	collectRows(root, &rowColors)
	if len(rowColors) != 2 {
		t.Fatalf("found %d rows, want 2", len(rowColors))
	}
	for i, c := range rowColors {
		if c != blue {
			t.Errorf("row %d after .app.dark = %+v, want blue", i, c)
		}
	}
}

func collectRows(w qui.Widget, out *[]qui.Color) {
	if el := findClass(w, "row"); el == w {
		*out = append(*out, el.ChildList()[0].Style().Foreground)
		return
	}
	if cl, ok := w.(interface{ ChildList() []qui.Widget }); ok {
		for _, c := range cl.ChildList() {
			collectRows(c, out)
		}
	}
}
