package widgets

import . "github.com/qizhanchan/qui"

// CSS text-shadow: each layer repaints the text in the shadow color, offset
// by (X, Y) and blurred by Blur (the CSS blur radius; the Gaussian's σ is
// half of it), behind the text. Layers listed first paint on top, so they
// are drawn last-to-first. Spread does not apply to text shadows.

// textShadowExtent is how far the shadows can reach past the text box.
func textShadowExtent(shadows []ShadowStyle) (left, top, right, bottom float32) {
	for _, sh := range shadows {
		halo := sh.Blur * 1.5 // 3σ with σ = blur/2
		left = max(left, halo-sh.X)
		right = max(right, halo+sh.X)
		top = max(top, halo-sh.Y)
		bottom = max(bottom, halo+sh.Y)
	}
	return
}

// textShadowBounds grows r by the shadows' reach.
func textShadowBounds(r Rect, shadows []ShadowStyle) Rect {
	if len(shadows) == 0 || r.IsEmpty() {
		return r
	}
	l, t, rt, b := textShadowExtent(shadows)
	return Rect{X: r.X - l, Y: r.Y - t, W: r.W + l + rt, H: r.H + t + b}
}

// tintColorFilter paints every source pixel in c, keeping the source's
// coverage: the shadow silhouette of whatever the layer holds.
func tintColorFilter(c Color) MatrixColorFilter {
	return MatrixColorFilter{M: [20]float32{
		0, 0, 0, 0, c.R,
		0, 0, 0, 0, c.G,
		0, 0, 0, 0, c.B,
		0, 0, 0, c.A, 0,
	}}
}

// drawTextShadowLayer paints one shadow pass: draw paints the text at its
// normal position; it lands offset by the shadow and, when tint is set,
// recolored to the shadow color through a layer (callers that draw the
// text in the shadow color themselves pass tint=false). area is the text's
// own paint extent, before the offset.
func drawTextShadowLayer(canvas Canvas, sh ShadowStyle, area Rect, tint bool, draw func(Canvas)) {
	if sh.Color.A <= 0 {
		return
	}
	depth := canvas.Save()
	defer canvas.RestoreTo(depth)
	canvas.Translate(sh.X, sh.Y)
	var p Paint
	layer := false
	if tint {
		p.ColorFilter = tintColorFilter(sh.Color)
		layer = true
	}
	if sh.Blur > 0 {
		p.ImageFilter = BlurImageFilter{Radius: sh.Blur / 2 * canvasScale(canvas)}
		layer = true
	}
	if layer {
		halo := sh.Blur * 1.5
		canvas.SaveLayer(Rect{X: area.X - halo, Y: area.Y - halo, W: area.W + 2*halo, H: area.H + 2*halo}, p)
	}
	draw(canvas)
}

// drawTextShadows paints every shadow layer of a text drawn by draw.
func drawTextShadows(canvas Canvas, shadows []ShadowStyle, area Rect, draw func(Canvas)) {
	for i := len(shadows) - 1; i >= 0; i-- {
		drawTextShadowLayer(canvas, shadows[i], area, true, draw)
	}
}
