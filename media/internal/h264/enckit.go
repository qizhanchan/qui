package h264

// Encoder kit: the decoder's prediction, transform, motion-compensation,
// CAVLC-table and deblocking primitives, exported for media/internal/h264enc.
// The encoder reconstructs with exactly the code that decodes its stream, so
// "encoder reconstruction == our decode" holds by construction and any
// FFmpeg mismatch points at the bitstream, not at a second implementation.

// Predict16x16 is the Intra16x16 predictor (top/left nil when unavailable).
func Predict16x16(mode int, top, left []byte, topLeft byte) [16][16]byte {
	return predict16x16(mode, top, left, topLeft)
}

// Predict4x4 is the Intra4x4 predictor; ref is [L3,L2,L1,L0, TL, T0..T7].
func Predict4x4(mode int, ref [13]byte, leftAvail, topAvail bool) [4][4]byte {
	return predict4x4(mode, ref, leftAvail, topAvail)
}

// Predict8x8 is the Intra8x8 predictor over already-filtered references;
// ref is [L7..L0, TL, T0..T15].
func Predict8x8(mode int, ref [25]byte, leftAvail, topAvail bool) [8][8]byte {
	return predict8x8(mode, ref, leftAvail, topAvail)
}

// PredictChroma is the 8x8 intra chroma predictor.
func PredictChroma(mode int, top, left []byte, topLeft byte) [8][8]byte {
	return predictChroma(mode, top, left, topLeft)
}

// IDCT4Add adds the inverse 4x4 transform of blk (raster, dequantised) to
// dst and clears blk.
func IDCT4Add(dst []byte, stride int, blk *[16]int32) { idct4Add(dst, stride, blk) }

// IDCT8Add is the 8x8 counterpart of IDCT4Add.
func IDCT8Add(dst []byte, stride int, blk *[64]int32) { idct8Add(dst, stride, blk) }

// InverseHadamard4 is the inverse 4x4 Hadamard of Intra16x16 DC levels
// (raster, in place).
func InverseHadamard4(c *[16]int32) { hadamard4(c) }

// ZigZag4x4 and ZigZag8x8 map scan position to raster position.
func ZigZag4x4() [16]uint8 { return zigzag4x4 }

// ZigZag8x8 maps 8x8 frame-scan position to raster position.
func ZigZag8x8() [64]uint8 { return zigzag8x8 }

// ChromaQP maps a luma QP and chroma_qp_index_offset to QPc.
func ChromaQP(qp, offset int) int { return chromaQP(qp, offset) }

// Dequant holds LevelScale << (qP/6) per list, qP and raster position, as
// the decoder applies it: AC coefficient c becomes (c*q + 8) >> 4.
type Dequant struct{ t *dequantTables }

// NewDequant returns the tables of pps, or the flat tables when pps is nil.
func NewDequant(pps *PPS) Dequant {
	if pps == nil {
		pps = &PPS{}
		f4, f8 := flat4(), flat8()
		for i := range pps.Scaling4 {
			pps.Scaling4[i] = f4
		}
		for i := range pps.Scaling8 {
			pps.Scaling8[i] = f8
		}
	}
	return Dequant{newDequantTables(pps)}
}

// Scale4 returns the 4x4 scales of list (0..2 intra Y/Cb/Cr, 3..5 inter).
func (d Dequant) Scale4(list, qp int) *[16]int32 { return &d.t.q4[list][qp] }

// Scale8 returns the 8x8 scales of list (0 intra Y, 1 inter Y).
func (d Dequant) Scale8(list, qp int) *[64]int32 { return &d.t.q8[list][qp] }

// CoeffTokenCode returns the coeff_token code for table t (0..3 by nC class,
// 4 = chroma DC).
func CoeffTokenCode(t, totalCoeff, trailingOnes int) (code, length int) {
	i := 4*totalCoeff + trailingOnes
	if t == 4 {
		return int(chromaDCCoeffTokenBits[i]), int(chromaDCCoeffTokenLen[i])
	}
	return int(coeffTokenBits[t][i]), int(coeffTokenLen[t][i])
}

// TotalZerosCode returns the total_zeros code (chromaDC selects the 2x2
// DC tables).
func TotalZerosCode(totalCoeff, totalZeros int, chromaDC bool) (code, length int) {
	if chromaDC {
		return int(chromaDCTotalZerosBits[totalCoeff-1][totalZeros]), int(chromaDCTotalZerosLen[totalCoeff-1][totalZeros])
	}
	return int(totalZerosBits[totalCoeff-1][totalZeros]), int(totalZerosLen[totalCoeff-1][totalZeros])
}

// RunBeforeCode returns the run_before code given zerosLeft.
func RunBeforeCode(zerosLeft, runBefore int) (code, length int) {
	row := min(zerosLeft-1, 6)
	return int(runBeforeBits[row][runBefore]), int(runBeforeLen[row][runBefore])
}

// CBPCodeNum returns the me(v) codeNum of coded_block_pattern cbp.
func CBPCodeNum(cbp int, intra bool) int {
	t := &golombToInterCBP
	if intra {
		t = &golombToIntraCBP
	}
	for i, v := range t {
		if int(v) == cbp {
			return i
		}
	}
	return 0
}

// MC is a motion-compensation context (scratch buffers for one goroutine).
type MC struct{ s sliceCtx }

// NewMC returns a motion-compensation context.
func NewMC() *MC { return &MC{} }

// Luma predicts the w x h luma block at (x, y) displaced by the
// quarter-sample vector (mvx, mvy) from a pw x ph reference plane.
func (m *MC) Luma(dst []byte, ds int, ref []byte, rs, pw, ph, x, y, mvx, mvy, w, h int) {
	m.s.mcLuma(dst, ds, ref, rs, pw, ph, x+(mvx>>2), y+(mvy>>2), mvx&3, mvy&3, w, h)
}

// Chroma predicts the w x h chroma block at chroma position (x, y) for the
// luma quarter-sample vector (mvx, mvy) (eighth-sample in chroma).
func (m *MC) Chroma(dst []byte, ds int, ref []byte, rs, pw, ph, x, y, mvx, mvy, w, h int) {
	m.s.mcChroma(dst, ds, ref, rs, pw, ph, x+(mvx>>3), y+(mvy>>3), mvx&7, mvy&7, w, h)
}

// Avg2 sets dst = (a + b + 1) >> 1 over a w x h block.
func Avg2(dst []byte, ds int, a []byte, as int, b []byte, bs, w, h int) {
	avg2(dst, ds, a, as, b, bs, w, h)
}

// Macroblock type flags for MBInfo.Type.
const (
	MBIntra4x4   = mtIntra4x4
	MBIntra16x16 = mtIntra16x16
	MBIPCM       = mtIPCM
	MB16x16      = mt16x16
	MB16x8       = mt16x8
	MB8x16       = mt8x16
	MB8x8        = mt8x8
	MBSkip       = mtSkip
	MBDirect     = mtDirect
)

// MBInfo is what deblocking needs to know about one coded macroblock.
type MBInfo struct {
	Slice int32 // index into the slices passed to Deblock
	Type  uint32
	QP    int8
	QPC   [2]int8
	T8x8  bool
	NZ    uint16 // luma 4x4 blocks (raster) with non-zero coefficients
}

// SliceDeblock is the deblocking control of one slice.
type SliceDeblock struct {
	Disable                 int // disable_deblocking_filter_idc
	AlphaOffset, BetaOffset int // slice_alpha/beta_c0_offset_div2 * 2
	ChromaOffset            [2]int
}

// Deblocker deblocks encoder reconstructions with the decoder's filter.
type Deblocker struct {
	pc  picCtx
	pic picture
}

// Deblock filters a picture in place. mv holds per-4x4 (x, y) quarter-sample
// vectors and refID per-4x4 reference-picture identities (-1 unused), both
// in picture raster order of 4x4 blocks (mbW*4 per row), for lists 0 and 1.
func (d *Deblocker) Deblock(y, cb, cr []byte, stride, cstride, mbW, mbH int, mbs []MBInfo, slices []SliceDeblock, mv [2][]int16, refID [2][]int32) {
	pc := &d.pc
	pc.mbW, pc.mbH = mbW, mbH
	n := mbW * mbH
	if cap(pc.mbs) < n {
		pc.mbs = make([]mbState, n)
	}
	pc.mbs = pc.mbs[:n]
	for i := range pc.mbs {
		m := &mbs[i]
		pc.mbs[i] = mbState{slice: m.Slice, typ: m.Type, qp: m.QP, qpc: m.QPC, t8x8: m.T8x8, nzMask: m.NZ}
	}
	pc.slices = pc.slices[:0]
	for _, s := range slices {
		pc.slices = append(pc.slices, sliceInfo{disable: s.Disable, alphaOffset: s.AlphaOffset, betaOffset: s.BetaOffset, chromaOff: s.ChromaOffset})
	}
	p := &d.pic
	p.y, p.cb, p.cr, p.stride, p.cstride, p.mbW, p.mbH = y, cb, cr, stride, cstride, mbW, mbH
	p.mv, p.refID = mv, refID
	pc.deblock(p)
}
