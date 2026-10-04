package htmlcss

import (
	"testing"

	"github.com/qizhanchan/qui"
)

// The text-decoration shorthand carries line + style + color + thickness in
// any order; all four are parsed onto the computed style.
func TestTextDecorationShorthandExtras(t *testing.T) {
	res := RenderDoc(`<body><span id="s">x</span></body>`,
		`#s{ text-decoration: underline wavy #ff0000 3px }`, Options{})
	cs := res.ByID["s"].(*El).lastCS
	if !cs.Underline {
		t.Error("underline line not set")
	}
	if cs.DecorationStyle != qui.DecorationWavy {
		t.Errorf("style = %v, want wavy", cs.DecorationStyle)
	}
	if !cs.HasDecorationColor || cs.DecorationColor.R < 0.9 || cs.DecorationColor.G > 0.1 {
		t.Errorf("color = %+v (has=%v), want red", cs.DecorationColor, cs.HasDecorationColor)
	}
	if cs.DecorationThickness != 3 {
		t.Errorf("thickness = %v, want 3", cs.DecorationThickness)
	}
}

// Longhand properties set / override the decoration paint.
func TestTextDecorationLonghands(t *testing.T) {
	res := RenderDoc(`<body><span id="s">x</span></body>`, `#s{
		text-decoration-line: line-through;
		text-decoration-style: dashed;
		text-decoration-color: #0000ff;
		text-decoration-thickness: 2px;
	}`, Options{})
	cs := res.ByID["s"].(*El).lastCS
	if !cs.LineThrough {
		t.Error("line-through not set")
	}
	if cs.DecorationStyle != qui.DecorationDashed {
		t.Errorf("style = %v, want dashed", cs.DecorationStyle)
	}
	if !cs.HasDecorationColor || cs.DecorationColor.B < 0.9 {
		t.Errorf("color = %+v, want blue", cs.DecorationColor)
	}
	if cs.DecorationThickness != 2 {
		t.Errorf("thickness = %v, want 2", cs.DecorationThickness)
	}
	// A longhand wins over the shorthand's implicit values.
	dp := cs.decorationPaint()
	if dp.Style != qui.DecorationDashed || !dp.HasColor || dp.Thickness != 2 {
		t.Errorf("decorationPaint() = %+v, want dashed/blue/2", dp)
	}
}

// The paint reaches a plain-text Label's paragraph style.
func TestTextDecorationOnLabel(t *testing.T) {
	res := RenderDoc(`<body><p id="p">hello</p></body>`,
		`#p{ text-decoration: underline dotted #00aa00 }`, Options{})
	res.Root.Layout(qui.Rect{X: 0, Y: 0, W: 300, H: 100})
	el := res.ByID["p"].(*El)
	if el.textLabel == nil {
		t.Fatal("paragraph should fold to a plain-text label")
	}
	dp := el.textLabel.Paragraph.DecorationPaint
	if dp.Style != qui.DecorationDotted {
		t.Errorf("label decoration style = %v, want dotted", dp.Style)
	}
	if !dp.HasColor || dp.Color.G < 0.5 {
		t.Errorf("label decoration color = %+v, want green", dp.Color)
	}
	if el.textLabel.Paragraph.Decoration&qui.DecorationUnderline == 0 {
		t.Error("label should carry the underline line")
	}
}

// Default (no color / style / thickness) leaves a zero paint so the renderer
// falls back to text color + solid + auto thickness.
func TestTextDecorationDefaultsZeroPaint(t *testing.T) {
	res := RenderDoc(`<body><span id="s">x</span></body>`,
		`#s{ text-decoration: underline }`, Options{})
	cs := res.ByID["s"].(*El).lastCS
	if dp := cs.decorationPaint(); dp != (qui.DecorationPaint{}) {
		t.Errorf("plain underline should yield a zero paint, got %+v", dp)
	}
}
