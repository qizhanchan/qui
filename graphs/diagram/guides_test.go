package diagram

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/qizhanchan/qui"
)

// The anchor "b" sits off the 10px grid so a guide snap and a grid snap
// land in different places.
func newGuideEditor() *Diagram {
	doc := NewDocument(900, 600)
	p := doc.Active()
	_ = p.AddCell(&Cell{ID: "a", Type: CellVertex, Geometry: Geometry{X: 100, Y: 100, W: 100, H: 60}})
	_ = p.AddCell(&Cell{ID: "b", Type: CellVertex, Geometry: Geometry{X: 403, Y: 303, W: 120, H: 60}})
	d := New(Option{Document: doc, Mode: ModeEdit, Snap: true, SnapSize: 10, Guides: true})
	d.Layout(qui.Rect{W: 900, H: 600})
	d.vp = Viewport{Scale: 1}
	return d
}

func dragBy(d *Diagram, from Point, dx, dy float32, mods qui.Modifiers) []Guide {
	d.Handle(mouse(qui.EventMouseDown, from.X, from.Y, 0))
	d.Handle(mouse(qui.EventMouseMove, from.X+dx, from.Y+dy, mods))
	guides := d.ActiveGuides()
	d.Handle(mouse(qui.EventMouseUp, from.X+dx, from.Y+dy, mods))
	return guides
}

func hasGuide(gs []Guide, vertical bool, pos float32) bool {
	for _, g := range gs {
		if g.Vertical == vertical && abs32(g.Pos-pos) < .01 {
			return true
		}
	}
	return false
}

func TestDragSnapsCentreToGuide(t *testing.T) {
	d := newGuideEditor()
	a := d.doc.Cell("a")
	// Raw move puts a's centre x at 459, 4px from b's centre (463).
	guides := dragBy(d, Point{X: 150, Y: 130}, 309, 0, 0)
	if got := a.Bounds().Center().X; got != 463 {
		t.Fatalf("centre x = %v, want 463 (snapped to b's centre)", got)
	}
	if !hasGuide(guides, true, 463) {
		t.Fatalf("no vertical guide at 463 during drag: %+v", guides)
	}
	if len(d.ActiveGuides()) != 0 {
		t.Fatal("guides still shown after mouse up")
	}
}

func TestDragSnapsEdgeToGuide(t *testing.T) {
	d := newGuideEditor()
	a := d.doc.Cell("a")
	// Raw move puts a's top at 300, 3px above b's top (303).
	guides := dragBy(d, Point{X: 150, Y: 130}, 0, 200, 0)
	if a.Geometry.Y != 303 {
		t.Fatalf("top = %v, want 303 (snapped to b's top)", a.Geometry.Y)
	}
	if a.Geometry.X != 100 {
		t.Fatalf("x drifted to %v on a vertical drag", a.Geometry.X)
	}
	if !hasGuide(guides, false, 303) {
		t.Fatalf("no horizontal guide at 303: %+v", guides)
	}
}

func TestDragOutsideToleranceUsesGrid(t *testing.T) {
	d := newGuideEditor()
	a := d.doc.Cell("a")
	// Centre lands at 431, 32px from b's centre: grid snap only.
	guides := dragBy(d, Point{X: 150, Y: 130}, 281, 0, 0)
	if a.Geometry.X != 380 {
		t.Fatalf("x = %v, want grid-snapped 380", a.Geometry.X)
	}
	if len(guides) != 0 {
		t.Fatalf("unexpected guides: %+v", guides)
	}
}

func TestAltDisablesGuides(t *testing.T) {
	d := newGuideEditor()
	a := d.doc.Cell("a")
	guides := dragBy(d, Point{X: 150, Y: 130}, 309, 0, qui.ModAlt)
	if got := a.Bounds().Center().X; got != 460 {
		t.Fatalf("centre x = %v, want grid-only 460 with Alt held", got)
	}
	if len(guides) != 0 {
		t.Fatalf("guides shown with Alt held: %+v", guides)
	}
}

func TestGuidesCanBeTurnedOff(t *testing.T) {
	d := newGuideEditor()
	d.SetGuides(false)
	a := d.doc.Cell("a")
	if guides := dragBy(d, Point{X: 150, Y: 130}, 309, 0, 0); len(guides) != 0 {
		t.Fatalf("guides shown while disabled: %+v", guides)
	}
	if got := a.Bounds().Center().X; got != 460 {
		t.Fatalf("centre x = %v, want grid-only 460", got)
	}
}

func TestResizeSnapsMovingEdgeToGuide(t *testing.T) {
	d := newGuideEditor()
	a := d.doc.Cell("a")
	// Select a, then drag its east handle so the right edge lands at 400,
	// 3px left of b's left edge (403).
	d.Handle(mouse(qui.EventMouseDown, 150, 130, 0))
	d.Handle(mouse(qui.EventMouseUp, 150, 130, 0))
	guides := dragBy(d, Point{X: 200, Y: 130}, 200, 0, 0)
	if a.Geometry.X != 100 || a.Geometry.X+a.Geometry.W != 403 {
		t.Fatalf("geometry = %+v, want left 100 and right 403", a.Geometry)
	}
	if !hasGuide(guides, true, 403) {
		t.Fatalf("no vertical guide at 403: %+v", guides)
	}
}

func TestDragUndoAfterGuideSnap(t *testing.T) {
	d := newGuideEditor()
	dragBy(d, Point{X: 150, Y: 130}, 309, 0, 0)
	if !d.Undo() {
		t.Fatal("undo unavailable")
	}
	if g := d.doc.Cell("a").Geometry; g.X != 100 || g.Y != 100 {
		t.Fatalf("undo restored %+v", g)
	}
}

func TestGuidesAreDrawnDuringDrag(t *testing.T) {
	d := newGuideEditor()
	d.Handle(mouse(qui.EventMouseDown, 150, 130, 0))
	d.Handle(mouse(qui.EventMouseMove, 459, 130, 0))
	img := image.NewRGBA(image.Rect(0, 0, 900, 600))
	d.Draw(qui.NewImageCanvas(img))
	if path := os.Getenv("QUI_GUIDES_PNG"); path != "" {
		if f, err := os.Create(path); err == nil {
			_ = png.Encode(f, img)
			_ = f.Close()
		}
	}
	// The centre guide at x=463 runs between the shapes (y 170..295),
	// where nothing else is painted.
	pink := func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r>>8 > 200 && g>>8 < 160 && b>>8 > 100
	}
	if !pink(463, 230) && !pink(462, 230) {
		r, g, b, _ := img.At(463, 230).RGBA()
		t.Fatalf("guide not painted at x=463: rgb(%d,%d,%d)", r>>8, g>>8, b>>8)
	}
	d.Handle(mouse(qui.EventMouseUp, 459, 130, 0))
}
