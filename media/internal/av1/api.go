package av1

import (
	"fmt"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// Frame is an output picture: 8-bit I420 from the shared frame pool (10-bit
// streams are rounded to 8 bits, monochrome ones get neutral chroma).
type Frame = h264.Frame

// SetTag sets the value carried by the frames that the next Decode
// outputs (e.g. the sample index of the temporal unit).
func (d *Decoder) SetTag(tag int64) { d.tag = tag }

// Decode decodes one temporal unit (an MP4 sample, an IVF frame or a run
// of OBUs such as av1C's configOBUs) and queues the frames it shows. A
// stream that would make the decoder fail internally returns ErrInvalid
// rather than panicking.
func (d *Decoder) Decode(tu []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			d.seenFrameHeader, d.fs = false, nil
			err = fmt.Errorf("%w: %v", ErrInvalid, r)
		}
		for _, f := range d.out {
			d.ready = append(d.ready, d.toFrame(f))
		}
		clear(d.out)
		d.out = d.out[:0]
	}()
	return d.DecodeTemporalUnit(tu)
}

// Next returns the next output frame, or nil.
func (d *Decoder) Next() *Frame {
	if len(d.ready) == 0 {
		return nil
	}
	f := d.ready[0]
	d.ready[0] = nil
	d.ready = d.ready[1:]
	return f
}

// Flush is a no-op: AV1 frames are output in decode order as they are
// shown, so nothing is held back.
func (d *Decoder) Flush() {}

// toFrame converts a shown frame to the output form.
func (d *Decoder) toFrame(f *frame) *Frame {
	y := &f.planes[0]
	w, h := y.w, y.h
	o := h264.NewFrame(w, h)
	o.Width, o.Height = w, h
	o.Tag = d.tag
	o.POC = 0
	o.KeyFrame = f.frameType == keyFrame
	o.FullRange, o.Matrix = d.seq.FullRange, d.seq.MatrixCoeffs
	dst := [3]*h264.Plane{o.Y, o.Cb, o.Cr}
	for p := 0; p < 3; p++ {
		dp := dst[p]
		if p >= f.numPlanes {
			for i := range dp.Pix[:dp.Stride*dp.H] {
				dp.Pix[i] = 128
			}
			continue
		}
		sp := &f.planes[p]
		for r := 0; r < dp.H; r++ {
			out := dp.Pix[r*dp.Stride : r*dp.Stride+dp.W]
			if sp.pix16 != nil {
				sh := uint(f.bitDepth - 8)
				in := sp.pix16[r*sp.stride : r*sp.stride+dp.W]
				for i, v := range in {
					out[i] = uint8(min((v+1<<(sh-1))>>sh, 255))
				}
			} else {
				copy(out, sp.pix8[r*sp.stride:r*sp.stride+dp.W])
			}
		}
	}
	return o
}
