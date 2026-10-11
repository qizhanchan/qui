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

	// QIndex is the base quantiser index 1..255 (default 128).
	QIndex int

	KeyInt int // frames between key frames (default 250)

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

	frameCount int64
	keyFrame   int64 // input index of the last key frame
	t          *tile
	sc         tileScratch
	sink       symSink
	cdefSeen   []bool // per 64x64: CDEF index already coded (record pass)
	lambda     float64
	sqrtLambda float64
	tq         encTQ
	nonZero    bool // a coefficient was coded since the flag was cleared
	snaps      []*encSnap
	snapTop    int
	nodes      []encNode // partition search arena (per superblock)
	nodeTop    int
	lumaBuf    []modeCand
	interBuf   []blkChoice
	interList  []blkChoice
	chromaBuf  []modeCand
	chromaOut  []modeCand

	// Search breadth.
	allow4x4     bool
	lumaCands    int
	chromaCands  int
	txTypeSearch int // 0 DCT only, 1 four types, 2 the whole set

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
	if cfg.Matrix == 0 {
		cfg.Matrix = 1
	}
	e := &Encoder{cfg: cfg, d: New(), allow4x4: true, lumaCands: 3, chromaCands: 2, txTypeSearch: 2}
	e.seq = encSeq{
		width: cfg.Width, height: cfg.Height, level: seqLevel(cfg.Width, cfg.Height),
		fullRange: cfg.FullRange, matrix: cfg.Matrix, primaries: 1, transfer: 1,
		orderHintBits: 7,
	}
	if cfg.Matrix == 6 {
		e.seq.primaries, e.seq.transfer = 6, 6
	}
	e.seqOBU = appendOBU(nil, obuSequenceHeader, e.seq.writeSequenceHeader())
	if err := e.d.DecodeTemporalUnit(e.seqOBU); err != nil {
		return nil, err
	}
	return e, nil
}

// SequenceHeader returns the sequence header OBU (for av1C).
func (e *Encoder) SequenceHeader() []byte { return e.seqOBU }

// Encode codes one frame (img must be at least Width x Height, I420).
func (e *Encoder) Encode(img *image.YCbCr) (Packet, error) {
	if img.Rect.Dx() < e.cfg.Width || img.Rect.Dy() < e.cfg.Height {
		return Packet{}, errors.New("av1: image smaller than the configured size")
	}
	e.loadSource(img)
	key := e.frameCount-e.keyFrame >= int64(e.cfg.KeyInt) || e.frameCount == 0
	if key {
		e.keyFrame = e.frameCount
	}
	pkt, err := e.encodeFrame(key)
	pkt.PTS = e.frameCount
	e.frameCount++
	return pkt, err
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

// frameHeader returns the header fields of a key frame or of an inter
// frame predicting from the previous frame (slot 0, all references).
func (e *Encoder) frameHeader(key bool) encFrameHdr {
	fh := encFrameHdr{
		frameType: keyFrame, showFrame: true,
		orderHint:   int(e.frameCount) & (1<<uint(e.seq.orderHintBits) - 1),
		baseQ:       e.cfg.QIndex,
		cdefDamping: 3,
		txSelect:    true,
	}
	if !key {
		fh.frameType = interFrame
		fh.primaryRef = 0
		fh.refresh = 1
		fh.interp = filterEightTap
		fh.useRefMVs = e.seq.refFrameMVs
	}
	return fh
}

func (e *Encoder) encodeFrame(key bool) (Packet, error) {
	d := e.d
	fh := e.frameHeader(key)
	var bw bitWriter
	fh.write(d, &bw)
	bw.trailing()
	if err := d.DecodeTemporalUnit(appendOBU(nil, obuFrameHeader, bw.buf)); err != nil {
		return Packet{}, err
	}
	f, h := d.fs, d.hdr
	e.lambda = rdLambda(h.baseQIdx)
	e.sqrtLambda = math.Sqrt(e.lambda)

	// Pass A: decisions, reconstruction and symbol records.
	t := &tile{f: f, h: h, seq: d.seq, mc: &e.sc.mc, recon: &e.sc.recon, enc: e}
	t.miRowStart, t.miRowEnd = 0, f.miRows
	t.miColStart, t.miColEnd = 0, f.miCols
	t.currentQ = h.baseQIdx
	t.cdf = f.cdf
	t.begin()
	e.t = t
	initCDF := t.cdf
	e.sink = symSink{recs: e.sink.recs[:0]}
	nSB := len(f.cdefIdx)
	if cap(e.cdefSeen) < nSB {
		e.cdefSeen = make([]bool, nSB)
	}
	e.cdefSeen = e.cdefSeen[:nSB]
	clear(e.cdefSeen)
	sb4 := num4x4W[t.sbSize()]
	for r := t.miRowStart; r < t.miRowEnd; r += sb4 {
		t.clearLeftContext()
		for c := t.miColStart; c < t.miColEnd; c += sb4 {
			f.clearCdef(r, c)
			t.clearBlockDecoded(r, c, sb4)
			e.encodeSB(r, c)
		}
	}

	// Pass B: in-loop filters, measured on the reconstruction, which is
	// then left unfiltered for finishFrame.
	backup := d.cloneFrame(f.cur)
	e.decideLoopFilter(&fh)
	h.lf.level = fh.lfLevel
	h.lf.sharpness = fh.lfSharpness
	d.deblock(f)
	e.decideCDEF(&fh)
	copyPlanes(f.cur, backup)
	d.release(&backup.planes, backup.numPlanes)

	// Tile data: replay the records from the initial CDFs.
	t.cdf = initCDF
	var me msacEnc
	me.init()
	e.sink.replay(&me, f.cdefIdx, h.cdefBits)
	tileData := me.done()
	f.savedCDF = t.cdf

	bw = bitWriter{}
	fh.write(d, &bw)
	bw.byteAlign()
	payload := append(bw.buf, tileData...)
	var out []byte
	if key {
		out = append(out, e.seqOBU...)
	}
	out = appendOBU(out, obuFrame, payload)

	d.seenFrameHeader = false
	if err := d.finishFrame(); err != nil {
		return Packet{}, err
	}
	e.collectRecon()
	typ := byte('K')
	if !key {
		typ = 'P'
	}
	return Packet{Data: out, Key: key, Type: typ, QIndex: h.baseQIdx}, nil
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
