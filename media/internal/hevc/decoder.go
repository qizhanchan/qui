package hevc

import (
	"sort"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// Frame is a decoded picture (shared with the H.264 decoder).
type Frame = h264.Frame

// Plane is one picture plane.
type Plane = h264.Plane

// picture is a decoded picture with the motion kept for temporal MV
// prediction.
type picture struct {
	y, cb, cr []byte
	ys, cs    int
	w, h      int

	poc        int
	isRef      bool
	longTerm   bool
	needOutput bool
	latency    int
	tag        int64
	key        bool

	// Per 4x4: motion for TMVP (8.5.3.2.8).
	mv     [2][]int32 // packed x<<16 | y&0xffff
	refIdx [2][]int8
	refPOC [2][]int32
	refLT  [2][]bool
	intra  []bool
	w4     int
	// Reference lists of each slice by slice index, with POCs, for TMVP of
	// later pictures (col block's references).
}

// frameCtx is the per-picture decoding state.
type frameCtx struct {
	w4, h4       int
	blk          []blkInfo
	ctbSliceAddr []int
	ctbSlice     []int
	sao          []saoParams
	slices       []*sliceHeader
}

func (f *frameCtx) reset(sp *SPS) {
	f.w4, f.h4 = (sp.Width+3)/4, (sp.Height+3)/4
	n := f.w4 * f.h4
	if cap(f.blk) < n {
		f.blk = make([]blkInfo, n)
	}
	f.blk = f.blk[:n]
	clear(f.blk)
	nc := sp.PicWCtb * sp.PicHCtb
	if cap(f.ctbSliceAddr) < nc {
		f.ctbSliceAddr = make([]int, nc)
		f.ctbSlice = make([]int, nc)
		f.sao = make([]saoParams, nc)
	}
	f.ctbSliceAddr = f.ctbSliceAddr[:nc]
	f.ctbSlice = f.ctbSlice[:nc]
	f.sao = f.sao[:nc]
	for i := range f.ctbSliceAddr {
		f.ctbSliceAddr[i] = -1
	}
	clear(f.sao)
	f.slices = f.slices[:0]
}

// Decoder decodes an HEVC elementary stream NAL unit by NAL unit and
// returns pictures in display order.
type Decoder struct {
	sps [16]*SPS
	pps [64]*PPS

	active *SPS
	dpb    []*picture
	pool   []*picture
	cur    *picture
	fc     frameCtx
	sd     sliceDec
	rbsp   []byte

	lastIndep   *sliceHeader
	sliceAddrRs int
	wppCtx      [numCtx]uint8
	wppValid    bool
	dsCtx       [numCtx]uint8
	dsCtxValid  bool
	dsQP        [2]int // QpY and qPY_PREV at the end of the last segment

	prevTid0POC int
	first       bool // next picture is the first (or follows an EOS)
	noRaslOut   bool // NoRaslOutputFlag of the associated IRAP
	skipPic     bool // current picture is a skipped RASL picture
	tag         int64

	out     []*Frame
	lastErr error
	saoTmp  [3][]byte
}

// New returns a decoder.
func New() *Decoder { return &Decoder{first: true} }

// SetTag sets the value attached to the next picture (see Frame.Tag).
func (d *Decoder) SetTag(tag int64) { d.tag = tag }

// SPSInfo returns the active SPS, or nil.
func (d *Decoder) SPSInfo() *SPS { return d.active }

// DecodeNAL decodes one NAL unit (2-byte header first, no start code).
func (d *Decoder) DecodeNAL(nal []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errCorrupt
		}
	}()
	if len(nal) < 2 || nal[0]&0x80 != 0 {
		return nil
	}
	if layer := int(nal[0]&1)<<5 | int(nal[1]>>3); layer != 0 {
		return nil
	}
	typ := int(nal[0]>>1) & 63
	switch {
	case typ == nalSPS:
		s, err := ParseSPS(nal)
		if err != nil {
			return err
		}
		d.sps[s.ID] = s
	case typ == nalPPS:
		p, err := parsePPS(nal, func(i int) *SPS { return d.sps[i] })
		if err != nil {
			return err
		}
		d.pps[p.ID] = p
	case typ == nalEOS || typ == nalEOB:
		d.EndOfPicture()
		d.first = true
	case typ == nalAUD:
		d.EndOfPicture()
	case typ <= 21 && (typ <= 9 || typ >= 16):
		return d.decodeSlice(nal, typ)
	}
	return nil
}

// DecodeNALs decodes the NAL units of one access unit and ends the
// picture.
func (d *Decoder) DecodeNALs(nals [][]byte) error {
	var first error
	for _, n := range nals {
		if err := d.DecodeNAL(n); err != nil && first == nil {
			first = err
		}
	}
	d.EndOfPicture()
	return first
}

// Next returns the next output frame, or nil.
func (d *Decoder) Next() *Frame {
	if len(d.out) == 0 {
		return nil
	}
	f := d.out[0]
	d.out = d.out[1:]
	return f
}

// Flush finishes the current picture and outputs everything still held.
func (d *Decoder) Flush() {
	d.EndOfPicture()
	for d.bump() {
	}
}

// DecodeAnnexB decodes a complete Annex-B stream.
func DecodeAnnexB(data []byte) ([]*Frame, error) {
	d := New()
	var frames []*Frame
	var first error
	for _, nal := range SplitAnnexB(data) {
		if err := d.DecodeNAL(nal); err != nil && first == nil {
			first = err
		}
		for f := d.Next(); f != nil; f = d.Next() {
			frames = append(frames, f)
		}
	}
	d.Flush()
	for f := d.Next(); f != nil; f = d.Next() {
		frames = append(frames, f)
	}
	return frames, first
}

func isRASL(t int) bool { return t == nalRASLN || t == nalRASLR }

func (d *Decoder) decodeSlice(nal []byte, typ int) error {
	d.rbsp = unescape(d.rbsp, nal[2:])
	var r bitReader
	r.reset(d.rbsp)
	// first_slice_segment_in_pic_flag decides whether a picture starts.
	if len(d.rbsp) == 0 {
		return errShort
	}
	if d.rbsp[0]&0x80 != 0 {
		d.EndOfPicture()
		d.lastIndep = nil
	} else if d.cur == nil && !d.skipPic {
		return errCorrupt // a picture without its first slice
	}
	h, err := parseSliceHeader(&r, nal, d.sps[:], d.pps[:], d.lastIndep)
	if err != nil {
		return err
	}
	if h.firstInPic {
		if err := d.startPicture(h); err != nil {
			return err
		}
	}
	if d.skipPic {
		return nil
	}
	if !h.dependent {
		d.lastIndep = h
	}
	if h.sps != d.active {
		return errCorrupt
	}
	fc := &d.fc
	fc.slices = append(fc.slices, h)
	sd := &d.sd
	sd.d, sd.h, sd.sps, sd.pps, sd.pic = d, h, h.sps, h.pps, d.cur
	sd.sliceIdx = len(fc.slices) - 1
	if !h.dependent {
		d.buildRefLists(h)
	}
	err = sd.decode(d.rbsp, h.dataOffset)
	if err != nil {
		d.lastErr = err
	}
	return err
}

// startPicture handles POC, the RPS, DPB output and allocation for a new
// picture (8.1.3, 8.3.1, 8.3.2, C.5.2.2).
func (d *Decoder) startPicture(h *sliceHeader) error {
	sp := h.sps
	irap := h.isIRAP()
	if irap {
		d.noRaslOut = h.isIDR() || h.nalType <= nalBLANLP || d.first
	}
	d.skipPic = false
	if isRASL(h.nalType) && d.noRaslOut {
		d.skipPic = true
		return nil
	}
	if d.active != sp {
		if d.active != nil && (d.active.Width != sp.Width || d.active.Height != sp.Height) {
			for d.bump() {
			}
			d.dpb = d.dpb[:0]
			d.pool = d.pool[:0]
		}
		d.active = sp
	}
	// POC (8.3.1).
	maxLsb := 1 << sp.Log2MaxPOCLsb
	var msb int
	if irap && d.noRaslOut {
		msb = 0
	} else {
		prevLsb := d.prevTid0POC & (maxLsb - 1)
		prevMsb := d.prevTid0POC - prevLsb
		switch {
		case h.pocLsb < prevLsb && prevLsb-h.pocLsb >= maxLsb/2:
			msb = prevMsb + maxLsb
		case h.pocLsb > prevLsb && h.pocLsb-prevLsb > maxLsb/2:
			msb = prevMsb - maxLsb
		default:
			msb = prevMsb
		}
	}
	poc := msb + h.pocLsb
	slnr := h.nalType <= 14 && h.nalType%2 == 0
	if h.temporal == 0 && !(h.nalType >= 6 && h.nalType <= 9) && !slnr {
		d.prevTid0POC = poc
	}
	// RPS (8.3.2).
	if h.isIDR() {
		for _, p := range d.dpb {
			p.isRef = false
		}
	} else {
		keep := make(map[*picture]bool)
		for i := 0; i < h.rps.num(); i++ {
			if p := d.findPOC(poc+h.rps.deltaPOC[i], false); p != nil {
				keep[p] = true
				p.longTerm = false
			}
		}
		for i := 0; i < h.numLT; i++ {
			if p := d.findLT(h, i, poc, maxLsb); p != nil {
				keep[p] = true
				p.longTerm = true
			}
		}
		for _, p := range d.dpb {
			if !keep[p] {
				p.isRef = false
			}
		}
	}
	// Output and removal before decoding (C.5.2.2).
	if irap && d.noRaslOut && !d.first {
		if h.noOutputPrior {
			d.dpb = d.dpb[:0]
		} else {
			for d.bump() {
			}
		}
	}
	d.pruneDPB()
	for {
		n := 0
		for _, p := range d.dpb {
			if p.needOutput {
				n++
			}
		}
		latency := false
		if sp.MaxLatencyIncrease != 0 {
			lim := sp.MaxNumReorder + sp.MaxLatencyIncrease - 1
			for _, p := range d.dpb {
				if p.needOutput && p.latency >= lim {
					latency = true
				}
			}
		}
		if n > sp.MaxNumReorder || latency || len(d.dpb) >= sp.MaxDecPicBuffering {
			if !d.bump() {
				break
			}
			continue
		}
		break
	}
	d.first = false
	p := d.newPicture(sp)
	p.poc = poc
	p.tag = d.tag
	p.key = irap
	p.needOutput = h.picOutput && !(isRASL(h.nalType) && d.noRaslOut)
	d.cur = p
	d.fc.reset(sp)
	d.wppValid, d.dsCtxValid = false, false
	// Missing references (8.3.3): generate grey stand-ins so decoding can
	// proceed on broken streams.
	if !h.isIDR() {
		for i := 0; i < h.rps.num(); i++ {
			if h.rps.used[i] && d.findPOC(poc+h.rps.deltaPOC[i], false) == nil {
				g := d.newPicture(sp)
				for j := range g.y {
					g.y[j] = 128
				}
				for j := range g.cb {
					g.cb[j], g.cr[j] = 128, 128
				}
				for j := range g.intra {
					g.intra[j] = true
				}
				g.poc = poc + h.rps.deltaPOC[i]
				g.isRef = true
				d.dpb = append(d.dpb, g)
			}
		}
	}
	return nil
}

func (d *Decoder) findPOC(poc int, lt bool) *picture {
	for _, p := range d.dpb {
		if p.isRef && p.poc == poc && (!lt || p.longTerm) {
			return p
		}
	}
	return nil
}

func (d *Decoder) findLT(h *sliceHeader, i, poc, maxLsb int) *picture {
	if h.ltMsb[i] {
		want := poc - h.ltDeltaMsb[i]*maxLsb - (poc & (maxLsb - 1)) + h.ltLsb[i]
		for _, p := range d.dpb {
			if p.isRef && p.poc == want {
				return p
			}
		}
		return nil
	}
	for _, p := range d.dpb {
		if p.isRef && p.poc&(maxLsb-1) == h.ltLsb[i] {
			return p
		}
	}
	return nil
}

// buildRefLists builds RefPicList0/1 for a slice (8.3.4).
func (d *Decoder) buildRefLists(h *sliceHeader) {
	h.refList[0], h.refList[1] = h.refList[0][:0], h.refList[1][:0]
	if h.typ == sliceI {
		return
	}
	poc := d.cur.poc
	maxLsb := 1 << h.sps.Log2MaxPOCLsb
	var before, after, lt []*picture
	for i := 0; i < h.rps.num(); i++ {
		if !h.rps.used[i] {
			continue
		}
		p := d.findPOC(poc+h.rps.deltaPOC[i], false)
		if i < h.rps.numNeg {
			before = append(before, p)
		} else {
			after = append(after, p)
		}
	}
	for i := 0; i < h.numLT; i++ {
		if h.ltUsed[i] {
			lt = append(lt, d.findLT(h, i, poc, maxLsb))
		}
	}
	total := len(before) + len(after) + len(lt)
	if total == 0 {
		return
	}
	for l := 0; l < 1+boolInt(h.typ == sliceB); l++ {
		var temp []*picture
		var tempLT []bool
		n := max(h.numRefIdx[l], total)
		for len(temp) < n {
			first, second := before, after
			if l == 1 {
				first, second = after, before
			}
			for _, p := range first {
				if len(temp) < n {
					temp, tempLT = append(temp, p), append(tempLT, false)
				}
			}
			for _, p := range second {
				if len(temp) < n {
					temp, tempLT = append(temp, p), append(tempLT, false)
				}
			}
			for _, p := range lt {
				if len(temp) < n {
					temp, tempLT = append(temp, p), append(tempLT, true)
				}
			}
		}
		for i := 0; i < h.numRefIdx[l]; i++ {
			idx := i
			if h.listMod[l] {
				idx = h.listEntry[l][i]
			}
			if idx >= len(temp) {
				idx = 0
			}
			p := temp[idx]
			if p == nil {
				p = d.cur // concealment: should not happen after 8.3.3
			}
			h.refList[l] = append(h.refList[l], refEntry{pic: p, poc: p.poc, lt: tempLT[idx]})
		}
	}
}

// newPicture returns a picture buffer for sp.
func (d *Decoder) newPicture(sp *SPS) *picture {
	var p *picture
	for i := len(d.pool) - 1; i >= 0; i-- {
		if c := d.pool[i]; c.w == sp.Width && c.h == sp.Height {
			p = c
			d.pool = append(d.pool[:i], d.pool[i+1:]...)
			break
		}
	}
	if p == nil {
		p = &picture{w: sp.Width, h: sp.Height, ys: sp.Width, cs: sp.Width / 2}
		p.y = make([]byte, sp.Width*sp.Height)
		p.cb = make([]byte, sp.Width*sp.Height/4)
		p.cr = make([]byte, sp.Width*sp.Height/4)
		p.w4 = (sp.Width + 3) / 4
		n := p.w4 * ((sp.Height + 3) / 4)
		for l := 0; l < 2; l++ {
			p.mv[l] = make([]int32, n)
			p.refIdx[l] = make([]int8, n)
			p.refPOC[l] = make([]int32, n)
			p.refLT[l] = make([]bool, n)
		}
		p.intra = make([]bool, n)
	}
	p.isRef, p.longTerm, p.needOutput, p.latency = false, false, false, 0
	return p
}

// EndOfPicture finishes the picture being decoded: in-loop filters,
// reference marking and output.
func (d *Decoder) EndOfPicture() {
	p := d.cur
	if p == nil {
		return
	}
	d.cur = nil
	defer func() {
		if r := recover(); r != nil {
			d.lastErr = errCorrupt
		}
	}()
	d.deblock(p)
	d.applySAO(p)
	p.isRef = true
	p.longTerm = false
	for _, q := range d.dpb {
		if q.needOutput {
			q.latency++
		}
	}
	d.dpb = append(d.dpb, p)
	sp := d.active
	for {
		n := 0
		latency := false
		lim := sp.MaxNumReorder + sp.MaxLatencyIncrease - 1
		for _, q := range d.dpb {
			if q.needOutput {
				n++
				if sp.MaxLatencyIncrease != 0 && q.latency >= lim {
					latency = true
				}
			}
		}
		if n > sp.MaxNumReorder || latency {
			if !d.bump() {
				break
			}
			continue
		}
		break
	}
}

// bump outputs the picture with the smallest POC that waits for output.
func (d *Decoder) bump() bool {
	var best *picture
	for _, p := range d.dpb {
		if p.needOutput && (best == nil || p.poc < best.poc) {
			best = p
		}
	}
	if best == nil {
		return false
	}
	best.needOutput = false
	d.out = append(d.out, d.frameOf(best))
	d.pruneDPB()
	return true
}

// pruneDPB removes pictures neither referenced nor waiting for output.
func (d *Decoder) pruneDPB() {
	k := 0
	for _, p := range d.dpb {
		if p.isRef || p.needOutput {
			d.dpb[k] = p
			k++
		} else if p != d.cur {
			d.pool = append(d.pool, p)
		}
	}
	d.dpb = d.dpb[:k]
}

// frameOf copies a picture out, cropped to the conformance window.
func (d *Decoder) frameOf(p *picture) *Frame {
	sp := d.active
	w := p.w - sp.ConfLeft - sp.ConfRight
	h := p.h - sp.ConfTop - sp.ConfBottom
	f := &Frame{Width: w, Height: h, POC: p.poc, Tag: p.tag, KeyFrame: p.key, FullRange: sp.FullRange, Matrix: sp.Matrix}
	cp := func(src []byte, ss, x0, y0, w, h int) *Plane {
		pl := &Plane{Pix: make([]byte, w*h), Stride: w, W: w, H: h}
		for y := 0; y < h; y++ {
			copy(pl.Pix[y*w:], src[(y0+y)*ss+x0:(y0+y)*ss+x0+w])
		}
		return pl
	}
	f.Y = cp(p.y, p.ys, sp.ConfLeft, sp.ConfTop, w, h)
	f.Cb = cp(p.cb, p.cs, sp.ConfLeft/2, sp.ConfTop/2, w/2, h/2)
	f.Cr = cp(p.cr, p.cs, sp.ConfLeft/2, sp.ConfTop/2, w/2, h/2)
	return f
}

var _ = sort.Ints
