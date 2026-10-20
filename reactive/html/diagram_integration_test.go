package html_test

import (
	"fmt"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/diagram"
	"github.com/qizhanchan/qui/reactive"
	h "github.com/qizhanchan/qui/reactive/html"
)

func TestDiagramRunsAsReactiveLeafAndMutatesInPlace(t *testing.T) {
	win := qui.NewTestWindow(qui.Size{W: 620, H: 400})
	makeDoc := func() *diagram.Document {
		doc := diagram.NewDocument(500, 300)
		_ = doc.Active().AddCell(&diagram.Cell{
			ID: "n", Type: diagram.CellVertex, Label: "Node 0",
			Geometry: diagram.Geometry{X: 170, Y: 110, W: 160, H: 70},
			Style:    diagram.Style{Shape: diagram.ShapeRoundedRect, Fill: "#dae8fc", Stroke: "#6c8ebf"},
		})
		return doc
	}
	rt := h.Mount(win, `.wrap { width: 600px; height: 380px; } .diagram { width: 560px; height: 300px; }`, func() h.Node {
		value, setValue := reactive.UseState(0)
		return h.Div(
			h.Button("rename").Class("rename").OnClick(func() { setValue(value + 1) }),
			h.Leaf("x-diagram", "diagram",
				func() *diagram.Diagram { return diagram.NewEditor(makeDoc()) },
				func(d *diagram.Diagram) reactive.Flags {
					label := fmt.Sprintf("Node %d", value)
					d.Mutate(func(doc *diagram.Document) bool {
						if c := doc.Cell("n"); c != nil {
							c.Label = label
							return true
						}
						return false
					})
					return reactive.FlagNone
				},
			).Class("diagram"),
		).Class("wrap")
	})
	layoutAll(win, rt.Root())
	el := findClass(rt.Root(), "diagram")
	if el == nil {
		t.Fatal("diagram leaf not found")
	}
	d, ok := el.HostedWidget().(*diagram.Diagram)
	if !ok {
		t.Fatalf("hosted widget = %T", el.HostedWidget())
	}
	if got := d.Document().Cell("n").Label; got != "Node 0" {
		t.Fatalf("initial label = %q", got)
	}
	button := findClass(rt.Root(), "rename")
	x, y := center(button)
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseDown, x, y, qui.MouseButtonLeft, 0))
	win.DispatchTestEvent(qui.NewMouseEvent(qui.EventMouseUp, x, y, qui.MouseButtonLeft, 0))
	layoutAll(win, rt.Root())
	if got := d.Document().Cell("n").Label; got != "Node 1" {
		t.Fatalf("updated label = %q", got)
	}
}
