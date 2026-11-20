package svg

import "testing"

// A <pattern> fill repeats its tile (userSpaceOnUse and objectBoundingBox
// units); before, url(#pattern) found no paint server and filled nothing.
func TestPatternFill(t *testing.T) {
	doc, err := ParseBytes([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 40 40">
<defs><pattern id="p" width="10" height="10" patternUnits="userSpaceOnUse"><rect width="5" height="5" fill="#ff0000"/></pattern>
<pattern id="q" width=".5" height=".5"><rect width="2" height="2" fill="#0000ff"/></pattern></defs>
<rect width="40" height="20" fill="url(#p)"/>
<rect y="20" width="40" height="20" fill="url(#q)"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	img := rasterizeDocument(doc, 40, 40)
	red := func(x, y int) bool { c := img.RGBAAt(x, y); return c.R > 200 && c.A > 200 && c.B < 50 }
	blue := func(x, y int) bool { c := img.RGBAAt(x, y); return c.B > 200 && c.A > 200 && c.R < 50 }
	clear := func(x, y int) bool { return img.RGBAAt(x, y).A == 0 }
	for _, p := range [][2]int{{2, 2}, {12, 2}, {32, 12}} {
		if !red(p[0], p[1]) {
			t.Errorf("(%d,%d) = %v, want the tile's red square", p[0], p[1], img.RGBAAt(p[0], p[1]))
		}
	}
	for _, p := range [][2]int{{7, 7}, {17, 3}} {
		if !clear(p[0], p[1]) {
			t.Errorf("(%d,%d) = %v, want transparent", p[0], p[1], img.RGBAAt(p[0], p[1]))
		}
	}
	// objectBoundingBox: tiles of 20×10 starting at the rect's origin (0,20).
	if !blue(1, 21) || !blue(21, 31) || !clear(10, 25) {
		t.Errorf("bbox pattern: %v %v %v", img.RGBAAt(1, 21), img.RGBAAt(21, 31), img.RGBAAt(10, 25))
	}
}
