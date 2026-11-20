package render

import (
	"image"
	"sync/atomic"

	"github.com/qizhanchan/qui/internal/gfx"
)

// Picture is a recorded, immutable list of raster operations — qui's
// display list (Skia's SkPicture, Blink's PaintRecord). It is produced by
// drawing through a canvas from NewPictureCanvas and consumed by Playback,
// any number of times, onto any RasterBackend, at an integer device
// offset.
//
// Recording happens BELOW the canvas frontend: the frontend still owns
// the matrix and clip stack, shapes and lays out text, flattens paths,
// rasterizes glyph masks (through the shared glyph cache) and ClipPath
// masks, and decides which primitive a draw becomes. What is recorded is
// the RasterBackend call stream, in physical pixels. So:
//
//   - every Canvas capability the frontend has (SaveLayer, ClipPath,
//     shaped-line drawing, BackdropFilter, rotated text and images, …)
//     records with no extra code, and replays exactly as it would have
//     rasterized;
//   - playback touches no widget, font or layout state: it is pure pixel
//     work over data the picture owns or shares immutably, which is what
//     lets a worker thread replay it (raster_pool.go).
//
// A picture is recorded at one device scale. Playback can shift it by
// whole device pixels (a tile of a larger picture) but not rescale it —
// a new scale is a new recording, as in Chromium when a layer's raster
// scale changes.
//
// Data a picture references: images passed to DrawImage* are shared, not
// copied, and must not be mutated afterwards (decoded images, cached
// scaled copies and glyph bitmaps never are). Buffers the frontend
// reuses (coverage scratch, flattened paths) are copied at record time.
type Picture struct {
	bounds image.Rectangle
	ops    []picOp
	bytes  int
	// mainOnly is set when an op needs main-thread state at playback
	// (legacy string text drawing, which shapes through font faces that
	// are not safe for concurrent use).
	mainOnly bool
	// filters is set when a layer runs an ImageFilter: its output depends
	// on pixels beyond the op's own clip, so a tile must replay with
	// margin (see PlaybackTile).
	filters bool
	// filterRects are the device rectangles of those layers (and of
	// backdrop filters).
	filterRects []image.Rectangle
}

type picKind uint8

const (
	picDraw picKind = iota
	picPush
	picPop
)

// picOp is one recorded backend call. r is the device rectangle the op
// can touch (its clip, conservatively): playback skips ops whose r misses
// the target, and a skipped push skips everything up to its pop.
type picOp struct {
	kind picKind
	r    image.Rectangle
	run  func(b RasterBackend, o picOffset)
}

// picOffset moves recorded device coordinates onto a playback target and
// clips them to it.
type picOffset struct {
	dx, dy int
	fx, fy float32
	target image.Rectangle // the playback target, in target coordinates
	tf     gfx.Rect
}

func newPicOffset(origin image.Point, target image.Rectangle) picOffset {
	return picOffset{
		dx: -origin.X, dy: -origin.Y,
		fx: float32(-origin.X), fy: float32(-origin.Y),
		target: target,
		tf:     pixelRectToRect(target),
	}
}

func (o picOffset) rect(r gfx.Rect) gfx.Rect {
	r.X += o.fx
	r.Y += o.fy
	return r
}

// clip moves a recorded clip and narrows it to the target.
func (o picOffset) clip(r gfx.Rect) gfx.Rect { return o.rect(r).Intersect(o.tf) }

func (o picOffset) irect(r image.Rectangle) image.Rectangle {
	return r.Add(image.Pt(o.dx, o.dy))
}

func (o picOffset) iclip(r image.Rectangle) image.Rectangle {
	return o.irect(r).Intersect(o.target)
}

func (o picOffset) point(p gfx.Point) gfx.Point { return gfx.Point{X: p.X + o.fx, Y: p.Y + o.fy} }

func (o picOffset) zero() bool { return o.dx == 0 && o.dy == 0 }

// Bounds is the device rectangle the picture was recorded for.
func (p *Picture) Bounds() image.Rectangle {
	if p == nil {
		return image.Rectangle{}
	}
	return p.bounds
}

// Len is the number of recorded operations.
func (p *Picture) Len() int {
	if p == nil {
		return 0
	}
	return len(p.ops)
}

// Bytes estimates the memory the picture holds beyond shared images.
func (p *Picture) Bytes() int {
	if p == nil {
		return 0
	}
	return p.bytes
}

// Empty reports whether playing the picture draws nothing.
func (p *Picture) Empty() bool { return p == nil || len(p.ops) == 0 }

// WorkerSafe reports whether the picture can be played on a thread other
// than the UI thread.
func (p *Picture) WorkerSafe() bool { return p != nil && !p.mainOnly }

// HasFilters reports whether a layer in the picture runs an ImageFilter.
func (p *Picture) HasFilters() bool { return p != nil && p.filters }

// FiltersNear reports whether a filtered layer or backdrop filter of the
// picture lies within pad pixels of r: playing r alone would cut what the
// filter reads.
func (p *Picture) FiltersNear(r image.Rectangle, pad int) bool {
	if p == nil {
		return false
	}
	g := r.Inset(-pad)
	for _, f := range p.filterRects {
		if f.Overlaps(g) {
			return true
		}
	}
	return false
}

// Playback replays the picture onto b. origin is the device point of the
// recording that lands on the target's (0,0); target is the target's
// pixel rectangle (in its own coordinates), which every op is clipped to.
func (p *Picture) Playback(b RasterBackend, origin image.Point, target image.Rectangle) {
	if p == nil {
		return
	}
	o := newPicOffset(origin, target)
	recBox := p.bounds.Sub(origin).Intersect(target)
	if recBox.Empty() {
		return
	}
	skip := 0 // >0: inside a culled layer
	for i := range p.ops {
		op := &p.ops[i]
		if skip > 0 {
			switch op.kind {
			case picPush:
				skip++
			case picPop:
				skip--
			}
			continue
		}
		if op.kind != picPop && !o.irect(op.r).Overlaps(target) {
			if op.kind == picPush {
				skip = 1
			}
			continue
		}
		op.run(b, o)
	}
	picStats.played.Add(1)
}

// Rasterize plays the picture into a new transparent RGBA covering r
// (device coordinates of the recording); the image's own rectangle
// starts at (0,0).
func (p *Picture) Rasterize(r image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	p.RasterizeInto(img, r.Min)
	return img
}

// RasterizeInto plays the picture over img (which keeps its pixels: clear
// it first for a fresh raster) with origin landing on img's (0,0).
func (p *Picture) RasterizeInto(img *image.RGBA, origin image.Point) {
	if p == nil || img == nil {
		return
	}
	b := newCPUBackend(img)
	b.Begin(gfx.Size{})
	p.Playback(b, origin, img.Rect)
	b.End()
}

var picStats struct{ recorded, played atomic.Int64 }

// PictureStats reports how many pictures were recorded and played since
// start (QUI_DEBUG_GPU_LAYERS and tests read it).
func PictureStats() (recorded, played int64) {
	return picStats.recorded.Load(), picStats.played.Load()
}
