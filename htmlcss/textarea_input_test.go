package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// A <textarea> built through the live-element API must deliver keystrokes to
// its SetOnInput handler. reactive/html wires every form handler through
// SetOnInput, so a textarea that drops it silently breaks every dialog that
// edits multi-line text.
func TestTextareaOnInputFires(t *testing.T) {
	eng := NewStyleEngine("")
	app := eng.NewEl("div")
	app.SetElementID("app")
	ta := eng.NewEl("textarea")
	ta.SetElementID("body")
	var got []string
	ta.SetOnInput(func(v string) { got = append(got, v) })
	app.SetElementChildren([]qui.Widget{ta})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	if err := win.Type("#body", "hi", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type(#body): %v", err)
	}
	if got2 := ta.InputValue(); got2 != "hi" {
		t.Fatalf("textarea value = %q, want hi", got2)
	}
	if len(got) == 0 {
		t.Fatal("OnInput never fired")
	}
	if got[len(got)-1] != "hi" {
		t.Errorf("OnInput calls = %v, want the last to be \"hi\"", got)
	}
}

// The same, with the handler installed BEFORE the element ever renders (the
// order reactive/html uses: the apply hook runs on the first pass, when the
// backing control does not exist yet).
func TestTextareaOnInputWiredBeforeBacking(t *testing.T) {
	eng := NewStyleEngine("")
	ta := eng.NewEl("textarea")
	ta.SetElementID("body")
	var fired int
	ta.SetOnInput(func(string) { fired++ })
	app := eng.NewEl("div")
	app.SetElementChildren([]qui.Widget{ta})
	eng.SetRoot(app)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})
	if err := win.Type("#body", "x", qui.TypeOptions{}); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if fired == 0 {
		t.Error("OnInput never fired for a handler installed before the backing existed")
	}
}
