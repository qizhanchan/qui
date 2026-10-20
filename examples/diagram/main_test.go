package main

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/graphs/diagram"
)

func TestStarterDocumentRenders(t *testing.T) {
	doc := starterDocument()
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := diagram.PNGBytes(doc, diagram.RenderOptions{Width: 1000, Height: 640, Fit: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 1000 {
		t.Fatalf("starter PNG is unexpectedly small: %d bytes", len(data))
	}

	ed := diagram.New(diagram.Option{
		Document: doc, Mode: diagram.ModeEdit, Grid: true,
		Snap: true, SnapSize: 10, Fit: true,
	})
	ed.Layout(qui.Rect{W: 1320, H: 760})
	img := image.NewRGBA(image.Rect(0, 0, 1320, 760))
	ed.Draw(qui.NewImageCanvas(img))
	if os.Getenv("QUI_GOLDEN") == "1" {
		f, err := os.Create("/tmp/qui-diagram-editor.png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		t.Log("wrote /tmp/qui-diagram-editor.png")
	}

	// The decision fan-out must visibly leave West/East, not from a hidden
	// diagonal perimeter point or the node center.
	for _, edgeID := range []string{"e3", "e4"} {
		edge := doc.Cell(edgeID)
		route := doc.Active().Route(edge)
		check := doc.Cell(edge.Source.Cell).Bounds()
		want := diagram.PortWest
		if edgeID == "e4" {
			want = diagram.PortEast
		}
		var wantPoint diagram.Point
		switch want {
		case diagram.PortWest:
			wantPoint = diagram.Point{X: check.X, Y: check.Y + check.H/2}
		case diagram.PortEast:
			wantPoint = diagram.Point{X: check.X + check.W, Y: check.Y + check.H/2}
		}
		if route.Start != wantPoint {
			t.Fatalf("%s starts at %v, want %v (%s port)", edgeID, route.Start, wantPoint, want)
		}
	}
}

func TestCopyPasteDocumentRoundTrip(t *testing.T) {
	ed := diagram.New(diagram.Option{Document: starterDocument(), Mode: diagram.ModeEdit})
	ed.Document().Active().Cell("load").Geometry.X = 605
	text := copyDocument(ed)

	other := diagram.New(diagram.Option{Document: diagram.NewDocument(100, 100), Mode: diagram.ModeEdit})
	if err := pasteDocument(other, text); err != nil {
		t.Fatal(err)
	}
	if got := other.Document().Active().Cell("load"); got == nil || got.Geometry.X != 605 {
		t.Fatalf("pasted document lost the layout: %+v", got)
	}
	if err := pasteDocument(other, "not json"); err == nil {
		t.Fatal("garbage clipboard was accepted")
	}
}
