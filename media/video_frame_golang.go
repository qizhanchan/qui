//go:build cgo

package media

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/media/internal/h264"
	"github.com/qizhanchan/qui/media/internal/yuv"
)

var frameGen atomic.Uint64

// golangVideoFrame is the pure-Go backend's VideoFrame: a decoded I420
// picture. Composite uploads the planes and converts colour on the GPU;
// RGBA converts on the CPU for other consumers.
type golangVideoFrame struct {
	pts       time.Duration
	frame     *h264.Frame
	matrix    int
	fullRange bool
	w, h      int
	gen       uint64
	refs      atomic.Int32
}

func newGolangVideoFrame(pts time.Duration, f *h264.Frame) *golangVideoFrame {
	v := &golangVideoFrame{pts: pts, frame: f, matrix: f.Matrix, fullRange: f.FullRange, w: f.Width, h: f.Height, gen: frameGen.Add(1)}
	v.refs.Store(1)
	return v
}

func (f *golangVideoFrame) PTS() time.Duration { return f.pts }
func (f *golangVideoFrame) Width() int         { return f.w }
func (f *golangVideoFrame) Height() int        { return f.h }

// RGBA converts the frame to packed RGBA on the CPU.
func (f *golangVideoFrame) RGBA() []byte {
	fr := f.frame
	if fr == nil {
		return nil
	}
	out := make([]byte, fr.Width*fr.Height*4)
	cs := yuv.BT601
	if f.matrix == 1 || (f.matrix != 5 && f.matrix != 6 && fr.Height >= 720) {
		cs = yuv.BT709
	}
	yuv.I420ToRGBAColorRange(out, fr.Y.Pix, fr.Cb.Pix, fr.Cr.Pix, fr.Width, fr.Height, fr.Y.Stride, fr.Cb.Stride, cs, f.fullRange)
	return out
}

func (f *golangVideoFrame) Retain() { f.refs.Add(1) }

// Composite implements VideoFrame: the I420 planes are uploaded and
// converted in the compositor's shader.
func (f *golangVideoFrame) Composite(c qui.Compositor, key any, src, physDst qui.Rect) bool {
	fr := f.frame
	if fr == nil || fr.Y == nil || fr.Cb == nil || fr.Cr == nil {
		return false
	}
	plane := func(p *h264.Plane) qui.YUVPlane {
		if len(p.Pix) == 0 {
			return qui.YUVPlane{}
		}
		return qui.YUVPlane{Pix: unsafe.Pointer(&p.Pix[0]), Stride: p.Stride, W: p.W, H: p.H}
	}
	m, off, sc := colorMatrix(f.matrix, f.fullRange, fr.Height)
	return c.DrawYUV420(key, f.gen, [3]qui.YUVPlane{plane(fr.Y), plane(fr.Cb), plane(fr.Cr)}, m, off, sc, src, physDst)
}

func (f *golangVideoFrame) Release() {
	if f.refs.Add(-1) > 0 {
		return
	}
	h264.Recycle(f.frame)
	f.frame = nil
}
