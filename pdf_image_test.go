package qui

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"io"
	"regexp"
	"strings"
	"testing"
)

// Raster export. DrawImage used to be a no-op on the PDF backend, so every
// picture in a document — an inserted photo, a chart, a picture watermark —
// was on screen and in the print preview but absent from the exported PDF
// and from anything printed through it.

func rgbImage(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func pdfBytes(t *testing.T, d *PDFDoc) string {
	t.Helper()
	b, err := d.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	return string(b)
}

func TestPDFDrawImageEmbedsAnXObject(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPageLogical(200, 100, 96)
	cv.DrawImage(rgbImage(8, 4, color.RGBA{R: 10, G: 200, B: 30, A: 255}), Rect{X: 20, Y: 10, W: 80, H: 40})
	out := pdfBytes(t, doc)

	if !strings.Contains(out, "/Subtype /Image") {
		t.Fatal("no image XObject in the file")
	}
	if !strings.Contains(out, "/ColorSpace /DeviceRGB") || !strings.Contains(out, "/Filter /FlateDecode") {
		t.Error("image stream is not a Flate-compressed DeviceRGB")
	}
	if !strings.Contains(out, "/XObject << /Im1") {
		t.Error("the page does not name the XObject in its resources")
	}
	if !strings.Contains(out, "/Im1 Do") {
		t.Error("the content stream never paints the image")
	}
	// Placed at the rect, with the Y flip that keeps it the right way up.
	if !regexp.MustCompile(`80 0 0 -40 20 50 cm`).MatchString(out) {
		t.Errorf("placement matrix is wrong; content:\n%s", contentOf(out))
	}
	// An opaque picture needs no soft mask.
	if strings.Contains(out, "/SMask") {
		t.Error("an opaque image should not carry an SMask")
	}
}

func TestPDFDrawImageAlphaBecomesSoftMask(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	// Premultiplied half-transparent red: the PDF colour channel must come
	// back out at FULL red with alpha 128, not at the premultiplied 128.
	img.SetRGBA(1, 0, color.RGBA{R: 128, A: 128})

	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.DrawImage(img, Rect{W: 10, H: 10})
	out := pdfBytes(t, doc)

	if !strings.Contains(out, "/SMask") || !strings.Contains(out, "/ColorSpace /DeviceGray") {
		t.Fatal("a translucent image did not get a greyscale soft mask")
	}
	rgb, alpha := imagePDFSamples(img)
	if got := []byte{rgb[3], rgb[4], rgb[5]}; got[0] < 250 {
		t.Errorf("translucent pixel un-premultiplied to %v, want ~255 red", got)
	}
	if alpha == nil || alpha[0] != 255 || alpha[1] != 128 {
		t.Errorf("alpha plane = %v, want [255 128]", alpha)
	}
}

// The same picture on every page is one stream, referenced from each page.
// A forty-page watermark that embedded forty copies would multiply the file
// size by forty.
func TestPDFRepeatedImageIsEmbeddedOnce(t *testing.T) {
	img := rgbImage(16, 16, color.RGBA{B: 200, A: 255})
	doc := NewPDFDoc()
	for i := 0; i < 3; i++ {
		cv := doc.NewPageLogical(200, 100, 96)
		cv.DrawImage(img, Rect{W: 50, H: 50})
	}
	out := pdfBytes(t, doc)
	if n := strings.Count(out, "/Subtype /Image"); n != 1 {
		t.Fatalf("%d image streams for one picture on three pages", n)
	}
	if n := strings.Count(out, "/Im1 Do"); n != 3 {
		t.Fatalf("the image is painted %d times, want 3", n)
	}
	if n := strings.Count(out, "/XObject << /Im1"); n != 3 {
		t.Fatalf("%d pages name the resource, want 3", n)
	}
}

// A page that draws no picture must not grow an /XObject dictionary.
func TestPDFPageWithoutImagesHasNoXObjectDict(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.FillRect(Rect{W: 10, H: 10}, Color{A: 1})
	if out := pdfBytes(t, doc); strings.Contains(out, "/XObject") {
		t.Error("an image-free page declared an XObject dictionary")
	}
}

// The compressed stream really is the pixels: inflate it and check.
func TestPDFImageStreamRoundTripsPixels(t *testing.T) {
	doc := NewPDFDoc()
	cv := doc.NewPage(100, 100)
	cv.DrawImage(rgbImage(2, 2, color.RGBA{R: 1, G: 2, B: 3, A: 255}), Rect{W: 10, H: 10})
	if _, err := doc.Bytes(); err != nil {
		t.Fatal(err)
	}
	entry := doc.images[0]
	zr, err := zlib.NewReader(bytes.NewReader(entry.rgb))
	if err != nil {
		t.Fatalf("stream is not zlib: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{1, 2, 3}, 4)
	if !bytes.Equal(raw, want) {
		t.Fatalf("pixels = %v, want %v", raw, want)
	}
}

// contentOf pulls the first content stream out for error messages.
func contentOf(pdf string) string {
	i := strings.Index(pdf, "stream\n")
	if i < 0 {
		return pdf
	}
	j := strings.Index(pdf[i:], "endstream")
	if j < 0 {
		return pdf[i:]
	}
	return pdf[i : i+j]
}
