package htmlcss

import (
	"sync"
	"testing"

	"github.com/qizhanchan/qui"
)

// The AX / DOM introspection surface is read from the agent's HTTP goroutine
// while the app keeps re-rendering on the main one. Attributes are rewritten
// on every render (SetManagedAttrs), so an unguarded read of the attribute map
// is not a benign data race — Go kills the process with "concurrent map read
// and map write". Run under -race, this pins the guard in place.
func TestAXReadersSurviveConcurrentAttrWrites(t *testing.T) {
	eng := NewStyleEngine("")
	app := eng.NewEl("div")
	app.SetElementID("app")
	in := eng.NewEl("input")
	in.SetAttr("type", "checkbox")
	in.SetElementID("flag")
	btn := eng.NewEl("button")
	btn.SetAttr("aria-label", "Save")
	app.SetElementChildren([]qui.Widget{in, btn})
	eng.SetRoot(app)

	win := qui.NewTestWindow(qui.Size{W: 400, H: 300})
	win.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{X: 0, Y: 0, W: 400, H: 300})

	const rounds = 300
	var wg sync.WaitGroup
	wg.Add(2)
	// "Main goroutine": a re-render rewriting the managed attributes.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			pressed := "true"
			if i%2 == 0 {
				pressed = "false"
			}
			btn.SetManagedAttrs(map[string]string{
				"aria-label":   "Save",
				"aria-pressed": pressed,
				"title":        "Save the document",
			})
			in.SetManagedAttrs(map[string]string{"type": "checkbox", "aria-label": "Flag"})
			in.SetDisabled(i%3 == 0)
		}
	}()
	// "Agent goroutine": the attribute-derived AX accessors. The full tree
	// walk (and the DOM projection over it) reads child slices and text as
	// well, which no per-element lock can cover — that one is serialized
	// onto the main goroutine instead, by qui.Window.AccessibilityTreeSynced.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			_ = btn.Role()
			_ = btn.AccessibleName()
			_ = btn.AccessibleState()
			_ = btn.AccessibleShortcut()
			_ = btn.AccessibleHasPopup()
			_ = in.Role()
			_ = in.AccessibleState()
		}
	}()
	wg.Wait()

	// The element still reports what the last render set.
	if got := in.Role(); got != qui.RoleCheckbox {
		t.Errorf("input role = %q, want %q", got, qui.RoleCheckbox)
	}
	if got := btn.AccessibleName(); got != "Save" {
		t.Errorf("button name = %q, want Save", got)
	}
}
