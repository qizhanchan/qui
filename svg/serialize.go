package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// WriteSVG serializes the document as an SVG 1.1 XML document to w.
// Output is the canonical form: xmlns + viewBox + width/height on the
// root, then each child encoded by its concrete type. Style is emitted
// as a single style="..." attribute to keep the per-element output
// compact and the per-attribute logic in one place.
func (d *Document) WriteSVG(w io.Writer) error {
	enc := xml.NewEncoder(w)
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	root := xml.StartElement{Name: xml.Name{Local: "svg"}}
	xmlns := d.XMLNS
	if xmlns == "" {
		xmlns = "http://www.w3.org/2000/svg"
	}
	root.Attr = append(root.Attr,
		xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: xmlns},
	)
	if d.ViewBox.W > 0 || d.ViewBox.H > 0 {
		root.Attr = append(root.Attr, xml.Attr{
			Name:  xml.Name{Local: "viewBox"},
			Value: fmt.Sprintf("%s %s %s %s", fmtNum(d.ViewBox.X), fmtNum(d.ViewBox.Y), fmtNum(d.ViewBox.W), fmtNum(d.ViewBox.H)),
		})
	}
	if d.Width > 0 {
		emitFloatAttr(&root, "width", d.Width, false)
	}
	if d.Height > 0 {
		emitFloatAttr(&root, "height", d.Height, false)
	}
	emitBaseAttrs(&root, &d.Root.elementBase)
	if err := enc.EncodeToken(root); err != nil {
		return err
	}
	for _, child := range d.Root.Children {
		if err := child.encode(enc); err != nil {
			return err
		}
	}
	if err := enc.EncodeToken(root.End()); err != nil {
		return err
	}
	return enc.Flush()
}

// MarshalSVG is the convenience []byte form.
func (d *Document) MarshalSVG() ([]byte, error) {
	var buf bytes.Buffer
	if err := d.WriteSVG(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- shared attribute emit helpers --------------------------------

// emitBaseAttrs writes id / transform / style — the three attributes
// every element + the root <svg> share. Style is encoded as a single
// "style=" attribute (never as individual presentation attrs) so the
// serializer has one place to format paint state.
func emitBaseAttrs(start *xml.StartElement, b *elementBase) {
	if b == nil {
		return
	}
	if b.ID != "" {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "id"}, Value: b.ID})
	}
	if t := b.Transform; t != nil {
		s := t.String()
		if s != "" {
			start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "transform"}, Value: s})
		}
	}
	if s := formatStyle(b.Style); s != "" {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "style"}, Value: s})
	}
}

// emitFloatAttr writes a single numeric attribute. allowZero=false
// elides the attr when v==0 — matching the SVG convention where rect
// without "x" defaults to 0. allowZero=true is for required attrs
// like width/height where 0 is meaningful and should still round-trip.
func emitFloatAttr(start *xml.StartElement, name string, v float32, allowZero bool) {
	if v == 0 && !allowZero {
		return
	}
	start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: name}, Value: fmtNum(v)})
}

// formatStyle prints a Style as a CSS declaration list, e.g.
// "fill:#ff0000;stroke:#000;stroke-width:2". Only fields whose Set /
// Kind flag indicates "explicit" are emitted; inherited fields stay
// off the wire so round-tripped cascades preserve their structure.
func formatStyle(s Style) string {
	var parts []string
	if s.Fill.Kind != PaintInherit {
		parts = append(parts, "fill:"+formatPaint(s.Fill))
	}
	if s.Stroke.Kind != PaintInherit {
		parts = append(parts, "stroke:"+formatPaint(s.Stroke))
	}
	if s.StrokeWidth.Set {
		parts = append(parts, "stroke-width:"+fmtNum(s.StrokeWidth.V))
	}
	if s.Opacity.Set {
		parts = append(parts, "opacity:"+fmtNum(s.Opacity.V))
	}
	if s.FillOpacity.Set {
		parts = append(parts, "fill-opacity:"+fmtNum(s.FillOpacity.V))
	}
	if s.StrokeOpacity.Set {
		parts = append(parts, "stroke-opacity:"+fmtNum(s.StrokeOpacity.V))
	}
	if cap := capName(s.LineCap); cap != "" {
		parts = append(parts, "stroke-linecap:"+cap)
	}
	if join := joinName(s.LineJoin); join != "" {
		parts = append(parts, "stroke-linejoin:"+join)
	}
	if s.MiterLimit.Set {
		parts = append(parts, "stroke-miterlimit:"+fmtNum(s.MiterLimit.V))
	}
	if s.HasDash {
		if len(s.Dash) == 0 {
			parts = append(parts, "stroke-dasharray:none")
		} else {
			var b strings.Builder
			for i, v := range s.Dash {
				if i > 0 {
					b.WriteByte(',')
				}
				b.WriteString(fmtNum(v))
			}
			parts = append(parts, "stroke-dasharray:"+b.String())
		}
	}
	if s.DashOffset.Set {
		parts = append(parts, "stroke-dashoffset:"+fmtNum(s.DashOffset.V))
	}
	switch s.FillRule {
	case FillRuleNonZero:
		parts = append(parts, "fill-rule:nonzero")
	case FillRuleEvenOdd:
		parts = append(parts, "fill-rule:evenodd")
	}
	return strings.Join(parts, ";")
}

func formatPaint(p Paint) string {
	switch p.Kind {
	case PaintNone:
		return "none"
	case PaintSolid:
		return formatColor(p.Color)
	}
	return ""
}

func capName(c LineCap) string {
	switch c {
	case LineCapButt:
		return "butt"
	case LineCapRound:
		return "round"
	case LineCapSquare:
		return "square"
	}
	return ""
}

func joinName(j LineJoin) string {
	switch j {
	case LineJoinMiter:
		return "miter"
	case LineJoinRound:
		return "round"
	case LineJoinBevel:
		return "bevel"
	}
	return ""
}
