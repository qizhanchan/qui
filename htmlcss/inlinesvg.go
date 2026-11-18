package htmlcss

import (
	"html"
	"strings"

	"github.com/qizhanchan/qui/svg"
)

// Inline <svg> (Options.ScriptDOM). A browser document keeps its <svg>
// elements as DOM — scripts walk and edit them, frameworks hydrate
// against them — while htmlcss renders the element as a replaced box:
// its subtree is serialized back to SVG markup and drawn through the svg
// package, like an <img> of that markup. RefreshInlineSVG re-reads the
// subtree after a script changed it.

// isImg reports an element rendered through the image path: <img>, or an
// inline <svg> drawn from its own subtree.
func (e *El) isImg() bool { return e.tag == "img" || e.inlineSVG != nil }

// buildInlineSVG makes e (an <svg> element node n) a replaced element.
func (e *El) buildInlineSVG(n *Node) {
	var b strings.Builder
	writeSVGNode(&b, n, true)
	e.setInlineSVGMarkup(b.String())
}

func (e *El) setInlineSVGMarkup(markup string) {
	doc, err := svg.ParseBytes([]byte(markup))
	if err != nil || doc == nil {
		doc = &svg.Document{}
	}
	e.inlineSVG = doc
	e.inlineSVGFill, e.inlineSVGStroke = doc.Root.Style.Fill, doc.Root.Style.Stroke
	e.imgSrc = "\x00reload"
}

// RefreshInlineSVG redraws an inline <svg> from its current subtree
// (after a script changed it). No-op for other elements.
func (e *El) RefreshInlineSVG() {
	if e.inlineSVG == nil {
		return
	}
	var b strings.Builder
	writeSVGEl(&b, e, true)
	e.setInlineSVGMarkup(b.String())
	if e.lastCS != nil {
		e.applyComputed(e.lastCS)
		e.syncChildren()
	}
	e.InvalidateLayout()
}

// InlineSVGRoot returns the inline <svg> element e belongs to (e itself
// or an ancestor), or nil.
func (e *El) InlineSVGRoot() *El {
	for p := e; p != nil; p = p.elParent {
		if p.inlineSVG != nil {
			return p
		}
	}
	return nil
}

func writeSVGNode(b *strings.Builder, n *Node, root bool) {
	switch n.Type {
	case TextNode:
		b.WriteString(html.EscapeString(n.Text))
		return
	case CommentNode:
		return
	}
	b.WriteByte('<')
	b.WriteString(svgTag(n.Tag))
	keys := n.AttrOrder
	if len(keys) != len(n.Attrs) {
		keys = keys[:0:0]
		for k := range n.Attrs {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		writeSVGAttr(b, k, n.Attrs[k])
	}
	if _, ok := n.Attrs["xmlns"]; root && !ok {
		writeSVGAttr(b, "xmlns", "http://www.w3.org/2000/svg")
	}
	b.WriteByte('>')
	for _, c := range n.Children {
		writeSVGNode(b, c, false)
	}
	b.WriteString("</")
	b.WriteString(svgTag(n.Tag))
	b.WriteByte('>')
}

func writeSVGEl(b *strings.Builder, e *El, root bool) {
	switch {
	case e.isTextSeg():
		b.WriteString(html.EscapeString(e.text))
		return
	case e.tag == "#comment":
		return
	}
	b.WriteByte('<')
	b.WriteString(svgTag(e.tag))
	for _, k := range e.AttrNames() {
		v, _ := e.readAttr(k)
		writeSVGAttr(b, k, v)
	}
	if _, ok := e.readAttr("xmlns"); root && !ok {
		writeSVGAttr(b, "xmlns", "http://www.w3.org/2000/svg")
	}
	b.WriteByte('>')
	if e.text != "" {
		b.WriteString(html.EscapeString(e.text))
	}
	for _, k := range e.elementKids {
		if c, ok := k.(*El); ok {
			writeSVGEl(b, c, false)
		}
	}
	b.WriteString("</")
	b.WriteString(svgTag(e.tag))
	b.WriteByte('>')
}

// svgTagNames restores the camelCase SVG element names the HTML parser
// lowercased.
var svgTagNames = map[string]string{
	"lineargradient": "linearGradient", "radialgradient": "radialGradient", "clippath": "clipPath",
	"foreignobject": "foreignObject", "textpath": "textPath", "fegaussianblur": "feGaussianBlur",
	"feoffset": "feOffset", "feblend": "feBlend", "fecolormatrix": "feColorMatrix", "feflood": "feFlood",
	"fecomposite": "feComposite", "femerge": "feMerge", "femergenode": "feMergeNode", "animatetransform": "animateTransform",
}

func svgTag(t string) string {
	if n, ok := svgTagNames[t]; ok {
		return n
	}
	return t
}

// svgAttrNames restores the camelCase SVG attributes the HTML parser
// lowercased (viewBox …); the svg package reads them by their SVG names.
var svgAttrNames = map[string]string{
	"viewbox": "viewBox", "preserveaspectratio": "preserveAspectRatio", "gradientunits": "gradientUnits",
	"gradienttransform": "gradientTransform", "patternunits": "patternUnits", "patterntransform": "patternTransform",
	"clippathunits": "clipPathUnits", "maskunits": "maskUnits", "maskcontentunits": "maskContentUnits",
	"stddeviation": "stdDeviation", "spreadmethod": "spreadMethod", "textlength": "textLength",
	"lengthadjust": "lengthAdjust", "markerwidth": "markerWidth", "markerheight": "markerHeight",
	"markerunits": "markerUnits", "refx": "refX", "refy": "refY", "pathlength": "pathLength",
	"xlink:href": "xlink:href",
}

func writeSVGAttr(b *strings.Builder, k, v string) {
	if n, ok := svgAttrNames[k]; ok {
		k = n
	}
	b.WriteByte(' ')
	b.WriteString(k)
	b.WriteString(`="`)
	b.WriteString(html.EscapeString(v))
	b.WriteByte('"')
}

// SVGTagName is an SVG element's name as the DOM reports it (camelCase
// restored: clipPath, linearGradient …).
func SVGTagName(tag string) string { return svgTag(tag) }
