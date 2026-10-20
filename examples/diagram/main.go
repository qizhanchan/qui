// Command diagram is the interactive gallery/editor for graphs/diagram.
// It exercises the public document model and the retained Diagram widget:
// add shapes, drag, resize, connect ports, edit labels, undo/redo,
// pan/zoom/fit, and export through the same native renderer.
package main

import (
	"fmt"
	"log"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/agent"
	"github.com/qizhanchan/qui/graphs/diagram"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — Native Diagram Editor", 1320, 860)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	doc := starterDocument()
	ed := diagram.New(diagram.Option{
		Document: doc, Mode: diagram.ModeEdit, Grid: true,
		Snap: true, SnapSize: 10, Guides: true, Fit: true, Padding: 28,
	})
	ed.SetID("diagram-editor")
	ed.OnChange = func(*diagram.Document) { window.Invalidate() }
	ed.OnSelect = func(sel []*diagram.Cell) { _ = sel }

	title := widgets.NewLabel("Native Diagram Editor")
	title.Style().Font.Size = 24
	title.Style().Font.Weight = qui.FontWeightBold
	title.Style().Foreground = qui.Color{R: .09, G: .13, B: .20, A: 1}

	hint := widgets.NewLabel("Drag · resize (snaps to alignment guides; hold ⌥ to bypass) · connect ports · double-click labels · ⌘Z / ⇧⌘Z")
	hint.Style().Font.Size = 12
	hint.Style().Foreground = qui.Color{R: .38, G: .45, B: .54, A: 1}

	status := widgets.NewLabel("")
	status.Style().Font.Size = 12
	status.Style().Foreground = qui.Color{R: .24, G: .50, B: .72, A: 1}
	refreshStatus := func() {
		sel := ed.SelectedCells()
		if len(sel) == 0 {
			status.SetText(ed.AccessibleValue())
			return
		}
		status.SetText(fmt.Sprintf("%s · %s", ed.AccessibleValue(), diagram.DescribeCell(sel[0])))
	}
	ed.OnChange = func(*diagram.Document) { refreshStatus(); window.Invalidate() }
	ed.OnSelect = func([]*diagram.Cell) { refreshStatus(); window.Invalidate() }

	addAtCenter := func(label string, shape diagram.ShapeKind) {
		b := ed.Bounds()
		r := qui.Rect{X: b.W/2 - 80, Y: b.H/2 - 30, W: 160, H: 60}
		ed.AddVertex(label, shape, diagram.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H})
		window.Invalidate()
	}
	actions := []qui.Widget{
		actionButton("Process", func() { addAtCenter("Process", diagram.ShapeRect) }),
		actionButton("Decision", func() { addAtCenter("Decision", diagram.ShapeDiamond) }),
		actionButton("Database", func() { addAtCenter("Database", diagram.ShapeCylinder) }),
		actionButton("Actor", func() { addAtCenter("Actor", diagram.ShapeActor) }),
		actionButton("Cloud", func() { addAtCenter("Cloud", diagram.ShapeCloud) }),
		actionButton("Delete", func() {
			ed.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDelete, 0))
		}),
		actionButton("Undo", func() { ed.Undo() }),
		actionButton("Redo", func() { ed.Redo() }),
		actionButton("−", func() { ed.ZoomOut() }),
		actionButton("+", func() { ed.ZoomIn() }),
		actionButton("Fit", func() { ed.Fit() }),
		actionButton("Copy JSON", func() {
			qui.SetClipboardText(copyDocument(ed))
			status.SetText("Copied the diagram as JSON — paste it into a bug report or chat")
		}),
		actionButton("Paste JSON", func() {
			if err := pasteDocument(ed, qui.GetClipboardText()); err != nil {
				status.SetText("Paste failed: " + err.Error())
				return
			}
			refreshStatus()
			window.Invalidate()
		}),
	}
	toolbar := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 8}, actions...)
	header := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 6}, title, hint, toolbar, status)
	header.Style().Padding = qui.Insets{Top: 16, Right: 18, Bottom: 10, Left: 18}

	canvasHost := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical}, ed)
	canvasHost.SetFlex(1)
	canvasHost.Style().Margin = qui.Insets{Top: 0, Right: 18, Bottom: 18, Left: 18}
	canvasHost.Style().Radius = 14
	canvasHost.Style().BorderSize = 1
	canvasHost.Style().Border = qui.Color{R: .84, G: .88, B: .93, A: 1}
	canvasHost.Style().Background = qui.Color{R: 1, G: 1, B: 1, A: 1}

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 8}, header, canvasHost)
	root.SetFlex(1)
	root.Style().Background = qui.Color{R: .96, G: .97, B: .985, A: 1}
	window.SetRoot(root)
	ed.Fit()
	refreshStatus()
	agent.BindEnv(window)
	app.Run()
}

// maxPasteBytes bounds what Paste JSON will try to decode.
const maxPasteBytes = 4 << 20

// copyDocument serializes the editor's document as native diagram JSON, the
// lossless form that reproduces a layout exactly (DecodeJSONBytes reads it).
func copyDocument(ed *diagram.Diagram) string {
	return string(diagram.EncodeJSON(ed.Document()))
}

// pasteDocument replaces the editor's document with clipboard JSON.
func pasteDocument(ed *diagram.Diagram, text string) error {
	if len(text) > maxPasteBytes {
		return fmt.Errorf("clipboard holds %d bytes, limit is %d", len(text), maxPasteBytes)
	}
	doc, err := diagram.DecodeJSONBytes([]byte(text))
	if err != nil {
		return err
	}
	ed.SetDocument(doc, true)
	return nil
}

func actionButton(label string, onClick func()) *widgets.Button {
	b := widgets.NewButton(label, onClick)
	b.Style().Font.Size = 12
	return b
}

func starterDocument() *diagram.Document {
	doc := diagram.NewDocument(1000, 640)
	p := doc.Active()
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(p.AddCell(&diagram.Cell{
		ID: "start", Type: diagram.CellVertex, Label: "Start",
		Geometry: diagram.Geometry{X: 420, Y: 40, W: 160, H: 60},
		Style:    diagram.Style{Shape: diagram.ShapeEllipse, Fill: "#d5e8d4", Stroke: "#82b366", FontFamily: "Arial", FontSize: 14},
	}))
	must(p.AddCell(&diagram.Cell{
		ID: "load", Type: diagram.CellVertex, Label: "Load input",
		Geometry: diagram.Geometry{X: 395, Y: 170, W: 210, H: 70},
		Style:    diagram.Style{Shape: diagram.ShapeRect, Fill: "#dae8fc", Stroke: "#6c8ebf", FontFamily: "Arial", FontSize: 14},
	}))
	must(p.AddCell(&diagram.Cell{
		ID: "valid", Type: diagram.CellVertex, Label: "Valid?",
		Geometry: diagram.Geometry{X: 420, Y: 315, W: 160, H: 90},
		Style:    diagram.Style{Shape: diagram.ShapeDiamond, Fill: "#fff2cc", Stroke: "#d6b656", FontFamily: "Arial", FontSize: 14},
	}))
	must(p.AddCell(&diagram.Cell{
		ID: "ok", Type: diagram.CellVertex, Label: "Process",
		Geometry: diagram.Geometry{X: 190, Y: 470, W: 180, H: 70},
		Style:    diagram.Style{Shape: diagram.ShapeRoundedRect, Fill: "#dae8fc", Stroke: "#6c8ebf", FontFamily: "Arial", FontSize: 14},
	}))
	must(p.AddCell(&diagram.Cell{
		ID: "err", Type: diagram.CellVertex, Label: "Show error",
		Geometry: diagram.Geometry{X: 630, Y: 470, W: 180, H: 70},
		Style:    diagram.Style{Shape: diagram.ShapeRoundedRect, Fill: "#f8cecc", Stroke: "#b85450", FontFamily: "Arial", FontSize: 14},
	}))
	connect := func(id, source, target string, route diagram.RouteKind, label string) {
		must(p.AddCell(&diagram.Cell{
			ID: id, Type: diagram.CellEdge, Label: label,
			Source: diagram.Endpoint{Cell: source, Port: diagram.PortAuto},
			Target: diagram.Endpoint{Cell: target, Port: diagram.PortAuto},
			Style:  diagram.Style{Route: route, Stroke: "#667085", StrokeW: 1.5, EndArrow: diagram.ArrowTriangle, FontFamily: "Arial", FontSize: 12},
		}))
	}
	connect("e1", "start", "load", diagram.RouteOrthogonal, "")
	connect("e2", "load", "valid", diagram.RouteOrthogonal, "")
	connect("e3", "valid", "ok", diagram.RouteRoundedOrthogonal, "yes")
	connect("e4", "valid", "err", diagram.RouteRoundedOrthogonal, "no")
	return doc
}
