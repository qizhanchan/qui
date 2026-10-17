package av1

import "fmt"

// planeBuf is one sample plane: 8-bit samples in pix8 or high bit depth
// samples in pix16.
type planeBuf struct {
	pix8   []uint8
	pix16  []uint16
	stride int
	w, h   int
}

// frame is a decoded picture plus the state later frames reference: its
// mode info (motion field), segment ids and final CDFs.
type frame struct {
	w, h, upscaledW int // coded and upscaled size
	bitDepth        int
	planes          [3]planeBuf
	numPlanes       int

	miCols, miRows int
	refFrames      [][2]int8 // per mi, for the motion field
	mvs            [][2]mv
	segIDs         []uint8
	mfRef          []int8 // per 8x8: reference of the saved motion vector
	mfMv           []mv
	cdf            cdfContext
	showable       bool
	frameType      int
}

func newFrame(w, h, bitDepth, numPlanes int) *frame {
	f := &frame{w: w, h: h, upscaledW: w, bitDepth: bitDepth, numPlanes: numPlanes}
	// Blocks and transforms may extend past the frame up to the
	// superblock grid; allocate whole 128x128 superblocks.
	aw, ah := (w+127)&^127, (h+127)&^127
	for p := 0; p < numPlanes; p++ {
		pw, ph, apw, aph := w, h, aw, ah
		if p > 0 {
			pw, ph, apw, aph = (w+1)>>1, (h+1)>>1, aw>>1, ah>>1
		}
		pb := &f.planes[p]
		pb.w, pb.h = pw, ph
		pb.stride = apw
		if bitDepth > 8 {
			pb.pix16 = make([]uint16, pb.stride*aph)
		} else {
			pb.pix8 = make([]uint8, pb.stride*aph)
		}
	}
	return f
}

// lrUnit holds the loop restoration parameters of one unit.
type lrUnit struct {
	typ    uint8
	sgrSet uint8
	wiener [2][3]int8
	xqd    [2]int16
}

type lrPlane struct {
	rows, cols int
	units      []lrUnit
}

// frameState is the frame being decoded.
type frameState struct {
	hdr *FrameHeader
	seq *SequenceHeader
	cur *frame

	miCols, miRows int
	mi             []miInfo
	interTxSize    []uint8
	cdefIdx        []int8
	cdefStride     int
	lfTxSize       [3][]uint8
	lfStride       [3]int
	lr             [3]lrPlane

	refs        [numRefFrames]refSlot // reference slots by RefFrame (1..7)
	scaled      [numRefFrames]bool
	gmValid     [numRefFrames]bool
	motionField []tplMv
	cdf         cdfContext // context every tile starts from
	savedCDF    cdfContext // from tile context_update_tile_id
	prevSeg     []uint8    // segment ids of the primary reference
	tiles       []*tile
	pipe        *filterPipe // filters running behind the tile decoder
}

func (f *frameState) cdefIndex(r, c int) int { return (r>>4)*f.cdefStride + c>>4 }

func (f *frameState) clearCdef(r, c int) {
	f.cdefIdx[f.cdefIndex(r, c)] = -1
	if f.seq.Use128x128 {
		if c+16 < f.miCols {
			f.cdefIdx[f.cdefIndex(r, c+16)] = -1
		}
		if r+16 < f.miRows {
			f.cdefIdx[f.cdefIndex(r+16, c)] = -1
			if c+16 < f.miCols {
				f.cdefIdx[f.cdefIndex(r+16, c+16)] = -1
			}
		}
	}
}

func unitsInFrame(unit, size int) int { return max((size+unit>>1)/unit, 1) }

// startFrame allocates the frame and loads the entropy state.
func (d *Decoder) startFrame(h *FrameHeader) error {
	s := d.seq
	// libsvtav1's resize mode codes frames rounded up to 8 lines beyond
	// max_frame_height; libdav1d accepts them, so only the level limit
	// (8.3: at most 2^16 in each direction, 35651584 luma samples) is
	// enforced.
	if h.Width > 1<<16 || h.Height > 1<<16 || h.UpscaledWidth*h.Height > 35651584 {
		return fmt.Errorf("%w: frame %dx%d too large", ErrInvalid, h.UpscaledWidth, h.Height)
	}
	f := &frameState{hdr: h, seq: s, miCols: h.miCols, miRows: h.miRows}
	d.fs = f
	f.cur = d.newFrame(h.Width, h.Height, s.BitDepth, s.NumPlanes())
	f.cur.upscaledW = h.UpscaledWidth
	f.cur.showable = h.Showable
	f.cur.frameType = h.FrameType
	n := f.miCols * f.miRows
	f.mi = make([]miInfo, n)
	f.interTxSize = make([]uint8, n)
	f.cdefStride = (f.miCols + 15) >> 4
	f.cdefIdx = make([]int8, f.cdefStride*((f.miRows+15)>>4))
	for p := 0; p < s.NumPlanes(); p++ {
		ss := min(p, 1)
		f.lfStride[p] = (f.miCols + ss) >> ss
		f.lfTxSize[p] = make([]uint8, f.lfStride[p]*((f.miRows+ss)>>ss))
	}
	if h.usesLR {
		for p := 0; p < s.NumPlanes(); p++ {
			if h.lrType[p] == restoreNone {
				continue
			}
			ss := min(p, 1)
			u := h.lrUnitSize[p]
			lp := &f.lr[p]
			lp.rows = unitsInFrame(u, (h.Height+ss)>>ss)
			lp.cols = unitsInFrame(u, (h.UpscaledWidth+ss)>>ss)
			lp.units = make([]lrUnit, lp.rows*lp.cols)
		}
	}
	if !h.intra {
		for i := 0; i < refsPerFrame; i++ {
			slot := d.ref[h.refFrameIdx[i]]
			f.refs[lastFrame+i] = slot
			xs := ((slot.upscaledWidth << 14) + h.Width/2) / h.Width
			ys := ((slot.frameHeight << 14) + h.Height/2) / h.Height
			f.scaled[lastFrame+i] = xs != 1<<14 || ys != 1<<14
			if h.gmType[lastFrame+i] > gmTranslation {
				gm := h.gmParams[lastFrame+i]
				_, _, _, _, f.gmValid[lastFrame+i] = setupShear(&gm)
			}
		}
		if h.useRefFrameMVs {
			d.motionFieldEstimation(f)
		}
	}
	// Entropy state: defaults or the primary reference frame's.
	if h.primaryRefFrame == primaryRefNone {
		f.cdf.initNonCoeff()
		f.cdf.initCoeff(h.baseQIdx)
	} else {
		ref := d.ref[h.refFrameIdx[h.primaryRefFrame]].pic
		if ref == nil {
			return fmt.Errorf("%w: primary reference frame missing", ErrInvalid)
		}
		f.cdf = ref.cdf
		if ref.segIDs != nil && ref.miRows == f.miRows && ref.miCols == f.miCols {
			f.prevSeg = ref.segIDs
		}
	}
	return nil
}

// decodeTile decodes tile number tn of the current frame.
func (d *Decoder) decodeTile(tn int, b []byte, sc *tileScratch) error {
	f := d.fs
	h := f.hdr
	ti := &h.tiles
	row, col := tn/ti.cols, tn%ti.cols
	t := &tile{f: f, h: h, seq: d.seq, mc: &sc.mc, recon: &sc.recon}
	t.miRowStart, t.miRowEnd = ti.miRowStarts[row], ti.miRowStarts[row+1]
	t.miColStart, t.miColEnd = ti.miColStarts[col], ti.miColStarts[col+1]
	t.currentQ = h.baseQIdx
	t.cdf = f.cdf
	t.ms.init(b, h.disableCDFUpdate)
	if err := t.decode(); err != nil {
		return fmt.Errorf("tile %d: %w", tn, err)
	}
	if tn == ti.contextUpdateID {
		f.savedCDF = t.cdf
	}
	return nil
}

func (d *Decoder) finishFrame() error {
	f := d.fs
	h := f.hdr
	cur := f.cur
	if !h.disableFrameEndUpdate {
		cur.cdf = f.savedCDF
		cur.cdf.resetCounts()
	} else {
		cur.cdf = f.cdf
	}
	// Segment ids and the motion field for later frames.
	cur.miCols, cur.miRows = f.miCols, f.miRows
	cur.segIDs = make([]uint8, len(f.mi))
	for i := range f.mi {
		cur.segIDs[i] = f.mi[i].segID
	}
	f.saveMotionField()
	if f.pipe != nil {
		d.finishPipe(f)
	} else {
		d.postFilter(f)
	}
	d.updateRefs(h)
	if h.ShowFrame || d.outputInvisible {
		d.output(cur, h)
	}
	return nil
}

func (d *Decoder) showExisting(h *FrameHeader) error {
	slot := &d.ref[h.FrameToShow]
	d.output(slot.pic, h)
	if h.FrameType == keyFrame {
		// The shown key frame resets the references (spec 7.21).
		s := *slot
		s.pic.frameType = keyFrame
		for i := range d.ref {
			d.ref[i] = s
		}
	}
	return nil
}

// updateRefs is the reference frame update process (spec 7.20).
func (d *Decoder) updateRefs(h *FrameHeader) {
	for i := 0; i < numRefFrames; i++ {
		if h.refreshFlags>>i&1 == 0 {
			continue
		}
		d.ref[i] = refSlot{
			valid:          true,
			frameID:        h.currentFrameID,
			upscaledWidth:  h.UpscaledWidth,
			frameWidth:     h.Width,
			frameHeight:    h.Height,
			renderWidth:    h.RenderWidth,
			renderHeight:   h.RenderHeight,
			miCols:         h.miCols,
			miRows:         h.miRows,
			frameType:      h.FrameType,
			orderHint:      h.OrderHint,
			savedOrderHint: h.orderHints,
			gmParams:       h.gmParams,
			lf:             h.lf,
			seg:            h.seg,
			grain:          h.grain,
			showable:       h.Showable,
			bitDepth:       d.seq.BitDepth,
			pic:            d.fs.cur,
		}
	}
}

// output queues a shown frame, with film grain when the header asks for
// it.
func (d *Decoder) output(f *frame, h *FrameHeader) {
	if f == nil {
		return
	}
	if h.grain.apply && !d.noFilmGrain && (h.ShowFrame || h.ShowExistingFrame) {
		f = applyFilmGrain(f, &h.grain, d.seq)
	}
	d.out = append(d.out, f)
}

// postFilter applies the in-loop filters (spec 7.4 decode_frame_wrapup):
// deblocking, CDEF, superres upscaling and loop restoration.
func (d *Decoder) postFilter(f *frameState) {
	h := f.hdr
	cur := f.cur
	orig := cur.planes
	d.deblock(f)
	cdefF := cur
	if d.seq.EnableCDEF && !h.codedLossless && !h.allowIntraBC {
		cdefF = d.cloneFrame(cur)
		d.cdef(f, cur, cdefF)
	}
	upCdef, upCur := cdefF, cur
	if h.UpscaledWidth != h.Width {
		upCdef = upscale(f, cdefF)
		if h.usesLR {
			upCur = upscale(f, cur)
		}
	}
	final := upCdef
	if h.usesLR {
		final = d.restore(f, upCdef, upCur)
	}
	cur.planes = final.planes
	// The intermediate frames' buffers are free again.
	done := []*[3]planeBuf{&final.planes}
next:
	for _, ps := range []*[3]planeBuf{&orig, &cdefF.planes, &upCdef.planes, &upCur.planes} {
		for _, q := range done {
			if samePlanes(ps, q) {
				continue next
			}
		}
		done = append(done, ps)
		d.release(ps, cur.numPlanes)
	}
}

// samePlanes reports whether a and b share their luma buffer.
func samePlanes(a, b *[3]planeBuf) bool {
	if a[0].pix16 != nil {
		return b[0].pix16 != nil && &a[0].pix16[0] == &b[0].pix16[0]
	}
	return b[0].pix8 != nil && &a[0].pix8[0] == &b[0].pix8[0]
}

// release returns plane buffers to the pool.
func (d *Decoder) release(ps *[3]planeBuf, n int) {
	const maxFree = 12
	for p := 0; p < n; p++ {
		if b := ps[p].pix16; b != nil && len(d.free16) < maxFree {
			d.free16 = append(d.free16, b)
		}
		if b := ps[p].pix8; b != nil && len(d.free8) < maxFree {
			d.free8 = append(d.free8, b)
		}
	}
}

func take[T uint8 | uint16](free *[][]T, n int) []T {
	for i, b := range *free {
		if len(b) == n {
			l := len(*free) - 1
			(*free)[i] = (*free)[l]
			*free = (*free)[:l]
			return b
		}
	}
	return make([]T, n)
}

// newFrame is newFrame with zeroed buffers from the pool when possible.
func (d *Decoder) newFrame(w, h, bitDepth, numPlanes int) *frame {
	f := &frame{w: w, h: h, upscaledW: w, bitDepth: bitDepth, numPlanes: numPlanes}
	aw, ah := (w+127)&^127, (h+127)&^127
	for p := 0; p < numPlanes; p++ {
		pw, ph, apw, aph := w, h, aw, ah
		if p > 0 {
			pw, ph, apw, aph = (w+1)>>1, (h+1)>>1, aw>>1, ah>>1
		}
		pb := &f.planes[p]
		pb.w, pb.h = pw, ph
		pb.stride = apw
		if bitDepth > 8 {
			pb.pix16 = take(&d.free16, pb.stride*aph)
			clear(pb.pix16)
		} else {
			pb.pix8 = take(&d.free8, pb.stride*aph)
			clear(pb.pix8)
		}
	}
	return f
}

// cloneFrame is cloneFrame with buffers from the pool when possible.
func (d *Decoder) cloneFrame(f *frame) *frame {
	g := *f
	for p := 0; p < f.numPlanes; p++ {
		if b := f.planes[p].pix16; b != nil {
			g.planes[p].pix16 = take(&d.free16, len(b))
			copy(g.planes[p].pix16, b)
		} else {
			b := f.planes[p].pix8
			g.planes[p].pix8 = take(&d.free8, len(b))
			copy(g.planes[p].pix8, b)
		}
	}
	return &g
}
