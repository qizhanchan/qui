package qui

import (
	"bytes"
	"fmt"
	"sort"
)

// Constant alpha in the PDF backend.
//
// A PDF colour operator (`rg` / `RG`) carries no alpha — transparency is a
// graphics-state parameter, set through an /ExtGState resource (/ca for
// fills, /CA for strokes). Without one, everything exported opaque: faint
// text printed at full strength, and a half-transparent watermark came out
// as solid ink on paper while looking right on screen.
//
// One state per distinct 8-bit alpha, shared across pages the way fonts and
// images are; a page names only the ones it uses.

// alphaResourceName is the /ExtGState key for an alpha byte.
func alphaResourceName(a uint8) string { return fmt.Sprintf("GA%d", a) }

// useAlpha registers the alpha with the document and the page, returning the
// resource name to emit, and whether anything needs emitting at all.
func (c *pdfCanvas) useAlpha(a float32) (string, bool) {
	b := toByte(a)
	if b == 255 && !c.page.usesAlpha {
		// Nothing translucent has been drawn on this page, so the default
		// state is already fully opaque: no resource, no operator.
		return "", false
	}
	name := alphaResourceName(b)
	if b != 255 {
		c.page.usesAlpha = true
	}
	if c.doc.alphas == nil {
		c.doc.alphas = map[uint8]bool{}
	}
	c.doc.alphas[b] = true
	c.page.useGState(name)
	return name, true
}

// applyAlpha emits the graphics-state switch for a colour's alpha. It is
// called before every fill / stroke / text run, because the state is sticky:
// a translucent shape would otherwise fade everything drawn after it.
func (c *pdfCanvas) applyAlpha(a float32) {
	if name, ok := c.useAlpha(a); ok {
		c.emit("/%s gs\n", name)
	}
}

func (p *pdfPage) useGState(name string) {
	for _, existing := range p.gsNames {
		if existing == name {
			return
		}
	}
	p.gsNames = append(p.gsNames, name)
}

// buildGStateObjects serializes the alpha states and returns them plus a
// name→id map for the page resource dictionaries.
func (d *PDFDoc) buildGStateObjects(nextID *int) ([]pdfObject, map[string]int) {
	if len(d.alphas) == 0 {
		return nil, nil
	}
	values := make([]int, 0, len(d.alphas))
	for a := range d.alphas {
		values = append(values, int(a))
	}
	sort.Ints(values)

	ids := make(map[string]int, len(values))
	objs := make([]pdfObject, 0, len(values))
	for _, v := range values {
		name := alphaResourceName(uint8(v))
		id := *nextID
		*nextID++
		ids[name] = id
		alpha := float64(v) / 255
		objs = append(objs, pdfObject{id, []byte(fmt.Sprintf(
			"<< /Type /ExtGState /ca %s /CA %s >>", pdfNum(alpha), pdfNum(alpha)))})
	}
	return objs, ids
}

// gstateResources renders a page's /ExtGState dictionary entries.
func gstateResources(names []string, ids map[string]int) string {
	if len(names) == 0 {
		return ""
	}
	var out bytes.Buffer
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			continue
		}
		fmt.Fprintf(&out, "/%s %d 0 R ", name, id)
	}
	return out.String()
}
