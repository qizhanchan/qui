package canvas2d

import (
	"strconv"
	"strings"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/htmlcss"
)

// Font is the serialized font.
func (c *Context) Font() string { return c.st.fontStr }

// SetFont parses the CSS font shorthand; invalid values are ignored.
// Relative sizes resolve against 10px, the canvas default.
func (c *Context) SetFont(v string) {
	f, ok := htmlcss.ParseFont(v, 10)
	if !ok {
		return
	}
	f.LetterSpacing, f.WordSpacing = c.st.font.LetterSpacing, c.st.font.WordSpacing
	c.st.font = f
	c.st.fontStr = serializeFont(v, f)
}

// serializeFont normalizes the font shorthand the way Chrome reports it:
// "[italic ][bold ]<size>px <families>".
func serializeFont(v string, f qui.Font) string {
	fields := strings.Fields(v)
	fam := ""
	for i, fld := range fields {
		base := fld
		if j := strings.IndexByte(fld, '/'); j >= 0 {
			base = fld[:j]
		}
		if strings.ContainsAny(base, "0123456789") && (strings.HasSuffix(base, "px") || strings.HasSuffix(base, "pt") || strings.HasSuffix(base, "em") || strings.HasSuffix(base, "rem") || strings.HasSuffix(base, "%")) {
			fam = strings.Join(fields[i+1:], " ")
			break
		}
	}
	var b strings.Builder
	if f.Italic {
		b.WriteString("italic ")
	}
	if f.Weight != 0 && f.Weight != qui.FontWeightNormal {
		b.WriteString(itoa(int(f.Weight)))
		b.WriteByte(' ')
	}
	b.WriteString(trimFloat(float64(f.Size)))
	b.WriteString("px ")
	b.WriteString(fam)
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 32) }

// spacingPx parses letterSpacing / wordSpacing (a CSS length in px).
func spacingPx(v string) (float32, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasSuffix(v, "px") {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
	return float32(f), err == nil && finite(f)
}

func (c *Context) TextAlign() string { return c.st.align }
func (c *Context) SetTextAlign(v string) {
	switch v {
	case "start", "end", "left", "right", "center":
		c.st.align = v
	}
}

func (c *Context) TextBaseline() string { return c.st.baseline }
func (c *Context) SetTextBaseline(v string) {
	switch v {
	case "top", "hanging", "middle", "alphabetic", "ideographic", "bottom":
		c.st.baseline = v
	}
}

func (c *Context) Direction() string { return c.st.direction }
func (c *Context) SetDirection(v string) {
	switch v {
	case "ltr", "rtl", "inherit":
		c.st.direction = v
	}
}

// TextMetrics is measureText's result.
type TextMetrics struct {
	Width                                              float64
	ActualBoundingBoxLeft, ActualBoundingBoxRight      float64
	ActualBoundingBoxAscent, ActualBoundingBoxDescent  float64
	FontBoundingBoxAscent, FontBoundingBoxDescent      float64
	EmHeightAscent, EmHeightDescent                    float64
	HangingBaseline, AlphabeticBaseline, IdeographicBaseline float64
}

func normalizeText(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\f', '\r':
			return ' '
		}
		return r
	}, s)
}

// baselineShift is how far below the requested y the alphabetic baseline
// lies for the current textBaseline.
func (c *Context) baselineShift() float64 {
	asc, desc := qui.FontVerticalMetrics(c.st.font)
	a, d := float64(asc), float64(desc)
	switch c.st.baseline {
	case "top":
		return a
	case "hanging":
		return a * 0.8
	case "middle":
		return (a - d) / 2
	case "ideographic", "bottom":
		return -d
	}
	return 0
}

// alignShift is the x offset of the text start for a run of width w.
func (c *Context) alignShift(w float64) float64 {
	rtl := c.st.direction == "rtl"
	switch c.st.align {
	case "center":
		return -w / 2
	case "right":
		return -w
	case "end":
		if !rtl {
			return -w
		}
	case "start":
		if rtl {
			return -w
		}
	}
	return 0
}

// textPath lays text out at (x, y): the outline in user space, the extra
// transform (a horizontal squeeze about x for maxWidth) and the device
// bounds. ok is false when nothing is drawn.
func (c *Context) textPath(text string, x, y float64, maxWidth *float64) (*qui.Path, qui.Matrix, qui.Rect, bool) {
	if c.bmp == nil || !finite(x, y) || (maxWidth != nil && (!finite(*maxWidth) || *maxWidth <= 0)) {
		return nil, qui.Matrix{}, qui.Rect{}, false
	}
	text = normalizeText(text)
	_, adv := qui.TextOutline(text, c.st.font, 0, 0)
	w := float64(adv)
	sx := 1.0
	if maxWidth != nil && w > *maxWidth && w > 0 {
		sx = *maxWidth / w
	}
	ox := x + c.alignShift(w*sx)
	outline, _ := qui.TextOutline(text, c.st.font, float32(ox), float32(y+c.baselineShift()))
	extra := qui.IdentityMatrix()
	if sx != 1 {
		// Squeeze about the start of the run.
		extra = qui.Matrix{A: float32(sx), D: 1, TX: float32(ox * (1 - sx))}
	}
	if outline.IsEmpty() {
		return nil, extra, qui.Rect{}, false
	}
	b := c.st.m.Concat(extra).TransformRect(outline.Bounds())
	return outline, extra, b, true
}

// FillText is fillText; maxWidth nil means none.
func (c *Context) FillText(text string, x, y float64, maxWidth *float64) {
	if p, extra, b, ok := c.textPath(text, x, y, maxWidth); ok {
		c.drawUser(p, extra, b, c.st.fill, false, false)
	}
}

// StrokeText is strokeText.
func (c *Context) StrokeText(text string, x, y float64, maxWidth *float64) {
	if p, extra, b, ok := c.textPath(text, x, y, maxWidth); ok {
		c.drawUser(p, extra, b, c.st.stroke, true, false)
	}
}

// MeasureText is measureText.
func (c *Context) MeasureText(text string) TextMetrics {
	text = normalizeText(text)
	outline, adv := qui.TextOutline(text, c.st.font, 0, 0)
	asc, desc := qui.FontVerticalMetrics(c.st.font)
	w := float64(adv)
	shift := c.baselineShift()
	ax := c.alignShift(w)
	tm := TextMetrics{
		Width:                  w,
		FontBoundingBoxAscent:  float64(asc) - shift,
		FontBoundingBoxDescent: float64(desc) + shift,
		EmHeightAscent:         float64(asc) - shift,
		EmHeightDescent:        float64(desc) + shift,
		AlphabeticBaseline:     -shift,
		HangingBaseline:        float64(asc)*0.8 - shift,
		IdeographicBaseline:    -float64(desc) - shift,
	}
	if !outline.IsEmpty() {
		b := outline.Bounds()
		tm.ActualBoundingBoxLeft = -(float64(b.X) + ax)
		tm.ActualBoundingBoxRight = float64(b.X+b.W) + ax
		tm.ActualBoundingBoxAscent = -float64(b.Y) - shift
		tm.ActualBoundingBoxDescent = float64(b.Y+b.H) + shift
	} else {
		tm.ActualBoundingBoxLeft = -ax
		tm.ActualBoundingBoxRight = w + ax
	}
	return tm
}
