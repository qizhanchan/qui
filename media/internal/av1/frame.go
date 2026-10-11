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

	cdf      cdfContext // context every tile starts from
	savedCDF cdfContext // from tile context_update_tile_id
	prevSeg  []uint8    // segment ids of the primary reference
	tiles    []*tile
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
	if h.Width > s.MaxWidth || h.Height > s.MaxHeight {
		return fmt.Errorf("%w: frame %dx%d above the sequence maximum", ErrInvalid, h.Width, h.Height)
	}
	f := &frameState{hdr: h, seq: s, miCols: h.miCols, miRows: h.miRows}
	d.fs = f
	f.cur = newFrame(h.Width, h.Height, s.BitDepth, s.NumPlanes())
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
func (d *Decoder) decodeTile(tn int, b []byte) error {
	f := d.fs
	h := f.hdr
	ti := &h.tiles
	row, col := tn/ti.cols, tn%ti.cols
	t := &tile{f: f, h: h, seq: d.seq}
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
	d.postFilter(f)
	d.updateRefs(h)
	if h.ShowFrame {
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

// output queues a shown frame.
func (d *Decoder) output(f *frame, h *FrameHeader) {
	if f == nil {
		return
	}
	d.out = append(d.out, f)
}

// postFilter applies the in-loop filters (spec 7.4 decode_frame_wrapup):
// deblocking, CDEF, superres upscaling and loop restoration.
func (d *Decoder) postFilter(f *frameState) {
	h := f.hdr
	cur := f.cur
	d.deblock(f)
	cdefF := cur
	if d.seq.EnableCDEF && !h.codedLossless && !h.allowIntraBC {
		cdefF = cloneFrame(cur)
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
}
