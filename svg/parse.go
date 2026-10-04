package svg

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// parseSVG drives the XML decoder. The high-level shape: pull tokens
// off the stream, dispatch by start-element local name; <svg> fills
// in document metadata, every recognised shape becomes an Element
// appended to its parent group. <defs>/<title>/<desc>/<metadata> get
// their subtrees consumed silently; unknown elements similarly skip.
//
// The parser is permissive about namespaces — it ignores the URI part
// of qualified names so SVG documents that declare xmlns:xlink etc.
// parse the same as plain ones.
func parseSVG(r io.Reader, into *Document) error {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		// Most SVG files declare UTF-8; treat any other charset as
		// pass-through. We don't pull in golang.org/x/text just for
		// the rare windows-1252 SVG.
		return input, nil
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("svg: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "svg" {
			if err := parseSVGRoot(dec, start, into); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func parseSVGRoot(dec *xml.Decoder, start xml.StartElement, doc *Document) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "viewBox":
			vb, err := parseViewBoxAttr(a.Value)
			if err != nil {
				return fmt.Errorf("svg: viewBox: %w", err)
			}
			doc.ViewBox = vb
		case "width":
			doc.Width = parseDimension(a.Value)
		case "height":
			doc.Height = parseDimension(a.Value)
		case "xmlns":
			doc.XMLNS = a.Value
		case "id":
			doc.Root.ID = a.Value
		case "transform":
			t, err := parseTransformAttr(a.Value)
			if err != nil {
				return err
			}
			doc.Root.Transform = t
		case "style":
			applyStyleString(&doc.Root.Style, a.Value)
		default:
			applyPresentationAttr(&doc.Root.Style, a.Name.Local, a.Value)
		}
	}
	children, err := parseChildren(dec, start.End())
	if err != nil {
		return err
	}
	doc.Root.Children = children
	return nil
}

func parseChildren(dec *xml.Decoder, end xml.EndElement) ([]Element, error) {
	var out []Element
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out, fmt.Errorf("svg: unexpected EOF inside <%s>", end.Name.Local)
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			el, err := parseElement(dec, t)
			if err != nil {
				return nil, err
			}
			if el != nil {
				out = append(out, el)
			}
		case xml.EndElement:
			if t.Name.Local == end.Name.Local {
				return out, nil
			}
		}
	}
}

func parseElement(dec *xml.Decoder, start xml.StartElement) (Element, error) {
	switch start.Name.Local {
	case "g":
		g := &Group{}
		if err := applyBaseAttrs(&g.elementBase, start.Attr); err != nil {
			return nil, err
		}
		kids, err := parseChildren(dec, start.End())
		if err != nil {
			return nil, err
		}
		g.Children = kids
		return g, nil
	case "rect":
		r := &Rect{}
		if err := applyBaseAttrs(&r.elementBase, start.Attr); err != nil {
			return nil, err
		}
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "x":
				r.X = parseDimension(a.Value)
			case "y":
				r.Y = parseDimension(a.Value)
			case "width":
				r.W = parseDimension(a.Value)
			case "height":
				r.H = parseDimension(a.Value)
			case "rx":
				r.RX = parseDimension(a.Value)
			case "ry":
				r.RY = parseDimension(a.Value)
			}
		}
		return r, skipTo(dec, start.End())
	case "circle":
		c := &Circle{}
		if err := applyBaseAttrs(&c.elementBase, start.Attr); err != nil {
			return nil, err
		}
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "cx":
				c.CX = parseDimension(a.Value)
			case "cy":
				c.CY = parseDimension(a.Value)
			case "r":
				c.R = parseDimension(a.Value)
			}
		}
		return c, skipTo(dec, start.End())
	case "ellipse":
		e := &Ellipse{}
		if err := applyBaseAttrs(&e.elementBase, start.Attr); err != nil {
			return nil, err
		}
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "cx":
				e.CX = parseDimension(a.Value)
			case "cy":
				e.CY = parseDimension(a.Value)
			case "rx":
				e.RX = parseDimension(a.Value)
			case "ry":
				e.RY = parseDimension(a.Value)
			}
		}
		return e, skipTo(dec, start.End())
	case "line":
		l := &Line{}
		if err := applyBaseAttrs(&l.elementBase, start.Attr); err != nil {
			return nil, err
		}
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "x1":
				l.X1 = parseDimension(a.Value)
			case "y1":
				l.Y1 = parseDimension(a.Value)
			case "x2":
				l.X2 = parseDimension(a.Value)
			case "y2":
				l.Y2 = parseDimension(a.Value)
			}
		}
		return l, skipTo(dec, start.End())
	case "polyline":
		pl := &Polyline{}
		if err := applyBaseAttrs(&pl.elementBase, start.Attr); err != nil {
			return nil, err
		}
		pl.Points = pointsAttr(start.Attr)
		return pl, skipTo(dec, start.End())
	case "polygon":
		pg := &Polygon{}
		if err := applyBaseAttrs(&pg.elementBase, start.Attr); err != nil {
			return nil, err
		}
		pg.Points = pointsAttr(start.Attr)
		return pg, skipTo(dec, start.End())
	case "path":
		p := &Path{}
		if err := applyBaseAttrs(&p.elementBase, start.Attr); err != nil {
			return nil, err
		}
		for _, a := range start.Attr {
			if a.Name.Local == "d" {
				parsed, err := ParsePath(a.Value)
				if err != nil {
					return nil, err
				}
				p.Commands = parsed.Commands
			}
		}
		return p, skipTo(dec, start.End())
	case "defs", "title", "desc", "metadata":
		// Recognised-but-unused — skip the subtree so following
		// siblings parse correctly.
		return nil, skipTo(dec, start.End())
	}
	// Unknown element — silently skip its subtree.
	return nil, skipTo(dec, start.End())
}

// skipTo consumes tokens until the current element closes. Tracks
// depth so nested children don't trip the outer return.
func skipTo(dec *xml.Decoder, _ xml.EndElement) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return nil
}

// applyBaseAttrs handles the id/transform/style/presentation attrs that
// any element can carry. style="..." applies last so it wins over
// individual presentation attributes — matching CSS precedence.
func applyBaseAttrs(b *elementBase, attrs []xml.Attr) error {
	// Pass 1: presentation attrs (lower precedence).
	for _, a := range attrs {
		switch a.Name.Local {
		case "id":
			b.ID = a.Value
		case "transform":
			t, err := parseTransformAttr(a.Value)
			if err != nil {
				return err
			}
			b.Transform = t
		case "style":
			// handled in pass 2
		default:
			applyPresentationAttr(&b.Style, a.Name.Local, a.Value)
		}
	}
	// Pass 2: style="..." wins per CSS precedence
	for _, a := range attrs {
		if a.Name.Local == "style" {
			applyStyleString(&b.Style, a.Value)
		}
	}
	return nil
}

// applyPresentationAttr writes one SVG presentation attribute onto a
// Style, e.g. ("fill", "red") → Style.Fill = SolidPaint(red). Unknown
// names are silently ignored — there are dozens of presentation attrs
// in the spec and we deliberately only support the v1 list.
func applyPresentationAttr(s *Style, name, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	switch name {
	case "fill":
		if p, err := parsePaint(value); err == nil {
			s.Fill = p
		}
	case "stroke":
		if p, err := parsePaint(value); err == nil {
			s.Stroke = p
		}
	case "stroke-width":
		s.StrokeWidth = SetLength(parseDimension(value))
	case "opacity":
		s.Opacity = SetLength(parseDimension(value))
	case "fill-opacity":
		s.FillOpacity = SetLength(parseDimension(value))
	case "stroke-opacity":
		s.StrokeOpacity = SetLength(parseDimension(value))
	case "stroke-linecap":
		s.LineCap = parseLineCap(value)
	case "stroke-linejoin":
		s.LineJoin = parseLineJoin(value)
	case "stroke-miterlimit":
		s.MiterLimit = SetLength(parseDimension(value))
	case "stroke-dasharray":
		applyDashArray(s, value)
	case "stroke-dashoffset":
		s.DashOffset = SetLength(parseDimension(value))
	case "fill-rule":
		s.FillRule = parseFillRule(value)
	}
}

// applyStyleString parses a CSS-like declaration list and applies
// each key:value pair as if it were a presentation attribute.
func applyStyleString(s *Style, css string) {
	for _, decl := range strings.Split(css, ";") {
		decl = strings.TrimSpace(decl)
		if decl == "" {
			continue
		}
		colon := strings.IndexByte(decl, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(decl[:colon])
		val := strings.TrimSpace(decl[colon+1:])
		applyPresentationAttr(s, key, val)
	}
}

func parseLineCap(s string) LineCap {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "round":
		return LineCapRound
	case "square":
		return LineCapSquare
	case "butt":
		return LineCapButt
	}
	return LineCapInherit
}

func parseLineJoin(s string) LineJoin {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "round":
		return LineJoinRound
	case "bevel":
		return LineJoinBevel
	case "miter":
		return LineJoinMiter
	}
	return LineJoinInherit
}

func parseFillRule(s string) FillRule {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "evenodd":
		return FillRuleEvenOdd
	case "nonzero":
		return FillRuleNonZero
	}
	return FillRuleInherit
}

func applyDashArray(s *Style, value string) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "none") {
		s.SetNoDash()
		return
	}
	parts, err := parseNumberList(value)
	if err != nil || len(parts) == 0 {
		return
	}
	s.SetDash(parts)
}

// parseDimension strips a trailing "px" (the only unit SVG icons
// commonly carry) and returns the numeric value. Unparseable input
// returns 0 — same behavior as oksvg's coercion.
func parseDimension(s string) float32 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "px") {
		s = strings.TrimSpace(strings.TrimSuffix(s, "px"))
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return 0
	}
	return float32(v)
}

func parseViewBoxAttr(s string) (ViewBox, error) {
	parts, err := parseNumberList(s)
	if err != nil {
		return ViewBox{}, err
	}
	if len(parts) != 4 {
		return ViewBox{}, fmt.Errorf("viewBox needs 4 numbers, got %d", len(parts))
	}
	return ViewBox{X: parts[0], Y: parts[1], W: parts[2], H: parts[3]}, nil
}

// pointsAttr parses a "points=" attribute (used by polyline/polygon)
// into a Point slice. SVG accepts numbers separated by whitespace,
// commas, or any combination — same number-list grammar as everywhere
// else.
func pointsAttr(attrs []xml.Attr) []Point {
	for _, a := range attrs {
		if a.Name.Local != "points" {
			continue
		}
		nums, err := parseNumberList(a.Value)
		if err != nil {
			return nil
		}
		if len(nums)%2 != 0 {
			nums = nums[:len(nums)-1]
		}
		pts := make([]Point, 0, len(nums)/2)
		for i := 0; i+1 < len(nums); i += 2 {
			pts = append(pts, Point{X: nums[i], Y: nums[i+1]})
		}
		return pts
	}
	return nil
}

// Parse reads an SVG document from r and returns the parsed *Document.
// Caller is responsible for closing r.
func Parse(r io.Reader) (*Document, error) {
	d := &Document{}
	if err := parseSVG(r, d); err != nil {
		return nil, err
	}
	return d, nil
}

// ParseBytes is the in-memory form — useful for go:embed assets.
func ParseBytes(data []byte) (*Document, error) {
	return Parse(bytes.NewReader(data))
}
