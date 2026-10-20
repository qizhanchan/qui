package diagram

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
)

// ShapeKind identifies a built-in vertex silhouette.
type ShapeKind string

const (
	ShapeRect          ShapeKind = "rect"
	ShapeRoundedRect   ShapeKind = "roundedRect"
	ShapeEllipse       ShapeKind = "ellipse"
	ShapeDiamond       ShapeKind = "diamond"
	ShapeParallelogram ShapeKind = "parallelogram"
	ShapeHexagon       ShapeKind = "hexagon"
	ShapeCylinder      ShapeKind = "cylinder"
	ShapeDocument      ShapeKind = "document"
	ShapeActor         ShapeKind = "actor"
	ShapeCloud         ShapeKind = "cloud"
	ShapeNote          ShapeKind = "note"
	ShapeText          ShapeKind = "text"
)

// RouteKind selects an edge routing policy.
type RouteKind string

const (
	RouteStraight          RouteKind = "straight"
	RouteOrthogonal        RouteKind = "orthogonal"
	RouteRoundedOrthogonal RouteKind = "roundedOrthogonal"
	RouteEntityRelation    RouteKind = "entityRelation"
	RouteCurved            RouteKind = "curved"
	RoutePolyline          RouteKind = "polyline"
)

// ArrowKind selects the marker painted at an edge end.
type ArrowKind string

const (
	ArrowNone     ArrowKind = "none"
	ArrowTriangle ArrowKind = "triangle"
	ArrowVee      ArrowKind = "vee"
	ArrowDiamond  ArrowKind = "diamond"
	ArrowCircle   ArrowKind = "circle"
	ArrowRect     ArrowKind = "rect"
)

// LabelAlign is the horizontal label alignment.
type LabelAlign string

const (
	AlignCenter LabelAlign = "center"
	AlignLeft   LabelAlign = "left"
	AlignRight  LabelAlign = "right"
)

// LabelVAlign is the vertical label alignment.
type LabelVAlign string

const (
	VAlignMiddle LabelVAlign = "middle"
	VAlignTop    LabelVAlign = "top"
	VAlignBottom LabelVAlign = "bottom"
)

// Style is the serializable visual style of one cell. Empty fields inherit
// the active Theme; "none" explicitly disables a paint.
type Style struct {
	Shape ShapeKind `json:"shape,omitempty"`
	Route RouteKind `json:"route,omitempty"`

	Fill      string  `json:"fill,omitempty"`
	Stroke    string  `json:"stroke,omitempty"`
	StrokeW   float32 `json:"strokeWidth,omitempty"`
	Dashed    bool    `json:"dashed,omitempty"`
	Opacity   float32 `json:"opacity,omitempty"`
	Radius    float32 `json:"radius,omitempty"`
	ShapeSize float32 `json:"shapeSize,omitempty"`
	Shadow    bool    `json:"shadow,omitempty"`

	StartArrow ArrowKind `json:"startArrow,omitempty"`
	EndArrow   ArrowKind `json:"endArrow,omitempty"`
	ArrowSize  float32   `json:"arrowSize,omitempty"`

	FontFamily string      `json:"fontFamily,omitempty"`
	FontSize   float32     `json:"fontSize,omitempty"`
	FontColor  string      `json:"fontColor,omitempty"`
	Bold       bool        `json:"bold,omitempty"`
	Italic     bool        `json:"italic,omitempty"`
	Align      LabelAlign  `json:"align,omitempty"`
	VAlign     LabelVAlign `json:"vAlign,omitempty"`
	Wrap       bool        `json:"wrap,omitempty"`

	LabelFill    string  `json:"labelFill,omitempty"`
	LabelX       float32 `json:"labelX,omitempty"`
	LabelY       float32 `json:"labelY,omitempty"`
	LabelOffsetX float32 `json:"labelOffsetX,omitempty"`
	LabelOffsetY float32 `json:"labelOffsetY,omitempty"`
}

// Theme supplies diagram defaults. Cell styles override these values.
type Theme struct {
	Background    string `json:"background"`
	Grid          string `json:"grid"`
	Selection     string `json:"selection"`
	SelectionFill string `json:"selectionFill"`
	PortFill      string `json:"portFill"`
	PortRing      string `json:"portRing"`
	Guide         string `json:"guide"`

	DefaultVertex Style `json:"defaultVertex"`
	DefaultEdge   Style `json:"defaultEdge"`
	DefaultGroup  Style `json:"defaultGroup"`
}

// DefaultTheme is draw.io's familiar light appearance.
func DefaultTheme() Theme {
	return Theme{
		Background:    "#ffffff",
		Grid:          "#e5e7eb",
		Selection:     "#2563eb",
		SelectionFill: "rgba(37,99,235,0.12)",
		PortFill:      "#2563eb",
		PortRing:      "#ffffff",
		Guide:         "#ec4899",
		DefaultVertex: Style{
			Shape: ShapeRect, Fill: "#ffffff", Stroke: "#000000",
			StrokeW: 1, FontFamily: "Arial", FontSize: 14, FontColor: "#000000",
			Align: AlignCenter, VAlign: VAlignMiddle, Wrap: true,
		},
		DefaultEdge: Style{
			Route: RouteOrthogonal, Stroke: "#000000", StrokeW: 1,
			StartArrow: ArrowNone, EndArrow: ArrowNone, ArrowSize: 10,
			FontFamily: "Arial", FontSize: 12, FontColor: "#000000",
			LabelFill: "#ffffff", Align: AlignCenter, VAlign: VAlignMiddle,
		},
		DefaultGroup: Style{
			Shape: ShapeRect, Fill: "none", Stroke: "#94a3b8", StrokeW: 1,
			Dashed: true, FontFamily: "Arial", FontSize: 12, FontColor: "#64748b",
			Align: AlignLeft, VAlign: VAlignTop,
		},
	}
}

// ResolvedStyle merges a cell style over the relevant theme defaults.
func ResolvedStyle(s Style, def Style) Style {
	if s.Shape == "" {
		s.Shape = def.Shape
	}
	if s.Shape == "" {
		s.Shape = ShapeRect
	}
	if s.Route == "" {
		s.Route = def.Route
	}
	if s.Route == "" {
		s.Route = RouteStraight
	}
	if s.Fill == "" {
		s.Fill = def.Fill
	}
	if s.Fill == "" {
		s.Fill = "#ffffff"
	}
	if s.Stroke == "" {
		s.Stroke = def.Stroke
	}
	if s.Stroke == "" {
		s.Stroke = "#000000"
	}
	if s.StrokeW == 0 {
		s.StrokeW = def.StrokeW
	}
	if s.StrokeW == 0 {
		s.StrokeW = 1
	}
	if s.Opacity == 0 {
		s.Opacity = 1
	}
	if s.StartArrow == "" {
		s.StartArrow = def.StartArrow
	}
	if s.StartArrow == "" {
		s.StartArrow = ArrowNone
	}
	if s.EndArrow == "" {
		s.EndArrow = def.EndArrow
	}
	if s.EndArrow == "" {
		s.EndArrow = ArrowNone
	}
	if s.ArrowSize == 0 {
		s.ArrowSize = def.ArrowSize
	}
	if s.ArrowSize == 0 {
		s.ArrowSize = 10
	}
	if s.FontFamily == "" {
		s.FontFamily = def.FontFamily
	}
	if s.FontFamily == "" {
		s.FontFamily = "Arial"
	}
	if s.FontSize == 0 {
		s.FontSize = def.FontSize
	}
	if s.FontSize == 0 {
		s.FontSize = 14
	}
	if s.FontColor == "" {
		s.FontColor = def.FontColor
	}
	if s.FontColor == "" {
		s.FontColor = "#111827"
	}
	if s.Align == "" {
		s.Align = def.Align
	}
	if s.Align == "" {
		s.Align = AlignCenter
	}
	if s.VAlign == "" {
		s.VAlign = def.VAlign
	}
	if s.VAlign == "" {
		s.VAlign = VAlignMiddle
	}
	if s.LabelFill == "" {
		s.LabelFill = def.LabelFill
	}
	return s
}

func parsePaint(value, fallback string, defaultAlpha float32) (qui.Color, bool) {
	s := strings.TrimSpace(value)
	if s == "" {
		s = fallback
	}
	if s == "" {
		return qui.Color{}, false
	}
	if strings.EqualFold(s, "none") {
		return qui.Color{}, false
	}
	c, ok := ParseColor(s)
	if !ok {
		return qui.Color{}, false
	}
	c.A *= defaultAlpha
	return c, c.A > 0
}

// ParseColor parses the CSS color forms used by draw.io and diagram's JSON
// surface: #RGB, #RGBA, #RRGGBB, #RRGGBBAA, rgb(), rgba() and transparent.
func ParseColor(value string) (qui.Color, bool) {
	s := strings.ToLower(strings.TrimSpace(value))
	if s == "" {
		return qui.Color{}, false
	}
	if s == "transparent" || s == "none" {
		return qui.Color{A: 0}, true
	}
	if strings.HasPrefix(s, "#") {
		return parseHexColor(s[1:])
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		return parseRGBColor(s[4:len(s)-1], false)
	}
	if strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")") {
		return parseRGBColor(s[5:len(s)-1], true)
	}
	return qui.Color{}, false
}

func parseHexColor(s string) (qui.Color, bool) {
	expand := func(v byte) (byte, bool) {
		n, err := strconv.ParseUint(string([]byte{v}), 16, 8)
		if err != nil {
			return 0, false
		}
		return byte(n*16 + n), true
	}
	switch len(s) {
	case 3, 4:
		parts := make([]byte, len(s))
		for i := range s {
			var ok bool
			parts[i], ok = expand(s[i])
			if !ok {
				return qui.Color{}, false
			}
		}
		a := byte(255)
		if len(parts) == 4 {
			a = parts[3]
		}
		return rgba(parts[0], parts[1], parts[2], a), true
	case 6, 8:
		parts := make([]byte, len(s)/2)
		for i := range parts {
			n, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
			if err != nil {
				return qui.Color{}, false
			}
			parts[i] = byte(n)
		}
		a := byte(255)
		if len(parts) == 4 {
			a = parts[3]
		}
		return rgba(parts[0], parts[1], parts[2], a), true
	default:
		return qui.Color{}, false
	}
}

func parseRGBColor(body string, hasAlpha bool) (qui.Color, bool) {
	parts := strings.Split(body, ",")
	want := 3
	if hasAlpha {
		want = 4
	}
	if len(parts) != want {
		return qui.Color{}, false
	}
	r, ok := parseByteChannel(parts[0])
	if !ok {
		return qui.Color{}, false
	}
	g, ok := parseByteChannel(parts[1])
	if !ok {
		return qui.Color{}, false
	}
	b, ok := parseByteChannel(parts[2])
	if !ok {
		return qui.Color{}, false
	}
	a := byte(255)
	if hasAlpha {
		alpha, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		if err != nil || alpha < 0 || alpha > 1 {
			return qui.Color{}, false
		}
		a = byte(alpha*255 + .5)
	}
	return rgba(r, g, b, a), true
}

func parseByteChannel(value string) (byte, bool) {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || n < 0 || n > 255 {
		return 0, false
	}
	return byte(n + .5), true
}

func rgba(r, g, b, a byte) qui.Color {
	return qui.Color{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: float32(a) / 255}
}

func withOpacity(c qui.Color, opacity float32) qui.Color {
	if opacity <= 0 {
		opacity = 1
	}
	c.A *= opacity
	return c
}
