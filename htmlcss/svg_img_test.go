package htmlcss

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// <img src="*.svg"> must load through the svg package and render as a
// vector widget sized from CSS width/height — not fail to a placeholder.
func TestSVGImageRendersAndSizes(t *testing.T) {
	dir := t.TempDir()
	svgSrc := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M4 4h16v16H4z"/></svg>`
	if err := os.WriteFile(filepath.Join(dir, "box.svg"), []byte(svgSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	root := Render(
		`<body><img src="box.svg" class="ic"></body>`,
		`.ic { width: 18px; height: 18px; }`,
		Options{BaseDir: dir},
	)

	var found qui.Widget
	var walk func(w qui.Widget)
	walk = func(w qui.Widget) {
		if _, isImg := w.(*widgets.Image); isImg {
			t.Error("svg loaded as a raster *widgets.Image (placeholder path), want a vector widget")
		}
		if qui.WidgetRole(w) == qui.RoleImage {
			found = w
		}
		if c, ok := w.(interface{ ChildList() []qui.Widget }); ok {
			for _, ch := range c.ChildList() {
				walk(ch)
			}
		}
	}
	walk(root)

	if found == nil {
		t.Fatal("no image-role widget produced for <img src=*.svg>")
	}
	if got := found.Measure(qui.Size{W: 100, H: 100}); got.W != 18 || got.H != 18 {
		t.Errorf("svg image measured %vx%v, want 18x18 (from CSS)", got.W, got.H)
	}
}
