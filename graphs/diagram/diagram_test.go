package diagram

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qizhanchan/qui"
)

func sampleDocument() *Document {
	doc := NewDocument(640, 360)
	page := doc.Active()
	_ = page.AddCell(&Cell{ID: "start", Type: CellVertex, Label: "Start", Geometry: Geometry{X: 50, Y: 60, W: 140, H: 56},
		Style: Style{Shape: ShapeRoundedRect, Fill: "#dae8fc", Stroke: "#6c8ebf", FontFamily: "Arial", FontSize: 14}})
	_ = page.AddCell(&Cell{ID: "check", Type: CellVertex, Label: "Valid?", Geometry: Geometry{X: 310, Y: 45, W: 160, H: 90},
		Style: Style{Shape: ShapeDiamond, Fill: "#fff2cc", Stroke: "#d6b656", FontFamily: "Arial", FontSize: 14}})
	_ = page.AddCell(&Cell{ID: "db", Type: CellVertex, Label: "Database", Geometry: Geometry{X: 230, Y: 230, W: 140, H: 80},
		Style: Style{Shape: ShapeCylinder, Fill: "#d5e8d4", Stroke: "#82b366", FontFamily: "Arial", FontSize: 14}})
	_ = page.AddCell(&Cell{ID: "e1", Type: CellEdge, Source: Endpoint{Cell: "start", Port: PortEast}, Target: Endpoint{Cell: "check", Port: PortWest},
		Style: Style{Route: RouteOrthogonal, Stroke: "#666666", EndArrow: ArrowTriangle}})
	_ = page.AddCell(&Cell{ID: "e2", Type: CellEdge, Label: "yes", Source: Endpoint{Cell: "check", Port: PortSouth}, Target: Endpoint{Cell: "db", Port: PortNorth},
		Style: Style{Route: RouteRoundedOrthogonal, Stroke: "#666666", EndArrow: ArrowTriangle}})
	return doc
}

func TestAllBuiltInShapesRenderAndHitTest(t *testing.T) {
	kinds := []ShapeKind{ShapeRect, ShapeRoundedRect, ShapeEllipse, ShapeDiamond, ShapeParallelogram, ShapeHexagon, ShapeCylinder, ShapeDocument, ShapeActor, ShapeCloud, ShapeNote, ShapeText}
	doc := NewDocument(1200, 600)
	page := doc.Active()
	for i, kind := range kinds {
		x := float32((i%4)*280 + 30)
		y := float32((i/4)*180 + 30)
		_ = page.AddCell(&Cell{ID: string(kind), Type: CellVertex, Label: strings.ToUpper(string(kind)),
			Geometry: Geometry{X: x, Y: y, W: 200, H: 100}, Style: Style{Shape: kind, Fill: "#dae8fc", Stroke: "#6c8ebf", FontSize: 12}})
	}
	rec := &qui.RecordingCanvas{}
	drawScene(rec, doc, sceneConfig{
		Origin: Point{}, Viewport: Viewport{Scale: 1}, Clip: Rect{W: 1200, H: 600},
		Theme: DefaultTheme(), Selected: map[string]bool{},
	})
	if len(rec.Paths)+len(rec.Rounds)+len(rec.Fills) == 0 {
		t.Fatal("shape gallery emitted no shapes")
	}
	for _, kind := range kinds {
		c := page.Cell(string(kind))
		if c == nil {
			t.Fatalf("missing %s", kind)
		}
		center := c.Bounds().Center()
		if got := (&Diagram{doc: doc, theme: DefaultTheme(), vp: Viewport{Scale: 1}, selected: map[string]bool{}}).hitCell(center, 4); got == nil || got.ID != c.ID {
			t.Errorf("hit test missed %s center", kind)
		}
	}
}

func TestAllRoutesProduceGeometryAndAvoidTarget(t *testing.T) {
	doc := sampleDocument()
	page := doc.Active()
	check := page.Cell("check")
	db := page.Cell("db")
	styles := []RouteKind{RouteStraight, RouteOrthogonal, RouteRoundedOrthogonal, RouteEntityRelation, RouteCurved, RoutePolyline}
	for _, route := range styles {
		t.Run(string(route), func(t *testing.T) {
			e := &Cell{ID: "route", Type: CellEdge, Source: Endpoint{Cell: "check", Port: PortSouth}, Target: Endpoint{Cell: "db", Port: PortNorth}, Style: Style{Route: route}}
			r := page.Route(e)
			if len(r.Polyline) < 2 || r.Path == nil {
				t.Fatalf("route emitted %d points", len(r.Polyline))
			}
			if route != RouteStraight && route != RouteCurved {
				for i := 1; i < len(r.Polyline)-2; i++ {
					if shapeContains(ShapeRect, db.Bounds().Inflate(-1), r.Polyline[i]) {
						t.Errorf("interior point %d entered target bounds", i)
					}
				}
			}
			_ = check
		})
	}
	loop := &Cell{ID: "loop", Type: CellEdge, Source: Endpoint{Cell: "start"}, Target: Endpoint{Cell: "start"}, Style: Style{Route: RouteOrthogonal, EndArrow: ArrowTriangle}}
	if r := page.Route(loop); len(r.Polyline) < 4 {
		t.Fatalf("self loop has %d points: %+v", len(r.Polyline), r.Polyline)
	}
}

func TestDrawioImportExportRoundTrip(t *testing.T) {
	data, err := EncodeDrawio(sampleDocument())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeDrawio(data)
	if err != nil {
		t.Fatalf("decode exported XML: %v\n%s", err, data)
	}
	if len(doc.Pages) != 1 || len(doc.Pages[0].Cells) != 5 {
		t.Fatalf("round trip produced %d pages / %d cells", len(doc.Pages), len(doc.Pages[0].Cells))
	}
	check := doc.Cell("check")
	if check == nil || check.Style.Shape != ShapeDiamond {
		t.Fatalf("diamond style lost: %+v", check)
	}
	edge := doc.Cell("e2")
	if edge == nil || edge.Style.Route != RouteRoundedOrthogonal || edge.Target.Cell != "db" {
		t.Fatalf("edge lost: %+v", edge)
	}
	if edge.Label != "yes" {
		t.Fatalf("edge label = %q", edge.Label)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	doc := sampleDocument()
	got, err := DecodeJSONBytes(EncodeJSON(doc))
	if err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Cell("e1") == nil || got.Cell("e1").Source.Port != PortEast {
		t.Fatal("JSON did not preserve edge endpoint")
	}
}

func TestExportsPNGSVGPDF(t *testing.T) {
	doc := sampleDocument()
	pngData, err := PNGBytes(doc, RenderOptions{Width: 640, Height: 360, Fit: true})
	if err != nil || len(pngData) < 100 {
		t.Fatalf("PNG export: len=%d err=%v", len(pngData), err)
	}
	svgData, err := SVGBytes(doc, RenderOptions{Width: 640, Height: 360, Fit: true})
	if err != nil || !bytes.Contains(svgData, []byte("<svg")) {
		t.Fatalf("SVG export: %s err=%v", svgData, err)
	}
	pdfData, err := PDFBytes(doc, RenderOptions{Width: 640, Height: 360, Fit: true})
	if err != nil || !bytes.HasPrefix(pdfData, []byte("%PDF")) {
		t.Fatalf("PDF export: len=%d err=%v", len(pdfData), err)
	}
}

func newTestEditor() *Diagram {
	d := New(Option{Document: sampleDocument(), Mode: ModeEdit, Grid: true, Snap: true, SnapSize: 10})
	d.Layout(qui.Rect{W: 900, H: 600})
	d.vp = Viewport{Scale: 1}
	return d
}

func mouse(kind qui.EventType, x, y float32, mods qui.Modifiers) qui.MouseEvent {
	return qui.NewMouseEvent(kind, x, y, qui.MouseButtonLeft, mods)
}

func TestEditorDragResizeUndoRedo(t *testing.T) {
	d := newTestEditor()
	start := d.doc.Cell("start")
	d.Handle(mouse(qui.EventMouseDown, 120, 88, 0))
	if !d.selected["start"] {
		t.Fatal("node was not selected")
	}
	d.Handle(mouse(qui.EventMouseMove, 170, 128, 0))
	d.Handle(mouse(qui.EventMouseUp, 170, 128, 0))
	if start.Geometry.X <= 50 || start.Geometry.Y <= 60 {
		t.Fatalf("drag did not move node: %+v", start.Geometry)
	}
	if !d.Undo() {
		t.Fatal("undo unavailable")
	}
	if got := d.doc.Cell("start").Geometry; got.X != 50 || got.Y != 60 {
		t.Fatalf("undo restored %+v", got)
	}
	if !d.Redo() {
		t.Fatal("redo unavailable")
	}
	if d.doc.Cell("start").Geometry.X == 50 {
		t.Fatal("redo did not restore move")
	}
	// Resize from the selected node's bottom-right handle.
	c := d.doc.Cell("start")
	b := c.Bounds()
	d.Handle(mouse(qui.EventMouseDown, b.X+b.W, b.Y+b.H, 0))
	d.Handle(mouse(qui.EventMouseMove, b.X+b.W+40, b.Y+b.H+20, 0))
	d.Handle(mouse(qui.EventMouseUp, b.X+b.W+40, b.Y+b.H+20, 0))
	if c.Geometry.W <= b.W || c.Geometry.H <= b.H {
		t.Fatalf("resize failed: before=%+v after=%+v", b, c.Geometry)
	}
}

func TestEditorConnectorAndLabelEditing(t *testing.T) {
	d := newTestEditor()
	beforeEdges := len(d.doc.Active().Cells)
	// Reveal ports by hovering Start, then drag its east port to Database.
	d.Handle(mouse(qui.EventMouseMove, 190, 88, 0))
	d.Handle(mouse(qui.EventMouseDown, 190, 88, 0))
	if d.interact.mode != modeConnect {
		t.Fatalf("port drag did not start: hover=%q mode=%d", d.hoverID, d.interact.mode)
	}
	d.Handle(mouse(qui.EventMouseMove, 300, 270, 0))
	if d.interact.connectToCell != "db" {
		t.Fatalf("connector target = %q, want db", d.interact.connectToCell)
	}
	d.Handle(mouse(qui.EventMouseUp, 300, 270, 0))
	edges := 0
	for _, c := range d.doc.Active().Cells {
		if c.Type == CellEdge {
			edges++
		}
	}
	if edges != 3 {
		t.Fatalf("edge count = %d, want 3 (cells %d)", edges, beforeEdges)
	}

	// Double click the database node to edit its label.
	d.Handle(mouse(qui.EventMouseDown, 300, 270, 0))
	d.Handle(mouse(qui.EventMouseUp, 300, 270, 0))
	d.Handle(mouse(qui.EventMouseDown, 300, 270, 0))
	if d.editing == "" {
		t.Fatal("double click did not enter label edit")
	}
	for _, r := range []rune("-v2") {
		d.Handle(qui.NewCharEvent(r, 0))
	}
	d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyEnter, 0))
	if got := d.doc.Cell("db").Label; !strings.HasSuffix(got, "-v2") {
		t.Fatalf("label = %q", got)
	}
	if !d.Undo() {
		t.Fatal("label edit was not recorded")
	}
}

func TestEditorDeleteCopyPasteAndAccessibility(t *testing.T) {
	d := newTestEditor()
	d.Select("start")
	if got := d.AccessibleChildren(); len(got) != 5 {
		t.Fatalf("AX children = %d, want 5", len(got))
	} else if got[0].ID != "start" || got[0].Role != qui.RoleDiagramNode {
		t.Fatalf("unexpected AX child %+v", got[0])
	}
	fc := &fakeClipboard{}
	qui.SetClipboardProvider(fc)
	t.Cleanup(func() { qui.SetClipboardProvider(nil) })
	if !d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyC, qui.ModControl)) {
		t.Fatal("copy not handled")
	}
	if !d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyV, qui.ModControl)) {
		t.Fatal("paste not handled")
	}
	if got := len(d.doc.Active().Cells); got != 6 {
		t.Fatalf("cells after paste = %d, want 6", got)
	}
	d.SelectAll()
	d.Handle(qui.NewKeyEvent(qui.EventKeyDown, qui.KeyDelete, 0))
	if len(d.doc.Active().Cells) != 0 {
		t.Fatalf("delete left %d cells", len(d.doc.Active().Cells))
	}
}

func TestDefaultThemeColorsParse(t *testing.T) {
	th := DefaultTheme()
	for _, s := range []string{th.Background, th.Grid, th.Selection, th.PortFill, th.DefaultVertex.Fill, th.DefaultEdge.Stroke} {
		if _, ok := ParseColor(s); !ok {
			t.Errorf("theme color %q did not parse", s)
		}
	}
	if !bytes.Equal(mustPNG(t), mustPNG(t)) {
		t.Fatal("deterministic PNG changed")
	}
}

func mustPNG(t *testing.T) []byte {
	t.Helper()
	data, err := PNGBytes(sampleDocument(), RenderOptions{Width: 320, Height: 180, Fit: true})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fakeClipboard struct{ text string }

func (f *fakeClipboard) Get() string     { return f.text }
func (f *fakeClipboard) Set(text string) { f.text = text }

func TestRenderImageContainsPixels(t *testing.T) {
	img, err := RenderImage(sampleDocument(), RenderOptions{Width: 640, Height: 360})
	if err != nil {
		t.Fatal(err)
	}
	nonWhite := 0
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r != 65535 || g != 65535 || b != 65535 {
				nonWhite++
			}
		}
	}
	if nonWhite == 0 {
		t.Fatal("RenderImage produced a blank page")
	}
}

func TestCustomShapeRegistry(t *testing.T) {
	shapes := NewShapeRegistry()
	shapes.Register("triangleDown", ShapeDefinition{
		Draw: func(canvas qui.Canvas, rect Rect, paint ShapePaint) {
			p := qui.NewPath().MoveTo(rect.X, rect.Y).
				LineTo(rect.X+rect.W, rect.Y).
				LineTo(rect.X+rect.W/2, rect.Y+rect.H).Close()
			canvas.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: paint.Fill, AntiAlias: true})
		},
		Contains: func(rect Rect, p Point) bool {
			y := (p.Y - rect.Y) / rect.H
			half := rect.W * (1 - y) / 2
			return p.Y >= rect.Y && p.Y <= rect.Y+rect.H && abs32(p.X-(rect.X+rect.W/2)) <= half
		},
		Perimeter: func(rect Rect, toward Point) Point { return perimeterPoint(ShapeDiamond, rect, toward) },
	})
	doc := NewDocument(320, 200)
	_ = doc.Active().AddCell(&Cell{ID: "custom", Type: CellVertex, Label: "Custom", Geometry: Geometry{X: 80, Y: 40, W: 160, H: 100},
		Style: Style{Shape: "triangleDown", Fill: "#dae8fc", Stroke: "#6c8ebf"}})
	img, err := RenderImage(doc, RenderOptions{Width: 320, Height: 200, Shapes: shapes})
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := img.At(160, 80).RGBA()
	if r == 65535 && g == 65535 && b == 65535 {
		t.Fatal("custom shape did not paint")
	}
}

func TestOrthogonalAutoPortsUseVisibleBoundaryPorts(t *testing.T) {
	doc := sampleDocument()
	page := doc.Active()
	edge := &Cell{
		ID: "decision-out", Type: CellEdge,
		Source: Endpoint{Cell: "check", Port: PortAuto},
		Target: Endpoint{Cell: "db", Port: PortAuto},
		Style:  Style{Route: RouteRoundedOrthogonal},
	}
	r := page.Route(edge)
	src := page.Cell("check").Bounds()
	onPort := false
	for _, port := range []Port{PortNorth, PortEast, PortSouth, PortWest} {
		if dist32(r.Start, portPoint(src, port)) <= .01 {
			onPort = true
			break
		}
	}
	if !onPort {
		t.Fatalf("orthogonal route started at %v, not a visible boundary port", r.Start)
	}
}
