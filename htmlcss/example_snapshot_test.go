package htmlcss

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/qizhanchan/qui"
)

// TestExamplePageSnapshot renders the SHIPPED examples/html-css page +
// stylesheet to a PNG so a human can eyeball the engine's output against
// the real demo assets. Gated on QUI_HTMLCSS_SNAPSHOT=1 (writes to
// /tmp/qui-htmlcss/example.png). Run:
//
//	QUI_HTMLCSS_SNAPSHOT=1 go test ./htmlcss -run ExamplePageSnapshot -v
func TestExamplePageSnapshot(t *testing.T) {
	if os.Getenv("QUI_HTMLCSS_SNAPSHOT") == "" {
		t.Skip("set QUI_HTMLCSS_SNAPSHOT=1 to render the example page")
	}
	base := filepath.Join("..", "examples", "html-css")
	root, err := RenderFiles(filepath.Join(base, "page.html"), filepath.Join(base, "style.css"), Options{BaseDir: base})
	if err != nil {
		t.Fatalf("RenderFiles: %v", err)
	}

	const w = 960
	sz := root.Measure(qui.Size{W: w, H: 0})
	h := int(sz.H) + 8
	if h < 100 {
		h = 100
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// White page background.
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	canvas := qui.NewImageCanvas(img)
	root.Layout(qui.Rect{X: 0, Y: 0, W: w, H: float32(h)})
	root.Draw(canvas)

	outDir := filepath.Join(os.TempDir(), "qui-htmlcss")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(outDir, "example.png")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%dx%d)", out, w, h)
}
