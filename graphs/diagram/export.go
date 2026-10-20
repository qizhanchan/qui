package diagram

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"os"

	"github.com/qizhanchan/qui"
)

// RenderOptions controls headless document rendering and export.
type RenderOptions struct {
	Page       int
	Width      int
	Height     int
	Theme      *Theme
	Fit        bool
	Padding    float32
	Background string
	Shapes     *ShapeRegistry
}

// RenderImage rasterizes a page through the same qui.Canvas primitives as
// the interactive widget.
func RenderImage(doc *Document, opt RenderOptions) (*image.RGBA, error) {
	if doc == nil {
		return nil, errors.New("diagram: nil document")
	}
	page := doc.PageAt(opt.Page)
	if page == nil {
		return nil, errors.New("diagram: document has no pages")
	}
	w, h := opt.Width, opt.Height
	if w <= 0 {
		w = int(page.Width + .5)
	}
	if h <= 0 {
		h = int(page.Height + .5)
	}
	if w <= 0 || h <= 0 {
		return nil, errors.New("diagram: render size must be positive")
	}
	theme := DefaultTheme()
	if opt.Theme != nil {
		theme = *opt.Theme
	}
	background := page.Background
	if opt.Background != "" {
		background = opt.Background
	}
	bg, ok := ParseColor(background)
	if !ok {
		bg, _ = ParseColor(theme.Background)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	canvas := qui.NewImageCanvas(img)
	canvas.FillRect(qui.Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}, bg)

	// Render the selected page without mutating the document's active page.
	viewDoc := doc
	if page != doc.Active() {
		viewDoc = &Document{Pages: []*Page{page}, ActivePage: 0}
	}
	vp := Viewport{Scale: 1}
	if opt.Fit {
		border := opt.Padding
		if border <= 0 {
			border = .5
		}
		content := page.ContentBounds()
		if content.Empty() {
			content = Rect{X: 0, Y: 0, W: page.Width, H: page.Height}
		}
		vp = drawioViewport(content, Rect{X: 0, Y: 0, W: float32(w), H: float32(h)}, border)
	}
	drawScene(canvas, viewDoc, sceneConfig{
		Origin:   Point{},
		Viewport: vp,
		Clip:     Rect{X: 0, Y: 0, W: float32(w), H: float32(h)},
		Theme:    theme,
		Shapes:   opt.Shapes,
		Grid:     false,
		Selected: map[string]bool{},
	})
	return img, nil
}

// WritePNG encodes a rendered page as PNG.
func WritePNG(w io.Writer, doc *Document, opt RenderOptions) error {
	img, err := RenderImage(doc, opt)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

// PNGBytes is the byte-slice convenience form of WritePNG.
func PNGBytes(doc *Document, opt RenderOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := WritePNG(&buf, doc, opt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteSVG renders a page into qui's SVG canvas backend.
func WriteSVG(w io.Writer, doc *Document, opt RenderOptions) error {
	if doc == nil {
		return errors.New("diagram: nil document")
	}
	page := doc.PageAt(opt.Page)
	if page == nil {
		return errors.New("diagram: document has no pages")
	}
	width, height := opt.Width, opt.Height
	if width <= 0 {
		width = int(page.Width + .5)
	}
	if height <= 0 {
		height = int(page.Height + .5)
	}
	theme := DefaultTheme()
	if opt.Theme != nil {
		theme = *opt.Theme
	}
	background := page.Background
	if opt.Background != "" {
		background = opt.Background
	}
	bg, ok := ParseColor(background)
	if !ok {
		bg, _ = ParseColor(theme.Background)
	}
	canvas := qui.NewSVGCanvas(float32(width), float32(height))
	canvas.FillRect(qui.Rect{X: 0, Y: 0, W: float32(width), H: float32(height)}, bg)
	viewDoc := doc
	if page != doc.Active() {
		viewDoc = &Document{Pages: []*Page{page}, ActivePage: 0}
	}
	vp := Viewport{Scale: 1}
	if opt.Fit {
		border := opt.Padding
		if border <= 0 {
			border = .5
		}
		content := page.ContentBounds()
		if content.Empty() {
			content = Rect{X: 0, Y: 0, W: page.Width, H: page.Height}
		}
		vp = drawioViewport(content, Rect{X: 0, Y: 0, W: float32(width), H: float32(height)}, border)
	}
	drawScene(canvas, viewDoc, sceneConfig{
		Origin: Point{}, Viewport: vp,
		Clip:  Rect{X: 0, Y: 0, W: float32(width), H: float32(height)},
		Theme: theme, Shapes: opt.Shapes, Grid: false, Selected: map[string]bool{},
	})
	_, err := w.Write(canvas.Bytes())
	return err
}

// SVGBytes is the byte-slice convenience form of WriteSVG.
func SVGBytes(doc *Document, opt RenderOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := WriteSVG(&buf, doc, opt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WritePDF renders a page into qui's vector PDF backend.
func WritePDF(w io.Writer, doc *Document, opt RenderOptions) error {
	if doc == nil {
		return errors.New("diagram: nil document")
	}
	page := doc.PageAt(opt.Page)
	if page == nil {
		return errors.New("diagram: document has no pages")
	}
	width, height := opt.Width, opt.Height
	if width <= 0 {
		width = int(page.Width + .5)
	}
	if height <= 0 {
		height = int(page.Height + .5)
	}
	theme := DefaultTheme()
	if opt.Theme != nil {
		theme = *opt.Theme
	}
	background := page.Background
	if opt.Background != "" {
		background = opt.Background
	}
	bg, ok := ParseColor(background)
	if !ok {
		bg, _ = ParseColor(theme.Background)
	}
	pdf := qui.NewPDFDoc()
	canvas := pdf.NewPage(float64(width), float64(height))
	canvas.FillRect(qui.Rect{X: 0, Y: 0, W: float32(width), H: float32(height)}, bg)
	viewDoc := doc
	if page != doc.Active() {
		viewDoc = &Document{Pages: []*Page{page}, ActivePage: 0}
	}
	vp := Viewport{Scale: 1}
	if opt.Fit {
		border := opt.Padding
		if border <= 0 {
			border = .5
		}
		content := page.ContentBounds()
		if content.Empty() {
			content = Rect{X: 0, Y: 0, W: page.Width, H: page.Height}
		}
		vp = drawioViewport(content, Rect{X: 0, Y: 0, W: float32(width), H: float32(height)}, border)
	}
	drawScene(canvas, viewDoc, sceneConfig{
		Origin: Point{}, Viewport: vp,
		Clip:  Rect{X: 0, Y: 0, W: float32(width), H: float32(height)},
		Theme: theme, Shapes: opt.Shapes, Grid: false, Selected: map[string]bool{},
	})
	data, err := pdf.Bytes()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// PDFBytes is the byte-slice convenience form of WritePDF.
func PDFBytes(doc *Document, opt RenderOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := WritePDF(&buf, doc, opt); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WritePNGFile writes a rendered page to path.
func WritePNGFile(path string, doc *Document, opt RenderOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WritePNG(f, doc, opt)
}

// WriteSVGFile writes an SVG page to path.
func WriteSVGFile(path string, doc *Document, opt RenderOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteSVG(f, doc, opt)
}

// WritePDFFile writes a vector PDF page to path.
func WritePDFFile(path string, doc *Document, opt RenderOptions) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WritePDF(f, doc, opt)
}
