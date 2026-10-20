package diagram

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// DrawioLimits bounds untrusted draw.io XML before it reaches the model.
type DrawioLimits struct {
	MaxBytes     int
	MaxCells     int
	MaxTextBytes int
}

var defaultDrawioLimits = DrawioLimits{MaxBytes: 16 << 20, MaxCells: 100_000, MaxTextBytes: 4 << 20}

type mxFileXML struct {
	XMLName  xml.Name       `xml:"mxfile"`
	Host     string         `xml:"host,attr,omitempty"`
	Modified string         `xml:"modified,attr,omitempty"`
	Version  string         `xml:"version,attr,omitempty"`
	Diagrams []mxDiagramXML `xml:"diagram"`
}

type mxDiagramXML struct {
	XMLName xml.Name         `xml:"diagram"`
	ID      string           `xml:"id,attr,omitempty"`
	Name    string           `xml:"name,attr,omitempty"`
	Model   *mxGraphModelXML `xml:"mxGraphModel"`
	Payload string           `xml:",chardata"`
}

type mxGraphModelXML struct {
	XMLName    xml.Name  `xml:"mxGraphModel"`
	Grid       string    `xml:"grid,attr,omitempty"`
	GridSize   string    `xml:"gridSize,attr,omitempty"`
	Page       string    `xml:"page,attr,omitempty"`
	PageWidth  string    `xml:"pageWidth,attr,omitempty"`
	PageHeight string    `xml:"pageHeight,attr,omitempty"`
	Background string    `xml:"background,attr,omitempty"`
	Root       mxRootXML `xml:"root"`
}

type mxRootXML struct {
	Cells []mxCellXML `xml:"mxCell"`
}

type mxCellXML struct {
	ID       string         `xml:"id,attr,omitempty"`
	Value    string         `xml:"value,attr,omitempty"`
	Style    string         `xml:"style,attr,omitempty"`
	Vertex   string         `xml:"vertex,attr,omitempty"`
	Edge     string         `xml:"edge,attr,omitempty"`
	Parent   string         `xml:"parent,attr,omitempty"`
	Source   string         `xml:"source,attr,omitempty"`
	Target   string         `xml:"target,attr,omitempty"`
	Visible  string         `xml:"visible,attr,omitempty"`
	Locked   string         `xml:"locked,attr,omitempty"`
	Geometry *mxGeometryXML `xml:"mxGeometry"`
}

type mxGeometryXML struct {
	X           string      `xml:"x,attr,omitempty"`
	Y           string      `xml:"y,attr,omitempty"`
	Width       string      `xml:"width,attr,omitempty"`
	Height      string      `xml:"height,attr,omitempty"`
	Relative    string      `xml:"relative,attr,omitempty"`
	As          string      `xml:"as,attr,omitempty"`
	SourcePoint *mxPointXML `xml:"-"`
	TargetPoint *mxPointXML `xml:"-"`
	Array       *mxArrayXML `xml:"-"`
}

type mxPointXML struct {
	X  string `xml:"x,attr,omitempty"`
	Y  string `xml:"y,attr,omitempty"`
	As string `xml:"as,attr,omitempty"`
}

type mxArrayXML struct {
	Points []mxPointXML `xml:"mxPoint"`
}

func (g *mxGeometryXML) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	for _, a := range start.Attr {
		switch a.Name.Local {
		case "x":
			g.X = a.Value
		case "y":
			g.Y = a.Value
		case "width":
			g.Width = a.Value
		case "height":
			g.Height = a.Value
		case "relative":
			g.Relative = a.Value
		case "as":
			g.As = a.Value
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "mxPoint":
				var point mxPointXML
				if err := dec.DecodeElement(&point, &t); err != nil {
					return err
				}
				switch point.As {
				case "sourcePoint":
					g.SourcePoint = &point
				case "targetPoint":
					g.TargetPoint = &point
				}
			case "Array":
				var points mxArrayXML
				if err := dec.DecodeElement(&points, &t); err != nil {
					return err
				}
				g.Array = &points
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func (g mxGeometryXML) MarshalXML(enc *xml.Encoder, start xml.StartElement) error {
	attrs := []struct {
		name  string
		value string
	}{
		{"x", g.X}, {"y", g.Y}, {"width", g.Width}, {"height", g.Height},
		{"relative", g.Relative}, {"as", g.As},
	}
	for _, attr := range attrs {
		if attr.value != "" {
			start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: attr.name}, Value: attr.value})
		}
	}
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if g.SourcePoint != nil {
		if err := enc.EncodeElement(g.SourcePoint, xml.StartElement{Name: xml.Name{Local: "mxPoint"}}); err != nil {
			return err
		}
	}
	if g.TargetPoint != nil {
		if err := enc.EncodeElement(g.TargetPoint, xml.StartElement{Name: xml.Name{Local: "mxPoint"}}); err != nil {
			return err
		}
	}
	if g.Array != nil {
		if err := enc.EncodeElement(g.Array, xml.StartElement{Name: xml.Name{Local: "Array"}}); err != nil {
			return err
		}
	}
	return enc.EncodeToken(start.End())
}

// DecodeDrawio imports mxfile, mxGraphModel and uncompressed/compressed
// draw.io diagrams into the native model. XML entities are decoded by the
// standard library; DTDs and external entities are not supported.
func DecodeDrawio(data []byte) (*Document, error) {
	return DecodeDrawioWithLimits(data, defaultDrawioLimits)
}

// DecodeDrawioBytes is an alias of DecodeDrawio for callers that prefer
// the explicit naming.
func DecodeDrawioBytes(data []byte) (*Document, error) { return DecodeDrawio(data) }

// DecodeDrawioWithLimits imports draw.io XML under caller-supplied bounds.
func DecodeDrawioWithLimits(data []byte, limits DrawioLimits) (*Document, error) {
	if len(data) == 0 {
		return nil, errors.New("diagram: empty draw.io document")
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = defaultDrawioLimits.MaxBytes
	}
	if limits.MaxCells <= 0 {
		limits.MaxCells = defaultDrawioLimits.MaxCells
	}
	if limits.MaxTextBytes <= 0 {
		limits.MaxTextBytes = defaultDrawioLimits.MaxTextBytes
	}
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("diagram: draw.io document exceeds %d bytes", limits.MaxBytes)
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		if end := bytes.Index(trimmed, []byte("?>")); end >= 0 {
			trimmed = bytes.TrimSpace(trimmed[end+2:])
		}
	}
	switch {
	case bytes.HasPrefix(trimmed, []byte("<mxfile")):
		var file mxFileXML
		if err := xml.Unmarshal(trimmed, &file); err != nil {
			return nil, err
		}
		if len(file.Diagrams) == 0 {
			return nil, errors.New("diagram: mxfile has no diagrams")
		}
		doc := &Document{}
		for _, d := range file.Diagrams {
			model, err := decodeDiagramModel(d, limits)
			if err != nil {
				return nil, err
			}
			page, err := modelToPage(model, d, limits)
			if err != nil {
				return nil, err
			}
			doc.Pages = append(doc.Pages, page)
		}
		normalizeDocument(doc)
		if err := doc.Validate(); err != nil {
			return nil, err
		}
		return doc, nil
	case bytes.HasPrefix(trimmed, []byte("<mxGraphModel")):
		var model mxGraphModelXML
		if err := xml.Unmarshal(trimmed, &model); err != nil {
			return nil, err
		}
		page, err := modelToPage(&model, mxDiagramXML{}, limits)
		if err != nil {
			return nil, err
		}
		doc := &Document{Pages: []*Page{page}}
		normalizeDocument(doc)
		if err := doc.Validate(); err != nil {
			return nil, err
		}
		return doc, nil
	case bytes.HasPrefix(trimmed, []byte("<diagram")):
		var d mxDiagramXML
		if err := xml.Unmarshal(trimmed, &d); err != nil {
			return nil, err
		}
		model, err := decodeDiagramModel(d, limits)
		if err != nil {
			return nil, err
		}
		page, err := modelToPage(model, d, limits)
		if err != nil {
			return nil, err
		}
		doc := &Document{Pages: []*Page{page}}
		normalizeDocument(doc)
		if err := doc.Validate(); err != nil {
			return nil, err
		}
		return doc, nil
	default:
		return nil, errors.New("diagram: unsupported draw.io root element")
	}
}

// Reader form of DecodeDrawio.
func DecodeDrawioReader(r io.Reader, limits DrawioLimits) (*Document, error) {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = defaultDrawioLimits.MaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(limits.MaxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limits.MaxBytes {
		return nil, fmt.Errorf("diagram: draw.io document exceeds %d bytes", limits.MaxBytes)
	}
	return DecodeDrawioWithLimits(data, limits)
}

func decodeDiagramModel(d mxDiagramXML, limits DrawioLimits) (*mxGraphModelXML, error) {
	if d.Model != nil {
		return d.Model, nil
	}
	payload := strings.TrimSpace(d.Payload)
	if payload == "" {
		return nil, errors.New("diagram: empty diagram payload")
	}
	decoded, err := decodeDrawioPayload(payload, limits)
	if err != nil {
		return nil, err
	}
	var model mxGraphModelXML
	if err := xml.Unmarshal(decoded, &model); err != nil {
		return nil, err
	}
	return &model, nil
}

func decodeDrawioPayload(payload string, limits DrawioLimits) ([]byte, error) {
	if !strings.HasPrefix(payload, "7V") && !strings.HasPrefix(payload, "7Z") {
		if bytes.Contains([]byte(payload), []byte("<mxGraphModel")) {
			if len(payload) > limits.MaxTextBytes {
				return nil, errors.New("diagram: inline diagram payload too large")
			}
			return []byte(payload), nil
		}
	}
	s := payload
	if unescaped, err := url.PathUnescape(s); err == nil {
		s = unescaped
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(s)
	}
	if err != nil {
		return nil, errors.New("diagram: invalid compressed diagram payload")
	}
	// draw.io stores diagram payloads with raw DEFLATE. Some exporters
	// wrap the stream in a zlib header, so fall back to zlib when needed.
	fr := flate.NewReader(bytes.NewReader(raw))
	out, readErr := io.ReadAll(io.LimitReader(fr, int64(limits.MaxTextBytes)+1))
	_ = fr.Close()
	if readErr == nil {
		if len(out) > limits.MaxTextBytes {
			return nil, errors.New("diagram: decompressed diagram payload too large")
		}
		return out, nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err = io.ReadAll(io.LimitReader(zr, int64(limits.MaxTextBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > limits.MaxTextBytes {
		return nil, errors.New("diagram: decompressed diagram payload too large")
	}
	return out, nil
}

func modelToPage(model *mxGraphModelXML, diagram mxDiagramXML, limits DrawioLimits) (*Page, error) {
	if model == nil {
		return nil, errors.New("diagram: missing mxGraphModel")
	}
	if len(model.Root.Cells) > limits.MaxCells {
		return nil, fmt.Errorf("diagram: cell count %d exceeds limit %d", len(model.Root.Cells), limits.MaxCells)
	}
	page := &Page{
		ID:     diagram.ID,
		Name:   diagram.Name,
		Width:  parseF(model.PageWidth, 800),
		Height: parseF(model.PageHeight, 600),
		Background: func() string {
			if model.Background == "" || model.Background == "none" {
				return "#ffffff"
			}
			return model.Background
		}(),
		Grid: Grid{Visible: model.Grid != "0", Size: parseF(model.GridSize, 10), Color: "#e5e7eb"},
	}
	if page.ID == "" {
		page.ID = "page-1"
	}
	if page.Name == "" {
		page.Name = "Page-1"
	}
	layers := map[string]string{}
	for _, c := range model.Root.Cells {
		if c.ID == "" || c.ID == "0" {
			continue
		}
		if c.Parent == "0" && c.Vertex != "1" && c.Edge != "1" {
			layer := Layer{ID: c.ID, Name: c.Value, Hidden: c.Visible == "0", Locked: c.Locked == "1"}
			if len(page.Layers) == 0 {
				layer.Default = true
			}
			page.Layers = append(page.Layers, layer)
			layers[c.ID] = c.ID
		}
	}
	if len(page.Layers) == 0 {
		page.Layers = []Layer{{ID: "1", Name: "Layer-1", Default: true}}
	}
	cellMap := map[string]*Cell{}
	for _, x := range model.Root.Cells {
		if x.ID == "" || x.ID == "0" {
			continue
		}
		if x.Parent == "0" && x.Vertex != "1" && x.Edge != "1" {
			continue
		}
		cell := &Cell{
			ID: x.ID, Parent: x.Parent, Label: drawioLabel(x.Value),
			Hidden: x.Visible == "0", Locked: x.Locked == "1",
			Style: parseDrawioStyle(x.Style, x.Vertex == "1"),
		}
		if len(cell.Label) > limits.MaxTextBytes {
			return nil, fmt.Errorf("diagram: label on cell %q is too large", x.ID)
		}
		switch {
		case x.Edge == "1" || (x.Source != "" && x.Target != ""):
			cell.Type = CellEdge
			cell.Source = Endpoint{Cell: x.Source}
			cell.Target = Endpoint{Cell: x.Target}
			if x.Geometry != nil {
				cell.Geometry.Points = drawioWaypoints(x.Geometry)
				cell.Geometry.SourcePoint = drawioPoint(x.Geometry.SourcePoint, "sourcePoint")
				cell.Geometry.TargetPoint = drawioPoint(x.Geometry.TargetPoint, "targetPoint")
				cell.Geometry.LabelPosition = parseF(styleValue(x.Style, "labelPosition"), .5)
			}
		default:
			cell.Type = CellVertex
			if isDrawioGroup(x.Style) {
				cell.Type = CellGroup
			}
			if x.Geometry != nil {
				cell.Geometry = Geometry{
					X: parseF(x.Geometry.X, 0), Y: parseF(x.Geometry.Y, 0),
					W: parseF(x.Geometry.Width, 120), H: parseF(x.Geometry.Height, 60),
				}
			}
		}
		cellMap[cell.ID] = cell
		page.Cells = append(page.Cells, cell)
	}
	// Resolve layers through groups after all cells exist.
	for _, c := range page.Cells {
		cur := c
		seen := map[string]bool{}
		for cur != nil && cur.Parent != "" && cur.Parent != "0" {
			if seen[cur.Parent] {
				break
			}
			seen[cur.Parent] = true
			if _, ok := layers[cur.Parent]; ok {
				c.Layer = cur.Parent
				break
			}
			parent, ok := cellMap[cur.Parent]
			if !ok {
				break
			}
			cur = parent
		}
		if c.Layer == "" {
			if len(page.Layers) > 0 {
				c.Layer = page.Layers[0].ID
			}
		}
	}
	normalizeGroupedGeometry(page, layers)
	return page, nil
}

// normalizeGroupedGeometry converts draw.io's parent-relative child
// coordinates into the native document convention: every cell is stored in
// page/world coordinates. Export subtracts the parent origin again.
func normalizeGroupedGeometry(page *Page, layers map[string]string) {
	if page == nil {
		return
	}
	memo := map[string]bool{}
	visiting := map[string]bool{}
	var normalize func(*Cell) bool
	normalize = func(c *Cell) bool {
		if c == nil || memo[c.ID] {
			return true
		}
		if visiting[c.ID] {
			return false
		}
		visiting[c.ID] = true
		if c.Parent != "" {
			if _, isLayer := layers[c.Parent]; !isLayer {
				parent := page.Cell(c.Parent)
				if parent == nil || !normalize(parent) {
					return false
				}
				c.Geometry.X += parent.Geometry.X
				c.Geometry.Y += parent.Geometry.Y
				for i := range c.Geometry.Points {
					c.Geometry.Points[i].X += parent.Geometry.X
					c.Geometry.Points[i].Y += parent.Geometry.Y
				}
				if c.Geometry.SourcePoint != nil {
					c.Geometry.SourcePoint.X += parent.Geometry.X
					c.Geometry.SourcePoint.Y += parent.Geometry.Y
				}
				if c.Geometry.TargetPoint != nil {
					c.Geometry.TargetPoint.X += parent.Geometry.X
					c.Geometry.TargetPoint.Y += parent.Geometry.Y
				}
			}
		}
		visiting[c.ID] = false
		memo[c.ID] = true
		return true
	}
	for _, c := range page.Cells {
		normalize(c)
	}
}

func drawioLabel(value string) string {
	if value == "" {
		return ""
	}
	v := html.UnescapeString(value)
	replacer := strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "</div>", "\n", "</p>", "\n")
	v = replacer.Replace(v)
	var b strings.Builder
	inTag := false
	for _, r := range v {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(b.String(), "&nbsp;", " "))
}

func parseDrawioStyle(raw string, vertex bool) Style {
	values := drawioStyleMap(raw)
	style := Style{}
	if vertex {
		style.Shape = drawioShape(raw, values)
		style.Fill = values["fillColor"]
		style.Stroke = values["strokeColor"]
		style.StrokeW = parseF(values["strokeWidth"], 0)
		style.Dashed = parseB(values["dashed"], false)
		style.Opacity = parseF(values["opacity"], 1)
		style.Radius = parseF(values["arcSize"], 0)
		style.ShapeSize = parseF(values["size"], 0)
		style.Shadow = parseB(values["shadow"], false)
	} else {
		style.Route = drawioRoute(raw, values)
		style.Stroke = values["strokeColor"]
		style.StrokeW = parseF(values["strokeWidth"], 0)
		style.Dashed = parseB(values["dashed"], false)
		style.Opacity = parseF(values["opacity"], 1)
		style.StartArrow = drawioArrow(values["startArrow"])
		style.EndArrow = drawioArrow(values["endArrow"])
		style.ArrowSize = parseF(values["endSize"], parseF(values["startSize"], 0))
		style.LabelFill = values["labelBackgroundColor"]
		style.LabelOffsetX = parseF(values["labelOffsetX"], 0)
		style.LabelOffsetY = parseF(values["labelOffsetY"], 0)
	}
	style.FontFamily = values["fontFamily"]
	style.FontSize = parseF(values["fontSize"], 0)
	style.FontColor = values["fontColor"]
	fontStyle := parseI(values["fontStyle"], 0)
	style.Bold = fontStyle&1 != 0
	style.Italic = fontStyle&2 != 0
	style.Align = drawioAlign(values["align"])
	style.VAlign = drawioVAlign(values["verticalAlign"])
	style.Wrap = values["whiteSpace"] == "wrap" || parseB(values["html"], false)
	return style
}

func drawioStyleMap(raw string) map[string]string {
	out := map[string]string{}
	for _, token := range strings.Split(raw, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		k, v, ok := strings.Cut(token, "=")
		if !ok {
			out[token] = "1"
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func styleValue(raw, key string) string { return drawioStyleMap(raw)[key] }

func drawioShape(raw string, values map[string]string) ShapeKind {
	shape := strings.ToLower(values["shape"])
	switch {
	case values["text"] == "1":
		return ShapeText
	case strings.Contains(shape, "cylinder"):
		return ShapeCylinder
	case strings.Contains(shape, "document"):
		return ShapeDocument
	case strings.Contains(shape, "actor"):
		return ShapeActor
	case strings.Contains(shape, "cloud"):
		return ShapeCloud
	case strings.Contains(shape, "note"):
		return ShapeNote
	case strings.Contains(shape, "parallelogram"):
		return ShapeParallelogram
	case strings.Contains(shape, "hexagon"):
		return ShapeHexagon
	case strings.Contains(raw, "rhombus"):
		return ShapeDiamond
	case strings.Contains(raw, "ellipse"):
		return ShapeEllipse
	case values["rounded"] == "1":
		return ShapeRoundedRect
	default:
		return ShapeRect
	}
}

func drawioRoute(raw string, values map[string]string) RouteKind {
	edgeStyle := strings.ToLower(values["edgeStyle"])
	switch {
	case strings.Contains(edgeStyle, "entityrelation"):
		return RouteEntityRelation
	case parseB(values["curved"], false):
		return RouteCurved
	case values["shape"] == "link":
		return RoutePolyline
	case strings.Contains(edgeStyle, "orthogonal"):
		if values["rounded"] == "1" {
			return RouteRoundedOrthogonal
		}
		return RouteOrthogonal
	case strings.Contains(raw, "polyline"):
		return RoutePolyline
	default:
		return RouteStraight
	}
}

func drawioArrow(v string) ArrowKind {
	switch strings.ToLower(v) {
	case "block", "classic", "classicThin", "blockThin":
		return ArrowTriangle
	case "open", "openThin":
		return ArrowVee
	case "diamond", "diamondThin":
		return ArrowDiamond
	case "oval", "ellipse":
		return ArrowCircle
	case "box":
		return ArrowRect
	default:
		return ArrowNone
	}
}

func drawioAlign(v string) LabelAlign {
	switch strings.ToLower(v) {
	case "left":
		return AlignLeft
	case "right":
		return AlignRight
	default:
		return AlignCenter
	}
}

func drawioVAlign(v string) LabelVAlign {
	switch strings.ToLower(v) {
	case "top":
		return VAlignTop
	case "bottom":
		return VAlignBottom
	default:
		return VAlignMiddle
	}
}

func drawioPoint(p *mxPointXML, role string) *Point {
	if p == nil || (p.As != "" && p.As != role) {
		return nil
	}
	return &Point{X: parseF(p.X, 0), Y: parseF(p.Y, 0)}
}

func drawioWaypoints(g *mxGeometryXML) []Point {
	if g == nil || g.Array == nil {
		return nil
	}
	out := make([]Point, 0, len(g.Array.Points))
	for _, p := range g.Array.Points {
		out = append(out, Point{X: parseF(p.X, 0), Y: parseF(p.Y, 0)})
	}
	return out
}

func isDrawioGroup(raw string) bool {
	m := drawioStyleMap(raw)
	return m["group"] == "1" || m["container"] == "1"
}

func parseF(s string, fallback float32) float32 {
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return fallback
	}
	return float32(v)
}

func parseI(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return v
}

func parseB(s string, fallback bool) bool {
	if s == "" {
		return fallback
	}
	return s == "1" || strings.EqualFold(s, "true")
}

// EncodeDrawio exports the native document as an uncompressed mxfile.
func EncodeDrawio(doc *Document) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("diagram: nil document")
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	file := mxFileXML{Host: "qui", Version: "1.0"}
	for _, page := range doc.Pages {
		if page == nil {
			continue
		}
		file.Diagrams = append(file.Diagrams, pageToDrawio(page))
	}
	data, err := xml.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

func pageToDrawio(page *Page) mxDiagramXML {
	model := &mxGraphModelXML{
		Grid: boolString(page.Grid.Visible), GridSize: fnum(page.Grid.Size),
		Page: "1", PageWidth: fnum(page.Width), PageHeight: fnum(page.Height),
		Background: page.Background,
	}
	model.Root.Cells = append(model.Root.Cells, mxCellXML{ID: "0"})
	for _, layer := range page.Layers {
		model.Root.Cells = append(model.Root.Cells, mxCellXML{
			ID: layer.ID, Value: layer.Name, Parent: "0", Visible: visibleString(layer.Hidden),
		})
	}
	for _, c := range page.Cells {
		if c == nil {
			continue
		}
		parent := c.Parent
		if parent == "" {
			parent = c.Layer
		}
		if parent == "" && len(page.Layers) > 0 {
			parent = page.Layers[0].ID
		}
		x := mxCellXML{
			ID: c.ID, Value: c.Label, Style: encodeDrawioStyle(c), Parent: parent,
			Visible: visibleString(c.Hidden),
		}
		if c.Locked {
			x.Locked = "1"
		}
		switch c.Type {
		case CellEdge:
			x.Edge = "1"
			x.Source = c.Source.Cell
			x.Target = c.Target.Cell
			g := &mxGeometryXML{As: "geometry", Relative: "1"}
			if len(c.Geometry.Points) > 0 {
				arr := &mxArrayXML{}
				for _, p := range c.Geometry.Points {
					arr.Points = append(arr.Points, mxPointXML{X: fnum(p.X), Y: fnum(p.Y)})
				}
				g.Array = arr
			}
			if c.Geometry.SourcePoint != nil {
				g.SourcePoint = &mxPointXML{X: fnum(c.Geometry.SourcePoint.X), Y: fnum(c.Geometry.SourcePoint.Y), As: "sourcePoint"}
			}
			if c.Geometry.TargetPoint != nil {
				g.TargetPoint = &mxPointXML{X: fnum(c.Geometry.TargetPoint.X), Y: fnum(c.Geometry.TargetPoint.Y), As: "targetPoint"}
			}
			x.Geometry = g
		default:
			x.Vertex = "1"
			gx, gy := c.Geometry.X, c.Geometry.Y
			if parentCell := page.Cell(c.Parent); parentCell != nil {
				gx -= parentCell.Geometry.X
				gy -= parentCell.Geometry.Y
			}
			x.Geometry = &mxGeometryXML{
				X: fnum(gx), Y: fnum(gy),
				Width: fnum(c.Geometry.W), Height: fnum(c.Geometry.H), As: "geometry",
			}
		}
		model.Root.Cells = append(model.Root.Cells, x)
	}
	return mxDiagramXML{ID: page.ID, Name: page.Name, Model: model}
}

func encodeDrawioStyle(c *Cell) string {
	s := c.Style
	tokens := make([]string, 0, 24)
	if c.Type == CellGroup {
		tokens = append(tokens, "group")
	}
	switch s.Shape {
	case ShapeEllipse:
		tokens = append(tokens, "ellipse")
	case ShapeDiamond:
		tokens = append(tokens, "rhombus")
	case ShapeRoundedRect:
		tokens = append(tokens, "rounded=1")
	case ShapeParallelogram:
		tokens = append(tokens, "shape=parallelogram")
	case ShapeHexagon:
		tokens = append(tokens, "shape=hexagon")
	case ShapeCylinder:
		tokens = append(tokens, "shape=cylinder3")
	case ShapeDocument:
		tokens = append(tokens, "shape=document")
	case ShapeActor:
		tokens = append(tokens, "shape=actor")
	case ShapeCloud:
		tokens = append(tokens, "shape=cloud")
	case ShapeNote:
		tokens = append(tokens, "shape=note")
	case ShapeText:
		tokens = append(tokens, "text")
	}
	if s.Fill != "" {
		tokens = append(tokens, "fillColor="+s.Fill)
	}
	if s.Stroke != "" {
		tokens = append(tokens, "strokeColor="+s.Stroke)
	}
	if s.StrokeW > 0 {
		tokens = append(tokens, "strokeWidth="+fnum(s.StrokeW))
	}
	if s.Dashed {
		tokens = append(tokens, "dashed=1")
	}
	if s.Opacity > 0 && s.Opacity != 1 {
		tokens = append(tokens, "opacity="+fnum(s.Opacity))
	}
	if c.Type == CellEdge {
		switch s.Route {
		case RouteOrthogonal:
			tokens = append(tokens, "edgeStyle=orthogonalEdgeStyle")
		case RouteRoundedOrthogonal:
			tokens = append(tokens, "edgeStyle=orthogonalEdgeStyle", "rounded=1")
		case RouteEntityRelation:
			tokens = append(tokens, "edgeStyle=entityRelationEdgeStyle")
		case RouteCurved:
			tokens = append(tokens, "curved=1")
		case RoutePolyline:
			tokens = append(tokens, "edgeStyle=orthogonalEdgeStyle")
		}
	}
	if s.StartArrow != "" && s.StartArrow != ArrowNone {
		tokens = append(tokens, "startArrow="+encodeArrow(s.StartArrow))
	}
	if s.EndArrow != "" && s.EndArrow != ArrowNone {
		tokens = append(tokens, "endArrow="+encodeArrow(s.EndArrow))
	}
	if s.ShapeSize > 0 {
		tokens = append(tokens, "size="+fnum(s.ShapeSize))
	}
	if s.FontFamily != "" {
		tokens = append(tokens, "fontFamily="+s.FontFamily)
	}
	if s.FontSize > 0 {
		tokens = append(tokens, "fontSize="+fnum(s.FontSize))
	}
	if s.FontColor != "" {
		tokens = append(tokens, "fontColor="+s.FontColor)
	}
	fs := 0
	if s.Bold {
		fs |= 1
	}
	if s.Italic {
		fs |= 2
	}
	if fs != 0 {
		tokens = append(tokens, "fontStyle="+strconv.Itoa(fs))
	}
	if s.Align != "" {
		tokens = append(tokens, "align="+string(s.Align))
	}
	if s.VAlign != "" {
		tokens = append(tokens, "verticalAlign="+string(s.VAlign))
	}
	if s.Wrap {
		tokens = append(tokens, "whiteSpace=wrap", "html=1")
	}
	return strings.Join(tokens, ";") + ";"
}

func encodeArrow(a ArrowKind) string {
	switch a {
	case ArrowTriangle:
		return "block"
	case ArrowVee:
		return "open"
	case ArrowDiamond:
		return "diamond"
	case ArrowCircle:
		return "oval"
	case ArrowRect:
		return "box"
	default:
		return "none"
	}
}

func fnum(v float32) string {
	if v == float32(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(float64(v), 'f', -1, 32)
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func visibleString(hidden bool) string {
	if hidden {
		return "0"
	}
	return "1"
}
