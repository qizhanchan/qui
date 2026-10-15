package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// An <input>'s keydown listener runs before the field's default action, as
// in the DOM: Esc reaches it even while text is selected (the bare field
// would spend that Esc dropping the selection and swallow it), and a
// handler that consumes Enter keeps the field from submitting.
func TestInputKeyDownRunsBeforeControl(t *testing.T) {
	eng := NewStyleEngine(``)
	root := eng.NewEl("div")
	in := eng.NewEl("input")
	in.SetAttr("value", "main.go")
	root.SetElementChildren([]qui.Widget{in})
	eng.SetRoot(root)
	win := qui.NewTestWindow(qui.Size{W: 400, H: 100})
	win.SetRoot(root)
	eng.Restyle()
	root.Layout(qui.Rect{W: 400, H: 100})

	in.ensureBacking()
	field := in.backing.(*widgets.Input)
	submitted := 0
	in.SetOnSubmit(func(string) { submitted++ })
	var seen []qui.Key
	in.SetOnKeyDown(func(ke qui.KeyEvent) bool {
		seen = append(seen, ke.Key)
		return ke.Key == qui.KeyEscape || ke.Key == qui.KeyEnter
	})
	win.SetFocus(field)
	field.SelectRange(0, 4)

	if err := win.SendKey("", qui.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := win.SendKey("", qui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != qui.KeyEscape || seen[1] != qui.KeyEnter {
		t.Fatalf("keydown saw %v, want [Escape Enter] (once each)", seen)
	}
	if submitted != 0 {
		t.Error("a consumed Enter still submitted the field")
	}

	// Keys the handler leaves alone keep their default action, and the
	// handler is not called a second time while the key bubbles.
	seen = nil
	if err := win.SendKey("", qui.KeyHome, 0); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 {
		t.Fatalf("Home seen %d times, want 1", len(seen))
	}
}
