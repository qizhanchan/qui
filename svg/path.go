package svg

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/rasterx"
)

// PathOp identifies one SVG path command. Lowercase / relative variants
// in the d string get normalised to the absolute form during parse, so
// the renderer / serializer only deal with absolute positions. The
// origin of relative-vs-absolute lives in the parser, not the data
// model — keeps the IR small and unambiguous.
type PathOp uint8

const (
	OpMoveTo PathOp = iota + 1
	OpLineTo
	OpCurveTo // cubic bezier (C)
	OpQuadTo  // quadratic bezier (Q)
	OpArcTo
	OpClose
)

// PathCommand is one node in a Path. Args holds operand-specific
// floats; the layout per op is:
//
//	OpMoveTo / OpLineTo:  Args[0]=x, Args[1]=y
//	OpCurveTo:            Args[0..1]=cp1, Args[2..3]=cp2, Args[4..5]=end
//	OpQuadTo:             Args[0..1]=cp,  Args[2..3]=end
//	OpArcTo:              Args[0]=rx, [1]=ry, [2]=xAxisDeg,
//	                       [3]=largeArc(0/1), [4]=sweep(0/1),
//	                       [5]=x, [6]=y
//	OpClose:              (no args)
type PathCommand struct {
	Op   PathOp
	Args [7]float32
}

// Path is the <path> element with explicit command list. Build a Path
// directly with struct literals, parse one with ParsePath, or use the
// fluent MoveTo / LineTo / ... builder methods.
type Path struct {
	elementBase
	Commands []PathCommand
}

// NewPath returns an empty Path ready for chained builder calls.
func NewPath() *Path { return &Path{} }

// MoveTo appends an M command.
func (p *Path) MoveTo(x, y float32) *Path {
	p.Commands = append(p.Commands, PathCommand{Op: OpMoveTo, Args: [7]float32{x, y}})
	return p
}

// LineTo appends an L command.
func (p *Path) LineTo(x, y float32) *Path {
	p.Commands = append(p.Commands, PathCommand{Op: OpLineTo, Args: [7]float32{x, y}})
	return p
}

// HLineTo / VLineTo are sugar — there's no dedicated op, they become
// LineTo against the current cursor. To resolve the cursor at build
// time we walk Commands; that's O(n) per call but Path builders aren't
// hot.
func (p *Path) HLineTo(x float32) *Path {
	_, cy := p.currentPoint()
	return p.LineTo(x, cy)
}

func (p *Path) VLineTo(y float32) *Path {
	cx, _ := p.currentPoint()
	return p.LineTo(cx, y)
}

// CurveTo appends a cubic bezier (C) command.
func (p *Path) CurveTo(x1, y1, x2, y2, x, y float32) *Path {
	p.Commands = append(p.Commands, PathCommand{Op: OpCurveTo, Args: [7]float32{x1, y1, x2, y2, x, y}})
	return p
}

// QuadTo appends a quadratic bezier (Q) command.
func (p *Path) QuadTo(x1, y1, x, y float32) *Path {
	p.Commands = append(p.Commands, PathCommand{Op: OpQuadTo, Args: [7]float32{x1, y1, x, y}})
	return p
}

// ArcTo appends an elliptical-arc (A) command.
func (p *Path) ArcTo(rx, ry, xAxisDeg float32, largeArc, sweep bool, x, y float32) *Path {
	la, sw := float32(0), float32(0)
	if largeArc {
		la = 1
	}
	if sweep {
		sw = 1
	}
	p.Commands = append(p.Commands, PathCommand{
		Op:   OpArcTo,
		Args: [7]float32{rx, ry, xAxisDeg, la, sw, x, y},
	})
	return p
}

// Close appends a Z command.
func (p *Path) Close() *Path {
	p.Commands = append(p.Commands, PathCommand{Op: OpClose})
	return p
}

// currentPoint walks back to find the end of the last command, used by
// HLineTo / VLineTo to resolve their target y / x. Z snaps to the
// start-of-subpath point.
func (p *Path) currentPoint() (float32, float32) {
	startX, startY := float32(0), float32(0)
	curX, curY := float32(0), float32(0)
	for _, c := range p.Commands {
		switch c.Op {
		case OpMoveTo:
			startX, startY = c.Args[0], c.Args[1]
			curX, curY = c.Args[0], c.Args[1]
		case OpLineTo:
			curX, curY = c.Args[0], c.Args[1]
		case OpCurveTo:
			curX, curY = c.Args[4], c.Args[5]
		case OpQuadTo:
			curX, curY = c.Args[2], c.Args[3]
		case OpArcTo:
			curX, curY = c.Args[5], c.Args[6]
		case OpClose:
			curX, curY = startX, startY
		}
	}
	return curX, curY
}

// Bounds approximates the path's bbox by checking command endpoints +
// control points — not exact for curves (the curve can swing past its
// hull) but tight enough for hit testing and consistent with what most
// SVG viewers report.
func (p *Path) Bounds() ViewBox {
	if len(p.Commands) == 0 {
		return ViewBox{}
	}
	first := true
	var minX, minY, maxX, maxY float32
	put := func(x, y float32) {
		if first {
			minX, maxX = x, x
			minY, maxY = y, y
			first = false
			return
		}
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}
	for _, c := range p.Commands {
		switch c.Op {
		case OpMoveTo, OpLineTo:
			put(c.Args[0], c.Args[1])
		case OpCurveTo:
			put(c.Args[0], c.Args[1])
			put(c.Args[2], c.Args[3])
			put(c.Args[4], c.Args[5])
		case OpQuadTo:
			put(c.Args[0], c.Args[1])
			put(c.Args[2], c.Args[3])
		case OpArcTo:
			put(c.Args[5], c.Args[6])
		}
	}
	if first {
		return ViewBox{}
	}
	return ViewBox{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}
}

func (p *Path) draw(d *rasterx.Dasher, m rasterx.Matrix2D, s resolvedStyle, opacity float64) {
	if len(p.Commands) == 0 {
		return
	}
	add := func(ad rasterx.Adder) {
		feedPathCommands(ad, p.Commands)
	}
	drawShape(d, m, s, opacity, add)
}

// feedPathCommands replays the IR into a rasterx Adder. Maintains
// subpath start (for Z), current point (for arcs), and emits a fresh
// Start at each MoveTo. AddArc reuses rasterx's helper but its input
// shape is awkward (needs a pre-built float64 slice and the current
// point), so we marshal accordingly.
func feedPathCommands(ad rasterx.Adder, cmds []PathCommand) {
	var startX, startY, curX, curY float64
	started := false
	stopOpen := func() {
		if started {
			ad.Stop(false)
			started = false
		}
	}
	for _, c := range cmds {
		switch c.Op {
		case OpMoveTo:
			stopOpen()
			startX, startY = float64(c.Args[0]), float64(c.Args[1])
			curX, curY = startX, startY
			ad.Start(rasterx.ToFixedP(curX, curY))
			started = true
		case OpLineTo:
			curX, curY = float64(c.Args[0]), float64(c.Args[1])
			ad.Line(rasterx.ToFixedP(curX, curY))
		case OpCurveTo:
			b := rasterx.ToFixedP(float64(c.Args[0]), float64(c.Args[1]))
			c2 := rasterx.ToFixedP(float64(c.Args[2]), float64(c.Args[3]))
			d2 := rasterx.ToFixedP(float64(c.Args[4]), float64(c.Args[5]))
			ad.CubeBezier(b, c2, d2)
			curX, curY = float64(c.Args[4]), float64(c.Args[5])
		case OpQuadTo:
			b := rasterx.ToFixedP(float64(c.Args[0]), float64(c.Args[1]))
			c2 := rasterx.ToFixedP(float64(c.Args[2]), float64(c.Args[3]))
			ad.QuadBezier(b, c2)
			curX, curY = float64(c.Args[2]), float64(c.Args[3])
		case OpArcTo:
			// SVG path A: rx ry xRotDeg largeArc sweep endX endY. rasterx.AddArc
			// is the wrong abstraction layer though: it takes the *ellipse
			// center*, not the SVG arc parameters. We compute the center from
			// the endpoint form via FindEllipseCenter (which also possibly
			// scales rx/ry if the endpoints are farther apart than the ellipse
			// can span), then build the 7-element points slice AddArc indexes.
			rx := float64(c.Args[0])
			ry := float64(c.Args[1])
			rotDeg := float64(c.Args[2])
			rotRad := rotDeg * math.Pi / 180
			largeArc := c.Args[3] != 0
			sweep := c.Args[4] != 0
			endX := float64(c.Args[5])
			endY := float64(c.Args[6])
			if rx <= 0 || ry <= 0 || (endX == curX && endY == curY) {
				// Degenerate arc — SVG spec: render as a line to (x,y).
				if endX != curX || endY != curY {
					ad.Line(rasterx.ToFixedP(endX, endY))
				}
				curX, curY = endX, endY
				break
			}
			cx, cy := rasterx.FindEllipseCenter(&rx, &ry, rotRad, curX, curY, endX, endY, sweep, !largeArc)
			pts := []float64{rx, ry, rotDeg, float64(c.Args[3]), float64(c.Args[4]), endX, endY}
			lx, ly := rasterx.AddArc(pts, cx, cy, curX, curY, ad)
			curX, curY = lx, ly
		case OpClose:
			ad.Stop(true)
			curX, curY = startX, startY
			started = false
		}
	}
	stopOpen()
}

// D returns the path as an SVG "d" attribute string. We emit absolute
// commands only (M/L/C/Q/A/Z) — no relative encoding, no command
// collapsing. Trades string compactness for parser simplicity.
func (p *Path) D() string {
	var b strings.Builder
	for i, c := range p.Commands {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch c.Op {
		case OpMoveTo:
			b.WriteByte('M')
			writeNums(&b, c.Args[:2])
		case OpLineTo:
			b.WriteByte('L')
			writeNums(&b, c.Args[:2])
		case OpCurveTo:
			b.WriteByte('C')
			writeNums(&b, c.Args[:6])
		case OpQuadTo:
			b.WriteByte('Q')
			writeNums(&b, c.Args[:4])
		case OpArcTo:
			b.WriteByte('A')
			b.WriteByte(' ')
			b.WriteString(fmtNum(c.Args[0]))
			b.WriteByte(' ')
			b.WriteString(fmtNum(c.Args[1]))
			b.WriteByte(' ')
			b.WriteString(fmtNum(c.Args[2]))
			b.WriteByte(' ')
			if c.Args[3] != 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
			b.WriteByte(' ')
			if c.Args[4] != 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
			b.WriteByte(' ')
			b.WriteString(fmtNum(c.Args[5]))
			b.WriteByte(' ')
			b.WriteString(fmtNum(c.Args[6]))
		case OpClose:
			b.WriteByte('Z')
		}
	}
	return b.String()
}

func writeNums(b *strings.Builder, nums []float32) {
	for i, n := range nums {
		if i == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(' ')
		}
		b.WriteString(fmtNum(n))
	}
}

func (p *Path) encode(enc *xml.Encoder) error {
	start := xml.StartElement{Name: xml.Name{Local: "path"}}
	emitBaseAttrs(&start, &p.elementBase)
	d := p.D()
	if d != "" {
		start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "d"}, Value: d})
	}
	return encodeEmpty(enc, start)
}

// floatBool maps a 0/1 float32 onto a bool — used by ArcTo flags so
// the parser can stuff sweep/large-arc into the same float array as
// the radii.
func floatBool(v float32) bool { return v != 0 }

// emitPointsAttr is shared by Polyline/Polygon for serialization.
// "x1,y1 x2,y2 ..." matches SVG's authored form.
func emitPointsAttr(start *xml.StartElement, pts []Point) {
	if len(pts) == 0 {
		return
	}
	var b strings.Builder
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatFloat(float64(p.X), 'f', -1, 32))
		b.WriteByte(',')
		b.WriteString(strconv.FormatFloat(float64(p.Y), 'f', -1, 32))
	}
	start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: "points"}, Value: b.String()})
}
