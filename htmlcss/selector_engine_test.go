package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// One selector string addresses both surfaces: the widget grammar first,
// CSS for whatever it rejects, with DOM and AX nodes sharing a NodeID.
func TestSelectorEngine_CSSThroughRootFind(t *testing.T) {
	eng := NewStyleEngine(`.btn { padding: 4px }`)
	app := eng.NewEl("div")
	app.SetElementID("app")
	clicked := 0
	btn := eng.NewEl("button")
	btn.SetClass("btn save")
	btn.SetTextContent("Save")
	btn.SetOnClick(func() { clicked++ })
	app.SetElementChildren([]qui.Widget{btn})
	eng.SetRoot(app)
	w := qui.NewTestWindow(qui.Size{W: 300, H: 200})
	w.SetRoot(app)
	eng.Restyle()
	app.Layout(qui.Rect{W: 300, H: 200})

	for _, sel := range []string{"button.save", "#app > .btn", `button:has-text("save")`, btn.NodeID().String()} {
		got, err := w.Find(sel)
		if err != nil || got != btn {
			t.Errorf("Find(%q) = %v, %v; want the button", sel, got, err)
		}
	}
	if err := w.Click(".save", qui.ClickOptions{}); err != nil || clicked != 1 {
		t.Fatalf("Click(.save) err=%v clicked=%d", err, clicked)
	}

	dom := InspectWindow(w)
	nodes, _ := dom.Query("button")
	if len(nodes) != 1 || nodes[0].NodeID != btn.NodeID() {
		t.Fatalf("DOM nodeId = %v, want %d", nodes, btn.NodeID())
	}
	if dom.Root.NodeID == 0 || dom.Root.NodeID != w.DocumentNodeID() {
		t.Fatalf("#document nodeId = %d, want %d", dom.Root.NodeID, w.DocumentNodeID())
	}
}
