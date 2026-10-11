package hevc

import (
	"errors"
	"image"
	"math"
)

// The HEVC encoder. It keeps its reconstruction in an embedded Decoder:
// parameter sets and slice headers are parsed back with the decoder's own
// parsers, CU decisions are reconstructed with the decoder's prediction,
// transform and state functions, and pictures go through the decoder's
// DPB and in-loop filters. Its reconstruction is therefore, by
// construction, what a decoder produces from its stream.

// EncoderConfig describes an encoding session. Zero values pick defaults.
type EncoderConfig struct {
	Width, Height  int
	FPSNum, FPSDen int // frame rate (default 30/1)

	// Rate control: constant QP when Bitrate and CRF are zero.
	QP      int     // 0..51 (default 30; also the starting QP of ABR)
	CRF     float64 // constant rate factor (x264/x265-like scale), 0 = off
	Bitrate int     // average bits/s (ABR), 0 = off
	// VBV: maximum bits/s and buffer size in bits (0 = unconstrained).
	VBVMaxRate, VBVBuffer int

	KeyInt int // frames between IDR pictures (default 250)
	Refs   int // reference pictures for P pictures (default 1)
	// BFrames is the number of non-reference B pictures between anchors
	// (0 = P pictures only). B pictures predict from the previous Refs
	// anchors and the next one.
	BFrames int

	// Preset trades speed for compression: 0 fastest ... 2 best (default 1).
	Preset int
	CTU    int // CTU size 16, 32 or 64 (default 32)

	NoSAO, NoDeblock, NoSignHiding bool

	// Parallel tools: wavefront parallel processing, uniform tiles, slices
	// per picture (by CTB rows) and, with DependentSlices, one dependent
	// slice segment per CTB row inside each slice.
	WPP                bool
	TileCols, TileRows int
	Slices             int
	DependentSlices    bool

	FullRange bool
	Matrix    int // H.273 matrix_coefficients: 1 BT.709 (default), 6 BT.601
}

// Packet is one coded picture.
type Packet struct {
	NALs [][]byte // VPS/SPS/PPS precede IDR pictures, then slice segments
	Key  bool
	PTS  int64 // input frame index
	DTS  int64
	Type byte // 'I', 'P' or 'B'
	QP   int
}

// Encoder encodes I420 frames to HEVC Main.
type Encoder struct {
	cfg           EncoderConfig
	seq           encSeq
	vps, sps, pps []byte
	d             *Decoder

	srcY, srcCb, srcCr []byte // padded source of the picture being coded
	ys, cs             int

	frameCount int64
	coded      int64
	keyPTS     int64       // input index of the last IDR picture
	anchors    []int       // POCs of the reference pictures since the IDR
	queue      []*encFrame // frames waiting for their mini-GOP's anchor
	framePool  []*encFrame

	cab    cabacEnc
	bw     bitWriter
	est    cabacEst
	lambda float64 // SSD + lambda * bits
	sqrtL  float64 // SATD + sqrtL * bits

	snaps [16]encSnap
	tq    encTQ
	rc    rateControl

	arena    []int32  // coefficient levels of the picture being coded
	roots    []*encCU // decided coding quadtrees by CTB (raster)
	saoMerge []int8   // per CTB: 0 explicit SAO, 1 merge left, 2 merge up

	keepRecon bool
	recon     []*Frame
}

// allocLevels returns n zeroed levels from the picture's arena. Levels
// must outlive the decision pass until the picture is written; growing
// allocates a new block (earlier slices keep the old one alive).
func (e *Encoder) allocLevels(n int) []int32 {
	if len(e.arena)+n > cap(e.arena) {
		e.arena = make([]int32, 0, max(2*cap(e.arena), 1<<16))
	}
	l := e.arena[len(e.arena) : len(e.arena)+n]
	e.arena = e.arena[:len(e.arena)+n]
	clear(l)
	return l
}

// NewEncoder returns an encoder for cfg.
func NewEncoder(cfg EncoderConfig) (*Encoder, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width%2 != 0 || cfg.Height%2 != 0 {
		return nil, errors.New("hevc: frame size must be positive and even")
	}
	if cfg.FPSNum == 0 || cfg.FPSDen == 0 {
		cfg.FPSNum, cfg.FPSDen = 30, 1
	}
	if cfg.QP == 0 {
		cfg.QP = 30
	}
	cfg.QP = clip3(0, 51, cfg.QP)
	if cfg.KeyInt <= 0 {
		cfg.KeyInt = 250
	}
	if cfg.Refs <= 0 {
		cfg.Refs = 1
	}
	if cfg.Preset == 0 && cfg.CTU == 0 {
		cfg.Preset = 1
	}
	switch cfg.CTU {
	case 0:
		cfg.CTU = 32
	case 16, 32, 64:
	default:
		return nil, errors.New("hevc: CTU size must be 16, 32 or 64")
	}
	if cfg.Matrix == 0 {
		cfg.Matrix = 1
	}
	cfg.TileCols, cfg.TileRows = max(cfg.TileCols, 1), max(cfg.TileRows, 1)
	cfg.Slices = max(cfg.Slices, 1)
	e := &Encoder{cfg: cfg}
	q := &e.seq
	q.width = (cfg.Width + 7) &^ 7
	q.height = (cfg.Height + 7) &^ 7
	q.confRight, q.confBottom = q.width-cfg.Width, q.height-cfg.Height
	q.log2Ctb = log2(cfg.CTU)
	q.log2MaxTb = min(5, q.log2Ctb)
	q.maxTrDepthIntra, q.maxTrDepthInter = 1, 1
	q.sao = !cfg.NoSAO
	q.sdh = !cfg.NoSignHiding
	q.tmvp = true
	q.log2MaxPOCLsb = 8
	cfg.BFrames = clip3(0, 7, cfg.BFrames)
	e.cfg.BFrames = cfg.BFrames
	q.numReorder = 0
	q.maxDecPicBuf = cfg.Refs + 1
	if cfg.BFrames > 0 {
		q.numReorder = 1
		q.maxDecPicBuf = cfg.Refs + 2
	}
	q.level = pickLevel(q.width, q.height, cfg.FPSNum, cfg.FPSDen)
	q.initQP = cfg.QP
	q.deblockOff = cfg.NoDeblock
	q.wpp = cfg.WPP
	ctbW := (q.width + cfg.CTU - 1) / cfg.CTU
	ctbH := (q.height + cfg.CTU - 1) / cfg.CTU
	q.tileCols, q.tileRows = min(cfg.TileCols, ctbW), min(cfg.TileRows, ctbH)
	q.dependentSlices = cfg.DependentSlices
	q.numRefL0, q.numRefL1 = cfg.Refs, 1
	q.fpsNum, q.fpsDen = cfg.FPSNum, cfg.FPSDen
	q.fullRange, q.matrix = cfg.FullRange, cfg.Matrix
	e.vps, e.sps, e.pps = q.vps(), q.sps(), q.pps()
	e.d = New()
	for _, n := range [][]byte{e.vps, e.sps, e.pps} {
		if err := e.d.DecodeNAL(n); err != nil {
			return nil, err
		}
	}
	e.d.pps[0].derive(e.d.sps[0])
	e.ys, e.cs = q.width, q.width/2
	e.rc.init(e)
	return e, nil
}

// pickLevel returns general_level_idc for the picture size and rate.
func pickLevel(w, h, num, den int) int {
	samples := w * h
	rate := float64(samples) * float64(num) / float64(den)
	levels := []struct {
		idc     int
		maxPic  int
		maxRate float64
	}{
		{30, 36864, 552960}, {60, 122880, 3686400}, {63, 245760, 7372800},
		{90, 552960, 16588800}, {93, 983040, 33177600}, {120, 2228224, 66846720},
		{123, 2228224, 133693440}, {150, 8912896, 267386880}, {153, 8912896, 534773760},
		{156, 8912896, 1069547520}, {183, 35651584, 2139095040},
	}
	for _, l := range levels {
		if samples <= l.maxPic && rate <= l.maxRate {
			return l.idc
		}
	}
	return 186
}

// Headers returns the VPS, SPS and PPS NAL units.
func (e *Encoder) Headers() (vps, sps, pps []byte) { return e.vps, e.sps, e.pps }

// encFrame is a padded source frame.
type encFrame struct {
	y, cb, cr []byte
	pts       int64
}

// Encode codes one frame. With B pictures, frames are held until their
// mini-GOP's anchor arrives, so a call may return no packets or several
// (in decoding order).
func (e *Encoder) Encode(img *image.YCbCr) ([]Packet, error) {
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return nil, errors.New("hevc: only 4:2:0 input is supported")
	}
	if img.Rect.Dx() < e.cfg.Width || img.Rect.Dy() < e.cfg.Height {
		return nil, errors.New("hevc: frame smaller than the configured size")
	}
	f := e.loadSource(img)
	f.pts = e.frameCount
	e.frameCount++
	key := e.coded == 0 && len(e.queue) == 0 || f.pts-e.keyPTS >= int64(e.cfg.KeyInt)
	if key {
		pkts := e.flushQueue()
		return append(pkts, e.codePicture(f, sliceI, true, true)), nil
	}
	if e.cfg.BFrames == 0 {
		return []Packet{e.codePicture(f, sliceP, false, true)}, nil
	}
	e.queue = append(e.queue, f)
	if len(e.queue) == e.cfg.BFrames+1 {
		return e.flushQueue(), nil
	}
	return nil, nil
}

// flushQueue codes the queued frames: the last as the anchor (P), then
// the others as B pictures in display order.
func (e *Encoder) flushQueue() []Packet {
	if len(e.queue) == 0 {
		return nil
	}
	n := len(e.queue)
	pkts := []Packet{e.codePicture(e.queue[n-1], sliceP, false, true)}
	for _, f := range e.queue[:n-1] {
		pkts = append(pkts, e.codePicture(f, sliceB, false, false))
	}
	e.framePool = append(e.framePool, e.queue...)
	e.queue = e.queue[:0]
	return pkts
}

// Flush codes any frames still held.
func (e *Encoder) Flush() ([]Packet, error) { return e.flushQueue(), nil }

// loadSource copies img into padded source planes, repeating the last
// column and row into the padding.
func (e *Encoder) loadSource(img *image.YCbCr) *encFrame {
	q := &e.seq
	var f *encFrame
	if n := len(e.framePool); n > 0 {
		f = e.framePool[n-1]
		e.framePool = e.framePool[:n-1]
	} else {
		f = &encFrame{y: make([]byte, q.width*q.height), cb: make([]byte, q.width*q.height/4), cr: make([]byte, q.width*q.height/4)}
	}
	w, h := e.cfg.Width, e.cfg.Height
	for y := 0; y < q.height; y++ {
		sy := min(y, h-1)
		row := img.Y[img.YOffset(img.Rect.Min.X, img.Rect.Min.Y+sy):]
		dst := f.y[y*e.ys : y*e.ys+q.width]
		copy(dst, row[:w])
		for x := w; x < q.width; x++ {
			dst[x] = dst[w-1]
		}
	}
	cw, ch := w/2, h/2
	for y := 0; y < q.height/2; y++ {
		sy := min(y, ch-1)
		off := img.COffset(img.Rect.Min.X, img.Rect.Min.Y+2*sy)
		for c, src := range [2][]byte{img.Cb, img.Cr} {
			dst := f.cb
			if c == 1 {
				dst = f.cr
			}
			d := dst[y*e.cs : y*e.cs+q.width/2]
			copy(d, src[off:off+cw])
			for x := cw; x < q.width/2; x++ {
				d[x] = d[cw-1]
			}
		}
	}
	return f
}

// segment is one slice segment of a picture: CTBs [start, end) in tile
// scan order.
type segment struct {
	start, end int
	dependent  bool
}

// segments splits the picture into slices (by CTB rows of the picture
// in raster order, aligned to tiles when tiles are used) and dependent
// segments.
func (e *Encoder) segments() []segment {
	sp := e.d.sps[0]
	n := sp.PicWCtb * sp.PicHCtb
	if e.cfg.Slices <= 1 && !e.cfg.DependentSlices {
		return []segment{{0, n, false}}
	}
	rowsPer := max(1, (sp.PicHCtb+e.cfg.Slices-1)/e.cfg.Slices)
	var out []segment
	if e.seq.tileCols > 1 || e.seq.tileRows > 1 {
		// Slices must contain whole tiles: one slice per tile.
		p := e.d.pps[0]
		start := 0
		for ts := 1; ts <= n; ts++ {
			if ts == n || p.tileID[ts] != p.tileID[ts-1] {
				out = append(out, segment{start, ts, false})
				start = ts
			}
		}
		return out
	}
	for r := 0; r < sp.PicHCtb; r += rowsPer {
		end := min(r+rowsPer, sp.PicHCtb)
		if e.cfg.DependentSlices {
			for rr := r; rr < end; rr++ {
				out = append(out, segment{rr * sp.PicWCtb, (rr + 1) * sp.PicWCtb, rr != r})
			}
		} else {
			out = append(out, segment{r * sp.PicWCtb, end * sp.PicWCtb, false})
		}
	}
	return out
}

// codePicture codes frame f as one picture of slice type typ; ref
// pictures become anchors for later pictures.
func (e *Encoder) codePicture(f *encFrame, typ int, key, ref bool) Packet {
	d := e.d
	q := &e.seq
	e.srcY, e.srcCb, e.srcCr = f.y, f.cb, f.cr
	if key {
		e.keyPTS = f.pts
		e.anchors = e.anchors[:0]
	}
	poc := int(f.pts - e.keyPTS)
	sl := encSlice{nalType: nalTrailR, typ: typ, pocLsb: poc & (1<<q.log2MaxPOCLsb - 1), sao: [2]bool{q.sao, q.sao}, maxMerge: 5}
	switch {
	case key:
		sl.nalType = nalIDRWRADL
	case !ref:
		sl.nalType = nalTrailN
	}
	if typ != sliceI {
		// Short-term RPS: the last Refs anchors before the picture, and
		// for B pictures the anchor after it.
		var before []int
		for i := len(e.anchors) - 1; i >= 0 && len(before) < e.cfg.Refs; i-- {
			if a := e.anchors[i]; a < poc {
				before = append(before, a)
			}
		}
		for _, a := range before {
			sl.negDelta = append(sl.negDelta, a-poc)
			sl.negUsed = append(sl.negUsed, true)
		}
		sl.numRefIdx[0] = len(before)
		if typ == sliceB {
			for _, a := range e.anchors {
				if a > poc {
					sl.posDelta = append(sl.posDelta, a-poc)
					sl.posUsed = append(sl.posUsed, true)
					break
				}
			}
			sl.numRefIdx[1] = len(sl.posDelta)
		}
		sl.tmvp = q.tmvp
		sl.colFromL0 = typ == sliceP
	}
	pkt := Packet{Key: key, PTS: f.pts, DTS: e.coded, Type: "BPI"[typ]}
	qp := e.rc.pictureQP(typ, e)
	bits := 0
	for attempt := 0; ; attempt++ {
		pkt.NALs = pkt.NALs[:0]
		if key {
			pkt.NALs = append(pkt.NALs, e.vps, e.sps, e.pps)
		}
		pkt.QP = qp
		e.lambda = 0.57 * math.Pow(2, float64(qp-12)/3)
		if typ != sliceI {
			e.lambda = 0.68 * math.Pow(2, float64(qp-12)/3)
		}
		e.sqrtL = math.Sqrt(e.lambda)
		sl.qpDelta = qp - q.initQP
		bits = e.codeSlices(&pkt, sl, attempt > 0)
		// A picture that overflows the VBV buffer is coded again at a
		// higher QP; the picture was started already, so only its frame
		// context is reset.
		if !e.rc.vbvOverflow(bits) || qp >= 51 || attempt >= 4 {
			break
		}
		qp = min(51, qp+2+attempt)
	}
	e.rc.update(typ, qp, bits, e)
	d.EndOfPicture()
	if e.keepRecon {
		for f := d.Next(); f != nil; f = d.Next() {
			e.recon = append(e.recon, f)
		}
	}
	if ref {
		e.anchors = append(e.anchors, poc)
		if len(e.anchors) > e.cfg.Refs+1 {
			e.anchors = e.anchors[1:]
		}
	}
	e.coded++
	return pkt
}

// codeSlices decides, filters and writes the slice segments of the
// current picture, appending their NAL units to pkt, and returns the
// picture's coded bits. restart re-codes a picture already started.
func (e *Encoder) codeSlices(pkt *Packet, sl encSlice, restart bool) int {
	d := e.d
	picSize := d.sps[0].PicWCtb * d.sps[0].PicHCtb
	segs := e.segments()
	es := make([]encSeg, len(segs))
	for i, sg := range segs {
		es[i].sl = sl
		es[i].sl.first = i == 0
		es[i].sl.dependent = sg.dependent
		es[i].sl.address = d.pps[0].ctbTsToRs[sg.start]
		es[i].sg = sg
	}
	// Pass A: decide every CTU (reconstructing it), with the estimator's
	// contexts evolving as the coder's will.
	e.arena = e.arena[:0]
	if len(e.roots) < picSize {
		e.roots = make([]*encCU, picSize)
		e.saoMerge = make([]int8, picSize)
	}
	var ca ctxStore
	for i := range es {
		e.beginSegment(&es[i], picSize, restart && i == 0)
		e.walkSegment(&es[i], &e.est.ctx, &ca, func(x0, y0, rs int) {
			root, _ := e.decideQT(x0, y0, d.active.Log2Ctb, 0)
			e.roots[rs] = root
		}, nil)
	}
	// In-loop filter decisions: deblock here, choose SAO from the
	// deblocked picture, and let the decoder's filter job apply SAO only.
	d.waitFilter()
	d.lf.noDeblock = false
	if sl.sao[0] || sl.sao[1] {
		e.deblockPicture()
		e.decideSAO(sl.sao)
		d.lf.noDeblock = true
	}
	// Pass B: entropy coding.
	var cb ctxStore
	bits := 0
	for i := range es {
		nal := e.writeSegment(&es[i], picSize, &cb)
		pkt.NALs = append(pkt.NALs, nal)
		bits += 8 * len(nal)
	}
	return bits
}

// encSeg is one slice segment of the picture being coded.
type encSeg struct {
	sl       encSlice
	sg       segment
	h        *sliceHeader
	sliceIdx int
}

// ctxStore holds the contexts carried between CTUs: the WPP storage and
// the end of the previous slice segment (for dependent segments).
type ctxStore struct {
	wpp      [numCtx]uint8
	wppValid bool
	ds       [numCtx]uint8
	dsValid  bool
}

// beginSegment parses the segment's header back and sets up the decoder
// state for it (a picture start for the first segment).
func (e *Encoder) beginSegment(es *encSeg, picSize int, restart bool) {
	d := e.d
	var hw bitWriter
	e.seq.header(&hw, &es.sl, picSize)
	hnal := nalUnit(es.sl.nalType, hw.buf)
	var r bitReader
	r.reset(unescape(nil, hnal[2:]))
	h, err := parseSliceHeader(&r, hnal, d.sps[:], d.pps[:], d.lastIndep)
	if err != nil {
		panic("hevc: encoder wrote an unparsable slice header: " + err.Error())
	}
	if restart {
		d.fc.reset(h.sps)
		d.lastIndep = nil
	} else if h.firstInPic {
		d.EndOfPicture()
		d.lastIndep = nil
		if err := d.startPicture(h); err != nil {
			panic("hevc: encoder picture start: " + err.Error())
		}
		// Motion search reads reference planes directly.
		for _, p := range d.dpb {
			p.prog.wait()
		}
	}
	if !h.dependent {
		d.lastIndep = h
	}
	fc := d.fc
	fc.slices = append(fc.slices, h)
	es.h, es.sliceIdx = h, len(fc.slices)-1
	if !h.dependent {
		e.useSegment(es)
		d.buildRefLists(h)
	}
}

// useSegment points the slice state at segment es.
func (e *Encoder) useSegment(es *encSeg) {
	d := e.d
	s := &d.sd
	h := es.h
	s.d, s.h, s.sps, s.pps, s.pic = d, h, h.sps, h.pps, d.cur
	s.sliceIdx = es.sliceIdx
}

// walkSegment visits the CTUs of a segment in decoding order, keeping ctx
// as the decoder initialises and synchronises its contexts (9.3.1) and
// recording the slice addresses. fn handles each CTU; sub, when not nil,
// is called after each CTU with whether it ends the segment (true) or a
// substream (false), or not called when neither.
func (e *Encoder) walkSegment(es *encSeg, ctx *[numCtx]uint8, st *ctxStore, fn func(x0, y0, rs int), sub func(ctx *[numCtx]uint8, endSeg bool)) {
	d := e.d
	e.useSegment(es)
	s := &d.sd
	sp, p, h := s.sps, s.pps, s.h
	fc := d.fc
	sliceQP := p.InitQP + h.qpDelta
	initType := 0
	switch h.typ {
	case sliceP:
		initType = 1
	case sliceB:
		initType = 2
	}
	s.qpY, s.qpYPred = sliceQP, sliceQP
	s.firstQG = true
	sliceAddr := h.address
	if h.dependent {
		sliceAddr = d.sliceAddrRs
	} else {
		d.sliceAddrRs = h.address
	}
	sg := es.sg
	first := true
	for ts := sg.start; ts < sg.end; ts++ {
		rs := p.ctbTsToRs[ts]
		s.ctbAddrRs, s.ctbAddrTs = rs, ts
		s.ctbX, s.ctbY = rs%sp.PicWCtb, rs/sp.PicWCtb
		fc.ctbSliceAddr[rs] = sliceAddr
		fc.ctbSlice[rs] = s.sliceIdx
		x0, y0 := s.ctbX<<sp.Log2Ctb, s.ctbY<<sp.Log2Ctb
		tileStart := ts == 0 || p.tileID[ts] != p.tileID[ts-1]
		rowStart := p.EntropySync && s.ctbX == p.colBd[s.tileCol()]
		switch {
		case tileStart:
			initContexts(ctx, initType, sliceQP)
		case rowStart:
			initContexts(ctx, initType, sliceQP)
			if st.wppValid && s.available(x0, y0, x0+sp.CtbSize, y0-sp.CtbSize) {
				*ctx = st.wpp
			}
		case first && h.dependent && st.dsValid:
			*ctx = st.ds
		case first:
			initContexts(ctx, initType, sliceQP)
		}
		if tileStart || rowStart {
			s.firstQG = true
			s.qpY, s.qpYPred = sliceQP, sliceQP
		}
		first = false
		fn(x0, y0, rs)
		end := ts == sg.end-1
		if p.EntropySync {
			col := s.tileCol()
			if s.ctbX == p.colBd[col]+1 || (p.colBd[col+1]-p.colBd[col] == 1 && s.ctbX == p.colBd[col]) {
				st.wpp = *ctx
				st.wppValid = true
			}
		}
		if end {
			st.ds = *ctx
			st.dsValid = true
			if sub != nil {
				sub(ctx, true)
			}
			break
		}
		nextRs := p.ctbTsToRs[ts+1]
		newTile := p.tileID[ts+1] != p.tileID[ts]
		newRow := p.EntropySync && nextRs%sp.PicWCtb == p.colBd[s.tileColOf(nextRs%sp.PicWCtb)]
		if (newTile || newRow) && sub != nil {
			sub(ctx, false)
		} else if sub != nil {
			e.cab.terminate(0) // end_of_slice_segment_flag = 0
		}
	}
}

// writeSegment entropy-codes a decided segment and returns its NAL unit.
func (e *Encoder) writeSegment(es *encSeg, picSize int, st *ctxStore) []byte {
	d := e.d
	q := &e.seq
	s := &d.sd
	fc := d.fc
	e.bw.reset()
	e.cab.start(&e.bw)
	var subs []int
	subStart := 0
	e.walkSegment(es, &e.cab.ctx, st, func(x0, y0, rs int) {
		if s.h.saoLuma || s.h.saoChroma {
			s.writeSAO(&e.cab, &fc.sao[rs], int(e.saoMerge[rs]))
		}
		s.writeQuadtree(&e.cab, e.roots[rs], 0)
	}, func(ctx *[numCtx]uint8, endSeg bool) {
		if endSeg {
			e.cab.terminate(1) // end_of_slice_segment_flag
			return
		}
		e.cab.terminate(0)
		e.cab.terminate(1) // end_of_subset_one_bit
		e.bw.alignZero()
		subs = append(subs, len(e.bw.buf)-subStart)
		subStart = len(e.bw.buf)
		e.cab.start(&e.bw)
	})
	e.bw.alignZero()
	data := e.bw.buf
	// Entry points count emulation prevention bytes: iterate until the
	// header's offsets match the escaped substream sizes.
	sl := &es.sl
	sl.entries = append(sl.entries[:0], subs...)
	var hw bitWriter
	var nal []byte
	for iter := 0; ; iter++ {
		hw.reset()
		q.header(&hw, sl, picSize)
		hdrLen := len(hw.buf)
		payload := append(hw.buf, data...)
		nal = nalUnit(sl.nalType, payload)
		if len(subs) == 0 || iter > 8 {
			break
		}
		esc := escapedSizes(payload, hdrLen, subs)
		same := true
		for i := range esc {
			if esc[i] != sl.entries[i] {
				same = false
			}
		}
		if same {
			break
		}
		copy(sl.entries, esc)
	}
	return nal
}

// escapedSizes returns the escaped byte counts of the substreams whose
// unescaped sizes are subs, starting at offset start of payload.
func escapedSizes(payload []byte, start int, subs []int) []int {
	out := make([]int, len(subs))
	// Boundaries in unescaped payload offsets.
	bounds := make([]int, len(subs)+1)
	bounds[0] = start
	for i, n := range subs {
		bounds[i+1] = bounds[i] + n
	}
	zeros, k := 0, 0
	for i, b := range payload {
		for k < len(subs) && i >= bounds[k+1] {
			k++
		}
		epb := zeros >= 2 && b <= 3
		if epb {
			zeros = 0
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
		if i >= start && k < len(subs) {
			out[k]++
			if epb {
				out[k]++
			}
		}
	}
	return out
}
