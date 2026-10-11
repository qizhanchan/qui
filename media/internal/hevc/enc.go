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

	QP     int // constant QP 0..51 (default 30)
	KeyInt int // frames between IDR pictures (default 250)
	Refs   int // reference pictures for P pictures (default 1)

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
	sinceKey   int
	poc        int

	cab    cabacEnc
	bw     bitWriter
	est    cabacEst
	lambda float64 // SSD + lambda * bits
	sqrtL  float64 // SATD + sqrtL * bits

	snaps [16]encSnap
	tq    encTQ

	arena []int32 // coefficient levels of the CTU being coded

	keepRecon bool
	recon     []*Frame
}

// allocLevels returns n zeroed levels from the CTU arena. Levels must
// outlive the CTU's decision pass until it is written, so the arena is
// only reset per CTU; growth copies are never needed for correctness
// because slices keep the old backing array alive.
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
	q.numReorder = 0
	q.maxDecPicBuf = cfg.Refs + 1
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
	e.srcY = make([]byte, q.width*q.height)
	e.srcCb = make([]byte, q.width*q.height/4)
	e.srcCr = make([]byte, q.width*q.height/4)
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

// Encode codes one frame.
func (e *Encoder) Encode(img *image.YCbCr) ([]Packet, error) {
	if img.SubsampleRatio != image.YCbCrSubsampleRatio420 {
		return nil, errors.New("hevc: only 4:2:0 input is supported")
	}
	if img.Rect.Dx() < e.cfg.Width || img.Rect.Dy() < e.cfg.Height {
		return nil, errors.New("hevc: frame smaller than the configured size")
	}
	e.loadSource(img)
	pts := e.frameCount
	e.frameCount++
	key := e.coded == 0 || e.sinceKey >= e.cfg.KeyInt
	typ := sliceP
	if key {
		typ = sliceI
	}
	return []Packet{e.codePicture(pts, typ, key)}, nil
}

// Flush returns any pictures still held (none: there is no reordering).
func (e *Encoder) Flush() ([]Packet, error) { return nil, nil }

// loadSource copies img into the padded source planes, repeating the last
// column and row into the padding.
func (e *Encoder) loadSource(img *image.YCbCr) {
	q := &e.seq
	w, h := e.cfg.Width, e.cfg.Height
	for y := 0; y < q.height; y++ {
		sy := min(y, h-1)
		row := img.Y[img.YOffset(img.Rect.Min.X, img.Rect.Min.Y+sy):]
		dst := e.srcY[y*e.ys : y*e.ys+q.width]
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
			dst := e.srcCb
			if c == 1 {
				dst = e.srcCr
			}
			d := dst[y*e.cs : y*e.cs+q.width/2]
			copy(d, src[off:off+cw])
			for x := cw; x < q.width/2; x++ {
				d[x] = d[cw-1]
			}
		}
	}
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

// codePicture codes the loaded source as one picture.
func (e *Encoder) codePicture(pts int64, typ int, key bool) Packet {
	d := e.d
	q := &e.seq
	qp := e.cfg.QP
	if key {
		e.sinceKey = 0
		e.poc = 0
	}
	pkt := Packet{Key: key, PTS: pts, DTS: e.coded, Type: "BPI"[typ], QP: qp}
	if key {
		pkt.NALs = append(pkt.NALs, e.vps, e.sps, e.pps)
	}
	e.lambda = 0.57 * math.Pow(2, float64(qp-12)/3)
	if typ != sliceI {
		e.lambda = 0.68 * math.Pow(2, float64(qp-12)/3)
	}
	e.sqrtL = math.Sqrt(e.lambda)
	sl := encSlice{nalType: nalTrailR, typ: typ, pocLsb: e.poc & (1<<q.log2MaxPOCLsb - 1), sao: [2]bool{q.sao, q.sao}, maxMerge: 5}
	if key {
		sl.nalType = nalIDRWRADL
	}
	if typ != sliceI {
		// Short-term RPS: the previous Refs pictures in coding order.
		nref := min(e.cfg.Refs, e.sinceKey)
		for i := 1; i <= nref; i++ {
			sl.negDelta = append(sl.negDelta, -i)
			sl.negUsed = append(sl.negUsed, true)
		}
		sl.numRefIdx[0] = nref
		sl.tmvp = q.tmvp
		sl.colFromL0 = true
	}
	sl.qpDelta = qp - q.initQP
	segs := e.segments()
	picSize := d.sps[0].PicWCtb * d.sps[0].PicHCtb
	for i, sg := range segs {
		s := sl
		s.first = i == 0
		s.dependent = sg.dependent
		s.address = d.pps[0].ctbTsToRs[sg.start]
		pkt.NALs = append(pkt.NALs, e.codeSegment(&s, sg, picSize))
	}
	d.EndOfPicture()
	if e.keepRecon {
		for f := d.Next(); f != nil; f = d.Next() {
			e.recon = append(e.recon, f)
		}
	}
	e.coded++
	e.sinceKey++
	e.poc++
	return pkt
}

// codeSegment codes one slice segment and returns its NAL unit.
func (e *Encoder) codeSegment(sl *encSlice, sg segment, picSize int) []byte {
	d := e.d
	q := &e.seq
	// Parse the header back to set up the decoder state (entry points do
	// not affect it).
	var hw bitWriter
	q.header(&hw, sl, picSize)
	hnal := nalUnit(sl.nalType, hw.buf)
	var r bitReader
	r.reset(unescape(nil, hnal[2:]))
	h, err := parseSliceHeader(&r, hnal, d.sps[:], d.pps[:], d.lastIndep)
	if err != nil {
		panic("hevc: encoder wrote an unparsable slice header: " + err.Error())
	}
	if h.firstInPic {
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
	s := &d.sd
	s.d, s.h, s.sps, s.pps, s.pic = d, h, h.sps, h.pps, d.cur
	s.sliceIdx = len(fc.slices) - 1
	if !h.dependent {
		d.buildRefLists(h)
	}
	data, subs := e.codeSliceData(sg)
	// Entry points count emulation prevention bytes: iterate until the
	// header's offsets match the escaped substream sizes.
	sl.entries = append(sl.entries[:0], subs...)
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

// codeSliceData codes the CTUs of a segment and returns the slice data
// and the unescaped sizes of the substreams after the first (the entry
// points).
func (e *Encoder) codeSliceData(sg segment) ([]byte, []int) {
	d := e.d
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
	if h.dependent {
		s.qpY, s.qpYPred = d.dsQP[0], d.dsQP[1]
	} else {
		s.qpY, s.qpYPred = sliceQP, sliceQP
		s.firstQG = true
	}
	sliceAddr := h.address
	if h.dependent {
		sliceAddr = d.sliceAddrRs
	} else {
		d.sliceAddrRs = h.address
	}
	e.bw.reset()
	e.cab.start(&e.bw)
	var subs []int
	subStart := 0
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
			initContexts(&e.cab.ctx, initType, sliceQP)
		case rowStart:
			initContexts(&e.cab.ctx, initType, sliceQP)
			if d.wppValid && s.available(x0, y0, x0+sp.CtbSize, y0-sp.CtbSize) {
				e.cab.ctx = d.wppCtx
			}
		case first && h.dependent && d.dsCtxValid:
			e.cab.ctx = d.dsCtx
		case first:
			initContexts(&e.cab.ctx, initType, sliceQP)
		}
		if tileStart || rowStart {
			s.firstQG = true
			s.qpY, s.qpYPred = sliceQP, sliceQP
		}
		first = false
		e.codeCTU(x0, y0)
		end := ts == sg.end-1
		e.cab.terminate(boolInt(end)) // end_of_slice_segment_flag
		if p.EntropySync {
			col := s.tileCol()
			if s.ctbX == p.colBd[col]+1 || (p.colBd[col+1]-p.colBd[col] == 1 && s.ctbX == p.colBd[col]) {
				d.wppCtx = e.cab.ctx
				d.wppValid = true
			}
		}
		if end {
			d.dsCtx = e.cab.ctx
			d.dsCtxValid = true
			d.dsQP = [2]int{s.qpY, s.qpYPred}
			break
		}
		nextRs := p.ctbTsToRs[ts+1]
		newTile := p.tileID[ts+1] != p.tileID[ts]
		newRow := p.EntropySync && nextRs%sp.PicWCtb == p.colBd[s.tileColOf(nextRs%sp.PicWCtb)]
		if newTile || newRow {
			e.cab.terminate(1) // end_of_subset_one_bit
			e.bw.alignZero()
			subs = append(subs, len(e.bw.buf)-subStart)
			subStart = len(e.bw.buf)
			e.cab.start(&e.bw)
		}
	}
	e.bw.alignZero()
	// The entry points are the sizes of all substreams but the last.
	return e.bw.buf, subs
}

// codeCTU decides and writes one CTU.
func (e *Encoder) codeCTU(x0, y0 int) {
	s := &e.d.sd
	sp := s.sps
	if s.h.saoLuma || s.h.saoChroma {
		prm := &e.d.fc.sao[s.ctbAddrRs]
		*prm = saoParams{}
		s.writeSAO(&e.cab, prm, 0)
	}
	e.est.ctx = e.cab.ctx
	e.est.bits = 0
	e.arena = e.arena[:0]
	root, _ := e.decideQT(x0, y0, sp.Log2Ctb, 0)
	s.writeQuadtree(&e.cab, root, 0)
}
