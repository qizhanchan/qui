package htmlcss

import "github.com/qizhanchan/qui"

// canvasLeaf is the widget backing a <canvas> element whose pixels are
// produced by a user-supplied paint callback (El.SetCanvasDraw). It fills
// its layout bounds and invokes draw(canvas, bounds) each frame — the
// "draw it yourself" escape hatch the html-css engine offers for
// <canvas>, mirroring how a browser hands JavaScript a 2D drawing
// context. For richer needs (event handling, animation) callers can plug
// an arbitrary widget in via El.SetCanvas instead.
//
// Intrinsic size is the element's CSS width/height, defaulting to HTML's
// 300×150 canvas geometry when neither is declared.
type canvasLeaf struct {
	qui.BaseWidget
	draw        func(cv qui.Canvas, bounds qui.Rect)
	w, h        float32
	placeholder bool // true for the built-in "unwired canvas" stand-in
}

func newCanvasLeaf(draw func(qui.Canvas, qui.Rect), w, h float32) *canvasLeaf {
	c := &canvasLeaf{draw: draw, w: w, h: h}
	c.BaseWidget = qui.NewBaseWidget()
	c.SetSelf(c)
	return c
}

func (c *canvasLeaf) Measure(available qui.Size) qui.Size {
	return qui.Size{W: c.w, H: c.h}
}

func (c *canvasLeaf) Draw(canvas qui.Canvas) {
	if c.draw != nil {
		c.draw(canvas, c.Bounds())
	}
}

func (c *canvasLeaf) HitTest(p qui.Point) qui.Widget {
	if c.Bounds().Contains(p) {
		return c
	}
	return nil
}

// Role reports a graphics role so the accessibility / agent surface can
// address the canvas region.
func (c *canvasLeaf) Role() string { return qui.RoleImage }

// canvasPlaceholderDraw paints the stand-in for a <canvas> that has no
// drawing wired yet: a dashed frame with a diagonal so an un-wired canvas
// still reads as a distinct, visible region (mirrors the <img> placeholder).
func canvasPlaceholderDraw(cv qui.Canvas, b qui.Rect) {
	cv.FillRect(b, qui.Color{R: 0.96, G: 0.96, B: 0.97, A: 1})
	line := qui.Color{R: 0.72, G: 0.74, B: 0.80, A: 1}
	cv.StrokeRect(b, line, 1)
	cv.DrawLine(qui.Point{X: b.X, Y: b.Y}, qui.Point{X: b.X + b.W, Y: b.Y + b.H}, line, 1)
	cv.DrawLine(qui.Point{X: b.X + b.W, Y: b.Y}, qui.Point{X: b.X, Y: b.Y + b.H}, line, 1)
}
