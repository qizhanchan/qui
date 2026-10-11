package h264enc

import (
	"sync"

	"github.com/qizhanchan/qui/media/internal/h264"
)

// mbInfo is the per-macroblock state later macroblocks predict from.
type mbInfo struct {
	typ   uint32 // h264.MB* flags
	slice int32
	qp    int8
	nz    [16]uint8 // CAVLC total_coeff per luma 4x4 (raster)
	nzC   [2][4]uint8
	modes [16]int8 // Intra4x4/8x8 modes (raster), -1 when not I_NxN
	t8x8  bool
	cbp   uint8
	// CABAC context state.
	chromaMode uint8
	cbf        uint32 // coded_block_flag bits as in the decoder's mbState
	mvd        [2][16][2]uint8
	skip       bool
	direct     bool  // B_Skip / B_Direct_16x16
	directMask uint8 // 8x8 quadrants predicted in direct mode
}

// sliceEnc codes one slice (a band of macroblock rows).
type sliceEnc struct {
	e                *Encoder
	mc               *h264.MC
	firstRow, endRow int

	w   bitWriter
	cb  cabacEnc
	hdr *sliceHeader
	src *srcPic
	cur *refPic
	typ int

	qp, lastQP int
	lambda     int
	skipRun    int
	prevSkip   bool // CABAC: previous macroblock skipped (mb_qp_delta context)
	prevQPNZ   bool // CABAC: previous macroblock had a non-zero mb_qp_delta

	mbX, mbY, addr int
	mb             *mbInfo
	availA, availB bool
	availC, availD bool

	// Current-macroblock decision and coefficients.
	intra      bool
	i16        bool
	i16Mode    int
	chromaMode int
	modes      [16]int8 // raster
	t8x8       bool
	lumaLv     [16][16]int32 // raster block, raster coefficient
	luma8Lv    [4][64]int32
	dcLv       [16]int32 // raster
	cDCLv      [2][4]int32
	cACLv      [2][4][16]int32
	cbpL, cbpC int

	// Inter decision.
	part   int          // 0 16x16, 1 16x8, 2 8x16, 3 8x8
	pmv    [4][2][2]int // per partition, per list: (x, y)
	pref   [4][2]int
	plists [4]int // bit 0 L0, bit 1 L1 (B)
	bType  int    // B mb_type when applicable
	sub    [4]int // B sub_mb_type
	direct bool
	done   uint16       // 4x4 blocks whose motion is final (mvpred availability)
	mvp    [4][2][2]int // predictor per partition and list (mvd = pmv - mvp)
	tryRef [4]int
	tryMV  [4][2]int
	cands  [8][2]int
	// CABAC ref_idx context of the partitions coded so far.
	curRef    [2][16]int8
	curDirect uint8
	// Spatial direct motion of the current B macroblock.
	dRef  [2]int
	dMV   [4][2][2]int // per 8x8 quadrant, per list
	leftY [16]byte
	leftC [2][8]byte

	predY   [256]byte
	predI16 [256]byte
	predCb  [64]byte
	predCr  [64]byte
	tmpY    [256]byte
	tmpY2   [256]byte
	tmpC    [2][64]byte
}

// codeSlices codes all slices of the current picture (in parallel when
// there are several) and returns their NAL units.
func (e *Encoder) codeSlices(src *srcPic, h *sliceHeader) [][]byte {
	out := make([][]byte, len(e.slices))
	run := func(i int) {
		s := e.slices[i]
		hh := *h
		hh.firstMB = s.firstRow * e.mbW
		out[i] = s.encode(src, e.cur, &hh, int32(i))
	}
	if len(e.slices) == 1 {
		run(0)
		return out
	}
	var wg sync.WaitGroup
	for i := range e.slices {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run(i)
		}()
	}
	wg.Wait()
	return out
}

func (s *sliceEnc) encode(src *srcPic, cur *refPic, h *sliceHeader, sliceID int32) []byte {
	e := s.e
	s.src, s.cur, s.hdr, s.typ = src, cur, h, h.typ
	s.qp, s.lastQP = h.qp, h.qp
	s.lambda = lambdaTab[h.qp]
	s.skipRun = 0
	s.prevSkip, s.prevQPNZ = false, false
	s.w.reset()
	e.writeSliceHeader(&s.w, h)
	if e.cfg.CABAC {
		s.w.align(1)
		s.cb.init(&s.w, h.typ, h.cabacInit, h.qp)
	}
	for y := s.firstRow; y < s.endRow; y++ {
		for x := 0; x < e.mbW; x++ {
			s.mbs()[y*e.mbW+x].slice = sliceID
			s.encodeMB(x, y)
			if e.cfg.CABAC {
				s.cb.encodeTerminate(x == e.mbW-1 && y == s.endRow-1)
			}
		}
	}
	if e.cfg.CABAC {
		s.cb.finish()
	} else {
		if s.skipRun > 0 {
			s.w.ue(uint32(s.skipRun))
		}
		s.w.trailing()
	}
	typ := nalSlice
	if h.idr {
		typ = nalIDR
	}
	return nalUnit(h.refIdc, typ, s.w.buf)
}

func (s *sliceEnc) mbs() []mbInfo { return s.e.mbs }

// setup positions the slice encoder on macroblock (x, y).
func (s *sliceEnc) setup(x, y int) {
	e := s.e
	s.mbX, s.mbY, s.addr = x, y, y*e.mbW+x
	s.mb = &e.mbs[s.addr]
	sl := s.mb.slice
	*s.mb = mbInfo{slice: sl}
	for i := range s.mb.modes {
		s.mb.modes[i] = -1
	}
	s.availA = x > 0
	s.availB = y > s.firstRow
	s.availC = y > s.firstRow && x < e.mbW-1
	s.availD = y > s.firstRow && x > 0
	s.done = 0
}

func (s *sliceEnc) left() *mbInfo { return &s.e.mbs[s.addr-1] }
func (s *sliceEnc) top() *mbInfo  { return &s.e.mbs[s.addr-s.e.mbW] }
func (s *sliceEnc) plane() ([]byte, int) {
	c := s.cur
	return c.y[c.yo+s.mbY*16*c.ys+s.mbX*16:], c.ys
}

func (s *sliceEnc) chromaPlanes() ([]byte, []byte, int) {
	c := s.cur
	o := c.co + s.mbY*8*c.cs + s.mbX*8
	return c.cb[o:], c.cr[o:], c.cs
}

func (s *sliceEnc) srcY() ([]byte, int) {
	return s.src.y[s.mbY*16*s.src.ys+s.mbX*16:], s.src.ys
}

func (s *sliceEnc) srcC() ([]byte, []byte, int) {
	o := s.mbY*8*s.src.cs + s.mbX*8
	return s.src.cb[o:], s.src.cr[o:], s.src.cs
}

// encodeMB analyses, codes and reconstructs one macroblock.
func (s *sliceEnc) encodeMB(x, y int) {
	s.setup(x, y)
	switch s.typ {
	case sliceI:
		s.analyseIntra(false)
		s.finishIntra()
	case sliceP:
		s.encodeP()
	default:
		s.encodeB()
	}
}

// finishIntra codes and writes the intra decision made by analyseIntra.
func (s *sliceEnc) finishIntra() {
	s.intra = true
	if s.i16 {
		s.codeI16()
	} else if s.t8x8 {
		s.codeI8Final()
	}
	s.codeChroma(true)
	s.clearMotion()
	mb := s.mb
	if s.i16 {
		mb.typ = h264.MBIntra16x16
	} else {
		mb.typ = h264.MBIntra4x4
		mb.modes = s.modes
		mb.t8x8 = s.t8x8
	}
	mb.chromaMode = uint8(s.chromaMode)
	s.writeMB()
}

// clearMotion marks the macroblock's 4x4 blocks as intra (no motion).
func (s *sliceEnc) clearMotion() {
	e := s.e
	w4 := e.mbW * 4
	for l := 0; l < 2; l++ {
		for by := 0; by < 4; by++ {
			i := (s.mbY*4+by)*w4 + s.mbX*4
			for bx := 0; bx < 4; bx++ {
				e.ref[l][i+bx] = -1
				e.mv[l][2*(i+bx)] = 0
				e.mv[l][2*(i+bx)+1] = 0
			}
		}
	}
}

// qpUsed is the macroblock's QPY: its own QP when mb_qp_delta is coded,
// otherwise the predicted QP.
func (s *sliceEnc) qpUsed() int {
	if s.cbpL|s.cbpC != 0 || (s.intra && s.i16) {
		return s.qp
	}
	return s.lastQP
}

// writeMB writes the macroblock layer and records deblocking metadata.
func (s *sliceEnc) writeMB() {
	qp := s.qpUsed()
	if s.e.cfg.CABAC {
		s.writeMBCABAC()
	} else {
		s.writeMBCAVLC()
	}
	s.lastQP = qp
	s.recordDeblock(qp)
}

func (s *sliceEnc) recordDeblock(qp int) {
	e := s.e
	mb := s.mb
	mb.qp = int8(qp)
	var nz uint16
	for r := 0; r < 16; r++ {
		if mb.nz[r] != 0 {
			nz |= 1 << uint(r)
		}
	}
	if mb.t8x8 {
		// Deblocking sees an 8x8 transform block as one unit.
		for b8 := 0; b8 < 4; b8++ {
			m := uint16(0x33) << uint((b8>>1)*8+(b8&1)*2)
			if nz&m != 0 {
				nz |= m
			}
		}
	}
	qpc := int8(h264.ChromaQP(qp, e.cfg.ChromaQPOffset))
	e.dbInfo[s.addr] = h264.MBInfo{
		Slice: mb.slice, Type: mb.typ, QP: int8(qp), QPC: [2]int8{qpc, qpc},
		T8x8: mb.t8x8, NZ: nz,
	}
}

// nC predicts total_coeff for luma block (bx, by) of the current MB.
func (s *sliceEnc) nC(bx, by int) int {
	var nA, nB int
	var a, b bool
	if bx > 0 {
		nA, a = int(s.mb.nz[by*4+bx-1]), true
	} else if s.availA {
		nA, a = int(s.left().nz[by*4+3]), true
	}
	if by > 0 {
		nB, b = int(s.mb.nz[(by-1)*4+bx]), true
	} else if s.availB {
		nB, b = int(s.top().nz[12+bx]), true
	}
	switch {
	case a && b:
		return (nA + nB + 1) >> 1
	case a:
		return nA
	case b:
		return nB
	}
	return 0
}

// nCChroma predicts total_coeff for chroma AC block (bx, by) of plane c.
func (s *sliceEnc) nCChroma(c, bx, by int) int {
	var nA, nB int
	var a, b bool
	if bx > 0 {
		nA, a = int(s.mb.nzC[c][by*2]), true
	} else if s.availA {
		nA, a = int(s.left().nzC[c][by*2+1]), true
	}
	if by > 0 {
		nB, b = int(s.mb.nzC[c][bx]), true
	} else if s.availB {
		nB, b = int(s.top().nzC[c][2+bx]), true
	}
	switch {
	case a && b:
		return (nA + nB + 1) >> 1
	case a:
		return nA
	case b:
		return nB
	}
	return 0
}

// blkOrder lists luma 4x4 blocks in decoding order as raster indices.
var blkOrder = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// rasterToBlk maps a raster 4x4 index to its decoding-order index.
var rasterToBlk = [16]int{0, 1, 4, 5, 2, 3, 6, 7, 8, 9, 12, 13, 10, 11, 14, 15}

// writeMBCAVLC writes the macroblock layer with CAVLC.
func (s *sliceEnc) writeMBCAVLC() {
	w := &s.w
	mb := s.mb
	if s.typ != sliceI {
		w.ue(uint32(s.skipRun))
		s.skipRun = 0
	}
	cbp := s.cbpL | s.cbpC<<4
	if s.intra {
		off := 0
		switch s.typ {
		case sliceP:
			off = 5
		case sliceB:
			off = 23
		}
		if s.i16 {
			t := 1 + s.i16Mode + 4*s.cbpC
			if s.cbpL != 0 {
				t += 12
			}
			w.ue(uint32(off + t))
		} else {
			w.ue(uint32(off))
			if s.e.cfg.Transform8x8 {
				w.flag(s.t8x8)
			}
			if s.t8x8 {
				for b8 := 0; b8 < 4; b8++ {
					r := (b8>>1)*8 + (b8&1)*2
					s.writeIntraMode(s.predIntraMode(r&3, r>>2), int(s.modes[r]))
				}
			} else {
				for _, r := range blkOrder {
					s.writeIntraMode(s.predIntraMode(r&3, r>>2), int(s.modes[r]))
					mb.modes[r] = s.modes[r] // later blocks predict from it
				}
			}
		}
		w.ue(uint32(s.chromaMode))
		if !s.i16 {
			w.ue(uint32(h264.CBPCodeNum(cbp, true)))
		}
	} else {
		s.writeInterPred()
		w.ue(uint32(h264.CBPCodeNum(cbp, false)))
		if cbp&15 != 0 && s.e.cfg.Transform8x8 && s.allow8x8() {
			w.flag(s.t8x8)
		}
	}
	mb.cbp = uint8(cbp)
	if cbp > 0 || (s.intra && s.i16) {
		w.se(int32(s.qp - s.lastQP))
	}
	s.writeResidualCAVLC()
}

func (s *sliceEnc) writeIntraMode(pred, mode int) {
	if mode == pred {
		s.w.u(1, 1)
		return
	}
	if mode > pred {
		mode--
	}
	s.w.u(4, uint32(mode))
}

// writeResidualCAVLC writes the residual of the current macroblock and
// records total_coeff for neighbour prediction.
func (s *sliceEnc) writeResidualCAVLC() {
	w := &s.w
	mb := s.mb
	var sc [16]int32
	if s.intra && s.i16 {
		for i := 0; i < 16; i++ {
			sc[i] = s.dcLv[zz4[i]]
		}
		w.writeResidual(sc[:16], s.nC(0, 0))
		for _, r := range blkOrder {
			if s.cbpL == 0 {
				mb.nz[r] = 0
				continue
			}
			for i := 0; i < 15; i++ {
				sc[i] = s.lumaLv[r][zz4[i+1]]
			}
			mb.nz[r] = uint8(w.writeResidual(sc[:15], s.nC(r&3, r>>2)))
		}
	} else if s.t8x8 {
		for b8 := 0; b8 < 4; b8++ {
			x0, y0 := (b8&1)*2, (b8>>1)*2
			if s.cbpL&(1<<uint(b8)) == 0 {
				for k := 0; k < 4; k++ {
					mb.nz[(y0+k>>1)*4+x0+k&1] = 0
				}
				continue
			}
			// CAVLC codes an 8x8 block as four interleaved 4x4 scans.
			for i4 := 0; i4 < 4; i4++ {
				x, y := x0+i4&1, y0+i4>>1
				for i := 0; i < 16; i++ {
					sc[i] = s.luma8Lv[b8][zz8[4*i+i4]]
				}
				mb.nz[y*4+x] = uint8(w.writeResidual(sc[:16], s.nC(x, y)))
			}
		}
	} else {
		for _, r := range blkOrder {
			if s.cbpL&(1<<uint((r>>3)*2+(r&3)>>1)) == 0 {
				mb.nz[r] = 0
				continue
			}
			for i := 0; i < 16; i++ {
				sc[i] = s.lumaLv[r][zz4[i]]
			}
			mb.nz[r] = uint8(w.writeResidual(sc[:16], s.nC(r&3, r>>2)))
		}
	}
	if s.cbpC != 0 {
		for c := 0; c < 2; c++ {
			w.writeResidual(s.cDCLv[c][:], -1)
		}
	}
	for c := 0; c < 2; c++ {
		for blk := 0; blk < 4; blk++ {
			if s.cbpC != 2 {
				mb.nzC[c][blk] = 0
				continue
			}
			for i := 0; i < 15; i++ {
				sc[i] = s.cACLv[c][blk][zz4[i+1]]
			}
			mb.nzC[c][blk] = uint8(w.writeResidual(sc[:15], s.nCChroma(c, blk&1, blk>>1)))
		}
	}
}
