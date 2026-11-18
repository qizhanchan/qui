package vp9

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// Frame is an output picture: 8-bit I420 from the shared frame pool.
type Frame = h264.Frame

// mv is a motion vector in 1/8 pel.
type mv struct{ row, col int16 }

// mvRef is what a frame keeps per 8x8 for the next frame's MV prediction.
type mvRef struct {
	ref [2]int8
	mv  [2]mv
}

// frame is a decoded picture: 4:2:0 planes padded to whole superblocks.
type frame struct {
	w, h      int // display (crop) size
	planes    [3][]byte
	stride    [3]int
	pw, ph    [3]int // crop size per plane
	miCols    int
	miRows    int
	mvs       []mvRef
	intraOnly bool
}

func newFrame(w, h int) *frame {
	f := &frame{w: w, h: h}
	f.miCols, f.miRows = (w+7)>>3, (h+7)>>3
	aw, ah := (f.miCols+7)>>3<<6, (f.miRows+7)>>3<<6
	for p := 0; p < 3; p++ {
		sw, sh := aw, ah
		cw, ch := w, h
		if p > 0 {
			sw, sh = aw>>1, ah>>1
			cw, ch = (w+1)>>1, (h+1)>>1
		}
		// 8 spare rows/cols below and right absorb loop-filter reads.
		f.stride[p] = sw + 16
		f.planes[p] = make([]byte, f.stride[p]*(sh+16))
		f.pw[p], f.ph[p] = cw, ch
	}
	f.mvs = make([]mvRef, f.miCols*f.miRows)
	return f
}

// Decoder decodes a VP9 profile 0 stream, one WebM block (frame or
// superframe) per Decode call.
type Decoder struct {
	refs     [8]*frame
	fc       frameContext
	contexts [4]frameContext
	lf       loopFilterParams
	seg      segmentation

	segMapLast []uint8
	segMapCur  []uint8
	segCols    int
	segRows    int

	lastW, lastH  int
	lastShow      bool
	lastIntraOnly bool
	lastFrameType int
	prev          *frame
	haveKey       bool

	tag   int64
	ready []*Frame

	stats *decodeStats // tests only
	pool  []*frame

	// per-frame state
	cur        *frame
	hdr        *frameHeader
	miCols     int
	miRows     int
	miStride   int
	mi         []*blockInfo
	aboveNZ    [3][]uint8
	abovePart  []uint8
	scale      [3]scaleFactors
	refFrames  [3]*frame
	usePrevMVs bool
	tiles      []*tileState
	lfInfo     lfLevels
	counts     frameCounts
	dequant    [8][2][2]int32 // [segment][y/uv][dc/ac]
}

// debugPanic lets tests see a panic's stack.
var debugPanic = false

// New returns a decoder.
func New() *Decoder { return &Decoder{} }

// SetTag sets the value carried by the frames that the next Decode outputs.
func (d *Decoder) SetTag(tag int64) { d.tag = tag }

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

// Flush is a no-op: VP9 frames come out in decode order.
func (d *Decoder) Flush() {}

// SplitSuperframe splits a WebM block into its frames using the
// superframe index, if any.
func SplitSuperframe(data []byte) [][]byte {
	n := len(data)
	if n == 0 {
		return nil
	}
	m := data[n-1]
	if m&0xe0 == 0xc0 {
		frames := int(m&7) + 1
		mag := int((m>>3)&3) + 1
		idx := 2 + mag*frames
		if n >= idx && data[n-idx] == m {
			out := make([][]byte, 0, frames)
			p := data[n-idx+1:]
			off := 0
			for i := 0; i < frames; i++ {
				sz := 0
				for j := 0; j < mag; j++ {
					sz |= int(p[i*mag+j]) << (8 * j)
				}
				if sz > n-idx-off {
					return out
				}
				if sz > 0 {
					out = append(out, data[off:off+sz])
				}
				off += sz
			}
			return out
		}
	}
	return [][]byte{data}
}

// ProfileOf reports the profile of the frame in data (a WebM block), or
// -1 when it does not start with a VP9 frame header.
func ProfileOf(data []byte) int {
	fs := SplitSuperframe(data)
	if len(fs) == 0 || len(fs[0]) == 0 || fs[0][0]>>6 != 2 {
		return -1
	}
	b := fs[0][0]
	return int(b>>5&1) | int(b>>4&1)<<1
}

// Decode decodes one WebM block. Malformed input returns ErrInvalid (never
// panics); profiles 1–3 return ErrUnsupported.
func (d *Decoder) Decode(data []byte) (err error) {
	defer func() {
		if debugPanic {
			return
		}
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrInvalid, r)
		}
	}()
	for _, f := range SplitSuperframe(data) {
		if e := d.decodeFrame(f); e != nil {
			return e
		}
	}
	return nil
}

func (d *Decoder) output(f *frame) {
	o := h264.NewFrame(f.w, f.h)
	o.Width, o.Height = f.w, f.h
	o.Tag = d.tag
	o.Matrix = 2
	dst := [3]*h264.Plane{o.Y, o.Cb, o.Cr}
	for p := 0; p < 3; p++ {
		dp := dst[p]
		for r := 0; r < dp.H; r++ {
			copy(dp.Pix[r*dp.Stride:r*dp.Stride+dp.W], f.planes[p][r*f.stride[p]:])
		}
	}
	d.ready = append(d.ready, o)
}

func (d *Decoder) decodeFrame(data []byte) error {
	h := &frameHeader{}
	lastFrameType := d.lastFrameType
	if err := d.parseUncompressed(data, h); err != nil {
		return err
	}
	if d.stats != nil {
		d.stats.frame(h)
	}
	if h.showExisting {
		f := d.refs[h.showIdx]
		if f == nil {
			return ErrInvalid
		}
		d.output(f)
		return nil
	}
	if h.frameType == keyFrame {
		d.haveKey = true
	} else if !d.haveKey {
		return ErrInvalid
	}
	d.lastFrameType = h.frameType
	if len(data) < h.uncompSize+h.headerSize {
		return ErrInvalid
	}
	d.hdr = h
	cur := d.getFrame(h.width, h.height)
	cur.intraOnly = h.intraOnly()
	d.cur = cur
	d.miCols, d.miRows = cur.miCols, cur.miRows
	d.miStride = d.miCols

	// Segmentation maps follow the frame size.
	if d.segCols != d.miCols || d.segRows != d.miRows {
		d.segCols, d.segRows = d.miCols, d.miRows
		d.segMapLast = make([]uint8, d.miCols*d.miRows)
		d.segMapCur = make([]uint8, d.miCols*d.miRows)
	}

	if !h.intraOnly() {
		for i := 0; i < 3; i++ {
			r := d.refs[h.refIdx[i]]
			if r == nil {
				return ErrInvalid
			}
			if 2*h.width < r.w || 2*h.height < r.h || h.width > 16*r.w || h.height > 16*r.h {
				return ErrInvalid
			}
			d.refFrames[i] = r
			d.scale[i] = newScale(r.w, r.h, h.width, h.height)
		}
	}
	d.usePrevMVs = !h.errorRes && h.width == d.lastW && h.height == d.lastH && !d.lastIntraOnly && d.lastShow && d.prev != nil

	d.fc = d.contexts[h.contextIdx]
	cd := &boolDecoder{}
	if !cd.init(data[h.uncompSize : h.uncompSize+h.headerSize]) {
		return ErrInvalid
	}
	d.readCompressedHeader(cd, h, &d.fc)

	d.setupDequant(h)
	d.counts = frameCounts{}
	if err := d.decodeTiles(data[h.uncompSize+h.headerSize:]); err != nil {
		return err
	}
	if d.lf.level != 0 {
		d.loopFilter()
	}

	if !h.errorRes && !h.parallelMode {
		pre := &d.contexts[h.contextIdx]
		adaptCoefProbs(&d.fc, pre, &d.counts, h.intraOnly(), lastFrameType == keyFrame)
		if !h.intraOnly() {
			adaptModeProbs(&d.fc, pre, &d.counts, h)
			adaptMVProbs(&d.fc, pre, &d.counts, h.allowHP)
		}
	}
	if h.refreshContext {
		d.contexts[h.contextIdx] = d.fc
	}

	old := d.refs
	oldPrev := d.prev
	for i := 0; i < 8; i++ {
		if h.refreshFlags&(1<<i) != 0 {
			d.refs[i] = cur
		}
	}
	d.lastW, d.lastH = h.width, h.height
	d.lastShow = h.showFrame
	d.lastIntraOnly = h.intraOnlyFlag
	d.prev = cur
	for _, f := range append(old[:], oldPrev) {
		d.putFrame(f)
	}
	if d.seg.enabled {
		d.segMapLast, d.segMapCur = d.segMapCur, d.segMapLast
	}
	if h.showFrame {
		d.output(cur)
	}
	d.mi = d.mi[:0]
	return nil
}

// getFrame returns a frame buffer, reusing a retired one of the same size.
func (d *Decoder) getFrame(w, h int) *frame {
	for i, f := range d.pool {
		if f.w == w && f.h == h {
			d.pool = append(d.pool[:i], d.pool[i+1:]...)
			return f
		}
	}
	return newFrame(w, h)
}

// putFrame retires f unless a reference slot or the MV source still
// holds it.
func (d *Decoder) putFrame(f *frame) {
	if f == nil || f == d.prev {
		return
	}
	for _, r := range d.refs {
		if r == f {
			return
		}
	}
	for _, p := range d.pool {
		if p == f {
			return
		}
	}
	if len(d.pool) >= 4 {
		d.pool = d.pool[1:]
	}
	d.pool = append(d.pool, f)
}

func clampQ(q int) int { return min(max(q, 0), 255) }

func (d *Decoder) setupDequant(h *frameHeader) {
	for s := 0; s < 8; s++ {
		q := h.baseQ
		if d.seg.active(s, segLvlAltQ) {
			v := d.seg.featureData[s][segLvlAltQ]
			if !d.seg.absDelta {
				v += h.baseQ
			}
			q = clampQ(v)
		}
		d.dequant[s][0][0] = int32(dcQLookup[clampQ(q+h.dqYDC)])
		d.dequant[s][0][1] = int32(acQLookup[clampQ(q)])
		d.dequant[s][1][0] = int32(dcQLookup[clampQ(q+h.dqUVDC)])
		d.dequant[s][1][1] = int32(acQLookup[clampQ(q+h.dqUVAC)])
	}
}

// decodeTiles decodes all tiles: tile rows in order, the tile columns of
// a row in parallel.
func (d *Decoder) decodeTiles(data []byte) error {
	h := d.hdr
	tileCols, tileRows := 1<<h.tileColsLog2, 1<<h.tileRowsLog2
	if need := d.miRows * d.miCols; cap(d.mi) < need {
		d.mi = make([]*blockInfo, need)
	} else {
		d.mi = d.mi[:need]
		clear(d.mi)
	}
	alignedCols := (d.miCols + 7) &^ 7
	for p := 0; p < 3; p++ {
		n := alignedCols * 2
		if p > 0 {
			n = alignedCols
		}
		if cap(d.aboveNZ[p]) < n {
			d.aboveNZ[p] = make([]uint8, n)
		}
		d.aboveNZ[p] = d.aboveNZ[p][:n]
		clear(d.aboveNZ[p])
	}
	if cap(d.abovePart) < alignedCols {
		d.abovePart = make([]uint8, alignedCols)
	}
	d.abovePart = d.abovePart[:alignedCols]
	clear(d.abovePart)

	for len(d.tiles) < tileCols {
		d.tiles = append(d.tiles, &tileState{})
	}
	for _, t := range d.tiles {
		t.frameStart(d)
	}
	sbCols := (d.miCols + 7) >> 3
	sbRows := (d.miRows + 7) >> 3
	off := func(i, n, log2 int) int { return min(((i*n)>>log2)<<3, n<<3) }
	for tr := 0; tr < tileRows; tr++ {
		rowStart := min(off(tr, sbRows, h.tileRowsLog2), d.miRows)
		rowEnd := min(off(tr+1, sbRows, h.tileRowsLog2), d.miRows)
		for tc := 0; tc < tileCols; tc++ {
			last := tr == tileRows-1 && tc == tileCols-1
			var size int
			if last {
				size = len(data)
			} else {
				if len(data) < 4 {
					return ErrInvalid
				}
				size = int(binary.BigEndian.Uint32(data))
				data = data[4:]
				if size > len(data) {
					return ErrInvalid
				}
			}
			t := d.tiles[tc]
			t.reset(d)
			t.miRowStart, t.miRowEnd = rowStart, rowEnd
			t.miColStart = min(off(tc, sbCols, h.tileColsLog2), d.miCols)
			t.miColEnd = min(off(tc+1, sbCols, h.tileColsLog2), d.miCols)
			if !t.bd.init(data[:size]) {
				return ErrInvalid
			}
			data = data[size:]
		}
		if tileCols == 1 || runtime.GOMAXPROCS(0) == 1 {
			for tc := 0; tc < tileCols; tc++ {
				if err := d.tiles[tc].decode(); err != nil {
					return err
				}
			}
		} else {
			var wg sync.WaitGroup
			errs := make([]error, tileCols)
			for tc := 0; tc < tileCols; tc++ {
				wg.Add(1)
				go func(tc int) {
					defer wg.Done()
					defer func() {
						if r := recover(); r != nil {
							errs[tc] = fmt.Errorf("%w: %v", ErrInvalid, r)
						}
					}()
					errs[tc] = d.tiles[tc].decode()
				}(tc)
			}
			wg.Wait()
			for _, e := range errs {
				if e != nil {
					return e
				}
			}
		}
	}
	if !h.errorRes && !h.parallelMode {
		for tc := 0; tc < tileCols; tc++ {
			d.counts.add(&d.tiles[tc].counts)
		}
	}
	return nil
}

// Recycle returns an output frame to the shared pool.
func Recycle(f *Frame) { h264.Recycle(f) }

// KeyframeSize returns the frame size of a profile 0 keyframe (the first
// frame of data, a WebM block); ok is false for anything else.
func KeyframeSize(data []byte) (w, h int, ok bool) {
	fs := SplitSuperframe(data)
	if len(fs) == 0 {
		return 0, 0, false
	}
	b := &bitReader{buf: fs[0]}
	if b.f(2) != 2 || b.bit() != 0 || b.bit() != 0 { // marker, profile 0
		return 0, 0, false
	}
	if b.bit() != 0 || b.bit() != keyFrame { // show_existing, frame_type
		return 0, 0, false
	}
	b.f(2) // show_frame, error_resilient_mode
	if b.f(24) != 0x498342 || b.f(3) == 7 {
		return 0, 0, false
	}
	b.bit() // color_range
	w, h = b.f(16)+1, b.f(16)+1
	return w, h, !b.err
}
