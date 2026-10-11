// Package h264enc is a pure-Go H.264/AVC encoder. It reuses the decoder's
// prediction, inverse transform, motion compensation and deblocking
// (media/internal/h264 kit), so its reconstruction is, by construction, what
// a decoder produces from its stream.
package h264enc

import (
	"errors"
	"image"
	"math"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// Config describes an encoding session. Zero values pick defaults.
type Config struct {
	Width, Height  int
	FPSNum, FPSDen int // frame rate as a fraction (default 30/1)

	Profile int // ProfileBaseline (default), ProfileMain or ProfileHigh

	// Rate control: constant QP when Bitrate and CRF are zero.
	QP      int     // 0..51, default 26 (also the starting QP of ABR)
	CRF     float64 // constant rate factor (x264-like scale), 0 = off
	Bitrate int     // average bits/s (ABR), 0 = off
	// VBV: maximum bits/s and buffer size in bits (0 = unconstrained).
	VBVMaxRate, VBVBuffer int

	KeyInt int // frames between IDR pictures (default 250)
	Refs   int // reference frames for P pictures (default 1)

	// Preset trades speed for compression: 0 = fastest ... 2 = best.
	Preset int

	CABAC        bool // Main/High only
	Transform8x8 bool // High only
	BFrames      int  // consecutive B pictures (Main/High), 0 = none

	// Slices per picture (encoded in parallel). 0/1 = one slice.
	Slices int

	NoDeblock                 bool
	DeblockAlpha, DeblockBeta int // slice_alpha/beta_c0_offset_div2
	ChromaQPOffset            int

	FullRange bool
	Matrix    int // H.273 matrix_coefficients: 1 BT.709 (default), 6 BT.601
}

// Packet is one coded picture.
type Packet struct {
	NALs [][]byte // slice NAL units (SPS/PPS precede IDR pictures)
	Key  bool
	PTS  int64 // input frame index
	DTS  int64
	Type byte // 'I', 'P' or 'B'
	QP   int  // slice QP
}

// Encoder encodes I420 frames.
type Encoder struct {
	cfg              Config
	mbW, mbH         int
	level, pocType   int
	numRefs, reorder int
	initQP           int
	sps, pps         []byte

	frameCount int64 // input frames
	coded      int64 // coded pictures (DTS)
	sinceIDR   int
	frameNum   int
	idrID      int

	srcPool         []*srcPic
	curSrc, prevSrc *srcPic
	refs            []*refPic // most recent first
	pool []*refPic
	cur  *refPic

	// Per-macroblock and per-4x4 state of the picture being coded.
	mbs    []mbInfo
	mv     [2][]int16
	ref    [2][]int8
	dbInfo []h264.MBInfo
	refID  [2][]int32
	nextID int32

	slices []*sliceEnc
	db     h264.Deblocker
	rc     rateControl

	// Reference lists of the picture being coded.
	lists [2][]*refPic

	// Lookahead queue for B pictures.
	queue      []queued
	reconLog   [][]byte // display-order reconstructions (tests, B pictures)
	pocBase    int64
	pendingIDR bool
}

type queued struct {
	img *srcPic
	pts int64
}

const (
	lumaPad   = 64
	chromaPad = 32
)

// refPic is a reconstructed picture with padded planes.
type refPic struct {
	y, cb, cr []byte
	ys, cs    int
	yo, co    int // offset of sample (0, 0)
	id        int32
	poc       int
	frameNum  int
	// Per-4x4 L0 motion of the picture (temporal candidates / direct).
	mv     [2][]int16
	ref    [2][]int8
	refPOC [2][]int32
	intra  []bool // per macroblock
}

// srcPic is a source picture padded to whole macroblocks.
type srcPic struct {
	y, cb, cr []byte
	ys, cs    int
}

// New returns an encoder for cfg.
func New(cfg Config) (*Encoder, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width%2 != 0 || cfg.Height%2 != 0 {
		return nil, errors.New("h264enc: width and height must be positive and even")
	}
	if cfg.FPSNum <= 0 || cfg.FPSDen <= 0 {
		cfg.FPSNum, cfg.FPSDen = 30, 1
	}
	if cfg.Profile == 0 {
		cfg.Profile = ProfileBaseline
	}
	if cfg.Profile == ProfileBaseline {
		cfg.CABAC, cfg.Transform8x8, cfg.BFrames = false, false, 0
	}
	if cfg.Profile != ProfileHigh {
		cfg.Transform8x8 = false
	}
	if cfg.QP == 0 {
		cfg.QP = 26
	}
	cfg.QP = min(max(cfg.QP, 0), 51)
	if cfg.KeyInt <= 0 {
		cfg.KeyInt = 250
	}
	if cfg.Refs <= 0 {
		cfg.Refs = 1
	}
	cfg.Refs = min(cfg.Refs, 16)
	cfg.BFrames = min(max(cfg.BFrames, 0), 16)
	if cfg.Matrix == 0 {
		cfg.Matrix = 1
	}
	if cfg.Slices <= 0 {
		cfg.Slices = 1
	}
	e := &Encoder{cfg: cfg}
	e.mbW, e.mbH = (cfg.Width+15)/16, (cfg.Height+15)/16
	cfg.Slices = min(cfg.Slices, e.mbH)
	e.cfg.Slices = cfg.Slices
	e.numRefs = cfg.Refs
	if cfg.BFrames > 0 {
		e.pocType = 0
		e.reorder = 1
		e.numRefs = max(e.numRefs, 2)
	} else {
		e.pocType = 2
	}
	e.initQP = cfg.QP
	fps := float64(cfg.FPSNum) / float64(cfg.FPSDen)
	kbps := cfg.Bitrate / 1000
	if cfg.VBVMaxRate > 0 {
		kbps = cfg.VBVMaxRate / 1000
	}
	e.level = pickLevel(e.mbW, e.mbH, fps, e.numRefs+e.reorder, kbps, cfg.Profile >= ProfileHigh)
	e.sps = e.writeSPS()
	e.pps = e.writePPS()

	n := e.mbW * e.mbH
	e.mbs = make([]mbInfo, n)
	e.dbInfo = make([]h264.MBInfo, n)
	for l := 0; l < 2; l++ {
		e.mv[l] = make([]int16, n*32)
		e.ref[l] = make([]int8, n*16)
		e.refID[l] = make([]int32, n*16)
	}
	rows := e.mbH / cfg.Slices
	for i := 0; i < cfg.Slices; i++ {
		s := &sliceEnc{e: e, mc: h264.NewMC()}
		s.firstRow = i * rows
		s.endRow = (i + 1) * rows
		if i == cfg.Slices-1 {
			s.endRow = e.mbH
		}
		e.slices = append(e.slices, s)
	}
	e.rc.init(e)
	return e, nil
}

// Headers returns the SPS and PPS NAL units (for avcC).
func (e *Encoder) Headers() (sps, pps []byte) { return e.sps, e.pps }

// Level returns the chosen level_idc.
func (e *Encoder) Level() int { return e.level }

// Width and Height report the configured size.
func (e *Encoder) Width() int  { return e.cfg.Width }
func (e *Encoder) Height() int { return e.cfg.Height }

// Config returns the effective configuration.
func (e *Encoder) Config() Config { return e.cfg }

// Encode codes one 4:2:0 frame. With B pictures enabled, packets come out
// in decoding order and may lag the input; call Flush at the end.
func (e *Encoder) Encode(img *image.YCbCr) ([]Packet, error) {
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return nil, errors.New("h264enc: only 4:2:0 input is supported")
	}
	b := img.Rect
	if b.Dx() < e.cfg.Width || b.Dy() < e.cfg.Height {
		return nil, errors.New("h264enc: frame smaller than the configured size")
	}
	src := e.loadSource(img)
	pts := e.frameCount
	e.frameCount++
	if e.cfg.BFrames == 0 {
		p := e.codePicture(src, pts, pts, e.pictureType(), true)
		return []Packet{p}, nil
	}
	return e.queueB(src, pts), nil
}

// Flush codes any queued pictures.
func (e *Encoder) Flush() ([]Packet, error) {
	if e.cfg.BFrames == 0 {
		return nil, nil
	}
	return e.flushB(), nil
}

// pictureType decides I or P for the next picture in coding order.
func (e *Encoder) pictureType() int {
	if e.coded == 0 || e.sinceIDR >= e.cfg.KeyInt {
		return sliceI
	}
	return sliceP
}

// loadSource copies img into macroblock-aligned planes, replicating edges.
func (e *Encoder) loadSource(img *image.YCbCr) *srcPic {
	var s *srcPic
	if n := len(e.srcPool); n > 0 {
		s = e.srcPool[n-1]
		e.srcPool = e.srcPool[:n-1]
	} else {
		s = &srcPic{ys: e.mbW * 16, cs: e.mbW * 8}
		s.y = make([]byte, s.ys*e.mbH*16)
		s.cb = make([]byte, s.cs*e.mbH*8)
		s.cr = make([]byte, s.cs*e.mbH*8)
	}
	w, h := e.cfg.Width, e.cfg.Height
	copyPlane(s.y, s.ys, e.mbW*16, e.mbH*16, img.Y[img.YOffset(img.Rect.Min.X, img.Rect.Min.Y):], img.YStride, w, h)
	co := img.COffset(img.Rect.Min.X, img.Rect.Min.Y)
	copyPlane(s.cb, s.cs, e.mbW*8, e.mbH*8, img.Cb[co:], img.CStride, w/2, h/2)
	copyPlane(s.cr, s.cs, e.mbW*8, e.mbH*8, img.Cr[co:], img.CStride, w/2, h/2)
	return s
}

func copyPlane(dst []byte, ds, dw, dh int, src []byte, ss, w, h int) {
	for y := 0; y < dh; y++ {
		sy := min(y, h-1)
		row := dst[y*ds : y*ds+dw]
		copy(row, src[sy*ss:sy*ss+w])
		for x := w; x < dw; x++ {
			row[x] = row[w-1]
		}
	}
}

// newRef returns a reconstruction buffer.
func (e *Encoder) newRef() *refPic {
	var p *refPic
	if n := len(e.pool); n > 0 {
		p = e.pool[n-1]
		e.pool = e.pool[:n-1]
	} else {
		w, h := e.mbW*16, e.mbH*16
		p = &refPic{ys: w + 2*lumaPad, cs: w/2 + 2*chromaPad}
		p.y = make([]byte, p.ys*(h+2*lumaPad))
		p.cb = make([]byte, p.cs*(h/2+2*chromaPad))
		p.cr = make([]byte, p.cs*(h/2+2*chromaPad))
		p.yo = lumaPad*p.ys + lumaPad
		p.co = chromaPad*p.cs + chromaPad
		n := e.mbW * e.mbH
		for l := 0; l < 2; l++ {
			p.mv[l] = make([]int16, n*32)
			p.ref[l] = make([]int8, n*16)
			p.refPOC[l] = make([]int32, n*16)
		}
		p.intra = make([]bool, n)
	}
	e.nextID++
	p.id = e.nextID
	return p
}

// padEdges replicates the picture border into the padding.
func (p *refPic) padEdges(w, h int) {
	padPlane(p.y, p.ys, p.yo, w, h, lumaPad)
	padPlane(p.cb, p.cs, p.co, w/2, h/2, chromaPad)
	padPlane(p.cr, p.cs, p.co, w/2, h/2, chromaPad)
}

func padPlane(b []byte, s, o, w, h, pad int) {
	for y := 0; y < h; y++ {
		r := b[o+y*s-pad : o+y*s+w+pad]
		l, rr := r[pad], r[pad+w-1]
		for i := 0; i < pad; i++ {
			r[i] = l
			r[pad+w+i] = rr
		}
	}
	top := b[o-pad : o-pad+s]
	for y := 1; y <= pad; y++ {
		copy(b[o-pad-y*s:], top[:w+2*pad])
	}
	bot := b[o-pad+(h-1)*s : o-pad+(h-1)*s+w+2*pad]
	for y := 0; y < pad; y++ {
		copy(b[o-pad+(h+y)*s:], bot)
	}
}

// codePicture codes src as a picture of type typ and returns its packet.
func (e *Encoder) codePicture(src *srcPic, pts, poc int64, typ int, isRef bool) Packet {
	idr := typ == sliceI && (e.coded == 0 || e.sinceIDR >= e.cfg.KeyInt || e.cfg.BFrames == 0)
	if typ == sliceI && e.cfg.BFrames > 0 {
		idr = e.pendingIDR
	}
	if idr {
		e.sinceIDR = 0
		e.frameNum = 0
		for _, r := range e.refs {
			e.pool = append(e.pool, r)
		}
		e.refs = e.refs[:0]
		e.pocBase = poc
	}
	e.curSrc = src
	qp := e.rc.pictureQP(typ, e)
	cur := e.newRef()
	cur.poc = int(2 * (poc - e.pocBase))
	cur.frameNum = e.frameNum
	e.cur = cur
	hdr := sliceHeader{
		typ: typ, idr: idr, frameNum: e.frameNum, idrPicID: e.idrID & 0xffff,
		pocLsb: cur.poc, qp: qp, numRefs: min(len(e.refs), e.numRefs),
	}
	if isRef {
		hdr.refIdc = 3
		if typ == sliceP {
			hdr.refIdc = 2
		}
	}
	if typ == sliceB {
		hdr.numRefs = 1
		hdr.numRefsL1 = 1
		hdr.directSpat = true
		hdr.refIdc = 0
	}
	if typ == sliceI {
		hdr.numRefs = 0
	}
	e.setupLists(typ)
	var nals [][]byte
	bits := 0
	for attempt := 0; ; attempt++ {
		nals = e.codeSlices(src, &hdr)
		bits = 0
		if idr {
			bits = 8 * (len(e.sps) + len(e.pps))
		}
		for _, n := range nals {
			bits += 8 * len(n)
		}
		if !e.rc.vbvOverflow(bits) || hdr.qp >= 51 {
			break
		}
		// The picture would underflow the VBV: re-encode it coarser.
		hdr.qp = min(51, hdr.qp+3+2*attempt)
	}
	qp = hdr.qp
	e.deblockAndStore(cur)
	e.rc.update(typ, qp, bits, e)

	pkt := Packet{PTS: pts, DTS: e.coded, Key: idr, QP: qp}
	switch typ {
	case sliceI:
		pkt.Type = 'I'
	case sliceP:
		pkt.Type = 'P'
	default:
		pkt.Type = 'B'
	}
	if idr {
		pkt.NALs = append(pkt.NALs, e.sps, e.pps)
		e.idrID++
	}
	pkt.NALs = append(pkt.NALs, nals...)
	e.coded++
	e.sinceIDR++
	if isRef {
		e.frameNum = (e.frameNum + 1) & (1<<log2MaxFrameNum - 1)
		e.refs = append([]*refPic{cur}, e.refs...)
		if len(e.refs) > e.numRefs {
			e.pool = append(e.pool, e.refs[e.numRefs:]...)
			e.refs = e.refs[:e.numRefs]
		}
	} else {
		e.pool = append(e.pool, cur)
	}
	if e.prevSrc != nil {
		e.srcPool = append(e.srcPool, e.prevSrc)
	}
	e.prevSrc, e.curSrc = src, nil
	e.cur = nil
	return pkt
}

// setupLists builds the reference lists of the next picture.
func (e *Encoder) setupLists(typ int) {
	e.lists[0], e.lists[1] = nil, nil
	switch typ {
	case sliceP:
		e.lists[0] = e.refs
	case sliceB:
		e.setupBLists()
	}
}

// deblockAndStore deblocks the reconstruction and pads it for prediction.
func (e *Encoder) deblockAndStore(cur *refPic) {
	n := e.mbW * e.mbH
	for i := 0; i < n; i++ {
		cur.intra[i] = e.mbs[i].typ&h264.MBIntra4x4|e.mbs[i].typ&h264.MBIntra16x16 != 0
	}
	copy(cur.mv[0], e.mv[0])
	copy(cur.mv[1], e.mv[1])
	copy(cur.ref[0], e.ref[0])
	copy(cur.ref[1], e.ref[1])
	for l := 0; l < 2; l++ {
		for i, r := range e.ref[l] {
			id, poc := int32(-1), int32(0)
			if r >= 0 {
				rp := e.lists[l][r]
				id, poc = rp.id, int32(rp.poc)
			}
			e.refID[l][i] = id
			cur.refPOC[l][i] = poc
		}
	}
	if !e.cfg.NoDeblock {
		var sl []h264.SliceDeblock
		for range e.slices {
			sl = append(sl, h264.SliceDeblock{
				AlphaOffset:  2 * e.cfg.DeblockAlpha,
				BetaOffset:   2 * e.cfg.DeblockBeta,
				ChromaOffset: [2]int{e.cfg.ChromaQPOffset, e.cfg.ChromaQPOffset},
			})
		}
		e.db.Deblock(cur.y[cur.yo:], cur.cb[cur.co:], cur.cr[cur.co:], cur.ys, cur.cs,
			e.mbW, e.mbH, e.dbInfo, sl, e.mv, e.refID)
	}
	cur.padEdges(e.mbW*16, e.mbH*16)
}

// Recon returns the last reconstructed reference picture (display size),
// for tests.
func (e *Encoder) Recon() *h264.Frame {
	if len(e.refs) == 0 {
		return nil
	}
	return e.frameOf(e.refs[0])
}

func (e *Encoder) frameOf(p *refPic) *h264.Frame {
	w, h := e.cfg.Width, e.cfg.Height
	f := &h264.Frame{Width: w, Height: h}
	f.Y = &h264.Plane{Pix: p.y[p.yo:], Stride: p.ys, W: w, H: h}
	f.Cb = &h264.Plane{Pix: p.cb[p.co:], Stride: p.cs, W: w / 2, H: h / 2}
	f.Cr = &h264.Plane{Pix: p.cr[p.co:], Stride: p.cs, W: w / 2, H: h / 2}
	return f
}

// lambda is the SATD-domain Lagrange multiplier per QP.
var lambdaTab [52]int

// lambda2 is the SSD-domain multiplier (lambda^2-ish, in 1/256 units).
var lambda2Tab [52]int

func init() {
	for qp := range lambdaTab {
		l := math.Pow(2, float64(qp-12)/6)
		lambdaTab[qp] = max(1, int(l+0.5))
		lambda2Tab[qp] = int(0.85 * math.Pow(2, float64(qp-12)/3) * 256)
	}
}
