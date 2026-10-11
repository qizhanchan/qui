package h264

// Macroblock type flags (an internal superset of the spec's mb_type).
const (
	mtIntra4x4   = 1 << 0 // I_NxN (I_8x8 when t8x8)
	mtIntra16x16 = 1 << 1
	mtIPCM       = 1 << 2
	mt16x16      = 1 << 3
	mt16x8       = 1 << 4
	mt8x16       = 1 << 5
	mt8x8        = 1 << 6
	mtSkip       = 1 << 7
	mtDirect     = 1 << 8 // B_Skip / B_Direct_16x16
	mtRef0       = 1 << 9 // P_8x8ref0
	mtP0L0       = 1 << 12
	mtP1L0       = 1 << 13
	mtP0L1       = 1 << 14
	mtP1L1       = 1 << 15

	mtIntra = mtIntra4x4 | mtIntra16x16 | mtIPCM
)

// bMbType maps B-slice mb_type 0..22 to flags (Table 7-14).
var bMbType = [23]uint32{
	mtDirect | mt16x16,
	mt16x16 | mtP0L0,
	mt16x16 | mtP0L1,
	mt16x16 | mtP0L0 | mtP0L1,
	mt16x8 | mtP0L0 | mtP1L0,
	mt8x16 | mtP0L0 | mtP1L0,
	mt16x8 | mtP0L1 | mtP1L1,
	mt8x16 | mtP0L1 | mtP1L1,
	mt16x8 | mtP0L0 | mtP1L1,
	mt8x16 | mtP0L0 | mtP1L1,
	mt16x8 | mtP0L1 | mtP1L0,
	mt8x16 | mtP0L1 | mtP1L0,
	mt16x8 | mtP0L0 | mtP1L0 | mtP1L1,
	mt8x16 | mtP0L0 | mtP1L0 | mtP1L1,
	mt16x8 | mtP0L1 | mtP1L0 | mtP1L1,
	mt8x16 | mtP0L1 | mtP1L0 | mtP1L1,
	mt16x8 | mtP0L0 | mtP0L1 | mtP1L0,
	mt8x16 | mtP0L0 | mtP0L1 | mtP1L0,
	mt16x8 | mtP0L0 | mtP0L1 | mtP1L1,
	mt8x16 | mtP0L0 | mtP0L1 | mtP1L1,
	mt16x8 | mtP0L0 | mtP0L1 | mtP1L0 | mtP1L1,
	mt8x16 | mtP0L0 | mtP0L1 | mtP1L0 | mtP1L1,
	mt8x8,
}

var pMbType = [5]uint32{
	mt16x16 | mtP0L0,
	mt16x8 | mtP0L0 | mtP1L0,
	mt8x16 | mtP0L0 | mtP1L0,
	mt8x8,
	mt8x8 | mtRef0,
}

// Sub-macroblock partition description: shape (0 8x8, 1 8x4, 2 4x8, 3 4x4),
// list mask (1 = L0, 2 = L1) and direct flag.
type subInfo struct {
	shape  uint8
	lists  uint8
	direct bool
}

var pSubType = [4]subInfo{{0, 1, false}, {1, 1, false}, {2, 1, false}, {3, 1, false}}

var bSubType = [13]subInfo{
	{0, 3, true},
	{0, 1, false}, {0, 2, false}, {0, 3, false},
	{1, 1, false}, {2, 1, false},
	{1, 2, false}, {2, 2, false},
	{1, 3, false}, {2, 3, false},
	{3, 1, false}, {3, 2, false}, {3, 3, false},
}

// subParts lists the 4x4-unit (x, y, w, h) sub-partitions of a shape inside
// an 8x8 at the origin.
var subParts = [4][][4]int{
	{{0, 0, 2, 2}},
	{{0, 0, 2, 1}, {0, 1, 2, 1}},
	{{0, 0, 1, 2}, {1, 0, 1, 2}},
	{{0, 0, 1, 1}, {1, 0, 1, 1}, {0, 1, 1, 1}, {1, 1, 1, 1}},
}

// mbState is the per-macroblock state kept for the whole picture.
type mbState struct {
	slice      int32 // slice number within the picture, -1 = not decoded
	typ        uint32
	qp         int8
	qpc        [2]int8
	cbp        uint8 // luma bits 0..3, chroma << 4
	t8x8       bool
	chromaMode uint8
	direct     uint8     // bit per 8x8 predicted in direct mode
	nz         [16]uint8 // CAVLC total_coeff per luma 4x4 (raster)
	nzC        [2][4]uint8
	// cbf holds CABAC coded_block_flags: bits 0..15 luma 4x4 (raster),
	// 16..19 Cb AC, 20..23 Cr AC, 24 luma DC, 25 Cb DC, 26 Cr DC.
	cbf       uint32
	nzMask    uint16   // deblocking: luma 4x4 (raster) with coefficients
	intraMode [16]int8 // raster; -1 when not I_NxN
	mvd       [2][16][2]uint8
}

// sliceInfo is what deblocking needs to know about each slice.
type sliceInfo struct {
	disable     int
	alphaOffset int
	betaOffset  int
	chromaOff   [2]int
}

// picCtx is the per-picture macroblock state.
type picCtx struct {
	mbW, mbH int
	mbs      []mbState
	slices   []sliceInfo
	dq       *dequantTables
	pps      *PPS
}

func (pc *picCtx) reset(sps *SPS, pps *PPS) {
	pc.mbW, pc.mbH = sps.PicWidthInMbs, sps.FrameHeightInMbs()
	n := pc.mbW * pc.mbH
	if cap(pc.mbs) < n {
		pc.mbs = make([]mbState, n)
	}
	pc.mbs = pc.mbs[:n]
	for i := range pc.mbs {
		pc.mbs[i].slice = -1
	}
	pc.slices = pc.slices[:0]
	pc.pps = pps
}

// Neighbour cache layout: index (y+1)*8 + (x+1) for 4x4 block (x, y) of the
// current macroblock, x in [-1, 4], y in [-1, 3].
func cidx(x, y int) int { return (y+1)*8 + x + 1 }

const (
	refNA   = -2 // not available
	refNone = -1 // list unused / intra
)

// sliceCtx holds the slice-level and current-macroblock decoding state.
type sliceCtx struct {
	d   *Decoder
	pc  *picCtx
	pic *picture
	h   *sliceHeader
	sps *SPS
	pps *PPS
	br  bitReader
	cb  cabac

	cabacMode  bool
	sliceNum   int32
	qp         int
	lastDQuant int
	refs       [2][]*picture
	implicitW  [32][32]int16 // implicit bi-pred weight w1 (w0 = 64 - w1)
	dsf        [32]int       // temporal direct DistScaleFactor per refIdxL0
	dsfLong    [32]bool
	colZeroOK  bool // RefPicList1[0] is short-term

	mbAddr, mbX, mbY int
	mb               *mbState
	mbA, mbB         *mbState // left / top neighbour (nil if unavailable)
	availC, availD   bool

	refCache    [2][40]int8
	mvCache     [2][40][2]int16
	mvdCache    [2][40][2]uint8
	directCache [40]bool

	// Current macroblock syntax.
	typ        uint32
	subType    [4]subInfo
	i16Mode    int
	predModes  [16]int8 // raster
	chromaMode int
	cbp        int
	t8x8       bool

	// Spatial-direct per-macroblock results.
	spatialDone bool
	spRef       [2]int8
	spMv        [2][2]int16
	spZero      bool

	// Residual (dequantised), per luma 4x4 block in raster block order.
	coef     [16][16]int32
	coef8    [4][64]int32
	coefDC   [16]int32
	chromaDC [2][4]int32
	chromaAC [2][4][16]int32
	lumaNZ   uint16 // raster bitmask of luma blocks with coefficients
	chromaNZ uint8  // bit c*4+blk
	scanTmp  [64]int32
	predTmp  [2][256]byte
	cpredTmp [2][2][64]byte
	emu      [21 * 21]byte
	mcTmp    [21 * 21]int32
	hvTmp    [16 * 24]int16 // NEON centre-position scratch
}

// setupMB prepares the neighbour pointers for macroblock addr.
func (s *sliceCtx) setupMB(addr int) {
	pc := s.pc
	s.mbAddr = addr
	s.mbX, s.mbY = addr%pc.mbW, addr/pc.mbW
	s.mb = &pc.mbs[addr]
	s.mb.slice = s.sliceNum
	s.mbA, s.mbB = nil, nil
	s.availC, s.availD = false, false
	if s.mbX > 0 && pc.mbs[addr-1].slice == s.sliceNum {
		s.mbA = &pc.mbs[addr-1]
	}
	if s.mbY > 0 {
		if pc.mbs[addr-pc.mbW].slice == s.sliceNum {
			s.mbB = &pc.mbs[addr-pc.mbW]
		}
		if s.mbX < pc.mbW-1 && pc.mbs[addr-pc.mbW+1].slice == s.sliceNum {
			s.availC = true
		}
		if s.mbX > 0 && pc.mbs[addr-pc.mbW-1].slice == s.sliceNum {
			s.availD = true
		}
	}
	s.spatialDone = false
}

// fillMotionCaches loads neighbouring motion into the caches.
func (s *sliceCtx) fillMotionCaches() {
	pic := s.pic
	stride4 := s.pc.mbW * 4
	bx0, by0 := s.mbX*4, s.mbY*4
	lists := 1
	if s.h.isB() {
		lists = 2
	}
	for l := 0; l < 2; l++ {
		rc, mc := &s.refCache[l], &s.mvCache[l]
		for i := range rc {
			rc[i] = refNA
			mc[i] = [2]int16{}
		}
		if l >= lists {
			continue
		}
		mvd := &s.mvdCache[l]
		if s.cabacMode {
			for i := range mvd {
				mvd[i] = [2]uint8{}
			}
		}
		load := func(ci, bx, by int) {
			i := by*stride4 + bx
			rc[ci] = pic.ref[l][i]
			mc[ci] = [2]int16{pic.mv[l][2*i], pic.mv[l][2*i+1]}
		}
		if s.mbB != nil {
			for x := 0; x < 4; x++ {
				load(cidx(x, -1), bx0+x, by0-1)
			}
			if s.cabacMode {
				for x := 0; x < 4; x++ {
					mvd[cidx(x, -1)] = s.mbB.mvd[l][12+x]
				}
			}
		}
		if s.mbA != nil {
			for y := 0; y < 4; y++ {
				load(cidx(-1, y), bx0-1, by0+y)
			}
			if s.cabacMode {
				for y := 0; y < 4; y++ {
					mvd[cidx(-1, y)] = s.mbA.mvd[l][y*4+3]
				}
			}
		}
		if s.availC {
			load(cidx(4, -1), bx0+4, by0-1)
		}
		if s.availD {
			load(cidx(-1, -1), bx0-1, by0-1)
		}
	}
	if s.cabacMode {
		dc := &s.directCache
		for i := range dc {
			dc[i] = false
		}
	}
	if s.cabacMode && s.h.isB() {
		dc := &s.directCache
		if s.mbB != nil {
			dc[cidx(0, -1)] = s.mbB.direct&4 != 0
			dc[cidx(1, -1)] = s.mbB.direct&4 != 0
			dc[cidx(2, -1)] = s.mbB.direct&8 != 0
			dc[cidx(3, -1)] = s.mbB.direct&8 != 0
		}
		if s.mbA != nil {
			dc[cidx(-1, 0)] = s.mbA.direct&2 != 0
			dc[cidx(-1, 1)] = s.mbA.direct&2 != 0
			dc[cidx(-1, 2)] = s.mbA.direct&8 != 0
			dc[cidx(-1, 3)] = s.mbA.direct&8 != 0
		}
	}
}

// setMotion fills a w x h (4x4 units) region of the cache for list l.
func (s *sliceCtx) setMotion(l, x, y, w, h int, ref int8, mvx, mvy int16) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			c := cidx(i, j)
			s.refCache[l][c] = ref
			s.mvCache[l][c] = [2]int16{mvx, mvy}
		}
	}
}

func (s *sliceCtx) setMvd(l, x, y, w, h int, dx, dy int) {
	ax, ay := min(absInt(dx), 70), min(absInt(dy), 70)
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			s.mvdCache[l][cidx(i, j)] = [2]uint8{uint8(ax), uint8(ay)}
		}
	}
}

// writeBackMotion stores the macroblock's motion into the picture.
func (s *sliceCtx) writeBackMotion() {
	pic := s.pic
	stride4 := s.pc.mbW * 4
	bx0, by0 := s.mbX*4, s.mbY*4
	mb := s.mb
	for l := 0; l < 2; l++ {
		refs := s.refs[l]
		for y := 0; y < 4; y++ {
			i := (by0+y)*stride4 + bx0
			refRow := pic.ref[l][i : i+4 : i+4]
			idRow := pic.refID[l][i : i+4 : i+4]
			mvRow := pic.mv[l][2*i : 2*i+8 : 2*i+8]
			c := cidx(0, y)
			rc := s.refCache[l][c : c+4 : c+4]
			mc := s.mvCache[l][c : c+4 : c+4]
			for x := 0; x < 4; x++ {
				r := rc[x]
				if r < 0 {
					refRow[x], idRow[x] = -1, -1
					mvRow[2*x], mvRow[2*x+1] = 0, 0
					continue
				}
				refRow[x] = r
				idRow[x] = refs[r].id
				mvRow[2*x], mvRow[2*x+1] = mc[x][0], mc[x][1]
			}
		}
		if s.cabacMode {
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					mb.mvd[l][y*4+x] = s.mvdCache[l][cidx(x, y)]
				}
			}
		}
	}
}

// writeBackIntra marks the macroblock as having no motion.
func (s *sliceCtx) writeBackIntra() {
	pic := s.pic
	stride4 := s.pc.mbW * 4
	bx0, by0 := s.mbX*4, s.mbY*4
	for l := 0; l < 2; l++ {
		for y := 0; y < 4; y++ {
			i := (by0+y)*stride4 + bx0
			for x := 0; x < 4; x++ {
				pic.ref[l][i+x] = -1
				pic.refID[l][i+x] = -1
				pic.mv[l][2*(i+x)], pic.mv[l][2*(i+x)+1] = 0, 0
			}
		}
	}
	if s.cabacMode {
		s.mb.mvd = [2][16][2]uint8{}
	}
}
