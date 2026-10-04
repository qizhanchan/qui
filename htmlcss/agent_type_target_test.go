package htmlcss

import (
	"errors"
	"testing"

	"github.com/qizhanchan/qui"
)

// A custom leaf widget hosted inside an El tree — the shape apps use for
// canvas-like surfaces (q-excel's sheet grid, editors, chart panes): it holds
// focus itself and consumes CharEvent to start its own inline editing.
type charSink struct {
	qui.BaseWidget
	typed   string
	focused bool
}

func newCharSink() *charSink {
	c := &charSink{BaseWidget: qui.NewBaseWidget()}
	c.SetSelf(c)
	return c
}

func (c *charSink) Measure(qui.Size) qui.Size { return qui.Size{W: 100, H: 40} }
func (c *charSink) Draw(qui.Canvas)           {}
func (c *charSink) Focusable() bool           { return c.Enabled() }
func (c *charSink) SetFocused(f bool)         { c.focused = f }

func (c *charSink) Handle(event qui.Event) bool {
	if ce, ok := event.(qui.CharEvent); ok {
		c.typed += string(ce.Rune)
		return true
	}
	return false
}

// typeTargetFixture mounts an htmlcss root holding a plain div (the "menu bar"
// at the top of the page) plus a custom focusable widget:
//
//	div#app
//	  div#bar     "menu"
//	  charSink#grid
func typeTargetFixture(t *testing.T) (*qui.Window, *El, *El, *charSink) {
	t.Helper()
	eng := NewStyleEngine(`#app { display: flex; flex-direction: column }`)
	app := eng.NewEl("div")
	app.SetElementID("app")

	bar := eng.NewEl("div")
	bar.SetElementID("bar")
	bar.SetTextContent("menu")

	grid := newCharSink()
	grid.SetID("grid")

	app.SetElementChildren([]qui.Widget{bar, grid})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	return win, app, bar, grid
}

// childGeometry snapshots the laid-out bounds of the root element's children,
// which is what a stray text node injected into the root would shift.
func childGeometry(root *El) []qui.Rect {
	var out []qui.Rect
	for _, c := range root.ChildList() {
		out = append(out, c.Bounds())
	}
	return out
}

func sameGeometry(a, b []qui.Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The Type action on a non-text widget must reach that widget — never fall
// back to replacing an ancestor element's text content, which used to inject
// a phantom text line at the top of the root El and push the whole page down.
func TestTypeOnCustomWidgetDoesNotMutateRootElement(t *testing.T) {
	win, app, _, grid := typeTargetFixture(t)
	before := childGeometry(app)

	if err := win.Type("#grid", "41", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#grid): %v", err)
	}
	if grid.typed != "41" {
		t.Errorf("target received %q, want %q", grid.typed, "41")
	}
	if got := app.TextContent(); got != "" {
		t.Errorf("root element text = %q, want empty (no phantom text node)", got)
	}
	if after := childGeometry(app); !sameGeometry(before, after) {
		t.Errorf("root child geometry changed: before %v, after %v", before, after)
	}
}

// An element that isn't a text control has no business absorbing typed text:
// the action must fail loudly instead of rewriting the element's content.
func TestTypeOnPlainElementFails(t *testing.T) {
	win, app, bar, _ := typeTargetFixture(t)
	before := childGeometry(app)

	err := win.Type("#bar", "oops", qui.TypeOptions{})
	if !errors.Is(err, qui.ErrNotTextTarget) {
		t.Fatalf("err = %v, want ErrNotTextTarget", err)
	}
	if got := bar.TextContent(); got != "menu" {
		t.Errorf("element text = %q, want unchanged %q", got, "menu")
	}
	if after := childGeometry(app); !sameGeometry(before, after) {
		t.Errorf("root child geometry changed: before %v, after %v", before, after)
	}
}

// An empty target used to match every node, so the action silently landed on
// the root element (the shape of an agent request that misnames the field).
func TestTypeWithEmptyTargetFails(t *testing.T) {
	win, app, _, _ := typeTargetFixture(t)
	before := childGeometry(app)

	if err := win.Type("", "41", qui.TypeOptions{}); err == nil {
		t.Fatal("Type with empty target returned nil error")
	}
	if got := app.TextContent(); got != "" {
		t.Errorf("root element text = %q, want empty", got)
	}
	if after := childGeometry(app); !sameGeometry(before, after) {
		t.Errorf("root child geometry changed: before %v, after %v", before, after)
	}
}

// A text <input> element renders through a backing Input widget; addressing
// the ELEMENT by id must drive that control's value, not the element's text.
func TestTypeOnInputElementReachesBacking(t *testing.T) {
	eng := NewStyleEngine("")
	app := eng.NewEl("div")
	app.SetElementID("app")
	in := eng.NewEl("input")
	in.SetAttr("type", "text")
	in.SetElementID("email")
	app.SetElementChildren([]qui.Widget{in})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if err := win.Type("#email", "a@b.c", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#email): %v", err)
	}
	if got := in.InputValue(); got != "a@b.c" {
		t.Errorf("input value = %q, want a@b.c", got)
	}
	if got := in.TextContent(); got != "" {
		t.Errorf("element text = %q, want empty", got)
	}
}

// llm.txt documents picking a <select> option with the `type` action: the
// element hands the text to its backing Select, which matches it against
// the option labels. The element's own content must stay untouched.
func TestTypeOnSelectElementPicksOption(t *testing.T) {
	eng := NewStyleEngine("")
	app := eng.NewEl("div")
	app.SetElementID("app")
	sel := eng.NewEl("select")
	sel.SetElementID("method")
	sel.SetSelectOptions([]string{"GET", "POST", "PUT"})
	app.SetElementChildren([]qui.Widget{sel})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if err := win.Type("#method", "POST", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#method): %v", err)
	}
	// EditableTarget is the DOM surface's own hop to the control; both
	// surfaces must land on the same Select.
	control := EditableTarget(sel)
	if control == qui.Widget(sel) {
		t.Fatalf("EditableTarget did not unwrap the select element")
	}
	if got := qui.WidgetValue(control); got != "POST" {
		t.Errorf("select value = %q, want POST", got)
	}
	if got := sel.TextContent(); got != "" {
		t.Errorf("element text = %q, want empty", got)
	}
}
