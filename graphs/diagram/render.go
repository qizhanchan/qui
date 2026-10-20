package diagram

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qizhanchan/qui"
)

// Viewport maps diagram coordinates into widget-local coordinates:
// local = Pan + World*Scale.
type Viewport struct {
	Pan   Point   `json:"pan"`
	Scale float32 `json:"scale"`
}

func (v Viewport) normalized() Viewport {
	if v.Scale <= 0 {
		v.Scale = 1
	}
	return v
}

func (v Viewport) worldToLocal(p Point) Point {
	v = v.normalized()
	return Point{X: v.Pan.X + p.X*v.Scale, Y: v.Pan.Y + p.Y*v.Scale}
}

func (v Viewport) localToWorld(p Point) Point {
	v = v.normalized()
	return Point{X: (p.X - v.Pan.X) / v.Scale, Y: (p.Y - v.Pan.Y) / v.Scale}
}

func (v Viewport) rectToLocal(r Rect) Rect {
	p := v.worldToLocal(Point{X: r.X, Y: r.Y})
	return Rect{X: p.X, Y: p.Y, W: r.W * v.normalized().Scale, H: r.H * v.normalized().Scale}
}

type sceneConfig struct {
	Origin       Point
	Viewport     Viewport
	Clip         Rect
	Theme        Theme
	Shapes       *ShapeRegistry
	Grid         bool
	Selected     map[string]bool
	EditingID    string
	EditingLabel string
	ShowControls bool
	ShowPortsFor string
	ReadOnly     bool
}

type scenePainter struct {
	doc *Document
	cfg sceneConfig
}

func drawScene(canvas qui.Canvas, doc *Document, cfg sceneConfig) {
	if canvas == nil || doc == nil {
		return
	}
	page := doc.Active()
	if page == nil {
		return
	}
	cfg.Viewport = cfg.Viewport.normalized()
	if cfg.Theme.Background == "" {
		cfg.Theme = DefaultTheme()
	}
	if cfg.Shapes == nil {
		cfg.Shapes = DefaultShapeRegistry()
	}
	p := &scenePainter{doc: doc, cfg: cfg}

	bg, ok := ParseColor(page.Background)
	if !ok {
		bg, _ = ParseColor(cfg.Theme.Background)
	}
	canvas.FillRect(qui.Rect{X: cfg.Clip.X, Y: cfg.Clip.Y, W: cfg.Clip.W, H: cfg.Clip.H}, bg)
	depth := canvas.Save()
	canvas.ClipRect(qui.Rect{X: cfg.Clip.X, Y: cfg.Clip.Y, W: cfg.Clip.W, H: cfg.Clip.H})
	canvas.Translate(cfg.Origin.X+cfg.Viewport.Pan.X, cfg.Origin.Y+cfg.Viewport.Pan.Y)
	canvas.Scale(cfg.Viewport.Scale, cfg.Viewport.Scale)

	if cfg.Grid && page.Grid.Visible {
		p.drawGrid(canvas)
	}
	p.drawLayers(canvas)
	p.drawSelectionControls(canvas)
	canvas.RestoreTo(depth)
}

func (p *scenePainter) page() *Page { return p.doc.Active() }

func (p *scenePainter) cellVisible(c *Cell) bool {
	return p.page().CellVisible(c)
}

func (p *scenePainter) orderedCells() []*Cell {
	page := p.page()
	if page == nil {
		return nil
	}
	out := make([]*Cell, 0, len(page.Cells))
	for _, c := range page.Cells {
		if p.cellVisible(c) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Z == out[j].Z {
			return false
		}
		return out[i].Z < out[j].Z
	})
	return out
}

func (p *scenePainter) drawGrid(canvas qui.Canvas) {
	page := p.page()
	if page == nil {
		return
	}
	size := page.Grid.Size
	if size <= 0 {
		size = 10
	}
	// Draw one grid line every 5 minor cells when zoomed far out.
	for size*p.cfg.Viewport.Scale < 6 {
		size *= 5
	}
	world := p.cfg.Viewport.localToWorld(Point{})
	worldEnd := p.cfg.Viewport.localToWorld(Point{X: p.cfg.Clip.W, Y: p.cfg.Clip.H})
	startX := float32(int(world.X/size)-1) * size
	startY := float32(int(world.Y/size)-1) * size
	color, ok := ParseColor(page.Grid.Color)
	if !ok {
		color, _ = ParseColor(p.cfg.Theme.Grid)
	}
	color.A *= .75
	for x := startX; x <= worldEnd.X+size; x += size {
		canvas.DrawLine(qui.Point{X: x, Y: startY}, qui.Point{X: x, Y: worldEnd.Y + size}, color, 1/p.cfg.Viewport.Scale)
	}
	for y := startY; y <= worldEnd.Y+size; y += size {
		canvas.DrawLine(qui.Point{X: startX, Y: y}, qui.Point{X: worldEnd.X + size, Y: y}, color, 1/p.cfg.Viewport.Scale)
	}
}

func (p *scenePainter) drawLayers(canvas qui.Canvas) {
	cells := p.orderedCells()
	// Edges are painted below vertices so their endpoints disappear beneath
	// filled silhouettes. Arrowheads and labels are separate passes above.
	for _, c := range cells {
		if c.Type == CellEdge {
			p.drawEdge(canvas, c)
		}
	}
	for _, c := range cells {
		if c.Type == CellVertex || c.Type == CellGroup {
			p.drawVertex(canvas, c)
		}
	}
	for _, c := range cells {
		if c.Type == CellEdge {
			p.drawEdgeArrows(canvas, c)
		}
	}
	for _, c := range cells {
		if c.Type == CellVertex || c.Type == CellGroup {
			p.drawVertexLabel(canvas, c)
		}
	}
	for _, c := range cells {
		if c.Type == CellEdge && (c.Label != "" || c.ID == p.cfg.EditingID) {
			p.drawEdgeLabel(canvas, c)
		}
	}
	if p.cfg.ShowPortsFor != "" {
		if c := p.page().Cell(p.cfg.ShowPortsFor); c != nil {
			p.drawPorts(canvas, c)
		}
	}
}

func (p *scenePainter) vertexStyle(c *Cell) Style {
	def := p.cfg.Theme.DefaultVertex
	if c.Type == CellGroup {
		def = p.cfg.Theme.DefaultGroup
	}
	return ResolvedStyle(c.Style, def)
}

func (p *scenePainter) edgeStyle(c *Cell) Style {
	return ResolvedStyle(c.Style, p.cfg.Theme.DefaultEdge)
}

func (p *scenePainter) drawVertex(canvas qui.Canvas, c *Cell) {
	style := p.vertexStyle(c)
	fill, _ := parsePaint(style.Fill, "", style.Opacity)
	stroke, _ := parsePaint(style.Stroke, "#000000", style.Opacity)
	selected := p.cfg.Selected[c.ID]
	if selected {
		fill, _ = parsePaint(style.Fill, "", style.Opacity*.93)
		stroke, _ = ParseColor(p.cfg.Theme.Selection)
	}
	depth := canvas.Save()
	if style.Shadow {
		qui.DrawElevation(canvas, qui.Rect{X: c.Geometry.X + 1, Y: c.Geometry.Y + 2, W: c.Geometry.W, H: c.Geometry.H}, 4, 2)
	}
	p.cfg.Shapes.Draw(canvas, style.Shape, c.Bounds(), ShapePaint{
		Fill: fill, Stroke: stroke, StrokeWidth: style.StrokeW, Radius: style.Radius, ShapeSize: style.ShapeSize,
		Dashed: style.Dashed, Opacity: style.Opacity,
	})
	canvas.RestoreTo(depth)
}

func (p *scenePainter) drawVertexLabel(canvas qui.Canvas, c *Cell) {
	text := c.Label
	editing := c.ID == p.cfg.EditingID
	if editing {
		text = p.cfg.EditingLabel
	}
	if text == "" && !editing {
		return
	}
	style := p.vertexStyle(c)
	font := p.font(style)
	color, _ := ParseColor(style.FontColor)
	if color.A == 0 {
		color = qui.Color{R: .1, G: .1, B: .1, A: 1}
	}
	p.drawLabelBlock(canvas, text, c.Bounds(), style, font, color, editing, false)
}

func (p *scenePainter) drawEdge(canvas qui.Canvas, c *Cell) {
	depth := canvas.Save()
	canvas.Translate(.5, .5)
	defer canvas.RestoreTo(depth)
	style := p.edgeStyle(c)
	stroke, _ := parsePaint(style.Stroke, "#000000", style.Opacity)
	if p.cfg.Selected[c.ID] {
		under, _ := ParseColor(p.cfg.Theme.Selection)
		under.A = .22
		r := p.page().RouteWithRegistry(c, p.cfg.Shapes)
		canvas.DrawPolyline(toQuiPoints(r.Polyline), under, style.StrokeW+6)
		stroke, _ = ParseColor(p.cfg.Theme.Selection)
	}
	r := p.page().RouteWithRegistry(c, p.cfg.Shapes)
	if style.Dashed {
		p.drawDashed(canvas, r.Polyline, stroke, style.StrokeW)
	} else {
		canvas.DrawShape(qui.ShapePath{Path: r.Path}, qui.Paint{
			Color: stroke, Style: qui.PaintStroke, StrokeWidth: style.StrokeW,
			Cap: qui.CapRound, Join: qui.JoinRound, AntiAlias: true,
		})
	}
}

func (p *scenePainter) drawEdgeArrows(canvas qui.Canvas, c *Cell) {
	depth := canvas.Save()
	canvas.Translate(.5, .5)
	defer canvas.RestoreTo(depth)
	style := p.edgeStyle(c)
	stroke, _ := parsePaint(style.Stroke, "#000000", style.Opacity)
	if p.cfg.Selected[c.ID] {
		stroke, _ = ParseColor(p.cfg.Theme.Selection)
	}
	r := p.page().RouteWithRegistry(c, p.cfg.Shapes)
	drawArrow(canvas, r.End, r.EndTangent, style.EndArrow, stroke, style.StrokeW, style.ArrowSize)
	drawArrow(canvas, r.Start, Point{X: -r.StartTangent.X, Y: -r.StartTangent.Y}, style.StartArrow, stroke, style.StrokeW, style.ArrowSize)
}

func drawArrow(canvas qui.Canvas, tip, tangent Point, kind ArrowKind, color qui.Color, strokeW, size float32) {
	if kind == "" || kind == ArrowNone || color.A <= 0 {
		return
	}
	if size <= 0 {
		size = 10
	}
	t := normalize32(tangent)
	if t == (Point{}) {
		return
	}
	// The body points back from the tip along -t.
	px, py := -t.Y, t.X
	back := Point{X: tip.X - t.X*size, Y: tip.Y - t.Y*size}
	half := size * .45
	switch kind {
	case ArrowTriangle:
		p := qui.NewPath().MoveTo(tip.X, tip.Y).
			LineTo(back.X+px*half, back.Y+py*half).
			LineTo(back.X-px*half, back.Y-py*half).Close()
		canvas.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: color, AntiAlias: true})
	case ArrowVee:
		canvas.DrawLine(qui.Point{X: tip.X, Y: tip.Y}, qui.Point{X: back.X + px*half, Y: back.Y + py*half}, color, strokeW+1)
		canvas.DrawLine(qui.Point{X: tip.X, Y: tip.Y}, qui.Point{X: back.X - px*half, Y: back.Y - py*half}, color, strokeW+1)
	case ArrowDiamond:
		mid := Point{X: tip.X - t.X*size*.5, Y: tip.Y - t.Y*size*.5}
		p := qui.NewPath().MoveTo(tip.X, tip.Y).
			LineTo(mid.X+px*half, mid.Y+py*half).
			LineTo(back.X, back.Y).
			LineTo(mid.X-px*half, mid.Y-py*half).Close()
		canvas.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: color, AntiAlias: true})
	case ArrowCircle:
		r := size * .38
		c := Point{X: tip.X - t.X*r, Y: tip.Y - t.Y*r}
		p := qui.NewPath().AddCircle(c.X, c.Y, r)
		canvas.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: color, AntiAlias: true})
	case ArrowRect:
		long := size
		c0 := Point{X: tip.X - t.X*long + px*half, Y: tip.Y - t.Y*long + py*half}
		c1 := Point{X: tip.X + px*half, Y: tip.Y + py*half}
		c2 := Point{X: tip.X - px*half, Y: tip.Y - py*half}
		c3 := Point{X: tip.X - t.X*long - px*half, Y: tip.Y - t.Y*long - py*half}
		p := qui.NewPath().MoveTo(c0.X, c0.Y).LineTo(c1.X, c1.Y).LineTo(c2.X, c2.Y).LineTo(c3.X, c3.Y).Close()
		canvas.DrawShape(qui.ShapePath{Path: p}, qui.Paint{Color: color, AntiAlias: true})
	}
}

func (p *scenePainter) drawDashed(canvas qui.Canvas, pts []Point, color qui.Color, width float32) {
	const dash, gap float32 = 8, 5
	draw := true
	left := dash
	for i := 0; i < len(pts)-1; i++ {
		a, b := pts[i], pts[i+1]
		length := dist32(a, b)
		if length == 0 {
			continue
		}
		dir := normalize32(Point{X: b.X - a.X, Y: b.Y - a.Y})
		pos := float32(0)
		for pos < length {
			step := left
			if step > length-pos {
				step = length - pos
			}
			if draw {
				s := Point{X: a.X + dir.X*pos, Y: a.Y + dir.Y*pos}
				e := Point{X: s.X + dir.X*step, Y: s.Y + dir.Y*step}
				canvas.DrawLine(qui.Point{X: s.X, Y: s.Y}, qui.Point{X: e.X, Y: e.Y}, color, width)
			}
			pos += step
			left -= step
			if left <= 1e-4 {
				draw = !draw
				if draw {
					left = dash
				} else {
					left = gap
				}
			}
		}
	}
}

func (p *scenePainter) drawEdgeLabel(canvas qui.Canvas, c *Cell) {
	style := p.edgeStyle(c)
	font := p.font(style)
	color, _ := ParseColor(style.FontColor)
	if color.A == 0 {
		color = qui.Color{R: .2, G: .2, B: .2, A: 1}
	}
	r := p.page().RouteWithRegistry(c, p.cfg.Shapes)
	center := r.LabelPoint
	if style.LabelOffsetX != 0 || style.LabelOffsetY != 0 {
		center.X += style.LabelOffsetX
		center.Y += style.LabelOffsetY
	}
	text := c.Label
	editing := c.ID == p.cfg.EditingID
	if editing {
		text = p.cfg.EditingLabel
	}
	tw, th := measureBlock(text, font, style.Wrap, 180)
	if tw < 12 {
		tw = 12
	}
	pad := font.Size*.28 + 3
	box := Rect{X: center.X - tw/2 - pad, Y: center.Y - th/2 - pad*.4, W: tw + pad*2, H: th + pad*.8}
	if bg, ok := ParseColor(style.LabelFill); ok && bg.A > 0 {
		canvas.DrawShape(qui.ShapeRRect{Rect: qui.Rect{X: box.X, Y: box.Y, W: box.W, H: box.H}, Radius: 3},
			qui.Paint{Color: bg, AntiAlias: true})
	}
	labelStyle := style
	labelStyle.Align = AlignCenter
	labelStyle.VAlign = VAlignMiddle
	p.drawLabelBlock(canvas, text, box, labelStyle, font, color, editing, true)
}

func (p *scenePainter) drawLabelBlock(canvas qui.Canvas, text string, r Rect, style Style, font qui.Font, color qui.Color, editing, edge bool) {
	lines := wrapLines(text, font, r.W, style.Wrap)
	if len(lines) == 0 {
		lines = []string{""}
	}
	lineH := font.Size * 1.22
	totalH := float32(len(lines)) * lineH
	y := r.Y
	switch style.VAlign {
	case VAlignTop:
	case VAlignBottom:
		y = r.Y + r.H - totalH
	default:
		y = r.Y + (r.H-totalH)/2
	}
	for i, line := range lines {
		tw, _ := qui.TextMetrics(line, font)
		x := r.X
		switch style.Align {
		case AlignLeft:
		case AlignRight:
			x = r.X + r.W - tw
		default:
			x = r.X + (r.W-tw)/2
		}
		canvas.DrawText(line, qui.Rect{X: x, Y: y + float32(i)*lineH, W: tw + 1, H: font.Size + 2}, color, font)
	}
	if editing {
		last := ""
		if len(lines) > 0 {
			last = lines[len(lines)-1]
		}
		tw, _ := qui.TextMetrics(last, font)
		x := r.X
		switch style.Align {
		case AlignLeft:
		case AlignRight:
			x = r.X + r.W - tw
		default:
			x = r.X + (r.W-tw)/2
		}
		cy := y + float32(len(lines)-1)*lineH
		canvas.FillRect(qui.Rect{X: x + tw + 1, Y: cy + 1, W: 1.2 / p.cfg.Viewport.Scale, H: font.Size + 1}, color)
	}
	_ = edge
}

func (p *scenePainter) font(style Style) qui.Font {
	weight := qui.FontWeightNormal
	if style.Bold {
		weight = qui.FontWeightBold
	}
	return qui.Font{Family: style.FontFamily, Size: style.FontSize, Weight: weight, Italic: style.Italic}
}

func (p *scenePainter) drawPorts(canvas qui.Canvas, c *Cell) {
	r := c.Bounds()
	portFill, _ := ParseColor(p.cfg.Theme.PortFill)
	ring, _ := ParseColor(p.cfg.Theme.PortRing)
	s := 6 / p.cfg.Viewport.Scale
	for _, port := range []Port{PortNorth, PortEast, PortSouth, PortWest} {
		pt := portPoint(r, port)
		canvas.DrawShape(qui.ShapePath{Path: qui.NewPath().AddCircle(pt.X, pt.Y, s*1.35)},
			qui.Paint{Color: ring, AntiAlias: true})
		canvas.DrawShape(qui.ShapePath{Path: qui.NewPath().AddCircle(pt.X, pt.Y, s)},
			qui.Paint{Color: portFill, AntiAlias: true})
	}
}

func (p *scenePainter) drawSelectionControls(canvas qui.Canvas) {
	if p.cfg.ReadOnly || len(p.cfg.Selected) == 0 || !p.cfg.ShowControls {
		return
	}
	sel, _ := ParseColor(p.cfg.Theme.Selection)
	fill, _ := ParseColor(p.cfg.Theme.SelectionFill)
	page := p.page()
	for _, c := range page.Cells {
		if !p.cfg.Selected[c.ID] || !p.cellVisible(c) {
			continue
		}
		if c.Type == CellEdge {
			continue
		}
		r := c.Bounds()
		canvas.DrawShape(qui.ShapeRect(qui.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}), qui.Paint{
			Color: fill, Style: qui.PaintStroke, StrokeWidth: 1.5 / p.cfg.Viewport.Scale, AntiAlias: true,
		})
		if len(p.cfg.Selected) == 1 {
			p.drawResizeHandles(canvas, r, sel)
		}
	}
}

func (p *scenePainter) drawResizeHandles(canvas qui.Canvas, r Rect, color qui.Color) {
	s := 4 / p.cfg.Viewport.Scale
	for _, pt := range resizePoints(r) {
		canvas.DrawShape(qui.ShapeRect(qui.Rect{X: pt.X - s, Y: pt.Y - s, W: 2 * s, H: 2 * s}),
			qui.Paint{Color: color, AntiAlias: true})
		canvas.DrawShape(qui.ShapeRect(qui.Rect{X: pt.X - s, Y: pt.Y - s, W: 2 * s, H: 2 * s}),
			qui.Paint{Color: qui.ColorWhite, Style: qui.PaintStroke, StrokeWidth: 1 / p.cfg.Viewport.Scale, AntiAlias: true})
	}
}

func resizePoints(r Rect) []Point {
	return []Point{
		{X: r.X, Y: r.Y}, {X: r.X + r.W/2, Y: r.Y}, {X: r.X + r.W, Y: r.Y},
		{X: r.X + r.W, Y: r.Y + r.H/2}, {X: r.X + r.W, Y: r.Y + r.H},
		{X: r.X + r.W/2, Y: r.Y + r.H}, {X: r.X, Y: r.Y + r.H},
		{X: r.X, Y: r.Y + r.H/2},
	}
}

func measureBlock(text string, font qui.Font, wrap bool, width float32) (float32, float32) {
	lines := wrapLines(text, font, width, wrap)
	var w float32
	for _, line := range lines {
		lw, _ := qui.TextMetrics(line, font)
		if lw > w {
			w = lw
		}
	}
	return w, float32(max32(1, float32(len(lines)))) * font.Size * 1.22
}

func wrapLines(text string, font qui.Font, width float32, wrap bool) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	raw := strings.Split(text, "\n")
	if !wrap || width <= 0 {
		return raw
	}
	var out []string
	for _, line := range raw {
		if line == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(line)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := words[0]
		for _, word := range words[1:] {
			cand := cur + " " + word
			w, _ := qui.TextMetrics(cand, font)
			if w <= width {
				cur = cand
				continue
			}
			out = append(out, cur)
			cur = word
		}
		out = append(out, cur)
	}
	return out
}

func toQuiPoints(pts []Point) []qui.Point {
	out := make([]qui.Point, len(pts))
	for i, p := range pts {
		out[i] = qui.Point{X: p.X, Y: p.Y}
	}
	return out
}

// drawioViewport reproduces draw.io viewer's fit transform:
// a 0.5px viewBox border, uniform "meet" scaling, and centering.
func drawioViewport(content Rect, size Rect, border float32) Viewport {
	if content.Empty() {
		return Viewport{Scale: 1}
	}
	if border < 0 {
		border = 0
	}
	view := content.Inflate(border)
	if view.Empty() || size.W <= 0 || size.H <= 0 {
		return Viewport{Scale: 1}
	}
	s := min32(size.W/view.W, size.H/view.H)
	s = clamp32(s, .02, 16)
	pan := Point{
		X: (size.W-view.W*s)/2 - view.X*s,
		Y: (size.H-view.H*s)/2 - view.Y*s,
	}
	return Viewport{Pan: pan, Scale: s}
}

func fitViewport(content Rect, size Rect, padding float32) Viewport {
	if content.Empty() {
		return Viewport{Scale: 1}
	}
	availW, availH := size.W-padding*2, size.H-padding*2
	if availW <= 1 || availH <= 1 {
		return Viewport{Scale: 1}
	}
	s := min32(availW/content.W, availH/content.H)
	s = clamp32(s, .02, 8)
	pan := Point{X: (size.W-content.W*s)/2 - content.X*s, Y: (size.H-content.H*s)/2 - content.Y*s}
	return Viewport{Pan: pan, Scale: s}
}

func clampViewport(v Viewport, content Rect, size Rect) Viewport {
	v = v.normalized()
	v.Scale = clamp32(v.Scale, .05, 8)
	if !content.Empty() {
		c := v.rectToLocal(content)
		const margin float32 = 80
		if c.X+c.W < margin {
			v.Pan.X += margin - (c.X + c.W)
		}
		if c.Y+c.H < margin {
			v.Pan.Y += margin - (c.Y + c.H)
		}
		if c.X > size.W-margin {
			v.Pan.X -= c.X - (size.W - margin)
		}
		if c.Y > size.H-margin {
			v.Pan.Y -= c.Y - (size.H - margin)
		}
	}
	return v
}

func describeRoute(r RouteKind) string {
	switch r {
	case RouteStraight:
		return "Straight"
	case RouteRoundedOrthogonal:
		return "Rounded orthogonal"
	case RouteEntityRelation:
		return "Entity relation"
	case RouteCurved:
		return "Curved"
	case RoutePolyline:
		return "Polyline"
	default:
		return "Orthogonal"
	}
}

func describeShape(s ShapeKind) string {
	switch s {
	case ShapeRoundedRect:
		return "Rounded rectangle"
	case ShapeEllipse:
		return "Ellipse"
	case ShapeDiamond:
		return "Diamond"
	case ShapeParallelogram:
		return "Parallelogram"
	case ShapeHexagon:
		return "Hexagon"
	case ShapeCylinder:
		return "Cylinder"
	case ShapeDocument:
		return "Document"
	case ShapeActor:
		return "Actor"
	case ShapeCloud:
		return "Cloud"
	case ShapeNote:
		return "Note"
	case ShapeText:
		return "Text"
	default:
		return "Rectangle"
	}
}

func formatCell(c *Cell) string {
	if c == nil {
		return ""
	}
	if c.Label != "" {
		return c.Label
	}
	return fmt.Sprintf("%s %s", c.Type, c.ID)
}

// DescribeCell returns a concise human-readable description for status
// surfaces and accessibility values.
func DescribeCell(c *Cell) string {
	if c == nil {
		return ""
	}
	if c.Label != "" {
		return fmt.Sprintf("%s: %s", c.Type, c.Label)
	}
	return fmt.Sprintf("%s %s", c.Type, c.ID)
}
