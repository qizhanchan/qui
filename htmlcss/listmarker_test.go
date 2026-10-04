package htmlcss

import (
	"image"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// renderList lays out one <ul>/<ol> item and returns the li and its marker.
func renderList(t *testing.T, listStyle, tag string) (*El, qui.Widget) {
	t.Helper()
	style := ""
	if listStyle != "" {
		style = ` style="list-style-type:` + listStyle + `"`
	}
	root := Render(`<`+tag+style+`><li>item text</li></`+tag+`>`,
		`body{font-size:14px} ul,ol{margin:0;padding:0}`, Options{})
	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 80})

	var li *El
	walkWidgets(root, func(w qui.Widget) {
		if e, ok := w.(*El); ok && e.tag == "li" && li == nil {
			li = e
		}
	})
	if li == nil {
		t.Fatalf("no <li> found for list-style-type %q", listStyle)
	}
	kids := li.ChildList()
	if len(kids) != 2 {
		t.Fatalf("li has %d children, want [marker, content]", len(kids))
	}
	return li, kids[0]
}

// Bullets draw as geometry; numbers stay text. The bullet codepoints are
// full-width in a CJK primary font (qui's default), which is why they can't
// go through a Label — see listmarker.go.
func TestListBulletsAreGeometryNumbersAreText(t *testing.T) {
	for _, tc := range []struct {
		style, tag string
		shape      listMarkerShape
	}{
		{"", "ul", markerDisc},
		{"disc", "ul", markerDisc},
		{"circle", "ul", markerCircle},
		{"square", "ul", markerSquare},
	} {
		_, marker := renderList(t, tc.style, tc.tag)
		m, ok := marker.(*listMarkerWidget)
		if !ok {
			t.Errorf("list-style-type %q marker is %T, want *listMarkerWidget", tc.style, marker)
			continue
		}
		if m.shape != tc.shape {
			t.Errorf("list-style-type %q shape = %v, want %v", tc.style, m.shape, tc.shape)
		}
	}
	if _, marker := renderList(t, "", "ol"); true {
		if _, ok := marker.(*widgets.Label); !ok {
			t.Errorf("ol marker is %T, want *widgets.Label (numbers are real text)", marker)
		}
	}
}

// The bug this fixes: disc measured 14px (full-width U+2022) while circle
// measured 6.97 and square 9.09, so items in the same document hung at
// different indents depending on list-style-type.
func TestListBulletMarkersShareOneColumnWidth(t *testing.T) {
	var width float32
	for i, style := range []string{"disc", "circle", "square"} {
		_, marker := renderList(t, style, "ul")
		got := marker.Bounds().W
		if i == 0 {
			width = got
			continue
		}
		if got != width {
			t.Errorf("list-style-type %q marker width = %v, want %v (same box for every bullet)", style, got, width)
		}
	}
	if width <= 0 {
		t.Fatalf("marker width = %v, want > 0", width)
	}
}

// renderListImage draws one list item and returns the pixels plus the marker
// widget, so a test can look at exactly the marker's own box. The item text
// lives in a sibling host to the right of that box (the row has a Gap), so
// scanning the box needs no diffing against a second render.
func renderListImage(t *testing.T, style string) (*image.RGBA, qui.Widget) {
	t.Helper()
	root := Render(`<ul style="list-style-type:`+style+`"><li>item text</li></ul>`,
		`body{font-size:14px} ul{margin:0;padding:0}`, Options{})
	img := image.NewRGBA(image.Rect(0, 0, 200, 80))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	root.Layout(qui.Rect{X: 0, Y: 0, W: 200, H: 80})
	root.Draw(qui.NewImageCanvas(img))

	_, marker := renderList(t, style, "ul")
	return img, marker
}

func inked(img *image.RGBA, x, y int) bool {
	r, _, _, _ := img.At(x, y).RGBA()
	return 255-int(r>>8) > 60
}

// markerInk returns the ink bounding box within the marker's own bounds.
func markerInk(t *testing.T, style string) (minX, maxX, minY, maxY int, ok bool) {
	t.Helper()
	img, marker := renderListImage(t, style)
	b := marker.Bounds()
	x0, x1 := int(b.X), int(b.X+b.W)+1
	y0, y1 := int(b.Y), int(b.Y+b.H)+1

	minX, minY = 1<<30, 1<<30
	maxX, maxY = -1, -1
	for y := y0; y <= y1 && y < 80; y++ {
		for x := x0; x <= x1 && x < 200; x++ {
			if x < 0 || y < 0 || !inked(img, x, y) {
				continue
			}
			if x < minX {
				minX = x
			}
			if x > maxX {
				maxX = x
			}
			if y < minY {
				minY = y
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	return minX, maxX, minY, maxY, maxX >= 0
}

// list-style-type:circle must render as a RING, not a blob or a pair of
// dashes. At 0.35em the ring's side walls fell in the same pixel columns as
// its hole and averaged away; 0.40em is the smallest box that survives at a
// 14px body font.
func TestListCircleMarkerIsHollow(t *testing.T) {
	minX, maxX, minY, maxY, ok := markerInk(t, "circle")
	if !ok {
		t.Fatal("circle marker drew no ink")
	}
	// Some row must read ink, gap, ink.
	img, _ := renderListImage(t, "circle")
	hollow := false
	for y := minY; y <= maxY && !hollow; y++ {
		seenInk, seenGap := false, false
		for x := minX; x <= maxX; x++ {
			switch on := inked(img, x, y); {
			case on && seenGap:
				hollow = true
			case on:
				seenInk = true
			case seenInk:
				seenGap = true
			}
		}
	}
	if !hollow {
		t.Errorf("circle marker ink x=[%d..%d] y=[%d..%d] has no hole — it is not rendering as a ring",
			minX, maxX, minY, maxY)
	}
}

// The disc, by contrast, must be solid.
func TestListDiscMarkerIsSolid(t *testing.T) {
	minX, maxX, minY, maxY, ok := markerInk(t, "disc")
	if !ok {
		t.Fatal("disc marker drew no ink")
	}
	if w, h := maxX-minX+1, maxY-minY+1; w < 3 || h < 3 {
		t.Errorf("disc ink is %dx%d px, want at least 3x3 (a visible dot)", w, h)
	}
}

// Markers sit on the same optical line as the item's text — centered on the
// x-height band, not on the row box.
func TestListMarkerAlignsWithItemText(t *testing.T) {
	_, marker := renderList(t, "disc", "ul")
	b := marker.Bounds()
	_, _, minY, maxY, ok := markerInk(t, "disc")
	if !ok {
		t.Fatal("disc marker drew no ink")
	}
	inkMid := float32(minY+maxY) / 2
	wantMid := qui.TextXHeightCenterY(b, qui.Font{Size: 14})
	if diff := inkMid - wantMid; diff < -1.5 || diff > 1.5 {
		t.Errorf("marker ink center = %.2f, x-height center = %.2f (off by %.2f, want within 1.5)",
			inkMid, wantMid, diff)
	}
}

// Drawing the bullet must not change its semantic value: clipboard rebuild
// and the AX tree still read the marker string.
func TestListBulletKeepsMarkerString(t *testing.T) {
	for style, want := range map[string]string{"disc": "•", "circle": "◦", "square": "▪"} {
		li, _ := renderList(t, style, "ul")
		if li.marker != want {
			t.Errorf("list-style-type %q marker string = %q, want %q", style, li.marker, want)
		}
		if li.listTag != "ul" {
			t.Errorf("list-style-type %q listTag = %q, want ul", style, li.listTag)
		}
	}
}
