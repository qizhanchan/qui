package render

import (
	"image"

	"github.com/qizhanchan/qui/internal/gfx"
)

// PictureRecorder is the RasterBackend behind a recording canvas: every
// call becomes a picOp.
type PictureRecorder struct {
	pic   *Picture
	depth int
}

// NewPictureCanvas returns a canvas that records into a Picture covering
// bounds (device pixels; the canvas's initial clip), and the recorder to
// finish it with. The canvas is a full ImageCanvas frontend: everything a
// raster canvas offers, it offers.
func NewPictureCanvas(bounds image.Rectangle) (gfx.Canvas, *PictureRecorder) {
	rec := &PictureRecorder{pic: &Picture{bounds: bounds}}
	return imageCanvas{
		CanvasState: gfx.NewCanvasState(pixelRectToRect(bounds)),
		Backend:     rec,
		Layers:      newFrontendLayerTracker(),
	}, rec
}

// Finish returns the recorded picture. The recorder must not be drawn
// into afterwards.
func (r *PictureRecorder) Finish() *Picture {
	p := r.pic
	// A layer left open (an unbalanced Save in a widget) is closed the way
	// a raster target would leave it: composited at End.
	for ; r.depth > 0; r.depth-- {
		r.PopLayer(nil)
	}
	r.pic = nil
	picStats.recorded.Add(1)
	return p
}

func (r *PictureRecorder) add(kind picKind, bounds image.Rectangle, bytes int, run func(RasterBackend, picOffset)) {
	if r.pic == nil {
		return
	}
	r.pic.ops = append(r.pic.ops, picOp{kind: kind, r: bounds, run: run})
	r.pic.bytes += bytes + 48
}

func (r *PictureRecorder) draw(clip gfx.Rect, bytes int, run func(RasterBackend, picOffset)) {
	b := outPix(clip)
	if b.Empty() {
		return
	}
	r.add(picDraw, b, bytes, run)
}

func (r *PictureRecorder) all() image.Rectangle {
	if r.pic == nil {
		return image.Rectangle{}
	}
	return r.pic.bounds.Inset(-1 << 20)
}

func (r *PictureRecorder) Begin(gfx.Size)        {}
func (r *PictureRecorder) End()                  {}
func (r *PictureRecorder) CPUImage() *image.RGBA { return nil }

func (r *PictureRecorder) Clear(color gfx.Color) {
	r.add(picDraw, r.all(), 0, func(b RasterBackend, o picOffset) { b.Clear(color) })
}

func (r *PictureRecorder) ClearRect(dst image.Rectangle) {
	if dst.Empty() {
		return
	}
	r.add(picDraw, dst, 0, func(b RasterBackend, o picOffset) {
		if d := o.iclip(dst); !d.Empty() {
			b.ClearRect(d)
		}
	})
}

func (r *PictureRecorder) FillRect(dst image.Rectangle, color gfx.Color) {
	if dst.Empty() {
		return
	}
	r.add(picDraw, dst, 0, func(b RasterBackend, o picOffset) {
		if d := o.iclip(dst); !d.Empty() {
			b.FillRect(d, color)
		}
	})
}

func (r *PictureRecorder) FillRoundedRect(rect gfx.Rect, radius float32, clip gfx.Rect, color gfx.Color) {
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.FillRoundedRect(o.rect(rect), radius, o.clip(clip), color)
	})
}

func (r *PictureRecorder) StrokeRect(rect gfx.Rect, clip gfx.Rect, color gfx.Color, width float32) {
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.StrokeRect(o.rect(rect), o.clip(clip), color, width)
	})
}

func (r *PictureRecorder) StrokeRoundedRect(rect gfx.Rect, radius float32, clip gfx.Rect, color gfx.Color, width float32) {
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.StrokeRoundedRect(o.rect(rect), radius, o.clip(clip), color, width)
	})
}

func copySubs(subs [][]gfx.Point) ([][]gfx.Point, int) {
	n := 0
	for _, s := range subs {
		n += len(s)
	}
	flat := make([]gfx.Point, 0, n)
	out := make([][]gfx.Point, len(subs))
	for i, s := range subs {
		start := len(flat)
		flat = append(flat, s...)
		out[i] = flat[start:len(flat):len(flat)]
	}
	return out, n * 8
}

func offsetSubs(subs [][]gfx.Point, o picOffset) [][]gfx.Point {
	if o.zero() {
		return subs
	}
	out, _ := copySubs(subs)
	for _, s := range out {
		for i := range s {
			s[i] = o.point(s[i])
		}
	}
	return out
}

func (r *PictureRecorder) FillPath(subs [][]gfx.Point, clip image.Rectangle, paint gfx.Paint, invMatrix gfx.Matrix) {
	if clip.Empty() {
		return
	}
	own, n := copySubs(subs)
	r.add(picDraw, clip, n, func(b RasterBackend, o picOffset) {
		inv, pt := invMatrix, paint
		if !o.zero() && paint.Shader != nil {
			// The device moved by (dx, dy). Undo the move exactly (whole
			// pixels) and sample through the recorded inverse, so a tile's
			// gradient is bit-identical to the whole picture's.
			inv = gfx.TranslateMatrix(-o.fx, -o.fy)
			pt.Shader = movedShader{paint.Shader, invMatrix}
		}
		if c := o.iclip(clip); !c.Empty() {
			b.FillPath(offsetSubs(own, o), c, pt, inv)
		}
	})
}

func (r *PictureRecorder) StrokePath(subs [][]gfx.Point, closedFlags []bool, clip image.Rectangle, width float32, paint gfx.Paint) {
	if clip.Empty() {
		return
	}
	own, n := copySubs(subs)
	flags := append([]bool(nil), closedFlags...)
	r.add(picDraw, clip, n, func(b RasterBackend, o picOffset) {
		if c := o.iclip(clip); !c.Empty() {
			b.StrokePath(offsetSubs(own, o), flags, c, width, paint)
		}
	})
}

func (r *PictureRecorder) DrawLine(p1, p2 gfx.Point, clip gfx.Rect, color gfx.Color, width float32, cap gfx.StrokeCap) {
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.DrawLine(o.point(p1), o.point(p2), o.clip(clip), color, width, cap)
	})
}

func (r *PictureRecorder) DrawPolyline(pts []gfx.Point, clip gfx.Rect, color gfx.Color, width float32) {
	own := append([]gfx.Point(nil), pts...)
	r.draw(clip, len(own)*8, func(b RasterBackend, o picOffset) {
		p := own
		if !o.zero() {
			p = make([]gfx.Point, len(own))
			for i, q := range own {
				p[i] = o.point(q)
			}
		}
		b.DrawPolyline(p, o.clip(clip), color, width)
	})
}

func (r *PictureRecorder) DrawShadow(rect gfx.Rect, radius float32, spec gfx.ElevationSpec, shadowColor gfx.Color, clip gfx.Rect) {
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.DrawShadow(o.rect(rect), radius, spec, shadowColor, o.clip(clip))
	})
}

// DrawText (a string, shaped at raster time) is the legacy text route:
// it reads font faces that only the UI thread may use, so a picture that
// holds one plays on the UI thread.
func (r *PictureRecorder) DrawText(text string, rect gfx.Rect, clip gfx.Rect, color gfx.Color, fontSpec gfx.Font) {
	if r.pic != nil {
		r.pic.mainOnly = true
	}
	r.draw(clip, len(text), func(b RasterBackend, o picOffset) {
		b.DrawText(text, o.rect(rect), o.clip(clip), color, fontSpec)
	})
}

func (r *PictureRecorder) DrawImage(img image.Image, dst gfx.Rect, clip gfx.Rect) {
	if img == nil {
		return
	}
	img = gfx.StableImage(img)
	r.draw(clip.Intersect(dst.Inset(gfx.Insets{Top: -1, Right: -1, Bottom: -1, Left: -1})), 0, func(b RasterBackend, o picOffset) {
		b.DrawImage(img, o.rect(dst), o.clip(clip))
	})
}

func (r *PictureRecorder) DrawImageTransformed(img image.Image, m gfx.Matrix, clip gfx.Rect) {
	if img == nil {
		return
	}
	img = gfx.StableImage(img)
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		mm := m
		if !o.zero() {
			mm = gfx.TranslateMatrix(o.fx, o.fy).Concat(m)
		}
		b.DrawImageTransformed(img, mm, o.clip(clip))
	})
}

// movedShader samples s through the paint-space inverse recorded with it.
type movedShader struct {
	s   gfx.Shader
	inv gfx.Matrix
}

func (m movedShader) ColorAt(x, y float32) gfx.Color {
	p := m.inv.TransformPoint(gfx.Point{X: x, Y: y})
	return m.s.ColorAt(p.X, p.Y)
}

// fixedVector replays a vector source's raster from record time, so the
// source (and its caches) is never touched off the UI thread.
type fixedVector struct{ img image.Image }

func (f fixedVector) Rasterize(int, int, gfx.Color) image.Image { return f.img }

func (r *PictureRecorder) DrawVector(src gfx.VectorSource, dst gfx.Rect, clip gfx.Rect, tint gfx.Color) {
	if src == nil {
		return
	}
	dstPix := rectToPixelRect(dst)
	if dstPix.Empty() || rectToPixelRect(clip).Intersect(dstPix).Empty() {
		return
	}
	raster := src.Rasterize(dstPix.Dx(), dstPix.Dy(), tint)
	if raster == nil {
		return
	}
	fv := fixedVector{raster}
	r.draw(clip, 0, func(b RasterBackend, o picOffset) {
		b.DrawVector(fv, o.rect(dst), o.clip(clip), tint)
	})
}

func (r *PictureRecorder) PushLayer(physBounds, clip image.Rectangle, paint gfx.Paint) {
	r.depth++
	if paint.ImageFilter != nil && r.pic != nil {
		r.pic.filters = true
		r.pic.filterRects = append(r.pic.filterRects, physBounds)
	}
	r.add(picPush, clip, 0, func(b RasterBackend, o picOffset) {
		b.PushLayer(o.irect(physBounds), o.iclip(clip), paint)
	})
}

func (r *PictureRecorder) PopLayer(mask *image.Alpha) {
	if r.depth == 0 {
		return
	}
	r.depth--
	n := 0
	if mask != nil {
		n = len(mask.Pix)
	}
	r.add(picPop, image.Rectangle{}, n, func(b RasterBackend, o picOffset) {
		m := mask
		if m != nil && !o.zero() {
			mm := *m
			mm.Rect = o.irect(m.Rect)
			m = &mm
		}
		b.PopLayer(m)
	})
}

// pathMask is a ClipPath mask kept as its device path: playback
// rasterizes it (rasterizePathToAlpha) where the picture is played.
type pathMask struct {
	subs [][]gfx.Point
	rect image.Rectangle
}

func (r *PictureRecorder) maskSpec(subs [][]gfx.Point, rect image.Rectangle) *pathMask {
	own, _ := copySubs(subs)
	return &pathMask{subs: own, rect: rect}
}

// popLayerPathMask is PopLayer with a mask rasterized at playback.
func (r *PictureRecorder) popLayerPathMask(pm *pathMask) {
	if r.depth == 0 {
		return
	}
	r.depth--
	n := 0
	for _, s := range pm.subs {
		n += len(s) * 8
	}
	r.add(picPop, image.Rectangle{}, n, func(b RasterBackend, o picOffset) {
		// Only the part of the mask over the target matters: the layer it
		// masks is no larger.
		b.PopLayer(rasterizePathToAlpha(offsetSubs(pm.subs, o), o.iclip(pm.rect)))
	})
}

// blendCoverage records a glyph line's summed coverage (glyphMaskBlender),
// cropped to what the clip lets through.
func (r *PictureRecorder) blendCoverage(cov []float32, bounds, clip image.Rectangle, col gfx.Color) {
	vis := bounds.Intersect(clip)
	if vis.Empty() {
		return
	}
	w := bounds.Dx()
	own := make([]float32, vis.Dx()*vis.Dy())
	for y := vis.Min.Y; y < vis.Max.Y; y++ {
		src := cov[(y-bounds.Min.Y)*w+vis.Min.X-bounds.Min.X:]
		copy(own[(y-vis.Min.Y)*vis.Dx():(y-vis.Min.Y+1)*vis.Dx()], src[:vis.Dx()])
	}
	r.add(picDraw, vis, len(own)*4, func(b RasterBackend, o picOffset) {
		bl, ok := b.(glyphMaskBlender)
		if !ok {
			return
		}
		if c := o.iclip(vis); !c.Empty() {
			bl.blendCoverage(own, o.irect(vis), c, col)
		}
	})
}

// backdropFilter records a backdrop-filter region (backdrop.go).
func (r *PictureRecorder) backdropFilter(region image.Rectangle, filter gfx.ImageFilter, mask *image.Alpha) {
	if region.Empty() {
		return
	}
	if r.pic != nil {
		r.pic.filters = true
		r.pic.filterRects = append(r.pic.filterRects, region)
	}
	r.add(picDraw, region, 0, func(b RasterBackend, o picOffset) {
		bf, ok := b.(backdropFilterer)
		if !ok {
			return
		}
		m := mask
		if m != nil && !o.zero() {
			mm := *m
			mm.Rect = o.irect(m.Rect)
			m = &mm
		}
		bf.backdropFilter(o.iclip(region), filter, m)
	})
}

var (
	_ RasterBackend    = (*PictureRecorder)(nil)
	_ glyphMaskBlender = (*PictureRecorder)(nil)
	_ backdropFilterer = (*PictureRecorder)(nil)
	_ backdropFilterer = (*CPUBackend)(nil)
)
