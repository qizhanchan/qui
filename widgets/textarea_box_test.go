package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

// boxArea is a CSS-like textarea: 1px border, 4px radius, 9px/11px padding,
// 20px lines.
func boxArea(text string, w, h float32) *TextArea {
	ta := NewTextArea("")
	ta.BoxModel = true
	st := ta.Style()
	st.Border, st.BorderSize, st.Radius = Color{A: 1}, 1, 4
	st.Padding = Insets{Top: 9, Right: 11, Bottom: 9, Left: 11}
	st.Font = Font{Size: 14}
	st.LineHeight = 20.0 / 14 // CSS line-height: 20px at 14px
	ta.Text = text
	ta.Layout(Rect{W: w, H: h})
	return ta
}

// In the box model the text starts at border + padding; without it Padding
// is the whole inset from the outer edge (a cell editor positions its text
// from the box edge).
func TestTextAreaBoxModelInsetsPaddingInsideBorder(t *testing.T) {
	ta := boxArea("hi", 200, 80)
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	if content.X != 12 || content.Y != 10 {
		t.Errorf("content origin = (%v,%v), want (12,10): border 1 + padding 11/9", content.X, content.Y)
	}
	if content.W != 200-24 || content.H != 80-20 {
		t.Errorf("content size = %vx%v, want 176x60", content.W, content.H)
	}

	ta.BoxModel = false
	content, _, _ = ta.visibleContentRect(ta.Bounds())
	if content.X != 11 || content.Y != 9 {
		t.Errorf("legacy content origin = (%v,%v), want (11,9): padding from the outer edge", content.X, content.Y)
	}
}

// An unpadded box-model field keeps the UA inset instead of running the text
// into the border.
func TestTextAreaBoxModelUnpaddedKeepsUAInset(t *testing.T) {
	ta := NewTextArea("")
	ta.BoxModel = true
	ta.Style().Padding = Insets{}
	ta.Layout(Rect{W: 200, H: 80})
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	if content.X < 3 || content.Y < 2 {
		t.Errorf("content origin = (%v,%v), want at least the UA inset (3,2)", content.X, content.Y)
	}
}

// Measure grows with padding and border: two text lines inside them.
func TestTextAreaBoxModelMeasureHonoursPadding(t *testing.T) {
	ta := boxArea("", 0, 0)
	if lh := ta.lineHeight(); lh < 19.99 || lh > 20.01 {
		t.Fatalf("line height = %v, want the style's 20", lh)
	}
	want := float32(60)
	if got := ta.Measure(Size{W: 400, H: 400}).H; got != want {
		t.Errorf("height = %v, want %v (2 lines + 9+9 padding + 1+1 border)", got, want)
	}
	plain := NewTextArea("")
	if got := plain.Measure(Size{W: 400, H: 400}).H; got != 40 {
		t.Errorf("legacy default height = %v, want the 40 minimum", got)
	}
}

// The scrollbar sits against the border's inner edge, outside the padding;
// the text keeps clear of it.
func TestTextAreaBoxModelScrollbarInsideBorderOutsidePadding(t *testing.T) {
	ta := boxArea("1\n2\n3\n4\n5\n6\n7\n8", 200, 80)
	content, showX, showY := ta.visibleContentRect(ta.Bounds())
	if !showY || showX {
		t.Fatalf("showY/showX = %v/%v, want a vertical bar only", showY, showX)
	}
	track := ta.verticalBarTrack(content)
	if got := track.X + track.W; got != 199 {
		t.Errorf("track right edge = %v, want 199 (the border's inner edge)", got)
	}
	if track.Y != 1 || track.Y+track.H != 79 {
		t.Errorf("track spans y %v..%v, want 1..79 (inside the border)", track.Y, track.Y+track.H)
	}
	if gap := track.X - (content.X + content.W); gap != 11 {
		t.Errorf("gap between text and bar = %v, want the 11px right padding", gap)
	}
}

func alphaAt(img *image.RGBA, x, y int) uint8 { return img.RGBAAt(x, y).A }

// The border follows the radius: the corner pixel is clear while the edges
// are solid.
func TestTextAreaBorderHonoursRadius(t *testing.T) {
	ta := boxArea("", 60, 40)
	ta.Style().Radius = 8
	ta.Layout(Rect{W: 60, H: 40})
	canvas, img := NewOffscreenCanvas(60, 40)
	ta.Draw(canvas)
	if a := alphaAt(img, 0, 0); a > 64 {
		t.Errorf("corner pixel alpha = %d, want the corner rounded away", a)
	}
	if a := alphaAt(img, 30, 0); a != 255 {
		t.Errorf("top edge alpha = %d, want solid border", a)
	}
}

// :hover, :focus and :disabled each swap the chrome; a disabled baseline
// textarea dims.
func TestTextAreaStateChrome(t *testing.T) {
	ta := boxArea("", 60, 40)
	hover := *ta.Style()
	hover.Border = Color{R: 1, A: 1}
	focus := *ta.Style()
	focus.Border = Color{G: 1, A: 1}
	ta.HoverStyle, ta.FocusedStyle = &hover, &focus

	if st, _ := ta.chrome(); st.Border != ta.Style().Border {
		t.Error("resting chrome is not the base style")
	}
	ta.hovering = true
	if st, _ := ta.chrome(); st.Border != hover.Border {
		t.Errorf("hover border = %+v, want the hover style", st.Border)
	}
	ta.focused = true
	if st, _ := ta.chrome(); st.Border != focus.Border {
		t.Errorf("focused border = %+v, want the focus style over hover", st.Border)
	}
	ta.SetEnabled(false)
	dis := Color{B: 1, A: 1}
	ta.DisabledStyle = &Style{Border: dis}
	if st, _ := ta.chrome(); st.Border != dis {
		t.Errorf("disabled border = %+v, want the disabled style over focus", st.Border)
	}
	ta.DisabledStyle = nil
	base := NewTextArea("")
	base.SetEnabled(false)
	if st, _ := base.chrome(); st.Foreground != htmlDisabledText || st.Border != htmlDisabledBorder {
		t.Errorf("disabled baseline = fg %+v border %+v, want the dimmed colors", st.Foreground, st.Border)
	}
}
