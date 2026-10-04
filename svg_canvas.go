package qui

// SVG export backend: a Canvas that RECORDS drawing instead of rasterizing
// it, emitting an SVG 1.1 document.
//
// It sits beside pdf_canvas.go for the same reason that one exists: anything
// that can already paint itself onto a Canvas (a spreadsheet range, a
// document page, a slide, a chart, a whole widget tree) becomes exportable
// without the exporter knowing anything about the thing being exported. The
// alternative — a per-app SVG serializer walking that app's own model — would
// duplicate every drawing decision the renderer already makes, and drift.
//
// Two implementation choices worth knowing:
//
//   - The transform is BAKED into each emitted element as its own
//     `transform="matrix(…)"`, rather than mirrored as nested `<g>` groups.
//     Canvas's matrix stack is not a tree (Translate mutates the current
//     frame in place), so a group-per-frame mapping would need bookkeeping
//     that a per-element matrix makes unnecessary. Output size is the only
//     cost, and gzip erases it.
//   - Clips accumulate as `<clipPath>` defs that reference their parent clip.
//     That keeps ClipRect and ClipPath composable, and matches how the
//     rasterizing backends nest masks.
//
// Unsupported-by-SVG primitives degrade honestly: DrawShadow paints nothing
// (an elevation shadow is a rasterizer effect, not a document feature) and
// SaveLayer's blend modes and image filters are ignored, so a blurred layer
// exports as its unblurred content rather than as nothing.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
)

// SVGCanvas is a Canvas that serializes to SVG. Build one with NewSVGCanvas,
// draw into it exactly as into any other Canvas, then call Bytes or
// WriteFile.
type SVGCanvas struct {
	*canvasState

	width, height float32
	body          strings.Builder
	defs          strings.Builder
	nextID        int

	// clips is a stack parallel to the state stack: clips[i] is the
	// clip-path id in force at save depth i ("" = unclipped).
	clips []string
	// title is written as the document's <title>, for accessibility.
	title string
}

// NewSVGCanvas returns a canvas that records into an SVG document of the
// given logical size. Coordinates are written unchanged, so the document's
// user units are the caller's logical pixels.
func NewSVGCanvas(width, height float32) *SVGCanvas {
	return &SVGCanvas{
		canvasState: newCanvasState(Rect{W: width, H: height}),
		width:       width,
		height:      height,
		clips:       []string{""},
	}
}

// SetTitle sets the document's <title> element.
func (c *SVGCanvas) SetTitle(title string) { c.title = title }

// Bytes returns the finished SVG document.
func (c *SVGCanvas) Bytes() []byte {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" `+
		`xmlns:xlink="http://www.w3.org/1999/xlink" `+
		`width="%s" height="%s" viewBox="0 0 %s %s">`+"\n",
		num(c.width), num(c.height), num(c.width), num(c.height))
	if c.title != "" {
		fmt.Fprintf(&sb, "<title>%s</title>\n", escapeXML(c.title))
	}
	if c.defs.Len() > 0 {
		sb.WriteString("<defs>\n")
		sb.WriteString(c.defs.String())
		sb.WriteString("</defs>\n")
	}
	sb.WriteString(c.body.String())
	sb.WriteString("</svg>\n")
	return []byte(sb.String())
}

// WriteFile writes the document to path.
func (c *SVGCanvas) WriteFile(path string) error {
	return os.WriteFile(path, c.Bytes(), 0o644)
}

// --- state stack ----------------------------------------------------------

// Save pushes a frame on both the matrix/clip stack and the clip-id stack.
func (c *SVGCanvas) Save() int {
	depth := c.canvasState.Save()
	c.clips = append(c.clips, c.currentClipID())
	return depth
}

// SaveLayer degenerates to Save: SVG has no offscreen-compositing
// equivalent, so layer effects (blur, drop shadow, blend modes) are dropped
// and their CONTENT is still written — an exported document missing a blur
// beats one missing the blurred thing.
func (c *SVGCanvas) SaveLayer(bounds Rect, _ Paint) int {
	depth := c.Save()
	c.ClipRect(bounds)
	return depth
}

// Restore pops one frame.
func (c *SVGCanvas) Restore() {
	c.canvasState.Restore()
	if len(c.clips) > 1 {
		c.clips = c.clips[:len(c.clips)-1]
	}
}

// RestoreTo unwinds to the given depth.
func (c *SVGCanvas) RestoreTo(depth int) {
	c.canvasState.RestoreTo(depth)
	if depth < 1 {
		depth = 1
	}
	// The clip stack is kept the same length as the state stack, and
	// canvasState.RestoreTo leaves exactly `depth` frames.
	if depth < len(c.clips) {
		c.clips = c.clips[:depth]
	}
}

// currentClipID returns the clip-path id in force.
func (c *SVGCanvas) currentClipID() string { return c.clips[len(c.clips)-1] }

// setClipID replaces the clip in force at the current depth.
func (c *SVGCanvas) setClipID(id string) { c.clips[len(c.clips)-1] = id }

// ClipRect narrows the clip to a rectangle in the current user space.
func (c *SVGCanvas) ClipRect(rect Rect) {
	c.canvasState.ClipRect(rect)
	c.pushClip(fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s"%s/>`,
		num(rect.X), num(rect.Y), num(rect.W), num(rect.H), c.matrixAttr()))
}

// ClipPath narrows the clip to a path's interior.
func (c *SVGCanvas) ClipPath(path *Path) {
	if path == nil || path.IsEmpty() {
		return
	}
	c.canvasState.ClipRect(path.Bounds())
	c.pushClip(fmt.Sprintf(`<path d="%s"%s/>`, pathData(path), c.matrixAttr()))
}

// pushClip registers a `<clipPath>` def whose content is `shape`, chained to
// whatever clip was already in force.
func (c *SVGCanvas) pushClip(shape string) {
	id := c.mintID("clip")
	parent := ""
	if prev := c.currentClipID(); prev != "" {
		parent = fmt.Sprintf(` clip-path="url(#%s)"`, prev)
	}
	fmt.Fprintf(&c.defs, `<clipPath id="%s"%s>%s</clipPath>`+"\n", id, parent, shape)
	c.setClipID(id)
}

// mintID returns a document-unique id with the given prefix.
func (c *SVGCanvas) mintID(prefix string) string {
	c.nextID++
	return fmt.Sprintf("%s%d", prefix, c.nextID)
}

// --- attribute helpers ----------------------------------------------------

// matrixAttr renders the current matrix as a transform attribute ("" when it
// is the identity).
func (c *SVGCanvas) matrixAttr() string {
	m := c.CurrentMatrix()
	if m == (Matrix{A: 1, D: 1}) {
		return ""
	}
	return fmt.Sprintf(` transform="matrix(%s %s %s %s %s %s)"`,
		num(m.A), num(m.B), num(m.C), num(m.D), num(m.TX), num(m.TY))
}

// clipAttr renders the clip-path attribute in force ("" when unclipped).
func (c *SVGCanvas) clipAttr() string {
	if id := c.currentClipID(); id != "" {
		return fmt.Sprintf(` clip-path="url(#%s)"`, id)
	}
	return ""
}

// commonAttrs is the transform + clip pair every emitted element carries.
func (c *SVGCanvas) commonAttrs() string { return c.matrixAttr() + c.clipAttr() }

// fillAttrs renders the paint as presentation attributes. A Shader becomes a
// gradient def and a `fill="url(#…)"` reference.
func (c *SVGCanvas) fillAttrs(paint Paint) string {
	if paint.Style == PaintStroke {
		return c.strokeAttrs(paint)
	}
	fill := "none"
	opacity := float32(1)
	switch {
	case paint.Shader != nil:
		if id := c.gradientDef(paint.Shader); id != "" {
			fill = "url(#" + id + ")"
		} else {
			fill, opacity = colorHex(paint.Color), paint.Color.A
		}
	default:
		fill, opacity = colorHex(paint.Color), paint.Color.A
	}
	out := ` fill="` + fill + `"`
	if opacity < 1 {
		out += fmt.Sprintf(` fill-opacity="%s"`, num(opacity))
	}
	if paint.FillRule == FillEvenOdd {
		out += ` fill-rule="evenodd"`
	}
	return out
}

// strokeAttrs renders a stroke paint.
func (c *SVGCanvas) strokeAttrs(paint Paint) string {
	w := paint.StrokeWidth
	if w <= 0 {
		w = 1
	}
	out := fmt.Sprintf(` fill="none" stroke="%s" stroke-width="%s"`, colorHex(paint.Color), num(w))
	if paint.Color.A < 1 {
		out += fmt.Sprintf(` stroke-opacity="%s"`, num(paint.Color.A))
	}
	switch paint.Cap {
	case CapRound:
		out += ` stroke-linecap="round"`
	case CapSquare:
		out += ` stroke-linecap="square"`
	}
	switch paint.Join {
	case JoinRound:
		out += ` stroke-linejoin="round"`
	case JoinBevel:
		out += ` stroke-linejoin="bevel"`
	}
	return out
}

// gradientDef emits a gradient def for a shader and returns its id ("" for
// shader kinds SVG cannot express).
func (c *SVGCanvas) gradientDef(shader Shader) string {
	stops := func(list []GradientStop) string {
		var sb strings.Builder
		for _, s := range list {
			fmt.Fprintf(&sb, `<stop offset="%s" stop-color="%s"`, num(s.Offset), colorHex(s.Color))
			if s.Color.A < 1 {
				fmt.Fprintf(&sb, ` stop-opacity="%s"`, num(s.Color.A))
			}
			sb.WriteString("/>")
		}
		return sb.String()
	}
	switch g := shader.(type) {
	case LinearGradient:
		id := c.mintID("grad")
		fmt.Fprintf(&c.defs, `<linearGradient id="%s" gradientUnits="userSpaceOnUse" `+
			`x1="%s" y1="%s" x2="%s" y2="%s">%s</linearGradient>`+"\n",
			id, num(g.Start.X), num(g.Start.Y), num(g.End.X), num(g.End.Y), stops(g.Stops))
		return id
	case RadialGradient:
		id := c.mintID("grad")
		fmt.Fprintf(&c.defs, `<radialGradient id="%s" gradientUnits="userSpaceOnUse" `+
			`cx="%s" cy="%s" r="%s">%s</radialGradient>`+"\n",
			id, num(g.Center.X), num(g.Center.Y), num(g.Radius), stops(g.Stops))
		return id
	}
	return ""
}

// --- draw primitives ------------------------------------------------------

// Clear paints the whole document area.
func (c *SVGCanvas) Clear(color Color) {
	fmt.Fprintf(&c.body, `<rect x="0" y="0" width="%s" height="%s" fill="%s"/>`+"\n",
		num(c.width), num(c.height), colorHex(color))
}

// FillRect fills an axis-aligned rectangle.
func (c *SVGCanvas) FillRect(rect Rect, color Color) {
	c.DrawShape(ShapeRect(rect), Paint{Color: color})
}

// FillRoundedRect fills a rounded rectangle.
func (c *SVGCanvas) FillRoundedRect(rect Rect, radius float32, color Color) {
	c.DrawShape(ShapeRRect{Rect: rect, Radius: radius}, Paint{Color: color})
}

// StrokeRect strokes a rectangle.
func (c *SVGCanvas) StrokeRect(rect Rect, color Color, width float32) {
	c.DrawShape(ShapeRect(rect), Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// StrokeRoundedRect strokes a rounded rectangle.
func (c *SVGCanvas) StrokeRoundedRect(rect Rect, radius float32, color Color, width float32) {
	c.DrawShape(ShapeRRect{Rect: rect, Radius: radius},
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width})
}

// DrawLine draws a segment.
func (c *SVGCanvas) DrawLine(p1, p2 Point, color Color, width float32) {
	c.DrawShape(ShapeLine{P1: p1, P2: p2},
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width, Cap: CapRound})
}

// DrawPolyline draws a connected run of segments.
func (c *SVGCanvas) DrawPolyline(points []Point, color Color, width float32) {
	c.DrawShape(ShapePolyline(points),
		Paint{Color: color, Style: PaintStroke, StrokeWidth: width, Cap: CapRound})
}

// DrawShape is the single geometry entry point, exactly as on the raster
// backends: everything else here funnels through it.
func (c *SVGCanvas) DrawShape(shape Shape, paint Paint) {
	attrs := c.fillAttrs(paint) + c.commonAttrs()
	switch s := shape.(type) {
	case ShapeRect:
		if s.W <= 0 || s.H <= 0 {
			return
		}
		fmt.Fprintf(&c.body, `<rect x="%s" y="%s" width="%s" height="%s"%s/>`+"\n",
			num(s.X), num(s.Y), num(s.W), num(s.H), attrs)
	case ShapeRRect:
		if s.Rect.W <= 0 || s.Rect.H <= 0 {
			return
		}
		fmt.Fprintf(&c.body, `<rect x="%s" y="%s" width="%s" height="%s" rx="%s"%s/>`+"\n",
			num(s.Rect.X), num(s.Rect.Y), num(s.Rect.W), num(s.Rect.H), num(s.Radius), attrs)
	case ShapeLine:
		fmt.Fprintf(&c.body, `<line x1="%s" y1="%s" x2="%s" y2="%s"%s/>`+"\n",
			num(s.P1.X), num(s.P1.Y), num(s.P2.X), num(s.P2.Y), attrs)
	case ShapePolyline:
		if len(s) < 2 {
			return
		}
		var pts strings.Builder
		for i, p := range s {
			if i > 0 {
				pts.WriteByte(' ')
			}
			fmt.Fprintf(&pts, "%s,%s", num(p.X), num(p.Y))
		}
		fmt.Fprintf(&c.body, `<polyline points="%s"%s/>`+"\n", pts.String(), attrs)
	case ShapePath:
		if s.Path == nil || s.Path.IsEmpty() {
			return
		}
		fmt.Fprintf(&c.body, `<path d="%s"%s/>`+"\n", pathData(s.Path), attrs)
	}
}

// DrawText emits a `<text>` element. Placement mirrors the raster backend's
// single-line convention (optically centered on cap height inside rect) so an
// exported document lines up with what was on screen.
func (c *SVGCanvas) DrawText(text string, rect Rect, color Color, font Font) {
	if text == "" {
		return
	}
	face := GetFontFaceFor(font)
	metrics := face.Metrics()
	capHeight := float32(metrics.CapHeight.Ceil())
	if capHeight <= 0 {
		capHeight = float32(metrics.Ascent.Ceil() - metrics.Descent.Ceil())
	}
	lineHeight := float32(metrics.Height.Ceil())
	ascent := float32(metrics.Ascent.Ceil())

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	baseline := rect.Y + (rect.H+capHeight)/2
	if len(lines) > 1 {
		baseline = rect.Y + ascent
	}
	attrs := c.commonAttrs()
	family := font.Family
	if family == "" {
		family = "sans-serif"
	}
	style := fmt.Sprintf(` font-family="%s" font-size="%s" fill="%s"`,
		escapeXML(family), num(font.Size), colorHex(color))
	if w := font.effectiveWeight(); w != FontWeightNormal {
		style += fmt.Sprintf(` font-weight="%d"`, int(w))
	}
	if font.Italic {
		style += ` font-style="italic"`
	}
	if color.A < 1 {
		style += fmt.Sprintf(` fill-opacity="%s"`, num(color.A))
	}
	if font.LetterSpacing != 0 {
		style += fmt.Sprintf(` letter-spacing="%s"`, num(font.LetterSpacing))
	}
	for i, line := range lines {
		if line == "" {
			continue
		}
		y := baseline + float32(i)*lineHeight
		fmt.Fprintf(&c.body, `<text x="%s" y="%s"%s%s xml:space="preserve">%s</text>`+"\n",
			num(rect.X), num(y), style, attrs, escapeXML(line))
	}
}

// DrawImage embeds a raster image as a base64 PNG data URI. Self-contained
// output is the point of an exported document — a file:// reference would
// break the moment the SVG moved.
func (c *SVGCanvas) DrawImage(img image.Image, rect Rect) {
	if img == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return
	}
	fmt.Fprintf(&c.body, `<image x="%s" y="%s" width="%s" height="%s" `+
		`preserveAspectRatio="none" xlink:href="data:image/png;base64,%s"%s/>`+"\n",
		num(rect.X), num(rect.Y), num(rect.W), num(rect.H),
		base64.StdEncoding.EncodeToString(buf.Bytes()), c.commonAttrs())
}

// DrawVector rasterizes a vector source and embeds it. Re-serializing the
// source's own geometry would be better, but VectorSource only promises a
// rasterization — and an exported document that shows the icon beats one
// that omits it.
func (c *SVGCanvas) DrawVector(src VectorSource, rect Rect, tint Color) {
	if src == nil || rect.W <= 0 || rect.H <= 0 {
		return
	}
	const supersample = 3
	img := src.Rasterize(int(rect.W*supersample), int(rect.H*supersample), tint)
	if img == nil {
		return
	}
	c.DrawImage(img, rect)
}

// DrawShadow paints nothing: an elevation shadow is a rasterizer effect
// rather than document content, and a fake one would print.
func (c *SVGCanvas) DrawShadow(Rect, float32, ElevationSpec, Color) {}

// --- serialization helpers ------------------------------------------------

// pathData renders a Path as an SVG `d` attribute.
func pathData(p *Path) string {
	var sb strings.Builder
	for _, cmd := range p.cmds {
		switch cmd.op {
		case pathMove:
			fmt.Fprintf(&sb, "M%s %s", num(cmd.a.X), num(cmd.a.Y))
		case pathLine:
			fmt.Fprintf(&sb, "L%s %s", num(cmd.a.X), num(cmd.a.Y))
		case pathQuad:
			fmt.Fprintf(&sb, "Q%s %s %s %s", num(cmd.a.X), num(cmd.a.Y), num(cmd.b.X), num(cmd.b.Y))
		case pathCubic:
			fmt.Fprintf(&sb, "C%s %s %s %s %s %s", num(cmd.a.X), num(cmd.a.Y),
				num(cmd.b.X), num(cmd.b.Y), num(cmd.c.X), num(cmd.c.Y))
		case pathClose:
			sb.WriteString("Z")
		}
		sb.WriteByte(' ')
	}
	return strings.TrimSpace(sb.String())
}

// num formats a coordinate compactly, dropping a trailing ".0".
func num(v float32) string {
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return "0"
	}
	s := strconv.FormatFloat(float64(v), 'f', 3, 32)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// colorHex renders a color as #rrggbb (alpha travels in a separate opacity
// attribute, which is how SVG 1.1 spells it).
func colorHex(c Color) string {
	clamp := func(v float32) int {
		n := int(v*255 + 0.5)
		if n < 0 {
			return 0
		}
		if n > 255 {
			return 255
		}
		return n
	}
	return fmt.Sprintf("#%02x%02x%02x", clamp(c.R), clamp(c.G), clamp(c.B))
}

// escapeXML escapes text content and attribute values.
func escapeXML(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			sb.WriteString("&amp;")
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			if r < 0x20 && r != '\t' {
				continue
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
