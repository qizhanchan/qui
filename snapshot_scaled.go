package qui

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// SnapshotScaled returns the last fully-rendered frame resampled to
// logical_size × scale. Returns nil if no frame has been rendered yet
// (e.g., before the first Step) or if the renderer is not a
// GLRenderer.
//
// Scale interpretation: 1.0 == logical-pixel resolution (DPR cancels
// out), 0.5 == half logical, 2.0 == physical pixel size on a Retina
// display. Pick a value < 1 to reduce LLM token usage at the cost of
// detail; pick 2 if you need to OCR small text.
//
// Algorithm:
//   - Source is the physical CPU image (logical_size × DPR).
//   - For shrink (src/dst > 1) we average a src×src box per output
//     pixel — alpha-aware integer sum, no fp.
//   - For 1:1 the source is copied directly.
//   - For expand (src/dst < 1) we nearest-neighbor sample; LLMs read
//     better-than-bilinear interpolation poorly anyway, and this
//     keeps boundaries crisp.
//
// Cost: O(src_pixels) for shrink, O(dst_pixels) for expand. On a
// 1600×1000 Retina frame at scale=0.5 this is ~1.6 MP processed →
// well under a millisecond on a modern CPU. Output is a fresh
// *image.RGBA — callers may mutate it (e.g., add annotations) without
// affecting the renderer's retained buffer.
//
// Concurrency: the copy is serialized through the main-thread job
// queue (same discipline as Click/Type), so a capture taken from an
// agent HTTP handler never races a frame the rasterizer is painting.
// Consequently do NOT call this from the main goroutine of a live
// window (animators, event handlers) — use Snapshot there.
func (w *Window) SnapshotScaled(scale float32) *image.RGBA {
	var img *image.RGBA
	_ = w.synchronously(func() error {
		img = w.snapshotScaledInline(scale)
		return nil
	})
	return img
}

func (w *Window) snapshotScaledInline(scale float32) *image.RGBA {
	if w == nil || scale <= 0 {
		return nil
	}
	src := w.Snapshot()
	if src == nil {
		return nil
	}
	logical := w.Bounds()
	dstW := int(logical.W*scale + 0.5)
	dstH := int(logical.H*scale + 0.5)
	return resampleRGBA(src, dstW, dstH)
}

// SnapshotRegion captures a logical-coordinate sub-rectangle scaled
// to the given factor. The rect is clamped to the window bounds;
// passing the full window bounds is equivalent to SnapshotScaled.
//
// Common use: focus on a specific widget by passing its Bounds(),
// reducing the LLM context to just the part of the UI being acted
// on. Serialized like SnapshotScaled — off-main-goroutine callers
// only on a live window.
func (w *Window) SnapshotRegion(r Rect, scale float32) *image.RGBA {
	var img *image.RGBA
	_ = w.synchronously(func() error {
		img = w.snapshotRegionInline(r, scale)
		return nil
	})
	return img
}

func (w *Window) snapshotRegionInline(r Rect, scale float32) *image.RGBA {
	if w == nil || scale <= 0 {
		return nil
	}
	src := w.Snapshot()
	if src == nil {
		return nil
	}
	logical := w.Bounds()
	r = r.Intersect(logical)
	if r.IsEmpty() {
		return nil
	}
	// EffectiveScale, not DevicePixelRatio: the captured image is
	// viewport×dpr×zoom pixels, so a viewport rect converts with both
	// factors. Using the bare DPR would crop the wrong region whenever
	// the window is zoomed.
	dpr := w.EffectiveScale()
	srcRect := image.Rect(
		int(r.X*dpr+0.5),
		int(r.Y*dpr+0.5),
		int((r.X+r.W)*dpr+0.5),
		int((r.Y+r.H)*dpr+0.5),
	)
	srcRect = srcRect.Intersect(src.Bounds())
	if srcRect.Empty() {
		return nil
	}
	sub := image.NewRGBA(image.Rect(0, 0, srcRect.Dx(), srcRect.Dy()))
	for y := 0; y < srcRect.Dy(); y++ {
		for x := 0; x < srcRect.Dx(); x++ {
			sub.SetRGBA(x, y, src.RGBAAt(srcRect.Min.X+x, srcRect.Min.Y+y))
		}
	}
	dstW := int(r.W*scale + 0.5)
	dstH := int(r.H*scale + 0.5)
	return resampleRGBA(sub, dstW, dstH)
}

// AnnotateOptions controls SnapshotAnnotated. The zero value draws
// every visible widget's bounds and labels them with id (when set),
// otherwise role:name.
type AnnotateOptions struct {
	// Scale is the snapshot scale factor (matches SnapshotScaled). 0
	// is treated as 1.
	Scale float32

	// OnlyLeaves restricts annotation to leaf widgets (those without
	// children). Reduces clutter on deep trees.
	OnlyLeaves bool

	// OnlyFocusable restricts annotation to widgets that implement
	// Focusable() == true. Useful for "what can the agent interact
	// with right now".
	OnlyFocusable bool

	// MaxDepth caps annotation depth (0 = no cap).
	MaxDepth int

	// Highlight is an optional selector. Matching widgets get a
	// thicker / different-colored outline. Non-matching widgets are
	// still annotated (use OnlyLeaves to thin them out) unless
	// HideUnhighlighted is set.
	Highlight string

	// HideUnhighlighted removes annotation for widgets that don't
	// match Highlight. Implies Highlight is non-empty.
	HideUnhighlighted bool
}

// SnapshotAnnotated returns a SnapshotScaled image with widget
// bounding boxes and id/role/name labels drawn on top. The result is
// the single most useful artifact for an agent: one PNG that shows
// the UI AND tells the agent how to address each piece.
//
// Annotations are drawn in the snapshot's pixel space (post-scale),
// so dst-pixel positions match what an OCR or vision model would
// read. Outline thickness is fixed at 1 pixel; labels use the basic
// 7×13 bitmap font to stay sharp at any DPR. Serialized like
// SnapshotScaled (the AX-tree walk must not race widget mutations
// either) — off-main-goroutine callers only on a live window.
func (w *Window) SnapshotAnnotated(opts AnnotateOptions) *image.RGBA {
	var img *image.RGBA
	_ = w.synchronously(func() error {
		img = w.snapshotAnnotatedInline(opts)
		return nil
	})
	return img
}

func (w *Window) snapshotAnnotatedInline(opts AnnotateOptions) *image.RGBA {
	if w == nil {
		return nil
	}
	scale := opts.Scale
	if scale <= 0 {
		scale = 1
	}
	img := w.snapshotScaledInline(scale)
	if img == nil {
		return nil
	}
	tree := w.AccessibilityTree()
	if tree == nil {
		return img
	}
	// Resolve highlight set (if any).
	var highlightSet map[*AXNode]bool
	if opts.Highlight != "" {
		if nodes, err := w.FindNodes(opts.Highlight); err == nil {
			highlightSet = make(map[*AXNode]bool, len(nodes))
			for _, n := range nodes {
				highlightSet[n] = true
			}
		}
	}
	depth := 0
	for _, node := range tree.Root.subtree(nil) {
		annotateNode(img, scale, node, depth, opts, highlightSet)
	}
	for _, ov := range tree.Overlays {
		for _, node := range ov.subtree(nil) {
			annotateNode(img, scale, node, depth, opts, highlightSet)
		}
	}
	return img
}

// subtree returns the node and every descendant flattened in
// pre-order. Tests use it; AnnotatedSnapshot walks via this too.
func (n *AXNode) subtree(acc []*AXNode) []*AXNode {
	if n == nil {
		return acc
	}
	acc = append(acc, n)
	for _, c := range n.Children {
		acc = c.subtree(acc)
	}
	return acc
}

func annotateNode(img *image.RGBA, scale float32, node *AXNode, depth int, opts AnnotateOptions, highlight map[*AXNode]bool) {
	if node == nil || !node.Visible {
		return
	}
	if opts.MaxDepth > 0 {
		if d := annotateDepth(node); d > opts.MaxDepth {
			return
		}
	}
	if opts.OnlyLeaves && len(node.Children) > 0 {
		return
	}
	if opts.OnlyFocusable {
		// Heuristic: focusable widgets typically have a non-empty
		// role in the focused interaction set. Without window
		// context we can't run Focusable() here; agents that need
		// strict filtering should pass a selector via Highlight.
		switch node.Role {
		case RoleButton, RoleTextbox, RoleTextarea, RoleCheckbox, RoleRadio,
			RoleSlider, RoleCombobox, RoleListitem, RoleTab,
			RoleMenuitem:
		default:
			return
		}
	}
	isHighlight := highlight != nil && highlight[node]
	if opts.HideUnhighlighted && !isHighlight {
		return
	}
	r := node.Bounds
	x0 := int(r.X*scale + 0.5)
	y0 := int(r.Y*scale + 0.5)
	x1 := int((r.X+r.W)*scale + 0.5)
	y1 := int((r.Y+r.H)*scale + 0.5)
	col := annotationColor(depth, isHighlight)
	drawRectOutline(img, x0, y0, x1, y1, col, 1)
	label := annotationLabel(node)
	if label != "" {
		drawLabel(img, x0+2, y0+2, label, col)
	}
}

func annotateDepth(n *AXNode) int {
	// Walk the cached path string and count slashes — cheap proxy
	// for tree depth.
	d := 0
	for _, c := range n.Path {
		if c == '/' {
			d++
		}
	}
	return d
}

func annotationLabel(n *AXNode) string {
	if n == nil {
		return ""
	}
	if n.ID != "" {
		return "#" + n.ID
	}
	if n.Name != "" {
		return n.Role + ":" + n.Name
	}
	return n.Role
}

func annotationColor(depth int, highlight bool) color.RGBA {
	if highlight {
		return color.RGBA{R: 0xff, G: 0x20, B: 0x40, A: 0xff}
	}
	palette := []color.RGBA{
		{R: 0x4d, G: 0xa6, B: 0xff, A: 0xff},
		{R: 0x36, G: 0xe8, B: 0x8c, A: 0xff},
		{R: 0xff, G: 0xb3, B: 0x33, A: 0xff},
		{R: 0xd9, G: 0x73, B: 0xff, A: 0xff},
	}
	return palette[depth%len(palette)]
}

// drawRectOutline paints a 1-px rectangle outline (clamped to the
// image bounds, no anti-aliasing).
func drawRectOutline(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, _ int) {
	b := img.Bounds()
	if x1 <= x0 || y1 <= y0 {
		return
	}
	for x := x0; x < x1; x++ {
		setPx(img, b, x, y0, c)
		setPx(img, b, x, y1-1, c)
	}
	for y := y0; y < y1; y++ {
		setPx(img, b, x0, y, c)
		setPx(img, b, x1-1, y, c)
	}
}

func setPx(img *image.RGBA, b image.Rectangle, x, y int, c color.RGBA) {
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		return
	}
	img.SetRGBA(x, y, c)
}

// drawLabel paints a small text label with a translucent black
// background plaque. Uses the basic bitmap font already linked by the
// renderer so it stays sharp at any DPR.
func drawLabel(img *image.RGBA, x, y int, text string, c color.RGBA) {
	face := annotationFace()
	if face == nil {
		return
	}
	const padX, padY = 2, 1
	advance := font.MeasureString(face, text).Round()
	height := face.Metrics().Height.Ceil()
	bgX0 := x
	bgY0 := y
	bgX1 := x + advance + 2*padX
	bgY1 := y + height + 2*padY
	plaque := color.RGBA{R: 0, G: 0, B: 0, A: 0xb0}
	for py := bgY0; py < bgY1; py++ {
		for px := bgX0; px < bgX1; px++ {
			setPx(img, img.Bounds(), px, py, plaque)
		}
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.Int26_6((x + padX) << 6),
			Y: fixed.Int26_6((y + padY + face.Metrics().Ascent.Ceil()) << 6),
		},
	}
	d.DrawString(text)
}

// annotationFace returns a small face suitable for compact labels.
// Falls back to the basic 7×13 face the renderer ships as a default.
func annotationFace() font.Face {
	// renderer_gl.go GetFontFace handles the fallback to basicfont
	// when opentype isn't initialized. Use a smallish size so labels
	// stay readable at scale=0.4.
	return GetFontFaceFor(Font{Size: 10})
}

// resampleRGBA returns a fresh *image.RGBA scaled to (dstW × dstH)
// from src. Box-filter shrink, nearest-neighbor expand, identity
// copy at 1:1. Returns nil for invalid inputs.
func resampleRGBA(src *image.RGBA, dstW, dstH int) *image.RGBA {
	if src == nil || dstW <= 0 || dstH <= 0 {
		return nil
	}
	sb := src.Bounds()
	srcW, srcH := sb.Dx(), sb.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if srcW == dstW && srcH == dstH {
		copy(dst.Pix, src.Pix)
		return dst
	}
	// Per-axis ratio. >1 = shrink, <1 = expand.
	rx := float64(srcW) / float64(dstW)
	ry := float64(srcH) / float64(dstH)
	if rx >= 1 && ry >= 1 {
		// Box-average shrink.
		for yo := 0; yo < dstH; yo++ {
			y0 := int(float64(yo) * ry)
			y1 := int(float64(yo+1) * ry)
			if y1 > srcH {
				y1 = srcH
			}
			if y1 == y0 {
				y1 = y0 + 1
			}
			for xo := 0; xo < dstW; xo++ {
				x0 := int(float64(xo) * rx)
				x1 := int(float64(xo+1) * rx)
				if x1 > srcW {
					x1 = srcW
				}
				if x1 == x0 {
					x1 = x0 + 1
				}
				var ar, ag, ab, aa, count uint32
				for sy := y0; sy < y1; sy++ {
					row := sy * src.Stride
					for sx := x0; sx < x1; sx++ {
						i := row + sx*4
						ar += uint32(src.Pix[i])
						ag += uint32(src.Pix[i+1])
						ab += uint32(src.Pix[i+2])
						aa += uint32(src.Pix[i+3])
						count++
					}
				}
				if count == 0 {
					continue
				}
				oi := yo*dst.Stride + xo*4
				dst.Pix[oi] = uint8(ar / count)
				dst.Pix[oi+1] = uint8(ag / count)
				dst.Pix[oi+2] = uint8(ab / count)
				dst.Pix[oi+3] = uint8(aa / count)
			}
		}
		return dst
	}
	// Mixed / expand: nearest neighbor.
	for yo := 0; yo < dstH; yo++ {
		sy := int(float64(yo) * ry)
		if sy >= srcH {
			sy = srcH - 1
		}
		for xo := 0; xo < dstW; xo++ {
			sx := int(float64(xo) * rx)
			if sx >= srcW {
				sx = srcW - 1
			}
			si := sy*src.Stride + sx*4
			oi := yo*dst.Stride + xo*4
			dst.Pix[oi] = src.Pix[si]
			dst.Pix[oi+1] = src.Pix[si+1]
			dst.Pix[oi+2] = src.Pix[si+2]
			dst.Pix[oi+3] = src.Pix[si+3]
		}
	}
	return dst
}
