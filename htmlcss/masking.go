package htmlcss

import (
	"encoding/base64"
	"image"
	"math"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/svg"
)

// CSS Masking 1: clip-path (basic shapes and path()) and mask-image.
// Both compile into widgets.Box callbacks evaluated against the box's
// current border box, so they follow layout.

// lenPct is a <length-percentage>: px + pct × the reference length.
type lenPct struct{ px, pct float32 }

func (l lenPct) resolve(ref float32) float32 { return l.px + l.pct*ref }

// parseLenPct parses a length or percentage token.
func parseLenPct(tok string, fs float32) (lenPct, bool) {
	tok = strings.TrimSpace(tok)
	if tok == "0" {
		return lenPct{}, true
	}
	px, pct, ok := lengthOrPct(tok, fs)
	return lenPct{px: px, pct: pct}, ok
}

// --- reference boxes ---

// boxEdges returns the border and padding insets the reference boxes
// (padding-box, content-box) need, plus the margin for margin-box.
func boxEdges(cs *ComputedStyle) (border, padding, margin qui.Insets) {
	if cs.HasSideBorders {
		border = cs.SideWidths
	} else if cs.BorderWidth > 0 {
		w := cs.BorderWidth
		border = qui.Insets{Top: w, Right: w, Bottom: w, Left: w}
	}
	return border, cs.Padding, cs.Margin
}

// referenceBox maps a <geometry-box> keyword onto the border box.
func referenceBox(bounds qui.Rect, kind string, border, padding, margin qui.Insets) qui.Rect {
	inset := func(r qui.Rect, in qui.Insets) qui.Rect {
		return qui.Rect{X: r.X + in.Left, Y: r.Y + in.Top, W: max(0, r.W-in.Left-in.Right), H: max(0, r.H-in.Top-in.Bottom)}
	}
	switch kind {
	case "padding-box":
		return inset(bounds, border)
	case "content-box":
		return inset(inset(bounds, border), padding)
	case "margin-box":
		return qui.Rect{X: bounds.X - margin.Left, Y: bounds.Y - margin.Top,
			W: bounds.W + margin.Left + margin.Right, H: bounds.H + margin.Top + margin.Bottom}
	}
	return bounds // border-box (and fill-box / stroke-box / view-box for HTML)
}

func isGeometryBox(tok string) bool {
	switch tok {
	case "border-box", "padding-box", "content-box", "margin-box", "fill-box", "stroke-box", "view-box":
		return true
	}
	return false
}

// --- positions ---

// parsePosition parses a <position> (1–4 tokens of keywords and
// length-percentages) into x / y offsets, where a percentage is of
// (reference − object) as background-position / mask-position use it.
// Default is center.
func parsePosition(toks []string, fs float32) (x, y lenPct, ok bool) {
	x, y = lenPct{pct: 0.5}, lenPct{pct: 0.5}
	if len(toks) == 0 {
		return x, y, true
	}
	kw := func(t string) (v lenPct, axis byte, isKw bool) {
		switch t {
		case "left":
			return lenPct{}, 'x', true
		case "right":
			return lenPct{pct: 1}, 'x', true
		case "top":
			return lenPct{}, 'y', true
		case "bottom":
			return lenPct{pct: 1}, 'y', true
		case "center":
			return lenPct{pct: 0.5}, 'c', true
		}
		return lenPct{}, 0, false
	}
	switch len(toks) {
	case 1:
		if v, axis, isKw := kw(toks[0]); isKw {
			if axis == 'y' {
				y = v
			} else {
				x = v
			}
			return x, y, true
		}
		v, ok := parseLenPct(toks[0], fs)
		return v, y, ok
	case 2:
		v0, a0, k0 := kw(toks[0])
		v1, a1, k1 := kw(toks[1])
		if !k0 {
			if v0, ok = parseLenPct(toks[0], fs); !ok {
				return x, y, false
			}
			a0 = 'x'
		}
		if !k1 {
			if v1, ok = parseLenPct(toks[1], fs); !ok {
				return x, y, false
			}
			a1 = 'y'
		}
		if a0 == 'y' || a1 == 'x' {
			v0, v1 = v1, v0
		}
		return v0, v1, true
	}
	// 3–4 tokens: keyword offset pairs (right 10px bottom 20%).
	for i := 0; i < len(toks); i++ {
		v, axis, isKw := kw(toks[i])
		if !isKw {
			return x, y, false
		}
		if i+1 < len(toks) {
			if off, ok := parseLenPct(toks[i+1], fs); ok {
				i++
				if v.pct == 1 { // from the far edge
					v = lenPct{px: -off.px, pct: 1 - off.pct}
				} else {
					v = off
				}
			}
		}
		if axis == 'y' {
			y = v
		} else {
			x = v
		}
	}
	return x, y, true
}

// --- clip-path ---

// clipPathSpec is a parsed `clip-path`.
type clipPathSpec struct {
	shape string // inset, circle, ellipse, polygon, path; "" = the box itself
	box   string // reference box keyword ("" = border-box)

	inset  [4]lenPct // top right bottom left
	radius qui.CornerRadii
	// circle / ellipse: radii (rxKw/ryKw = closest-side / farthest-side).
	rx, ry     lenPct
	rxKw, ryKw string
	cx, cy     lenPct
	pts        [][2]lenPct
	evenOdd    bool
	path       *svg.Path
}

// parseClipRect parses the legacy clip property, rect(top, right, bottom,
// left) — offsets from the border box's top-left corner, `auto` meaning
// that edge — as the equivalent inset() clip-path. nil for auto or junk.
func parseClipRect(v string, fs float32) *clipPathSpec {
	low := strings.ToLower(strings.TrimSpace(v))
	if !strings.HasPrefix(low, "rect(") || !strings.HasSuffix(low, ")") {
		return nil
	}
	args := strings.FieldsFunc(low[5:len(low)-1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(args) != 4 {
		return nil
	}
	c := &clipPathSpec{shape: "inset"}
	for i, a := range args {
		if a == "auto" {
			continue // that edge of the box: a zero inset
		}
		l, ok := parseLenPct(a, fs)
		if !ok || l.pct != 0 {
			return nil
		}
		switch i {
		case 0, 3: // top, left: offsets from the near edge are insets
			c.inset[i] = l
		default: // right, bottom: measured from the top-left too
			c.inset[i] = lenPct{px: -l.px, pct: 1}
		}
	}
	return c
}

// parseClipPath parses a clip-path value. ok=false for none, url()
// references (no SVG <clipPath> support) and unparsable values.
func parseClipPath(v string, fs float32) (*clipPathSpec, bool) {
	v = strings.TrimSpace(v)
	low := strings.ToLower(v)
	if v == "" || low == "none" || strings.HasPrefix(low, "url(") {
		return nil, false
	}
	c := &clipPathSpec{}
	for _, tok := range fieldsTopLevel(v) {
		lt := strings.ToLower(tok)
		if isGeometryBox(lt) {
			c.box = lt
			continue
		}
		open := strings.IndexByte(tok, '(')
		if open < 0 || !strings.HasSuffix(tok, ")") || c.shape != "" {
			return nil, false
		}
		name, inner := strings.ToLower(tok[:open]), tok[open+1:len(tok)-1]
		if !c.parseShape(name, inner, fs) {
			return nil, false
		}
		c.shape = name
	}
	if c.shape == "" && c.box == "" {
		return nil, false
	}
	return c, true
}

func (c *clipPathSpec) parseShape(name, inner string, fs float32) bool {
	switch name {
	case "inset":
		f := fieldsTopLevel(inner)
		var lens []lenPct
		i := 0
		for ; i < len(f) && strings.ToLower(f[i]) != "round"; i++ {
			l, ok := parseLenPct(f[i], fs)
			if !ok {
				return false
			}
			lens = append(lens, l)
		}
		if len(lens) == 0 || len(lens) > 4 {
			return false
		}
		switch len(lens) {
		case 1:
			lens = []lenPct{lens[0], lens[0], lens[0], lens[0]}
		case 2:
			lens = []lenPct{lens[0], lens[1], lens[0], lens[1]}
		case 3:
			lens = []lenPct{lens[0], lens[1], lens[2], lens[1]}
		}
		copy(c.inset[:], lens)
		if i < len(f) { // round <border-radius>
			cs := &ComputedStyle{}
			parseBorderRadius(map[string]string{"border-radius": strings.Join(f[i+1:], " ")}, cs, fs)
			c.radius = cs.Corners.Resolved(cs.Radius)
		}
		return true
	case "circle", "ellipse":
		f := fieldsTopLevel(inner)
		at := len(f)
		for i, t := range f {
			if strings.EqualFold(t, "at") {
				at = i
				break
			}
		}
		radii := f[:at]
		if (name == "circle" && len(radii) > 1) || len(radii) > 2 {
			return false
		}
		parseR := func(t string) (lenPct, string, bool) {
			switch lt := strings.ToLower(t); lt {
			case "closest-side", "farthest-side":
				return lenPct{}, lt, true
			}
			l, ok := parseLenPct(t, fs)
			return l, "", ok
		}
		c.rxKw, c.ryKw = "closest-side", "closest-side"
		var ok bool
		if len(radii) >= 1 {
			if c.rx, c.rxKw, ok = parseR(radii[0]); !ok {
				return false
			}
			c.ry, c.ryKw = c.rx, c.rxKw
		}
		if len(radii) == 2 {
			if c.ry, c.ryKw, ok = parseR(radii[1]); !ok {
				return false
			}
		}
		pos := []string{}
		if at < len(f) {
			for _, t := range f[at+1:] {
				pos = append(pos, strings.ToLower(t))
			}
		}
		c.cx, c.cy, ok = parsePosition(pos, fs)
		return ok
	case "polygon":
		parts := splitTopLevel(inner, ',')
		for i, p := range parts {
			f := fieldsTopLevel(strings.TrimSpace(p))
			if i == 0 && len(f) == 1 {
				switch strings.ToLower(f[0]) {
				case "evenodd":
					c.evenOdd = true
					continue
				case "nonzero":
					continue
				}
			}
			if len(f) != 2 {
				return false
			}
			x, ok1 := parseLenPct(f[0], fs)
			y, ok2 := parseLenPct(f[1], fs)
			if !ok1 || !ok2 {
				return false
			}
			c.pts = append(c.pts, [2]lenPct{x, y})
		}
		return len(c.pts) >= 3
	case "path":
		parts := splitTopLevel(inner, ',')
		d := strings.TrimSpace(parts[len(parts)-1])
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "evenodd") {
			c.evenOdd = true
		}
		if len(d) < 2 || (d[0] != '"' && d[0] != '\'') {
			return false
		}
		p, err := svg.ParsePath(d[1 : len(d)-1])
		if err != nil || len(p.Commands) == 0 {
			return false
		}
		c.path = p
		return true
	}
	return false
}

// build returns the clip path for a border box, or nil for an empty
// region (the element is clipped away entirely).
func (c *clipPathSpec) build(bounds qui.Rect, border, padding, margin qui.Insets) *qui.Path {
	ref := referenceBox(bounds, c.box, border, padding, margin)
	p := qui.NewPath()
	switch c.shape {
	case "":
		p.AddRect(ref) // a bare <geometry-box> (border-radius not followed)
	case "inset":
		t, r := c.inset[0].resolve(ref.H), c.inset[1].resolve(ref.W)
		b, l := c.inset[2].resolve(ref.H), c.inset[3].resolve(ref.W)
		rect := qui.Rect{X: ref.X + l, Y: ref.Y + t, W: ref.W - l - r, H: ref.H - t - b}
		if rect.W <= 0 || rect.H <= 0 {
			return nil
		}
		if c.radius.IsZero() {
			p.AddRect(rect)
		} else {
			rr := c.radius
			p.AddRRectCorners(rect, rr.TL, rr.TR, rr.BR, rr.BL)
		}
	case "circle", "ellipse":
		cx := ref.X + c.cx.resolve(ref.W)
		cy := ref.Y + c.cy.resolve(ref.H)
		side := func(kw string, horiz bool) float32 {
			var a, b float32
			if horiz {
				a, b = cx-ref.X, ref.X+ref.W-cx
			} else {
				a, b = cy-ref.Y, ref.Y+ref.H-cy
			}
			a, b = absf(a), absf(b)
			if kw == "farthest-side" {
				return max(a, b)
			}
			return min(a, b)
		}
		var rx, ry float32
		if c.shape == "circle" {
			if c.rxKw != "" {
				if c.rxKw == "farthest-side" {
					rx = max(side(c.rxKw, true), side(c.rxKw, false))
				} else {
					rx = min(side(c.rxKw, true), side(c.rxKw, false))
				}
			} else {
				diag := float32(math.Hypot(float64(ref.W), float64(ref.H)) / math.Sqrt2)
				rx = c.rx.resolve(diag)
			}
			ry = rx
		} else {
			rx, ry = c.rx.resolve(ref.W), c.ry.resolve(ref.H)
			if c.rxKw != "" {
				rx = side(c.rxKw, true)
			}
			if c.ryKw != "" {
				ry = side(c.ryKw, false)
			}
		}
		if rx <= 0 || ry <= 0 {
			return nil
		}
		p.AddOval(qui.Rect{X: cx - rx, Y: cy - ry, W: 2 * rx, H: 2 * ry})
	case "polygon":
		for i, pt := range c.pts {
			x, y := ref.X+pt[0].resolve(ref.W), ref.Y+pt[1].resolve(ref.H)
			if i == 0 {
				p.MoveTo(x, y)
			} else {
				p.LineTo(x, y)
			}
		}
		p.Close()
	case "path":
		appendSVGPath(p, c.path, ref.X, ref.Y)
	}
	return p
}

// appendSVGPath converts svg path commands (absolute) into p, offset by
// (ox, oy). Elliptical arcs become cubic Béziers.
func appendSVGPath(p *qui.Path, sp *svg.Path, ox, oy float32) {
	var curX, curY, startX, startY float32
	for _, c := range sp.Commands {
		a := c.Args
		switch c.Op {
		case svg.OpMoveTo:
			curX, curY = a[0], a[1]
			startX, startY = curX, curY
			p.MoveTo(ox+curX, oy+curY)
		case svg.OpLineTo:
			curX, curY = a[0], a[1]
			p.LineTo(ox+curX, oy+curY)
		case svg.OpCurveTo:
			p.CubicTo(ox+a[0], oy+a[1], ox+a[2], oy+a[3], ox+a[4], oy+a[5])
			curX, curY = a[4], a[5]
		case svg.OpQuadTo:
			p.QuadTo(ox+a[0], oy+a[1], ox+a[2], oy+a[3])
			curX, curY = a[2], a[3]
		case svg.OpArcTo:
			arcToCubics(p, curX, curY, a[0], a[1], a[2], a[3] != 0, a[4] != 0, a[5], a[6], ox, oy)
			curX, curY = a[5], a[6]
		case svg.OpClose:
			p.Close()
			curX, curY = startX, startY
		}
	}
}

// arcToCubics appends an SVG elliptical arc (endpoint form, SVG 1.1 F.6)
// as cubic Bézier segments of at most 90° each.
func arcToCubics(p *qui.Path, x1, y1, rx, ry, rotDeg float32, large, sweep bool, x2, y2, ox, oy float32) {
	if x1 == x2 && y1 == y2 {
		return
	}
	frx, fry := math.Abs(float64(rx)), math.Abs(float64(ry))
	if frx == 0 || fry == 0 {
		p.LineTo(ox+x2, oy+y2)
		return
	}
	phi := float64(rotDeg) * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)
	dx, dy := float64(x1-x2)/2, float64(y1-y2)/2
	x1p := cosP*dx + sinP*dy
	y1p := -sinP*dx + cosP*dy
	if l := x1p*x1p/(frx*frx) + y1p*y1p/(fry*fry); l > 1 {
		s := math.Sqrt(l)
		frx, fry = frx*s, fry*s
	}
	num := frx*frx*fry*fry - frx*frx*y1p*y1p - fry*fry*x1p*x1p
	den := frx*frx*y1p*y1p + fry*fry*x1p*x1p
	coef := 0.0
	if den > 0 && num > 0 {
		coef = math.Sqrt(num / den)
	}
	if large == sweep {
		coef = -coef
	}
	cxp, cyp := coef*frx*y1p/fry, -coef*fry*x1p/frx
	cx := cosP*cxp - sinP*cyp + float64(x1+x2)/2
	cy := sinP*cxp + cosP*cyp + float64(y1+y2)/2
	ang := func(ux, uy, vx, vy float64) float64 { return math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy) }
	th1 := ang(1, 0, (x1p-cxp)/frx, (y1p-cyp)/fry)
	dth := ang((x1p-cxp)/frx, (y1p-cyp)/fry, (-x1p-cxp)/frx, (-y1p-cyp)/fry)
	if !sweep && dth > 0 {
		dth -= 2 * math.Pi
	} else if sweep && dth < 0 {
		dth += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dth) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dth / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64, float64, float64) {
		ct, st := math.Cos(t), math.Sin(t)
		x := cx + frx*ct*cosP - fry*st*sinP
		y := cy + frx*ct*sinP + fry*st*cosP
		dxt := -frx*st*cosP - fry*ct*sinP
		dyt := -frx*st*sinP + fry*ct*cosP
		return x, y, dxt, dyt
	}
	t := th1
	for i := 0; i < n; i++ {
		ax, ay, adx, ady := pt(t)
		bx, by, bdx, bdy := pt(t + step)
		p.CubicTo(ox+float32(ax+k*adx), oy+float32(ay+k*ady),
			ox+float32(bx-k*bdx), oy+float32(by-k*bdy), ox+float32(bx), oy+float32(by))
		t += step
	}
}

// --- mask-image ---

// maskSpec is a parsed mask layer (the first layer of mask-image).
type maskSpec struct {
	url  string
	grad *gradientSpec

	size         string // "", auto, contain, cover; else explicit sizeW/sizeH
	sizeW, sizeH *lenPct
	posX, posY   lenPct
	repeatX      bool
	repeatY      bool
	origin       string // reference box for positioning (default border-box)
	luminance    bool
}

// parseMask reads mask-image and its longhands (plus the -webkit- forms
// and the mask / -webkit-mask shorthands) from the declarations.
func parseMask(m map[string]string, fs float32) (*maskSpec, bool) {
	get := func(prop string) (string, bool) {
		if v, ok := m[prop]; ok {
			return v, true
		}
		v, ok := m["-webkit-"+prop]
		return v, ok
	}
	ms := &maskSpec{repeatX: true, repeatY: true}
	ms.posX, ms.posY = lenPct{}, lenPct{} // initial mask-position: 0% 0%
	have := false
	setImage := func(v string) bool {
		v = strings.TrimSpace(v)
		if g, ok := parseGradient(v); ok {
			ms.grad, ms.url = g, ""
			return true
		}
		if u, ok := parseCSSURL(v); ok {
			ms.url, ms.grad = u, nil
			return true
		}
		return false
	}
	// Shorthand first; longhands declared with it win (they come later).
	if v, ok := get("mask"); ok {
		layer := strings.TrimSpace(splitTopLevel(v, ',')[0])
		var posToks []string
		afterSlash := false
		var sizeToks []string
		for _, tok := range fieldsTopLevel(layer) {
			lt := strings.ToLower(tok)
			switch {
			case strings.Contains(lt, "(") && setImage(tok):
				have = true
			case lt == "/":
				afterSlash = true
			case strings.HasPrefix(lt, "/"):
				afterSlash = true
				sizeToks = append(sizeToks, lt[1:])
			case lt == "repeat" || lt == "no-repeat" || lt == "repeat-x" || lt == "repeat-y" || lt == "space" || lt == "round":
				ms.setRepeat(lt)
			case lt == "alpha" || lt == "luminance" || lt == "match-source":
				ms.luminance = lt == "luminance"
			case isGeometryBox(lt):
				if ms.origin == "" {
					ms.origin = lt
				}
			case lt == "add" || lt == "subtract" || lt == "intersect" || lt == "exclude" || lt == "no-clip" || lt == "none":
			default:
				if afterSlash {
					sizeToks = append(sizeToks, lt)
				} else {
					posToks = append(posToks, lt)
				}
			}
		}
		if len(posToks) > 0 {
			if x, y, ok := parsePosition(posToks, fs); ok {
				ms.posX, ms.posY = x, y
			}
		}
		if len(sizeToks) > 0 {
			ms.setSize(strings.Join(sizeToks, " "), fs)
		}
	}
	if v, ok := get("mask-image"); ok {
		have = setImage(strings.TrimSpace(splitTopLevel(v, ',')[0]))
	}
	if !have {
		return nil, false
	}
	if v, ok := get("mask-size"); ok {
		ms.setSize(strings.TrimSpace(splitTopLevel(v, ',')[0]), fs)
	}
	if v, ok := get("mask-position"); ok {
		if x, y, ok := parsePosition(strings.Fields(strings.ToLower(strings.TrimSpace(splitTopLevel(v, ',')[0]))), fs); ok {
			ms.posX, ms.posY = x, y
		}
	}
	if v, ok := get("mask-repeat"); ok {
		f := strings.Fields(strings.ToLower(strings.TrimSpace(splitTopLevel(v, ',')[0])))
		if len(f) == 2 {
			ms.repeatX = f[0] == "repeat" || f[0] == "space" || f[0] == "round"
			ms.repeatY = f[1] == "repeat" || f[1] == "space" || f[1] == "round"
		} else if len(f) == 1 {
			ms.setRepeat(f[0])
		}
	}
	if v, ok := get("mask-origin"); ok {
		if lt := strings.ToLower(strings.TrimSpace(v)); isGeometryBox(lt) {
			ms.origin = lt
		}
	}
	if v, ok := get("mask-mode"); ok {
		ms.luminance = strings.EqualFold(strings.TrimSpace(v), "luminance")
	}
	return ms, true
}

func (ms *maskSpec) setRepeat(kw string) {
	switch kw {
	case "no-repeat":
		ms.repeatX, ms.repeatY = false, false
	case "repeat-x":
		ms.repeatX, ms.repeatY = true, false
	case "repeat-y":
		ms.repeatX, ms.repeatY = false, true
	default: // repeat, space, round
		ms.repeatX, ms.repeatY = true, true
	}
}

func (ms *maskSpec) setSize(v string, fs float32) {
	v = strings.ToLower(strings.TrimSpace(v))
	ms.size, ms.sizeW, ms.sizeH = "", nil, nil
	switch v {
	case "", "auto", "auto auto":
		return
	case "contain", "cover":
		ms.size = v
		return
	}
	f := strings.Fields(v)
	one := func(t string) *lenPct {
		if t == "auto" {
			return nil
		}
		if l, ok := parseLenPct(t, fs); ok {
			return &l
		}
		return nil
	}
	ms.sizeW = one(f[0])
	if len(f) > 1 {
		ms.sizeH = one(f[1])
	}
	ms.size = "explicit"
}

// tileSize resolves the mask image size in area (CSS background-size
// rules); iw/ih are the intrinsic size (0 = none).
func (ms *maskSpec) tileSize(area qui.Rect, iw, ih, ratio float32) (float32, float32) {
	switch ms.size {
	case "contain", "cover":
		if ratio == 0 {
			return area.W, area.H
		}
		return fitRatio(area, ratio, ms.size == "cover")
	case "explicit":
		var w, h float32
		hasW, hasH := ms.sizeW != nil, ms.sizeH != nil
		if hasW {
			w = ms.sizeW.resolve(area.W)
		}
		if hasH {
			h = ms.sizeH.resolve(area.H)
		}
		switch {
		case hasW && hasH:
		case hasW:
			h = area.H
			if ratio > 0 {
				h = w / ratio
			} else if ih > 0 {
				h = ih
			}
		case hasH:
			w = area.W
			if ratio > 0 {
				w = h * ratio
			} else if iw > 0 {
				w = iw
			}
		default: // "auto auto"
			return autoTileSize(area, iw, ih, ratio)
		}
		return w, h
	}
	return autoTileSize(area, iw, ih, ratio)
}

// autoTileSize is `auto` sizing: the intrinsic size; with only a ratio,
// contain (CSS Images 3 default sizing); with neither, the area.
func autoTileSize(area qui.Rect, iw, ih, ratio float32) (float32, float32) {
	if iw > 0 && ih > 0 {
		return iw, ih
	}
	if ratio > 0 {
		return fitRatio(area, ratio, false)
	}
	return area.W, area.H
}

// fitRatio sizes a ratio-only image to contain (or cover) area.
func fitRatio(area qui.Rect, ratio float32, cover bool) (float32, float32) {
	w := area.W
	h := w / ratio
	if !cover == (h > area.H) {
		h = area.H
		w = h * ratio
	}
	return w, h
}

// maskSource is a mask layer's loaded image.
type maskSource struct {
	raster image.Image
	vector *svg.Document
}

// intrinsic returns the image's natural size (0 = none on that axis) and
// aspect ratio (0 = none): an SVG with only a viewBox has a ratio but no
// size.
func (s maskSource) intrinsic() (w, h, ratio float32) {
	switch {
	case s.vector != nil:
		w, h = s.vector.Width, s.vector.Height
		if vb := s.vector.ViewBox; vb.W > 0 && vb.H > 0 {
			ratio = vb.W / vb.H
		}
		switch {
		case w > 0 && h > 0:
			ratio = w / h
		case w > 0 && ratio > 0:
			h = w / ratio
		case h > 0 && ratio > 0:
			w = h * ratio
		}
		return w, h, ratio
	case s.raster != nil:
		b := s.raster.Bounds()
		w, h = float32(b.Dx()), float32(b.Dy())
		if h > 0 {
			ratio = w / h
		}
		return w, h, ratio
	}
	return 0, 0, 0
}

// painter returns the Box.Mask callback for this layer. src is the loaded
// image (zero for a gradient layer, or one still loading — which masks
// everything away, as an unloaded mask image is transparent).
func (ms *maskSpec) painter(src maskSource, border, padding, margin qui.Insets) func(qui.Canvas, qui.Rect) {
	lum := luminanceToAlpha()
	return func(canvas qui.Canvas, bounds qui.Rect) {
		area := referenceBox(bounds, ms.origin, border, padding, margin)
		if area.W <= 0 || area.H <= 0 {
			return
		}
		iw, ih, ratio := src.intrinsic()
		tw, th := ms.tileSize(area, iw, ih, ratio)
		if tw <= 0 || th <= 0 {
			return
		}
		depth := canvas.Save()
		defer canvas.RestoreTo(depth)
		// mask-clip: border-box — nothing outside the border box shows.
		canvas.ClipRect(bounds)
		if ms.luminance {
			canvas.SaveLayer(bounds, qui.Paint{ColorFilter: lum})
		}
		x0 := area.X + ms.posX.resolve(area.W-tw)
		y0 := area.Y + ms.posY.resolve(area.H-th)
		xs, ys := []float32{x0}, []float32{y0}
		if ms.repeatX {
			xs = tileStarts(x0, tw, bounds.X, bounds.X+bounds.W)
		}
		if ms.repeatY {
			ys = tileStarts(y0, th, bounds.Y, bounds.Y+bounds.H)
		}
		for _, y := range ys {
			for _, x := range xs {
				r := qui.Rect{X: x, Y: y, W: tw, H: th}
				switch {
				case ms.grad != nil:
					canvas.DrawShape(qui.ShapeRect(r), qui.Paint{Shader: ms.grad.shaderFor(r), AntiAlias: true})
				case src.vector != nil:
					canvas.DrawVector(src.vector, r, qui.Color{})
				case src.raster != nil:
					canvas.DrawImage(src.raster, r)
				}
			}
		}
	}
}

// luminanceToAlpha is mask-mode: luminance — alpha becomes the color's
// luminance times its alpha (approximated on straight color: alpha × lum).
func luminanceToAlpha() qui.ColorFilter { return lumFilter{} }

type lumFilter struct{}

func (lumFilter) ApplyColor(c qui.Color) qui.Color {
	l := 0.2125*c.R + 0.7154*c.G + 0.0721*c.B
	return qui.Color{R: 0, G: 0, B: 0, A: c.A * l}
}

// tileStarts lists the tile origins along one axis that cover [lo, hi),
// aligned so one tile starts at start.
func tileStarts(start, size, lo, hi float32) []float32 {
	if size < 0.5 {
		size = 0.5
	}
	first := start - float32(math.Ceil(float64((start-lo)/size)))*size
	var out []float32
	for x := first; x < hi && len(out) < 4096; x += size {
		out = append(out, x)
	}
	return out
}

// loadMaskSource loads a mask-image url(): through the document's
// resource loader when there is one (q-browser), else local files and
// data: URIs — SVG kept as a vector so an icon mask stays crisp.
func (e *El) loadMaskSource(src string) maskSource {
	if res, ok := e.loadExternal(src); ok {
		if res.Vector != nil || res.Raster != nil {
			return maskSource{raster: res.Raster, vector: res.Vector}
		}
		if res.Pending {
			return maskSource{}
		}
	}
	low := strings.ToLower(src)
	if strings.HasPrefix(low, "data:") {
		comma := strings.IndexByte(src, ',')
		if comma > 0 && strings.Contains(strings.ToLower(src[:comma]), "svg") {
			payload := src[comma+1:]
			var raw []byte
			if strings.Contains(strings.ToLower(src[:comma]), "base64") {
				b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
				if err != nil {
					return maskSource{}
				}
				raw = b
			} else {
				if u, err := url.PathUnescape(payload); err == nil {
					payload = u
				}
				raw = []byte(payload)
			}
			if doc, err := svg.ParseBytes(raw); err == nil {
				return maskSource{vector: doc}
			}
			return maskSource{}
		}
	}
	baseDir := ""
	if e.engine != nil {
		baseDir = e.engine.opts.BaseDir
	}
	if strings.HasSuffix(low, ".svg") && !strings.HasPrefix(low, "http") {
		path := src
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, src)
		}
		if doc, err := svg.ParseFile(path); err == nil {
			return maskSource{vector: doc}
		}
		return maskSource{}
	}
	return maskSource{raster: loadRasterImage(src, baseDir)}
}

// applyMasking wires clip-path and mask-image onto the element's box.
func (e *El) applyMasking(cs *ComputedStyle) {
	border, padding, margin := boxEdges(cs)
	if c := cs.ClipPath; c != nil {
		e.Box.ClipPath = func(b qui.Rect) *qui.Path { return c.build(b, border, padding, margin) }
	} else {
		e.Box.ClipPath = nil
	}
	ms := cs.Mask
	if ms == nil {
		e.Box.Mask, e.maskSrcURL, e.maskSrc = nil, "", maskSource{}
		return
	}
	if ms.url != "" && (ms.url != e.maskSrcURL || (e.maskSrc.raster == nil && e.maskSrc.vector == nil)) {
		e.maskSrc = e.loadMaskSource(ms.url)
		e.maskSrcURL = ms.url
	}
	src := e.maskSrc
	if ms.url == "" {
		src = maskSource{}
	}
	e.Box.Mask = ms.painter(src, border, padding, margin)
}
