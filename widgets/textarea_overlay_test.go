package widgets

import (
	"image"
	"testing"

	. "github.com/qizhanchan/qui"
)

func newOverlayArea(text string) *TextArea {
	ta := NewTextArea("")
	ta.Style().Padding = Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}
	ta.Layout(Rect{X: 10, Y: 20, W: 200, H: 80})
	ta.Text = text
	return ta
}

func TestTextAreaRangeRectsOneLine(t *testing.T) {
	ta := newOverlayArea("I saw teh cat")
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	rs := ta.RangeRects(6, 9)
	if len(rs) != 1 {
		t.Fatalf("rects = %v, want one", rs)
	}
	r := rs[0]
	wantX := content.X + TextXForOffset(ta.Text, ta.font(), ta.Paragraph.Direction, 6)
	if d := r.X - wantX; d > 0.5 || d < -0.5 {
		t.Fatalf("x = %v, want %v", r.X, wantX)
	}
	if r.Y != content.Y || r.H != ta.lineHeight() || r.W <= 0 {
		t.Fatalf("rect = %+v, content = %+v", r, content)
	}
	// Reversed bounds are normalised; an empty range has no boxes.
	if got := ta.RangeRects(9, 6); len(got) != 1 || got[0] != r {
		t.Fatalf("reversed = %v, want %v", got, rs)
	}
	if got := ta.RangeRects(4, 4); got != nil {
		t.Fatalf("empty range = %v", got)
	}
}

func TestTextAreaRangeRectsWrapped(t *testing.T) {
	ta := newOverlayArea("alpha beta gamma delta epsilon zeta eta theta iota kappa")
	ta.AutoWrap = true
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	lines := ta.visualLines([]rune(ta.Text), content.W)
	if len(lines) < 2 {
		t.Fatalf("want a wrapped text, got %d lines", len(lines))
	}
	// A range straddling the first soft break gets one box per line.
	rs := ta.RangeRects(lines[0].end-2, lines[1].start+2)
	if len(rs) != 2 {
		t.Fatalf("rects = %v, want two", rs)
	}
	if rs[1].Y != rs[0].Y+ta.lineHeight() {
		t.Fatalf("second box y = %v, want %v", rs[1].Y, rs[0].Y+ta.lineHeight())
	}
	if rs[1].X > content.X+0.5 {
		t.Fatalf("second line box should start at the line start, x = %v (content x %v)", rs[1].X, content.X)
	}
}

func TestTextAreaRangeRectsScrolled(t *testing.T) {
	ta := newOverlayArea("0123456789012345678901234567890123456789 teh")
	n := len([]rune(ta.Text))
	before := ta.RangeRects(n-3, n)
	ta.cursorPos = n
	ta.scrollToCursor()
	if ta.scrollX <= 0 {
		t.Fatal("expected a horizontal scroll")
	}
	after := ta.RangeRects(n-3, n)
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf("before %v after %v", before, after)
	}
	if d := before[0].X - ta.scrollX - after[0].X; d > 0.5 || d < -0.5 {
		t.Fatalf("scrolled x = %v, want %v", after[0].X, before[0].X-ta.scrollX)
	}
	// Lines scrolled out vertically are omitted.
	ta2 := newOverlayArea("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk")
	ta2.scrollY = ta2.lineHeight() * 3
	if rs := ta2.RangeRects(0, 1); rs != nil {
		t.Fatalf("scrolled-away line still has boxes: %v", rs)
	}
}

func TestTextAreaOffsetAtRoundTrip(t *testing.T) {
	ta := newOverlayArea("I saw teh cat\nsecond line")
	for _, off := range []int{0, 3, 6, 9, 12, 14, 18} {
		rs := ta.RangeRects(off, off+1)
		if len(rs) == 0 {
			t.Fatalf("no box for %d", off)
		}
		r := rs[0]
		if got := ta.OffsetAt(Point{X: r.X + r.W*0.25, Y: r.Y + r.H/2}); got != off {
			t.Errorf("OffsetAt(box of %d) = %d", off, got)
		}
	}
}

func TestTextAreaDrawOverlayUnderContentClip(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 240, 120))
	cv := NewImageCanvas(img)
	ta := newOverlayArea("I saw teh cat")
	content, _, _ := ta.visibleContentRect(ta.Bounds())
	calls := 0
	var clip Rect
	ta.DrawOverlay = func(c Canvas) {
		calls++
		clip = c.ClipBounds()
		c.FillRect(Rect{X: 0, Y: 0, W: 240, H: 120}, Color{R: 1, A: 1})
	}
	ta.Draw(cv)
	if calls != 1 {
		t.Fatalf("overlay calls = %d", calls)
	}
	if clip != content {
		t.Fatalf("clip = %+v, want content %+v", clip, content)
	}
	red := func(x, y int) bool {
		r, g, _, _ := img.At(x, y).RGBA()
		return r > 0xf000 && g < 0x1000
	}
	if !red(int(content.X+content.W/2), int(content.Y+content.H/2)) {
		t.Error("overlay did not paint inside the content rect")
	}
	if red(2, 2) || red(int(ta.Bounds().X+2), int(ta.Bounds().Y+2)) {
		t.Error("overlay painted outside the content clip")
	}
}
