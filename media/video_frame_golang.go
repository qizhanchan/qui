//go:build cgo

package media

import (
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/media/internal/h264"
	"github.com/qizhanchan/qui/media/internal/yuv"
)

var frameGen atomic.Uint64

// golangVideoFrame is the pure-Go backend's VideoFrame: a decoded I420
// picture. VideoView draws it through DrawIntoFramebuffer, which uploads the
// planes and converts colour on the GPU; GLTexture lazily builds an RGBA
// texture for other consumers.
type golangVideoFrame struct {
	pts       time.Duration
	frame     *h264.Frame
	matrix    int
	fullRange bool
	w, h      int
	gen       uint64
	refs      atomic.Int32

	rgbaTex uint32 // lazily created by GLTexture
}

func newGolangVideoFrame(pts time.Duration, f *h264.Frame) *golangVideoFrame {
	v := &golangVideoFrame{pts: pts, frame: f, matrix: f.Matrix, fullRange: f.FullRange, w: f.Width, h: f.Height, gen: frameGen.Add(1)}
	v.refs.Store(1)
	return v
}

func (f *golangVideoFrame) PTS() time.Duration { return f.pts }
func (f *golangVideoFrame) Width() int         { return f.w }
func (f *golangVideoFrame) Height() int        { return f.h }

// DrawIntoFramebuffer implements framebufferDrawer.
func (f *golangVideoFrame) DrawIntoFramebuffer(state qui.GLState, src, physDst qui.Rect, flipY bool) bool {
	return gYUV.draw(f, src, physDst, state.FramebufferSize, flipY)
}

// GLTexture returns an RGBA GL_TEXTURE_2D of the frame, converting on the
// CPU on first use. Must be called with the GL context current.
func (f *golangVideoFrame) GLTexture() uint32 {
	drainTexDeletes()
	if f.rgbaTex != 0 {
		return f.rgbaTex
	}
	rgba := f.RGBA()
	if rgba == nil {
		return 0
	}
	gl.GenTextures(1, &f.rgbaTex)
	gl.BindTexture(gl.TEXTURE_2D, f.rgbaTex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, int32(f.Width()), int32(f.Height()), 0, gl.RGBA, gl.UNSIGNED_BYTE, unsafe.Pointer(&rgba[0]))
	gl.BindTexture(gl.TEXTURE_2D, 0)
	return f.rgbaTex
}

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

// drawMetal implements metalVideoFrame: the I420 planes are uploaded and
// converted in the compositor's shader with the same coefficients as the
// GL path.
func (f *golangVideoFrame) drawMetal(c qui.MetalCompositor, key any, src, physDst qui.Rect) bool {
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
	return c.DrawYUV420(key, [3]qui.YUVPlane{plane(fr.Y), plane(fr.Cb), plane(fr.Cr)}, m, off, sc, src, physDst)
}

func (f *golangVideoFrame) Release() {
	if f.refs.Add(-1) > 0 {
		return
	}
	if f.rgbaTex != 0 {
		queueTexDelete(f.rgbaTex)
		f.rgbaTex = 0
	}
	h264.Recycle(f.frame)
	f.frame = nil
}
