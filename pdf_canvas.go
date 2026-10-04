package qui

import (
	"fmt"
	"image"
)

// pdfCanvas records qui drawing operations into a PDF page content stream.
// It embeds *canvasState so Save/Restore/Clip/matrix bookkeeping matches
// every other Canvas, and mirrors each state mutation with the equivalent
// PDF operator (q / Q / cm / re W n). Draw coordinates are emitted verbatim
// in the page's user space; the base CTM (set at page creation) handles the
// logical->points scale and the Y-flip.
//
// Invariant: every frame pushed onto canvasState corresponds to exactly one
// emitted `q`, so RestoreTo can emit the matching number of `Q`.
type pdfCanvas struct {
	*canvasState
	doc  *PDFDoc
	page *pdfPage
}

func (c *pdfCanvas) emit(format string, args ...any) {
	fmt.Fprintf(&c.page.buf, format, args...)
}

// --- state stack ---

func (c *pdfCanvas) Save() int {
	c.emit("q\n")
	return c.canvasState.Save()
}

func (c *pdfCanvas) Restore() {
	if len(c.canvasState.stack) <= 1 {
		return
	}
	c.emit("Q\n")
	c.canvasState.Restore()
}

func (c *pdfCanvas) RestoreTo(depth int) {
	if depth < 1 {
		depth = 1
	}
	for len(c.canvasState.stack) > depth {
		c.emit("Q\n")
		c.canvasState.Restore()
	}
}

func (c *pdfCanvas) Scale(sx, sy float32) {
	c.emit("%s 0 0 %s 0 0 cm\n", pdfNum(float64(sx)), pdfNum(float64(sy)))
	c.canvasState.Scale(sx, sy)
}

func (c *pdfCanvas) Translate(dx, dy float32) {
	c.emit("1 0 0 1 %s %s cm\n", pdfNum(float64(dx)), pdfNum(float64(dy)))
	c.canvasState.Translate(dx, dy)
}

func (c *pdfCanvas) Rotate(theta float32) {
	m := RotateMatrix(theta)
	c.concatMatrix(m)
	c.canvasState.Rotate(theta)
}

func (c *pdfCanvas) Concat(m Matrix) {
	c.concatMatrix(m)
	c.canvasState.Concat(m)
}

// concatMatrix emits a qui Matrix as a PDF `cm`. qui stores row-major
// [A B TX; C D TY]; PDF `cm a b c d e f` is column-major, so a=A b=C c=B
// d=D e=TX f=TY.
func (c *pdfCanvas) concatMatrix(m Matrix) {
	c.emit("%s %s %s %s %s %s cm\n",
		pdfNum(float64(m.A)), pdfNum(float64(m.C)),
		pdfNum(float64(m.B)), pdfNum(float64(m.D)),
		pdfNum(float64(m.TX)), pdfNum(float64(m.TY)))
}

func (c *pdfCanvas) ClipRect(rect Rect) {
	c.emit("%s %s %s %s re W n\n",
		pdfNum(float64(rect.X)), pdfNum(float64(rect.Y)),
		pdfNum(float64(rect.W)), pdfNum(float64(rect.H)))
	c.canvasState.ClipRect(rect)
}

// SaveLayer degenerates to a clipped Save (no transparency group in v1),
// matching noopCanvas — one pushed frame, one emitted `q`.
func (c *pdfCanvas) SaveLayer(bounds Rect, _ Paint) int {
	c.emit("q\n")
	depth := c.canvasState.Save()
	c.ClipRect(bounds)
	return depth
}

// ClipPath narrows the clip to the path's bounding box (v1 approximation,
// same as noopCanvas). The caller wraps it in Save/Restore.
func (c *pdfCanvas) ClipPath(path *Path) {
	if path == nil || path.IsEmpty() {
		return
	}
	c.ClipRect(path.Bounds())
}

func (c *pdfCanvas) CurrentMatrix() Matrix { return c.canvasState.CurrentMatrix() }

// --- draw primitives ---

func (c *pdfCanvas) Clear(color Color) {
	c.FillRect(Rect{W: c.page.wLogical, H: c.page.hLogical}, color)
}

func (c *pdfCanvas) setFill(color Color) {
	c.applyAlpha(color.A)
	c.emit("%s %s %s rg\n", pdfNum(float64(color.R)), pdfNum(float64(color.G)), pdfNum(float64(color.B)))
}

func (c *pdfCanvas) setStroke(color Color) {
	c.applyAlpha(color.A)
	c.emit("%s %s %s RG\n", pdfNum(float64(color.R)), pdfNum(float64(color.G)), pdfNum(float64(color.B)))
}

func (c *pdfCanvas) FillRect(rect Rect, color Color) {
	if color.A <= 0 || rect.W <= 0 || rect.H <= 0 {
		return
	}
	c.setFill(color)
	c.emit("%s %s %s %s re f\n",
		pdfNum(float64(rect.X)), pdfNum(float64(rect.Y)),
		pdfNum(float64(rect.W)), pdfNum(float64(rect.H)))
}

func (c *pdfCanvas) FillRoundedRect(rect Rect, _ float32, color Color) {
	// v1: approximate as a plain rect (spreadsheet cells don't need radii).
	c.FillRect(rect, color)
}

func (c *pdfCanvas) StrokeRect(rect Rect, color Color, width float32) {
	if color.A <= 0 || width <= 0 {
		return
	}
	c.setStroke(color)
	c.emit("%s w\n", pdfNum(float64(width)))
	c.emit("%s %s %s %s re S\n",
		pdfNum(float64(rect.X)), pdfNum(float64(rect.Y)),
		pdfNum(float64(rect.W)), pdfNum(float64(rect.H)))
}

func (c *pdfCanvas) StrokeRoundedRect(rect Rect, _ float32, color Color, width float32) {
	c.StrokeRect(rect, color, width)
}

func (c *pdfCanvas) DrawLine(p1, p2 Point, color Color, width float32) {
	if color.A <= 0 {
		return
	}
	if width <= 0 {
		width = 1
	}
	c.setStroke(color)
	c.emit("%s w\n", pdfNum(float64(width)))
	c.emit("%s %s m %s %s l S\n",
		pdfNum(float64(p1.X)), pdfNum(float64(p1.Y)),
		pdfNum(float64(p2.X)), pdfNum(float64(p2.Y)))
}

func (c *pdfCanvas) DrawPolyline(points []Point, color Color, width float32) {
	if len(points) < 2 || color.A <= 0 {
		return
	}
	if width <= 0 {
		width = 1
	}
	c.setStroke(color)
	c.emit("%s w\n", pdfNum(float64(width)))
	c.emit("%s %s m\n", pdfNum(float64(points[0].X)), pdfNum(float64(points[0].Y)))
	for _, p := range points[1:] {
		c.emit("%s %s l\n", pdfNum(float64(p.X)), pdfNum(float64(p.Y)))
	}
	c.emit("S\n")
}

func (c *pdfCanvas) DrawText(text string, rect Rect, color Color, font Font) {
	c.drawTextRuns(text, rect, color, font)
}

// DrawShape handles the common rect shapes; exotic shapes fall through to
// the convenience-method paths elsewhere. q-excel's cell renderer uses the
// FillRect/DrawText/StrokeRect helpers, so this stays minimal in v1.
func (c *pdfCanvas) DrawShape(shape Shape, paint Paint) {
	r, ok := shape.(ShapeRect)
	if !ok {
		return
	}
	if paint.Style == PaintStroke {
		c.StrokeRect(Rect(r), paint.Color, paint.StrokeWidth)
	} else {
		c.FillRect(Rect(r), paint.Color)
	}
}

// DrawImage places a raster as an image XObject (see pdf_image.go): the
// pixels are embedded once per document and each call is a placement.
//
// The placement matrix flips Y. An XObject is painted through the unit
// square with the image's TOP row at v=1, but the page's base CTM has
// already flipped user space to qui's Y-down convention — so mapping v=1 to
// the rect's top means a negative height and an origin at the bottom edge.
// Without it every picture would print upside down.
func (c *pdfCanvas) DrawImage(img image.Image, rect Rect) {
	if img == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	entry := c.doc.addImage(img)
	if entry == nil {
		return
	}
	c.page.useImage(entry.name)
	c.emit("q\n%s 0 0 %s %s %s cm\n/%s Do\nQ\n",
		pdfNum(float64(rect.W)), pdfNum(float64(-rect.H)),
		pdfNum(float64(rect.X)), pdfNum(float64(rect.Y+rect.H)),
		entry.name)
}

func (c *pdfCanvas) DrawShadow(rect Rect, radius float32, spec ElevationSpec, color Color) {}

func (c *pdfCanvas) DrawVector(src VectorSource, rect Rect, tint Color) {}
