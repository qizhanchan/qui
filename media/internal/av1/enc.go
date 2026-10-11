package av1

import (
	"errors"
	"image"
	"math"
)

// The AV1 encoder. It keeps its reconstruction in an embedded Decoder:
// the sequence and frame headers it writes are parsed back by the
// decoder's parsers, blocks are reconstructed with the decoder's
// prediction, dequantisation and inverse transforms (the encoder only
// supplies coefficients, through tile.enc), and frames finish through
// the decoder's in-loop filters and reference update. Its reconstruction
// is therefore, by construction, what a decoder produces from its stream.
//
// A frame is coded in three passes: decisions and reconstruction with
// every symbol recorded (pass A), in-loop filter choices on the
// reconstruction (pass B), then the records are replayed into the range
// encoder with the CDEF indices filled in.

// EncoderConfig describes an encoding session. Zero values pick defaults.
type EncoderConfig struct {
	Width, Height  int
	FPSNum, FPSDen int // frame rate (default 30/1)

	// Rate control: constant quantiser when Bitrate and CRF are zero.
	QIndex  int     // base quantiser index 1..255 (default 128; also ABR's start)
	CRF     float64 // constant rate factor on the 0..63 scale (qindex ~ 4 CRF), 0 = off
	Bitrate int     // average bits/s (ABR), 0 = off
	// VBV: maximum bits/s and buffer size in bits (0 = unconstrained).
	VBVMaxRate, VBVBuffer int

	KeyInt int // frames between key frames (default 250)
	// MiniGOP is the number of frames per mini-GOP: with MiniGOP > 1 the
	// last frame of each group is coded first as a hidden ALTREF the
	// others also predict from, then shown with show_existing_frame
	// (each temporal unit still holds one shown frame, in display order).
	// 1 (the default) codes every frame from past frames only.
	MiniGOP int
	Refs    int // previous frames inter frames may reference, 1..3 (default by preset)

	// Preset trades speed for compression: 0 fastest (the default), 1
	// balanced, 2 best.
	Preset int

	// TileCols and TileRows split frames into uniform tiles coded in
	// parallel (powers of two; 0 picks by frame size: 2 columns from 1280
	// wide, 4 from 2560, the same for rows from 720 / 1440 high).
	TileCols, TileRows int

	FullRange bool
	Matrix    int // H.273 matrix_coefficients: 1 BT.709 (default), 6 BT.601
}

// Packet is one temporal unit (OBUs with obu_size, no temporal
// delimiter: the MP4 sample form).
type Packet struct {
	Data   []byte
	Key    bool
	PTS    int64 // input frame index
	Type   byte  // 'K' key frame, 'P' inter frame
	QIndex int
}

// Encoder encodes I420 frames to AV1 Main (8-bit 4:2:0).
type Encoder struct {
	cfg    EncoderConfig
	seq    encSeq
	seqOBU []byte
	d      *Decoder

	// Source of the frame being coded, padded to the superblock grid by
	// edge replication.
	src       [3][]uint8
	srcStride [3]int

	frameCount      int64 // display index of the frame being coded
	keyFrame        int64 // display index of the last key frame
	inCount         int64 // frames received
	queue           []*image.YCbCr
	slotPTS         [numRefFrames]int64 // display index held by each reference slot (-1: none)
	pending         []byte              // OBUs of a hidden frame, sent with the next shown one
	t               *tile
	sc              tileScratch
	sink            symSink
	cdefSeen        []bool // per 64x64: CDEF index already coded (record pass)
	lambda          float64
	sqrtLambda      float64
	tq              encTQ
	nonZero         bool       // a coefficient was coded since the flag was cleared
	tileRecs        [][]symRec // per tile: the symbol records of pass A
	workers         []*Encoder // per tile-encoding goroutine
	snaps           []*encSnap
	snapTop         int
	nodes           []encNode // partition search arena (per superblock)
	nodeTop         int
	lumaBuf         []modeCand
	interBuf        []blkChoice
	searched        [numRefFrames]mv // NEWMV search result per reference (current block)
	compoundAllRefs bool
	activeRefs      []int // references searched in the current inter frame
	interList       []blkChoice
	chromaBuf       []modeCand
	chromaOut       []modeCand

	// Search breadth.
	allow4x4       bool
	rectParts      bool // HORZ / VERT partitions
	angleDeltas    bool
	lumaCands      int
	chromaCands    int
	cflCands       int
	txDepth        bool
	txTypeSearch   int // 0 DCT only, 1 four types, 2 the whole set
	interpSearch   bool
	cdefCands      [][2]int
	lrWiener       bool
	sgrSets        []int
	lrForce        bool    // tests: restore every unit
	cdefSparse     bool    // measure CDEF on half the 8x8 blocks
	lfStep         int     // finest deblocking level step searched
	interRank      int     // inter candidates coded in full (0: all)
	intraSkipBelow float64 // no intra trial when the best inter cost per sample is below
	scoreBuf       []modeCand
	rankedBuf      []blkChoice

	rc rateControl

	keepRecon bool
	recon     []*Frame
}

// NewEncoder returns an encoder for cfg.
func NewEncoder(cfg EncoderConfig) (*Encoder, error) {
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width%2 != 0 || cfg.Height%2 != 0 {
		return nil, errors.New("av1: frame size must be positive and even")
	}
	if cfg.Width > 1<<16 || cfg.Height > 1<<16 {
		return nil, errors.New("av1: frame size too large")
	}
	if cfg.FPSNum == 0 || cfg.FPSDen == 0 {
		cfg.FPSNum, cfg.FPSDen = 30, 1
	}
	if cfg.QIndex == 0 {
		cfg.QIndex = 128
	}
	cfg.QIndex = clip3(1, 255, cfg.QIndex)
	if cfg.KeyInt <= 0 {
		cfg.KeyInt = 250
	}
	cfg.Preset = clip3(0, 2, cfg.Preset)
	if cfg.MiniGOP <= 0 {
		cfg.MiniGOP = 1
	}
	cfg.MiniGOP = min(cfg.MiniGOP, 16)
	if cfg.TileCols <= 0 {
		cfg.TileCols = 1
		if cfg.Width >= 1280 {
			cfg.TileCols = 2
		}
		if cfg.Width >= 2560 {
			cfg.TileCols = 4
		}
	}
	if cfg.TileRows <= 0 {
		cfg.TileRows = 1
		if cfg.Height >= 720 {
			cfg.TileRows = 2
		}
		if cfg.Height >= 1440 {
			cfg.TileRows = 4
		}
	}
	if cfg.Refs <= 0 {
		cfg.Refs = cfg.Preset + 1
	}
	cfg.Refs = min(cfg.Refs, 3)
	if cfg.Matrix == 0 {
		cfg.Matrix = 1
	}
	e := &Encoder{cfg: cfg, d: New()}
	for i := range e.slotPTS {
		e.slotPTS[i] = -1
	}
	e.setPreset(cfg.Preset)
	e.seq = encSeq{
		width: cfg.Width, height: cfg.Height, level: seqLevel(cfg.Width, cfg.Height),
		fullRange: cfg.FullRange, matrix: cfg.Matrix, primaries: 1, transfer: 1,
		orderHintBits: 7, refFrameMVs: true,
	}
	if cfg.Matrix == 6 {
		e.seq.primaries, e.seq.transfer = 6, 6
	}
	e.rc.init(e)
	e.seqOBU = appendOBU(nil, obuSequenceHeader, e.seq.writeSequenceHeader())
	if err := e.d.DecodeTemporalUnit(e.seqOBU); err != nil {
		return nil, err
	}
	return e, nil
}

// SequenceHeader returns the sequence header OBU (for av1C).
func (e *Encoder) SequenceHeader() []byte { return e.seqOBU }

// finishFrame commits the coded frame: in-loop filters, reference update
// and the reconstruction.
func (e *Encoder) finishFrame() error {
	e.d.seenFrameHeader = false
	if err := e.d.finishFrame(); err != nil {
		return err
	}
	e.collectRecon()
	return nil
}

// loadSource copies img into the padded source planes.
func (e *Encoder) loadSource(img *image.YCbCr) {
	w, h := e.cfg.Width, e.cfg.Height
	aw, ah := (w+63)&^63, (h+63)&^63
	for p := 0; p < 3; p++ {
		pw, ph, apw, aph := w, h, aw, ah
		src, stride := img.Y, img.YStride
		x0, y0 := img.Rect.Min.X, img.Rect.Min.Y
		if p > 0 {
			pw, ph, apw, aph = w/2, h/2, aw/2, ah/2
			src, stride = img.Cb, img.CStride
			x0, y0 = x0/2, y0/2
			if p == 2 {
				src = img.Cr
			}
		}
		if len(e.src[p]) < apw*aph {
			e.src[p] = make([]uint8, apw*aph)
		}
		e.srcStride[p] = apw
		dst := e.src[p]
		for y := 0; y < aph; y++ {
			sy := min(y, ph-1)
			row := src[(y0+sy)*stride+x0 : (y0+sy)*stride+x0+pw]
			d := dst[y*apw : y*apw+apw]
			copy(d, row)
			for x := pw; x < apw; x++ {
				d[x] = row[pw-1]
			}
		}
	}
}

// encodeFrame codes the loaded source as the job describes, returning
// its OBUs and leaving the frame to finishFrame.
func (e *Encoder) encodeFrame(job *encJob, q int) ([]byte, error) {
	d := e.d
	key := job.key
	fh := e.frameHeader(job, q)
	var bw bitWriter
	fh.write(d, &bw)
	bw.trailing()
	if err := d.DecodeTemporalUnit(appendOBU(nil, obuFrameHeader, bw.buf)); err != nil {
		return nil, err
	}
	f, h := d.fs, d.hdr
	if encFrameHook != nil {
		encFrameHook()
	}
	e.lambda = rdLambda(h.baseQIdx)
	e.sqrtLambda = math.Sqrt(e.lambda)

	// Pass A: decisions, reconstruction and symbol records, the tiles in
	// parallel (each worker an Encoder copy with its own scratch).
	nSB := len(f.cdefIdx)
	if cap(e.cdefSeen) < nSB {
		e.cdefSeen = make([]bool, nSB)
	}
	e.cdefSeen = e.cdefSeen[:nSB]
	clear(e.cdefSeen)
	ti := &h.tiles
	nt := ti.cols * ti.rows
	for len(e.tileRecs) < nt {
		e.tileRecs = append(e.tileRecs, nil)
	}
	tiles := make([]*tile, nt)
	workers := e.getWorkers(min(nt, workers()))
	parallelFor(nt, func(wk, i int) {
		w := workers[wk]
		w.lambda, w.sqrtLambda, w.activeRefs, w.cdefSeen = e.lambda, e.sqrtLambda, e.activeRefs, e.cdefSeen
		t := &tile{f: f, h: h, seq: d.seq, mc: &w.sc.mc, recon: &w.sc.recon, enc: w}
		row, col := i/ti.cols, i%ti.cols
		t.miRowStart, t.miRowEnd = ti.miRowStarts[row], ti.miRowStarts[row+1]
		t.miColStart, t.miColEnd = ti.miColStarts[col], ti.miColStarts[col+1]
		t.currentQ = h.baseQIdx
		t.cdf = f.cdf
		t.begin()
		w.t = t
		w.sink = symSink{recs: e.tileRecs[i][:0]}
		sb4 := num4x4W[t.sbSize()]
		for r := t.miRowStart; r < t.miRowEnd; r += sb4 {
			t.clearLeftContext()
			for c := t.miColStart; c < t.miColEnd; c += sb4 {
				f.clearCdef(r, c)
				t.clearBlockDecoded(r, c, sb4)
				w.writeLRPlaceholders(r, c, t.sbSize())
				w.encodeSB(r, c)
			}
		}
		e.tileRecs[i] = w.sink.recs
		tiles[i] = t
	})

	// Pass B: in-loop filters, measured on the reconstruction, which is
	// then left unfiltered for finishFrame.
	backup := d.cloneFrame(f.cur)
	e.decideLoopFilter(&fh)
	h.lf.level = fh.lfLevel
	h.lf.sharpness = fh.lfSharpness
	d.deblock(f)
	e.decideCDEF(&fh)
	if h.usesLR {
		cdefF := d.cloneFrame(f.cur)
		d.cdef(f, f.cur, cdefF)
		e.decideLR(&fh, cdefF, f.cur)
		d.release(&cdefF.planes, cdefF.numPlanes)
	}
	copyPlanes(f.cur, backup)
	d.release(&backup.planes, backup.numPlanes)

	// Tile data: each tile's records replayed from the initial CDFs.
	data := make([][]byte, nt)
	parallelFor(nt, func(_, i int) {
		t := tiles[i]
		t.cdf = f.cdf
		var me msacEnc
		me.init()
		k := symSink{recs: e.tileRecs[i]}
		k.replay(&me, f.cdefIdx, h.cdefBits, func(me *msacEnc, p, unit int) {
			t.writeLRUnit(me, p, &f.lr[p].units[unit])
		})
		data[i] = me.done()
	})
	f.savedCDF = tiles[ti.contextUpdateID].cdf
	maxSize := 0
	for _, b := range data[:nt-1] {
		maxSize = max(maxSize, len(b))
	}
	fh.tileSizeBytes = 1
	for maxSize > 1<<(8*uint(fh.tileSizeBytes)) {
		fh.tileSizeBytes++
	}

	bw = bitWriter{}
	fh.write(d, &bw)
	bw.byteAlign()
	if nt > 1 {
		bw.flag(false) // tile_start_and_end_present_flag
		bw.byteAlign()
		for _, b := range data[:nt-1] {
			bw.le(fh.tileSizeBytes, len(b)-1)
			bw.buf = append(bw.buf, b...)
		}
	}
	payload := append(bw.buf, data[nt-1]...)
	var out []byte
	if key {
		out = append(out, e.seqOBU...)
	}
	out = appendOBU(out, obuFrame, payload)
	return out, nil
}

// collectRecon keeps (or drops) the decoder's output frames.
func (e *Encoder) collectRecon() {
	for _, f := range e.d.out {
		if e.keepRecon {
			e.recon = append(e.recon, e.d.toFrame(f))
		}
	}
	clear(e.d.out)
	e.d.out = e.d.out[:0]
}

// rdLambda is the Lagrange multiplier for SSD + lambda * bits at a
// quantiser index (from the AC quantiser step, as libaom's
// av1_compute_rd_mult does for 8-bit).
func rdLambda(qindex int) float64 {
	q := float64(acQ(8, qindex))
	return 0.0021 * q * q
}

// setPreset sets the search breadth.
func (e *Encoder) setPreset(p int) {
	switch p {
	case 0:
		e.lumaCands, e.chromaCands, e.cflCands = 1, 1, 2
		e.cdefSparse, e.lfStep = true, 4
		e.interRank, e.intraSkipBelow = 2, 4
		e.cdefCands = cdefStrengthSubset([]int{0, 2, 4, 8}, []int{0, 2})
	case 1:
		e.rectParts, e.angleDeltas, e.txDepth, e.interpSearch = true, true, true, true
		e.lumaCands, e.chromaCands, e.cflCands = 2, 1, 4
		e.txTypeSearch = 1
		e.lfStep = 2
		e.interRank = 4
		e.cdefCands = cdefStrengthSubset([]int{0, 1, 2, 3, 4, 6, 8, 12}, []int{0, 1, 2})
		e.lrWiener = true
		e.sgrSets = []int{0, 4, 10, 14}
	default:
		e.allow4x4, e.rectParts, e.angleDeltas, e.txDepth, e.interpSearch = true, true, true, true, true
		e.lumaCands, e.chromaCands, e.cflCands = 3, 2, 8
		e.txTypeSearch = 2
		e.lfStep = 1
		e.cdefCands = cdefStrengths
		e.lrWiener = true
		e.sgrSets = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	}
}

// getWorkers returns n per-goroutine encoders: copies of e sharing its
// configuration and state but with their own scratch and pools.
func (e *Encoder) getWorkers(n int) []*Encoder {
	for len(e.workers) < n {
		w := new(Encoder)
		*w = *e
		w.workers, w.tileRecs = nil, nil
		w.snaps, w.snapTop, w.nodes, w.nodeTop = nil, 0, nil, 0
		w.lumaBuf, w.chromaBuf, w.chromaOut, w.interBuf, w.interList = nil, nil, nil, nil, nil
		w.scoreBuf, w.rankedBuf = nil, nil
		w.sink = symSink{}
		w.sc = tileScratch{}
		w.recon, w.queue, w.pending = nil, nil, nil
		e.workers = append(e.workers, w)
	}
	return e.workers[:n]
}
