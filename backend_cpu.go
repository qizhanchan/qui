package qui

import (
	"image"
	"image/color"
	stddraw "image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
)

// cpuBackend is the reference RasterBackend implementation. It writes
// straight into an *image.RGBA framebuffer and reuses the top-level
// rasterizer primitives (fillRectBlend, drawCornerAA, drawLineInto,
// fillPolygonAA, ...) that were the workhorses of the pre-refactor
// imageCanvas.
//
// State:
//   - base:   the framebuffer target owned by GLRenderer. Resized in
//     Begin() when the window physical size changes.
//   - layers: CPU-side layer stack — each frame stores its offscreen
//     RGBA plus the paint to composite it back with. Owned here (not
//     on the frontend canvas) because layer buffers are backend-
//     specific: a future gpuBackend uses FBO IDs instead.
type cpuBackend struct {
	base   *image.RGBA
	layers *layerStack
}

// newCPUBackend constructs a fresh backend. The caller supplies the
// initial framebuffer (usually GLRenderer's *image.RGBA); Begin() will
// resize / reallocate as the physical framebuffer size changes.
func newCPUBackend(base *image.RGBA) *cpuBackend {
	return &cpuBackend{base: base, layers: newLayerStack()}
}

// SetBase swaps the framebuffer target. GLRenderer.Begin calls this
// on resize.
func (b *cpuBackend) SetBase(img *image.RGBA) { b.base = img }

// Begin resets the layer stack for a new frame. The framebuffer image
// itself is managed by GLRenderer (which owns the RGBA lifecycle and
// hands us a fresh pointer via SetBase on resize).
func (b *cpuBackend) Begin(_ Size) {
	if b.layers == nil {
		b.layers = newLayerStack()
		return
	}
	b.layers.frames = b.layers.frames[:0]
}

// End is a no-op for the CPU backend — the framebuffer image already
// holds final pixels for the GL blit that follows.
func (b *cpuBackend) End() {}

// CPUImage returns the framebuffer for GLRenderer's blit step.
func (b *cpuBackend) CPUImage() *image.RGBA { return b.base }

// CompositeToDefault uploads the CPU RGBA into the renderer's shared
// texture and draws it with the same full-screen quad program the
// pre-refactor End() used. Called from GLRenderer.End() after the
// clear/hook/deferredGL replay.
func (b *cpuBackend) CompositeToDefault(r *GLRenderer) {
	if r == nil || b.base == nil {
		return
	}
	compositeCPUImageToDefault(r, b.base)
}

// activeImg returns the innermost layer's buffer, or the base
// framebuffer if no layer is active. Every rasterization method
// resolves the target through this helper — pushing a layer inside a
// widget's Draw() transparently redirects subsequent writes.
func (b *cpuBackend) activeImg() *image.RGBA {
	if b.layers == nil {
		return b.base
	}
	return b.layers.activeImg(b.base)
}

// PushLayer allocates an offscreen RGBA aligned with the current active
// target's device space. `physBounds` is expected to already be
// intersected with the outer clip (frontend does this).
func (b *cpuBackend) PushLayer(physBounds, clip image.Rectangle, paint Paint) {
	outer := b.activeImg()
	// Clamp allocation to the outer target's bounds so we never
	// allocate outside the parent — critical for nested SaveLayer chains.
	physBounds = physBounds.Intersect(outer.Bounds())
	if physBounds.Empty() {
		// Degenerate — push a no-op frame so PopLayer still balances.
		b.layers.push(layerFrame{paint: paint})
		return
	}
	off := image.NewRGBA(physBounds)
	b.layers.push(layerFrame{outer: outer, img: off, paint: paint,
		clip: clip.Intersect(outer.Bounds())})
}

// PopLayer composites the innermost layer back onto its parent through
// the paint stored at PushLayer time. Applies ColorFilter first (per
// pixel), then ImageFilter (whole-image), then Porter-Duff blend with
// optional `mask` for ClipPath's DstIn semantics.
func (b *cpuBackend) PopLayer(mask *image.Alpha) {
	f, ok := b.layers.pop()
	if !ok {
		return
	}
	if f.outer == nil || f.img == nil {
		return
	}
	src := f.img
	if f.paint.ColorFilter != nil {
		applyColorFilterInPlace(src, f.paint.ColorFilter)
	}
	if f.paint.ImageFilter != nil {
		src = f.paint.ImageFilter.ApplyImage(src)
		if src == nil {
			return
		}
	}
	// Confine the composite to the clip the layer was pushed under. A
	// blur / drop-shadow ImageFilter returns an image GROWN past the
	// layer it was handed — legitimate (that halo is the effect), but
	// only inside the clip. Without this a partial repaint smears the
	// halo across the dirty region's border onto pixels the frame never
	// invalidated, so nothing repaints them: a menu hover left a blurred
	// rim around the row that survived until the next full repaint, and
	// darkened again on every hover.
	compositeLayerClipped(f.outer, src, mask, f.paint, f.clip)
}

// Clear paints `color` across the entire active target. Ignores clip
// — matches the old Canvas.Clear semantics (fill the ENTIRE current
// target).
func (b *cpuBackend) Clear(color Color) {
	img := b.activeImg()
	if img == nil {
		return
	}
	fillRectBlend(img, img.Bounds(), color)
}

// FillRect writes color into dst (already clipped by the frontend to
// the intersection of physical rect, physical clip, and target bounds).
func (b *cpuBackend) FillRect(dst image.Rectangle, color Color) {
	img := b.activeImg()
	if img == nil || dst.Empty() {
		return
	}
	dst = dst.Intersect(img.Bounds())
	if dst.Empty() {
		return
	}
	fillRectBlend(img, dst, color)
}

// FillRoundedRect emits an AA rounded fill. Physical coordinates.
func (b *cpuBackend) FillRoundedRect(rect Rect, radius float32, clip Rect, color Color) {
	img := b.activeImg()
	if img == nil {
		return
	}
	imgBounds := img.Bounds()
	clipPix := rectToPixelRect(clip).Intersect(imgBounds)
	if clipPix.Empty() {
		return
	}
	if radius <= 0 {
		dst := rectToPixelRect(rect).Intersect(imgBounds).Intersect(clipPix)
		if !dst.Empty() {
			fillRectBlend(img, dst, color)
		}
		return
	}
	maxR := rect.W / 2
	if rect.H/2 < maxR {
		maxR = rect.H / 2
	}
	if radius > maxR {
		radius = maxR
	}
	if radius <= 0.5 {
		dst := rectToPixelRect(rect).Intersect(imgBounds).Intersect(clipPix)
		if !dst.Empty() {
			fillRectBlend(img, dst, color)
		}
		return
	}

	rgba := toRGBA(color)
	rInt := int(radius + 0.5)
	left := int(rect.X)
	top := int(rect.Y)
	right := int(rect.X + rect.W)
	bottom := int(rect.Y + rect.H)

	center := image.Rect(left, top+rInt, right, bottom-rInt).Intersect(clipPix)
	if !center.Empty() {
		fillRectBlend(img, center, color)
	}
	topStrip := image.Rect(left+rInt, top, right-rInt, top+rInt).Intersect(clipPix)
	if !topStrip.Empty() {
		fillRectBlend(img, topStrip, color)
	}
	botStrip := image.Rect(left+rInt, bottom-rInt, right-rInt, bottom).Intersect(clipPix)
	if !botStrip.Empty() {
		fillRectBlend(img, botStrip, color)
	}

	tl := image.Rect(left, top, left+rInt, top+rInt).Intersect(clipPix)
	tr := image.Rect(right-rInt, top, right, top+rInt).Intersect(clipPix)
	bl := image.Rect(left, bottom-rInt, left+rInt, bottom).Intersect(clipPix)
	br := image.Rect(right-rInt, bottom-rInt, right, bottom).Intersect(clipPix)
	if !tl.Empty() {
		drawCornerAA(img, float32(left)+radius, float32(top)+radius, radius, tl.Min.X, tl.Min.Y, tl.Max.X, tl.Max.Y, rgba)
	}
	if !tr.Empty() {
		drawCornerAA(img, float32(right)-radius, float32(top)+radius, radius, tr.Min.X, tr.Min.Y, tr.Max.X, tr.Max.Y, rgba)
	}
	if !bl.Empty() {
		drawCornerAA(img, float32(left)+radius, float32(bottom)-radius, radius, bl.Min.X, bl.Min.Y, bl.Max.X, bl.Max.Y, rgba)
	}
	if !br.Empty() {
		drawCornerAA(img, float32(right)-radius, float32(bottom)-radius, radius, br.Min.X, br.Min.Y, br.Max.X, br.Max.Y, rgba)
	}
}

// StrokeRect paints a hollow rectangle outline.
func (b *cpuBackend) StrokeRect(rect Rect, clip Rect, color Color, width float32) {
	img := b.activeImg()
	if img == nil || width <= 0 {
		return
	}
	w := float32(int(width))
	clipInt := rectToPixelRect(clip)
	imgBounds := img.Bounds()
	paintEdge := func(r Rect) {
		dst := rectToPixelRect(r).Intersect(clipInt).Intersect(imgBounds)
		if !dst.Empty() {
			fillRectBlend(img, dst, color)
		}
	}
	paintEdge(Rect{X: rect.X, Y: rect.Y, W: rect.W, H: w})
	paintEdge(Rect{X: rect.X, Y: rect.Y + rect.H - w, W: rect.W, H: w})
	paintEdge(Rect{X: rect.X, Y: rect.Y, W: w, H: rect.H})
	paintEdge(Rect{X: rect.X + rect.W - w, Y: rect.Y, W: w, H: rect.H})
}

// StrokeRoundedRect paints an outlined rounded rectangle.
func (b *cpuBackend) StrokeRoundedRect(rect Rect, radius float32, clip Rect, color Color, width float32) {
	img := b.activeImg()
	if img == nil || width <= 0 {
		return
	}
	imgBounds := img.Bounds()
	clipPix := rectToPixelRect(clip).Intersect(imgBounds)
	if clipPix.Empty() {
		return
	}
	if radius <= 0 {
		b.StrokeRect(rect, clip, color, width)
		return
	}
	maxR := rect.W / 2
	if rect.H/2 < maxR {
		maxR = rect.H / 2
	}
	if radius > maxR {
		radius = maxR
	}
	if width > radius {
		b.FillRoundedRect(rect, radius, clip, color)
		return
	}

	rOuter := radius
	rInner := radius - width
	rInt := int(rOuter + 0.5)
	left := int(rect.X)
	top := int(rect.Y)
	right := int(rect.X + rect.W)
	bottom := int(rect.Y + rect.H)
	w := width
	rgba := toRGBA(color)

	clipEdge := func(r Rect) {
		dst := rectToPixelRect(r).Intersect(clipPix)
		if !dst.Empty() {
			fillRectBlend(img, dst, color)
		}
	}
	clipEdge(Rect{X: rect.X + radius, Y: rect.Y, W: rect.W - 2*radius, H: w})
	clipEdge(Rect{X: rect.X + radius, Y: rect.Y + rect.H - w, W: rect.W - 2*radius, H: w})
	clipEdge(Rect{X: rect.X, Y: rect.Y + radius, W: w, H: rect.H - 2*radius})
	clipEdge(Rect{X: rect.X + rect.W - w, Y: rect.Y + radius, W: w, H: rect.H - 2*radius})

	tl := image.Rect(left, top, left+rInt, top+rInt).Intersect(clipPix)
	tr := image.Rect(right-rInt, top, right, top+rInt).Intersect(clipPix)
	bl := image.Rect(left, bottom-rInt, left+rInt, bottom).Intersect(clipPix)
	br := image.Rect(right-rInt, bottom-rInt, right, bottom).Intersect(clipPix)
	if !tl.Empty() {
		drawCornerRingAA(img, float32(left)+radius, float32(top)+radius, rOuter, rInner, tl.Min.X, tl.Min.Y, tl.Max.X, tl.Max.Y, rgba)
	}
	if !tr.Empty() {
		drawCornerRingAA(img, float32(right)-radius, float32(top)+radius, rOuter, rInner, tr.Min.X, tr.Min.Y, tr.Max.X, tr.Max.Y, rgba)
	}
	if !bl.Empty() {
		drawCornerRingAA(img, float32(left)+radius, float32(bottom)-radius, rOuter, rInner, bl.Min.X, bl.Min.Y, bl.Max.X, bl.Max.Y, rgba)
	}
	if !br.Empty() {
		drawCornerRingAA(img, float32(right)-radius, float32(bottom)-radius, rOuter, rInner, br.Min.X, br.Min.Y, br.Max.X, br.Max.Y, rgba)
	}
}

// FillPath fills the interior of the flattened subpaths through
// fillPolygonAA (solid color or per-pixel shader). `invMatrix` maps
// physical → logical so shader sampling stays in the original coord
// space; consumed only when paint.Shader != nil.
func (b *cpuBackend) FillPath(subs [][]Point, clip image.Rectangle, paint Paint, invMatrix Matrix) {
	img := b.activeImg()
	if img == nil {
		return
	}
	pxClip := clip.Intersect(img.Bounds())
	if pxClip.Empty() {
		return
	}
	fillPolygon(img, pxClip, subs, paint.Color, paint.Shader, invMatrix, paint.FillRule, paint.AntiAlias)
}

// StrokePath rasterizes each subpath as a stroked polyline. Points are
// already in physical coords, so the transform passed to strokePolyline
// is identity.
func (b *cpuBackend) StrokePath(subs [][]Point, closedFlags []bool, clip image.Rectangle, width float32, paint Paint) {
	img := b.activeImg()
	if img == nil {
		return
	}
	pxClip := clip.Intersect(img.Bounds())
	if pxClip.Empty() {
		return
	}
	rgba := toRGBA(paint.Color)
	for i, sub := range subs {
		if len(sub) < 2 {
			continue
		}
		closed := false
		if i < len(closedFlags) {
			closed = closedFlags[i]
		}
		strokePolyline(img, pxClip, sub, IdentityMatrix(), width, rgba,
			paint.Cap, paint.Join, paint.MiterLimit, closed)
	}
}

// DrawLine paints a single straight-line segment.
func (b *cpuBackend) DrawLine(p1, p2 Point, clip Rect, color Color, width float32, cap StrokeCap) {
	img := b.activeImg()
	if img == nil {
		return
	}
	drawLineInto(img, rectToPixelRect(clip).Intersect(img.Bounds()), p1, p2, width, toRGBA(color), cap)
}

// DrawPolyline paints an open polyline using round caps by default.
func (b *cpuBackend) DrawPolyline(pts []Point, clip Rect, color Color, width float32) {
	img := b.activeImg()
	if img == nil || len(pts) < 2 {
		return
	}
	pxClip := rectToPixelRect(clip).Intersect(img.Bounds())
	if pxClip.Empty() {
		return
	}
	rgba := toRGBA(color)
	for i := 1; i < len(pts); i++ {
		drawLineInto(img, pxClip, pts[i-1], pts[i], width, rgba, CapRound)
	}
}

// DrawShadow projects elevation drop shadows via
// separable box blur on an alpha buffer, then composites tinted.
func (b *cpuBackend) DrawShadow(rect Rect, radius float32, spec ElevationSpec, shadowColor Color, clip Rect) {
	// Reuse the existing implementation attached to imageCanvas. The
	// image target argument is our active buffer.
	img := b.activeImg()
	if img == nil {
		return
	}
	drawShadowImplRaw(img, rect, radius, spec, shadowColor, rectToPixelRect(clip))
}

// DrawText renders `text` inside `rect`, clipped to `clip`. Uses the
// same clip-then-SubImage trick the pre-refactor drawTextClipped did.
func (b *cpuBackend) DrawText(text string, rect Rect, clip Rect, color Color, fontSpec Font) {
	img := b.activeImg()
	if img == nil || text == "" {
		return
	}
	imgRect := img.Bounds()
	clipInt := image.Rect(int(clip.X), int(clip.Y),
		int(clip.X+clip.W+0.999), int(clip.Y+clip.H+0.999)).Intersect(imgRect)
	if clipInt.Empty() {
		return
	}
	sub, ok := img.SubImage(clipInt).(*image.RGBA)
	if !ok {
		drawTextIntoRaw(img, text, rect, color, fontSpec)
		return
	}
	drawTextIntoRaw(sub, text, rect, color, fontSpec)
}

// DrawImage blits `img` into `dst` with bilinear resample.
func (b *cpuBackend) DrawImage(img image.Image, dst Rect, clip Rect) {
	target := b.activeImg()
	if target == nil || img == nil {
		return
	}
	dstPix := rectToPixelRect(dst).Intersect(target.Bounds())
	if dstPix.Empty() {
		return
	}
	clipped := dstPix.Intersect(rectToPixelRect(clip).Intersect(target.Bounds()))
	if clipped.Empty() {
		return
	}
	srcBounds := img.Bounds()
	scaleX := float32(srcBounds.Dx()) / float32(dstPix.Dx())
	scaleY := float32(srcBounds.Dy()) / float32(dstPix.Dy())
	srcRect := image.Rect(
		srcBounds.Min.X+int(float32(clipped.Min.X-dstPix.Min.X)*scaleX),
		srcBounds.Min.Y+int(float32(clipped.Min.Y-dstPix.Min.Y)*scaleY),
		srcBounds.Min.X+int(float32(clipped.Max.X-dstPix.Min.X)*scaleX),
		srcBounds.Min.Y+int(float32(clipped.Max.Y-dstPix.Min.Y)*scaleY),
	).Intersect(srcBounds)
	xdraw.ApproxBiLinear.Scale(target, clipped, img, srcRect, xdraw.Over, nil)
}

// DrawImageTransformed draws `img` through the affine `m` (image pixel
// space → physical device space), clipped to `clip`.
//
// Destination-driven inverse map: walk the device-space AABB of the
// transformed source rect, push each pixel center back through m⁻¹ and
// bilinearly sample. Two coverage classes keep quality and cost sane:
//
//   - interior pixels (the whole pixel footprint lands inside the source)
//     take ONE bilinear sample, so a 0°/90° blit stays as sharp as the
//     axis-aligned DrawImage path;
//   - pixels straddling the source edge take a subsampled coverage mask,
//     which is what antialiases the diagonal silhouette of rotated
//     content. Those are O(perimeter), not O(area).
//
// The classification is exact and costs nothing per pixel: because the
// inverse is affine, one destination pixel always maps to the same
// parallelogram in source space, so its half-extent is a constant.
func (b *cpuBackend) DrawImageTransformed(img image.Image, m Matrix, clip Rect) {
	target := b.activeImg()
	if target == nil || img == nil {
		return
	}
	src := img.Bounds()
	sw, sh := float32(src.Dx()), float32(src.Dy())
	if sw <= 0 || sh <= 0 {
		return
	}
	inv, ok := m.Invert()
	if !ok {
		// Degenerate transform (zero scale / collapsed axis) — nothing
		// with area to draw.
		return
	}
	area := rectToPixelRectOut(m.TransformRect(Rect{W: sw, H: sh})).
		Intersect(rectToPixelRectOut(clip)).
		Intersect(target.Bounds())
	if area.Empty() {
		return
	}
	// Half-extent, in source space, of one destination pixel: the inverse
	// maps the pixel's ±0.5 square to a parallelogram spanned by the
	// matrix columns.
	rx := 0.5 * (absf(inv.A) + absf(inv.B))
	ry := 0.5 * (absf(inv.C) + absf(inv.D))

	smp := newPremulSampler(img)
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			p := inv.TransformPoint(Point{X: float32(x) + 0.5, Y: float32(y) + 0.5})
			// Trivially outside — the pixel's whole footprint misses the source.
			if p.X+rx <= 0 || p.Y+ry <= 0 || p.X-rx >= sw || p.Y-ry >= sh {
				continue
			}
			cov := float32(1)
			if p.X-rx < 0 || p.Y-ry < 0 || p.X+rx > sw || p.Y+ry > sh {
				cov = transformedEdgeCoverage(inv, x, y, sw, sh)
				if cov <= 0 {
					continue
				}
			}
			cr, cg, cb, ca := smp.bilinear(p.X, p.Y)
			if ca == 0 {
				continue
			}
			if cov < 1 {
				cr, cg, cb, ca = cr*cov, cg*cov, cb*cov, ca*cov
			}
			blendPremulOver(target, x, y, cr, cg, cb, ca)
		}
	}
}

// transformedEdgeCoverage estimates what fraction of destination pixel
// (x, y) lands inside the source rect, by subsampling the pixel on an
// N×N grid. Only called for boundary pixels.
func transformedEdgeCoverage(inv Matrix, x, y int, sw, sh float32) float32 {
	const n = 4
	inside := 0
	for sy := 0; sy < n; sy++ {
		for sx := 0; sx < n; sx++ {
			p := inv.TransformPoint(Point{
				X: float32(x) + (float32(sx)+0.5)/n,
				Y: float32(y) + (float32(sy)+0.5)/n,
			})
			if p.X >= 0 && p.Y >= 0 && p.X < sw && p.Y < sh {
				inside++
			}
		}
	}
	return float32(inside) / (n * n)
}

// premulSampler reads an image as premultiplied 0–255 float channels.
// *image.RGBA (what every qui offscreen buffer is) takes a direct
// stride-indexed fast path; anything else falls back to the At/RGBA
// interface. Sampling coordinates are clamped to the edge, so bilinear
// taps near the silhouette never blend in transparent out-of-range
// texels — the silhouette itself comes from the coverage mask instead.
type premulSampler struct {
	rgba    *image.RGBA
	generic image.Image
	b       image.Rectangle
}

func newPremulSampler(img image.Image) premulSampler {
	s := premulSampler{generic: img, b: img.Bounds()}
	if r, ok := img.(*image.RGBA); ok {
		s.rgba = r
	}
	return s
}

// texel returns the premultiplied RGBA at integer source coordinates
// (relative to the image's Min), clamped to bounds.
func (s premulSampler) texel(ix, iy int) (r, g, b, a float32) {
	if ix < 0 {
		ix = 0
	}
	if iy < 0 {
		iy = 0
	}
	if w := s.b.Dx(); ix >= w {
		ix = w - 1
	}
	if h := s.b.Dy(); iy >= h {
		iy = h - 1
	}
	if s.rgba != nil {
		o := s.rgba.PixOffset(s.b.Min.X+ix, s.b.Min.Y+iy)
		p := s.rgba.Pix[o : o+4 : o+4]
		return float32(p[0]), float32(p[1]), float32(p[2]), float32(p[3])
	}
	cr, cg, cb, ca := s.generic.At(s.b.Min.X+ix, s.b.Min.Y+iy).RGBA()
	// RGBA() yields premultiplied 16-bit; scale to 0–255.
	return float32(cr) / 257, float32(cg) / 257, float32(cb) / 257, float32(ca) / 257
}

// bilinear samples at continuous source coordinates (u, v) measured in
// texels from the image's top-left, with texel centers at +0.5.
func (s premulSampler) bilinear(u, v float32) (r, g, b, a float32) {
	fu, fv := u-0.5, v-0.5
	x0 := int(mathFloor(fu))
	y0 := int(mathFloor(fv))
	tx := fu - float32(x0)
	ty := fv - float32(y0)

	r00, g00, b00, a00 := s.texel(x0, y0)
	r10, g10, b10, a10 := s.texel(x0+1, y0)
	r01, g01, b01, a01 := s.texel(x0, y0+1)
	r11, g11, b11, a11 := s.texel(x0+1, y0+1)

	lerp := func(p, q, t float32) float32 { return p + (q-p)*t }
	top := func(p, q float32) float32 { return lerp(p, q, tx) }
	return lerp(top(r00, r10), top(r01, r11), ty),
		lerp(top(g00, g10), top(g01, g11), ty),
		lerp(top(b00, b10), top(b01, b11), ty),
		lerp(top(a00, a10), top(a01, a11), ty)
}

// blendPremulOver composites a premultiplied source sample onto an
// *image.RGBA (also premultiplied) with the Porter-Duff Over operator.
func blendPremulOver(dst *image.RGBA, x, y int, sr, sg, sb, sa float32) {
	o := dst.PixOffset(x, y)
	p := dst.Pix[o : o+4 : o+4]
	ia := 1 - sa/255
	p[0] = clampChannel(sr + float32(p[0])*ia)
	p[1] = clampChannel(sg + float32(p[1])*ia)
	p[2] = clampChannel(sb + float32(p[2])*ia)
	p[3] = clampChannel(sa + float32(p[3])*ia)
}

func clampChannel(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func mathFloor(v float32) float32 { return float32(math.Floor(float64(v))) }

// rectToPixelRectOut is rectToPixelRect's outward-snapping sibling. A
// rotated footprint rarely lands on pixel boundaries, and truncating the
// max edge would shave the last row/column of a rotated blit.
func rectToPixelRectOut(r Rect) image.Rectangle {
	return image.Rect(
		int(math.Floor(float64(r.X))),
		int(math.Floor(float64(r.Y))),
		int(math.Ceil(float64(r.X+r.W))),
		int(math.Ceil(float64(r.Y+r.H))),
	)
}

// DrawVector rasterizes `src` at `dst`'s physical size then blits.
func (b *cpuBackend) DrawVector(src VectorSource, dst Rect, clip Rect, tint Color) {
	target := b.activeImg()
	if target == nil || src == nil {
		return
	}
	dstPix := rectToPixelRect(dst).Intersect(target.Bounds())
	if dstPix.Empty() {
		return
	}
	pxClip := rectToPixelRect(clip).Intersect(target.Bounds())
	clipped := dstPix.Intersect(pxClip)
	if clipped.Empty() {
		return
	}
	raster := src.Rasterize(dstPix.Dx(), dstPix.Dy(), tint)
	if raster == nil {
		return
	}
	offset := image.Point{
		X: raster.Bounds().Min.X + (clipped.Min.X - dstPix.Min.X),
		Y: raster.Bounds().Min.Y + (clipped.Min.Y - dstPix.Min.Y),
	}
	xdraw.Draw(target, clipped, raster, offset, xdraw.Over)
}

// rectToPixelRect quantizes a floating-point physical Rect into an
// image.Rectangle. Snaps outward at both ends so partial-coverage edge
// pixels aren't discarded.
func rectToPixelRect(r Rect) image.Rectangle {
	return image.Rect(int(r.X), int(r.Y), int(r.X+r.W), int(r.Y+r.H))
}

// drawShadowImplRaw is the extracted body of the pre-refactor
// imageCanvas.drawShadowImpl — pulled out so the CPU backend can call
// it without going through the old imageCanvas receiver.
func drawShadowImplRaw(img *image.RGBA, rect Rect, radius float32, spec ElevationSpec, shadowColor Color, clipPix image.Rectangle) {
	if img == nil || spec.Opacity <= 0 {
		return
	}
	alpha := shadowColor.A * spec.Opacity
	if alpha <= 0 {
		return
	}
	blur := spec.Blur
	if blur < 0 {
		blur = 0
	}
	shadowRect := Rect{
		X: rect.X + spec.X - spec.Spread,
		Y: rect.Y + spec.Y - spec.Spread,
		W: rect.W + 2*spec.Spread,
		H: rect.H + 2*spec.Spread,
	}
	shadowRadius := radius + spec.Spread
	if shadowRadius < 0 {
		shadowRadius = 0
	}
	if shadowRect.W <= 0 || shadowRect.H <= 0 {
		return
	}

	margin := int(math.Ceil(float64(blur))) + 2
	bufW := int(math.Ceil(float64(shadowRect.W))) + 2*margin
	bufH := int(math.Ceil(float64(shadowRect.H))) + 2*margin
	if bufW <= 0 || bufH <= 0 {
		return
	}

	srcAlpha := make([]uint8, bufW*bufH)
	stampRoundedAlpha(srcAlpha, bufW, bufH, margin, shadowRect.W, shadowRect.H, shadowRadius)

	dstAlpha := make([]uint8, bufW*bufH)
	if blur > 0 {
		sigma := float64(blur) / 2.0
		boxR := int(math.Round((math.Sqrt(1+3*sigma*sigma) - 1) / 2))
		if boxR < 1 {
			boxR = 1
		}
		boxBlurH(srcAlpha, dstAlpha, bufW, bufH, boxR)
		boxBlurV(dstAlpha, srcAlpha, bufW, bufH, boxR)
		boxBlurH(srcAlpha, dstAlpha, bufW, bufH, boxR)
		boxBlurV(dstAlpha, srcAlpha, bufW, bufH, boxR)
	}

	imgBounds := img.Bounds()
	effBounds := imgBounds.Intersect(clipPix)
	if effBounds.Empty() {
		return
	}
	dstX0 := int(math.Round(float64(shadowRect.X) - float64(margin)))
	dstY0 := int(math.Round(float64(shadowRect.Y) - float64(margin)))

	r := uint32(shadowColor.R * 255)
	g := uint32(shadowColor.G * 255)
	bch := uint32(shadowColor.B * 255)

	for by := 0; by < bufH; by++ {
		dy := dstY0 + by
		if dy < effBounds.Min.Y || dy >= effBounds.Max.Y {
			continue
		}
		row := srcAlpha[by*bufW : (by+1)*bufW]
		for bx := 0; bx < bufW; bx++ {
			a := row[bx]
			if a == 0 {
				continue
			}
			dx := dstX0 + bx
			if dx < effBounds.Min.X || dx >= effBounds.Max.X {
				continue
			}
			pixA := uint32(float32(a) * alpha)
			if pixA == 0 {
				continue
			}
			idx := img.PixOffset(dx, dy)
			pix := img.Pix[idx : idx+4 : idx+4]
			invA := 255 - pixA
			pix[0] = uint8((r*pixA + uint32(pix[0])*invA) / 255)
			pix[1] = uint8((g*pixA + uint32(pix[1])*invA) / 255)
			pix[2] = uint8((bch*pixA + uint32(pix[2])*invA) / 255)
			outA := pixA + uint32(pix[3])*invA/255
			if outA > 255 {
				outA = 255
			}
			pix[3] = uint8(outA)
		}
	}
}

// drawTextIntoRaw is the extracted body of imageCanvas.drawTextInto.
// Uses the shared font stack / emoji fallback / weight resolution.
func drawTextIntoRaw(dst *image.RGBA, text string, rect Rect, col Color, fontSpec Font) {
	if dst == nil || text == "" {
		return
	}
	face := GetFontFaceFor(fontSpec)
	faces := GetFontFacesFor(fontSpec)
	metrics := face.Metrics()
	drawer := font.Drawer{
		Dst:  dst,
		Src:  cachedUniform(toPremultipliedRGBA(col)),
		Face: face,
	}
	lineHeight := metrics.Height.Ceil()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	capHeight := metrics.CapHeight.Ceil()
	if capHeight <= 0 {
		capHeight = ascent - descent
	}
	lines := splitLines(text)
	x0 := int(rect.X)

	// Single-line: optically center on the CAP-HEIGHT box (baseline up to
	// cap height) instead of the ascent+descent ink block. Fonts reserve
	// internal leading above cap height, so ink-block centering — baseline
	// at rect.Y+ascent — drifts toward the box bottom in short/tight boxes
	// (buttons, inputs), and glyphs that don't use the full box (symbols
	// like × + −, digits, capitals) read as sitting low. Cap-height
	// centering is metric-independent and matches the emoji path
	// (drawTextLineRaw's capCenter).
	if len(lines) == 1 {
		baseline := int(rect.Y + (rect.H+float32(capHeight))/2)
		drawTextLineRaw(dst, trimTrailingCR(lines[0]), &drawer, fontSpec, faces, x0, baseline, ascent, capHeight, lineHeight)
		return
	}

	// Multi-line: center the whole ink block (or top-flow when it overflows).
	totalInk := (len(lines)-1)*lineHeight + ascent + descent
	extraY := int(rect.H) - totalInk
	if extraY < 0 {
		extraY = 0
	} else {
		extraY /= 2
	}
	y := int(rect.Y) + ascent + extraY
	for _, line := range lines {
		line = trimTrailingCR(line)
		drawTextLineRaw(dst, line, &drawer, fontSpec, faces, x0, y, ascent, capHeight, lineHeight)
		y += lineHeight
	}
}

func splitLines(s string) []string {
	// Same logic as the previous imageCanvas.drawTextInto — strings.Split
	// on "\n" then strip \r suffix in the caller.
	if len(s) == 0 {
		return []string{s}
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimTrailingCR(s string) string {
	if n := len(s); n > 0 && s[n-1] == '\r' {
		return s[:n-1]
	}
	return s
}

// Ensure the imports we use survive godoc-tools' unused-import checks.
var (
	_ = color.RGBA{}
	_ = stddraw.Src
)
