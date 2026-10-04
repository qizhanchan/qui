package qui

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
)

// PDF vector output backend.
//
// PDFDoc accumulates pages, each exposing a qui.Canvas that records vector
// drawing operators (fills, strokes, clips, transforms) plus real,
// selectable text with embedded (subset) fonts. The result is a
// searchable, resolution-independent PDF — not a rasterized page image.
//
// Typical use (spreadsheet print/export):
//
//	doc := qui.NewPDFDoc()
//	for _, page := range pages {
//		cv := doc.NewPageLogical(paperWpx, paperHpx, 96)
//		cv.Clear(qui.Color{R: 1, G: 1, B: 1, A: 1})
//		drawInto(cv) // any code that draws through a qui.Canvas
//	}
//	_ = doc.WritePDFFile("/tmp/out.pdf")
//
// Coordinates: NewPage draws in POINTS (1/72"), NewPageLogical in logical
// pixels at a chosen px-per-inch. Both use qui's top-left origin, Y-down
// convention; the Y-flip into PDF's bottom-left space is applied
// internally as the first operator of each page's content stream.
type PDFDoc struct {
	pages []*pdfPage
	fonts *pdfFontSet
	// images are the embedded rasters, shared across pages (see
	// pdf_image.go); imageIndex dedupes them by pixel content.
	images     []*pdfImage
	imageIndex map[uint64]*pdfImage
	// alphas are the distinct constant-alpha graphics states in use (see
	// pdf_gstate.go); PDF colour operators carry no alpha of their own.
	alphas map[uint8]bool
}

// NewPDFDoc returns an empty document.
func NewPDFDoc() *PDFDoc {
	return &PDFDoc{fonts: newPDFFontSet()}
}

// pdfPage is one page: its media box (points) and the recorded content
// stream (in the page's user space — see the base CTM emitted at setup).
type pdfPage struct {
	wPt, hPt           float64 // MediaBox size in points
	wLogical, hLogical float32 // logical drawing extent (for Clear)
	buf                bytes.Buffer
	// imageNames are the XObject resources this page references, in first
	// use order. A page's Resources dict must name every XObject its
	// content stream draws, or a reader shows nothing where the picture is.
	imageNames []string
	// gsNames are the /ExtGState (alpha) resources this page names, and
	// usesAlpha records that something translucent has been drawn — after
	// which even an opaque draw must reset the state explicitly.
	gsNames   []string
	usesAlpha bool
}

// NewPage appends a page sized in POINTS and returns a Canvas whose
// drawing coordinates are points with a top-left origin (Y-down).
func (d *PDFDoc) NewPage(widthPts, heightPts float64) Canvas {
	return d.newPage(widthPts, heightPts, float32(widthPts), float32(heightPts), 1.0)
}

// NewPageLogical appends a page whose drawing coordinates are logical
// pixels at pxPerInch (96 matches qui's on-screen convention). The
// resulting MediaBox is widthPx/pxPerInch inches wide.
func (d *PDFDoc) NewPageLogical(widthPx, heightPx float32, pxPerInch float64) Canvas {
	if pxPerInch <= 0 {
		pxPerInch = 96
	}
	s := 72.0 / pxPerInch // points per logical unit
	wPt := float64(widthPx) * s
	hPt := float64(heightPx) * s
	return d.newPage(wPt, hPt, widthPx, heightPx, s)
}

// newPage is the shared constructor. s = points per logical unit. The base
// CTM `s 0 0 -s 0 hPt cm` maps logical (x, y) top-left/Y-down into PDF
// points bottom-left/Y-up, so all subsequent operators use logical coords.
func (d *PDFDoc) newPage(wPt, hPt float64, wLogical, hLogical float32, s float64) Canvas {
	p := &pdfPage{wPt: wPt, hPt: hPt, wLogical: wLogical, hLogical: hLogical}
	// Base transform: flip Y and scale logical->points.
	fmt.Fprintf(&p.buf, "%s 0 0 %s 0 %s cm\n", pdfNum(s), pdfNum(-s), pdfNum(hPt))
	d.pages = append(d.pages, p)
	cv := &pdfCanvas{
		canvasState: newCanvasState(Rect{W: wLogical, H: hLogical}),
		doc:         d,
		page:        p,
	}
	return cv
}

// pdfObject is one serialized indirect object awaiting an id + byte offset.
type pdfObject struct {
	id   int
	body []byte
}

// Write serializes the document. Font subsetting/embedding runs here.
func (d *PDFDoc) Write(w *bufio.Writer) error {
	// Object id allocation:
	//   1 = Catalog, 2 = Pages, then per page {content, page}, then fonts.
	catalogID := 1
	pagesID := 2
	nextID := 3

	type pageIDs struct{ content, page int }
	pids := make([]pageIDs, len(d.pages))
	for i := range d.pages {
		pids[i].content = nextID
		nextID++
		pids[i].page = nextID
		nextID++
	}

	// Font objects (Type0/CIDFont/descriptor/fontfile/tounicode).
	fontObjs, fontRes, err := d.fonts.buildObjects(&nextID)
	if err != nil {
		return err
	}
	imageObjs, imageIDs := d.buildImageObjects(&nextID)
	gsObjs, gsIDs := d.buildGStateObjects(&nextID)

	var objs []pdfObject

	// Catalog + Pages tree.
	objs = append(objs, pdfObject{catalogID, []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesID))})
	var kids bytes.Buffer
	kids.WriteString("[ ")
	for _, p := range pids {
		fmt.Fprintf(&kids, "%d 0 R ", p.page)
	}
	kids.WriteString("]")
	objs = append(objs, pdfObject{pagesID, []byte(fmt.Sprintf("<< /Type /Pages /Kids %s /Count %d >>", kids.String(), len(d.pages)))})

	// Pages + content streams.
	for i, p := range d.pages {
		content := p.buf.Bytes()
		var cs bytes.Buffer
		fmt.Fprintf(&cs, "<< /Length %d >>\nstream\n", len(content))
		cs.Write(content)
		cs.WriteString("\nendstream")
		objs = append(objs, pdfObject{pids[i].content, cs.Bytes()})

		var pg bytes.Buffer
		xobjects := ""
		if res := xobjectResources(p.imageNames, imageIDs); res != "" {
			xobjects = fmt.Sprintf(" /XObject << %s>>", res)
		}
		gstates := ""
		if res := gstateResources(p.gsNames, gsIDs); res != "" {
			gstates = fmt.Sprintf(" /ExtGState << %s>>", res)
		}
		fmt.Fprintf(&pg, "<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Contents %d 0 R /Resources << /Font << %s >>%s%s >> >>",
			pagesID, pdfNum(p.wPt), pdfNum(p.hPt), pids[i].content, fontRes, xobjects, gstates)
		objs = append(objs, pdfObject{pids[i].page, pg.Bytes()})
	}

	objs = append(objs, fontObjs...)
	objs = append(objs, imageObjs...)
	objs = append(objs, gsObjs...)

	// Serialize with an xref table.
	offsets := make(map[int]int64)
	var pos int64
	writeStr := func(s string) error {
		n, e := w.WriteString(s)
		pos += int64(n)
		return e
	}
	writeBytes := func(b []byte) error {
		n, e := w.Write(b)
		pos += int64(n)
		return e
	}

	if err := writeStr("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"); err != nil {
		return err
	}
	// Objects sorted by id (they already are by construction, but be safe).
	maxID := nextID - 1
	byID := make(map[int]pdfObject, len(objs))
	for _, o := range objs {
		byID[o.id] = o
	}
	for id := 1; id <= maxID; id++ {
		o, ok := byID[id]
		if !ok {
			continue
		}
		offsets[id] = pos
		if err := writeStr(fmt.Sprintf("%d 0 obj\n", id)); err != nil {
			return err
		}
		if err := writeBytes(o.body); err != nil {
			return err
		}
		if err := writeStr("\nendobj\n"); err != nil {
			return err
		}
	}

	xrefPos := pos
	if err := writeStr(fmt.Sprintf("xref\n0 %d\n", maxID+1)); err != nil {
		return err
	}
	if err := writeStr("0000000000 65535 f \n"); err != nil {
		return err
	}
	for id := 1; id <= maxID; id++ {
		off, ok := offsets[id]
		if !ok {
			// Free entry for a gap (shouldn't happen).
			if err := writeStr("0000000000 65535 f \n"); err != nil {
				return err
			}
			continue
		}
		if err := writeStr(fmt.Sprintf("%010d 00000 n \n", off)); err != nil {
			return err
		}
	}
	if err := writeStr(fmt.Sprintf("trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", maxID+1, catalogID, xrefPos)); err != nil {
		return err
	}
	return nil
}

// WritePDFFile serializes the document to a file.
func (d *PDFDoc) WritePDFFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	if err := d.Write(bw); err != nil {
		return err
	}
	return bw.Flush()
}

// Bytes serializes the document to an in-memory byte slice (used by the
// native print bridge).
func (d *PDFDoc) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	if err := d.Write(bw); err != nil {
		return nil, err
	}
	if err := bw.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PageCount reports how many pages have been added.
func (d *PDFDoc) PageCount() int { return len(d.pages) }

// pdfNum formats a float for a PDF content stream: fixed notation (never
// scientific), trailing zeros trimmed, up to 4 decimals.
func pdfNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 4, 64)
	// Trim trailing zeros and a dangling dot.
	if bytes.ContainsRune([]byte(s), '.') {
		i := len(s)
		for i > 0 && s[i-1] == '0' {
			i--
		}
		if i > 0 && s[i-1] == '.' {
			i--
		}
		s = s[:i]
	}
	if s == "-0" {
		return "0"
	}
	return s
}
