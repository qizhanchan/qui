package widgets

// Golden snapshot harness for aligning qui's DEFAULT (zero-config)
// widgets with raw HTML form controls.
//
// QUI_GOLDEN=1 writes PNGs to /tmp/qui-html/<case>.png, each the widget
// rect + 16 px white padding — sitting next to the matching
// /tmp/html-native-ref/<case>.png the browser reference produces
// (widgets/scripts/web/capture.mjs). widgets/scripts/compare-html.sh
// then diffs the pairs with ImageMagick AE.
//
// Without QUI_GOLDEN the test still drives Measure+Layout+Draw so a nil
// deref / panic in a default widget is caught in CI.
//
// The font is pinned to Go Regular (the same face native.html injects
// via @font-face) so the AE diff isolates geometry + color, not glyph
// metrics.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	. "github.com/qizhanchan/qui"
	"golang.org/x/image/font/gofont/goregular"
)

// htmlCase pairs a stable file name with a factory producing a fresh
// default widget in the state the reference renders.
type htmlCase struct {
	name string
	make func() Widget
}

func htmlCases() []htmlCase {
	return []htmlCase{
		{name: "input-text", make: func() Widget {
			t := NewInput("")
			t.SetText("Text")
			return t
		}},
		{name: "textarea", make: func() Widget {
			t := NewTextArea("")
			t.SetText("Text")
			return t
		}},
		{name: "select", make: func() Widget {
			s := NewSelect(nil, []string{"Apple", "Banana", "Cherry"}, nil)
			s.SelectedIdx = 0
			return s
		}},
		{name: "button", make: func() Widget { return NewButton("Button", nil) }},
		{name: "checkbox-unchecked", make: func() Widget { return NewCheckBox("", nil) }},
		{name: "checkbox-checked", make: func() Widget {
			c := NewCheckBox("", nil)
			c.Checked = true
			return c
		}},
		{name: "radio-unchecked", make: func() Widget {
			return NewRadioButton(NewRadioGroup(), "")
		}},
		{name: "radio-checked", make: func() Widget {
			r := NewRadioButton(NewRadioGroup(), "")
			r.Checked = true
			return r
		}},
	}
}

func TestHTMLNativeWidgetsSnapshot(t *testing.T) {
	// Pin the font so the reference (which @font-face-loads the same
	// Go Regular) and qui rasterize identical glyphs.
	if err := LoadFontFromBytes(goregular.TTF); err != nil {
		t.Fatalf("pin font: %v", err)
	}
	for _, tc := range htmlCases() {
		t.Run(tc.name, func(t *testing.T) {
			renderHTMLCase(t, tc)
		})
	}
}

// renderHTMLCase measures the widget at its natural size, lays it out in
// a white canvas with 16 px padding on every side, draws, and (if
// QUI_GOLDEN=1) writes the PNG.
func renderHTMLCase(t *testing.T, tc htmlCase) {
	w := tc.make()
	size := w.Measure(Size{W: 480, H: 240})
	if size.W < 1 {
		size.W = 1
	}
	if size.H < 1 {
		size.H = 1
	}
	const pad = 16
	imgW := int(size.W) + pad*2
	imgH := int(size.H) + pad*2
	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	white := Color{R: 1, G: 1, B: 1, A: 1}
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.SetRGBA(x, y, ColorToRGBA(white))
		}
	}
	canvas := NewImageCanvas(img)
	w.Layout(Rect{X: pad, Y: pad, W: size.W, H: size.H})
	w.Draw(canvas)
	if os.Getenv("QUI_GOLDEN") == "1" {
		writeHTMLPNG(t, img, tc.name)
	}
}

func writeHTMLPNG(t *testing.T, img *image.RGBA, name string) {
	t.Helper()
	dir := "/tmp/qui-html"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
}
